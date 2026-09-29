package meizu

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

const (
	listURI    = "/open/api/v1/app/list"
	detailURI  = "/open/api/v1/app/detail"
	publishURI = "/open/api/v1/app/publish"
	failappURI = "/open/api/v1/app/failapp/update"
)

// detailFixture is the app/detail value the fake server returns: the
// currently-listed metadata publish must echo back.
const detailFixture = `{
	"id": 7, "verId": 42, "status": 50, "name": "MyApp", "versionName": "1.0.0",
	"packageName": "com.example", "appDescription": "old desc",
	"verDescription": "old notes", "keyword": "a b", "recommendDesc": "old brief",
	"authorName": "Dev", "devContact": "dev@example.com", "categoryId": 1,
	"category2Id": 12, "tagId": 34, "icon": "old/icon.png",
	"screenShots": ["old/s1.png", "old/s2.png"], "certificates": "c1.png,c2.png",
	"ageBracket": 3, "privacyPolicyUrl": "https://example.com/privacy",
	"softwareAuthorNum": "2020SR000001", "qualifcation": "q", "dwmc": "Org",
	"zjlx": 1, "zjhm": "Z1", "yyzzjlx": 2, "yyzmc": "Op", "yyzzjhm": "Y1",
	"yyzlxrlxfs": "13800000000", "yylb": "1", "zbzShengId": 11
}`

// wantPublishBody is the publish body for detailFixture with no listing:
// everything echoed, only packageUrl and verDesc replaced. It pins the
// request to what apkgo sent before listing support existed.
const wantPublishBody = `{
	"appName": "MyApp", "appDesc": "old desc", "verDesc": "new notes",
	"catid": 1, "cat2id": 12, "tagId": 34, "authorName": "Dev",
	"packageUrl": "apk/app.apk", "icon": "old/icon.png",
	"screenShots": ["old/s1.png", "old/s2.png"], "certificates": ["c1.png", "c2.png"],
	"privacyPolicyUrl": "https://example.com/privacy", "keyword": "a b",
	"recommendDesc": "old brief", "softwareAuthorNum": "2020SR000001",
	"devContact": "dev@example.com", "ageBracket": 3, "qualifcation": "q",
	"dwmc": "Org", "zjlx": 1, "zjhm": "Z1", "yyzzjlx": 2, "yyzmc": "Op",
	"yyzzjhm": "Y1", "yyzlxrlxfs": "13800000000", "yylb": "1", "zbzShengId": 11
}`

// fakeMeizu is an httptest handler for the endpoints Upload touches. It
// verifies every request's signature against its own path, records the
// call order and captures the submit body.
type fakeMeizu struct {
	t         *testing.T
	status    int  // latest version status reported by app/list
	failImage bool // app/image/upload answers 113007

	mu         sync.Mutex
	calls      []string
	submitURI  string
	submitBody map[string]any
}

func (f *fakeMeizu) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.calls = append(f.calls, r.URL.Path)
	f.mu.Unlock()

	h := r.Header
	sum := sha256.Sum256([]byte("clientId=" + h.Get("clientId") +
		"&timestamp=" + h.Get("timestamp") +
		"&traceId=" + h.Get("traceId") +
		"&uri=" + r.URL.Path + ":secret"))
	if h.Get("sign") != hex.EncodeToString(sum[:]) || h.Get("accessToken") != "tok" {
		f.t.Errorf("%s: bad signed headers", r.URL.Path)
	}

	// resty only decodes SetResult for a JSON content type.
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case listURI:
		fmt.Fprintf(w, `{"code":200,"value":{"total":1,"data":[{"id":7,"verId":42,"pkgName":"com.example","name":"MyApp","versionName":"1.0.0","status":%d}]}}`, f.status)
	case detailURI:
		if got := r.URL.Query().Get("verId"); got != "42" {
			f.t.Errorf("detail verId = %q, want 42", got)
		}
		fmt.Fprintf(w, `{"code":200,"value":%s}`, detailFixture)
	case apkUploadURI, imageUploadURI:
		file, hdr, err := r.FormFile("file")
		if err != nil {
			f.t.Errorf("%s: multipart field file: %v", r.URL.Path, err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		file.Close()
		prefix := "apk/"
		if r.URL.Path == imageUploadURI {
			if f.failImage {
				fmt.Fprint(w, `{"code":113007,"message":"文件格式不支持"}`)
				return
			}
			prefix = "img/"
		}
		fmt.Fprintf(w, `{"code":200,"value":{"destFileName":%q}}`, prefix+hdr.Filename)
	case publishURI, failappURI:
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Errorf("decode submit body: %v", err)
		}
		f.mu.Lock()
		f.submitURI, f.submitBody = r.URL.Path, body
		f.mu.Unlock()
		fmt.Fprint(w, `{"code":200,"value":{"verId":43}}`)
	default:
		http.NotFound(w, r)
	}
}

