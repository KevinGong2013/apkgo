package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		xff        []string
		want       string
	}{
		{"proxied", "10.0.0.2:45440", []string{"203.0.113.9"}, "203.0.113.9"},
		{"proxied takes last hop", "10.0.0.2:45440", []string{"1.1.1.1, 203.0.113.9"}, "203.0.113.9"},
		{"proxied without header", "10.0.0.2:45440", nil, "10.0.0.2"},
		{"public peer ignores header", "198.51.100.4:5000", []string{"1.1.1.1"}, "198.51.100.4"},
		{"loopback peer", "127.0.0.1:5000", []string{"203.0.113.9"}, "203.0.113.9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/v1/events", nil)
			r.RemoteAddr = tt.remoteAddr
			for _, v := range tt.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := clientIP(r); got != tt.want {
				t.Fatalf("clientIP = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRequireToken(t *testing.T) {
	adminToken = "secret"
	h := requireToken(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })

	tests := []struct {
		auth string
		want int
	}{
		{"", 401},
		{"Bearer wrong", 401},
		{"secret", 401},
		{"Bearer secret", 200},
	}
	for _, tt := range tests {
		r := httptest.NewRequest("GET", "/v1/stats", nil)
		if tt.auth != "" {
			r.Header.Set("Authorization", tt.auth)
		}
		w := httptest.NewRecorder()
		h(w, r)
		if w.Code != tt.want {
			t.Errorf("Authorization %q: status %d, want %d", tt.auth, w.Code, tt.want)
		}
	}
}

func TestLoadAdminTokenPersists(t *testing.T) {
	t.Setenv("ADMIN_TOKEN", "")
	dataDir = t.TempDir()

	first, err := loadAdminToken()
	if err != nil || len(first) != 64 {
		t.Fatalf("first load = %q, %v", first, err)
	}
	second, err := loadAdminToken()
	if err != nil || second != first {
		t.Fatalf("second load = %q, %v; want %q", second, err, first)
	}

	t.Setenv("ADMIN_TOKEN", "from-env")
	if got, _ := loadAdminToken(); got != "from-env" {
		t.Fatalf("env override = %q", got)
	}
}
