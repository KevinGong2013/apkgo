package meizu

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // register jpeg decoder for in-APK icons
	"image/png"
	"os"

	"github.com/shogo82148/androidbinary"
	"github.com/shogo82148/androidbinary/apk"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // modern Android (R8) stores launcher icons as webp

	"github.com/KevinGong2013/apkgo/v4/pkg/ctxlog"
	"github.com/KevinGong2013/apkgo/v4/pkg/progress"
)

// publish requires `icon`, and an update echoes the one app/detail
// returns. For some apps app/detail returns none, so every version is
// refused with "[113001] 应用ICON不能为空" until someone sets the icon in
// the console. apkgo fills it in from the APK instead: the review
// guideline (docs?id=110) wants the store icon to match the installed one
// anyway.
//
// The API doc gives no pixel size for the icon; 512×512 PNG is what Meizu
// asks for where it does name one (FAQ, docs?id=27).
const iconSize = 512

// iconDensities lists the launcher-icon densities to request, densest
// first. androidbinary resolves the icon resource for the requested
// config, and a zero density means mdpi — the 48px variant. 0xFFFE
// (anydpi) is deliberately not in the list: it resolves to the
// adaptive-icon XML, which image.Decode can't read. Lower densities are
// fallbacks for APKs that only ship an adaptive XML at the top end.
var iconDensities = []uint16{640, 480, 320, 240, 160}

// launcherIcon uploads the APK's launcher icon through app/image/upload
// and returns the destFileName publish takes as `icon`.
func (s *Store) launcherIcon(ctx context.Context, apkPath string) (string, error) {
	iconPath, err := extractIcon(apkPath)
	if err != nil {
		return "", fmt.Errorf("app/detail returned none and the APK's launcher icon can't be read (在商店资料里提供图标，或到魅族开发者中心补上应用图标): %w", err)
	}
	defer os.Remove(iconPath)

	ctxlog.FromContext(ctx).Info("meizu has no icon for this app; submitting the APK's launcher icon")
	// Small file; don't disturb the APK's progress bar.
	name, err := s.uploadFile(ctx, imageUploadURI, iconPath, progress.Safe(nil))
	if err != nil {
		return "", fmt.Errorf("upload the APK's launcher icon: %w", err)
	}
	return name, nil
}

// extractIcon writes the APK's launcher icon as a 512×512 PNG to a temp
// file, returning the path. The file name ends in .png because
// app/image/upload checks the extension.
func extractIcon(apkPath string) (string, error) {
	src, err := readIcon(apkPath)
	if err != nil {
		return "", err
	}

	img := src
	if b := src.Bounds(); b.Dx() != iconSize || b.Dy() != iconSize {
		dst := image.NewRGBA(image.Rect(0, 0, iconSize, iconSize))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
		img = dst
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", fmt.Errorf("encode png: %w", err)
	}

	f, err := os.CreateTemp("", "apkgo_meizu_icon_*.png")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// readIcon decodes the densest launcher icon the APK ships as an image.
func readIcon(apkPath string) (image.Image, error) {
	pkg, err := apk.OpenFile(apkPath)
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
	return nil, errors.Join(errs...)
}
