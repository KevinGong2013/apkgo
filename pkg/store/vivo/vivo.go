package vivo

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/pkg/progress"
	"github.com/KevinGong2013/apkgo/pkg/store"
)

func init() {
	store.Register("vivo", store.ConfigSchema{
		Name:       "vivo",
		ConsoleURL: "https://dev.vivo.com.cn/documentCenter/doc/326",
		Fields: []store.FieldSchema{
			{Key: "access_key", Required: true, Desc: "vivo open platform access key"},
			{Key: "access_secret", Required: true, Desc: "vivo open platform access secret"},
		},
	}, func(cfg map[string]string) (store.Store, error) {
		return New(cfg)
	})
	store.RegisterDiagnoser("vivo", diagnose)
}

type Store struct {
	client       *resty.Client
	accessKey    string
	accessSecret []byte
}

func New(cfg map[string]string) (*Store, error) {
	accessKey := cfg["access_key"]
	accessSecret := cfg["access_secret"]
	if accessKey == "" || accessSecret == "" {
		return nil, fmt.Errorf("access_key and access_secret are required")
	}

	client := resty.New().
		SetBaseURL("https://developer-api.vivo.com.cn/router/rest")

	return &Store{
		client:       client,
		accessKey:    accessKey,
		accessSecret: []byte(accessSecret),
	}, nil
}

func (s *Store) Name() string { return "vivo" }

func (s *Store) Upload(ctx context.Context, req *store.UploadRequest) *store.UploadResult {
	start := time.Now()
	if err := s.upload(ctx, req); err != nil {
		return store.ErrResult(s.Name(), start, err)
	}
	return store.NewResult(s.Name(), start)
}

func (s *Store) upload(_ context.Context, req *store.UploadRequest) error {
	rep := progress.Safe(req.Progress)

	updateReq := map[string]string{
		"packageName":      req.PackageName,
		"versionCode":      strconv.Itoa(int(req.VersionCode)),
		"onlineType":       "1",
		"updateDesc":       req.ReleaseNotes,
		"compatibleDevice": "2",
	}

	// Pre-declare combined upload bytes so the bar is stable across
	// sequential 32/64-bit transfers.
	total, err := sumFileSizes(req.FilePath, req.File64Path)
	if err != nil {
		return fmt.Errorf("stat apk: %w", err)
	}
	rep.Phase("uploading")
	rep.Total(total)

	if req.File64Path != "" {
		// Split package upload
		resp32, err := s.uploadAPK("app.upload.apk.app.32", req.PackageName, req.FilePath, rep)
		if err != nil {
			return fmt.Errorf("upload 32-bit: %w", err)
		}
		resp64, err := s.uploadAPK("app.upload.apk.app.64", req.PackageName, req.File64Path, rep)
		if err != nil {
			return fmt.Errorf("upload 64-bit: %w", err)
		}
		updateReq["apk32"] = resp32.SerialNumber
		updateReq["apk64"] = resp64.SerialNumber
		rep.Phase("publishing")
		return s.updateApp("app.sync.update.subpackage.app", updateReq)
	}

	// Single package upload
	resp, err := s.uploadAPK("app.upload.apk.app", req.PackageName, req.FilePath, rep)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	updateReq["apk"] = resp.SerialNumber
	updateReq["fileMd5"] = resp.FileMD5
	rep.Phase("publishing")
	return s.updateApp("app.sync.update.app", updateReq)
}

// sumFileSizes totals the byte sizes of the given paths. Empty paths are ignored.
func sumFileSizes(paths ...string) (int64, error) {
	var total int64
	for _, p := range paths {
		if p == "" {
			continue
		}
		fi, err := os.Stat(p)
		if err != nil {
			return 0, err
		}
		total += fi.Size()
	}
	return total, nil
}

func (s *Store) uploadAPK(method, packageName, filePath string, rep progress.Reporter) (*uploadResp, error) {
	fileMd5, err := fileMD5(filePath)
	if err != nil {
		return nil, err
	}

	params := s.signParams(method, map[string]string{
		"packageName": packageName,
		"fileMd5":     fileMd5,
	})

	rc, _, err := progress.WrapFile(filePath, rep)
	if err != nil {
		return nil, fmt.Errorf("open apk: %w", err)
	}
	defer rc.Close()

	httpResp, err := s.client.R().
		SetFileReader("file", filepath.Base(filePath), rc).
		SetQueryParams(params).
		Post("")
	if err != nil {
		return nil, err
	}
	body := httpResp.Body()
	var resp struct {
		envelope
		Data *uploadResp `json:"data"`
	}
	if jerr := json.Unmarshal(body, &resp); jerr != nil {
		return nil, fmt.Errorf("decode response (HTTP %d): %v: %s",
			httpResp.StatusCode(), jerr, truncateBody(string(body)))
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("[%d] %s (HTTP %d)",
			resp.Code, resp.text(), httpResp.StatusCode())
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("empty response data (HTTP %d): %s",
			httpResp.StatusCode(), truncateBody(string(body)))
	}
	return resp.Data, nil
}

// truncateBody caps a response body at 500 chars so diagnostic errors stay
// readable.
func truncateBody(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 500 {
		return s[:500] + "...(truncated)"
	}
	return s
}

