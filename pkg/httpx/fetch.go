package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// IsURL reports whether ref looks like an http(s) URL.
func IsURL(ref string) bool {
	s := strings.ToLower(ref)
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// FetchToTemp downloads ref into a temporary file and returns the path
// plus a cleanup func the caller MUST defer. The temp file is created
// in the system temp dir with a stable suffix so OS tooling treats it
// as the right kind of file.
//
// extraHeaders is applied to the GET request (e.g. Authorization). Pass
// nil for no extras.
func FetchToTemp(ctx context.Context, ref string, extraHeaders map[string]string) (path string, cleanup func(), err error) {
	if !IsURL(ref) {
		return "", nil, fmt.Errorf("not a URL: %q", ref)
	}
	parsed, err := url.Parse(ref)
	if err != nil {
		return "", nil, fmt.Errorf("parse url: %w", err)
	}

	// Use the URL's filename suffix when available so the temp file
	// has a recognisable extension (most stores key behaviour off the
	// extension to decide whether the upload is an APK).
	suffix := filepath.Ext(parsed.Path)
	if suffix == "" {
		suffix = ".apk"
	}
	tmp, err := os.CreateTemp("", "apkgo-fetch-*"+suffix)
	if err != nil {
		return "", nil, fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup = func() { _ = os.Remove(tmpPath) }

	// Defensive: clean up the temp file if anything from here on fails.
	success := false
	defer func() {
		if !success {
			tmp.Close()
			cleanup()
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref, nil)
	if err != nil {
		return "", nil, err
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := defaultClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("fetch %s: %w", ref, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return "", nil, fmt.Errorf("fetch %s: http %d: %s",
			ref, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Stream straight to disk; we never load the whole thing into RAM.
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return "", nil, fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", nil, fmt.Errorf("close temp: %w", err)
	}

	success = true
	return tmpPath, cleanup, nil
}

// FetchToTempBatch fetches each ref into a separate temp file and
// returns their local paths in the same order. The single returned
// cleanup func removes every temp file created. Empty refs are passed
// through unchanged so callers can use the helper for optional inputs
// (e.g. --file64).
//
// On any per-ref error, all already-fetched files are cleaned up before
// returning the error.
func FetchToTempBatch(ctx context.Context, refs []string, extraHeaders map[string]string) (paths []string, cleanup func(), err error) {
	paths = make([]string, len(refs))
	cleanups := make([]func(), 0, len(refs))
	cleanup = func() {
		for _, c := range cleanups {
			c()
		}
	}

	for i, ref := range refs {
		if ref == "" || !IsURL(ref) {
			paths[i] = ref
			continue
		}
		p, cl, ferr := FetchToTemp(ctx, ref, extraHeaders)
		if ferr != nil {
			cleanup()
			return nil, nil, ferr
		}
		paths[i] = p
		cleanups = append(cleanups, cl)
	}
	return paths, cleanup, nil
}

// ParseHeaders parses a list of "Header: value" strings (as accepted by
// repeatable CLI flags) into a map. Empty entries are skipped.
func ParseHeaders(specs []string) (map[string]string, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(specs))
	for _, raw := range specs {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		colon := strings.IndexByte(s, ':')
		if colon <= 0 {
			return nil, errors.New(`bad header (expected "Name: value"): ` + s)
		}
		name := strings.TrimSpace(s[:colon])
		value := strings.TrimSpace(s[colon+1:])
		if name == "" {
			return nil, errors.New(`bad header (empty name): ` + s)
		}
		out[name] = value
	}
	return out, nil
}
