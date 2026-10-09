package httptrace

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileRecorder appends exchanges to a file as JSON Lines: one exchange
// per line, numbered in the order they completed. The file is created
// readable by its owner only.
type FileRecorder struct {
	mu  sync.Mutex
	f   *os.File
	seq int
	err error
}

// NewFileRecorder opens (creating it and its directory if needed) the
// trace file at path for appending.
func NewFileRecorder(path string) (*FileRecorder, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &FileRecorder{f: f}, nil
}

// Record writes ex as the next line. A write failure never reaches the
// request being recorded; the first one is kept for Err.
func (r *FileRecorder) Record(ex Exchange) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	ex.Seq = r.seq
	line, err := json.Marshal(ex)
	if err == nil {
		_, err = r.f.Write(append(line, '\n'))
	}
	if err != nil && r.err == nil {
		r.err = err
	}
}

// Err returns the first error Record ran into, if any.
func (r *FileRecorder) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

// Close closes the file.
func (r *FileRecorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.f.Close()
}

// ReadFile reads a trace file written by FileRecorder.
func ReadFile(path string) ([]Exchange, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Exchange
	sc := bufio.NewScanner(f)
	// One line holds up to two MaxBody bodies plus JSON escaping.
	sc.Buffer(make([]byte, 0, 64<<10), 8*MaxBody)
	for line := 1; sc.Scan(); line++ {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var ex Exchange
		if err := json.Unmarshal(sc.Bytes(), &ex); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		out = append(out, ex)
	}
	return out, sc.Err()
}
