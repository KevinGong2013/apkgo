package apkgo_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/KevinGong2013/apkgo/v4/pkg/apkgo"
	"github.com/KevinGong2013/apkgo/v4/pkg/config"
	"github.com/KevinGong2013/apkgo/v4/pkg/listing"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// lstImages is the image host the fake stores' listings point at; a test
// sets it before fetching.
var lstImages string

// Fake stores for the listing read, registered once (not in a test body,
// so -count=2 can't register twice):
//
//	lst-full   text + icon + 2 screenshots
//	lst-same   same icon bytes as lst-full under another URL, its own text
//	lst-text   text only, images unavailable (like tencent)
//	lst-broken second screenshot 404s, icon isn't an image
//	lst-down   the query itself fails
//	lst-none   has no fetcher
func init() {
	store.RegisterListingFetcher("lst-full", func(_ context.Context, cfg map[string]string, q store.ListingQuery) store.RemoteListing {
		return store.RemoteListing{
			Brief: "简介 " + q.Package, Description: "第一行\n第二行: 带冒号",
			Icon:        lstImages + "/icon.png",
			Screenshots: []string{lstImages + "/s1.png", lstImages + "/s2.png"},
		}
	})
	store.RegisterListingFetcher("lst-same", func(context.Context, map[string]string, store.ListingQuery) store.RemoteListing {
		return store.RemoteListing{Brief: "另一家的简介", Icon: lstImages + "/icon-copy.png"}
	})
	store.RegisterListingFetcher("lst-text", func(context.Context, map[string]string, store.ListingQuery) store.RemoteListing {
		return store.RemoteListing{Brief: "只有文字", Unavailable: []string{store.ListingIcon, store.ListingScreenshots}}
	})
	store.RegisterListingFetcher("lst-broken", func(context.Context, map[string]string, store.ListingQuery) store.RemoteListing {
		return store.RemoteListing{
			Description: "图片有问题",
			Icon:        lstImages + "/notimage",
			Screenshots: []string{lstImages + "/s1.png", lstImages + "/missing.png"},
		}
	})
	store.RegisterListingFetcher("lst-down", func(context.Context, map[string]string, store.ListingQuery) store.RemoteListing {
		return store.RemoteListing{Error: "auth failed"}
	})

	// lst-spec and lst-spec-shots declare a ListingSpec and hand back
	// things they wouldn't accept, as real stores do: a preview-sized icon
	// and an over-long brief; a screenshot set with one odd-sized image.
	spec := &store.ListingSpec{
		Brief:          store.TextSpec{Max: 5},
		Icon:           store.ImageSpec{Formats: []string{"png"}, Sizes: []store.Size{{Width: 512, Height: 512}}},
		Screenshot:     store.ImageSpec{Sizes: []store.Size{{Width: 108, Height: 192}}},
		MinScreenshots: 2,
	}
	for _, name := range []string{"lst-spec", "lst-spec-shots"} {
		store.Register(name, store.ConfigSchema{Name: name, Listing: spec},
			func(map[string]string) (store.Store, error) { return nil, nil })
	}
	store.RegisterListingFetcher("lst-spec", func(context.Context, map[string]string, store.ListingQuery) store.RemoteListing {
		return store.RemoteListing{
			Brief: "超过五个字的简介", Description: "描述",
			Icon:        lstImages + "/preview.png",
			Screenshots: []string{lstImages + "/s1.png", lstImages + "/s2.png"},
		}
	})
	store.RegisterListingFetcher("lst-spec-shots", func(context.Context, map[string]string, store.ListingQuery) store.RemoteListing {
		return store.RemoteListing{Screenshots: []string{lstImages + "/s1.png", lstImages + "/preview.png"}}
	})
}

func lstPNG(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, c)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func lstServe(t *testing.T) {
	t.Helper()
	files := map[string][]byte{
		"/icon.png":      lstPNG(t, 512, 512, color.White),
		"/icon-copy.png": lstPNG(t, 512, 512, color.White),
		"/s1.png":        lstPNG(t, 108, 192, color.RGBA{R: 1, A: 255}),
		"/s2.png":        lstPNG(t, 108, 192, color.RGBA{R: 2, A: 255}),
		"/preview.png":   lstPNG(t, 320, 320, color.Black),
		"/notimage":      []byte("<html>login</html>"),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(data)
	}))
	t.Cleanup(srv.Close)
	lstImages = srv.URL
}

