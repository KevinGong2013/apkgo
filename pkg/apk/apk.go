package apk

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shogo82148/androidbinary"
	"github.com/shogo82148/androidbinary/apk"
)

// Info holds metadata extracted from an APK file.
type Info struct {
	// Platform is "android" for APK/AAB input and "harmony" for HarmonyOS
	// .app/.hap packages (see ParseHarmony). Empty on AAB input, whose
	// metadata isn't parsed locally.
	Platform    string `json:"platform,omitempty"`
	PackageName string `json:"package"`
	VersionName string `json:"version_name"`
	VersionCode int32  `json:"version_code"`
	AppName     string `json:"app_name"`
}

// Parse extracts metadata from an APK file.
func Parse(path string) (*Info, error) {
	var info *Info
	err := withFile(path, "apk", func(r io.ReaderAt, size int64) (err error) {
		info, err = ParseAt(r, size)
		return err
	})
	return info, err
}

// ParseAt is Parse over an APK read through r (size bytes long) — for a
// package that lives elsewhere and is read in ranges: only the zip
// directory, the manifest and the resource table are read.
func ParseAt(r io.ReaderAt, size int64) (*Info, error) {
	pkg, err := apk.OpenZipReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("parse apk: %w", err)
	}
	defer pkg.Close()

	appName := defaultLabel(pkg)

	return &Info{
		Platform:    PlatformAndroid,
		PackageName: pkg.PackageName(),
		VersionName: pkg.Manifest().VersionName.MustString(),
		VersionCode: pkg.Manifest().VersionCode.MustInt32(),
		AppName:     appName,
	}, nil
}

// zhCNConfig specifies Simplified Chinese (zh-CN) locale.
var zhCNConfig = &androidbinary.ResTableConfig{
	Language: [2]uint8{'z', 'h'},
	Country:  [2]uint8{'C', 'N'},
}

// defaultLabel returns the application label extracted from the APK.
// Because apkgo targets Chinese app stores, it first queries with a
// Simplified Chinese locale (zh-CN). If the APK defines a zh-CN or zh
// resource (like an i18n app whose base values/ is English), that is
// preferred. If there is no Chinese resource, it falls back to the base
// values/ label (zero-value config), and finally to whatever variant matches.
//
// Label(nil) must not be used as first choice: a nil config matches every
// locale variant and androidbinary then prefers the most specific one,
// so an APK shipping e.g. values-zh-rTW/ translations resolves to the
// Traditional Chinese name (#48).
func defaultLabel(pkg *apk.Apk) string {
	if s, err := pkg.Label(zhCNConfig); err == nil && s != "" {
		return s
	}
	if s, err := pkg.Label(&androidbinary.ResTableConfig{}); err == nil && s != "" {
		return s
	}
	s, _ := pkg.Label(nil)
	return s
}

// ABIs returns the sorted, de-duplicated set of native ABIs declared by
// the APK, derived from entries under `lib/<abi>/`. An APK with no
// native code returns an empty slice.
func ABIs(path string) ([]string, error) {
	var abis []string
	err := withFile(path, "apk", func(r io.ReaderAt, size int64) (err error) {
		abis, err = ABIsAt(r, size)
		return err
	})
	return abis, err
}

// ABIsAt is ABIs over an APK read through r: only the zip directory is
// read.
func ABIsAt(r io.ReaderAt, size int64) ([]string, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("open apk zip: %w", err)
	}

	seen := make(map[string]struct{})
	for _, f := range zr.File {
		name := f.Name
		if !strings.HasPrefix(name, "lib/") {
			continue
		}
		rest := name[len("lib/"):]
		slash := strings.IndexByte(rest, '/')
		if slash <= 0 {
			continue
		}
		seen[rest[:slash]] = struct{}{}
	}

	abis := make([]string, 0, len(seen))
	for abi := range seen {
		abis = append(abis, abi)
	}
	sort.Strings(abis)
	return abis, nil
}

// IsAAB reports whether the given path is an Android App Bundle, based
// on the file extension (case-insensitive). AAB files are protobuf-
// encoded archives that cannot be parsed by the androidbinary APK reader
// — callers use this to route to bundle-specific upload paths and skip
// APK metadata extraction.
func IsAAB(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".aab")
}

// Is64BitOnly reports whether the APK contains only 64-bit native libs
// (e.g. arm64-v8a, x86_64) and no 32-bit ones. APKs with no native libs
// at all return false — they're universal and can ship as 32-bit
// compatible.
func Is64BitOnly(abis []string) bool {
	var has32, has64 bool
	for _, abi := range abis {
		switch abi {
		case "armeabi", "armeabi-v7a", "x86", "mips":
			has32 = true
		case "arm64-v8a", "x86_64", "mips64":
			has64 = true
		}
	}
	return has64 && !has32
}

// withFile opens path and calls fn with it as a ReaderAt and its size;
// what names the kind of file in errors.
func withFile(path, what string, fn func(r io.ReaderAt, size int64) error) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", what, err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", what, err)
	}
	return fn(f, fi.Size())
}
