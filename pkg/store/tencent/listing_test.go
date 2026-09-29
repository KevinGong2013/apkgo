package tencent

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// fakeTencent stands in for both the open API and COS. It hands out
// serials "<file_type>-<n>" whose pre-signed URL points back at itself,
// and records every call in order.
type fakeTencent struct {
	srv     *httptest.Server
	failImg bool // get_file_upload_info with file_type=img returns ret != 0

	mu     sync.Mutex
	n      int
	events []string          // "info:<file_type>:<file_name>", "put:<serial>", "update_app"
	puts   map[string][]byte // serial → PUT body
	update url.Values        // update_app form; nil if never called
}

func newFakeTencent(t *testing.T) *fakeTencent {
	f := &fakeTencent{puts: map[string][]byte{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeTencent) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/cos/"):
		serial := strings.TrimPrefix(r.URL.Path, "/cos/")
		body, _ := io.ReadAll(r.Body)
		f.puts[serial] = body
		f.events = append(f.events, "put:"+serial)
	case r.URL.Path == "/get_file_upload_info":
		_ = r.ParseForm()
		fileType := r.PostForm.Get("file_type")
		f.events = append(f.events, "info:"+fileType+":"+r.PostForm.Get("file_name"))
		if f.failImg && fileType == "img" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ret": 4000100, "msg": "今日上传次数已达上限"})
			return
		}
		f.n++
		serial := fmt.Sprintf("%s-%d", fileType, f.n)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ret":           0,
			"pre_sign_url":  f.srv.URL + "/cos/" + serial,
			"serial_number": serial,
		})
	case r.URL.Path == "/update_app":
		_ = r.ParseForm()
		f.update = r.PostForm
		f.events = append(f.events, "update_app")
		_ = json.NewEncoder(w).Encode(map[string]any{"ret": 0})
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeTencent) store(t *testing.T) *Store {
	t.Helper()
	s, err := New(map[string]string{"user_id": "u1", "access_secret": "secret", "app_id": "123"})
	if err != nil {
		t.Fatal(err)
	}
	s.client.SetBaseURL(f.srv.URL)
	return s
}

// uploadRequest returns a request for a fake (non-zip) APK, which takes
// the default apk32_flag=1 branch.
func uploadRequest(t *testing.T, l *store.Listing) *store.UploadRequest {
	t.Helper()
	apkPath := filepath.Join(t.TempDir(), "app.apk")
	if err := os.WriteFile(apkPath, []byte("not a real apk"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &store.UploadRequest{
		FilePath:     apkPath,
		PackageName:  "com.example.app",
		VersionCode:  2,
		VersionName:  "1.1.0",
		ReleaseNotes: "修复若干问题",
		Listing:      l,
	}
}

func writePNG(t *testing.T, name string, w, h int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return path
}

func screenshots(t *testing.T, n, w, h int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = writePNG(t, fmt.Sprintf("shot%d.png", i+1), w, h)
	}
	return out
}

func TestUploadWithListing(t *testing.T) {
	f := newFakeTencent(t)
	l := &store.Listing{
		Brief:       "一句话简介测试",
		Description: strings.Repeat("这是一段应用的长描述。", 7),
		Icon:        writePNG(t, "icon.png", 512, 512),
		Screenshots: screenshots(t, 4, 108, 192),
	}

	res := f.store(t).Upload(context.Background(), uploadRequest(t, l))
	if !res.Success {
		t.Fatalf("upload failed: %s", res.Error)
	}

	// Every image is uploaded (serial + PUT) before update_app.
	want := []string{
		"info:apk:app.apk", "put:apk-1",
		"info:img:icon.png", "put:img-2",
		"info:img:shot1.png", "put:img-3",
		"info:img:shot2.png", "put:img-4",
		"info:img:shot3.png", "put:img-5",
		"info:img:shot4.png", "put:img-6",
		"update_app",
	}
	if !slices.Equal(f.events, want) {
		t.Fatalf("events:\n got %v\nwant %v", f.events, want)
	}
	icon, _ := os.ReadFile(l.Icon)
	if !slices.Equal(f.puts["img-2"], icon) {
		t.Errorf("icon PUT body differs from the icon file")
	}

	for k, v := range map[string]string{
		"one_word_summary":             l.Brief,
		"introduce":                    l.Description,
		"icon_file_serial_number":      "img-2",
		"snapshots_file_serial_number": "img-3|img-4|img-5|img-6",
		"apk32_file_serial_number":     "apk-1",
		"feature":                      "修复若干问题",
	} {
		if got := f.update.Get(k); got != v {
			t.Errorf("update_app %s = %q, want %q", k, got, v)
		}
	}
}

// Only the fields that are set go to update_app (不变更则不填); no image
// is uploaded when neither icon nor screenshots are given.
func TestUploadWithListingTextOnly(t *testing.T) {
	f := newFakeTencent(t)
	res := f.store(t).Upload(context.Background(), uploadRequest(t, &store.Listing{Brief: "一句话简介测试"}))
	if !res.Success {
		t.Fatalf("upload failed: %s", res.Error)
	}
	if want := []string{"info:apk:app.apk", "put:apk-1", "update_app"}; !slices.Equal(f.events, want) {
		t.Fatalf("events: got %v, want %v", f.events, want)
	}
	if got := f.update.Get("one_word_summary"); got != "一句话简介测试" {
		t.Errorf("one_word_summary = %q", got)
	}
	for _, k := range []string{"introduce", "icon_file_serial_number", "snapshots_file_serial_number"} {
		if f.update.Has(k) {
			t.Errorf("update_app has %s = %q, want it omitted", k, f.update.Get(k))
		}
	}
}

// Without a listing, update_app carries exactly the pre-listing fields.
func TestUploadWithoutListing(t *testing.T) {
	for name, l := range map[string]*store.Listing{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			f := newFakeTencent(t)
			res := f.store(t).Upload(context.Background(), uploadRequest(t, l))
			if !res.Success {
				t.Fatalf("upload failed: %s", res.Error)
			}
			if want := []string{"info:apk:app.apk", "put:apk-1", "update_app"}; !slices.Equal(f.events, want) {
				t.Fatalf("events: got %v, want %v", f.events, want)
			}
			var keys []string
			for k := range f.update {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			want := []string{
				"apk32_file_md5", "apk32_file_serial_number", "apk32_flag", "app_id",
				"deploy_type", "feature", "pkg_name", "sign", "timestamp", "user_id",
			}
			if !slices.Equal(keys, want) {
				t.Errorf("update_app keys:\n got %v\nwant %v", keys, want)
			}
		})
	}
}

