package honor

import (
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

	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// honorCall is one request seen by fakeHonor, in arrival order.
type honorCall struct {
	path     string
	body     any    // decoded JSON body (nil for multipart uploads)
	objectID string // file-upload: objectId query param
	fileName string // file-upload: multipart file name
}

// fakeHonor serves the publish endpoints Store.upload drives. Each
// get-file-upload-url entry gets objectId 1001, 1002, …; file-upload
// fails for objectIDs listed in failUpload.
type fakeHonor struct {
	t          *testing.T
	srv        *httptest.Server
	failUpload map[string]bool
	landscape  bool // get-app-detail lists a landscape screenshot (fileType 2)

	mu      sync.Mutex
	calls   []honorCall
	nextObj int64
}

func newFakeHonor(t *testing.T) *fakeHonor {
	f := &fakeHonor{t: t, nextObj: 1000, failUpload: map[string]bool{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeHonor) store() *Store {
	return &Store{
		client:      resty.New().SetBaseURL(f.srv.URL).SetHeader("Content-Type", "application/json"),
		accessToken: "tok",
		configAppID: "123",
	}
}

func (f *fakeHonor) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	call := honorCall{path: r.URL.Path}

	switch r.URL.Path {
	case "/openapi/v1/publish/get-app-detail":
		f.calls = append(f.calls, call)
		files := `[{"fileName":"app.apk","fileType":100}]`
		if f.landscape {
			files = `[{"fileName":"app.apk","fileType":100},{"fileName":"l1.png","fileType":2,"languageId":"zh-CN","order":0}]`
		}
		io.WriteString(w, `{"code":0,"data":{"languageInfo":[
			{"languageId":"en-US","appName":"App","intro":"old intro en"},
			{"languageId":"zh-CN","appName":"应用","intro":"旧介绍","briefIntro":"旧简介"}],"fileInfo":`+files+`}}`)
		return
	case "/openapi/v1/publish/file-upload":
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			f.t.Errorf("file-upload: parse multipart: %v", err)
		}
		call.objectID = r.URL.Query().Get("objectId")
		if _, hdr, err := r.FormFile("file"); err == nil {
			call.fileName = hdr.Filename
		}
		f.calls = append(f.calls, call)
		if f.failUpload[call.objectID] {
			io.WriteString(w, `{"code":50001,"msg":"file check failed"}`)
			return
		}
		io.WriteString(w, `{"code":0}`)
		return
	}

	b, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(b, &call.body)
	f.calls = append(f.calls, call)

	switch r.URL.Path {
	case "/openapi/v1/publish/get-file-upload-url":
		entries, _ := call.body.([]any)
		var data []map[string]any
		for _, e := range entries {
			f.nextObj++
			name, _ := e.(map[string]any)["fileName"].(string)
			data = append(data, map[string]any{
				"fileName":  name,
				"objectId":  f.nextObj,
				"uploadUrl": fmt.Sprintf("%s/openapi/v1/publish/file-upload?appId=123&objectId=%d", f.srv.URL, f.nextObj),
			})
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
	case "/openapi/v1/publish/submit-audit":
		io.WriteString(w, `{"code":0,"data":"rel-1"}`)
	default: // update-file-info, update-language-info
		io.WriteString(w, `{"code":0}`)
	}
}

// paths returns the request paths with the common prefix trimmed.
func (f *fakeHonor) paths() []string {
	var out []string
	for _, c := range f.calls {
		out = append(out, strings.TrimPrefix(c.path, "/openapi/v1/publish/"))
	}
	return out
}

// only returns the calls to one endpoint.
func (f *fakeHonor) only(endpoint string) []honorCall {
	var out []honorCall
	for _, c := range f.calls {
		if c.path == "/openapi/v1/publish/"+endpoint {
			out = append(out, c)
		}
	}
	return out
}

// jsonOf round-trips v through JSON so it compares equal to a decoded body.
func jsonOf(t *testing.T, v any) any {
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

func writeAPK(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "app.apk")
	if err := os.WriteFile(p, []byte("fake apk"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestUploadWithoutListingUnchanged pins the request sequence and bodies
// of a plain upload, so the listing work can't leak into it: APK staged
// and bound alone, language info echoed back verbatim with setAll=0 (so
// other languages survive), then submit.
func TestUploadWithoutListingUnchanged(t *testing.T) {
	f := newFakeHonor(t)
	dir := t.TempDir()
	req := &store.UploadRequest{FilePath: writeAPK(t, dir), PackageName: "com.example", ReleaseNotes: "bug fixes"}

	if res := f.store().Upload(context.Background(), req); !res.Success {
		t.Fatalf("Upload failed: %s", res.Error)
	}

	wantPaths := []string{"get-app-detail", "get-file-upload-url", "file-upload", "update-file-info", "update-language-info", "submit-audit"}
	if got := f.paths(); !reflect.DeepEqual(got, wantPaths) {
		t.Fatalf("request sequence = %v\nwant %v", got, wantPaths)
	}
	if got, want := f.only("get-file-upload-url")[0].body.([]any)[0].(map[string]any)["fileType"], float64(fileTypeAPK); got != want {
		t.Errorf("get-file-upload-url fileType = %v, want %v", got, want)
	}
	wantBind := jsonOf(t, map[string]any{"bindingFileList": []map[string]any{{"objectId": 1001}}})
	if got := f.only("update-file-info")[0].body; !reflect.DeepEqual(got, wantBind) {
		t.Errorf("update-file-info body = %v\nwant %v", got, wantBind)
	}
	wantLang := jsonOf(t, map[string]any{"languageInfoList": []map[string]any{{
		"languageId": "zh-CN", "appName": "应用", "intro": "旧介绍", "briefIntro": "旧简介", "newFeature": "bug fixes",
	}}, "setAll": 0})
	if got := f.only("update-language-info")[0].body; !reflect.DeepEqual(got, wantLang) {
		t.Errorf("update-language-info body = %v\nwant %v", got, wantLang)
	}
}

// TestUploadWithListing covers a full listing: icon (fileType 1) and
// portrait screenshots (fileType 3) are staged after the APK and bound in
// the same update-file-info call with languageId (+ order for
// screenshots); update-language-info carries the new intro / briefIntro
// with setAll=0; all of it lands before submit-audit.
func TestUploadWithListing(t *testing.T) {
	f := newFakeHonor(t)
	dir := t.TempDir()
	listing := &store.Listing{
		Brief:       "新的一句话简介",
		Description: "新的应用介绍",
		Icon:        writePNG(t, dir, "icon.png", 512, 512),
		Screenshots: []string{
			writePNG(t, dir, "s1.png", 1080, 1920),
			writePNG(t, dir, "s2.png", 1080, 1920),
			writePNG(t, dir, "s3.jpg", 1080, 1920), // png content, misleading extension
		},
	}
	if errs := store.ValidateListing("honor", listing); len(errs) > 0 {
		t.Fatalf("test listing should satisfy honor's spec: %v", errs)
	}
	req := &store.UploadRequest{FilePath: writeAPK(t, dir), PackageName: "com.example", ReleaseNotes: "bug fixes", Listing: listing}

	res := f.store().Upload(context.Background(), req)
	if !res.Success {
		t.Fatalf("Upload failed: %s", res.Error)
	}
	if res.ExternalID != "rel-1" {
		t.Errorf("ExternalID = %q, want rel-1", res.ExternalID)
	}

	wantPaths := []string{"get-app-detail",
		"get-file-upload-url", "file-upload", // apk
		"get-file-upload-url", "file-upload", // icon
		"get-file-upload-url", "file-upload", // screenshots
		"get-file-upload-url", "file-upload",
		"get-file-upload-url", "file-upload",
		"update-file-info", "update-language-info", "submit-audit"}
	if got := f.paths(); !reflect.DeepEqual(got, wantPaths) {
		t.Fatalf("request sequence = %v\nwant %v", got, wantPaths)
	}

	var gotTypes []any
	var gotNames []any
	for _, c := range f.only("get-file-upload-url") {
		e := c.body.([]any)[0].(map[string]any)
		gotTypes = append(gotTypes, e["fileType"])
		gotNames = append(gotNames, e["fileName"])
	}
	if want := []any{float64(fileTypeAPK), float64(1), float64(3), float64(3), float64(3)}; !reflect.DeepEqual(gotTypes, want) {
		t.Errorf("get-file-upload-url fileTypes = %v, want %v", gotTypes, want)
	}
	if want := []any{"app.apk", "icon.png", "s1.png", "s2.png", "s3.png"}; !reflect.DeepEqual(gotNames, want) {
		t.Errorf("get-file-upload-url fileNames = %v, want %v", gotNames, want)
	}
	var uploaded []string
	for _, c := range f.only("file-upload") {
		uploaded = append(uploaded, c.objectID)
	}
	if want := []string{"1001", "1002", "1003", "1004", "1005"}; !reflect.DeepEqual(uploaded, want) {
		t.Errorf("file-upload objectIds = %v, want %v", uploaded, want)
	}

	wantBind := jsonOf(t, map[string]any{"bindingFileList": []map[string]any{
		{"objectId": 1001},
		{"objectId": 1002, "languageId": "zh-CN"},
		{"objectId": 1003, "languageId": "zh-CN", "order": 0},
		{"objectId": 1004, "languageId": "zh-CN", "order": 1},
		{"objectId": 1005, "languageId": "zh-CN", "order": 2},
	}})
	if got := f.only("update-file-info")[0].body; !reflect.DeepEqual(got, wantBind) {
		t.Errorf("update-file-info body = %v\nwant %v", got, wantBind)
	}

	wantLang := jsonOf(t, map[string]any{
		"languageInfoList": []map[string]any{{
			"languageId": "zh-CN", "appName": "应用", "intro": "新的应用介绍", "briefIntro": "新的一句话简介", "newFeature": "bug fixes",
		}},
		"setAll": 0,
	})
	if got := f.only("update-language-info")[0].body; !reflect.DeepEqual(got, wantLang) {
		t.Errorf("update-language-info body = %v\nwant %v", got, wantLang)
	}
}

// TestUploadListingPartialFields: only non-empty listing fields are sent —
// a brief-only listing keeps the console's intro and uploads no images; an
// images-only listing without release notes skips update-language-info.
func TestUploadListingPartialFields(t *testing.T) {
	t.Run("brief only", func(t *testing.T) {
		f := newFakeHonor(t)
		dir := t.TempDir()
		req := &store.UploadRequest{FilePath: writeAPK(t, dir), Listing: &store.Listing{Brief: "只改简介"}}
		if res := f.store().Upload(context.Background(), req); !res.Success {
			t.Fatalf("Upload failed: %s", res.Error)
		}
		if n := len(f.only("get-file-upload-url")); n != 1 {
			t.Errorf("get-file-upload-url calls = %d, want 1 (apk only)", n)
		}
		wantLang := jsonOf(t, map[string]any{
			"languageInfoList": []map[string]any{{
				"languageId": "zh-CN", "appName": "应用", "intro": "旧介绍", "briefIntro": "只改简介",
			}},
			"setAll": 0,
		})
		if got := f.only("update-language-info")[0].body; !reflect.DeepEqual(got, wantLang) {
			t.Errorf("update-language-info body = %v\nwant %v", got, wantLang)
		}
	})

	t.Run("icon only, no notes", func(t *testing.T) {
		f := newFakeHonor(t)
		dir := t.TempDir()
		req := &store.UploadRequest{FilePath: writeAPK(t, dir), Listing: &store.Listing{Icon: writePNG(t, dir, "icon.png", 512, 512)}}
		if res := f.store().Upload(context.Background(), req); !res.Success {
			t.Fatalf("Upload failed: %s", res.Error)
		}
		want := []string{"get-app-detail", "get-file-upload-url", "file-upload", "get-file-upload-url", "file-upload", "update-file-info", "submit-audit"}
		if got := f.paths(); !reflect.DeepEqual(got, want) {
			t.Fatalf("request sequence = %v\nwant %v", got, want)
		}
	})
}

// TestUploadListingImageFailureSkipsSubmit: a failed listing step must
// abort before anything is bound or submitted for review.
func TestUploadListingImageFailureSkipsSubmit(t *testing.T) {
	f := newFakeHonor(t)
	f.failUpload["1003"] = true // first screenshot
	dir := t.TempDir()
	listing := &store.Listing{
		Icon: writePNG(t, dir, "icon.png", 512, 512),
		Screenshots: []string{
			writePNG(t, dir, "s1.png", 1080, 1920),
			writePNG(t, dir, "s2.png", 1080, 1920),
			writePNG(t, dir, "s3.png", 1080, 1920),
		},
	}
	req := &store.UploadRequest{FilePath: writeAPK(t, dir), ReleaseNotes: "notes", Listing: listing}

	res := f.store().Upload(context.Background(), req)
	if res.Success {
		t.Fatal("Upload succeeded, want a listing error")
	}
	if !strings.Contains(res.Error, "screenshot 1") {
		t.Errorf("error = %q, want it to name the failing screenshot", res.Error)
	}
	for _, endpoint := range []string{"update-file-info", "update-language-info", "submit-audit"} {
		if n := len(f.only(endpoint)); n != 0 {
			t.Errorf("%s called %d times after a failed listing upload, want 0", endpoint, n)
		}
	}
}

// TestUploadListingRejectsLandscapeApp: Honor takes one screenshot
// orientation per app; when the console currently holds landscape ones,
// a portrait listing is refused before anything is uploaded.
func TestUploadListingRejectsLandscapeApp(t *testing.T) {
	f := newFakeHonor(t)
	f.landscape = true
	dir := t.TempDir()
	listing := &store.Listing{Screenshots: []string{
		writePNG(t, dir, "s1.png", 1080, 1920),
		writePNG(t, dir, "s2.png", 1080, 1920),
		writePNG(t, dir, "s3.png", 1080, 1920),
	}}
	res := f.store().Upload(context.Background(), &store.UploadRequest{FilePath: writeAPK(t, dir), Listing: listing})
	if res.Success || !strings.Contains(res.Error, "landscape screenshots") {
		t.Fatalf("result = %+v, want a landscape-orientation error", res)
	}
	if got, want := f.paths(), []string{"get-app-detail"}; !reflect.DeepEqual(got, want) {
		t.Errorf("request sequence = %v, want %v (nothing uploaded)", got, want)
	}
	// Text-only listings don't touch screenshots and still go through.
	f = newFakeHonor(t)
	f.landscape = true
	res = f.store().Upload(context.Background(), &store.UploadRequest{FilePath: writeAPK(t, dir), Listing: &store.Listing{Brief: "新简介"}})
	if !res.Success {
		t.Fatalf("text-only listing on a landscape app failed: %s", res.Error)
	}
}
