package store_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"

	"github.com/KevinGong2013/apkgo/v4/pkg/imgcheck"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// test-conform takes images of 16×16 only: a PNG icon, and screenshots
// as PNG under 100 bytes (more than a 16×16 gradient needs) or JPEG.
// test-conform-jpeg takes JPEG screenshots only.
func init() {
	store.Register("test-conform", store.ConfigSchema{
		Name: "test-conform",
		Listing: &store.ListingSpec{
			Icon: store.ImageSpec{Formats: []string{"png"}, Sizes: []store.Size{{Width: 16, Height: 16}}, MaxBytes: 1 << 20},
			Screenshot: store.ImageSpec{
				Formats: []string{"png", "jpeg"}, Sizes: []store.Size{{Width: 16, Height: 16}},
				MaxBytesByFormat: map[string]int64{"png": 100},
			},
		},
	}, func(map[string]string) (store.Store, error) { return nil, nil })
	store.Register("test-conform-jpeg", store.ConfigSchema{
		Name:    "test-conform-jpeg",
		Listing: &store.ListingSpec{Screenshot: store.ImageSpec{Formats: []string{"jpeg"}}},
	}, func(map[string]string) (store.Store, error) { return nil, nil })
}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func inspect(t *testing.T, data []byte) imgcheck.Info {
	t.Helper()
	info, err := imgcheck.InspectBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// A WEBP the store serves but won't take back becomes an accepted format,
// PNG first, JPEG when PNG is allowed but too large.
func TestConformListingImageConverts(t *testing.T) {
	webp := readTestdata(t, "opaque-16x16.webp")

	out, converted := store.ConformListingImage("test-conform", store.ListingIcon, webp)
	if info := inspect(t, out); !converted || info.Format != "png" || info.Width != 16 || info.Height != 16 {
		t.Errorf("icon: converted %v, got %+v, want a 16x16 png", converted, info)
	}

	out, converted = store.ConformListingImage("test-conform", store.ListingScreenshots, webp)
	if info := inspect(t, out); !converted || info.Format != "jpeg" {
		t.Errorf("screenshot: converted %v, got %+v, want a jpeg (png is over its byte limit)", converted, info)
	}
	// The result passes the same validation an upload runs.
	path := t.TempDir() + "/shot.jpg"
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	if errs := store.ValidateListing("test-conform", &store.Listing{Screenshots: []string{path}}); len(errs) != 0 {
		t.Errorf("converted screenshot fails validation: %v", errs)
	}
}

// JPEG has no alpha: transparent pixels end up white, not black.
func TestConformListingImageFlattensAlpha(t *testing.T) {
	out, converted := store.ConformListingImage("test-conform-jpeg", store.ListingScreenshots, readTestdata(t, "alpha-16x16.webp"))
	if !converted {
		t.Fatal("not converted")
	}
	img, format, err := image.Decode(bytes.NewReader(out))
	if err != nil || format != "jpeg" {
		t.Fatalf("decode: %v, format %q", err, format)
	}
	// Left half is transparent in the source, right half opaque red.
	if r, g, b, _ := img.At(2, 8).RGBA(); r>>8 < 240 || g>>8 < 240 || b>>8 < 240 {
		t.Errorf("transparent pixel became %d,%d,%d, want white", r>>8, g>>8, b>>8)
	}
	if r, g, b, _ := img.At(13, 8).RGBA(); r>>8 < 200 || g>>8 > 60 || b>>8 > 60 {
		t.Errorf("red pixel became %d,%d,%d", r>>8, g>>8, b>>8)
	}
}

// Anything conversion can't fix, or that needs no fixing, comes back as is.
func TestConformListingImageUnchanged(t *testing.T) {
	webp := readTestdata(t, "opaque-16x16.webp")
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	img.Set(0, 0, color.Black)
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	fits := buf.Bytes()

	for _, tt := range []struct {
		name, store, field string
		data               []byte
	}{
		{"already fits", "test-conform", store.ListingIcon, fits},
		{"wrong pixel size", "test-listing", store.ListingIcon, webp}, // wants 512×512
		{"store without a spec", "test-nolisting", store.ListingIcon, webp},
		{"unknown store", "nope", store.ListingIcon, webp},
		{"not an image field", "test-conform", store.ListingBrief, webp},
		{"not an image", "test-conform", store.ListingIcon, []byte("<html>login</html>")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out, converted := store.ConformListingImage(tt.store, tt.field, tt.data)
			if converted || !bytes.Equal(out, tt.data) {
				t.Errorf("converted %v, %d bytes; want the input back unchanged", converted, len(out))
			}
		})
	}
}
