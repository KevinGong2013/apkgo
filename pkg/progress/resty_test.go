package progress_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/pkg/progress"
)

// countingReporter is a local spy since the internal one in progress_test.go
// lives in package progress (internal) and this file is package progress_test.
type countingReporter struct {
	added atomic.Int64
	total atomic.Int64
	phase atomic.Value
}

func (r *countingReporter) Phase(name string) { r.phase.Store(name) }
func (r *countingReporter) Total(n int64)     { r.total.Store(n) }
func (r *countingReporter) Add(n int64)       { r.added.Add(n) }

// TestProgressWithResty confirms that wrapping a file reader with
// progress.OpenFile + SetFileReader on a real resty client against a real
// HTTP server produces byte-accurate progress events. This is the core
// contract every store in pkg/store/* depends on.
func TestProgressWithResty(t *testing.T) {
	// Write a 200 KiB dummy file — large enough to span many Read() calls.
	dir := t.TempDir()
	path := filepath.Join(dir, "fake.apk")
	const size = 200 * 1024
	if err := os.WriteFile(path, make([]byte, size), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var serverSawBytes int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Drain the multipart body — for this smoke test we only care
		// that the whole thing arrives.
		r.ParseMultipartForm(32 << 20)
		for _, headers := range r.MultipartForm.File {
			for _, h := range headers {
				f, _ := h.Open()
				n, _ := io.Copy(io.Discard, f)
				f.Close()
				atomic.AddInt64(&serverSawBytes, n)
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	spy := &countingReporter{}
	rc, fileSize, err := progress.OpenFile(path, spy)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer rc.Close()

	if fileSize != size {
		t.Fatalf("file size = %d, want %d", fileSize, size)
	}
	if got := spy.total.Load(); got != size {
		t.Fatalf("spy.Total = %d, want %d", got, size)
	}

	_, err = resty.New().R().
		SetFileReader("file", filepath.Base(path), rc).
		Post(srv.URL)
	if err != nil {
		t.Fatalf("resty Post: %v", err)
	}

	if got := atomic.LoadInt64(&serverSawBytes); got != size {
		t.Fatalf("server received %d bytes, want %d", got, size)
	}
	if got := spy.added.Load(); got != size {
		t.Fatalf("reporter saw %d bytes via Add(), want %d", got, size)
	}
}