func (s *Store) updateApp(method string, bizParams map[string]string) error {
	params := s.signParams(method, bizParams)

	httpResp, err := s.client.R().SetQueryParams(params).Post("")
	if err != nil {
		return err
	}
	body := httpResp.Body()
	var resp envelope
	if jerr := json.Unmarshal(body, &resp); jerr != nil {
		return fmt.Errorf("decode response (HTTP %d): %v: %s",
			httpResp.StatusCode(), jerr, truncateBody(string(body)))
	}
	if resp.Code != 0 {
		return fmt.Errorf("[%d] %s (HTTP %d)",
			resp.Code, resp.text(), httpResp.StatusCode())
	}
	return nil
}

// envelope carries vivo's standard top-level error shape. Different
// endpoints use either `msg` or `message` for the human text; the
// helper picks whichever is non-empty so callers don't need to
// remember per-endpoint quirks.
type envelope struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg,omitempty"`
	Message string `json:"message,omitempty"`
}

func (e envelope) text() string {
	if e.Msg != "" {
		return e.Msg
	}
	return e.Message
}

// signParams builds the full param map with HMAC-SHA256 signature.
func (s *Store) signParams(method string, bizParams map[string]string) map[string]string {
	params := map[string]string{
		"method":         method,
		"access_key":     s.accessKey,
		"timestamp":      strconv.FormatInt(time.Now().UnixMilli(), 10),
		"format":         "json",
		"v":              "1.0",
		"sign_method":    "HMAC-SHA256",
		"target_app_key": "developer",
	}
	for k, v := range bizParams {
		params[k] = v
	}

	// Sort and concatenate
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(params[k])
	}

	h := hmac.New(sha256.New, s.accessSecret)
	h.Write([]byte(sb.String()))
	params["sign"] = hex.EncodeToString(h.Sum(nil))

	return params
}

func fileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	io.Copy(h, f)
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

type uploadResp struct {
	PackageName  string `json:"packageName"`
	SerialNumber string `json:"serialnumber"`
	VersionCode  string `json:"versionCode"`
	FileMD5      string `json:"fileMd5"`
}

// appDetails is the slice of fields apkgo needs from `app.query.details`.
// vivo returns more (status, app name in zh, online state, etc.) but we
// only surface what the doctor probe reports.
type appDetails struct {
	PackageName string `json:"packageName"`
	AppName     string `json:"appName"`
	VersionName string `json:"versionName"`
	VersionCode string `json:"versionCode"`
}

// queryApp calls the read-only `app.query.details` method. Used by the
// doctor probe; safe to invoke without side-effects.
//
// The body is unmarshalled by hand because vivo serves /router/rest
// with `Content-Type: text/plain;charset=utf-8` (yes, on a JSON body),
// and resty's auto-decode keys off content-type — so SetResult would
// silently leave the struct zero-valued, making a real auth failure
// look like "no app found".
func (s *Store) queryApp(packageName string) (*appDetails, error) {
	params := s.signParams("app.query.details", map[string]string{
		"packageName": packageName,
	})
	httpResp, err := s.client.R().SetQueryParams(params).Post("")
	if err != nil {
		return nil, err
	}
	body := httpResp.Body()
	var resp struct {
		envelope
		Data *appDetails `json:"data"`
	}
	if jerr := json.Unmarshal(body, &resp); jerr != nil {
		return nil, fmt.Errorf("decode response (HTTP %d): %v: %s",
			httpResp.StatusCode(), jerr, truncateBody(string(body)))
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("[%d] %s (HTTP %d)",
			resp.Code, resp.text(), httpResp.StatusCode())
	}
	return resp.Data, nil
}

// diagnose is registered with `apkgo doctor`. Single probe:
//
//   app-info — calls /router/rest with method=app.query.details, which
//              both validates the HMAC-SHA256 signature server-side and
//              checks that the package exists under this developer
//              account. A package-name hint is required since vivo has
//              no separate "verify credentials" endpoint to probe with
//              an empty body.
func diagnose(ctx context.Context, cfg map[string]string, hint store.DiagnoseHint) []store.Probe {
	probes := make([]store.Probe, 0, 1)

	s, err := New(cfg)
	if err != nil {
		probes = append(probes, store.Probe{Name: "config", Status: "fail", Error: err.Error()})
		return probes
	}

	if hint.Package == "" {
		probes = append(probes, store.Probe{Name: "app-info", Status: "skip", Detail: "needs --package or --file (vivo has no auth-only endpoint)"})
		return probes
	}

	app, err := s.queryApp(hint.Package)
	if err != nil {
		probes = append(probes, store.Probe{Name: "app-info", Status: "fail", Error: err.Error()})
		return probes
	}
	if app == nil {
		probes = append(probes, store.Probe{Name: "app-info", Status: "fail", Error: fmt.Sprintf("no app found for package %s under this developer account", hint.Package)})
		return probes
	}
	detail := fmt.Sprintf("%s → %q versionCode=%s versionName=%s", app.PackageName, app.AppName, app.VersionCode, app.VersionName)
	probes = append(probes, store.Probe{Name: "app-info", Status: "ok", Detail: detail})
	return probes
}
