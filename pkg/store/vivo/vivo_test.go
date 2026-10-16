package vivo

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v4/pkg/internal/offloadtest"
	"github.com/KevinGong2013/apkgo/v4/pkg/progress"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

func TestNewForEnvironment(t *testing.T) {
	cfg := map[string]string{
		"access_key":            "production-key",
		"access_secret":         "production-secret",
		"sandbox_access_key":    "sandbox-key",
		"sandbox_access_secret": "sandbox-secret",
	}

	production, err := NewForEnvironment(cfg, store.EnvironmentProduction)
	if err != nil {
		t.Fatalf("production NewForEnvironment: %v", err)
	}
	if production.baseURL != vivoBaseURL || production.client.BaseURL != vivoBaseURL || production.accessKey != "production-key" {
		t.Errorf("production store = baseURL %q, key %q", production.baseURL, production.accessKey)
	}

	sandbox, err := NewForEnvironment(cfg, store.EnvironmentSandbox)
	if err != nil {
		t.Fatalf("sandbox NewForEnvironment: %v", err)
	}
	if sandbox.baseURL != vivoSandboxBaseURL || sandbox.client.BaseURL != vivoSandboxBaseURL || sandbox.accessKey != "sandbox-key" {
		t.Errorf("sandbox store = baseURL %q, key %q", sandbox.baseURL, sandbox.accessKey)
	}
}

func TestNewForEnvironmentValidatesSelectedCredentials(t *testing.T) {
	tests := []struct {
		name string
		cfg  map[string]string
	}{
		{"missing both", map[string]string{}},
		{"missing secret", map[string]string{"sandbox_access_key": "key"}},
		{"missing key", map[string]string{"sandbox_access_secret": "secret"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewForEnvironment(tt.cfg, store.EnvironmentSandbox)
			if err == nil || !strings.Contains(err.Error(), "sandbox_access_key and sandbox_access_secret are required") {
				t.Fatalf("error = %v, want sandbox credential error", err)
			}
		})
	}

	if _, err := NewForEnvironment(map[string]string{
		"sandbox_access_key":    "key",
		"sandbox_access_secret": "secret",
	}, store.EnvironmentSandbox); err != nil {
		t.Fatalf("sandbox-only config should be accepted in sandbox mode: %v", err)
	}
	if _, err := New(map[string]string{
		"sandbox_access_key":    "key",
		"sandbox_access_secret": "secret",
	}); err == nil {
		t.Fatal("production mode accepted config without production credentials")
	}
}

