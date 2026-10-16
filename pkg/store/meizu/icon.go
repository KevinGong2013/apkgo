package meizu

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"os"

	"golang.org/x/image/draw"

	"github.com/KevinGong2013/apkgo/v4/pkg/ctxlog"
	"github.com/KevinGong2013/apkgo/v4/pkg/progress"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
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

// launcherIcon uploads the APK's launcher icon through app/image/upload
// and returns the destFileName publish takes as `icon`.
func (s *Store) launcherIcon(ctx context.Context, apkFile store.PackageFile) (string, error) {
	iconPath, err := extractIcon(apkFile)
	if err != nil {
		return "", fmt.Errorf("app/detail returned none and the APK's launcher icon can't be read (在商店资料里提供图标，或到魅族开发者中心补上应用图标): %w", err)
	}
	defer os.Remove(iconPath)

	ctxlog.FromContext(ctx).Info("meizu has no icon for this app; submitting the APK's launcher icon")
	// Small file; don't disturb the APK's progress bar.
	name, err := s.uploadFile(ctx, imageUploadURI, store.PackageFile{Path: iconPath}, progress.Safe(nil))
	if err != nil {
		return "", fmt.Errorf("upload the APK's launcher icon: %w", err)
	}
	return name, nil
}

// extractIcon writes the APK's launcher icon (PackageFile.LauncherIcon:
// the densest one it ships, so a 48px mdpi icon isn't blown up to a
// blurry 512px one) as a 512×512 PNG to a temp file, returning the path.
// The file name ends in .png because app/image/upload checks the
// extension.
func extractIcon(apkFile store.PackageFile) (string, error) {
	src, err := apkFile.LauncherIcon()
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
