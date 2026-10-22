package oppo

import (
	"bytes"
	"context"
	"encoding/json"
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
	"time"

	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// Listing values /app/info reports before the update.
const (
	oldSummary = "旧的一句话简介"
	oldDesc    = "这是商店里现有的软件介绍内容足够二十个字了吧"
	oldPicURL  = "https://cdn.test/old1.png,https://cdn.test/old2.png"
)

// fakeOppo is an httptest OPPO open platform that records the order of calls
// and the /app/upd form — enough to drive upload() end to end.
type fakeOppo struct {
	srv *httptest.Server

	updErrno   int    // errno /app/upd answers with
	updMessage string // message /app/upd answers with; "" = "应用审核中"
	failPhoto  bool   // photo uploads answer with an errno
	taskState  string // task_state /app/task-state answers with; "" = "2" (done)
	// latest version /app/info reports, its review label, status code and
	// refuse_reason
	infoVersion, infoAudit, infoAuditCode, infoRefuse string
	// laterInfo is how app/info calls after the first (the upload's own
	// query) fail: "errno" answers an errno, "504" keeps timing out;
	// "" = they work.
	laterInfo string
	staleIcon bool // the stored icon is 256×256, which /app/upd rejects
	// gateway504 makes a path answer 504 NoStackTraceTimeoutException this
	// many times before it works, like OPPO's gateway does now and then.
	gateway504 map[string]int
	// staleTokenAt makes this path refuse the store's first token with
	// 800003, as if it had been voided mid-upload; /developer/v1/token
	// then issues "fresh-token" (or, with tokenErrno, an errno).
	staleTokenAt string
	tokenErrno   int

	mu      sync.Mutex
	calls   []string // "info", "upload-url", "upload:<type>:<file>", "icon-check", "upd", "task-state"
	infos   int      // app/info calls so far
	upd     url.Values
	uploads []upload // what /upload received, in order
}

// upload is a file the fake OPPO received.
type upload struct {
	typ, name string
	data      []byte
}

func newFakeOppo(t *testing.T) *fakeOppo {
	t.Helper()
	old, oldWait := taskPollInterval, apiRetryWait
	taskPollInterval, apiRetryWait = time.Millisecond, time.Millisecond
	t.Cleanup(func() { taskPollInterval, apiRetryWait = old, oldWait })

	f := &fakeOppo{gateway504: map[string]int{}}
	var icon, staleIcon bytes.Buffer
	if err := png.Encode(&icon, image.NewRGBA(image.Rect(0, 0, oppoIconSize, oppoIconSize))); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&staleIcon, image.NewRGBA(image.Rect(0, 0, 256, 256))); err != nil {
		t.Fatal(err)
	}

	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		timeout := f.gateway504[r.URL.Path] > 0
		if timeout {
			f.gateway504[r.URL.Path]--
		}
		f.mu.Unlock()
		if timeout {
			f.record("504:" + r.URL.Path)
			w.WriteHeader(http.StatusGatewayTimeout)
			w.Write([]byte("NoStackTraceTimeoutException"))
			return
		}
		if r.URL.Path == f.staleTokenAt && r.FormValue("access_token") == "test-token" {
			f.record("800003:" + r.URL.Path)
			writeJSON(w, map[string]any{"errno": errnoTokenInvalid, "data": map[string]any{"message": "Access Token无效"}})
			return
		}
		switch r.URL.Path {
		case "/developer/v1/token":
			f.record("token")
			if f.tokenErrno != 0 {
				writeJSON(w, map[string]any{"errno": f.tokenErrno, "message": "invalid client"})
				return
			}
			writeJSON(w, map[string]any{"errno": 0, "data": map[string]any{"access_token": "fresh-token"}})
		case "/resource/v1/app/info":
			f.record("info")
			f.mu.Lock()
			f.infos++
			later := f.infos > 1
			f.mu.Unlock()
			switch {
			case later && f.laterInfo == "504":
				w.WriteHeader(http.StatusGatewayTimeout)
				w.Write([]byte("NoStackTraceTimeoutException"))
				return
			case later && f.laterInfo == "errno":
				writeJSON(w, map[string]any{"errno": 500, "message": "系统繁忙", "data": []any{}})
				return
			}
			writeJSON(w, map[string]any{"errno": 0, "data": map[string]any{
				"app_name":    "Test App",
				"summary":     oldSummary,
				"detail_desc": oldDesc,
				"icon_url":    f.srv.URL + "/old/icon.png", // compliant unless staleIcon, so kept
				"pic_url":     oldPicURL,

				"version_name":      "1.1.0",
				"version_code":      f.infoVersion,
				"audit_status":      f.infoAuditCode,
				"audit_status_name": f.infoAudit,
				"refuse_reason":     f.infoRefuse,
			}})
		case "/resource/v1/upload/get-upload-url":
			f.record("upload-url")
			writeJSON(w, map[string]any{"errno": 0, "data": map[string]any{
				"upload_url": f.srv.URL + "/upload",
				"sign":       "one-time-sign",
			}})
		case "/upload":
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				t.Errorf("parse multipart: %v", err)
				return
			}
			file, hdr, err := r.FormFile("file")
			if err != nil {
				t.Errorf("form file: %v", err)
				return
			}
			data, err := io.ReadAll(file)
			if err != nil {
				t.Errorf("read form file: %v", err)
				return
			}
			typ := r.FormValue("type")
			f.record("upload:" + typ + ":" + hdr.Filename)
			f.mu.Lock()
			f.uploads = append(f.uploads, upload{typ: typ, name: hdr.Filename, data: data})
			f.mu.Unlock()
			if typ == "photo" && f.failPhoto {
				writeJSON(w, map[string]any{"errno": 500, "message": "图片上传失败"})
				return
			}
			writeJSON(w, map[string]any{"errno": 0, "data": map[string]any{
				"url": "https://cdn.test/" + hdr.Filename, "md5": "md5", "id": "1",
			}})
		case "/old/icon.png":
			f.record("icon-check")
			w.Header().Set("Content-Type", "image/png")
			if f.staleIcon {
				w.Write(staleIcon.Bytes())
				return
			}
			w.Write(icon.Bytes())
		case "/resource/v1/app/upd":
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse form: %v", err)
			}
			f.mu.Lock()
			f.upd = r.PostForm
			f.mu.Unlock()
			f.record("upd")
			msg := f.updMessage
			if msg == "" {
				msg = "应用审核中"
			}
			writeJSON(w, map[string]any{"errno": f.updErrno, "message": msg})
		case "/resource/v1/app/task-state":
			f.record("task-state")
			state := f.taskState
			if state == "" {
				state = "2"
			}
			writeJSON(w, map[string]any{"errno": 0, "data": map[string]any{"task_state": state}})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeOppo) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

