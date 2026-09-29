package xiaomi

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/KevinGong2013/apkgo/v4/pkg/progress"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// writePNG writes a w×h grayscale PNG into dir and returns its path.
func writePNG(t *testing.T, dir, name string, w, h int) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func md5Of(b []byte) string { return fmt.Sprintf("%x", md5.Sum(b)) }

// pushPart is one multipart file part received by the fake /dev/push.
type pushPart struct {
	Field    string
	FileName string
	Body     []byte
}

// pushCapture records what the fake server received, in wire order.
type pushCapture struct {
	mu     sync.Mutex
	fields map[string]string
	parts  []pushPart
}

func (c *pushCapture) fileFields() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.parts))
	for _, p := range c.parts {
		out = append(out, p.Field)
	}
	return out
}

func (c *pushCapture) part(t *testing.T, field string) pushPart {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range c.parts {
		if p.Field == field {
			return p
		}
	}
	t.Fatalf("no multipart part %q", field)
	return pushPart{}
}

func (c *pushCapture) field(name string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fields[name]
}

// newPushServer fakes /dev/query (answering queryResp) and /dev/push,
// reading the multipart body part by part so the capture keeps wire order.
func newPushServer(t *testing.T, queryResp string) (*httptest.Server, *pushCapture) {
	t.Helper()
	c := &pushCapture{fields: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/dev/query":
			_, _ = w.Write([]byte(queryResp))
		case "/dev/push":
			mr, err := r.MultipartReader()
			if err != nil {
				t.Errorf("multipart reader: %v", err)
				return
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			for {
				p, err := mr.NextPart()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Errorf("next part: %v", err)
					return
				}
				body, err := io.ReadAll(p)
				if err != nil {
					t.Errorf("read part %s: %v", p.FormName(), err)
					return
				}
				if p.FileName() == "" {
					c.fields[p.FormName()] = string(body)
					continue
				}
				c.parts = append(c.parts, pushPart{Field: p.FormName(), FileName: p.FileName(), Body: body})
			}
			_, _ = w.Write([]byte(`{"result":0}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, c
}

// totalReporter records the byte total push announces.
type totalReporter struct{ total int64 }

func (r *totalReporter) Phase(string)  {}
func (r *totalReporter) Total(n int64) { r.total = n }
func (r *totalReporter) Add(int64)     {}

// TestPushWithListing pins how a listing rides along /dev/push: brief/desc
// in RequestData.appInfo, screenshots as screenshot_1..N parts in display
// order, each signed in the SIG list in the same order as the parts and
// with the md5 of exactly the bytes sent.
func TestPushWithListing(t *testing.T) {
	dir := t.TempDir()
	apkPath := filepath.Join(dir, "app.apk")
	if err := os.WriteFile(apkPath, []byte("apk bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	icon := writePNG(t, dir, "icon.png", 512, 512)
	var shots []string
	for i := range 3 {
		// Distinct sizes → distinct bytes, so a shuffled mapping shows up.
		shots = append(shots, writePNG(t, dir, fmt.Sprintf("shot%d.png", i+1), 90, 160+i))
	}

	srv, got := newPushServer(t, "")
	s, key := newTestStore(t, srv.URL)
	req := &store.UploadRequest{
		AppName:      "Demo",
		PackageName:  "com.example.demo",
		ReleaseNotes: "fix bugs",
		FilePath:     apkPath,
		Listing: &store.Listing{
			Brief:       "随手记录每一个灵感",
			Description: "一款简洁的笔记应用。\n支持多端同步。",
			Icon:        icon,
			Screenshots: shots,
		},
	}
	rep := &totalReporter{}
	if err := s.push(context.Background(), 1, req, icon, rep); err != nil {
		t.Fatalf("push: %v", err)
	}

	var reqData struct {
		SynchroType int            `json:"synchroType"`
		AppInfo     map[string]any `json:"appInfo"`
	}
	if err := json.Unmarshal([]byte(got.field("RequestData")), &reqData); err != nil {
		t.Fatalf("decode RequestData: %v", err)
	}
	if reqData.AppInfo["brief"] != req.Listing.Brief {
		t.Errorf("appInfo.brief = %v, want %q", reqData.AppInfo["brief"], req.Listing.Brief)
	}
	if reqData.AppInfo["desc"] != req.Listing.Description {
		t.Errorf("appInfo.desc = %v, want %q", reqData.AppInfo["desc"], req.Listing.Description)
	}
	if reqData.AppInfo["updateDesc"] != "fix bugs" || reqData.SynchroType != 1 {
		t.Errorf("existing fields changed: synchroType=%d appInfo=%v", reqData.SynchroType, reqData.AppInfo)
	}

	wantParts := []string{"apk", "icon", "screenshot_1", "screenshot_2", "screenshot_3"}
	if parts := got.fileFields(); !slices.Equal(parts, wantParts) {
		t.Fatalf("multipart file parts = %v, want %v", parts, wantParts)
	}
	for i, p := range shots {
		part := got.part(t, fmt.Sprintf("screenshot_%d", i+1))
		want, err := fileMD5(p)
		if err != nil {
			t.Fatal(err)
		}
		if md5Of(part.Body) != want || part.FileName != filepath.Base(p) {
			t.Errorf("%s = %s (md5 %s), want %s (md5 %s)", part.Field, part.FileName, md5Of(part.Body), filepath.Base(p), want)
		}
	}

	// SIG lists RequestData then every file part, in the parts' order,
	// each hash matching the bytes actually uploaded under that name.
	entries := decryptSIGEntries(t, key, got.field("SIG"))
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
	}
	if want := append([]string{"RequestData"}, wantParts...); !slices.Equal(names, want) {
		t.Fatalf("SIG sig list = %v, want %v", names, want)
	}
	if entries[0].Hash != md5Of([]byte(got.field("RequestData"))) {
		t.Error("RequestData sig hash doesn't match the RequestData sent")
	}
	for _, e := range entries[1:] {
		if h := md5Of(got.part(t, e.Name).Body); e.Hash != h {
			t.Errorf("sig %s hash = %s, uploaded part md5 = %s", e.Name, e.Hash, h)
		}
	}

	// Progress total covers the screenshots too.
	var wantTotal int64
	for _, p := range append([]string{apkPath, icon}, shots...) {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		wantTotal += st.Size()
	}
	if rep.total != wantTotal {
		t.Errorf("progress total = %d, want %d", rep.total, wantTotal)
	}
}

// TestPushListingSendsOnlyNonEmptyFields pins that a nil listing leaves the
// request byte-for-byte as before (RequestData, SIG list, parts), and that
// empty listing fields are omitted rather than sent blank.
func TestPushListingSendsOnlyNonEmptyFields(t *testing.T) {
	const base = `"appName":"Demo","packageName":"com.example.demo","updateDesc":"fix bugs"`
	cases := []struct {
		name            string
		listing         *store.Listing
		wantRequestData string
	}{
		{
			name:            "nil listing",
			listing:         nil,
			wantRequestData: `{"appInfo":{` + base + `},"synchroType":1,"userName":"dev@example.com"}`,
		},
		{
			// The icon travels as the icon part (chosen in upload), not in RequestData.
			name:            "icon only",
			listing:         &store.Listing{Icon: "unused-here.png"},
			wantRequestData: `{"appInfo":{` + base + `},"synchroType":1,"userName":"dev@example.com"}`,
		},
		{
			name:            "description only",
			listing:         &store.Listing{Description: "长描述"},
			wantRequestData: `{"appInfo":{"appName":"Demo","desc":"长描述","packageName":"com.example.demo","updateDesc":"fix bugs"},"synchroType":1,"userName":"dev@example.com"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			apkPath := filepath.Join(dir, "app.apk")
			if err := os.WriteFile(apkPath, []byte("apk bytes"), 0o644); err != nil {
				t.Fatal(err)
			}
			icon := writePNG(t, dir, "icon.png", 48, 48)

			srv, got := newPushServer(t, "")
			s, key := newTestStore(t, srv.URL)
			req := &store.UploadRequest{
				AppName:      "Demo",
				PackageName:  "com.example.demo",
				ReleaseNotes: "fix bugs",
				FilePath:     apkPath,
				Listing:      tc.listing,
			}
			if err := s.push(context.Background(), 1, req, icon, progress.Safe(nil)); err != nil {
				t.Fatalf("push: %v", err)
			}

			if rd := got.field("RequestData"); rd != tc.wantRequestData {
				t.Errorf("RequestData =\n  %s\nwant\n  %s", rd, tc.wantRequestData)
			}
			if parts := got.fileFields(); !slices.Equal(parts, []string{"apk", "icon"}) {
				t.Errorf("multipart file parts = %v, want [apk icon]", parts)
			}
			if names := decryptSIG(t, key, got.field("SIG")); !slices.Equal(names, []string{"RequestData", "apk", "icon"}) {
				t.Errorf("SIG sig list = %v, want [RequestData apk icon]", names)
			}
		})
	}
}

// TestUploadListingIcon pins the icon source: without a listing icon the
// densest launcher icon is extracted from the APK into a temp file that's
// removed afterwards; with one, the user's file is sent as is and left in
// place (no extraction, no deletion).
func TestUploadListingIcon(t *testing.T) {
	const queryResp = `{"result":0,"packageInfo":{"appName":"HelloWorld","packageName":"com.example.helloworld","versionCode":1,"versionName":"1.0"}}`

	t.Run("extracted from apk", func(t *testing.T) {
		dir := t.TempDir()
		apkPath := copyFixtureAPK(t, dir)
		srv, got := newPushServer(t, queryResp)
		s, _ := newTestStore(t, srv.URL)
		s.client.SetBaseURL(srv.URL)

		req := &store.UploadRequest{PackageName: "com.example.helloworld", VersionCode: 2, FilePath: apkPath}
		if err := s.upload(context.Background(), req); err != nil {
			t.Fatalf("upload: %v", err)
		}
		part := got.part(t, "icon")
		if part.FileName != "apkgo_icon_tmp.png" {
			t.Errorf("icon part filename = %q, want the extracted apkgo_icon_tmp.png", part.FileName)
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(part.Body))
		if err != nil || cfg.Width != 192 {
			t.Errorf("icon part = %dx%d (err %v), want the 192px launcher icon", cfg.Width, cfg.Height, err)
		}
		if _, err := os.Stat(filepath.Join(dir, "apkgo_icon_tmp.png")); !os.IsNotExist(err) {
			t.Errorf("extracted temp icon not removed: stat err = %v", err)
		}
	})

	t.Run("listing icon", func(t *testing.T) {
		dir := t.TempDir()
		apkPath := copyFixtureAPK(t, dir)
		userIcon := writePNG(t, dir, "store-icon.png", 512, 512)
		wantMD5, err := fileMD5(userIcon)
		if err != nil {
			t.Fatal(err)
		}
		srv, got := newPushServer(t, queryResp)
		s, _ := newTestStore(t, srv.URL)
		s.client.SetBaseURL(srv.URL)

		req := &store.UploadRequest{
			PackageName: "com.example.helloworld",
			VersionCode: 2,
			FilePath:    apkPath,
			Listing:     &store.Listing{Icon: userIcon},
		}
		if err := s.upload(context.Background(), req); err != nil {
			t.Fatalf("upload: %v", err)
		}
		part := got.part(t, "icon")
		if part.FileName != "store-icon.png" || md5Of(part.Body) != wantMD5 {
			t.Errorf("icon part = %s (md5 %s), want the user's store-icon.png (md5 %s)", part.FileName, md5Of(part.Body), wantMD5)
		}
		if h, err := fileMD5(userIcon); err != nil || h != wantMD5 {
			t.Errorf("user icon was removed or changed: md5 %s, err %v", h, err)
		}
		if _, err := os.Stat(filepath.Join(dir, "apkgo_icon_tmp.png")); !os.IsNotExist(err) {
			t.Errorf("icon extracted from APK despite a listing icon: stat err = %v", err)
		}
	})
}

// TestListingSpec checks the declared spec through store.ValidateListing,
// including the xiaomi-only rules in checkListing.
func TestListingSpec(t *testing.T) {
	dir := t.TempDir()
	icon := writePNG(t, dir, "icon.png", 512, 512)
	smallIcon := writePNG(t, dir, "icon216.png", 216, 216)
	portrait := writePNG(t, dir, "portrait.png", 1080, 1920)
	landscape := writePNG(t, dir, "landscape.png", 1920, 1080)
	rep := func(p string, n int) []string { return slices.Repeat([]string{p}, n) }

	const brief17 = "小米应用商店一句话简介最多十七个字" // 17 汉字 = 34 width

	cases := []struct {
		name    string
		listing store.Listing
		wantErr string // "" = valid
	}{
		{"valid portrait", store.Listing{Brief: brief17, Description: "描述。", Icon: icon, Screenshots: rep(portrait, 3)}, ""},
		{"valid landscape", store.Listing{Screenshots: rep(landscape, 5)}, ""},
		{"valid mixed-width brief", store.Listing{Brief: "Fast notes 快速笔记"}, ""},
		{"brief too long", store.Listing{Brief: brief17 + "了"}, "at most 34"},
		{"brief trailing 。", store.Listing{Brief: "随手记录每一个灵感。"}, "句末勿加标点"},
		{"brief trailing ! and space", store.Listing{Brief: "Take notes! "}, "句末勿加标点"},
		{"icon wrong size", store.Listing{Icon: smallIcon}, "size 216x216"},
		{"too few screenshots", store.Listing{Screenshots: rep(portrait, 2)}, "at least 3"},
		{"too many screenshots", store.Listing{Screenshots: rep(portrait, 6)}, "at most 5"},
		{"mixed orientation", store.Listing{Screenshots: []string{portrait, portrait, landscape}}, "截图方向"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := store.ValidateListing("xiaomi", &tc.listing)
			if tc.wantErr == "" {
				if len(errs) > 0 {
					t.Errorf("unexpected errors: %v", errs)
				}
				return
			}
			if len(errs) == 0 || !strings.Contains(errors.Join(errs...).Error(), tc.wantErr) {
				t.Errorf("errors = %v, want one containing %q", errs, tc.wantErr)
			}
		})
	}
}
