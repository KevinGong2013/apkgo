package huawei

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

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

// fakeDownloadAGC is an AGC that publishes by download: it records the
// calls and answers app-info from a scripted sequence (the last entry
// repeats), like the real one does while it fetches the package.
type fakeDownloadAGC struct {
	t          *testing.T
	srv        *httptest.Server
	submitCode int      // ret.code of app-submit-with-file
	infos      []string // app-info bodies, in order

	mu     sync.Mutex
	calls  []string
	submit map[string]any
	polls  int
}

func newFakeDownloadAGC(t *testing.T, infos ...string) (*fakeDownloadAGC, *Store) {
	t.Helper()
	poll, wait := urlPublishPoll, urlPublishMaxWait
	urlPublishPoll, urlPublishMaxWait = time.Millisecond, 2*time.Second
	t.Cleanup(func() { urlPublishPoll, urlPublishMaxWait = poll, wait })

	f := &fakeDownloadAGC{t: t, infos: infos}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f, &Store{client: resty.New().SetBaseURL(f.srv.URL), configAppID: "app-1"}
}

func (f *fakeDownloadAGC) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	call := r.Method + " " + strings.TrimPrefix(r.URL.Path, "/api/publish/v2/")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	switch call {
	case "PUT app-info":
		io.WriteString(w, `{"ret":{"code":0}}`)
	case "POST app-submit-with-file":
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &f.submit)
		fmt.Fprintf(w, `{"ret":{"code":%d,"msg":"queued or refused"}}`, f.submitCode)
	case "GET app-info":
		i := min(f.polls, len(f.infos)-1)
		f.polls++
		io.WriteString(w, f.infos[i])
	default:
		// app-package-file/by-url and app-submit must never be used here:
		// that pair is what sent the previous package to review.
		f.t.Errorf("unexpected request %s", call)
		http.NotFound(w, r)
	}
}

func (f *fakeDownloadAGC) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

const byURLLink = "https://oss.example.com/apks/0cb88316/7f3a/a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4.apk?e=1760000000&token=ak:sig"

func byURLRequest(t *testing.T) *store.UploadRequest {
	t.Helper()
	return &store.UploadRequest{
		FilePath:     filepath.Join(t.TempDir(), "app.apk"), // never read in download mode
		SourceURL:    byURLLink,
		PackageName:  "com.example.app",
		VersionCode:  10631,
		VersionName:  "11.0.9",
		ReleaseNotes: "修复若干问题",
	}
}

// Download mode goes through app-submit-with-file and succeeds only once
// Huawei's version under review carries the NEW versionCode. The first two
// answers reproduce the production bug's trap: the app is already "under
// review" but still with the previous package.
func TestPublishByURLWaitsForTheNewPackage(t *testing.T) {
	old := `{"ret":{"code":0},"appInfo":{"releaseState":5,"versionNumber":"11.0.8","versionCode":10629,"onShelfVersionNumber":"11.0.8","onShelfVersionCode":10629}}`
	done := `{"ret":{"code":0},"appInfo":{"releaseState":5,"versionNumber":"11.0.9","versionCode":10631,"onShelfVersionNumber":"11.0.8","onShelfVersionCode":10629}}`
	f, s := newFakeDownloadAGC(t, old, old, done)

	req := byURLRequest(t)
	at := time.Date(2026, 10, 15, 20, 0, 0, 0, time.FixedZone("CST", 8*3600))
	req.ReleaseTime = &at
	if res := s.Upload(context.Background(), req); !res.Success {
		t.Fatalf("Upload failed: %s", res.Error)
	}

	want := []string{"PUT app-info", "POST app-submit-with-file", "GET app-info", "GET app-info", "GET app-info"}
	if got := f.seen(); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	if f.submit["downloadUrl"] != byURLLink {
		t.Errorf("downloadUrl = %v, want the link unchanged", f.submit["downloadUrl"])
	}
	if f.submit["downloadFileName"] != "com.example.app.apk" {
		t.Errorf("downloadFileName = %v", f.submit["downloadFileName"])
	}
	if id, _ := f.submit["requestId"].(string); id == "" || len(id) > 64 {
		t.Errorf("requestId = %q, want 1–64 characters", id)
	}
	if f.submit["releaseType"] != float64(1) || f.submit["releaseTime"] != "2026-10-15T20:00:00+0800" {
		t.Errorf("releaseType / releaseTime = %v / %v", f.submit["releaseType"], f.submit["releaseTime"])
	}
}

// If Huawei never shows the new versionCode, the upload fails — it must
// not report success for a review that holds the previous package.
func TestPublishByURLFailsWhenTheNewPackageNeverShows(t *testing.T) {
	old := `{"ret":{"code":0},"appInfo":{"releaseState":5,"versionNumber":"11.0.8","versionCode":10629}}`
	_, s := newFakeDownloadAGC(t, old)
	urlPublishMaxWait = 30 * time.Millisecond

	res := s.Upload(context.Background(), byURLRequest(t))
	if res.Success {
		t.Fatal("Upload succeeded although the review still holds versionCode 10629")
	}
	for _, want := range []string{"has not put versionCode 10631 under review", "still reports versionCode 10629"} {
		if !strings.Contains(res.Error, want) {
			t.Errorf("error %q is missing %q", res.Error, want)
		}
	}
}

// A refused request is an error right away; nothing is polled.
func TestPublishByURLRefused(t *testing.T) {
	f, s := newFakeDownloadAGC(t, `{"ret":{"code":0},"appInfo":{}}`)
	f.submitCode = 204144647

	res := s.Upload(context.Background(), byURLRequest(t))
	if res.Success || !strings.Contains(res.Error, "submit with file: [204144647]") {
		t.Fatalf("result = %+v", res)
	}
	if got := f.seen(); !slices.Equal(got, []string{"PUT app-info", "POST app-submit-with-file"}) {
		t.Errorf("calls = %v, want no polling after a refusal", got)
	}
}