func TestUploadAPKUsesStoreBaseURLAndCredentials(t *testing.T) {
	var gotAccessKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccessKey = r.URL.Query().Get("access_key")
		if got := r.URL.Query().Get("method"); got != "app.upload.apk.app" {
			t.Errorf("method = %q", got)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]string{
				"packageName":  "com.example",
				"serialnumber": "serial",
				"versionCode":  "1",
				"fileMd5":      "md5",
			},
		})
	}))
	defer server.Close()

	file := t.TempDir() + "/app.apk"
	if err := os.WriteFile(file, []byte("apk"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Store{
		baseURL:      server.URL,
		accessKey:    "sandbox-key",
		accessSecret: []byte("sandbox-secret"),
	}
	resp, err := s.uploadAPK(context.Background(), "app.upload.apk.app", "com.example", store.PackageFile{Path: file}, progress.Nop{})
	if err != nil {
		t.Fatalf("uploadAPK: %v", err)
	}
	if resp.SerialNumber != "serial" {
		t.Errorf("serial = %q", resp.SerialNumber)
	}
	if gotAccessKey != "sandbox-key" {
		t.Errorf("access_key = %q, want sandbox-key", gotAccessKey)
	}
}

func TestAuthProbe(t *testing.T) {
	tests := []struct {
		name       string
		body       map[string]any
		wantStatus string
	}{
		// Valid credentials: the gateway accepts the signature and the
		// lookup for the sentinel package comes back empty.
		{"empty success envelope", map[string]any{"code": 0, "data": nil}, "ok"},
		// A business-layer error still proves the signature was
		// accepted — the gateway runs before the business method.
		{"business error", map[string]any{"code": 0, "subCode": "15000", "msg": "内部错误"}, "ok"},
		// Gateway rejection: what vivo actually returns for a bad
		// access_key / signature.
		{"gateway rejection", map[string]any{"code": 10018, "msg": "禁止访问，请核对接入信息"}, "fail"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPackage, gotMethod string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPackage = r.URL.Query().Get("packageName")
				gotMethod = r.URL.Query().Get("method")
				// vivo really serves JSON with a text/plain content
				// type; the probe must not depend on the header.
				w.Header().Set("Content-Type", "text/plain;charset=utf-8")
				_ = json.NewEncoder(w).Encode(tt.body)
			}))
			defer server.Close()

			s := &Store{
				client:       resty.New().SetBaseURL(server.URL),
				baseURL:      server.URL,
				accessKey:    "key",
				accessSecret: []byte("secret"),
			}
			probe := s.authProbe(context.Background())
			if probe.Name != "auth" || probe.Status != tt.wantStatus {
				t.Errorf("probe = %s/%s (%s), want auth/%s", probe.Name, probe.Status, probe.Error, tt.wantStatus)
			}
			if gotMethod != "app.query.details" {
				t.Errorf("method = %q, want app.query.details", gotMethod)
			}
			if gotPackage != sentinelPackage {
				t.Errorf("packageName = %q, want %q", gotPackage, sentinelPackage)
			}
		})
	}
}

// byteCount is a progress.Reporter that tallies an upload's bytes.
type byteCount struct {
	mu          sync.Mutex
	total, sent int64
}

func (c *byteCount) Phase(string) {}

func (c *byteCount) Total(n int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.total = n
}

func (c *byteCount) Add(n int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent += n
}

// is reports whether the total was n and all n bytes were sent.
func (c *byteCount) is(n int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total == n && c.sent == n
}

// signed reports whether q carries the sign signParams computes over its
// other parameters with secret.
func signed(q url.Values, secret string) bool {
	keys := make([]string, 0, len(q))
	for k := range q {
		if k != "sign" {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + q.Get(k)
	}
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(strings.Join(parts, "&")))
	return hmac.Equal([]byte(q.Get("sign")), []byte(hex.EncodeToString(h.Sum(nil))))
}

func md5Hex(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}

// TestUploadSendsThePackage runs single and split uploads with the APKs on
// local disk and, as apkgo-cloud does, with only their facts and an
// Offloader: vivo gets the same bytes either way, under the same signed
// fileMd5.
func TestUploadSendsThePackage(t *testing.T) {
	dir := t.TempDir()
	path32, path64 := writeAPK(t, dir, "app.apk"), writeAPK(t, dir, "app64.apk")
	for _, split := range []bool{false, true} {
		for _, remote := range []bool{false, true} {
			f, s := newFakeVivo(t)
			rep := &byteCount{}
			req := &store.UploadRequest{
				FilePath:     path32,
				PackageName:  "com.example",
				VersionCode:  7,
				ReleaseNotes: "修复若干问题",
				Progress:     rep,
			}
			methods := []string{"app.upload.apk.app", "app.sync.update.app"}
			uploads := map[string]string{"app.upload.apk.app": path32} // method → APK
			if split {
				req.File64Path = path64
				methods = []string{"app.upload.apk.app.32", "app.upload.apk.app.64", "app.sync.update.subpackage.app"}
				uploads = map[string]string{"app.upload.apk.app.32": path32, "app.upload.apk.app.64": path64}
			}
			ctx := context.Background()
			var off *offloadtest.Offloader
			if remote {
				req, ctx, off = offloadtest.Remote(t, ctx, req)
			}
			if res := s.Upload(ctx, req); !res.Success {
				t.Fatalf("split=%v remote=%v: upload failed: %s", split, remote, res.Error)
			}
			if got := f.methods(); !slices.Equal(got, methods) {
				t.Errorf("split=%v remote=%v: methods = %v, want %v", split, remote, got, methods)
			}

			var total int64
			for method, path := range uploads {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				total += int64(len(data))
				calls := f.callsTo(method)
				if len(calls) != 1 {
					t.Errorf("split=%v remote=%v: %d %s calls", split, remote, len(calls), method)
					continue
				}
				c := calls[0]
				if c.file != filepath.Base(path) || !bytes.Equal(c.data, data) {
					t.Errorf("split=%v remote=%v: %s got %q as %q, want %q", split, remote, method, c.data, c.file, data)
				}
				if got, want := c.inURL.Get("fileMd5"), md5Hex(t, path); got != want || !signed(c.inURL, "secret") {
					t.Errorf("split=%v remote=%v: %s fileMd5 = %q (signed %v), want %q signed", split, remote, method, got, signed(c.inURL, "secret"), want)
				}
			}
			if !rep.is(total) {
				t.Errorf("split=%v remote=%v: progress total %d, sent %d, want %d", split, remote, rep.total, rep.sent, total)
			}
			if remote && (!off.Sent(req.FilePath) || (split && !off.Sent(req.File64Path))) {
				t.Errorf("split=%v: the packages did not go out as transfers", split)
			}
		}
	}
}

