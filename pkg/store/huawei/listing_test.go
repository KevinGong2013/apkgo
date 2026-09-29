package huawei

import (
	"context"
	"encoding/binary"
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
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// fakeAGC is an httptest stand-in for the AGC Publishing API v2 endpoints
// the upload flow touches. It records every call in order ("METHOD what")
// plus the JSON bodies of the listing-related PUTs.
type fakeAGC struct {
	t   *testing.T
	srv *httptest.Server

	defaultLang      string // app-info appInfo.defaultLang; "" = omitted
	languageInfoCode int    // ret.code for app-language-info

	mu           sync.Mutex
	calls        []string
	uploads      int
	fileInfo     []map[string]any
	languageInfo []map[string]any
}

func newFakeAGC(t *testing.T) *fakeAGC {
	f := &fakeAGC{t: t, defaultLang: "en-US"}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAGC) store() *Store {
	return &Store{client: resty.New().SetBaseURL(f.srv.URL), configAppID: "app-1"}
}

func (f *fakeAGC) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeAGC) handle(w http.ResponseWriter, r *http.Request) {
	ok := `{"ret":{"code":0,"msg":"success"}}`
	w.Header().Set("Content-Type", "application/json") // resty only decodes JSON responses
	path := strings.TrimPrefix(r.URL.Path, "/api/publish/v2/")
	if r.URL.Path != "/upload" && r.URL.Query().Get("appId") != "app-1" {
		f.t.Errorf("%s %s: appId = %q, want app-1", r.Method, r.URL.Path, r.URL.Query().Get("appId"))
	}
	switch r.Method + " " + path {
	case "GET upload-url":
		f.record("GET upload-url suffix=" + r.URL.Query().Get("suffix"))
		fmt.Fprintf(w, `{"ret":{"code":0},"uploadUrl":%q,"authCode":"auth-1"}`, f.srv.URL+"/upload")

	case "POST /upload":
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			f.t.Errorf("upload: parse multipart: %v", err)
		}
		if got := r.FormValue("authCode"); got != "auth-1" {
			f.t.Errorf("upload: authCode = %q, want auth-1", got)
		}
		file, hdr, err := r.FormFile("file")
		if err != nil {
			f.t.Errorf("upload: no file part: %v", err)
			http.Error(w, "no file", http.StatusBadRequest)
			return
		}
		io.Copy(io.Discard, file)
		file.Close()
		f.record("POST upload")
		f.mu.Lock()
		f.uploads++
		n := f.uploads
		f.mu.Unlock()
		extra := ""
		if strings.HasSuffix(hdr.Filename, ".png") {
			extra = fmt.Sprintf(`,"imageResolution":"res-%d","imageResolutionSingature":"sig-%d"`, n, n)
		}
		fmt.Fprintf(w, `{"result":{"UploadFileRsp":{"ifSuccess":1,"fileInfoList":[{"fileDestUlr":"dest-%d"%s}]},"resultCode":"0"}}`, n, extra)

	case "PUT app-file-info":
		body := f.decode(r)
		f.record(fmt.Sprintf("PUT app-file-info fileType=%v", body["fileType"]))
		f.mu.Lock()
		f.fileInfo = append(f.fileInfo, body)
		f.mu.Unlock()
		io.WriteString(w, ok)

	case "PUT app-info":
		f.record("PUT app-info")
		io.WriteString(w, ok)

	case "GET app-info":
		f.record("GET app-info")
		if f.defaultLang == "" {
			io.WriteString(w, `{"ret":{"code":0},"appInfo":{"releaseState":0},"languages":[]}`)
			return
		}
		fmt.Fprintf(w, `{"ret":{"code":0},"appInfo":{"releaseState":0,"defaultLang":%q},"languages":[]}`, f.defaultLang)

	case "PUT app-language-info":
		body := f.decode(r)
		f.record("PUT app-language-info")
		f.mu.Lock()
		f.languageInfo = append(f.languageInfo, body)
		f.mu.Unlock()
		fmt.Fprintf(w, `{"ret":{"code":%d,"msg":"lang failed"}}`, f.languageInfoCode)

	case "POST app-submit":
		f.record("POST app-submit")
		io.WriteString(w, ok)

	default:
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}
}

func (f *fakeAGC) decode(r *http.Request) map[string]any {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		f.t.Errorf("%s %s: decode body: %v", r.Method, r.URL.Path, err)
	}
	return body
}