// seen returns the recorded calls and the last /app/upd form.
func (f *fakeOppo) seen() ([]string, url.Values) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls), f.upd
}

// received returns the files uploaded so far, in order.
func (f *fakeOppo) received() []upload {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.uploads)
}

func (f *fakeOppo) store() *Store {
	return &Store{
		client:       newAPIClient(f.srv.URL),
		accessToken:  "test-token",
		clientID:     "test-client",
		clientSecret: "test-secret",
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// writePNG writes a w×h PNG into dir and returns its path.
func writePNG(t *testing.T, dir, name string, w, h int) string {
	t.Helper()
	p := filepath.Join(dir, name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return p
}

func uploadRequest(t *testing.T, l *store.Listing) *store.UploadRequest {
	t.Helper()
	apkPath := filepath.Join(t.TempDir(), "app.apk")
	if err := os.WriteFile(apkPath, []byte("fake apk"), 0o644); err != nil {
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

func TestUpload_Listing(t *testing.T) {
	dir := t.TempDir()
	icon := writePNG(t, dir, "icon.png", 512, 512)
	s1 := writePNG(t, dir, "s1.png", 1080, 1920)
	s2 := writePNG(t, dir, "s2.png", 1080, 1920)
	const newDesc = "全新的软件介绍内容这里至少需要二十个字才行呢"

	tests := []struct {
		name    string
		listing *store.Listing
		// wanted /app/upd listing fields; iconURL "" = the stored icon
		summary, desc, iconURL, picURL string
		wantCalls                      []string
	}{
		{
			name:    "no listing",
			listing: nil,
			summary: oldSummary, desc: oldDesc, picURL: oldPicURL,
			wantCalls: []string{"info", "upload-url", "upload:apk:app.apk", "icon-check", "upd", "task-state"},
		},
		{
			name:    "brief only",
			listing: &store.Listing{Brief: "新版一句话简介"},
			summary: "新版一句话简介", desc: oldDesc, picURL: oldPicURL,
			wantCalls: []string{"info", "upload-url", "upload:apk:app.apk", "icon-check", "upd", "task-state"},
		},
		{
			name: "full listing",
			listing: &store.Listing{
				Brief:       "新版一句话简介",
				Description: newDesc,
				Icon:        icon,
				Screenshots: []string{s1, s2},
			},
			summary: "新版一句话简介", desc: newDesc,
			iconURL: "https://cdn.test/icon.png",
			picURL:  "https://cdn.test/s1.png,https://cdn.test/s2.png",
			// Images go up after the APK and before /app/upd; a listing
			// icon skips the stored-icon compliance check.
			wantCalls: []string{
				"info", "upload-url", "upload:apk:app.apk",
				"upload-url", "upload:photo:icon.png",
				"upload-url", "upload:photo:s1.png",
				"upload-url", "upload:photo:s2.png",
				"upd", "task-state",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeOppo(t)
			if err := f.store().upload(context.Background(), uploadRequest(t, tt.listing)); err != nil {
				t.Fatalf("upload: %v", err)
			}
			calls, upd := f.seen()
			if !slices.Equal(calls, tt.wantCalls) {
				t.Errorf("calls:\n got %v\nwant %v", calls, tt.wantCalls)
			}
			wantIcon := tt.iconURL
			if wantIcon == "" {
				wantIcon = f.srv.URL + "/old/icon.png"
			}
			for field, want := range map[string]string{
				"summary":     tt.summary,
				"detail_desc": tt.desc,
				"icon_url":    wantIcon,
				"pic_url":     tt.picURL,
				"app_name":    "Test App",
				"update_desc": "修复若干问题",
			} {
				if got := upd.Get(field); got != want {
					t.Errorf("upd %s = %q, want %q", field, got, want)
				}
			}
		})
	}
}

func TestUpload_ListingImageFailureSkipsPublish(t *testing.T) {
	dir := t.TempDir()
	f := newFakeOppo(t)
	f.failPhoto = true
	req := uploadRequest(t, &store.Listing{
		Brief:       "新版一句话简介",
		Screenshots: []string{writePNG(t, dir, "s1.png", 1080, 1920), writePNG(t, dir, "s2.png", 1080, 1920)},
	})

	err := f.store().upload(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "listing: upload screenshot 1") {
		t.Fatalf("err = %v, want listing screenshot upload error", err)
	}
	if calls, _ := f.seen(); slices.Contains(calls, "upd") {
		t.Errorf("/app/upd called after a failed listing step: %v", calls)
	}
}

func TestUpload_ListingNotSubmitted(t *testing.T) {
	// 911215 (already in review) and 911216 (an earlier task for this
	// version still running) mean OPPO kept the earlier submission: with
	// a listing both are already-done, so store.ListingResult doesn't
	// claim it. Without one, 911216 still waits and succeeds as before.
	for _, tc := range []struct {
		name     string
		errno    int
		listing  *store.Listing
		wantDone bool
	}{
		{"under review", 911215, &store.Listing{Brief: "新版一句话简介"}, true},
		{"in flight with listing", 911216, &store.Listing{Brief: "新版一句话简介"}, true},
		{"in flight without listing", 911216, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeOppo(t)
			f.updErrno = tc.errno
			err := f.store().upload(context.Background(), uploadRequest(t, tc.listing))
			if got := store.IsAlreadyDone(err); got != tc.wantDone || (!tc.wantDone && err != nil) {
				t.Fatalf("err = %v, want already-done %v", err, tc.wantDone)
			}
		})
	}
}

func TestListingSpec(t *testing.T) {
	dir := t.TempDir()
	valid := &store.Listing{
		Brief:       "一句话简介ABC123",
		Description: strings.Repeat("介", 20),
		Icon:        writePNG(t, dir, "icon.png", 512, 512),
		Screenshots: []string{writePNG(t, dir, "s1.png", 1080, 1920), writePNG(t, dir, "s2.png", 1080, 1920)},
	}
	if errs := store.ValidateListing("oppo", valid); len(errs) != 0 {
		t.Fatalf("valid listing rejected: %v", errs)
	}

	for _, tc := range []struct {
		name string
		l    *store.Listing
	}{
		{"brief too long", &store.Listing{Brief: strings.Repeat("简", 14)}},
		{"brief with space", &store.Listing{Brief: "简介 简介"}},
		{"brief with cjk punctuation", &store.Listing{Brief: "简介，简介"}},
		{"brief with ascii symbol", &store.Listing{Brief: "简介~简介"}},
		{"description too short", &store.Listing{Description: strings.Repeat("介", 19)}},
		{"icon wrong size", &store.Listing{Icon: writePNG(t, dir, "icon256.png", 256, 256)}},
		{"too few screenshots", &store.Listing{Screenshots: valid.Screenshots[:1]}},
		{"screenshot landscape", &store.Listing{Screenshots: []string{valid.Screenshots[0], writePNG(t, dir, "land.png", 1920, 1080)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if errs := store.ValidateListing("oppo", tc.l); len(errs) == 0 {
				t.Errorf("expected a validation error for %+v", tc.l)
			}
		})
	}
}
