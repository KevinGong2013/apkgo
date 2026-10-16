package apk

import (
	"archive/zip"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
)

// countingReaderAt counts the bytes read through it.
type countingReaderAt struct {
	f *os.File
	n atomic.Int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.f.ReadAt(p, off)
	c.n.Add(int64(n))
	return n, err
}

func openAt(t *testing.T, path string) (*countingReaderAt, int64) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	return &countingReaderAt{f: f}, fi.Size()
}

// The ReaderAt entry points give what the path ones do: they are what a
// service uses to read a package in object storage by range.
func TestAtVariantsMatchPathVariants(t *testing.T) {
	path := filepath.Join("..", "store", "xiaomi", "testdata", "helloworld.apk")

	want, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	r, size := openAt(t, path)
	got, err := ParseAt(r, size)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseAt = %+v, Parse = %+v", got, want)
	}

	wantABIs, err := ABIs(path)
	if err != nil {
		t.Fatal(err)
	}
	gotABIs, err := ABIsAt(r, size)
	if err != nil || !reflect.DeepEqual(gotABIs, wantABIs) {
		t.Errorf("ABIsAt = %v, %v; ABIs = %v", gotABIs, err, wantABIs)
	}

	img, err := LauncherIconAt(r, size)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 192 || b.Dy() != 192 {
		t.Errorf("launcher icon %dx%d, want the densest (192x192)", b.Dx(), b.Dy())
	}

	dest := filepath.Join(t.TempDir(), "icon.png")
	if err := WriteLauncherIconPNG(r, size, dest); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(dest); err != nil || fi.Size() == 0 {
		t.Errorf("icon PNG not written: %v", err)
	}
}

func TestParseHarmonyAtMatchesParseHarmony(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.app")
	writeZipEntries(t, path, map[string][]byte{"pack.info": []byte(testPackInfo)}, zip.Deflate)
	want, err := ParseHarmony(path)
	if err != nil {
		t.Fatal(err)
	}
	r, size := openAt(t, path)
	got, err := ParseHarmonyAt(r, size, "demo.app")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("ParseHarmonyAt = %+v, %v; ParseHarmony = %+v", got, err, want)
	}
}
