// Package progress defines a minimal interface for reporting upload
// progress from store implementations back to the CLI, which renders it
// as multi-bar output via mpb.
package progress

import (
	"io"
	"os"
)

// Reporter receives progress events from a single store's upload.
// Stores should treat nil receivers as no-ops — use Safe() to wrap.
//
// Event order is: Phase() zero or more times, Total() once when the
// upload phase begins (or zero-length), Add() as bytes are streamed.
// A store may call Phase() again for subsequent phases (e.g. "publishing")
// without calling Total/Add again; the bar will then show a spinner.
type Reporter interface {
	// Phase signals a new phase of work, e.g. "auth", "uploading",
	// "publishing", "polling". Short lowercase strings render best.
	Phase(name string)

	// Total sets the expected byte count for the current phase.
	// Pass 0 for an indeterminate phase (spinner only).
	Total(bytes int64)

	// Add increments the current phase's progress by n bytes.
	Add(bytes int64)
}

// Nop is a Reporter that discards all events. Safe for concurrent use.
type Nop struct{}

func (Nop) Phase(string) {}
func (Nop) Total(int64)  {}
func (Nop) Add(int64)    {}

// Safe returns r, or Nop{} if r is nil. Stores can call this once at
// the top of their upload function to avoid nil-checks everywhere.
func Safe(r Reporter) Reporter {
	if r == nil {
		return Nop{}
	}
	return r
}

// Reader wraps an io.Reader and forwards byte counts to a Reporter as
// data is read. Used to instrument multipart form file uploads.
type Reader struct {
	R        io.Reader
	Reporter Reporter
}

func (r *Reader) Read(p []byte) (int, error) {
	n, err := r.R.Read(p)
	if n > 0 {
		r.Reporter.Add(int64(n))
	}
	return n, err
}

// fileReadCloser bundles a progress-wrapped reader with the underlying
// *os.File so callers can defer Close() on a single value.
type fileReadCloser struct {
	io.Reader
	closer io.Closer
}

func (f *fileReadCloser) Close() error { return f.closer.Close() }

// WrapFile opens path and returns an io.ReadCloser that forwards byte
// counts to r as data is read, plus the file size. The caller MUST
// Close() the returned reader. This helper does NOT call r.Total() —
// use it when a single upload streams multiple files and the caller
// wants to set a combined total. For single-file uploads, OpenFile is
// more convenient.
func WrapFile(path string, r Reporter) (io.ReadCloser, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return &fileReadCloser{
		Reader: &Reader{R: f, Reporter: Safe(r)},
		closer: f,
	}, fi.Size(), nil
}

// OpenFile is WrapFile + an immediate r.Total(size) call. Use this for
// single-file uploads where the file size is the full expected byte
// count for the current phase.
func OpenFile(path string, r Reporter) (io.ReadCloser, int64, error) {
	rc, size, err := WrapFile(path, r)
	if err != nil {
		return nil, 0, err
	}
	Safe(r).Total(size)
	return rc, size, nil
}
