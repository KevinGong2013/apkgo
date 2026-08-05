package vivo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v3/pkg/progress"
	"github.com/KevinGong2013/apkgo/v3/pkg/store"
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
	resp, err := s.uploadAPK("app.upload.apk.app", "com.example", file, progress.Nop{})
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