// A failed listing step fails the store without submitting the version.
func TestUploadListingImageFailure(t *testing.T) {
	f := newFakeTencent(t)
	f.failImg = true
	l := &store.Listing{Brief: "一句话简介测试", Icon: writePNG(t, "icon.png", 512, 512)}

	res := f.store(t).Upload(context.Background(), uploadRequest(t, l))
	if res.Success {
		t.Fatal("upload succeeded, want listing failure")
	}
	if !strings.Contains(res.Error, "listing: upload icon") || !strings.Contains(res.Error, "4000100") {
		t.Errorf("error = %q, want the icon upload failure", res.Error)
	}
	if f.update != nil {
		t.Errorf("update_app was called: %v", f.update)
	}
}

func TestListingSpec(t *testing.T) {
	valid := func() *store.Listing {
		return &store.Listing{
			Brief:       "一句话简介",
			Description: strings.Repeat("描", 60),
			Icon:        writePNG(t, "icon.png", 512, 512),
			Screenshots: screenshots(t, 4, 108, 192),
		}
	}
	if errs := store.ValidateListing("tencent", valid()); len(errs) != 0 {
		t.Fatalf("valid listing rejected: %v", errs)
	}

	cases := map[string]struct {
		mutate func(l *store.Listing)
		want   string
	}{
		"brief too short":       {func(l *store.Listing) { l.Brief = "四个字的" }, "listing brief"},
		"brief too long":        {func(l *store.Listing) { l.Brief = strings.Repeat("字", 16) }, "listing brief"},
		"description too short": {func(l *store.Listing) { l.Description = strings.Repeat("描", 59) }, "listing description"},
		"icon not 512":          {func(l *store.Listing) { l.Icon = writePNG(t, "icon.png", 256, 256) }, "listing icon"},
		"too few screenshots":   {func(l *store.Listing) { l.Screenshots = l.Screenshots[:3] }, "need at least 4"},
		"too many screenshots": {func(l *store.Listing) {
			l.Screenshots = screenshots(t, 6, 108, 192)
		}, "at most 5"},
		"mixed screenshot sizes": {func(l *store.Listing) {
			l.Screenshots[2] = writePNG(t, "wide.png", 192, 108)
		}, "screenshots[2] is 192x108 but screenshots[0] is 108x192"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			l := valid()
			c.mutate(l)
			errs := store.ValidateListing("tencent", l)
			if len(errs) == 0 {
				t.Fatalf("want an error containing %q, got none", c.want)
			}
			var msgs []string
			for _, err := range errs {
				msgs = append(msgs, err.Error())
			}
			if joined := strings.Join(msgs, "\n"); !strings.Contains(joined, c.want) {
				t.Errorf("errors %q do not mention %q", joined, c.want)
			}
		})
	}
}
