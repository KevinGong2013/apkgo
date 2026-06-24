package samsung

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v3/pkg/httpx"
	"github.com/KevinGong2013/apkgo/v3/pkg/progress"
	"github.com/KevinGong2013/apkgo/v3/pkg/store"
)

func init() {
	store.Register("samsung", store.ConfigSchema{
		Name:                     "samsung",
		ConsoleURL:               "https://seller.samsungapps.com",
		SupportsScheduledRelease: true,
		Fields: []store.FieldSchema{
			{Key: "service_account_id", Required: true, Desc: "Samsung Seller Portal service account ID"},
			{Key: "private_key", Required: true, Desc: "RSA private key (PEM) from Seller Portal"},
			{Key: "content_id", Required: true, Desc: "App content ID in Galaxy Store"},
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
	store.RegisterAuditor("samsung", audit)
}

// audit is registered with `apkgo audit`. Galaxy Store has no
// query-by-contentId status endpoint, so it lists the seller's apps and
// picks out this content_id's contentStatus, mapping it to the unified
// review state — independent of upload.
func audit(ctx context.Context, cfg map[string]string, q store.AuditQuery) store.AuditResult {
	res := store.AuditResult{Store: "samsung"}
	s, err := New(cfg)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	var list []struct {
		ContentID     string `json:"contentId"`
		ContentName   string `json:"contentName"`
		ContentStatus string `json:"contentStatus"`
	}
	httpResp, err := s.client.R().SetContext(ctx).SetResult(&list).Get("/seller/contentList")
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if httpResp.StatusCode() >= 400 {
		res.Error = fmt.Sprintf("http %d: %s", httpResp.StatusCode(), strings.TrimSpace(string(httpResp.Body())))
		return res
	}
	for _, c := range list {
		if c.ContentID == s.contentID {
			res.State, res.Detail = mapSamsungStatus(c.ContentStatus)
			return res
		}
	}
	res.Error = fmt.Sprintf("content_id %s not found in seller app list", s.contentID)
	return res
}

// mapSamsungStatus maps Galaxy Store contentStatus to the unified state.
// There are ~38 values; we key off keywords (the raw status is always in
// Detail). FOR_SALE/READY_FOR_SALE = approved, *_REJECTED = rejected,
// UNDER_*/READY_*/DELAYED = in review, CANCELED/TERMINATED = withdrawn.
func mapSamsungStatus(status string) (store.AuditState, string) {
	u := strings.ToUpper(status)
	switch {
	case u == "FOR_SALE" || u == "READY_FOR_SALE" || u == "READY_FOR_CHANGE" || u == "SUSPENDED":
		return store.AuditApproved, status
	case strings.Contains(u, "REJECTED"):
		return store.AuditRejected, status
	case strings.Contains(u, "UNDER_") || strings.Contains(u, "READY_FOR_") || strings.Contains(u, "READY_TO_") || strings.Contains(u, "DELAYED"):
		return store.AuditReviewing, status
	case strings.Contains(u, "CANCELED") || u == "TERMINATED":
		return store.AuditWithdrawn, status
	case strings.Contains(u, "REGISTERING") || strings.Contains(u, "UPDATING"):
		return store.AuditUnknown, status + " (not submitted)"
	default:
		return store.AuditUnknown, status
	}
}

type Store struct {
	client           *resty.Client
	serviceAccountID string
	contentID        string
	privateKey       *rsa.PrivateKey
	accessToken      string // also set on resty client; kept here for the streaming upload path
}

const samsungBaseURL = "https://devapi.samsungapps.com"

// samsungAuthHeaders returns the Authorization header used by every
// store API call. Centralised so the streaming upload path stays in
// sync with the resty client's bearer auth.
func samsungAuthHeaders(s *Store) map[string]string {
	return map[string]string{"Authorization": "Bearer " + s.accessToken}
}

func New(cfg map[string]string) (*Store, error) {
	saID := cfg["service_account_id"]
	pkPEM := cfg["private_key"]
	contentID := cfg["content_id"]
	if saID == "" || pkPEM == "" || contentID == "" {
		return nil, fmt.Errorf("service_account_id, private_key, and content_id are required")
	}

	pk, err := parsePrivateKey(pkPEM)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	client := resty.New().
		SetBaseURL(samsungBaseURL).
		SetHeader("Content-Type", "application/json")

	s := &Store{
		client:           client,
		serviceAccountID: saID,
		contentID:        contentID,
		privateKey:       pk,
	}

	// Authenticate
	token, err := s.getAccessToken()
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	s.accessToken = token
	client.SetAuthToken(token)

	return s, nil
}

func (s *Store) Name() string { return "samsung" }

func (s *Store) Upload(ctx context.Context, req *store.UploadRequest) *store.UploadResult {
	start := time.Now()
	if err := s.upload(ctx, req); err != nil {
		return store.ErrResult(s.Name(), start, err)
	}
	return store.NewResult(s.Name(), start)
}

func (s *Store) upload(_ context.Context, req *store.UploadRequest) error {
	rep := progress.Safe(req.Progress)

	// 1. Create upload session
	rep.Phase("auth")
	var sessionResp struct {
		URL       string `json:"url"`
		SessionID string `json:"sessionId"`
	}
	_, err := s.client.R().
		SetBody(map[string]string{}).
		SetResult(&sessionResp).
		Post("/seller/createUploadSessionId")
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	// 2. Upload APK
	rep.Phase("uploading")
	rc, fSize, err := progress.OpenFile(req.FilePath, rep)
	if err != nil {
		return fmt.Errorf("open apk: %w", err)
	}
	defer rc.Close()

	httpResp, err := httpx.DoMultipart(context.Background(), httpx.MultipartRequest{
		Method:  http.MethodPost,
		URL:     samsungBaseURL + "/seller/fileUpload",
		Query:   url.Values{"sessionId": []string{sessionResp.SessionID}},
		Headers: samsungAuthHeaders(s),
		Files:   []httpx.FileField{{Field: "file", FileName: filepath.Base(req.FilePath), Reader: rc, Size: fSize}},
	})
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	defer httpResp.Body.Close()
	body, _ := io.ReadAll(httpResp.Body)
	if httpResp.StatusCode >= 400 {
		return fmt.Errorf("upload failed: http %d: %s", httpResp.StatusCode, strings.TrimSpace(string(body)))
	}
	var uploadResp struct {
		FileKey string `json:"fileKey"`
		ErrMsg  string `json:"errorMsg,omitempty"`
	}
	if jerr := json.Unmarshal(body, &uploadResp); jerr != nil {
		return fmt.Errorf("decode upload response (HTTP %d): %v: %s",
			httpResp.StatusCode, jerr, strings.TrimSpace(string(body)))
	}
	if uploadResp.FileKey == "" {
		return fmt.Errorf("upload failed: %s", uploadResp.ErrMsg)
	}

	// 3. Update content
	rep.Phase("publishing")
	contentBody := map[string]any{
		"contentId": s.contentID,
		"binaryList": []map[string]string{{
			"fileKey":         uploadResp.FileKey,
			"gmsYn":           "Y",
			"nativePlatforms": "APK",
		}},
	}
	if req.ReleaseTime != nil {
		// Scheduled release: publicationType 02 = scheduled date, with
		// startPublicationDate as a Beijing-local datetime string
		// (01 = auto after review, 03 = manual).
		contentBody["publicationType"] = "02"
		contentBody["startPublicationDate"] = store.BeijingLocalTime(*req.ReleaseTime)
	}
	_, err = s.client.R().
		SetBody(contentBody).
		Post("/seller/contentUpdate")
	if err != nil {
		return fmt.Errorf("update content: %w", err)
	}

	// 4. Submit for review
	rep.Phase("submitting")
	_, err = s.client.R().
		SetBody(map[string]string{"contentId": s.contentID}).
		Post("/seller/contentSubmit")
	if err != nil {
		return fmt.Errorf("submit: %w", err)
	}

	return nil
}

func (s *Store) getAccessToken() (string, error) {
	now := time.Now()
	header := base64url(mustJSON(map[string]string{"alg": "RS256", "typ": "JWT"}))
	payload := base64url(mustJSON(map[string]any{
		"iss":    s.serviceAccountID,
		"scopes": []string{"publishing"},
		"iat":    now.Unix(),
		"exp":    now.Add(20 * time.Minute).Unix(),
	}))

	sigInput := header + "." + payload
	hashed := sha256.Sum256([]byte(sigInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.privateKey, crypto.SHA256, hashed[:])
	if err != nil {
		return "", err
	}
	jwt := sigInput + "." + base64url(sig)

	var resp struct {
		CreatedItem struct {
			AccessToken string `json:"accessToken"`
		} `json:"createdItem"`
	}
	// The signed JWT goes in the Authorization header, NOT the request body —
	// Samsung's gateway rejects a bodied JWT with 401 AUTH_REQUIRE ("Set
	// authorization header like Bearer <jwt>").
	httpResp, err := s.client.R().
		SetHeader("Authorization", "Bearer "+jwt).
		SetResult(&resp).
		Post("/auth/accessToken")
	if err != nil {
		return "", err
	}
	if resp.CreatedItem.AccessToken == "" {
		// Samsung answers a bad request (wrong service_account_id, or a
		// private key that doesn't match the account → signature mismatch)
		// with a 200/4xx error body, not a token. resty doesn't treat a
		// non-2xx as an error, so without echoing the status+body this is an
		// opaque "empty access token". Surface it.
		body := strings.TrimSpace(httpResp.String())
		if len(body) > 500 {
			body = body[:500]
		}
		return "", fmt.Errorf("empty access token (http %d): %s", httpResp.StatusCode(), body)
	}
	return resp.CreatedItem.AccessToken, nil
}

func parsePrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		// A PEM pasted through a single-line web form loses its newlines —
		// HTML input value sanitization strips CR/LF — and config files
		// sometimes carry literal "\n" escapes instead of real breaks.
		// Either way pem.Decode sees no block. Rebuild the armored form
		// from whatever BEGIN/END markers and base64 we can find, then retry
		// once before giving up.
		if repaired := normalizePEM(pemStr); repaired != "" {
			block, _ = pem.Decode([]byte(repaired))
		}
	}
	if block == nil {
		return nil, fmt.Errorf("failed to parse PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS1
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA private key")
	}
	return rsaKey, nil
}

// pemArmorRE matches a PEM BEGIN or END line and captures the label
// (e.g. "PRIVATE KEY", "RSA PRIVATE KEY") so both markers can be rebuilt.
var pemArmorRE = regexp.MustCompile(`-----(BEGIN|END) ([A-Z0-9 ]+?)-----`)

// normalizePEM repairs a PEM private key whose line structure was lost in
// transit: flattened onto a single line, or carrying literal "\n"/"\r\n"
// escapes. It locates the first BEGIN and last END markers, keeps only the
// base64 characters of the body between them, and re-emits a canonical block
// with the body wrapped at 64 columns. Returns "" when no usable armor or
// body is found, leaving the caller's original parse error to stand.
func normalizePEM(s string) string {
	// Convert common literal escapes to real newlines first.
	s = strings.NewReplacer(`\r\n`, "\n", `\n`, "\n", `\r`, "\n").Replace(s)

	markers := pemArmorRE.FindAllStringSubmatchIndex(s, -1)
	if len(markers) < 2 {
		return ""
	}
	begin, end := markers[0], markers[len(markers)-1]
	label := s[begin[4]:begin[5]] // capture group 2 of the BEGIN marker
	body := s[begin[1]:end[0]]    // text between the two markers

	var raw strings.Builder
	for i := 0; i < len(body); i++ {
		switch c := body[i]; {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '+', c == '/', c == '=':
			raw.WriteByte(c)
		}
	}
	b64 := raw.String()
	if b64 == "" {
		return ""
	}

	var out strings.Builder
	out.WriteString("-----BEGIN " + label + "-----\n")
	for i := 0; i < len(b64); i += 64 {
		j := i + 64
		if j > len(b64) {
			j = len(b64)
		}
		out.WriteString(b64[i:j])
		out.WriteByte('\n')
	}
	out.WriteString("-----END " + label + "-----\n")
	return out.String()
}

func base64url(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
