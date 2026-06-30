package oppo

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg" // register jpeg decoder for in-APK icons
	"image/png"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/shogo82148/androidbinary"
	"github.com/shogo82148/androidbinary/apk"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // modern Android (R8) stores launcher icons as webp

	"github.com/KevinGong2013/apkgo/v3/pkg/progress"
)

// OPPO requires the app icon submitted to /app/upd to be a 512×512 PNG under
// 1MB. /app/upd re-validates whatever icon_url it round-trips back from
// /app/info, so an app whose stored icon predates (or never met) this rule is
// rejected on every APK update with e.g. "[800002] icon_url 图片宽度不符合要求".
const (
	oppoIconSize    = 512
	oppoIconMaxSize = 1 << 20 // 1MB
)

// compliantIconURL returns an icon_url that satisfies OPPO's /app/upd
// validation. If the app's existing OPPO-hosted icon already complies
// (512×512 PNG <1MB) it's returned unchanged — no work, no replacement. Only
// when it doesn't comply is the launcher icon pulled from the APK, normalized
// to a 512×512 PNG, uploaded to OPPO (type=photo), and the fresh URL returned.
//
// Best-effort: any failure along the replacement path returns the original URL
// unchanged, so publish proceeds exactly as it would have without this step
// (and fails with OPPO's own icon error if that icon was the blocker).
func (s *Store) compliantIconURL(ctx context.Context, currentURL, apkPath string, rep progress.Reporter) string {
	if iconURLCompliant(ctx, currentURL) {
		return currentURL
	}
	iconPath, err := extractCompliantIcon(apkPath)
	if err != nil {
		return currentURL
	}
	defer os.Remove(iconPath)

	// Small file; don't disturb the APK's progress bar.
	res, err := s.uploadFile(ctx, iconPath, "photo", progress.Safe(nil))
	if err != nil || res.URL == "" {
		return currentURL
	}
	return res.URL
}

// iconURLCompliant fetches the icon at url and reports whether it is a PNG of
// exactly 512×512 under 1MB. Any fetch/decode error → false (treated as
// non-compliant so the caller rebuilds it).
func iconURLCompliant(ctx context.Context, url string) bool {
	if url == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	// Read at most one byte past the limit so an oversized icon is rejected
	// without buffering an arbitrarily large body.
	body, err := io.ReadAll(io.LimitReader(resp.Body, oppoIconMaxSize+1))
	if err != nil || len(body) > oppoIconMaxSize {
		return false
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return false
	}
	return format == "png" && cfg.Width == oppoIconSize && cfg.Height == oppoIconSize
}

// extractCompliantIcon pulls the launcher icon from the APK and writes it as a
// 512×512 PNG to a temp file, returning the path. The in-APK icon is whatever
// density is closest to 512 (often smaller, e.g. xxxhdpi 192px), so it is
// always rescaled to exactly 512×512 with a high-quality kernel.
func extractCompliantIcon(apkPath string) (string, error) {
	pkg, err := apk.OpenFile(apkPath)
	if err != nil {
		return "", fmt.Errorf("open apk: %w", err)
	}
	defer pkg.Close()

	src, err := pkg.Icon(&androidbinary.ResTableConfig{Size: oppoIconSize})
	if err != nil {
		return "", fmt.Errorf("read icon: %w", err)
	}

	var img image.Image = src
	if b := src.Bounds(); b.Dx() != oppoIconSize || b.Dy() != oppoIconSize {
		dst := image.NewRGBA(image.Rect(0, 0, oppoIconSize, oppoIconSize))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
		img = dst
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", fmt.Errorf("encode png: %w", err)
	}
	if buf.Len() > oppoIconMaxSize {
		return "", fmt.Errorf("normalized icon is %d bytes, exceeds OPPO's 1MB limit", buf.Len())
	}

	f, err := os.CreateTemp("", "apkgo_oppo_icon_*.png")
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
