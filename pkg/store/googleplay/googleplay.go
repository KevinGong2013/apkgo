package googleplay

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
	"os"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/KevinGong2013/apkgo/pkg/store"
)

func init() {
	store.Register("googleplay", store.ConfigSchema{
		Name:       "googleplay",
		ConsoleURL: "https://play.google.com/console",
		Fields: []store.FieldSchema{
			{Key: "json_key_file", Required: true, Desc: "Path to service account JSON key file"},
			{Key: "package_name", Required: true, Desc: "Android package name (e.g. com.example.app)"},
			{Key: "track", Required: false, Desc: "Release track: production, beta, alpha, internal (default: production)"},
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
}

type Store struct {
	client      *resty.Client
	packageName string
	track       string
}

type serviceAccountKey struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

func New(cfg map[string]string) (*Store, error) {
	keyFile := cfg["json_key_file"]
	packageName := cfg["package_name"]
	if keyFile == "" || packageName == "" {
		return nil, fmt.Errorf("json_key_file and package_name are required")
	}

	track := cfg["track"]
	if track == "" {
		track = "production"
	}

	// Parse service account key
	data, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}

	var key serviceAccountKey
	if err := json.Unmarshal(data, &key); err != nil {
		return nil, fmt.Errorf("parse key file: %w", err)
	}

	// Get OAuth token
	token, err := getOAuthToken(key)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	client := resty.New().
		SetBaseURL("https://androidpublisher.googleapis.com/androidpublisher/v3/applications/"+packageName).
		SetAuthToken(token).
		SetHeader("Content-Type", "application/json")

	return &Store{
		client:      client,
		packageName: packageName,
		track:       track,
	}, nil
}

func (s *Store) Name() string { return "googleplay" }

func (s *Store) Upload(ctx context.Context, req *store.UploadRequest) *store.UploadResult {
	start := time.Now()
	if err := s.upload(ctx, req); err != nil {
		return store.ErrResult(s.Name(), start, err)
	}
	return store.NewResult(s.Name(), start)
}

func (s *Store) upload(_ context.Context, req *store.UploadRequest) error {
	// 1. Create edit
	var editResp struct {
		ID string `json:"id"`
	}
	_, err := s.client.R().
		SetBody(map[string]string{}).
		SetResult(&editResp).
		Post("/edits")
	if err != nil {
		return fmt.Errorf("create edit: %w", err)
	}
	editID := editResp.ID
	if editID == "" {
		return fmt.Errorf("empty edit ID")
	}

	// 2. Upload APK
	apkData, err := os.ReadFile(req.FilePath)
	if err != nil {
		return fmt.Errorf("read apk: %w", err)
	}

	var apkResp struct {
		VersionCode int `json:"versionCode"`
	}
	uploadURL := fmt.Sprintf(
		"https://androidpublisher.googleapis.com/upload/androidpublisher/v3/applications/%s/edits/%s/apks",
		s.packageName, editID,
	)
	_, err = s.client.R().
		SetHeader("Content-Type", "application/vnd.android.package-archive").
		SetBody(apkData).
		SetResult(&apkResp).
		Post(uploadURL)
	if err != nil {
		return fmt.Errorf("upload apk: %w", err)
	}

	// 3. Assign to track
	releaseNotes := []map[string]string{}
	if req.ReleaseNotes != "" {
		releaseNotes = append(releaseNotes, map[string]string{
			"language": "en-US",
			"text":     req.ReleaseNotes,
		})
	}

	_, err = s.client.R().
		SetBody(map[string]any{
			"track": s.track,
			"releases": []map[string]any{{
				"versionCodes": []int{apkResp.VersionCode},
				"status":       "completed",
				"releaseNotes": releaseNotes,
			}},
		}).
		Put(fmt.Sprintf("/edits/%s/tracks/%s", editID, s.track))
	if err != nil {
		return fmt.Errorf("set track: %w", err)
	}

	// 4. Commit edit
	_, err = s.client.R().
		Post(fmt.Sprintf("/edits/%s:commit", editID))
	if err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

func getOAuthToken(key serviceAccountKey) (string, error) {
	// Parse private key
	block, _ := pem.Decode([]byte(key.PrivateKey))
	if block == nil {
		return "", fmt.Errorf("failed to parse private key PEM")
	}
	pk, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse private key: %w", err)
	}
	rsaKey, ok := pk.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("not an RSA key")
	}

	// Build JWT
	now := time.Now()
	header := base64url(mustJSON(map[string]string{"alg": "RS256", "typ": "JWT"}))
	claims := base64url(mustJSON(map[string]any{
		"iss":   key.ClientEmail,
		"scope": "https://www.googleapis.com/auth/androidpublisher",
		"aud":   key.TokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}))

	sigInput := header + "." + claims
	hashed := sha256.Sum256([]byte(sigInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, hashed[:])
	if err != nil {
		return "", err
	}
	jwt := sigInput + "." + base64url(sig)

	// Exchange for access token
	tokenURI := key.TokenURI
	if tokenURI == "" {
		tokenURI = "https://oauth2.googleapis.com/token"
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error,omitempty"`
	}
	_, err = resty.New().R().
		SetFormData(map[string]string{
			"grant_type": "urn:ietf:params:oauth:grant-type:jwt-bearer",
			"assertion":  jwt,
		}).
		SetResult(&tokenResp).
		Post(tokenURI)
	if err != nil {
		return "", err
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("token error: %s", tokenResp.Error)
	}
	return tokenResp.AccessToken, nil
}

func base64url(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
