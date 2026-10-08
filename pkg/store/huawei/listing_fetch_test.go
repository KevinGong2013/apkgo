package huawei

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
)

func agcInfoStore(t *testing.T, body string) *Store {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/publish/v2/app-info" || r.URL.Query().Get("appId") != "app-1" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return &Store{client: resty.New().SetBaseURL(srv.URL), configAppID: "app-1"}
}

// The listing read takes the default language's text from app-info and the
// phone's images from its deviceMaterials (doc: 查询应用信息 response).
func TestRemoteListing(t *testing.T) {
	s := agcInfoStore(t, `{"ret":{"code":0,"msg":"success"},
		"appInfo":{"defaultLang":"zh-CN","releaseState":0},
		"languages":[
			{"lang":"en-US","appName":"App","appDesc":"English description","briefInfo":"English brief","icon":"https://img.test/en.png"},
			{"lang":"zh-CN","appName":"应用","appDesc":"应用介绍","briefInfo":"一句话简介",
			 "icon":"https://img.test/lang-icon.png","introPic":"https://img.test/old1.png,https://img.test/old2.png",
			 "deviceMaterials":[
				{"deviceType":5,"appIcon":"https://img.test/pad.png","screenShots":["https://img.test/pad1.png"]},
				{"deviceType":4,"appIcon":"https://img.test/phone.png","screenShots":["https://img.test/p1.png","https://img.test/p2.png","https://img.test/p3.png"]}]}]}`)

	got := s.remoteListing(context.Background(), "com.example.app")
	if got.Error != "" {
		t.Fatalf("error: %s", got.Error)
	}
	if got.Store != "huawei" || got.Brief != "一句话简介" || got.Description != "应用介绍" || got.Icon != "https://img.test/phone.png" {
		t.Errorf("listing = %+v", got)
	}
	if want := []string{"https://img.test/p1.png", "https://img.test/p2.png", "https://img.test/p3.png"}; !reflect.DeepEqual(got.Screenshots, want) {
		t.Errorf("screenshots = %v, want %v", got.Screenshots, want)
	}
}

// Without usable deviceMaterials the language-level icon / introPic are
// used; without a default language zh-CN is, then the first entry.
func TestRemoteListingFallbacks(t *testing.T) {
	s := agcInfoStore(t, `{"ret":{"code":0},
		"languages":[
			{"lang":"en-US","appDesc":"English description"},
			{"lang":"zh-CN","appDesc":"应用介绍","icon":"https://img.test/icon.png",
			 "introPic":"https://img.test/1.png,https://img.test/2.png","deviceMaterials":"unexpected"}]}`)
	got := s.remoteListing(context.Background(), "com.example.app")
	if got.Error != "" || got.Description != "应用介绍" || got.Icon != "https://img.test/icon.png" || len(got.Screenshots) != 2 {
		t.Errorf("zh-CN fallback = %+v", got)
	}

	s = agcInfoStore(t, `{"ret":{"code":0},"appInfo":{"defaultLang":"fr-FR"},
		"languages":[{"lang":"en-US","appDesc":"English description","briefInfo":"English brief"}]}`)
	if got := s.remoteListing(context.Background(), "com.example.app"); got.Description != "English description" || got.Brief != "English brief" {
		t.Errorf("first-entry fallback = %+v", got)
	}
}

func TestRemoteListingErrors(t *testing.T) {
	s := agcInfoStore(t, `{"ret":{"code":204144647,"msg":"app not exist"}}`)
	if got := s.remoteListing(context.Background(), "com.example.app"); !strings.Contains(got.Error, "204144647") {
		t.Errorf("error = %q", got.Error)
	}
	s = agcInfoStore(t, `{"ret":{"code":0},"appInfo":{"defaultLang":"zh-CN"}}`)
	if got := s.remoteListing(context.Background(), "com.example.app"); !strings.Contains(got.Error, "no languages") {
		t.Errorf("error = %q", got.Error)
	}
}