// URL push hands vivo the APKs' public URLs with their md5s and uploads
// nothing, so without the files here FileFacts.MD5 is all it needs.
func TestURLPushNeedsOnlyTheMD5(t *testing.T) {
	defer func(d time.Duration) { vivoTaskPollPeriod = d }(vivoTaskPollPeriod)
	vivoTaskPollPeriod = time.Millisecond

	dir := t.TempDir()
	path32, path64 := writeAPK(t, dir, "app.apk"), writeAPK(t, dir, "app64.apk")
	for _, split := range []bool{false, true} {
		for _, remote := range []bool{false, true} {
			f, s := newFakeVivo(t)
			req := &store.UploadRequest{
				FilePath:    path32,
				SourceURL:   "https://oss.example.com/app.apk",
				PackageName: "com.example",
				VersionCode: 7,
			}
			method := "app.update.app"
			want := map[string]string{"apkUrl": req.SourceURL, "apkMd5": md5Hex(t, path32)}
			if split {
				req.File64Path, req.Source64URL = path64, "https://oss.example.com/app64.apk"
				method = "app.update.subpackage.app"
				want = map[string]string{
					"apkUrl32": req.SourceURL, "apk32Md5": md5Hex(t, path32),
					"apkUrl64": req.Source64URL, "apkMd5": md5Hex(t, path64),
				}
			}
			ctx := context.Background()
			var off *offloadtest.Offloader
			if remote {
				req, ctx, off = offloadtest.Remote(t, ctx, req)
				// Only the md5: any other fact would be read from the
				// (missing) file and fail the upload.
				req.FileFacts = &store.FileFacts{MD5: req.FileFacts.MD5}
				if split {
					req.File64Facts = &store.FileFacts{MD5: req.File64Facts.MD5}
				}
			}
			if res := s.Upload(ctx, req); !res.Success {
				t.Fatalf("split=%v remote=%v: upload failed: %s", split, remote, res.Error)
			}
			if got := f.methods(); !slices.Equal(got, []string{method, "app.query.task.status"}) {
				t.Fatalf("split=%v remote=%v: methods = %v, want %s then app.query.task.status", split, remote, got, method)
			}
			q := f.callsTo(method)[0].inURL
			for k, v := range want {
				if q.Get(k) != v {
					t.Errorf("split=%v remote=%v: %s %s = %q, want %q", split, remote, method, k, q.Get(k), v)
				}
			}
			if !signed(q, "secret") {
				t.Errorf("split=%v remote=%v: %s is not signed", split, remote, method)
			}
			if remote && len(off.Transfers()) != 0 {
				t.Errorf("split=%v: URL push offered %d transfers", split, len(off.Transfers()))
			}
		}
	}
}
