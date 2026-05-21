package apk

import (
	"archive/zip"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestABIs(t *testing.T) {
	cases := []struct {
		name    string
		entries []string
		want    []string
	}{
		{
			name:    "64-bit only",
			entries: []string{"lib/arm64-v8a/libfoo.so", "AndroidManifest.xml"},
			want:    []string{"arm64-v8a"},
		},
		{
			name:    "split arch",
			entries: []string{"lib/armeabi-v7a/libfoo.so", "lib/arm64-v8a/libfoo.so"},
			want:    []string{"arm64-v8a", "armeabi-v7a"},
		},
		{
			name:    "no native libs",
			entries: []string{"AndroidManifest.xml", "classes.dex"},
			want:    []string{},
		},
		{
			name:    "ignore non-lib entries with lib prefix",
			entries: []string{"library.txt", "lib/", "lib/notes.txt"},
			want:    []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeZip(t, tc.entries)
			got, err := ABIs(path)
			if err != nil {
				t.Fatalf("ABIs: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) && !(len(got) == 0 && len(tc.want) == 0) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIs64BitOnly(t *testing.T) {
	cases := []struct {
		abis []string
		want bool
	}{
		{[]string{"arm64-v8a"}, true},
		{[]string{"arm64-v8a", "x86_64"}, true},
		{[]string{"arm64-v8a", "armeabi-v7a"}, false},
		{[]string{"armeabi-v7a"}, false},
		{[]string{}, false},
		{[]string{"unknown-future-abi"}, false},
	}
	for _, tc := range cases {
		if got := Is64BitOnly(tc.abis); got != tc.want {
			t.Errorf("Is64BitOnly(%v) = %v, want %v", tc.abis, got, tc.want)
		}
	}
}

func writeZip(t *testing.T, entries []string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.apk")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, name := range entries {
		if _, err := zw.Create(name); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
