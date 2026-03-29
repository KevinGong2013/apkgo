package xiaomi

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"image/png"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/KevinGong2013/apkgo/pkg/store"
	"github.com/shogo82148/androidbinary"
	"github.com/shogo82148/androidbinary/apk"
)

func init() {
	store.Register("xiaomi", store.ConfigSchema{
		Name: "xiaomi",
		Fields: []store.FieldSchema{
			{Key: "email", Required: true, Desc: "Xiaomi developer account email"},
			{Key: "private_key", Required: true, Desc: "Xiaomi API private key"},
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
}

type Store struct {
	client     *resty.Client
	email      string
	privateKey string
	pubKey     *rsa.PublicKey
}

func New(cfg map[string]string) (*Store, error) {
	email := cfg["email"]
	privateKey := cfg["private_key"]
	if email == "" || privateKey == "" {
		return nil, fmt.Errorf("email and private_key are required")
	}

	pubKey, err := loadPublicKey()
	if err != nil {
		return nil, fmt.Errorf("load xiaomi public key: %w", err)
	}

	client := resty.New().
		SetBaseURL("http://api.developer.xiaomi.com/devupload")

	return &Store{
		client:     client,
		email:      email,
		privateKey: privateKey,
		pubKey:     pubKey,
	}, nil
}

func (s *Store) Name() string { return "xiaomi" }

func (s *Store) Upload(ctx context.Context, req *store.UploadRequest) *store.UploadResult {
	start := time.Now()
	if err := s.upload(ctx, req); err != nil {
		return store.ErrResult(s.Name(), start, err)
	}
	return store.NewResult(s.Name(), start)
}

func (s *Store) upload(ctx context.Context, req *store.UploadRequest) error {
	// Query existing version
	info, err := s.query(req.PackageName)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}

	synchroType := 1 // update
	if info == nil {
		synchroType = 0 // new app
	} else if info.VersionCode >= int(req.VersionCode) {
		return fmt.Errorf("store version (%d) >= local version (%d)", info.VersionCode, req.VersionCode)
	}

	// Extract icon from APK
	iconPath, err := extractIcon(req.FilePath)
	if err != nil {
		return fmt.Errorf("extract icon: %w", err)
	}
	defer os.Remove(iconPath)

	// Push
	return s.push(synchroType, req, iconPath)
}

func (s *Store) query(packageName string) (*packageInfo, error) {
	body := s.encode(map[string]any{
		"packageName": packageName,
		"userName":    s.email,
	}, nil)

	var resp struct {
		Result      int          `json:"result"`
		PackageInfo *packageInfo `json:"packageInfo"`
	}
	_, err := s.client.R().
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetBody(body.Encode()).
		SetResult(&resp).
		Post("/dev/query")

	return resp.PackageInfo, err
}

func (s *Store) push(synchroType int, req *store.UploadRequest, iconPath string) error {
	appInfo := map[string]any{
		"appName":     req.AppName,
		"packageName": req.PackageName,
		"updateDesc":  req.ReleaseNotes,
	}

	files := map[string]string{
		"apk":  req.FilePath,
		"icon": iconPath,
	}
	if req.File64Path != "" {
		files["secondApkPath"] = req.File64Path
	}

	body := s.encode(map[string]any{
		"synchroType": synchroType,
		"userName":    s.email,
		"appInfo":     appInfo,
	}, files)

	r := s.client.R().
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetFormDataFromValues(body).
		SetFile("apk", req.FilePath).
		SetFile("icon", iconPath)

	if req.File64Path != "" {
		r.SetFile("secondApkPath", req.File64Path)
	}

	var resp struct {
		Result  int    `json:"result"`
		Message string `json:"message,omitempty"`
	}
	_, err := r.SetResult(&resp).Post("/dev/push")
	if err != nil {
		return err
	}
	if resp.Result != 0 {
		return fmt.Errorf("push failed: %s", resp.Message)
	}
	return nil
}

// encode builds form values with RSA-encrypted SIG.
func (s *Store) encode(params map[string]any, files map[string]string) url.Values {
	requestData, _ := json.Marshal(params)
	form := url.Values{}
	form.Set("RequestData", string(requestData))

	sigs := []map[string]string{{
		"name": "RequestData",
		"hash": md5hex(requestData),
	}}

	for key, path := range files {
		if path != "" {
			hash, err := fileMD5(path)
			if err != nil {
				continue
			}
			sigs = append(sigs, map[string]string{"name": key, "hash": hash})
		}
	}

	sigPayload, _ := json.Marshal(map[string]any{
		"sig":      sigs,
		"password": s.privateKey,
	})

	encrypted, _ := rsaEncrypt(sigPayload, s.pubKey)
	form.Set("SIG", encrypted)

	return form
}

// --- Crypto helpers ---

const publicCert = `-----BEGIN CERTIFICATE-----
MIICQTCCAaoCCQDab4c81p7I/jANBgkqhkiG9w0BAQQFADBqMQswCQYDVQQGEwJD
TjEQMA4GA1UECBMHQmVpSmluZzEQMA4GA1UEBxMHQmVpSmluZzEPMA0GA1UEChMG
eGlhb21pMQ0wCwYDVQQLEwRtaXVpMRcwFQYDVQQDEw5kZXYueGlhb21pLmNvbTAe
Fw0xMzA1MTUwMzMyNDJaFw0yMzA1MTMwMzMyNDJaMGAxCzAJBgNVBAYTAkNOMQsw
CQYDVQQIEwJCSjELMAkGA1UEBxMCQkoxDjAMBgNVBAoTBWNvbGluMQ4wDAYDVQQL
EwVjb2xpbjEXMBUGA1UEAxMOZGV2LnhpYW9taS5jb20wgZ8wDQYJKoZIhvcNAQEB
BQADgY0AMIGJAoGBAMBf5LzEiMy0i8LeENXU9v0bTF4coM/kLfK6RvjWS69/6tUx
NxJvjDFNbLsmU4xpF3qFY9RI0qyRf79pmKfYUeWomQCM/hKo2lKIbWV7/RVheZhE
C2yGbUMRygIzJq3AChBT2MO1a7bA9LINcv+xLmoy5+l3MnVwbVUpWsC/GI59AgMB
AAEwDQYJKoZIhvcNAQEEBQADgYEAQfYL1/EdtTXJthFzQxfdKt6y3Ts3b3waTn6o
d9b+LCcU8EzKHmFOAIpkqIOTvrhB3o/KXEMeMI0PiNHuFnHv9+VGQKiaPFQtb9Ds
T8iowNDb4G8rdUcoVaczUDbBMG9r5J45UCDxaEzcjp6J0xIS3v11JBK1PtAKHY6R
nEJIZuc=
-----END CERTIFICATE-----`

func loadPublicKey() (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(publicCert))
	if block == nil {
		return nil, fmt.Errorf("failed to parse PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA public key")
	}
	return pub, nil
}

func rsaEncrypt(data []byte, pub *rsa.PublicKey) (string, error) {
	const blockSize = 117 // 1024/8 - 11
	var encrypted []byte
	for len(data) > 0 {
		chunk := blockSize
		if len(data) < chunk {
			chunk = len(data)
		}
		enc, err := rsa.EncryptPKCS1v15(rand.Reader, pub, data[:chunk])
		if err != nil {
			return "", err
		}
		encrypted = append(encrypted, enc...)
		data = data[chunk:]
	}
	return fmt.Sprintf("%x", encrypted), nil
}

func md5hex(data []byte) string {
	return fmt.Sprintf("%x", md5.Sum(data))
}

func fileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func extractIcon(apkPath string) (string, error) {
	pkg, err := apk.OpenFile(apkPath)
	if err != nil {
		return "", err
	}
	defer pkg.Close()

	icon, err := pkg.Icon(&androidbinary.ResTableConfig{Size: 512})
	if err != nil {
		return "", err
	}

	iconPath := filepath.Join(filepath.Dir(apkPath), "apkgo_icon_tmp.png")
	var buf bytes.Buffer
	if err := png.Encode(&buf, icon); err != nil {
		return "", err
	}
	if err := os.WriteFile(iconPath, buf.Bytes(), 0644); err != nil {
		return "", err
	}
	return iconPath, nil
}

type packageInfo struct {
	AppName     string `json:"appName"`
	PackageName string `json:"packageName"`
	VersionCode int    `json:"versionCode"`
	VersionName string `json:"versionName"`
}
