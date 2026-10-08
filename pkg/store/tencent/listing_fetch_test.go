package tencent

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

func tencentQueryStore(t *testing.T, body string) (*Store, *string) {
	t.Helper()
	var form string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/query_app_detail" {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		form = string(b)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	s, err := New(map[string]string{"user_id": "u1", "access_secret": "secret", "app_id": "123"})
	if err != nil {
		t.Fatal(err)
	}
	s.client.SetBaseURL(srv.URL)
	return s, &form
}

// query_app_detail reports the text but no images, which the result says.
func TestRemoteListing(t *testing.T) {
	s, form := tencentQueryStore(t, `{"ret":0,"msg":"","pkg_name":"com.example.app","app_name":"示例",
		"introduce":"应用简介的内容","one_word_summary":"一句话简介","feature":"版本特性"}`)

	got := s.remoteListing("com.example.app")
	if got.Error != "" {
		t.Fatalf("error: %s", got.Error)
	}
	if got.Store != "tencent" || got.Brief != "一句话简介" || got.Description != "应用简介的内容" || got.Icon != "" || got.Screenshots != nil {
		t.Errorf("listing = %+v", got)
	}
	if want := []string{store.ListingIcon, store.ListingScreenshots}; !reflect.DeepEqual(got.Unavailable, want) {
		t.Errorf("unavailable = %v, want %v", got.Unavailable, want)
	}
	if !strings.Contains(*form, "pkg_name=com.example.app") || !strings.Contains(*form, "app_id=123") {
		t.Errorf("form = %q", *form)
	}
}

func TestRemoteListingError(t *testing.T) {
	s, _ := tencentQueryStore(t, `{"ret":1000009,"msg":"app_id与pkg_name不匹配"}`)
	got := s.remoteListing("com.example.app")
	if !strings.Contains(got.Error, "1000009") {
		t.Errorf("error = %q", got.Error)
	}
	if len(got.Unavailable) != 2 {
		t.Errorf("unavailable = %v, want it reported even on error", got.Unavailable)
	}
}
