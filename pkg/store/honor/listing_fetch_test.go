package honor

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
)

func honorDetailStore(t *testing.T, body string) *Store {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/openapi/v1/publish/get-app-detail" || r.URL.Query().Get("appId") != "123" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return &Store{
		client:      resty.New().SetBaseURL(srv.URL).SetHeader("Content-Type", "application/json"),
		accessToken: "tok",
		configAppID: "123",
	}
}

// The listing read takes zh-CN's text and that language's files from
// get-app-detail: fileType 1 is the icon, 3 the portrait screenshots in
// `order`; the APK and other languages' files are ignored.
func TestRemoteListing(t *testing.T) {
	s := honorDetailStore(t, `{"code":0,"data":{
		"languageInfo":[
			{"languageId":"en-US","appName":"App","intro":"English intro","briefIntro":"English brief"},
			{"languageId":"zh-CN","appName":"应用","intro":"应用介绍","briefIntro":"一句话介绍"}],
		"fileInfo":[
			{"fileName":"app.apk","fileType":100,"fileUrl":"https://f.test/app.apk"},
			{"fileName":"en.png","languageId":"en-US","fileType":1,"fileUrl":"https://f.test/icon-en.png"},
			{"fileName":"zh.png","languageId":"zh-CN","fileType":1,"fileUrl":"https://f.test/icon-zh.png"},
			{"fileName":"s2.png","languageId":"zh-CN","fileType":3,"fileUrl":"https://f.test/s2.png","order":1},
			{"fileName":"s1.png","languageId":"zh-CN","fileType":3,"fileUrl":"https://f.test/s1.png","order":0},
			{"fileName":"s3.png","languageId":"zh-CN","fileType":3,"fileUrl":"https://f.test/s3.png","order":2},
			{"fileName":"e1.png","languageId":"en-US","fileType":3,"fileUrl":"https://f.test/e1.png","order":0}]}}`)

	got := s.remoteListing("com.example.app")
	if got.Error != "" {
		t.Fatalf("error: %s", got.Error)
	}
	if got.Store != "honor" || got.Brief != "一句话介绍" || got.Description != "应用介绍" || got.Icon != "https://f.test/icon-zh.png" {
		t.Errorf("listing = %+v", got)
	}
	if want := []string{"https://f.test/s1.png", "https://f.test/s2.png", "https://f.test/s3.png"}; !reflect.DeepEqual(got.Screenshots, want) {
		t.Errorf("screenshots = %v, want %v", got.Screenshots, want)
	}
}

// An app that only has landscape screenshots still reports them.
func TestRemoteListingLandscape(t *testing.T) {
	s := honorDetailStore(t, `{"code":0,"data":{
		"languageInfo":[{"languageId":"zh-CN","appName":"应用","intro":"应用介绍"}],
		"fileInfo":[
			{"fileType":2,"fileUrl":"https://f.test/l1.png","languageId":"zh-CN","order":0},
			{"fileType":2,"fileUrl":"https://f.test/l2.png","languageId":"zh-CN","order":1}]}}`)

	got := s.remoteListing("com.example.app")
	if got.Brief != "" || got.Icon != "" || len(got.Screenshots) != 2 || got.Screenshots[0] != "https://f.test/l1.png" {
		t.Errorf("listing = %+v", got)
	}
}

func TestRemoteListingError(t *testing.T) {
	s := honorDetailStore(t, `{"code":10003,"msg":"应用不存在"}`)
	if got := s.remoteListing("com.example.app"); !strings.Contains(got.Error, "10003") {
		t.Errorf("error = %q", got.Error)
	}
}
