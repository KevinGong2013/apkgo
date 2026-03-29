package apk

import (
	"fmt"
	"os"

	"github.com/shogo82148/androidbinary/apk"
)

// Info holds metadata extracted from an APK file.
type Info struct {
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

	appName, _ := pkg.Label(nil)

	return &Info{
		PackageName: pkg.PackageName(),
		VersionName: pkg.Manifest().VersionName.MustString(),
		VersionCode: pkg.Manifest().VersionCode.MustInt32(),
		AppName:     appName,
	}, nil
}
