package huawei

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// AGC refuses a downloadFileName over 64 characters (203489281), which the
// name inside an object-storage link routinely is.
func TestByURLFileName(t *testing.T) {
	sha := strings.Repeat("a1b2c3d4", 8) // 64 hex chars, like a content-addressed key
	longPkg := "com." + strings.Repeat("verylongsegment.", 5) + "app"
	cases := []struct {
		name, url, pkg, want string
	}{
		{"short name kept", "https://cdn.example.com/dl/app-release.apk", "com.example.app", "app-release.apk"},
		{"query and fragment dropped", "https://cdn.example.com/dl/app.apk?e=1760000000&token=ak:sig#x", "com.example.app", "app.apk"},
		{"exactly 64 kept", "https://cdn.example.com/" + strings.Repeat("n", 60) + ".apk", "com.example.app", strings.Repeat("n", 60) + ".apk"},
		{"sha256 key falls back to package", "https://oss.example.com/apks/org/app/" + sha + ".apk?e=1&token=t", "com.example.app", "com.example.app.apk"},
		{"suffix follows the link", "https://oss.example.com/apks/" + sha + ".aab", "com.example.app", "com.example.app.aab"},
		{"no name in the link", "https://oss.example.com/", "com.example.app", "com.example.app.apk"},
		{"no suffix in the link", "https://oss.example.com/download", "com.example.app", "com.example.app.apk"},
		{"long name and long package: tail of the name", "https://oss.example.com/" + sha + "-release.apk", longPkg, (sha + "-release")[len(sha+"-release")-60:] + ".apk"},
	}
	for _, c := range cases {
		got := byURLFileName(c.url, c.pkg)
		if got != c.want {
			t.Errorf("%s: byURLFileName = %q, want %q", c.name, got, c.want)
		}
		if n := len([]rune(got)); n > maxDownloadFileName {
			t.Errorf("%s: %q is %d characters, over AGC's %d", c.name, got, n, maxDownloadFileName)
		}
	}
}

// The by-url request carries the full link and a name AGC accepts.
func TestSubmitPackageByURLFileName(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/publish/v2/app-package-file/by-url" || r.URL.Query().Get("appId") != "app-1" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		// Like AGC: over-long names are a validation error.
		if name, _ := body["downloadFileName"].(string); len(name) > 64 {
			io.WriteString(w, `{"ret":{"code":203489281,"msg":"addPackageFileByUrl.addPackageFileReq.downloadFileName: size must be between 0 and 64"}}`)
			return
		}
		io.WriteString(w, `{"ret":{"code":0,"msg":"success"}}`)
	}))
	defer srv.Close()
	s := &Store{client: resty.New().SetBaseURL(srv.URL)}

	link := "https://oss.example.com/apks/0cb88316/7f3a/" + strings.Repeat("a1b2c3d4", 8) + ".apk?e=1760000000&token=ak:sig"
	if err := s.submitPackageByURL("app-1", link, &store.UploadRequest{PackageName: "com.example.app", VersionCode: 7}); err != nil {
		t.Fatalf("submitPackageByURL: %v", err)
	}
	if body["downloadUrl"] != link {
		t.Errorf("downloadUrl = %v, want the link unchanged", body["downloadUrl"])
	}
	if body["downloadFileName"] != "com.example.app.apk" {
		t.Errorf("downloadFileName = %v", body["downloadFileName"])
	}
}
