package oppo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"
)

func TestParseError(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "envelope message with empty array data",
			body: `{"errno": 10001, "message": "应用不存在", "data": []}`,
			want: "应用不存在",
		},
		{
			name: "envelope msg with empty array data",
			body: `{"errno": 10002, "msg": "签名错误", "data": []}`,
			want: "签名错误",
		},
		{
			name: "data nested message",
			body: `{"errno": 10003, "data": {"message": "内部错误"}}`,
			want: "内部错误",
		},
		{
			name: "data nested msg",
			body: `{"errno": 10004, "data": {"msg": "参数错误"}}`,
			want: "参数错误",
		},
		{
			name: "no message fields, fallback to raw body",
			body: `{"errno": 10005, "data": []}`,
			want: `{"errno": 10005, "data": []}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseError([]byte(tt.body))
			if got != tt.want {
				t.Errorf("parseError() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestQueryApp_EmptyArrayData(t *testing.T) {
	// OPPO returns data: [] when no app exists under this developer account
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"errno": 0, "data": []}`))
	}))
	defer srv.Close()

	s := &Store{
		client:       resty.New().SetBaseURL(srv.URL),
		accessToken:  "test-token",
		clientSecret: "test-secret",
	}

	app, err := s.queryApp(context.Background(), "com.example.nonexistent")
	if err != nil {
		t.Fatalf("expected nil error on empty data array, got: %v", err)
	}
	if app != nil {
		t.Fatalf("expected nil app on empty data array, got: %+v", app)
	}
}

func TestQueryApp_ErrorWithArrayData(t *testing.T) {
	// OPPO returns errno != 0 along with data: []
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"errno": 10001, "message": "应用不存在或无权访问", "data": []}`))
	}))
	defer srv.Close()

	s := &Store{
		client:       resty.New().SetBaseURL(srv.URL),
		accessToken:  "test-token",
		clientSecret: "test-secret",
	}

	app, err := s.queryApp(context.Background(), "com.example.nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if app != nil {
		t.Fatalf("expected nil app on error, got: %+v", app)
	}
	expectedErr := "[10001] 应用不存在或无权访问"
	if err.Error() != expectedErr {
		t.Errorf("error = %q, want %q", err.Error(), expectedErr)
	}
}

func TestQueryApp_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"errno": 0, "data": {"app_name": "Test App", "version_name": "1.0.0", "version_code": "100"}}`))
	}))
	defer srv.Close()

	s := &Store{
		client:       resty.New().SetBaseURL(srv.URL),
		accessToken:  "test-token",
		clientSecret: "test-secret",
	}

	app, err := s.queryApp(context.Background(), "com.example.app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if app == nil {
		t.Fatal("expected non-nil app")
	}
	if app.AppName != "Test App" || app.VersionName != "1.0.0" || app.VersionCode != "100" {
		t.Errorf("app data mismatch: %+v", app)
	}
}
