package vivo

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// vivoCall is one request the fake router received.
type vivoCall struct {
	method string
	query  url.Values // query string and form body merged
	inURL  url.Values // query string only
	file   string     // multipart "file" part's filename, for upload methods
}

// fakeVivo mimics vivo's /router/rest: upload methods answer with a
// serialnumber, everything else with a bare success envelope. failMethod,
// when set, gets a business-layer rejection instead.
type fakeVivo struct {
	t          *testing.T
	failMethod string

	mu    sync.Mutex
	calls []vivoCall
	shots int
}

func (f *fakeVivo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Parameters arrive in the query string, or in a form body when the
	// update carries listing text; r.Form merges both.
	if err := r.ParseForm(); err != nil {
		f.t.Errorf("parse form: %v", err)
	}
	q := r.Form
	c := vivoCall{method: q.Get("method"), query: q, inURL: r.URL.Query()}
	if strings.HasPrefix(c.method, "app.upload.") {
		_, hdr, err := r.FormFile("file")
		if err != nil {
			f.t.Errorf("%s: no multipart file part: %v", c.method, err)
		} else {
			c.file = hdr.Filename
		}
	}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()

	// vivo serves JSON as text/plain.
	w.Header().Set("Content-Type", "text/plain;charset=utf-8")
	if c.method == f.failMethod {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "subCode": "12010", "msg": "当前更新应用正在审核，不允许更新"})
		return
	}
	var data any
	switch c.method {
	case "app.upload.icon":
		data = map[string]string{"serialnumber": "icon-sn"}
	case "app.upload.screenshot":
		f.mu.Lock()
		f.shots++
		data = map[string]string{"serialnumber": fmt.Sprintf("shot-sn-%d", f.shots)}
		f.mu.Unlock()
	case "app.upload.apk.app", "app.upload.apk.app.32", "app.upload.apk.app.64":
		data = map[string]string{"serialnumber": c.method + "-sn", "fileMd5": "apk-md5"}
	case "app.query.task.status":
		data = map[string]int{"status": 3}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "subCode": "0", "msg": "成功", "data": data})
}

func (f *fakeVivo) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.method
	}
	return out
}

