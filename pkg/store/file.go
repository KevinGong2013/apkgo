package store

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"image"
	_ "image/png" // FileFacts.Icon is a PNG
	"io"
	"os"
	"path/filepath"

	"github.com/KevinGong2013/apkgo/v4/pkg/apk"
)

// FileFacts are what the caller already knows about a package file
// (UploadRequest.FilePath / File64Path). A store takes a fact from here
// when it is set and reads the file otherwise — so a caller whose package
// sits in object storage, and whose uploads go through an
// httpx.Offloader, never needs the package on local disk: give every
// fact, and the stores neither open nor stat it.
type FileFacts struct {
	Size   int64  // bytes
	MD5    string // lowercase hex
	SHA256 string // lowercase hex
	// ABIs are the native ABIs under lib/<abi>/ (apk.ABIs). Nil means
	// unknown; an APK without native code has an empty, non-nil slice.
	ABIs []string
	// Icon is a local PNG of the package's launcher icon
	// (apk.WriteLauncherIconPNG); empty when unknown. Unused for packages
	// that have none (AAB, HarmonyOS).
	Icon string
	// IconErr says why the caller couldn't read the launcher icon, when
	// Icon is empty because it tried: LauncherIcon reports it rather than
	// reading the file.
	IconErr string
}

// PackageFile is a package to upload: its path and the facts the caller
// gave about it. Stores read package properties through it rather than
// from the file.
type PackageFile struct {
	Path  string
	Facts *FileFacts // nil: none
}

// Package returns the primary package (FilePath, FileFacts).
func (r *UploadRequest) Package() PackageFile {
	return PackageFile{Path: r.FilePath, Facts: r.FileFacts}
}

// Package64 returns the 64-bit package (File64Path, File64Facts); its Path
// is empty when there is none.
func (r *UploadRequest) Package64() PackageFile {
	return PackageFile{Path: r.File64Path, Facts: r.File64Facts}
}

// Name is the package's file name, as sent to stores.
func (p PackageFile) Name() string { return filepath.Base(p.Path) }

// Size returns the package's size in bytes.
func (p PackageFile) Size() (int64, error) {
	if p.Facts != nil && p.Facts.Size > 0 {
		return p.Facts.Size, nil
	}
	fi, err := os.Stat(p.Path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// MD5 returns the package's MD5 digest, lowercase hex.
func (p PackageFile) MD5() (string, error) {
	if p.Facts != nil && p.Facts.MD5 != "" {
		return p.Facts.MD5, nil
	}
	return hashFile(p.Path, md5.New())
}

// SHA256 returns the package's SHA-256 digest, lowercase hex.
func (p PackageFile) SHA256() (string, error) {
	if p.Facts != nil && p.Facts.SHA256 != "" {
		return p.Facts.SHA256, nil
	}
	return hashFile(p.Path, sha256.New())
}

// ABIs returns the native ABIs the APK declares (see FileFacts.ABIs).
func (p PackageFile) ABIs() ([]string, error) {
	if p.Facts != nil && p.Facts.ABIs != nil {
		return p.Facts.ABIs, nil
	}
	return apk.ABIs(p.Path)
}

// LauncherIcon decodes the package's launcher icon (apk.LauncherIcon).
func (p PackageFile) LauncherIcon() (image.Image, error) {
	if p.Facts != nil && p.Facts.Icon == "" && p.Facts.IconErr != "" {
		return nil, errors.New(p.Facts.IconErr)
	}
	if p.Facts == nil || p.Facts.Icon == "" {
		return apk.LauncherIcon(p.Path)
	}
	f, err := os.Open(p.Facts.Icon)
	if err != nil {
		return nil, fmt.Errorf("open icon: %w", err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode icon: %w", err)
	}
	return img, nil
}

// TotalSize returns the combined size of the packages with a path.
func TotalSize(files ...PackageFile) (int64, error) {
	var total int64
	for _, f := range files {
		if f.Path == "" {
			continue
		}
		n, err := f.Size()
		if err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

func hashFile(path string, h hash.Hash) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
