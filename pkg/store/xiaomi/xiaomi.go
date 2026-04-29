package xiaomi

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"image/png"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/shogo82148/androidbinary"
	"github.com/shogo82148/androidbinary/apk"

	"github.com/KevinGong2013/apkgo/pkg/progress"
	"github.com/KevinGong2013/apkgo/pkg/store"
)

func init() {
	store.Register("xiaomi", store.ConfigSchema{
		Name:       "xiaomi",
		ConsoleURL: "https://dev.mi.com/xiaomihyperos/documentation/detail?pId=1134",
		Fields: []store.FieldSchema{
			{Key: "email", Required: true, Desc: "Xiaomi developer account email (mapped to userName)"},
			{Key: "private_key", Required: true, Desc: "Xiaomi API private key (the value the upload SDK calls 'password')"},
			{Key: "cert", Required: false, Desc: "Xiaomi public key certificate (raw PEM or base64); required unless cert_file is set"},
			{Key: "cert_file", Required: false, Desc: "Path to Xiaomi public key certificate file (.cer/.pem)"},
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
	store.RegisterDiagnoser("xiaomi", diagnose)
}

type Store struct {
	client     *resty.Client
	email      string
	privateKey string
	pubKey     *rsa.PublicKey
}

func New(cfg map[string]string) (*Store, error) {
	email := strings.TrimSpace(cfg["email"])
	privateKey := strings.TrimSpace(cfg["private_key"])
	if email == "" || privateKey == "" {
		return nil, fmt.Errorf("email and private_key are required")
	}

	certInline := strings.TrimSpace(cfg["cert"])
	certFile := strings.TrimSpace(cfg["cert_file"])
	if certInline == "" && certFile == "" {
		return nil, fmt.Errorf("xiaomi: configure cert or cert_file (download the public-key certificate from dev.mi.com)")
	}
	pubKey, err := func() (*rsa.PublicKey, error) {
		if certInline != "" {
			return loadCert(certInline)
		}
		return loadCertFromFile(certFile)
	}()
	if err != nil {
		return nil, fmt.Errorf("load xiaomi public key: %w", err)
	}

	client := resty.New().
		SetBaseURL("https://api.developer.xiaomi.com/devupload")

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
	rep := progress.Safe(req.Progress)

	// Query existing version
	rep.Phase("query")
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
	rep.Phase("icon")
	iconPath, err := extractIcon(req.FilePath)
	if err != nil {
		return fmt.Errorf("extract icon: %w", err)
	}
	defer os.Remove(iconPath)

	// Push
	return s.push(synchroType, req, iconPath, rep)
}

func (s *Store) query(packageName string) (*packageInfo, error) {
	body := s.encode(map[string]any{
		"packageName": packageName,
		"userName":    s.email,
	}, nil)

	var resp struct {
		Result      int          `json:"result"`
		Message     string       `json:"message,omitempty"`
		Reason      string       `json:"reason,omitempty"`
		PackageInfo *packageInfo `json:"packageInfo"`
	}
	httpResp, err := s.client.R().
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetBody(body.Encode()).
		SetResult(&resp).
		Post("/dev/query")
	if err != nil {
		return nil, err
	}
	if httpResp.IsError() {
		return nil, fmt.Errorf("http %d: %s", httpResp.StatusCode(), strings.TrimSpace(string(httpResp.Body())))
	}
	if resp.Result != 0 {
		msg := resp.Message
		if msg == "" {
			msg = resp.Reason
		}
		if msg == "" {
			msg = strings.TrimSpace(string(httpResp.Body()))
		}
		return nil, fmt.Errorf("[%d] %s", resp.Result, msg)
	}
	return resp.PackageInfo, nil
}

func (s *Store) push(synchroType int, req *store.UploadRequest, iconPath string, rep progress.Reporter) error {
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

	// Wrap every streamed file so the same reporter sees combined progress.
	// Set Total once to the sum of all wrapped file sizes.
	rep.Phase("uploading")
	apkRC, apkSize, err := progress.WrapFile(req.FilePath, rep)
	if err != nil {
		return fmt.Errorf("open apk: %w", err)
	}
	defer apkRC.Close()

	iconRC, iconSize, err := progress.WrapFile(iconPath, rep)
	if err != nil {
		return fmt.Errorf("open icon: %w", err)
	}
	defer iconRC.Close()

	var (
		apk64RC   io.ReadCloser
		apk64Size int64
	)
	if req.File64Path != "" {
		apk64RC, apk64Size, err = progress.WrapFile(req.File64Path, rep)
		if err != nil {
			return fmt.Errorf("open apk64: %w", err)
		}
		defer apk64RC.Close()
	}
	rep.Total(apkSize + iconSize + apk64Size)

	r := s.client.R().
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetFormDataFromValues(body).
		SetFileReader("apk", filepath.Base(req.FilePath), apkRC).
		SetFileReader("icon", filepath.Base(iconPath), iconRC)

	if apk64RC != nil {
		r.SetFileReader("secondApkPath", filepath.Base(req.File64Path), apk64RC)
	}

	var resp struct {
		Result  int    `json:"result"`
		Message string `json:"message,omitempty"`
	}
	_, err = r.SetResult(&resp).Post("/dev/push")
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

// loadCert accepts either:
//   - the raw PEM-encoded X.509 certificate (string contains "-----BEGIN"), or
//   - inline base64-encoded PEM (handy for env vars / CI secrets)
//
// and returns the embedded RSA public key. The Xiaomi developer console
// distributes this cert per-account; there is no global default any more.
func loadCert(raw string) (*rsa.PublicKey, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty cert")
	}
	var pemBytes []byte
	if strings.Contains(raw, "BEGIN") {
		pemBytes = []byte(raw)
	} else {
		var err error
		for _, dec := range []*base64.Encoding{
			base64.StdEncoding, base64.RawStdEncoding,
			base64.URLEncoding, base64.RawURLEncoding,
		} {
			pemBytes, err = dec.DecodeString(raw)
			if err == nil {
				break
			}
		}
		if err != nil {
			return nil, fmt.Errorf("decode base64: %w", err)
		}
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("invalid PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}
	if !cert.NotAfter.IsZero() && time.Now().After(cert.NotAfter) {
		return nil, fmt.Errorf("certificate expired on %s — re-download from dev.mi.com", cert.NotAfter.Format("2006-01-02"))
	}
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA public key")
	}
	return pub, nil
}

func loadCertFromFile(path string) (*rsa.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return loadCert(string(data))
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

// diagnose is registered with `apkgo doctor`. It runs two layered probes:
//
//  1. cert  — public-key certificate is loadable, RSA, and not expired
//  2. query — /dev/query accepts the SIG (proves email + private_key + cert
//             all line up) and returns the stored package info if any
//
// Probe 2 needs a package-name hint and is reported as skipped without it,
// since /dev/query requires `packageName`.
func diagnose(ctx context.Context, cfg map[string]string, hint store.DiagnoseHint) []store.Probe {
	probes := make([]store.Probe, 0, 2)

	s, err := New(cfg)
	if err != nil {
		probes = append(probes, store.Probe{Name: "cert", Status: "fail", Error: err.Error()})
		return probes
	}
	probes = append(probes, store.Probe{Name: "cert", Status: "ok", Detail: fmt.Sprintf("RSA-%d public key loaded", s.pubKey.N.BitLen())})

	if hint.Package == "" {
		probes = append(probes, store.Probe{Name: "query", Status: "skip", Detail: "needs --package or --file"})
		return probes
	}

	info, err := s.query(hint.Package)
	if err != nil {
		probes = append(probes, store.Probe{Name: "query", Status: "fail", Error: err.Error()})
		return probes
	}
	detail := fmt.Sprintf("%s not yet uploaded under this account", hint.Package)
	if info != nil {
		detail = fmt.Sprintf("%s → versionCode=%d versionName=%s", info.PackageName, info.VersionCode, info.VersionName)
	}
	probes = append(probes, store.Probe{Name: "query", Status: "ok", Detail: detail})
	return probes
}
