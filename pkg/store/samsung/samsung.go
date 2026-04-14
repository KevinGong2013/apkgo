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
	"path/filepath"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/pkg/progress"
	"github.com/KevinGong2013/apkgo/pkg/store"
)

func init() {
	store.Register("samsung", store.ConfigSchema{
		Name:       "samsung",
		ConsoleURL: "https://seller.samsungapps.com",
		Fields: []store.FieldSchema{
			{Key: "service_account_id", Required: true, Desc: "Samsung Seller Portal service account ID"},
			{Key: "private_key", Required: true, Desc: "RSA private key (PEM) from Seller Portal"},
			{Key: "content_id", Required: true, Desc: "App content ID in Galaxy Store"},
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
}

type Store struct {
	client           *resty.Client
	serviceAccountID string
	contentID        string
	privateKey       *rsa.PrivateKey
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
		SetBaseURL("https://devapi.samsungapps.com").
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
	rc, _, err := progress.OpenFile(req.FilePath, rep)
	if err != nil {
		return fmt.Errorf("open apk: %w", err)
	}
	defer rc.Close()

	var uploadResp struct {
		FileKey string `json:"fileKey"`
		ErrMsg  string `json:"errorMsg,omitempty"`
	}
	_, err = s.client.R().
		SetQueryParam("sessionId", sessionResp.SessionID).
		SetFileReader("file", filepath.Base(req.FilePath), rc).
		SetResult(&uploadResp).
		Post("/seller/fileUpload")
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	if uploadResp.FileKey == "" {
		return fmt.Errorf("upload failed: %s", uploadResp.ErrMsg)
	}

	// 3. Update content
	rep.Phase("publishing")
	_, err = s.client.R().
		SetBody(map[string]any{
			"contentId": s.contentID,
			"binaryList": []map[string]string{{
				"fileKey":         uploadResp.FileKey,
				"gmsYn":           "Y",
				"nativePlatforms": "APK",
			}},
		}).
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
	_, err = s.client.R().
		SetBody(map[string]string{"accessToken": jwt}).
		SetResult(&resp).
		Post("/auth/accessToken")
	if err != nil {
		return "", err
	}
	if resp.CreatedItem.AccessToken == "" {
		return "", fmt.Errorf("empty access token")
	}
	return resp.CreatedItem.AccessToken, nil
}

func parsePrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
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

func base64url(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
