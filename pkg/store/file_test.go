package store

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/KevinGong2013/apkgo/v4/pkg/apk"
)

func TestPackageFileReadsTheFileWithoutFacts(t *testing.T) {
	path := filepath.Join("xiaomi", "testdata", "helloworld.apk")
	p := PackageFile{Path: path}
	size, err := p.Size()
	fi, _ := os.Stat(path)
	if err != nil || size != fi.Size() {
		t.Errorf("Size = %d, %v", size, err)
	}
	if md5Hex, err := p.MD5(); err != nil || len(md5Hex) != 32 {
		t.Errorf("MD5 = %q, %v", md5Hex, err)
	}
	if shaHex, err := p.SHA256(); err != nil || len(shaHex) != 64 {
		t.Errorf("SHA256 = %q, %v", shaHex, err)
	}
	if abis, err := p.ABIs(); err != nil || abis == nil {
		t.Errorf("ABIs = %v, %v", abis, err)
	}
	if img, err := p.LauncherIcon(); err != nil || img.Bounds().Dx() != 192 {
		t.Errorf("LauncherIcon: %v", err)
	}
	if p.Name() != "helloworld.apk" {
		t.Errorf("Name = %q", p.Name())
	}
}

// With every fact given, nothing reads the file — which doesn't exist.
func TestPackageFileUsesFacts(t *testing.T) {
	icon := filepath.Join(t.TempDir(), "icon.png")
	src, err := os.Open(filepath.Join("xiaomi", "testdata", "helloworld.apk"))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	fi, _ := src.Stat()
	if err := writeIcon(src, fi.Size(), icon); err != nil {
		t.Fatal(err)
	}

	p := (&UploadRequest{
		FilePath: "/nowhere/app.apk",
		FileFacts: &FileFacts{
			Size: 42, MD5: "m", SHA256: "s", ABIs: []string{}, Icon: icon,
		},
	}).Package()
	if n, err := p.Size(); n != 42 || err != nil {
		t.Errorf("Size = %d, %v", n, err)
	}
	if m, _ := p.MD5(); m != "m" {
		t.Errorf("MD5 = %q", m)
	}
	if s, _ := p.SHA256(); s != "s" {
		t.Errorf("SHA256 = %q", s)
	}
	if abis, err := p.ABIs(); err != nil || !reflect.DeepEqual(abis, []string{}) {
		t.Errorf("ABIs = %v, %v", abis, err)
	}
	if img, err := p.LauncherIcon(); err != nil || img.Bounds().Dx() != 192 {
		t.Errorf("LauncherIcon: %v", err)
	}
	if n, err := TotalSize(p, PackageFile{}); n != 42 || err != nil {
		t.Errorf("TotalSize = %d, %v", n, err)
	}
}

func writeIcon(src *os.File, size int64, dest string) error {
	return apk.WriteLauncherIconPNG(src, size, dest)
}

func TestPackageFileReportsTheIconError(t *testing.T) {
	p := PackageFile{Path: "/nowhere/app.apk", Facts: &FileFacts{IconErr: "read icon: no bitmap launcher icon"}}
	if _, err := p.LauncherIcon(); err == nil || err.Error() != "read icon: no bitmap launcher icon" {
		t.Errorf("LauncherIcon error = %v", err)
	}
}
