package samsung

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v4/pkg/internal/offloadtest"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// fakeSamsung is an httptest stand-in for the Content Publish API that
// records every call in order plus the contentUpdate body.
type fakeSamsung struct {
	srv *httptest.Server

	mu        sync.Mutex
	calls     []string          // "path" or "fileUpload:<filename>"
	sessions  []string          // sessionId of every fileUpload
	files     map[string][]byte // fileUpload bytes by filename
	fileAuth  []string          // service-account-id and Authorization of every fileUpload
	update    map[string]any    // decoded contentUpdate body
	failFile  string            // fileUpload of this filename answers 500
	submitted bool

	info   string   // contentInfo body; "" = a live app with versionCode 5
	binary []string // scripted Add Binary answers, in call order (see answer)
	submit []string // scripted contentSubmit answers, in call order
}

// answer writes the scripted reply: "drop" closes the connection without an
// answer, "inuse" is Add Binary's 400 for an already attached versionCode,
// any other non-empty string is a 200 body, and nothing left = ok. It
// returns the reply it used.
func answer(t *testing.T, w http.ResponseWriter, script *[]string, ok string) string {
	reply := ok
	if len(*script) > 0 {
		reply, *script = (*script)[0], (*script)[1:]
	}
	switch reply {
	case "drop":
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return reply
		}
		conn.Close()
	case "inuse":
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"body":{"resultCode":"5021","resultMessage":"This binary is already in use."},"from":"seller","message":"Request failed with status code 400"}`)
	default:
		io.WriteString(w, reply)
	}
	return reply
}

func newFakeSamsung(t *testing.T) *fakeSamsung {
	t.Helper()
	f := &fakeSamsung{files: map[string][]byte{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/seller/createUploadSessionId":
			f.calls = append(f.calls, r.URL.Path)
			fmt.Fprintf(w, `{"url":%q,"sessionId":"sess-1"}`, f.srv.URL+"/galaxyapi/fileUpload")
		case "/galaxyapi/fileUpload":
			file, hdr, err := r.FormFile("file")
			if err != nil {
				t.Errorf("fileUpload: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			data, _ := io.ReadAll(file)
			file.Close()
			f.calls = append(f.calls, "fileUpload:"+hdr.Filename)
			f.sessions = append(f.sessions, r.FormValue("sessionId"))
			f.files[hdr.Filename] = data
			f.fileAuth = append(f.fileAuth, r.Header.Get("service-account-id")+" "+r.Header.Get("Authorization"))
			if hdr.Filename == f.failFile {
				w.WriteHeader(http.StatusInternalServerError)
				io.WriteString(w, `{"errorCode":"500","errorMsg":"boom"}`)
				return
			}
			fmt.Fprintf(w, `{"fileKey":"key-%s","fileName":%q}`, hdr.Filename, hdr.Filename)
		case "/seller/contentInfo":
			f.calls = append(f.calls, r.URL.Path)
			if f.info != "" {
				io.WriteString(w, f.info)
				return
			}
			io.WriteString(w, `[{"contentStatus":"FOR_SALE","defaultLanguageCode":"CHI","paid":"N",`+
				`"binaryList":[{"versionCode":"5","versionName":"1.0","gms":"N","binarySeq":"3"}]}]`)
		case "/seller/contentUpdate":
			f.calls = append(f.calls, r.URL.Path)
			if err := json.NewDecoder(r.Body).Decode(&f.update); err != nil {
				t.Errorf("decode contentUpdate: %v", err)
			}
			io.WriteString(w, `{"resultCode":"0000"}`)
		case "/seller/v2/content/binary":
			f.calls = append(f.calls, r.URL.Path)
			answer(t, w, &f.binary, `{"resultCode":"0000"}`)
		case "/seller/contentSubmit":
			f.calls = append(f.calls, r.URL.Path)
			if answer(t, w, &f.submit, `{}`) == `{}` {
				f.submitted = true
			}
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSamsung) store() *Store {
	return &Store{
		client:           resty.New().SetBaseURL(f.srv.URL).SetHeader("Content-Type", "application/json").OnAfterResponse(errorOnHTTPStatus),
		serviceAccountID: "svc-acct",
		contentID:        "000001",
		accessToken:      "tok",
	}
}

func writePNG(t *testing.T, dir, name string, w, h int) string {
	t.Helper()
	path := filepath.Join(dir, name)
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if err := png.Encode(out, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeAPK(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "app.apk")
	if err := os.WriteFile(path, []byte("PK fake apk"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// baseUpdate is the contentUpdate body without a listing: the echoed
// contentInfo metadata plus the publication type.
var baseUpdate = map[string]any{
	"contentId":           "000001",
	"publicationType":     "01",
	"defaultLanguageCode": "CHI",
	"paid":                "N",
}

// TestUploadWithoutListing pins that a nil Listing leaves the flow and the
// contentUpdate body exactly as before: one binary upload, no listing keys.
func TestUploadWithoutListing(t *testing.T) {
	f := newFakeSamsung(t)
	dir := t.TempDir()
	if err := f.store().upload(t.Context(), &store.UploadRequest{FilePath: writeAPK(t, dir)}); err != nil {
		t.Fatalf("upload: %v", err)
	}
	wantCalls := []string{
		"/seller/createUploadSessionId", "fileUpload:app.apk", "/seller/contentInfo",
		"/seller/contentUpdate", "/seller/v2/content/binary", "/seller/contentSubmit",
	}
	if !reflect.DeepEqual(f.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", f.calls, wantCalls)
	}
	if !reflect.DeepEqual(f.update, baseUpdate) {
		t.Fatalf("contentUpdate body = %v, want %v", f.update, baseUpdate)
	}
}

// TestUploadWithListing checks that the icon and screenshots are uploaded
// through the binary's session before contentUpdate, and that contentUpdate
// carries the text fields plus the new file keys in display order. It goes
// the same way when the package isn't here (remote): only its facts and an
// Offloader, which sends the package and the (local) images.
func TestUploadWithListing(t *testing.T) {
	dir := t.TempDir()
	l := &store.Listing{
		Brief:       "一句话介绍",
		Description: "long description",
		Icon:        writePNG(t, dir, "icon.png", 512, 512),
	}
	for i := range 4 {
		l.Screenshots = append(l.Screenshots, writePNG(t, dir, fmt.Sprintf("shot%d.png", i), 540, 960))
	}
	apkPath := writeAPK(t, dir)
	apkData, err := os.ReadFile(apkPath)
	if err != nil {
		t.Fatal(err)
	}

	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprintf("remote=%v", remote), func(t *testing.T) {
			f := newFakeSamsung(t)
			req := &store.UploadRequest{FilePath: apkPath, Listing: l}
			ctx := context.Background()
			var off *offloadtest.Offloader
			if remote {
				req, ctx, off = offloadtest.Remote(t, ctx, req)
			}
			if res := f.store().Upload(ctx, req); !res.Success {
				t.Fatalf("upload: %s", res.Error)
			}

			wantCalls := []string{
				"/seller/createUploadSessionId", "fileUpload:app.apk",
				"fileUpload:icon.png", "fileUpload:shot0.png", "fileUpload:shot1.png", "fileUpload:shot2.png", "fileUpload:shot3.png",
				"/seller/contentInfo", "/seller/contentUpdate", "/seller/v2/content/binary", "/seller/contentSubmit",
			}
			if !reflect.DeepEqual(f.calls, wantCalls) {
				t.Fatalf("calls = %v, want %v", f.calls, wantCalls)
			}
			for i, s := range f.sessions {
				if s != "sess-1" {
					t.Errorf("fileUpload[%d] sessionId = %q, want sess-1", i, s)
				}
			}
			for i, a := range f.fileAuth {
				if a != "svc-acct Bearer tok" {
					t.Errorf("fileUpload[%d] service-account-id/Authorization = %q, want svc-acct Bearer tok", i, a)
				}
			}
			if !bytes.Equal(f.files["app.apk"], apkData) {
				t.Errorf("fileUpload got %d bytes of app.apk, want %d", len(f.files["app.apk"]), len(apkData))
			}

			want := map[string]any{
				"shortDescription": "一句话介绍",
				"longDescription":  "long description",
				"iconKey":          "key-icon.png",
				"screenshots": []any{
					map[string]any{"screenshotKey": "key-shot0.png", "reuseYn": false},
					map[string]any{"screenshotKey": "key-shot1.png", "reuseYn": false},
					map[string]any{"screenshotKey": "key-shot2.png", "reuseYn": false},
					map[string]any{"screenshotKey": "key-shot3.png", "reuseYn": false},
				},
			}
			for k, v := range baseUpdate {
				want[k] = v
			}
			if !reflect.DeepEqual(f.update, want) {
				t.Fatalf("contentUpdate body = %v, want %v", f.update, want)
			}

			if remote {
				if !off.Sent(req.FilePath) {
					t.Errorf("the package did not go out as a transfer")
				}
				if n := len(off.Transfers()); n != 6 {
					t.Errorf("%d transfers, want 6 (apk, icon, 4 screenshots)", n)
				}
			}
		})
	}
}

// TestUploadListingTextOnly checks that only non-empty listing fields are
// sent: no image uploads and no iconKey/screenshots keys.
func TestUploadListingTextOnly(t *testing.T) {
	f := newFakeSamsung(t)
	dir := t.TempDir()
	req := &store.UploadRequest{FilePath: writeAPK(t, dir), Listing: &store.Listing{Brief: "brief"}}
	if err := f.store().upload(t.Context(), req); err != nil {
		t.Fatalf("upload: %v", err)
	}
	want := map[string]any{"shortDescription": "brief"}
	for k, v := range baseUpdate {
		want[k] = v
	}
	if !reflect.DeepEqual(f.update, want) {
		t.Fatalf("contentUpdate body = %v, want %v", f.update, want)
	}
	if n := strings.Count(strings.Join(f.calls, ","), "fileUpload:"); n != 1 {
		t.Fatalf("got %d fileUploads, want 1 (the apk): %v", n, f.calls)
	}
}

// TestUploadListingImageFailure checks that a failed listing image upload
// fails the store before any contentUpdate / contentSubmit.
func TestUploadListingImageFailure(t *testing.T) {
	f := newFakeSamsung(t)
	f.failFile = "icon.png"
	dir := t.TempDir()
	req := &store.UploadRequest{
		FilePath: writeAPK(t, dir),
		Listing:  &store.Listing{Brief: "brief", Icon: writePNG(t, dir, "icon.png", 512, 512)},
	}
	err := f.store().upload(t.Context(), req)
	if err == nil || !strings.Contains(err.Error(), "listing icon") {
		t.Fatalf("upload error = %v, want a listing icon failure", err)
	}
	if f.update != nil || f.submitted {
		t.Fatalf("contentUpdate/contentSubmit ran after a listing failure: %v", f.calls)
	}
}

// TestListingSpec checks samsung's declared spec: byte-counted text, the
// 4–8 screenshot count, and the 2:1 aspect-ratio rule from checkListing.
func TestListingSpec(t *testing.T) {
	dir := t.TempDir()
	shot := writePNG(t, dir, "ok.png", 540, 960)
	edge := writePNG(t, dir, "edge.png", 400, 800) // exactly 2:1
	wide := writePNG(t, dir, "wide.png", 1000, 320)

	ok := &store.Listing{
		Brief:       strings.Repeat("中", 13), // 39 bytes
		Icon:        writePNG(t, dir, "icon.png", 512, 512),
		Screenshots: []string{shot, shot, shot, edge},
	}
	if errs := store.ValidateListing("samsung", ok); len(errs) > 0 {
		t.Fatalf("valid listing rejected: %v", errs)
	}

	cases := []struct {
		name string
		l    *store.Listing
		want string
	}{
		{"brief bytes", &store.Listing{Brief: strings.Repeat("中", 14)}, "42 bytes"},
		{"too few", &store.Listing{Screenshots: []string{shot, shot, shot}}, "at least 4"},
		{"aspect", &store.Listing{Screenshots: []string{shot, shot, shot, wide}}, "screenshots[3]"},
		{"icon size", &store.Listing{Icon: shot}, "512x512"},
	}
	for _, c := range cases {
		errs := store.ValidateListing("samsung", c.l)
		if len(errs) != 1 || !strings.Contains(errs[0].Error(), c.want) {
			t.Errorf("%s: errs = %v, want one containing %q", c.name, errs, c.want)
		}
	}
}
