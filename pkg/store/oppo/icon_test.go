package oppo

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// fixtureAPK ships mdpi..xxxhdpi launcher icons (48..192px). It lives with
// the xiaomi store, whose icon extraction had the same bug (#51).
var fixtureAPK = filepath.Join("..", "xiaomi", "testdata", "helloworld.apk")

// TestReadIconPicksDensestLauncherIcon pins the density walk. The old code
// set ResTableConfig.Size — the config struct's byte size, not a pixel
// size — so density stayed 0 (= mdpi) and the 48px variant was what got
// scaled up to 512×512 and sent to OPPO.
func TestReadIconPicksDensestLauncherIcon(t *testing.T) {
	img, err := readIcon(fixtureAPK)
	if err != nil {
		t.Fatalf("readIcon: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 192 || b.Dy() != 192 {
		t.Errorf("launcher icon is %dx%d, want 192x192 (xxxhdpi)", b.Dx(), b.Dy())
	}
}

func TestExtractCompliantIconMeetsOPPOSpec(t *testing.T) {
	iconPath, err := extractCompliantIcon(fixtureAPK)
	if err != nil {
		t.Fatalf("extractCompliantIcon: %v", err)
	}
	defer os.Remove(iconPath)

	info, err := os.Stat(iconPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > oppoIconMaxSize {
		t.Errorf("icon is %d bytes, want at most %d", info.Size(), oppoIconMaxSize)
	}
	f, err := os.Open(iconPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatalf("decode extracted icon: %v", err)
	}
	if cfg.Width != oppoIconSize || cfg.Height != oppoIconSize {
		t.Errorf("icon is %dx%d, want %dx%d", cfg.Width, cfg.Height, oppoIconSize, oppoIconSize)
	}
}
