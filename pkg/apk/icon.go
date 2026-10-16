package apk

import (
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // in-APK launcher icons may be JPEG
	"image/png"
	"io"
	"os"

	"github.com/shogo82148/androidbinary"
	"github.com/shogo82148/androidbinary/apk"
	_ "golang.org/x/image/webp" // modern Android (R8) stores launcher icons as webp
)

// iconDensities lists the launcher-icon densities to request, densest
// first. androidbinary resolves the icon resource for the requested
// config, and a zero density means mdpi — the 48px variant, which is what
// used to put blurry icons in store consoles (#51). 0xFFFE (anydpi) is
// deliberately not in the list: it resolves to the adaptive-icon XML,
// which image.Decode can't read. Lower densities are fallbacks for APKs
// that only ship an adaptive XML at the top end.
var iconDensities = []uint16{640, 480, 320, 240, 160}

// LauncherIcon decodes the densest launcher icon the APK at path ships.
func LauncherIcon(path string) (image.Image, error) {
	var img image.Image
	err := withFile(path, "apk", func(r io.ReaderAt, size int64) (err error) {
		img, err = LauncherIconAt(r, size)
		return err
	})
	return img, err
}

// LauncherIconAt is LauncherIcon over an APK read through r (size bytes
// long).
func LauncherIconAt(r io.ReaderAt, size int64) (image.Image, error) {
	pkg, err := apk.OpenZipReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("open apk: %w", err)
	}
	defer pkg.Close()

	var errs []error
	for _, d := range iconDensities {
		img, err := pkg.Icon(&androidbinary.ResTableConfig{Density: d})
		if err == nil {
			return img, nil
		}
		errs = append(errs, fmt.Errorf("density %d: %w", d, err))
	}
	return nil, fmt.Errorf("read icon: %w", errors.Join(errs...))
}

// WriteLauncherIconPNG writes the APK's launcher icon (LauncherIconAt) as
// a PNG file at dest — the form store.FileFacts.Icon takes.
func WriteLauncherIconPNG(r io.ReaderAt, size int64, dest string) error {
	img, err := LauncherIconAt(r, size)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		os.Remove(dest)
		return fmt.Errorf("encode png: %w", err)
	}
	return f.Close()
}
