package store_test

import (
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KevinGong2013/apkgo/v4/pkg/imgcheck"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// checkSaw is set by test-listing-check-inspect's Check.
var checkSaw string

func init() {
	store.Register("test-listing", store.ConfigSchema{
		Name: "test-listing",
		Listing: &store.ListingSpec{
			Brief:          store.TextSpec{Min: 5, Max: 15},
			Description:    store.TextSpec{Max: 20, Unit: store.UnitBytes},
			Icon:           store.ImageSpec{Formats: []string{"png"}, Sizes: []store.Size{{Width: 512, Height: 512}}},
			Screenshot:     store.ImageSpec{Formats: []string{"png", "jpeg"}, MinEdge: 320, MaxEdge: 3840},
			MinScreenshots: 2,
			MaxScreenshots: 3,
			Check: func(l *store.Listing, _ store.ImageInspector) []error {
				if strings.Contains(l.Brief, "!") {
					return []error{errors.New("brief must not contain punctuation")}
				}
				return nil
			},
		},
	}, func(map[string]string) (store.Store, error) { return nil, nil })
	store.Register("test-nolisting", store.ConfigSchema{Name: "test-nolisting"},
		func(map[string]string) (store.Store, error) { return nil, nil })
	store.Register("test-listing-aspect", store.ConfigSchema{
		Name: "test-listing-aspect",
		Listing: &store.ListingSpec{Screenshot: store.ImageSpec{
			MinWidth: 1080, MinHeight: 1920, Aspect: &store.Size{Width: 9, Height: 16},
		}},
	}, func(map[string]string) (store.Store, error) { return nil, nil })
	store.Register("test-listing-maxaspect", store.ConfigSchema{
		Name: "test-listing-maxaspect",
		Listing: &store.ListingSpec{Screenshot: store.ImageSpec{
			MaxAspect: &store.Size{Width: 2, Height: 1}, MaxBytes: 1 << 20, MaxBytesByFormat: map[string]int64{"jpeg": 100},
		}},
	}, func(map[string]string) (store.Store, error) { return nil, nil })
	// Its Check records what the injected inspector answered for the icon.
	store.Register("test-listing-check-inspect", store.ConfigSchema{
		Name: "test-listing-check-inspect",
		Listing: &store.ListingSpec{Check: func(l *store.Listing, in store.ImageInspector) []error {
			info, err := in(l.Icon)
			checkSaw = fmt.Sprintf("%v %v", info.Width, err)
			return nil
		}},
	}, func(map[string]string) (store.Store, error) { return nil, nil })
	// An unconstrained spec, like the script store's.
	store.Register("test-listing-any", store.ConfigSchema{Name: "test-listing-any", Listing: &store.ListingSpec{}},
		func(map[string]string) (store.Store, error) { return nil, nil })
}

func writeImage(t *testing.T, name string, w, h int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if strings.HasSuffix(name, ".jpg") {
		err = jpeg.Encode(f, img, nil)
	} else {
		err = png.Encode(f, img)
	}
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTextLength(t *testing.T) {
	cases := []struct {
		s, unit string
		want    int
	}{
		{"一句话介绍", store.UnitChars, 5},
		{"一句话介绍", store.UnitBytes, 15},
		{"一句话介绍", store.UnitWidth, 10},
		{"abc，中文", store.UnitWidth, 3 + 2 + 4},
		{"abc", "", 3},
	}
	for _, c := range cases {
		if got := store.TextLength(c.s, c.unit); got != c.want {
			t.Errorf("TextLength(%q, %q) = %d, want %d", c.s, c.unit, got, c.want)
		}
	}
}

func TestValidateListingOK(t *testing.T) {
	l := &store.Listing{
		Brief:       "一句话介绍",
		Description: "short",
		Icon:        writeImage(t, "icon.png", 512, 512),
		Screenshots: []string{writeImage(t, "s1.png", 1080, 1920), writeImage(t, "s2.jpg", 1080, 1920)},
	}
	if errs := store.ValidateListing("test-listing", l); len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	// Instance names resolve to the type's spec.
	if errs := store.ValidateListing("test-listing.cn", l); len(errs) > 0 {
		t.Fatalf("instance: unexpected errors: %v", errs)
	}
}

func TestValidateListingReportsEveryProblem(t *testing.T) {
	l := &store.Listing{
		Brief:       "太短!",
		Description: "描述超过二十个字节了",
		Icon:        writeImage(t, "icon.jpg", 512, 512),
		Screenshots: []string{writeImage(t, "s1.png", 200, 400)},
	}
	errs := store.ValidateListing("test-listing", l)
	joined := errors.Join(errs...).Error()
	for _, want := range []string{
		"test-listing: listing brief: length 3 chars, need at least 5",
		"listing description: length 30 bytes, at most 20 allowed",
		"listing icon:", "format jpeg, want png",
		"listing screenshots: got 1, need at least 2",
		"listing screenshots[0]:", "edges must be at least 320px",
		"brief must not contain punctuation",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("errors missing %q:\n%s", want, joined)
		}
	}
	if len(errs) != 6 {
		t.Errorf("got %d errors, want 6:\n%s", len(errs), joined)
	}
}

func TestValidateListingMinSizeAndAspect(t *testing.T) {
	for _, c := range []struct {
		w, h int
		want string // "" = valid
	}{
		{1080, 1920, ""},
		{1440, 2560, ""},
		{720, 1280, "need at least 1080x1920"},
		{1200, 1920, "aspect ratio must be 9:16"},
		{1920, 1080, "need at least 1080x1920"},
	} {
		path := writeImage(t, fmt.Sprintf("s-%dx%d.png", c.w, c.h), c.w, c.h)
		errs := store.ValidateListing("test-listing-aspect", &store.Listing{Screenshots: []string{path}})
		got := errors.Join(errs...)
		switch {
		case c.want == "" && got != nil:
			t.Errorf("%dx%d: unexpected %v", c.w, c.h, got)
		case c.want != "" && (got == nil || !strings.Contains(got.Error(), c.want)):
			t.Errorf("%dx%d: got %v, want %q", c.w, c.h, got, c.want)
		}
	}
}

func TestValidateListingMaxAspectAndBytesByFormat(t *testing.T) {
	for _, c := range []struct {
		name string
		w, h int
		want string // "" = valid
	}{
		{"s-1080x1920.png", 1080, 1920, ""},
		{"s-400x800.png", 400, 800, ""}, // exactly 2:1
		{"s-1000x320.png", 1000, 320, "aspect ratio must be at most 2:1"},
		{"s-1080x1920.jpg", 1080, 1920, "jpeg"}, // jpeg cap is 100 bytes
	} {
		path := writeImage(t, c.name, c.w, c.h)
		got := errors.Join(store.ValidateListing("test-listing-maxaspect", &store.Listing{Screenshots: []string{path}})...)
		switch {
		case c.want == "" && got != nil:
			t.Errorf("%s: unexpected %v", c.name, got)
		case c.want != "" && (got == nil || !strings.Contains(got.Error(), c.want)):
			t.Errorf("%s: got %v, want %q", c.name, got, c.want)
		}
	}
}

// An unconstrained ImageSpec (the script store) accepts any existing file
// and still reports a missing one.
func TestValidateListingUnconstrainedImages(t *testing.T) {
	svg := filepath.Join(t.TempDir(), "logo.svg")
	if err := os.WriteFile(svg, []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if errs := store.ValidateListing("test-listing-any", &store.Listing{Icon: svg, Screenshots: []string{svg}}); len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	errs := store.ValidateListing("test-listing-any", &store.Listing{Icon: filepath.Join(t.TempDir(), "nope.svg")})
	if len(errs) != 1 || !errors.Is(errs[0], os.ErrNotExist) {
		t.Errorf("errs = %v, want one not-exist error", errs)
	}
}

// ValidateListingWith reads image headers through the inspector: paths
// can be anything it understands (object keys here), and nothing is read
// from disk — including by the store's Check.
func TestValidateListingWithInspector(t *testing.T) {
	meta := map[string]imgcheck.Info{
		"k/icon":  {Format: "png", Width: 512, Height: 512, Bytes: 1000},
		"k/shot1": {Format: "jpeg", Width: 1080, Height: 1920, Bytes: 1000},
		"k/shot2": {Format: "png", Width: 1080, Height: 1920, Bytes: 1000},
		"k/tiny":  {Format: "png", Width: 100, Height: 100, Bytes: 10},
	}
	var seen []string
	inspect := func(key string) (imgcheck.Info, error) {
		seen = append(seen, key)
		info, ok := meta[key]
		if !ok {
			return imgcheck.Info{}, fmt.Errorf("%s: unknown asset", key)
		}
		return info, nil
	}

	ok := &store.Listing{Brief: "一句话介绍", Icon: "k/icon", Screenshots: []string{"k/shot1", "k/shot2"}}
	if errs := store.ValidateListingWith("test-listing", ok, inspect); len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(seen) == 0 {
		t.Fatal("inspector was not used")
	}

	bad := &store.Listing{Icon: "k/tiny", Screenshots: []string{"k/shot1", "k/missing"}}
	joined := errors.Join(store.ValidateListingWith("test-listing", bad, inspect)...).Error()
	for _, want := range []string{"size 100x100, want 512x512", "k/missing: unknown asset"} {
		if !strings.Contains(joined, want) {
			t.Errorf("errors missing %q:\n%s", want, joined)
		}
	}

	// The store's Check gets the same inspector.
	checkSaw = ""
	store.ValidateListingWith("test-listing-check-inspect", &store.Listing{Icon: "k/icon"}, inspect)
	if checkSaw != "512 <nil>" {
		t.Errorf("Check saw %q, want the injected inspector's answer", checkSaw)
	}
}

func TestValidateListingMissingFile(t *testing.T) {
	errs := store.ValidateListing("test-listing", &store.Listing{Icon: filepath.Join(t.TempDir(), "nope.png")})
	if len(errs) != 1 || !errors.Is(errs[0], os.ErrNotExist) {
		t.Fatalf("errs = %v, want one not-exist error", errs)
	}
}

func TestValidateListingUnsupportedAndEmpty(t *testing.T) {
	if errs := store.ValidateListing("test-nolisting", nil); errs != nil {
		t.Errorf("nil listing: %v", errs)
	}
	if errs := store.ValidateListing("test-nolisting", &store.Listing{}); errs != nil {
		t.Errorf("empty listing: %v", errs)
	}
	errs := store.ValidateListing("test-nolisting", &store.Listing{Brief: "x"})
	if len(errs) != 1 || !errors.Is(errs[0], store.ErrListingUnsupported) {
		t.Errorf("errs = %v, want ErrListingUnsupported", errs)
	}
	if store.ListingSpecFor("test-nolisting") != nil || store.ListingSpecFor("unknown") != nil {
		t.Error("ListingSpecFor should be nil")
	}
}

func TestListingResult(t *testing.T) {
	req := &store.UploadRequest{Listing: &store.Listing{Brief: "b", Screenshots: []string{"a.png"}}}
	ok := &store.UploadResult{Success: true}
	if got := strings.Join(store.ListingResult(req, ok), ","); got != "brief,screenshots" {
		t.Errorf("success: %q", got)
	}
	if got := store.ListingResult(req, &store.UploadResult{}); got != nil {
		t.Errorf("failure: %v", got)
	}
	if got := store.ListingResult(req, &store.UploadResult{Success: true, Category: store.CategoryAlreadyDone}); got != nil {
		t.Errorf("already done: %v", got)
	}
	if got := store.ListingResult(&store.UploadRequest{}, ok); got != nil {
		t.Errorf("no listing: %v", got)
	}
}
