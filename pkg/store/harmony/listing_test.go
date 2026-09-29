package harmony

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

func writePNG(t *testing.T, name string, w, h int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
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

// writeWebP writes a lossless-WebP header for a w×h image, padded to
// size bytes — enough for imgcheck.Inspect, which only decodes the header.
func writeWebP(t *testing.T, name string, w, h, size int) string {
	t.Helper()
	n := size - 20 // RIFF header (12) + VP8L chunk header (8)
	data := make([]byte, n)
	data[0] = 0x2f // VP8L signature
	binary.LittleEndian.PutUint32(data[1:], uint32(w-1)|uint32(h-1)<<14)
	b := append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(4+8+n))...)
	b = append(b, "WEBPVP8L"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(n))
	b = append(b, data...)
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func screenshots(t *testing.T, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = writePNG(t, "shot"+string(rune('1'+i))+".png", 1080, 1920)
	}
	return out
}

// assertJSON compares got with want after decoding both, so key order
// and whitespace don't matter.
func assertJSON(t *testing.T, got []byte, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("decode %s: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("body = %s\nwant   %s", got, want)
	}
}

func TestUploadWithListing(t *testing.T) {
	f := newFakeAGC(t)
	s := f.store()
	icon := writePNG(t, "icon.png", 216, 216)
	shots := screenshots(t, 3)

	res := s.Upload(context.Background(), &store.UploadRequest{
		FilePath:     writeApp(t, "demo.app"),
		PackageName:  "com.example.harmony",
		ReleaseNotes: "修复若干问题",
		Listing: &store.Listing{
			Brief:       "一句话简介",
			Description: "很长的应用描述",
			Icon:        icon,
			Screenshots: shots,
		},
	})
	if !res.Success {
		t.Fatalf("upload failed: %s (category %s)", res.Error, res.Category)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	want := []string{"appid-list", "upload-url", "obs-put", "app-package-info",
		"app-info", "app-language-info",
		"upload-url", "obs-put", // icon
		"upload-url", "obs-put", "upload-url", "obs-put", "upload-url", "obs-put", // screenshots
		"app-file-info", "compile-status", "app-submit"}
	if strings.Join(f.calls, ",") != strings.Join(want, ",") {
		t.Errorf("calls = %v\nwant    %v", f.calls, want)
	}
	wantLang := map[string]string{"lang": "zh-CN", "newFeatures": "修复若干问题", "briefInfo": "一句话简介", "appDesc": "很长的应用描述"}
	if !reflect.DeepEqual(f.langBody, wantLang) {
		t.Errorf("app-language-info body = %v, want %v", f.langBody, wantLang)
	}
	if f.fileInfoQuery["appId"] != "app-1" || f.fileInfoQuery["releaseType"] != "1" {
		t.Errorf("app-file-info query = %v", f.fileInfoQuery)
	}
	assertJSON(t, f.fileInfoBody, `{
		"appIconList": [{"lang": "zh-CN", "fileInfoList": [
			{"deviceType": 4, "objectIdList": ["CN/2026091101/icon.png"], "showType": 0}]}],
		"screenShotList": [{"lang": "zh-CN", "fileInfoList": [
			{"deviceType": 4, "objectIdList": ["CN/2026091101/shot1.png", "CN/2026091101/shot2.png", "CN/2026091101/shot3.png"], "showType": 0}]}]
	}`)
	iconBytes, _ := os.ReadFile(icon)
	if string(f.objects["CN/2026091101/icon.png"]) != string(iconBytes) {
		t.Error("icon bytes not uploaded to its objectId")
	}
	if q := f.uploadQueries[1]; q["fileName"] != "icon.png" || q["appId"] != "app-1" || q["sha256"] == "" {
		t.Errorf("icon upload-url query = %v", q)
	}
	if string(f.putBody) != "fake harmony app pack bytes" {
		t.Errorf("package bytes = %q", f.putBody)
	}
}

func TestUploadListingPartial(t *testing.T) {
	t.Run("text only, configured lang", func(t *testing.T) {
		f := newFakeAGC(t)
		s := f.store()
		s.lang = "en-US"
		res := s.Upload(context.Background(), &store.UploadRequest{
			FilePath: writeApp(t, "x.app"), PackageName: "com.example.harmony",
			Listing: &store.Listing{Brief: "Short intro"},
		})
		if !res.Success {
			t.Fatalf("upload failed: %s", res.Error)
		}
		want := "appid-list,upload-url,obs-put,app-package-info,app-language-info,compile-status,app-submit"
		if got := strings.Join(f.calls, ","); got != want {
			t.Errorf("calls = %s\nwant    %s", got, want)
		}
		if wantBody := map[string]string{"lang": "en-US", "briefInfo": "Short intro"}; !reflect.DeepEqual(f.langBody, wantBody) {
			t.Errorf("app-language-info body = %v, want %v", f.langBody, wantBody)
		}
	})

	t.Run("screenshots only", func(t *testing.T) {
		f := newFakeAGC(t)
		res := f.store().Upload(context.Background(), &store.UploadRequest{
			FilePath: writeApp(t, "x.app"), PackageName: "com.example.harmony",
			Listing: &store.Listing{Screenshots: screenshots(t, 3)},
		})
		if !res.Success {
			t.Fatalf("upload failed: %s", res.Error)
		}
		want := "appid-list,upload-url,obs-put,app-package-info,app-info," +
			"upload-url,obs-put,upload-url,obs-put,upload-url,obs-put,app-file-info,compile-status,app-submit"
		if got := strings.Join(f.calls, ","); got != want {
			t.Errorf("calls = %s\nwant    %s", got, want)
		}
		assertJSON(t, f.fileInfoBody, `{"screenShotList": [{"lang": "zh-CN", "fileInfoList": [
			{"deviceType": 4, "objectIdList": ["CN/2026091101/shot1.png", "CN/2026091101/shot2.png", "CN/2026091101/shot3.png"], "showType": 0}]}]}`)
	})

	t.Run("empty listing behaves like none", func(t *testing.T) {
		f := newFakeAGC(t)
		res := f.store().Upload(context.Background(), &store.UploadRequest{
			FilePath: writeApp(t, "x.app"), PackageName: "com.example.harmony",
			ReleaseNotes: "notes", Listing: &store.Listing{},
		})
		if !res.Success {
			t.Fatalf("upload failed: %s", res.Error)
		}
		want := "appid-list,upload-url,obs-put,app-package-info,app-info,app-language-info,compile-status,app-submit"
		if got := strings.Join(f.calls, ","); got != want {
			t.Errorf("calls = %s\nwant    %s", got, want)
		}
		if wantBody := map[string]string{"lang": "zh-CN", "newFeatures": "notes"}; !reflect.DeepEqual(f.langBody, wantBody) {
			t.Errorf("app-language-info body = %v, want %v", f.langBody, wantBody)
		}
	})
}

func TestUploadListingFailureBlocksSubmit(t *testing.T) {
	f := newFakeAGC(t)
	f.fileInfoCode = 204144660
	res := f.store().Upload(context.Background(), &store.UploadRequest{
		FilePath: writeApp(t, "x.app"), PackageName: "com.example.harmony",
		ReleaseNotes: "notes",
		Listing:      &store.Listing{Icon: writePNG(t, "icon.png", 1024, 1024)},
	})
	if res.Success {
		t.Fatal("upload should fail when app-file-info fails")
	}
	if !strings.Contains(res.Error, "app-file-info") || !strings.Contains(res.Error, consoleURL) {
		t.Errorf("error = %s", res.Error)
	}
	for _, c := range f.calls {
		if c == "compile-status" || c == "app-submit" {
			t.Errorf("%s called after a failed listing step: %v", c, f.calls)
		}
	}
}

func TestUploadNotesMustDifferFromListing(t *testing.T) {
	f := newFakeAGC(t)
	res := f.store().Upload(context.Background(), &store.UploadRequest{
		FilePath: writeApp(t, "x.app"), PackageName: "com.example.harmony",
		ReleaseNotes: "same text",
		Listing:      &store.Listing{Description: "same text"},
	})
	if res.Success || res.Category != store.CategoryConfigInvalid {
		t.Fatalf("got %+v", res)
	}
	if len(f.calls) != 0 {
		t.Errorf("no request should be made, got %v", f.calls)
	}
}

func TestListingSpec(t *testing.T) {
	if store.ListingSpecFor(StoreName) == nil {
		t.Fatal("harmony should declare a ListingSpec")
	}
	shots := screenshots(t, 3)
	cases := []struct {
		name    string
		listing store.Listing
		wantErr string // "" = valid
	}{
		{"valid png", store.Listing{Brief: "简介", Description: "描述", Icon: writePNG(t, "i.png", 216, 216), Screenshots: shots}, ""},
		{"valid 1024 icon", store.Listing{Icon: writePNG(t, "i.png", 1024, 1024)}, ""},
		{"brief equals description", store.Listing{Brief: "同样", Description: "同样"}, "must differ"},
		{"brief too long", store.Listing{Brief: strings.Repeat("字", 81)}, "at most 80"},
		{"icon wrong size", store.Listing{Icon: writePNG(t, "i.png", 512, 512)}, "want 216x216 or 1024x1024"},
		{"too few screenshots", store.Listing{Screenshots: shots[:2]}, "need at least 3"},
		{"landscape screenshot", store.Listing{Screenshots: append(shots[:2:2], writePNG(t, "l.png", 1920, 1080))}, "want 1080x1920"},
		{"small webp icon", store.Listing{Icon: writeWebP(t, "i.webp", 216, 216, 100<<10)}, ""},
		{"large webp icon", store.Listing{Icon: writeWebP(t, "i.webp", 216, 216, 100<<10+2)}, "webp 102402 bytes, at most 102400"},
		{"large webp screenshot", store.Listing{Screenshots: append(shots[:2:2], writeWebP(t, "s.webp", 1080, 1920, 200<<10+2))}, "listing: screenshots[2]: "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := store.ValidateListing(StoreName, &c.listing)
			if c.wantErr == "" {
				if len(errs) != 0 {
					t.Errorf("unexpected errors: %v", errs)
				}
				return
			}
			if len(errs) == 0 || !strings.Contains(errs[0].Error(), c.wantErr) {
				t.Errorf("errors = %v, want one containing %q", errs, c.wantErr)
			}
		})
	}
}
