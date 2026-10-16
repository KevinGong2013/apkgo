package oppo

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/KevinGong2013/apkgo/v4/pkg/internal/offloadtest"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// fixtureAPK ships mdpi..xxxhdpi launcher icons (48..192px). It lives with
// the xiaomi store, whose icon extraction had the same bug (#51).
var fixtureAPK = filepath.Join("..", "xiaomi", "testdata", "helloworld.apk")

// fixturePackage returns fixtureAPK as a local file, or (remote) the way
// apkgo-cloud passes a package: a path that doesn't exist here, with its
// facts — the launcher icon as FileFacts.Icon among them.
func fixturePackage(t *testing.T, remote bool) store.PackageFile {
	t.Helper()
	req := &store.UploadRequest{FilePath: fixtureAPK}
	if remote {
		req, _, _ = offloadtest.Remote(t, context.Background(), req)
	}
	return req.Package()
}

// TestLauncherIconPicksDensest pins the density walk behind the icon OPPO
// gets. The old code set ResTableConfig.Size — the config struct's byte
// size, not a pixel size — so density stayed 0 (= mdpi) and the 48px
// variant was what got scaled up to 512×512 and sent to OPPO. Without the
// APK at hand the icon comes from FileFacts.Icon, and must be the same one.
func TestLauncherIconPicksDensest(t *testing.T) {
	for _, remote := range []bool{false, true} {
		img, err := fixturePackage(t, remote).LauncherIcon()
		if err != nil {
			t.Fatalf("remote=%v: LauncherIcon: %v", remote, err)
		}
		if b := img.Bounds(); b.Dx() != 192 || b.Dy() != 192 {
			t.Errorf("remote=%v: launcher icon is %dx%d, want 192x192 (xxxhdpi)", remote, b.Dx(), b.Dy())
		}
	}
}

func TestExtractCompliantIconMeetsOPPOSpec(t *testing.T) {
	for _, remote := range []bool{false, true} {
		iconPath, err := extractCompliantIcon(fixturePackage(t, remote))
		if err != nil {
			t.Fatalf("remote=%v: extractCompliantIcon: %v", remote, err)
		}
		defer os.Remove(iconPath)

		info, err := os.Stat(iconPath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > oppoIconMaxSize {
			t.Errorf("remote=%v: icon is %d bytes, want at most %d", remote, info.Size(), oppoIconMaxSize)
		}
		f, err := os.Open(iconPath)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		cfg, err := png.DecodeConfig(f)
		if err != nil {
			t.Fatalf("remote=%v: decode extracted icon: %v", remote, err)
		}
		if cfg.Width != oppoIconSize || cfg.Height != oppoIconSize {
			t.Errorf("remote=%v: icon is %dx%d, want %dx%d", remote, cfg.Width, cfg.Height, oppoIconSize, oppoIconSize)
		}
	}
}
