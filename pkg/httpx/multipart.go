// Package httpx provides streaming-friendly HTTP helpers used by store
// uploaders. Specifically, DoMultipart performs a multipart/form-data
// POST/PUT whose body is fed through io.Pipe so file payloads stream
// straight to the network without ever residing in a single buffer.
//
// resty's multipart implementation buffers the entire request body in
// a *bytes.Buffer before sending — fine for tiny form payloads but a
// memory pressure bomb when the form contains a 100 MB+ APK. Each store
// uploader uses DoMultipart for its file-bearing call to keep peak
// memory ~independent of APK size and to make progress reporters fire
// as bytes actually leave the host.
package httpx

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// FileField is a multipart file part.
//
// Size is optional but strongly recommended: when every file in a
// MultipartRequest has Size > 0, DoMultipart pre-computes a total
// Content-Length and sends it as a header instead of falling back to
// Transfer-Encoding: chunked. Some COS-style upload endpoints (e.g.
// pgyer's, AWS S3) reject chunked multipart POSTs with
// "MalformedPOSTRequest", so passing Size matters in practice.
type FileField struct {
	Field    string    // form field name
	FileName string    // filename in Content-Disposition
	Reader   io.Reader // streamed; caller closes the underlying source
	Size     int64     // optional; required for Content-Length precomputation
}

// MultipartRequest describes a streaming multipart POST/PUT.
type MultipartRequest struct {
	Method  string            // "POST" / "PUT"
	URL     string            // absolute URL
	Query   url.Values        // optional URL query params
	Fields  map[string]string // multipart non-file form fields
	Files   []FileField       // multipart file parts (streamed)
	Headers map[string]string // extra request headers
	Client  *http.Client      // optional; nil → default (30 min timeout)
}

// defaultClient mirrors a resty default but with a long enough timeout
// to accommodate uploads of game-sized APKs over flaky networks.
var defaultClient = &http.Client{Timeout: 30 * time.Minute}

// DoMultipart issues the request and returns the response. The caller
// is responsible for reading and closing resp.Body.
//
// Implementation: a goroutine drains Fields + Files into the multipart
// writer, which writes into the writer side of an io.Pipe; the HTTP
// transport reads the matching pipe reader as the request body. Result:
//   - peak memory ≈ pipe buffer (a few KB), regardless of file size
//   - progress reporters wrapping FileField.Reader fire as bytes are
//     sent on the wire, not as they're staged in RAM
//
// When every FileField has Size set, Content-Length is precomputed and
// sent as a header (some upload endpoints reject chunked multipart).
// Otherwise the request body is sent with Transfer-Encoding: chunked.
func DoMultipart(ctx context.Context, mr MultipartRequest) (*http.Response, error) {
	urlStr := mr.URL
	if len(mr.Query) > 0 {
		sep := "?"
		if strings.Contains(urlStr, "?") {
			sep = "&"
		}
		urlStr += sep + mr.Query.Encode()
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)

	contentLength, lenKnown := computeContentLength(mw.Boundary(), mr.Fields, mr.Files)

	go func() {
		err := writeMultipart(mw, mr.Fields, mr.Files)
		if err == nil {
			err = mw.Close()
		}
		pw.CloseWithError(err)
	}()

	req, err := http.NewRequestWithContext(ctx, mr.Method, urlStr, pr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if lenKnown {
		req.ContentLength = contentLength
	}
	for k, v := range mr.Headers {
		req.Header.Set(k, v)
	}

	client := mr.Client
	if client == nil {
		client = defaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, RedactURLError(err)
	}
	return resp, nil
}

// RedactURLError strips the query string from the URL embedded in any
// *url.Error in err's chain. Store upload URLs are pre-signed: the query
// carries signatures and security tokens, and transport errors (`Put
// "https://…?x-cos-security-token=…": context deadline exceeded`) would
// otherwise leak them into logs and user-facing failure reasons — as
// well as burying the actual error behind a thousand characters of
// base64. The error is mutated in place and returned for chaining.
func RedactURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		if i := strings.IndexByte(ue.URL, '?'); i >= 0 {
			ue.URL = ue.URL[:i]
		}
	}
	return err
}

// computeContentLength does a dry-run multipart write into a counting
// writer to measure exactly how many bytes the headers + boundaries
// will produce, then adds the file body sizes. Returns (size, true)
// when every file has a known Size; (0, false) otherwise (caller falls
// back to chunked transfer encoding).
func computeContentLength(boundary string, fields map[string]string, files []FileField) (int64, bool) {
	for _, f := range files {
		if f.Size <= 0 {
			return 0, false
		}
	}
	cw := &countingWriter{}
	dry := multipart.NewWriter(cw)
	if err := dry.SetBoundary(boundary); err != nil {
		return 0, false
	}
	for k, v := range fields {
		if err := dry.WriteField(k, v); err != nil {
			return 0, false
		}
	}
	for _, f := range files {
		// CreateFormFile emits the per-file headers (boundary + content-
		// disposition + content-type) but no body. The actual body bytes
		// are accounted for separately via f.Size, and the trailing CRLF
		// before the next boundary is emitted by the next CreateFormFile
		// or by Close().
		if _, err := dry.CreateFormFile(f.Field, f.FileName); err != nil {
			return 0, false
		}
	}
	if err := dry.Close(); err != nil {
		return 0, false
	}
	total := cw.n
	for _, f := range files {
		total += f.Size
	}
	return total, true
}

type countingWriter struct{ n int64 }

func (c *countingWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

func writeMultipart(mw *multipart.Writer, fields map[string]string, files []FileField) error {
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			return err
		}
	}
	for _, f := range files {
		part, err := mw.CreateFormFile(f.Field, f.FileName)
		if err != nil {
			return err
		}
		if _, err := io.Copy(part, f.Reader); err != nil {
			return err
		}
	}
	return nil
}
