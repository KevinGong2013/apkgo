package imgcheck_test

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/KevinGong2013/apkgo/v4/pkg/imgcheck"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// InspectBytes and Inspect agree on the same image.
func TestInspectBytesMatchesInspect(t *testing.T) {
	data := pngBytes(t, 1080, 1920)
	path := filepath.Join(t.TempDir(), "s.png")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	fromFile, err := imgcheck.Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	fromBytes, err := imgcheck.InspectBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	want := imgcheck.Info{Format: "png", Width: 1080, Height: 1920, Bytes: int64(len(data))}
	if fromFile != want || fromBytes != want {
		t.Errorf("Inspect = %+v, InspectBytes = %+v, want %+v", fromFile, fromBytes, want)
	}
	if _, err := imgcheck.InspectBytes([]byte("<svg/>")); err == nil {
		t.Error("InspectBytes accepted a non-image")
	}
}
