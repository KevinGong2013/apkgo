package tencent

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/KevinGong2013/apkgo/v4/pkg/internal/offloadtest"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// nextDataFixture mirrors the shape of 应用宝's __NEXT_DATA__ blob: the props
// tree embeds the target app plus unrelated recommendation records, and a stub
// reference to the same package that carries no version_name.
const nextDataFixture = `{
  "props": {
    "pageProps": {
      "appInfo": {
        "pkg_name": "com.example.app",
        "version_name": "10.12.2",
        "app_id": "52463570"
      },
      "recommends": [
        {"pkg_name": "com.example.other", "version_name": "9.2.3"},
        {"pkg_name": "com.example.app", "version_name": ""}
      ]
    }
  }
}`

func TestFindVersionName(t *testing.T) {
	var data any
	if err := json.Unmarshal([]byte(nextDataFixture), &data); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	if got := findVersionName(data, "com.example.app"); got != "10.12.2" {
		t.Errorf("com.example.app: got %q, want %q (stub with empty version_name must be skipped)", got, "10.12.2")
	}
	if got := findVersionName(data, "com.example.other"); got != "9.2.3" {
		t.Errorf("com.example.other: got %q, want %q", got, "9.2.3")
	}
	if got := findVersionName(data, "com.absent.pkg"); got != "" {
		t.Errorf("absent package: got %q, want empty", got)
	}
}

func TestNextDataRe(t *testing.T) {
	html := `<html><head></head><body>` +
		`<script id="__NEXT_DATA__" type="application/json" crossorigin="anonymous">{"a":1}</script>` +
		`</body></html>`
	m := nextDataRe.FindStringSubmatch(html)
	if m == nil {
		t.Fatal("regex did not match the __NEXT_DATA__ script tag")
	}
	if m[1] != `{"a":1}` {
		t.Errorf("captured %q, want %q", m[1], `{"a":1}`)
	}
}

// writeZip writes a zip holding the named (empty) entries to dir/name —
// enough of an APK for apk.ABIs — and returns its path.
func writeZip(t *testing.T, dir, name string, entries ...string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		if _, err := zw.Create(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func md5Of(t *testing.T, path string) string {
	sum := md5.Sum(readFile(t, path))
	return hex.EncodeToString(sum[:])
}

// byteCounter is a progress.Reporter keeping the declared total and the
// bytes reported sent.
type byteCounter struct{ total, sent atomic.Int64 }

func (c *byteCounter) Phase(string)  {}
func (c *byteCounter) Total(n int64) { c.total.Store(n) }
func (c *byteCounter) Add(n int64)   { c.sent.Add(n) }

// The packages reach COS byte for byte under their own names, update_app
// carries their md5s and the flags for their architecture, and progress
// counts their bytes — the same whether the files are here or only their
// facts and an Offloader are (offloadtest.Remote). The 64-bit-only branch
// is picked from the ABIs, which the remote run has only as a fact.
func TestUploadSendsThePackages(t *testing.T) {
	dir := t.TempDir()
	universal := filepath.Join(dir, "app.apk")
	if err := os.WriteFile(universal, []byte("not a real apk"), 0o644); err != nil {
		t.Fatal(err)
	}
	arm64 := writeZip(t, dir, "arm64.apk", "lib/arm64-v8a/libx.so")
	arm32 := writeZip(t, dir, "arm32.apk", "lib/armeabi-v7a/libx.so")
	icon := writePNG(t, "icon.png", 512, 512)

	cases := []struct {
		name         string
		file, file64 string
		want         map[string]string // update_app fields; "" = absent
	}{
		{"universal", universal, "", map[string]string{
			"apk32_flag": "1", "apk32_file_serial_number": "apk-1", "apk32_file_md5": md5Of(t, universal),
			"apk64_flag": "", "apk64_file_serial_number": "",
		}},
		{"64-bit only", arm64, "", map[string]string{
			"apk32_flag": "2", "apk32_file_serial_number": "",
			"apk64_flag": "1", "apk64_file_serial_number": "apk-1", "apk64_file_md5": md5Of(t, arm64),
		}},
		{"split", arm32, arm64, map[string]string{
			"apk32_flag": "1", "apk32_file_serial_number": "apk-1", "apk32_file_md5": md5Of(t, arm32),
			"apk64_flag": "1", "apk64_file_serial_number": "apk-2", "apk64_file_md5": md5Of(t, arm64),
		}},
	}
	for _, c := range cases {
		for _, remote := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/remote=%v", c.name, remote), func(t *testing.T) {
				f := newFakeTencent(t)
				rep := &byteCounter{}
				req := &store.UploadRequest{
					FilePath:    c.file,
					File64Path:  c.file64,
					PackageName: "com.example.app",
					Progress:    rep,
					Listing:     &store.Listing{Icon: icon},
				}
				ctx := context.Background()
				var off *offloadtest.Offloader
				if remote {
					req, ctx, off = offloadtest.Remote(t, ctx, req)
				}
				if res := f.store(t).Upload(ctx, req); !res.Success {
					t.Fatalf("upload failed: %s", res.Error)
				}

				for k, v := range c.want {
					if got := f.update.Get(k); got != v {
						t.Errorf("update_app %s = %q, want %q", k, got, v)
					}
				}
				var size int64
				for i, p := range []string{c.file, c.file64} {
					if p == "" {
						continue
					}
					serial := fmt.Sprintf("apk-%d", i+1)
					data := readFile(t, p)
					size += int64(len(data))
					if !bytes.Equal(f.puts[serial], data) {
						t.Errorf("%s: COS got %d bytes, want %s's %d", serial, len(f.puts[serial]), p, len(data))
					}
					if want := "info:apk:" + filepath.Base(p); f.events[2*i] != want {
						t.Errorf("event %d = %q, want %q", 2*i, f.events[2*i], want)
					}
				}
				if serial := f.update.Get("icon_file_serial_number"); serial == "" || !bytes.Equal(f.puts[serial], readFile(t, icon)) {
					t.Errorf("icon (serial %q) not uploaded", serial)
				}
				if rep.total.Load() != size || rep.sent.Load() != size {
					t.Errorf("progress: total %d, sent %d; want %d", rep.total.Load(), rep.sent.Load(), size)
				}
				if remote {
					for _, p := range []string{req.FilePath, req.File64Path} {
						if p != "" && !off.Sent(p) {
							t.Errorf("%s did not go out as a transfer", p)
						}
					}
				}
			})
		}
	}
}