// callsTo returns the calls made to method, in order.
func (f *fakeVivo) callsTo(method string) []vivoCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []vivoCall
	for _, c := range f.calls {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

func newFakeVivo(t *testing.T) (*fakeVivo, *Store) {
	t.Helper()
	f := &fakeVivo{t: t}
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	return f, &Store{
		client:       resty.New().SetBaseURL(server.URL),
		baseURL:      server.URL,
		accessKey:    "key",
		accessSecret: []byte("secret"),
	}
}

// writePNG writes a blank w×h PNG into dir.
func writePNG(t *testing.T, dir, name string, w, h int) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, image.NewNRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeAPK(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("apk:"+name), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const testDescription = "这是一段用于测试的应用详细介绍，长度需要满足 vivo 对应用简介五十到一千个字符的要求，所以多写一点内容凑够字数。"

// testListing writes a full listing (icon + 3 screenshots) that passes
// vivo's ListingSpec.
func testListing(t *testing.T) *store.Listing {
	t.Helper()
	dir := t.TempDir()
	return &store.Listing{
		Brief:       "好用的测试工具",
		Description: testDescription,
		Icon:        writePNG(t, dir, "icon.png", 512, 512),
		Screenshots: []string{
			writePNG(t, dir, "s1.png", 1080, 1920),
			writePNG(t, dir, "s2.png", 1080, 1920),
			writePNG(t, dir, "s3.png", 1080, 1920),
		},
	}
}

// The listing's images are uploaded before the APK and the version
// update, and their serial numbers ride in the same update request as
// simpleDesc / detailDesc. The APKs come with public URLs, but images
// force the file-upload interfaces: vivo's URL mode only takes image URLs.
func TestUploadWithListingUploadsImagesBeforeUpdate(t *testing.T) {
	images := []string{"app.upload.icon", "app.upload.screenshot", "app.upload.screenshot", "app.upload.screenshot"}
	tests := []struct {
		name        string
		split       bool
		wantMethods []string
		updateVia   string
	}{
		{
			name:        "single",
			wantMethods: append(slices.Clone(images), "app.upload.apk.app", "app.sync.update.app"),
			updateVia:   "app.sync.update.app",
		},
		{
			name:        "split",
			split:       true,
			wantMethods: append(slices.Clone(images), "app.upload.apk.app.32", "app.upload.apk.app.64", "app.sync.update.subpackage.app"),
			updateVia:   "app.sync.update.subpackage.app",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, s := newFakeVivo(t)
			dir := t.TempDir()
			l := testListing(t)
			req := &store.UploadRequest{
				FilePath:     writeAPK(t, dir, "app.apk"),
				SourceURL:    "https://oss.example.com/app.apk",
				PackageName:  "com.example",
				VersionCode:  7,
				ReleaseNotes: "修复若干问题",
				Listing:      l,
			}
			if tt.split {
				req.File64Path = writeAPK(t, dir, "app64.apk")
				req.Source64URL = "https://oss.example.com/app64.apk"
			}

			if res := s.Upload(context.Background(), req); !res.Success {
				t.Fatalf("Upload failed: %s", res.Error)
			}
			if got := f.methods(); !slices.Equal(got, tt.wantMethods) {
				t.Fatalf("methods = %v, want %v", got, tt.wantMethods)
			}

			// Images: packageName + file only (no fileMd5), screenshots in
			// display order.
			wantFiles := []string{filepath.Base(l.Icon)}
			for _, p := range l.Screenshots {
				wantFiles = append(wantFiles, filepath.Base(p))
			}
			var gotFiles []string
			for _, c := range append(f.callsTo("app.upload.icon"), f.callsTo("app.upload.screenshot")...) {
				gotFiles = append(gotFiles, c.file)
				if c.query.Get("packageName") != "com.example" {
					t.Errorf("%s packageName = %q", c.method, c.query.Get("packageName"))
				}
				if c.query.Has("fileMd5") {
					t.Errorf("%s sent fileMd5; the image upload methods don't take it", c.method)
				}
			}
			if !slices.Equal(gotFiles, wantFiles) {
				t.Errorf("uploaded images = %v, want %v", gotFiles, wantFiles)
			}

			update := f.callsTo(tt.updateVia)[0].query
			for k, want := range map[string]string{
				"simpleDesc":  l.Brief,
				"detailDesc":  l.Description,
				"icon":        "icon-sn",
				"screenshot":  "shot-sn-1,shot-sn-2,shot-sn-3",
				"updateDesc":  "修复若干问题",
				"versionCode": "7",
			} {
				if got := update.Get(k); got != want {
					t.Errorf("%s %s = %q, want %q", tt.updateVia, k, got, want)
				}
			}
		})
	}
}

// Without a listing (nil or empty) the update request carries exactly
// the parameters it did before listing support.
func TestUploadWithoutListingUnchanged(t *testing.T) {
	for name, l := range map[string]*store.Listing{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			f, s := newFakeVivo(t)
			req := &store.UploadRequest{
				FilePath:     writeAPK(t, t.TempDir(), "app.apk"),
				PackageName:  "com.example",
				VersionCode:  7,
				ReleaseNotes: "修复若干问题",
				Listing:      l,
			}
			if res := s.Upload(context.Background(), req); !res.Success {
				t.Fatalf("Upload failed: %s", res.Error)
			}
			want := []string{"app.upload.apk.app", "app.sync.update.app"}
			if got := f.methods(); !slices.Equal(got, want) {
				t.Fatalf("methods = %v, want %v", got, want)
			}

			update := f.callsTo("app.sync.update.app")[0].query
			var keys []string
			for k := range update {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			wantKeys := []string{
				"access_key", "apk", "compatibleDevice", "fileMd5", "format", "method",
				"onlineType", "packageName", "sign", "sign_method", "target_app_key",
				"timestamp", "updateDesc", "v", "versionCode",
			}
			if !slices.Equal(keys, wantKeys) {
				t.Errorf("update params = %v, want %v", keys, wantKeys)
			}
		})
	}
}

