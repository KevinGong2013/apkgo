package apk

import (
	"archive/zip"
	"fmt"
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
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open apk: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat apk: %w", err)
	}

	pkg, err := apk.OpenZipReader(f, stat.Size())
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

// defaultLabel returns the application label from the APK's default
// (base values/) resources. Label(nil) must not be used here: a nil
// config matches every locale variant and androidbinary then prefers
// the most specific one, so an APK shipping e.g. values-zh-rTW/
// translations resolves to the Traditional Chinese name instead of the
// default label — which stores that accept appName (e.g. xiaomi) then
// write over the store listing. The zero-value config matches only
// locale-less entries, i.e. the base values/ label; if the base has no
// label at all, fall back to whatever variant matches.
func defaultLabel(pkg *apk.Apk) string {
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
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open apk zip: %w", err)
	}
	defer zr.Close()

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
