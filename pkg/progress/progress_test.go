package progress

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// spyReporter records every event for assertions.
type spyReporter struct {
	phases []string
	total  atomic.Int64
	added  atomic.Int64
}

func (r *spyReporter) Phase(name string) { r.phases = append(r.phases, name) }
func (r *spyReporter) Total(n int64)     { r.total.Store(n) }
func (r *spyReporter) Add(n int64)       { r.added.Add(n) }

func TestReaderForwardsBytes(t *testing.T) {
	src := bytes.NewReader(make([]byte, 4096))
	spy := &spyReporter{}
	r := &Reader{R: src, Reporter: spy}

	n, err := io.Copy(io.Discard, r)
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if n != 4096 {
		t.Fatalf("copied %d bytes, want 4096", n)
	}
	if spy.added.Load() != 4096 {
		t.Fatalf("reporter saw %d bytes, want 4096", spy.added.Load())
	}
}

func TestOpenFileSetsTotalAndReportsAdd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dummy.bin")
	payload := bytes.Repeat([]byte{'a'}, 1234)
	if err := os.WriteFile(path, payload, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	spy := &spyReporter{}
	rc, size, err := OpenFile(path, spy)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer rc.Close()

	if size != 1234 {
		t.Fatalf("size = %d, want 1234", size)
	}
	if got := spy.total.Load(); got != 1234 {
		t.Fatalf("Total() got %d, want 1234", got)
	}

	// Read the whole file and check Add() matches.
	n, err := io.Copy(io.Discard, rc)
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if n != 1234 {
		t.Fatalf("read %d, want 1234", n)
	}
	if got := spy.added.Load(); got != 1234 {
		t.Fatalf("Add() total %d, want 1234", got)
	}
}

func TestWrapFileDoesNotSetTotal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dummy.bin")
	if err := os.WriteFile(path, []byte("hello"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	spy := &spyReporter{}
	rc, size, err := WrapFile(path, spy)
	if err != nil {
		t.Fatalf("WrapFile: %v", err)
	}
	defer rc.Close()

	if size != 5 {
		t.Fatalf("size = %d, want 5", size)
	}
	// Unlike OpenFile, WrapFile must NOT call Total — callers set it
	// themselves when combining multiple files.
	if got := spy.total.Load(); got != 0 {
		t.Fatalf("WrapFile unexpectedly called Total (%d)", got)
	}
}

func TestSafeNil(t *testing.T) {
	r := Safe(nil)
	if r == nil {
		t.Fatal("Safe(nil) returned nil")
	}
	// Must not panic.
	r.Phase("x")
	r.Total(1)
	r.Add(1)
}