// Text-only listings keep URL-push: simpleDesc / detailDesc go into
// app.update.app alongside the APK URL.
func TestUploadListingTextOnlyKeepsURLPush(t *testing.T) {
	defer func(d time.Duration) { vivoTaskPollPeriod = d }(vivoTaskPollPeriod)
	vivoTaskPollPeriod = time.Millisecond

	f, s := newFakeVivo(t)
	req := &store.UploadRequest{
		FilePath:     writeAPK(t, t.TempDir(), "app.apk"),
		SourceURL:    "https://oss.example.com/app.apk",
		PackageName:  "com.example",
		VersionCode:  7,
		ReleaseNotes: "修复若干问题",
		Listing:      &store.Listing{Brief: "好用的测试工具", Description: testDescription},
	}
	if res := s.Upload(context.Background(), req); !res.Success {
		t.Fatalf("Upload failed: %s", res.Error)
	}
	want := []string{"app.update.app", "app.query.task.status"}
	if got := f.methods(); !slices.Equal(got, want) {
		t.Fatalf("methods = %v, want %v", got, want)
	}
	update := f.callsTo("app.update.app")[0].query
	if update.Get("simpleDesc") != "好用的测试工具" || update.Get("detailDesc") != testDescription {
		t.Errorf("simpleDesc/detailDesc = %q / %q", update.Get("simpleDesc"), update.Get("detailDesc"))
	}
	// Listing text is long enough to overflow a request line; it must go
	// in the form body, not the query string.
	if inURL := f.callsTo("app.update.app")[0].inURL; inURL.Has("detailDesc") || inURL.Has("sign") {
		t.Errorf("listing update put params in the URL: %v", inURL)
	}
	if update.Get("apkUrl") != req.SourceURL {
		t.Errorf("apkUrl = %q", update.Get("apkUrl"))
	}
	for _, k := range []string{"icon", "screenshot", "iconUrl", "screenshotUrl"} {
		if update.Has(k) {
			t.Errorf("text-only listing sent %s", k)
		}
	}
}

// A failed listing image upload fails the store before any APK upload
// or version update.
func TestUploadListingImageFailureSkipsVersion(t *testing.T) {
	f, s := newFakeVivo(t)
	f.failMethod = "app.upload.screenshot"
	req := &store.UploadRequest{
		FilePath:    writeAPK(t, t.TempDir(), "app.apk"),
		PackageName: "com.example",
		VersionCode: 7,
		Listing:     testListing(t),
	}
	res := s.Upload(context.Background(), req)
	if res.Success {
		t.Fatal("Upload succeeded despite the screenshot upload failing")
	}
	if !strings.Contains(res.Error, "upload listing screenshot 1") || !strings.Contains(res.Error, "12010") {
		t.Errorf("error = %q", res.Error)
	}
	want := []string{"app.upload.icon", "app.upload.screenshot"}
	if got := f.methods(); !slices.Equal(got, want) {
		t.Errorf("methods = %v, want %v", got, want)
	}
}

func TestListingSpec(t *testing.T) {
	valid := testListing(t)
	if errs := store.ValidateListing("vivo", valid); len(errs) > 0 {
		t.Fatalf("valid listing rejected: %v", errs)
	}

	dir := t.TempDir()
	tests := []struct {
		name    string
		listing store.Listing
		wantErr string
	}{
		{"brief 4 hanzi", store.Listing{Brief: "测试工具"}, "listing brief"},
		{"brief 17 hanzi", store.Listing{Brief: "一二三四五六七八九十一二三四五六七"}, "listing brief"},
		{"brief 9 latin", store.Listing{Brief: "abcdefghi"}, "listing brief"},
		{"brief trailing full stop", store.Listing{Brief: "好用的测试工具。"}, "must not end with"},
		{"description too short", store.Listing{Description: "太短了"}, "listing description"},
		{"icon too small", store.Listing{Icon: writePNG(t, dir, "small.png", 128, 128)}, "listing icon"},
		{"icon not square", store.Listing{Icon: writePNG(t, dir, "wide.png", 512, 256)}, "listing icon"},
		{"screenshot landscape", store.Listing{Screenshots: []string{
			writePNG(t, dir, "l1.png", 1920, 1080), valid.Screenshots[1], valid.Screenshots[2],
		}}, "listing screenshots[0]"},
		{"two screenshots", store.Listing{Screenshots: valid.Screenshots[:2]}, "need at least 3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := store.ValidateListing("vivo", &tt.listing)
			if len(errs) == 0 {
				t.Fatalf("accepted, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(fmt.Sprint(errs), tt.wantErr) {
				t.Errorf("errors = %v, want one containing %q", errs, tt.wantErr)
			}
		})
	}

	for _, brief := range []string{"好用的测试工具！", "Handy test tool?", "省电高达30%"} {
		if errs := store.ValidateListing("vivo", &store.Listing{Brief: brief}); len(errs) > 0 {
			t.Errorf("brief %q rejected: %v", brief, errs)
		}
	}
}
