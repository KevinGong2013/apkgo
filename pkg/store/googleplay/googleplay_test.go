package googleplay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v4/pkg/internal/offloadtest"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// upload is one media upload the fake received.
type upload struct {
	path        string
	contentType string
	uploadType  string
	auth        string // Authorization header
	body        []byte
}

// fakePlay is an httptest server scripting the Android Publisher edits
// API. The REST API is served at the root and media uploads under
// /upload, matching how the tests wire Store.client and Store.uploadBase.
type fakePlay struct {
	t   *testing.T
	srv *httptest.Server

	mu        sync.Mutex
	calls     []string
	patchBody map[string]any
	uploads   []upload // listing images
	binaries  []upload // APK / AAB uploads
	trackBody map[string]any

	failOn string // "METHOD path" answered with HTTP 400
}

func newFakePlay(t *testing.T) *fakePlay {
	f := &fakePlay{t: t}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakePlay) serve(w http.ResponseWriter, r *http.Request) {
	call := r.Method + " " + r.URL.Path
	body, _ := io.ReadAll(r.Body)

	f.mu.Lock()
	f.calls = append(f.calls, call)
	u := upload{
		path:        r.URL.Path,
		contentType: r.Header.Get("Content-Type"),
		uploadType:  r.URL.Query().Get("uploadType"),
		auth:        r.Header.Get("Authorization"),
		body:        body,
	}
	switch {
	case strings.HasPrefix(r.URL.Path, "/upload/edits/e1/listings/"):
		f.uploads = append(f.uploads, u)
	case r.URL.Path == "/upload/edits/e1/apks" || r.URL.Path == "/upload/edits/e1/bundles":
		f.binaries = append(f.binaries, u)
	case call == "PUT /edits/e1/tracks/production":
		f.trackBody = map[string]any{}
		if err := json.Unmarshal(body, &f.trackBody); err != nil {
			f.t.Errorf("track body %q: %v", body, err)
		}
	}
	if r.Method == http.MethodPatch {
		f.patchBody = map[string]any{}
		if err := json.Unmarshal(body, &f.patchBody); err != nil {
			f.t.Errorf("patch body %q: %v", body, err)
		}
	}
	fail := call == f.failOn
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if fail {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"code":400,"message":"Image is too small.","status":"INVALID_ARGUMENT"}}`)
		return
	}
	switch call {
	case "POST /edits":
		io.WriteString(w, `{"id":"e1"}`)
	case "POST /upload/edits/e1/apks":
		io.WriteString(w, `{"versionCode":42}`)
	case "POST /upload/edits/e1/bundles":
		io.WriteString(w, `{"versionCode":43}`)
	case "GET /edits/e1/details":
		io.WriteString(w, `{"defaultLanguage":"zh-CN","contactEmail":"dev@example.com"}`)
	case "DELETE /edits/e1/listings/zh-CN/phoneScreenshots":
		io.WriteString(w, `{"deleted":[{"id":"old1"}]}`)
	default:
		io.WriteString(w, `{}`)
	}
}

func (f *fakePlay) store() *Store {
	return &Store{
		client:      resty.New().SetBaseURL(f.srv.URL).SetHeader("Content-Type", "application/json").SetAuthToken("tok"),
		token:       "tok",
		uploadBase:  f.srv.URL + "/upload",
		packageName: "com.example.app",
		track:       "production",
	}
}

// snapshot returns the calls, last PATCH body and image uploads so far.
func (f *fakePlay) snapshot() ([]string, map[string]any, []upload) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls), f.patchBody, slices.Clone(f.uploads)
}

// writeImage encodes a w×h image to dir/name as PNG or JPEG (by
// extension) and returns its path and bytes.
func writeImage(t *testing.T, dir, name string, w, h int) (string, []byte) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	var err error
	if strings.HasSuffix(name, ".jpg") {
		err = jpeg.Encode(&buf, img, nil)
	} else {
		err = png.Encode(&buf, img)
	}
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p, buf.Bytes()
}

func writeAPK(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "app.apk")
	if err := os.WriteFile(p, []byte("PK\x03\x04fake-apk"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

var baseCalls = []string{
	"POST /edits",
	"POST /upload/edits/e1/apks",
	"PUT /edits/e1/tracks/production",
	"POST /edits/e1:commit",
}

func TestUploadWithoutListingKeepsSequence(t *testing.T) {
	for name, l := range map[string]*store.Listing{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			f := newFakePlay(t)
			res := f.store().Upload(context.Background(), &store.UploadRequest{FilePath: writeAPK(t), Listing: l})
			if !res.Success {
				t.Fatalf("upload failed: %s", res.Error)
			}
			if got, _, _ := f.snapshot(); !slices.Equal(got, baseCalls) {
				t.Fatalf("calls =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(baseCalls, "\n"))
			}
		})
	}
}

func TestUploadWithListing(t *testing.T) {
	dir := t.TempDir()
	icon, iconBytes := writeImage(t, dir, "icon.png", 512, 512)
	shot1, shot1Bytes := writeImage(t, dir, "1.jpg", 1080, 1920)
	shot2, shot2Bytes := writeImage(t, dir, "2.png", 1080, 1920)

	f := newFakePlay(t)
	res := f.store().Upload(context.Background(), &store.UploadRequest{
		FilePath: writeAPK(t),
		Listing: &store.Listing{
			Brief:       "一句话介绍",
			Description: "长描述",
			Icon:        icon,
			Screenshots: []string{shot1, shot2},
		},
	})
	if !res.Success {
		t.Fatalf("upload failed: %s", res.Error)
	}

	want := []string{
		"POST /edits",
		"POST /upload/edits/e1/apks",
		"PUT /edits/e1/tracks/production",
		"GET /edits/e1/details",
		"PATCH /edits/e1/listings/zh-CN",
		"POST /upload/edits/e1/listings/zh-CN/icon",
		"DELETE /edits/e1/listings/zh-CN/phoneScreenshots",
		"POST /upload/edits/e1/listings/zh-CN/phoneScreenshots",
		"POST /upload/edits/e1/listings/zh-CN/phoneScreenshots",
		"POST /edits/e1:commit",
	}
	got, patchBody, uploads := f.snapshot()
	if !slices.Equal(got, want) {
		t.Fatalf("calls =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	wantPatch := map[string]any{"shortDescription": "一句话介绍", "fullDescription": "长描述"}
	if len(patchBody) != len(wantPatch) || patchBody["shortDescription"] != wantPatch["shortDescription"] ||
		patchBody["fullDescription"] != wantPatch["fullDescription"] {
		t.Errorf("patch body = %v, want %v", patchBody, wantPatch)
	}

	wantUploads := []struct {
		imageType, contentType string
		body                   []byte
	}{
		{"icon", "image/png", iconBytes},
		{"phoneScreenshots", "image/jpeg", shot1Bytes},
		{"phoneScreenshots", "image/png", shot2Bytes},
	}
	if len(uploads) != len(wantUploads) {
		t.Fatalf("got %d image uploads, want %d", len(uploads), len(wantUploads))
	}
	for i, w := range wantUploads {
		u := uploads[i]
		if !strings.HasSuffix(u.path, "/"+w.imageType) || u.contentType != w.contentType || u.uploadType != "media" {
			t.Errorf("upload[%d] = %s (%s, uploadType=%s), want %s (%s, uploadType=media)",
				i, u.path, u.contentType, u.uploadType, w.imageType, w.contentType)
		}
		if !bytes.Equal(u.body, w.body) {
			t.Errorf("upload[%d] body differs from file (%d vs %d bytes)", i, len(u.body), len(w.body))
		}
	}
}

func TestUploadWithPartialListing(t *testing.T) {
	f := newFakePlay(t)
	res := f.store().Upload(context.Background(), &store.UploadRequest{
		FilePath: writeAPK(t),
		Listing:  &store.Listing{Description: "only the long one"},
	})
	if !res.Success {
		t.Fatalf("upload failed: %s", res.Error)
	}
	want := []string{
		"POST /edits",
		"POST /upload/edits/e1/apks",
		"PUT /edits/e1/tracks/production",
		"GET /edits/e1/details",
		"PATCH /edits/e1/listings/zh-CN",
		"POST /edits/e1:commit",
	}
	got, patchBody, _ := f.snapshot()
	if !slices.Equal(got, want) {
		t.Fatalf("calls =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if _, ok := patchBody["shortDescription"]; ok || len(patchBody) != 1 {
		t.Errorf("patch body = %v, want only fullDescription", patchBody)
	}
}

func TestUploadListingFailureSkipsCommit(t *testing.T) {
	dir := t.TempDir()
	shot1, _ := writeImage(t, dir, "1.png", 1080, 1920)
	shot2, _ := writeImage(t, dir, "2.png", 1080, 1920)

	for _, failOn := range []string{
		"GET /edits/e1/details",
		"PATCH /edits/e1/listings/zh-CN",
		"DELETE /edits/e1/listings/zh-CN/phoneScreenshots",
		"POST /upload/edits/e1/listings/zh-CN/phoneScreenshots",
	} {
		t.Run(failOn, func(t *testing.T) {
			f := newFakePlay(t)
			f.failOn = failOn
			res := f.store().Upload(context.Background(), &store.UploadRequest{
				FilePath: writeAPK(t),
				Listing:  &store.Listing{Brief: "hi", Screenshots: []string{shot1, shot2}},
			})
			if res.Success {
				t.Fatal("upload succeeded, want listing failure")
			}
			if !strings.Contains(res.Error, "listing: ") || !strings.Contains(res.Error, "Image is too small.") {
				t.Errorf("error = %q, want listing error with Google's message", res.Error)
			}
			if got, _, _ := f.snapshot(); slices.Contains(got, "POST /edits/e1:commit") {
				t.Errorf("edit committed after listing failure: %v", got)
			}
		})
	}
}

// Non-2xx answers to the release steps are failures, not silent
// successes (resty returns no error for them).
func TestUploadHTTPErrorFails(t *testing.T) {
	for _, failOn := range []string{
		"POST /edits",
		"POST /upload/edits/e1/apks",
		"PUT /edits/e1/tracks/production",
		"POST /edits/e1:commit",
	} {
		t.Run(failOn, func(t *testing.T) {
			f := newFakePlay(t)
			f.failOn = failOn
			res := f.store().Upload(context.Background(), &store.UploadRequest{FilePath: writeAPK(t)})
			if res.Success || !strings.Contains(res.Error, "HTTP 400") {
				t.Fatalf("result = %+v, want HTTP 400 failure", res)
			}
		})
	}
}

// byteCounter is a progress.Reporter keeping the declared total and the
// bytes reported sent.
type byteCounter struct{ total, sent atomic.Int64 }

func (c *byteCounter) Phase(string)  {}
func (c *byteCounter) Total(n int64) { c.total.Store(n) }
func (c *byteCounter) Add(n int64)   { c.sent.Add(n) }

// The package goes byte for byte to the endpoint for its kind, with its
// Content-Type and the bearer token; the versionCode Play answers is the
// one released to the track, and progress counts the bytes — the same
// whether the file is here or only its facts and an Offloader are
// (offloadtest.Remote).
func TestUploadSendsThePackage(t *testing.T) {
	data := bytes.Repeat([]byte("PK\x03\x04play-package "), 3000)
	cases := []struct {
		file, endpoint, contentType string
		versionCode                 float64
	}{
		{"app.apk", "/upload/edits/e1/apks", "application/vnd.android.package-archive", 42},
		{"app.aab", "/upload/edits/e1/bundles", "application/octet-stream", 43},
	}
	for _, c := range cases {
		path := filepath.Join(t.TempDir(), c.file)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		for _, remote := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/remote=%v", c.file, remote), func(t *testing.T) {
				f := newFakePlay(t)
				rep := &byteCounter{}
				req := &store.UploadRequest{FilePath: path, Progress: rep}
				ctx := context.Background()
				var off *offloadtest.Offloader
				if remote {
					req, ctx, off = offloadtest.Remote(t, ctx, req)
				}
				if res := f.store().Upload(ctx, req); !res.Success {
					t.Fatalf("upload failed: %s", res.Error)
				}

				want := []string{"POST /edits", "POST " + c.endpoint, "PUT /edits/e1/tracks/production", "POST /edits/e1:commit"}
				if got, _, _ := f.snapshot(); !slices.Equal(got, want) {
					t.Fatalf("calls =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
				}
				f.mu.Lock()
				binaries, trackBody := slices.Clone(f.binaries), f.trackBody
				f.mu.Unlock()
				if len(binaries) != 1 {
					t.Fatalf("got %d binary uploads, want 1", len(binaries))
				}
				if b := binaries[0]; !bytes.Equal(b.body, data) || b.contentType != c.contentType || b.auth != "Bearer tok" {
					t.Errorf("binary upload: %d bytes, Content-Type %q, Authorization %q; want %d bytes, %q, %q",
						len(b.body), b.contentType, b.auth, len(data), c.contentType, "Bearer tok")
				}
				releases, _ := trackBody["releases"].([]any)
				if len(releases) != 1 || !slices.Equal(releases[0].(map[string]any)["versionCodes"].([]any), []any{c.versionCode}) {
					t.Errorf("track body = %v, want versionCodes [%v]", trackBody, c.versionCode)
				}
				if n := int64(len(data)); rep.total.Load() != n || rep.sent.Load() != n {
					t.Errorf("progress: total %d, sent %d; want %d", rep.total.Load(), rep.sent.Load(), n)
				}
				if remote && !off.Sent(req.FilePath) {
					t.Errorf("the package did not go out as a transfer")
				}
			})
		}
	}
}

func TestListingSpec(t *testing.T) {
	dir := t.TempDir()
	icon, _ := writeImage(t, dir, "icon.png", 512, 512)
	smallIcon, _ := writeImage(t, dir, "small.png", 256, 256)
	portrait, _ := writeImage(t, dir, "portrait.jpg", 1080, 1920) // 16:9, exactly allowed
	tall, _ := writeImage(t, dir, "tall.png", 400, 900)           // long edge > 2× short

	ok := &store.Listing{
		Brief:       strings.Repeat("a", 80),
		Description: strings.Repeat("字", 4000),
		Icon:        icon,
		Screenshots: []string{portrait, portrait},
	}
	if errs := store.ValidateListing("googleplay", ok); len(errs) != 0 {
		t.Fatalf("valid listing rejected: %v", errs)
	}

	cases := map[string]*store.Listing{
		"brief too long":       {Brief: strings.Repeat("a", 81)},
		"description too long": {Description: strings.Repeat("a", 4001)},
		"icon wrong size":      {Icon: smallIcon},
		"too few screenshots":  {Screenshots: []string{portrait}},
		"too many screenshots": {Screenshots: slices.Repeat([]string{portrait}, 9)},
		"aspect ratio":         {Screenshots: []string{portrait, tall}},
	}
	for name, l := range cases {
		t.Run(name, func(t *testing.T) {
			if errs := store.ValidateListing("googleplay", l); len(errs) == 0 {
				t.Error("accepted, want error")
			}
		})
	}

	errs := store.ValidateListing("googleplay", &store.Listing{Screenshots: []string{portrait, tall}})
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "screenshots[1]") {
		t.Errorf("aspect errs = %v, want one error for screenshots[1]", errs)
	}
}
