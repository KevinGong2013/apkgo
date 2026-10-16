package meizu

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"maps"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/KevinGong2013/apkgo/v4/pkg/internal/offloadtest"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// TestSignedHeaders verifies the header signature against the documented
// algorithm (open.flyme.cn/docs?id=333): sorted "k=v" pairs of traceId,
// clientId, timestamp and uri joined with "&", then ":"+clientSecret,
// hashed with SHA-256. Alphabetically the order is clientId, timestamp,
// traceId, uri — a regression here would produce 113033 签名错误 on
// every call.
func TestSignedHeaders(t *testing.T) {
	s := &Store{clientID: "cid-123", clientSecret: "secret-xyz", accessToken: "tok"}
	h := s.signedHeaders("/open/api/v1/app/publish")

	for _, k := range []string{"traceId", "clientId", "timestamp", "sign", "accessToken"} {
		if h[k] == "" {
			t.Fatalf("header %s is empty", k)
		}
	}
	if h["clientId"] != "cid-123" || h["accessToken"] != "tok" {
		t.Fatalf("credential headers wrong: %v", h)
	}

	want := sha256.Sum256([]byte(
		"clientId=cid-123" +
			"&timestamp=" + h["timestamp"] +
			"&traceId=" + h["traceId"] +
			"&uri=/open/api/v1/app/publish" +
			":secret-xyz"))
	if h["sign"] != hex.EncodeToString(want[:]) {
		t.Errorf("sign = %s, want %s", h["sign"], hex.EncodeToString(want[:]))
	}
}

// TestEnvelopeCode covers the tolerant code parsing: the docs specify an
// integer, but Meizu's sibling APIs return the code as a quoted string,
// and both must map onto the same success/error check.
func TestEnvelopeCode(t *testing.T) {
	cases := []struct {
		name string
		body string
		code int
		msg  string
	}{
		{"integer code", `{"code":200,"msg":""}`, 200, ""},
		{"string code", `{"code":"113033","message":"签名错误"}`, 113033, "签名错误"},
		{"null code", `{"code":null,"msg":"x"}`, 0, "x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var e envelope
			if err := json.Unmarshal([]byte(c.body), &e); err != nil {
				t.Fatal(err)
			}
			if int(e.Code) != c.code {
				t.Errorf("code = %d, want %d", e.Code, c.code)
			}
			if got := e.errOf().Error(); c.msg != "" && !strings.Contains(got, c.msg) {
				t.Errorf("errOf() = %q, want it to contain %q", got, c.msg)
			}
		})
	}
}

// TestPublishBody checks the detail→publish round-trip transforms: release
// notes override, comma-separated certificates become a list, and the
// fixed top-level category defaults when detail omits it.
func TestPublishBody(t *testing.T) {
	d := &appDetail{
		Name:           "MyApp",
		VerDescription: "old notes",
		Certificates:   "a.png, b.png,,",
	}
	body := d.publishBody("pkg-file-name", "new notes", nil)
	if body["verDesc"] != "new notes" {
		t.Errorf("verDesc = %v, want new notes", body["verDesc"])
	}
	if body["packageUrl"] != "pkg-file-name" {
		t.Errorf("packageUrl = %v", body["packageUrl"])
	}
	if body["catid"] != int64(1) {
		t.Errorf("catid = %v, want 1", body["catid"])
	}
	certs, ok := body["certificates"].([]string)
	if !ok || len(certs) != 2 || certs[0] != "a.png" || certs[1] != "b.png" {
		t.Errorf("certificates = %v, want [a.png b.png]", body["certificates"])
	}

	// Empty release notes keep the currently-listed version description.
	if got := d.publishBody("p", "", nil)["verDesc"]; got != "old notes" {
		t.Errorf("verDesc fallback = %v, want old notes", got)
	}
}

// spyReporter records the byte total uploadFile announces and the bytes
// then reported as sent (Add runs on the request-writing goroutine).
type spyReporter struct{ total, added atomic.Int64 }

func (r *spyReporter) Phase(string)  {}
func (r *spyReporter) Total(n int64) { r.total.Store(n) }
func (r *spyReporter) Add(n int64)   { r.added.Add(n) }