// newTestStore points a Store at srv with a pre-issued token, skipping
// New's token fetch.
func newTestStore(srv *httptest.Server) *Store {
	return &Store{
		baseURL:      srv.URL,
		client:       resty.New().SetBaseURL(srv.URL),
		uploadClient: srv.Client(),
		clientID:     "cid",
		clientSecret: "secret",
		accessToken:  "tok",
	}
}

func writePNG(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	return path
}

// runUpload runs Upload against a fake server and returns it for
// inspection.
func runUpload(t *testing.T, f *fakeMeizu, dir string, l *store.Listing) *store.UploadResult {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)

	apk := filepath.Join(dir, "app.apk")
	if err := os.WriteFile(apk, []byte("PK\x03\x04 fake apk"), 0o644); err != nil {
		t.Fatal(err)
	}
	return newTestStore(srv).Upload(context.Background(), &store.UploadRequest{
		FilePath:     apk,
		PackageName:  "com.example",
		VersionName:  "1.1.0",
		ReleaseNotes: "new notes",
		Listing:      l,
	})
}

func decodeJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// TestUploadListing drives the full upload flow and checks the publish
// body: without a listing it's byte-for-byte the pre-listing body; with
// one, only the non-empty fields are overridden, and the icon and
// screenshots go through app/image/upload (in display order) after the
// APK and before publish.
func TestUploadListing(t *testing.T) {
	dir := t.TempDir()
	icon := writePNG(t, dir, "icon.png")
	shot1 := writePNG(t, dir, "shot1.png")
	shot2 := writePNG(t, dir, "shot2.png")
	desc := strings.Repeat("新描述", 40)

	cases := []struct {
		name      string
		listing   *store.Listing
		wantCalls []string
		override  map[string]any
	}{
		{
			name:      "no listing",
			wantCalls: []string{listURI, detailURI, apkUploadURI, publishURI},
		},
		{
			name:      "text only",
			listing:   &store.Listing{Brief: "new brief", Description: desc},
			wantCalls: []string{listURI, detailURI, apkUploadURI, publishURI},
			override:  map[string]any{"recommendDesc": "new brief", "appDesc": desc},
		},
		{
			name:      "images only",
			listing:   &store.Listing{Icon: icon, Screenshots: []string{shot1, shot2}},
			wantCalls: []string{listURI, detailURI, apkUploadURI, imageUploadURI, imageUploadURI, imageUploadURI, publishURI},
			override: map[string]any{
				"icon":        "img/icon.png",
				"screenShots": []any{"img/shot1.png", "img/shot2.png"},
			},
		},
		{
			name:      "full listing",
			listing:   &store.Listing{Brief: "new brief", Description: desc, Icon: icon, Screenshots: []string{shot2, shot1}},
			wantCalls: []string{listURI, detailURI, apkUploadURI, imageUploadURI, imageUploadURI, imageUploadURI, publishURI},
			override: map[string]any{
				"recommendDesc": "new brief",
				"appDesc":       desc,
				"icon":          "img/icon.png",
				"screenShots":   []any{"img/shot2.png", "img/shot1.png"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeMeizu{t: t, status: statusOnSale}
			res := runUpload(t, f, dir, c.listing)
			if !res.Success {
				t.Fatalf("upload failed: %s", res.Error)
			}
			if res.ExternalID != "43" {
				t.Errorf("ExternalID = %q, want 43", res.ExternalID)
			}
			if !slices.Equal(f.calls, c.wantCalls) {
				t.Errorf("calls = %v, want %v", f.calls, c.wantCalls)
			}
			if f.submitURI != publishURI {
				t.Errorf("submitted to %s, want %s", f.submitURI, publishURI)
			}
			want := decodeJSON(t, wantPublishBody)
			maps.Copy(want, c.override)
			if !reflect.DeepEqual(f.submitBody, want) {
				got, _ := json.Marshal(f.submitBody)
				exp, _ := json.Marshal(want)
				t.Errorf("publish body\n got %s\nwant %s", got, exp)
			}
		})
	}
}

// TestUploadListingImageFailure: a failed listing step fails the store
// and nothing is submitted.
func TestUploadListingImageFailure(t *testing.T) {
	dir := t.TempDir()
	f := &fakeMeizu{t: t, status: statusOnSale, failImage: true}
	res := runUpload(t, f, dir, &store.Listing{Brief: "new brief", Icon: writePNG(t, dir, "icon.png")})
	if res.Success {
		t.Fatal("upload succeeded, want listing failure")
	}
	if !strings.Contains(res.Error, "listing") || !strings.Contains(res.Error, "113007") {
		t.Errorf("error = %q, want listing error carrying 113007", res.Error)
	}
	if slices.Contains(f.calls, publishURI) || f.submitBody != nil {
		t.Errorf("submitted despite listing failure; calls = %v", f.calls)
	}
}

// TestUploadRejectedUsesVerisonID: a rejected latest version resubmits
// via failapp/update, whose version-id parameter is spelled `verisonId`
// in the official docs (§3.7); `verId` goes along until one spelling is
// confirmed live. The listing applies there too.
func TestUploadRejectedUsesVerisonID(t *testing.T) {
	f := &fakeMeizu{t: t, status: statusRejected}
	res := runUpload(t, f, t.TempDir(), &store.Listing{Brief: "new brief"})
	if !res.Success {
		t.Fatalf("upload failed: %s", res.Error)
	}
	if f.submitURI != failappURI {
		t.Fatalf("submitted to %s, want %s", f.submitURI, failappURI)
	}
	if got := f.submitBody["verisonId"]; got != float64(42) {
		t.Errorf("verisonId = %v, want 42", got)
	}
	if got := f.submitBody["verId"]; got != float64(42) {
		t.Errorf("verId = %v, want 42", got)
	}
	if got := f.submitBody["recommendDesc"]; got != "new brief" {
		t.Errorf("recommendDesc = %v, want new brief", got)
	}
}

// TestListingSpec checks the declared spec through the shared validator,
// including the Check rules: banned description characters and the
// upload endpoint's jpg/png/jpeg extension check.
func TestListingSpec(t *testing.T) {
	dir := t.TempDir()
	okIcon := writePNG(t, dir, "icon.PNG")
	noExt := writePNG(t, dir, "icon")
	desc := strings.Repeat("描", 100)

	cases := []struct {
		name    string
		listing *store.Listing
		wantErr string
	}{
		{"long brief is unbounded", &store.Listing{Brief: strings.Repeat("推", 500)}, ""},
		{"description at min", &store.Listing{Description: desc}, ""},
		{"description too short", &store.Listing{Description: desc[:len(desc)-len("描")]}, "at least 100"},
		{"description too long", &store.Listing{Description: strings.Repeat("描", 1001)}, "at most 1000"},
		{"description banned char", &store.Listing{Description: desc + "Q&A"}, `"&"`},
		{"upper-case extension", &store.Listing{Icon: okIcon, Screenshots: []string{okIcon}}, ""},
		{"icon without extension", &store.Listing{Icon: noExt}, "must end in .png, .jpg or .jpeg"},
		{"screenshot without extension", &store.Listing{Screenshots: []string{okIcon, noExt}}, "screenshots[1]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := store.ValidateListing("meizu", c.listing)
			if c.wantErr == "" {
				if len(errs) > 0 {
					t.Fatalf("unexpected errors: %v", errs)
				}
				return
			}
			if len(errs) == 0 {
				t.Fatalf("no error, want %q", c.wantErr)
			}
			var msgs []string
			for _, err := range errs {
				msgs = append(msgs, err.Error())
			}
			if joined := strings.Join(msgs, "; "); !strings.Contains(joined, c.wantErr) {
				t.Errorf("errors = %s, want %q", joined, c.wantErr)
			}
		})
	}
}
