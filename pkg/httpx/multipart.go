// Package httpx provides streaming-friendly HTTP helpers used by store
// uploaders. DoMultipart performs a multipart/form-data POST/PUT and
// DoUpload a request whose body is one file; both stream file payloads
// straight to the network without ever holding them in a single buffer,
// and both can hand the request to an Offloader instead (see Transfer).
//
// resty's multipart implementation buffers the entire request body in
// a *bytes.Buffer before sending — fine for tiny form payloads but a
// memory pressure bomb when the form contains a 100 MB+ APK. Each store
// uploader uses DoMultipart for its file-bearing call to keep peak
// memory ~independent of APK size and to make progress reporters fire
// as bytes actually leave the host.
package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/KevinGong2013/apkgo/v4/pkg/httptrace"
	"github.com/KevinGong2013/apkgo/v4/pkg/progress"
)

// FileField is a multipart file part: a file on disk (Path) or a stream
// (Reader).
//
// Size is required with Path and strongly recommended with Reader: when
// every part's size is known, DoMultipart sends a Content-Length header
// instead of falling back to Transfer-Encoding: chunked. Some COS-style
// upload endpoints (e.g. pgyer's, AWS S3) reject chunked multipart POSTs
// with "MalformedPOSTRequest", so passing Size matters in practice.
type FileField struct {
	Field    string // form field name
	FileName string // filename in Content-Disposition
	// Path is the file to send. DoMultipart opens it only when the request
	// goes out from this process, so under an Offloader (see Transfer) it
	// need not exist here; its bytes count towards
	// MultipartRequest.Progress.
	Path string
	// Reader streams the part instead of Path; the caller closes the
	// underlying source and reports its progress. A request with a Reader
	// part is always sent from this process.
	Reader io.Reader
	Size   int64
}

// MultipartRequest describes a streaming multipart POST/PUT.
type MultipartRequest struct {
	Method  string            // "POST" / "PUT"
	URL     string            // absolute URL
	Query   url.Values        // optional URL query params
	Fields  map[string]string // multipart non-file form fields
	Files   []FileField       // multipart file parts (streamed)
	Headers map[string]string // extra request headers
	// Progress receives the bytes of Path parts as they are sent (Add
	// only — set Total yourself).
	Progress progress.Reporter
	Client   *http.Client // optional; nil → default (30 min timeout)
}

// defaultClient mirrors a resty default but with a long enough timeout
// to accommodate uploads of game-sized APKs over flaky networks.
var defaultClient = &http.Client{Timeout: 30 * time.Minute}

// DoMultipart issues the request and returns the response. The caller
// is responsible for reading and closing resp.Body.
//
// The body is built as segments — the encoded form fields and part
// headers, then each file's bytes — and streamed: peak memory stays
// independent of file size, and progress fires as bytes are sent on the
// wire, not as they're staged in RAM. Fields are written in sorted order.
// Under an Offloader the request is described to it as a Transfer
// instead (see Transfer).
//
// When every file's size is known, Content-Length is sent as a header
// (some upload endpoints reject chunked multipart). Otherwise the request
// body is sent with Transfer-Encoding: chunked.
func DoMultipart(ctx context.Context, mr MultipartRequest) (*http.Response, error) {
	segs, contentType, err := multipartSegments(mr.Fields, mr.Files)
	if err != nil {
		return nil, err
	}
	header := http.Header{}
	header.Set("Content-Type", contentType)
	for k, v := range mr.Headers {
		header.Set(k, v)
	}

	// The body is a one-way stream of file bytes: tell a recording
	// transport what it is made of instead of having it read along.
	parts := make([]httptrace.FilePart, len(mr.Files))
	for i, f := range mr.Files {
		parts[i] = httptrace.FilePart{Field: f.Field, Name: f.FileName, Size: f.Size}
	}
	ctx = httptrace.WithMultipart(ctx, mr.Fields, parts)

	return send(ctx, mr.Client, &Transfer{
		Method:   mr.Method,
		URL:      withQuery(mr.URL, mr.Query),
		Header:   header,
		Body:     segs,
		Progress: progress.Safe(mr.Progress),
	})
}

// multipartSegments encodes fields and files as body segments and returns
// them with the request's Content-Type (boundary included).
func multipartSegments(fields map[string]string, files []FileField) ([]Segment, string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	var segs []Segment
	flush := func() {
		if buf.Len() > 0 {
			segs = append(segs, Segment{Data: append([]byte(nil), buf.Bytes()...)})
			buf.Reset()
		}
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := mw.WriteField(k, fields[k]); err != nil {
			return nil, "", err
		}
	}
	for _, f := range files {
		// CreateFormFile emits the boundary and the part headers; the
		// CRLF that ends the part comes with the next boundary.
		if _, err := mw.CreateFormFile(f.Field, f.FileName); err != nil {
			return nil, "", err
		}
		flush()
		switch {
		case f.Path != "":
			if f.Size <= 0 {
				return nil, "", fmt.Errorf("multipart file %q: size required with Path", f.FileName)
			}
			segs = append(segs, Segment{File: &FileRef{Path: f.Path, Size: f.Size}})
		case f.Reader != nil:
			r := f.Reader
			if f.Size > 0 {
				r = &sizedReader{r: f.Reader, n: f.Size}
			}
			segs = append(segs, Segment{reader: r})
		default:
			return nil, "", fmt.Errorf("multipart file %q: neither Path nor Reader", f.FileName)
		}
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	flush()
	return segs, mw.FormDataContentType(), nil
}

// sizedReader marks a Reader part whose size the caller declared, so the
// body's Content-Length is known.
type sizedReader struct {
	r io.Reader
	n int64
}

func (s *sizedReader) Read(p []byte) (int, error) { return s.r.Read(p) }

func withQuery(rawURL string, q url.Values) string {
	if len(q) == 0 {
		return rawURL
	}
	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	return rawURL + sep + q.Encode()
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
