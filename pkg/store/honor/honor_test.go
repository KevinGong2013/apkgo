package honor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/KevinGong2013/apkgo/v4/pkg/internal/offloadtest"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// TestShouldURLPush covers honor's size gate: it only hands the APK to
// honor's async download-from-URL (which throttles status polls to
// ~3min) when the file is at least urlPushMinBytes; smaller files upload
// faster directly, and a threshold of 0 always takes the URL. The size
// comes from the facts when given; an unknown size falls back to the
// upload path.
func TestShouldURLPush(t *testing.T) {
	dir := t.TempDir()
	mkfile := func(name string, size int) store.PackageFile {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
		return store.PackageFile{Path: p}
	}
	small := mkfile("small.apk", 10)
	big := mkfile("big.apk", 200)
	missing := store.PackageFile{Path: filepath.Join(dir, "nope.apk")}
	remoteBig := store.PackageFile{Path: missing.Path, Facts: &store.FileFacts{Size: 200}}

	cases := []struct {
		name     string
		minBytes int64
		file     store.PackageFile
		want     bool
	}{
		{"above threshold", 100, big, true},
		{"below threshold", 100, small, false},
		{"equal threshold is inclusive", 200, big, true},
		{"zero always URL-pushes", 0, small, true},
		{"zero needs no size", 0, missing, true},
		{"size from facts", 100, remoteBig, true},
		{"missing file falls back to upload", 100, missing, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &Store{urlPushMinBytes: c.minBytes}
			if got := s.shouldURLPush(c.file); got != c.want {
				t.Errorf("shouldURLPush(min=%d) = %v, want %v", c.minBytes, got, c.want)
			}
		})
	}
}

// TestParseURLPushMin: url_push_min_mb 0 means always URL-push; unset or
// invalid keeps the 100MB default.
func TestParseURLPushMin(t *testing.T) {
	const mb = 1 << 20
	cases := []struct {
		in   string
		want int64
	}{
		{"", 100 * mb},
		{"0", 0},
		{" 0 ", 0},
		{"5", 5 * mb},
		{"-1", 100 * mb},
		{"abc", 100 * mb},
	}
	for _, c := range cases {
		if got := parseURLPushMin(c.in); got != c.want {
			t.Errorf("parseURLPushMin(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestUploadByURL: with -f a URL and the APK at or over the size gate (0 =
// any size), Honor gets the URL with the package's name, size and sha256
// and pulls the package itself, which is then bound like an uploaded one.
// No bytes are sent, so with the package not here (remote) its facts are
// all it takes — the size gate included.
func TestUploadByURL(t *testing.T) {
	apkPath := writeAPK(t, t.TempDir())
	apkData, err := os.ReadFile(apkPath)
	if err != nil {
		t.Fatal(err)
	}
	const sourceURL = "https://cdn.example.com/app.apk"
	for _, minBytes := range []int64{0, 1} {
		for _, remote := range []bool{false, true} {
			t.Run(fmt.Sprintf("min=%d/remote=%v", minBytes, remote), func(t *testing.T) {
				f := newFakeHonor(t)
				s := f.store()
				s.urlPushMinBytes = minBytes
				req := &store.UploadRequest{FilePath: apkPath, SourceURL: sourceURL, ReleaseNotes: "notes"}
				ctx := context.Background()
				var off *offloadtest.Offloader
				if remote {
					req, ctx, off = offloadtest.Remote(t, ctx, req)
				}

				res := s.Upload(ctx, req)
				if !res.Success {
					t.Fatalf("Upload failed: %s", res.Error)
				}
				want := []string{"get-app-detail", "upload-by-url", "update-file-info", "update-language-info", "submit-audit"}
				if got := f.paths(); !reflect.DeepEqual(got, want) {
					t.Fatalf("request sequence = %v\nwant %v", got, want)
				}
				wantTask := jsonOf(t, map[string]any{"type": 1, "uploadList": []map[string]any{{
					"fileName":      "app.apk",
					"fileType":      fileTypeAPK,
					"fileSize":      len(apkData),
					"fileSha256":    sha256Hex(apkData),
					"fileUploadUrl": sourceURL,
				}}})
				if got := f.only("upload-by-url")[0].body; !reflect.DeepEqual(got, wantTask) {
					t.Errorf("upload-by-url body = %v\nwant %v", got, wantTask)
				}
				wantBind := jsonOf(t, map[string]any{"bindingFileList": []map[string]any{{"objectId": 1001}}})
				if got := f.only("update-file-info")[0].body; !reflect.DeepEqual(got, wantBind) {
					t.Errorf("update-file-info body = %v\nwant %v", got, wantBind)
				}
				if remote && len(off.Transfers()) != 0 {
					t.Errorf("%d transfers, want none (Honor pulls the URL)", len(off.Transfers()))
				}
			})
		}
	}
}
