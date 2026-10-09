package meizu

import (
	"bytes"
	"image"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// fixtureAPK is xiaomi's helloworld.apk (from shogo82148/androidbinary's
// MIT-licensed testdata). It ships mdpi..xxxhdpi launcher icons
// (48..192px).
var fixtureAPK = filepath.Join("..", "xiaomi", "testdata", "helloworld.apk")

// TestUploadFillsMissingIcon: when app/detail returns no icon, the APK's
// launcher icon is uploaded as a 512×512 PNG ahead of the APK and its
// destFileName is what publish gets. The echoed empty icon used to come
// back as "[113001] 应用ICON不能为空".
func TestUploadFillsMissingIcon(t *testing.T) {
	f := &fakeMeizu{t: t, status: statusOnSale, noIcon: true}
	res := runUploadAPK(t, f, fixtureAPK, nil)
	if !res.Success {
		t.Fatalf("upload failed: %s", res.Error)
	}
	wantCalls := []string{listURI, detailURI, imageUploadURI, apkUploadURI, publishURI}
	if !slices.Equal(f.calls, wantCalls) {
		t.Errorf("calls = %v, want %v", f.calls, wantCalls)
	}
	icon, _ := f.submitBody["icon"].(string)
	if !strings.HasPrefix(icon, "img/apkgo_meizu_icon_") || !strings.HasSuffix(icon, ".png") {
		t.Errorf("published icon = %q, want the uploaded launcher icon's destFileName", icon)
	}
	if len(f.images) != 1 {
		t.Fatalf("uploaded %d images, want 1", len(f.images))
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(f.images[0]))
	if err != nil {
		t.Fatalf("decode uploaded icon: %v", err)
	}
	if format != "png" || cfg.Width != iconSize || cfg.Height != iconSize {
		t.Errorf("uploaded icon is %s %dx%d, want png %dx%d", format, cfg.Width, cfg.Height, iconSize, iconSize)
	}
}

// TestReadIconPicksDensestLauncherIcon: the icon must come from the
// densest variant the APK ships. Requesting density 0 resolves to mdpi,
// which would upscale a 48px icon into a blurry 512px one.
func TestReadIconPicksDensestLauncherIcon(t *testing.T) {
	img, err := readIcon(fixtureAPK)
	if err != nil {
		t.Fatalf("readIcon: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 192 || b.Dy() != 192 {
		t.Errorf("read a %dx%d icon, want 192x192 (xxxhdpi)", b.Dx(), b.Dy())
	}
}

// TestUploadMissingIconListingWins: a listing icon is the user's own
// choice, so the APK is not consulted — runUpload's placeholder APK has
// no icon to read and the upload still succeeds.
func TestUploadMissingIconListingWins(t *testing.T) {
	dir := t.TempDir()
	f := &fakeMeizu{t: t, status: statusOnSale, noIcon: true}
	res := runUpload(t, f, dir, &store.Listing{Icon: writePNG(t, dir, "icon.png")})
	if !res.Success {
		t.Fatalf("upload failed: %s", res.Error)
	}
	wantCalls := []string{listURI, detailURI, apkUploadURI, imageUploadURI, publishURI}
	if !slices.Equal(f.calls, wantCalls) {
		t.Errorf("calls = %v, want %v", f.calls, wantCalls)
	}
	if got := f.submitBody["icon"]; got != "img/icon.png" {
		t.Errorf("published icon = %v, want img/icon.png", got)
	}
}

// TestUploadMissingIconFailsEarly: with no icon on Meizu's side and none
// to be had from the APK or uploaded, the store fails before the APK
// upload and says where to supply one.
func TestUploadMissingIconFailsEarly(t *testing.T) {
	cases := []struct {
		name      string
		failImage bool
		apk       string // "" = runUpload's placeholder, which has no icon
		wantErr   string
		wantCalls []string
	}{
		{
			name:      "apk has no readable icon",
			wantErr:   "魅族开发者中心",
			wantCalls: []string{listURI, detailURI},
		},
		{
			name:      "icon upload refused",
			failImage: true,
			apk:       fixtureAPK,
			wantErr:   "113007",
			wantCalls: []string{listURI, detailURI, imageUploadURI},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeMeizu{t: t, status: statusOnSale, noIcon: true, failImage: c.failImage}
			var res *store.UploadResult
			if c.apk == "" {
				res = runUpload(t, f, t.TempDir(), nil)
			} else {
				res = runUploadAPK(t, f, c.apk, nil)
			}
			if res.Success {
				t.Fatal("upload succeeded, want icon failure")
			}
			if !strings.HasPrefix(res.Error, "icon: ") || !strings.Contains(res.Error, c.wantErr) {
				t.Errorf("error = %q, want an icon error carrying %q", res.Error, c.wantErr)
			}
			if !slices.Equal(f.calls, c.wantCalls) {
				t.Errorf("calls = %v, want %v", f.calls, c.wantCalls)
			}
		})
	}
}
