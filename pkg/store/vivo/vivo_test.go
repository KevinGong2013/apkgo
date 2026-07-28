package vivo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

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