func lstConfig(names ...string) *config.Config {
	c := &config.Config{Stores: map[string]map[string]string{}}
	for _, n := range names {
		c.Stores[n] = map[string]string{"key": "v"}
	}
	return c
}

// Without OutDir the report carries what each store said and nothing is
// written or downloaded.
func TestFetchListingReport(t *testing.T) {
	lstServe(t)
	rep, err := apkgo.FetchListing(context.Background(), apkgo.ListingJob{
		Config:  lstConfig("lst-full", "lst-text", "lst-down", "lst-none"),
		Package: "com.example.app",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Package != "com.example.app" || rep.File != "" || len(rep.Stores) != 4 {
		t.Fatalf("report = %+v", rep)
	}
	by := map[string]apkgo.ListingStoreResult{}
	for _, s := range rep.Stores {
		by[s.Store] = s
	}
	if s := by["lst-full"]; !s.Supported || s.Brief != "简介 com.example.app" || len(s.Screenshots) != 2 || s.Icon != lstImages+"/icon.png" {
		t.Errorf("lst-full = %+v", s)
	}
	if s := by["lst-text"]; len(s.Unavailable) != 2 || s.Brief != "只有文字" {
		t.Errorf("lst-text = %+v", s)
	}
	if s := by["lst-down"]; !s.Supported || s.Error != "auth failed" {
		t.Errorf("lst-down = %+v", s)
	}
	if s := by["lst-none"]; s.Supported {
		t.Errorf("lst-none = %+v, want unsupported", s)
	}
	if !rep.AnyFailed() {
		t.Error("AnyFailed() = false with a failing store")
	}

	if _, err := apkgo.FetchListing(context.Background(), apkgo.ListingJob{Config: lstConfig("lst-full")}); err == nil {
		t.Error("no package: want an error")
	}
	if _, err := apkgo.FetchListing(context.Background(), apkgo.ListingJob{Config: lstConfig("lst-full"), Package: "p", Stores: []string{"nope"}}); err == nil {
		t.Error("no matching store: want an error")
	}
}

// With OutDir the images are downloaded once per distinct content and the
// listing file holds each store under `stores:` — and that file is one
// `apkgo upload --listing` accepts.
func TestFetchListingSave(t *testing.T) {
	lstServe(t)
	dir := filepath.Join(t.TempDir(), "out")
	job := apkgo.ListingJob{
		Config:  lstConfig("lst-full", "lst-same", "lst-text", "lst-broken", "lst-down", "lst-none"),
		Package: "com.example.app",
		OutDir:  dir,
	}
	rep, err := apkgo.FetchListing(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if rep.File != filepath.Join(dir, "listing.yaml") {
		t.Fatalf("File = %q", rep.File)
	}

	f, err := listing.Load(rep.File)
	if err != nil {
		t.Fatalf("written file doesn't load: %v", err)
	}
	if f.Brief != "" || f.Description != "" || f.Icon != "" || len(f.Screenshots) != 0 {
		t.Errorf("defaults = %+v, want none (every store sits under stores:)", f.Fields)
	}
	if len(f.Stores) != 4 {
		t.Fatalf("stores = %v, want lst-full, lst-same, lst-text, lst-broken", f.Stores)
	}

	full := f.Stores["lst-full"]
	if full.Brief != "简介 com.example.app" || full.Description != "第一行\n第二行: 带冒号" || len(full.Screenshots) != 2 {
		t.Errorf("lst-full = %+v", full)
	}
	if want := filepath.Join(dir, "assets", "icon-1.png"); full.Icon != want {
		t.Errorf("lst-full icon = %q, want %q", full.Icon, want)
	}
	if want := []string{filepath.Join(dir, "assets", "screenshot-1.png"), filepath.Join(dir, "assets", "screenshot-2.png")}; !reflect.DeepEqual(full.Screenshots, want) {
		t.Errorf("lst-full screenshots = %v, want %v", full.Screenshots, want)
	}
	for _, p := range append([]string{full.Icon}, full.Screenshots...) {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("image not saved: %v", err)
		}
	}
	// Same bytes under another URL reuse the file already written.
	if same := f.Stores["lst-same"]; same.Icon != full.Icon || same.Brief != "另一家的简介" {
		t.Errorf("lst-same = %+v, want the icon file of lst-full", same)
	}
	if text := f.Stores["lst-text"]; text.Brief != "只有文字" || text.Icon != "" || text.Screenshots != nil {
		t.Errorf("lst-text = %+v", text)
	}
	// A failed download keeps the text, drops the images it couldn't get
	// (no partial screenshot set) and says so.
	if broken := f.Stores["lst-broken"]; broken.Description != "图片有问题" || broken.Icon != "" || broken.Screenshots != nil {
		t.Errorf("lst-broken = %+v", broken)
	}
	for _, s := range rep.Stores {
		if s.Store == "lst-broken" && (!strings.Contains(s.Error, "download icon") || !strings.Contains(s.Error, "download screenshot 2: http 404")) {
			t.Errorf("lst-broken error = %q", s.Error)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "assets"))
	if len(entries) != 3 {
		t.Errorf("%d files in assets, want icon + 2 screenshots", len(entries))
	}

	// What a store gets on upload is its own entry; one that couldn't be
	// read gets nothing.
	if l := f.Resolve("lst-full"); l == nil || l.Icon != full.Icon {
		t.Errorf("Resolve(lst-full) = %+v", l)
	}
	if l := f.Resolve("lst-down"); l != nil {
		t.Errorf("Resolve(lst-down) = %+v, want nil", l)
	}

	// A second run must not overwrite the (possibly edited) file.
	if _, err := apkgo.FetchListing(context.Background(), job); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("second run: err = %v", err)
	}
}

// What a store returns but wouldn't accept back is handled per kind: an
// image that fails the store's own spec (a preview, not the original) is
// left out so the store keeps what it has; text is written with a note.
func TestFetchListingSaveChecksSpec(t *testing.T) {
	lstServe(t)
	dir := filepath.Join(t.TempDir(), "out")
	cfg := lstConfig("lst-spec")
	cfg.Stores["lst-spec.b"] = map[string]string{"key": "v"} // an instance of the same type
	rep, err := apkgo.FetchListing(context.Background(), apkgo.ListingJob{Config: cfg, Package: "com.example.app", OutDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	f, err := listing.Load(rep.File)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range rep.Stores {
		got := f.Stores[s.Store]
		if got.Brief != "超过五个字的简介" || got.Description != "描述" || got.Icon != "" || len(got.Screenshots) != 2 {
			t.Errorf("%s in file = %+v, want text and screenshots but no icon", s.Store, got)
		}
		if s.Error != "" || len(s.Notes) != 2 {
			t.Fatalf("%s: error %q, notes %q; want 2 notes", s.Store, s.Error, s.Notes)
		}
		if n := s.Notes[0]; !strings.HasPrefix(n, "icon left out") || !strings.Contains(n, "assets/icon-1.png: size 320x320, want 512x512") {
			t.Errorf("%s icon note = %q", s.Store, n)
		}
		if n := s.Notes[1]; !strings.HasPrefix(n, "text needs changing") || !strings.Contains(n, "at most 5 allowed") {
			t.Errorf("%s text note = %q", s.Store, n)
		}
	}
	// The rejected download stays on disk for reference.
	if _, err := os.Stat(filepath.Join(dir, "assets", "icon-1.png")); err != nil {
		t.Errorf("preview icon not kept: %v", err)
	}
}

// One unfit screenshot drops the whole set rather than leaving a partial one.
func TestFetchListingSaveUnfitScreenshots(t *testing.T) {
	lstServe(t)
	rep, err := apkgo.FetchListing(context.Background(), apkgo.ListingJob{
		Config: lstConfig("lst-spec-shots"), Package: "p", OutDir: filepath.Join(t.TempDir(), "out"),
	})
	if err != nil {
		t.Fatal(err)
	}
	f, err := listing.Load(rep.File)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Stores) != 0 {
		t.Errorf("stores = %v, want none: the only field was unfit", f.Stores)
	}
	if notes := rep.Stores[0].Notes; len(notes) != 1 || !strings.HasPrefix(notes[0], "screenshots left out") {
		t.Errorf("notes = %q", notes)
	}
}