func writeAPK(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "app.apk")
	if err := os.WriteFile(p, []byte("PK\x03\x04 fake apk"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func writePNG(t *testing.T, dir, name string, w, h int) string {
	t.Helper()
	p := filepath.Join(dir, name)
	out, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if err := png.Encode(out, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return p
}

// jsonValue round-trips v through JSON so it compares equal to a decoded
// request body.
func jsonValue(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestUploadWithListing: text goes to app-language-info and images to
// app-file-info (icon fileType 0, screenshots fileType 2) for the app's
// default language — all after the APK is bound and before app-submit.
func TestUploadWithListing(t *testing.T) {
	agc := newFakeAGC(t)
	dir := t.TempDir()
	listing := &store.Listing{
		Brief:       "一句话简介",
		Description: "应用介绍长描述",
		Icon:        writePNG(t, dir, "icon.png", 216, 216),
		Screenshots: []string{
			writePNG(t, dir, "s1.png", 1080, 1920),
			writePNG(t, dir, "s2.png", 1080, 1920),
			writePNG(t, dir, "s3.png", 1080, 1920),
		},
	}
	if errs := store.ValidateListing("huawei", listing); len(errs) > 0 {
		t.Fatalf("test listing violates listingSpec: %v", errs)
	}

	res := agc.store().Upload(context.Background(), &store.UploadRequest{
		FilePath:     writeAPK(t),
		PackageName:  "com.example",
		ReleaseNotes: "fix bugs",
		Listing:      listing,
	})
	if !res.Success {
		t.Fatalf("upload failed: %s", res.Error)
	}

	wantCalls := []string{
		"GET upload-url suffix=apk", "POST upload", "PUT app-file-info fileType=5",
		"PUT app-info",
		"GET app-info",
		"PUT app-language-info",
		"GET upload-url suffix=png", "POST upload", "PUT app-file-info fileType=0",
		"GET upload-url suffix=png", "POST upload",
		"GET upload-url suffix=png", "POST upload",
		"GET upload-url suffix=png", "POST upload",
		"PUT app-file-info fileType=2",
		"POST app-submit",
	}
	if !reflect.DeepEqual(agc.calls, wantCalls) {
		t.Fatalf("calls:\n got  %q\n want %q", agc.calls, wantCalls)
	}

	wantLang := map[string]any{"lang": "en-US", "briefInfo": "一句话简介", "appDesc": "应用介绍长描述"}
	if !reflect.DeepEqual(agc.languageInfo, []map[string]any{wantLang}) {
		t.Errorf("app-language-info body = %v, want %v", agc.languageInfo, wantLang)
	}

	if len(agc.fileInfo) != 3 {
		t.Fatalf("app-file-info calls = %d, want 3", len(agc.fileInfo))
	}
	wantIcon := jsonValue(t, map[string]any{
		"fileType":   0,
		"lang":       "en-US",
		"deviceType": 4,
		"files": []map[string]string{
			{"fileName": "icon.png", "fileDestUrl": "dest-2", "imageResolution": "res-2", "imageResolutionSingature": "sig-2"},
		},
	})
	if got := jsonValue(t, agc.fileInfo[1]); !reflect.DeepEqual(got, wantIcon) {
		t.Errorf("icon app-file-info body:\n got  %v\n want %v", got, wantIcon)
	}
	wantShots := jsonValue(t, map[string]any{
		"fileType":    2,
		"lang":        "en-US",
		"deviceType":  4,
		"imgShowType": 0,
		"files": []map[string]string{
			{"fileName": "s1.png", "fileDestUrl": "dest-3", "imageResolution": "res-3", "imageResolutionSingature": "sig-3"},
			{"fileName": "s2.png", "fileDestUrl": "dest-4", "imageResolution": "res-4", "imageResolutionSingature": "sig-4"},
			{"fileName": "s3.png", "fileDestUrl": "dest-5", "imageResolution": "res-5", "imageResolutionSingature": "sig-5"},
		},
	})
	if got := jsonValue(t, agc.fileInfo[2]); !reflect.DeepEqual(got, wantShots) {
		t.Errorf("screenshots app-file-info body:\n got  %v\n want %v", got, wantShots)
	}
}

// TestUploadWithoutListing: a nil (or empty) Listing adds no requests —
// the flow is exactly upload-url → upload → app-file-info (APK) →
// app-info (notes) → app-submit, with the APK body unchanged.
func TestUploadWithoutListing(t *testing.T) {
	for name, listing := range map[string]*store.Listing{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			agc := newFakeAGC(t)
			res := agc.store().Upload(context.Background(), &store.UploadRequest{
				FilePath:     writeAPK(t),
				PackageName:  "com.example",
				ReleaseNotes: "fix bugs",
				Listing:      listing,
			})
			if !res.Success {
				t.Fatalf("upload failed: %s", res.Error)
			}
			wantCalls := []string{
				"GET upload-url suffix=apk", "POST upload", "PUT app-file-info fileType=5",
				"PUT app-info",
				"POST app-submit",
			}
			if !reflect.DeepEqual(agc.calls, wantCalls) {
				t.Fatalf("calls:\n got  %q\n want %q", agc.calls, wantCalls)
			}
			wantAPK := jsonValue(t, map[string]any{
				"fileType": 5,
				"files":    []map[string]string{{"fileName": "app.apk", "fileDestUrl": "dest-1"}},
			})
			if got := jsonValue(t, agc.fileInfo[0]); !reflect.DeepEqual(got, wantAPK) {
				t.Errorf("apk app-file-info body:\n got  %v\n want %v", got, wantAPK)
			}
		})
	}
}

// TestUploadListingTextOnly: only the given text field is sent, no image
// calls are made, and a missing defaultLang falls back to zh-CN.
func TestUploadListingTextOnly(t *testing.T) {
	agc := newFakeAGC(t)
	agc.defaultLang = ""
	res := agc.store().Upload(context.Background(), &store.UploadRequest{
		FilePath:    writeAPK(t),
		PackageName: "com.example",
		Listing:     &store.Listing{Brief: "新简介"},
	})
	if !res.Success {
		t.Fatalf("upload failed: %s", res.Error)
	}
	wantCalls := []string{
		"GET upload-url suffix=apk", "POST upload", "PUT app-file-info fileType=5",
		"GET app-info",
		"PUT app-language-info",
		"POST app-submit",
	}
	if !reflect.DeepEqual(agc.calls, wantCalls) {
		t.Fatalf("calls:\n got  %q\n want %q", agc.calls, wantCalls)
	}
	want := []map[string]any{{"lang": "zh-CN", "briefInfo": "新简介"}}
	if !reflect.DeepEqual(agc.languageInfo, want) {
		t.Errorf("app-language-info body = %v, want %v", agc.languageInfo, want)
	}
}

// TestUploadListingFailureSkipsSubmit: a failed listing step fails the
// store and the version is not submitted for review.
func TestUploadListingFailureSkipsSubmit(t *testing.T) {
	agc := newFakeAGC(t)
	agc.languageInfoCode = 204144647
	res := agc.store().Upload(context.Background(), &store.UploadRequest{
		FilePath:    writeAPK(t),
		PackageName: "com.example",
		Listing:     &store.Listing{Description: "desc"},
	})
	if res.Success {
		t.Fatal("upload succeeded, want listing failure")
	}
	if !strings.Contains(res.Error, "update listing") || !strings.Contains(res.Error, "204144647") {
		t.Errorf("error = %q, want it to name the listing step and ret code", res.Error)
	}
	for _, c := range agc.calls {
		if c == "POST app-submit" {
			t.Fatalf("app-submit called after listing failure; calls = %q", agc.calls)
		}
	}
}

// writeWebP writes a minimal lossless WEBP header of the given size
// (enough for image.DecodeConfig), padded to size bytes.
func writeWebP(t *testing.T, dir, name string, w, h, size int) string {
	t.Helper()
	n := size - 20 // RIFF header (12) + VP8L chunk header (8)
	data := make([]byte, n)
	data[0] = 0x2f // VP8L signature
	binary.LittleEndian.PutUint32(data[1:], uint32(w-1)|uint32(h-1)<<14)
	b := append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(4+8+n))...)
	b = append(b, "WEBPVP8L"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(n))
	b = append(b, data...)
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestListingSpec pins the console's Android phone rules: 3–5 screenshots
// at 9:16 (450×800 is only the suggested size), PNG/JPEG ≤2MB, WEBP ≤100KB.
func TestListingSpec(t *testing.T) {
	dir := t.TempDir()
	shot := func(name string, w, h int) string { return writePNG(t, dir, name, w, h) }
	three := func(first string) []string {
		return []string{first, shot("b.png", 450, 800), shot("c.png", 450, 800)}
	}
	cases := []struct {
		name    string
		listing store.Listing
		want    string // "" = valid
	}{
		{"suggested size", store.Listing{Icon: shot("icon.png", 216, 216), Screenshots: three(shot("a.png", 450, 800))}, ""},
		{"larger 9:16", store.Listing{Screenshots: three(shot("big.png", 1080, 1920))}, ""},
		{"small webp", store.Listing{Icon: writeWebP(t, dir, "i.webp", 216, 216, 50<<10), Screenshots: three(writeWebP(t, dir, "s.webp", 450, 800, 90<<10))}, ""},
		{"landscape", store.Listing{Screenshots: three(shot("l.png", 800, 450))}, "aspect ratio must be 9:16"},
		{"too few", store.Listing{Screenshots: three(shot("a.png", 450, 800))[:2]}, "need at least 3"},
		{"too many", store.Listing{Screenshots: append(three(shot("a.png", 450, 800)), shot("d.png", 450, 800), shot("e.png", 450, 800), shot("f.png", 450, 800))}, "at most 5 allowed"},
		{"large webp screenshot", store.Listing{Screenshots: three(writeWebP(t, dir, "big.webp", 450, 800, 100<<10+1))}, "webp 102401 bytes, at most 102400 allowed"},
		{"large webp icon", store.Listing{Icon: writeWebP(t, dir, "big-icon.webp", 216, 216, 100<<10+1)}, "icon: "},
		{"wrong icon size", store.Listing{Icon: shot("icon512.png", 512, 512)}, "want 216x216"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := store.ValidateListing("huawei", &c.listing)
			got := errors.Join(errs...)
			switch {
			case c.want == "" && got != nil:
				t.Errorf("unexpected errors: %v", got)
			case c.want != "" && (got == nil || !strings.Contains(got.Error(), c.want)):
				t.Errorf("errors = %v, want one containing %q", got, c.want)
			}
		})
	}
}
