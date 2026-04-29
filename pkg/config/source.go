package config

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// CredsSource is a parsed --creds-from spec. The CLI accepts:
//
//	stdin       — read JSON from os.Stdin
//	fd:N        — read JSON from file descriptor N (parent must have
//	              opened it before fork+exec; useful when an
//	              orchestrator wants to keep stdin free for piping)
//
// Future-friendly: vault:path / kms:arn / etc. can slot in here
// without changing the calling sites.
type CredsSource struct {
	kind  string // "stdin" | "fd"
	fd    int    // for kind == "fd"
	close bool   // whether the returned reader needs to be closed
}

// ParseCredsSource validates the --creds-from value. Returns an
// error for unknown forms.
func ParseCredsSource(spec string) (CredsSource, error) {
	switch {
	case spec == "":
		return CredsSource{}, fmt.Errorf("empty creds source")
	case spec == "stdin":
		return CredsSource{kind: "stdin"}, nil
	case strings.HasPrefix(spec, "fd:"):
		n, err := strconv.Atoi(strings.TrimPrefix(spec, "fd:"))
		if err != nil || n < 0 {
			return CredsSource{}, fmt.Errorf("bad fd in --creds-from %q (expected fd:N)", spec)
		}
		return CredsSource{kind: "fd", fd: n, close: true}, nil
	default:
		return CredsSource{}, fmt.Errorf("unknown --creds-from %q (expected stdin or fd:N)", spec)
	}
}

// Open returns an io.Reader for this source. Caller must Close the
// returned reader.
func (s CredsSource) Open() (io.ReadCloser, error) {
	switch s.kind {
	case "stdin":
		// Wrap os.Stdin in a no-close shim so the close in Load doesn't
		// kill stdin for any downstream readers (e.g. hooks that also
		// expect stdin to be available).
		return io.NopCloser(os.Stdin), nil
	case "fd":
		f := os.NewFile(uintptr(s.fd), fmt.Sprintf("fd:%d", s.fd))
		if f == nil {
			return nil, fmt.Errorf("file descriptor %d not open", s.fd)
		}
		return f, nil
	default:
		return nil, fmt.Errorf("creds source not initialised")
	}
}

// LoadCreds opens the source, parses JSON config, and closes the
// underlying reader. Convenience wrapper for the common case.
func LoadCreds(spec string) (*Config, error) {
	src, err := ParseCredsSource(spec)
	if err != nil {
		return nil, err
	}
	r, err := src.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return LoadFromJSON(r)
}
