package honor

import (
	"os"
	"path/filepath"
	"testing"
)

// TestShouldURLPush covers honor's size gate: it only hands the APK to
// honor's async download-from-URL (which throttles status polls to
// ~3min) when the file is at least urlPushMinBytes; smaller files upload
// faster directly. A stat failure must fall back to the upload path.
func TestShouldURLPush(t *testing.T) {
	dir := t.TempDir()
	mkfile := func(name string, size int) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	small := mkfile("small.apk", 10)
	big := mkfile("big.apk", 200)

	cases := []struct {
		name     string
		minBytes int64
		path     string
		want     bool
	}{
		{"above threshold", 100, big, true},
		{"below threshold", 100, small, false},
		{"equal threshold is inclusive", 200, big, true},
		{"zero falls back to 100MB default", 0, big, false}, // 200 bytes < 100MiB
		{"missing file falls back to upload", 100, filepath.Join(dir, "nope.apk"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &Store{urlPushMinBytes: c.minBytes}
			if got := s.shouldURLPush(c.path); got != c.want {
				t.Errorf("shouldURLPush(min=%d) = %v, want %v", c.minBytes, got, c.want)
			}
		})
	}
}