// TestUploadRemotePackage runs each upload with the APK on disk, then the
// way a service that keeps it in object storage does: a path that doesn't
// exist here, FileFacts and an Offloader (offloadtest.Remote). Meizu must
// get the same package under the same name, the same 512×512 launcher icon
// when it has none, the same listing images, and the same publish body
// either way; only the package's bytes count towards progress.
func TestUploadRemotePackage(t *testing.T) {
	dir := t.TempDir()
	raw, err := os.ReadFile(fixtureAPK)
	if err != nil {
		t.Fatal(err)
	}
	apk := filepath.Join(dir, "helloworld.apk")
	apk64 := filepath.Join(dir, "helloworld-arm64.apk")
	for _, p := range []string{apk, apk64} {
		if err := os.WriteFile(p, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The 32-bit slot of the split-arch case has no icon and other bytes,
	// so reading or sending it instead of the 64-bit package shows.
	placeholder := filepath.Join(dir, "app.apk")
	if err := os.WriteFile(placeholder, []byte("PK\x03\x04 fake apk"), 0o644); err != nil {
		t.Fatal(err)
	}
	listing := &store.Listing{Icon: writePNG(t, dir, "icon.png"), Screenshots: []string{writePNG(t, dir, "s1.png"), writePNG(t, dir, "s2.png")}}

	cases := []struct {
		name         string
		file, file64 string
		noIcon       bool // app/detail has none: the APK's launcher icon is uploaded
		listing      *store.Listing
		wantName     string // file name the package is uploaded as
		wantCalls    []string
	}{
		{
			name: "icon from the apk", file: apk, noIcon: true, wantName: "helloworld.apk",
			wantCalls: []string{listURI, detailURI, imageUploadURI, apkUploadURI, publishURI},
		},
		{
			name: "split arch sends the 64-bit package", file: placeholder, file64: apk64, noIcon: true, wantName: "helloworld-arm64.apk",
			wantCalls: []string{listURI, detailURI, imageUploadURI, apkUploadURI, publishURI},
		},
		{
			name: "listing images", file: apk, listing: listing, wantName: "helloworld.apk",
			wantCalls: []string{listURI, detailURI, apkUploadURI, imageUploadURI, imageUploadURI, imageUploadURI, publishURI},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var local *fakeMeizu
			for _, remote := range []bool{false, true} {
				f := &fakeMeizu{t: t, status: statusOnSale, noIcon: c.noIcon}
				srv := httptest.NewServer(f)
				t.Cleanup(srv.Close)
				rep := &spyReporter{}
				req := &store.UploadRequest{
					FilePath:     c.file,
					File64Path:   c.file64,
					PackageName:  "com.example",
					VersionName:  "1.1.0",
					ReleaseNotes: "new notes",
					Listing:      c.listing,
					Progress:     rep,
				}
				ctx := context.Background()
				var off *offloadtest.Offloader
				if remote {
					req, ctx, off = offloadtest.Remote(t, ctx, req)
				}
				if res := newTestStore(srv).Upload(ctx, req); !res.Success {
					t.Fatalf("remote=%v: upload failed: %s", remote, res.Error)
				}

				if !slices.Equal(f.calls, c.wantCalls) {
					t.Errorf("remote=%v: calls = %v, want %v", remote, f.calls, c.wantCalls)
				}
				if len(f.apks) != 1 || !bytes.Equal(f.apks[0], raw) || f.submitBody["packageUrl"] != "apk/"+c.wantName {
					t.Errorf("remote=%v: uploaded %d packages, published %v; want the %d-byte package as apk/%s", remote, len(f.apks), f.submitBody["packageUrl"], len(raw), c.wantName)
				}
				if c.noIcon {
					if len(f.images) != 1 {
						t.Fatalf("remote=%v: uploaded %d images, want the launcher icon", remote, len(f.images))
					}
					cfg, format, err := image.DecodeConfig(bytes.NewReader(f.images[0]))
					if err != nil || format != "png" || cfg.Width != iconSize || cfg.Height != iconSize {
						t.Errorf("remote=%v: uploaded icon is %s %dx%d (err %v), want png %dx%d", remote, format, cfg.Width, cfg.Height, err, iconSize, iconSize)
					}
				} else {
					var want [][]byte
					for _, p := range append([]string{c.listing.Icon}, c.listing.Screenshots...) {
						b, err := os.ReadFile(p)
						if err != nil {
							t.Fatal(err)
						}
						want = append(want, b)
					}
					if !reflect.DeepEqual(f.images, want) {
						t.Errorf("remote=%v: uploaded images differ from the listing's files", remote)
					}
				}
				if total, added := rep.total.Load(), rep.added.Load(); total != int64(len(raw)) || added != total {
					t.Errorf("remote=%v: progress total %d, added %d, want both %d", remote, total, added, len(raw))
				}

				if !remote {
					local = f
					continue
				}
				if !reflect.DeepEqual(f.images, local.images) {
					t.Errorf("remote run uploaded other images than the local one")
				}
				// The extracted icon's temp file name is random, so is its
				// destFileName; everything else must match.
				body, localBody := maps.Clone(f.submitBody), maps.Clone(local.submitBody)
				if c.noIcon {
					for _, b := range []map[string]any{body, localBody} {
						if icon, _ := b["icon"].(string); !strings.HasPrefix(icon, "img/apkgo_meizu_icon_") {
							t.Errorf("published icon = %q, want the uploaded launcher icon's destFileName", icon)
						}
						delete(b, "icon")
					}
				}
				if !reflect.DeepEqual(body, localBody) {
					t.Errorf("remote publish body = %v, local = %v", body, localBody)
				}
				sent := req.FilePath
				if req.File64Path != "" {
					sent = req.File64Path
				}
				if !off.Sent(sent) {
					t.Errorf("the package did not go out as a transfer")
				}
				if c.listing != nil && !off.Sent(c.listing.Icon) {
					t.Errorf("the listing icon did not go out as a transfer")
				}
			}
		})
	}
}
