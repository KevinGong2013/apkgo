package vivo

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

func vivoQueryStore(t *testing.T, body string) (*Store, *string) {
	t.Helper()
	var method string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.URL.Query().Get("method")
		// vivo serves JSON as text/plain.
		w.Header().Set("Content-Type", "text/plain;charset=utf-8")
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return &Store{client: resty.New().SetBaseURL(srv.URL), baseURL: srv.URL, accessKey: "key", accessSecret: []byte("secret")}, &method
}

// The listing read maps app.query.details' simpleDesc / detailDesc / icon /
// screenshot (doc 346), splitting the comma-joined URLs. versionCode is a
// string here, as the live API sends it (the doc's sample shows a number).
func TestRemoteListing(t *testing.T) {
	s, method := vivoQueryStore(t, `{"code":0,"subCode":"0","msg":"成功","data":{
		"packageName":"com.test.test","status":3,"versionCode":"12","versionName":"1.2",
		"icon":"https://img.test/icon.png",
		"detailDesc":"应用介绍","simpleDesc":"一句话简介",
		"screenshot":"https://img.test/1.png,https://img.test/2.png, https://img.test/3.png",
		"updateDesc":"应用更新内容介绍"}}`)

	got := s.remoteListing(context.Background(), "com.test.test")
	if got.Error != "" {
		t.Fatalf("error: %s", got.Error)
	}
	if *method != "app.query.details" {
		t.Errorf("method = %q", *method)
	}
	if got.Store != "vivo" || got.Brief != "一句话简介" || got.Description != "应用介绍" || got.Icon != "https://img.test/icon.png" {
		t.Errorf("listing = %+v", got)
	}
	if want := []string{"https://img.test/1.png", "https://img.test/2.png", "https://img.test/3.png"}; !reflect.DeepEqual(got.Screenshots, want) {
		t.Errorf("screenshots = %v", got.Screenshots)
	}
}

func TestRemoteListingErrors(t *testing.T) {
	s, _ := vivoQueryStore(t, `{"code":0,"subCode":"11011","msg":"开发者账号不存在该应用"}`)
	if got := s.remoteListing(context.Background(), "com.missing"); !strings.Contains(got.Error, "11011") {
		t.Errorf("business error = %q", got.Error)
	}
	s, _ = vivoQueryStore(t, `{"code":0,"subCode":"0","msg":"成功"}`)
	if got := s.remoteListing(context.Background(), "com.missing"); !strings.Contains(got.Error, "no app found") {
		t.Errorf("empty data error = %q", got.Error)
	}
}
