package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/KevinGong2013/apkgo/v4/pkg/httptrace"
	"github.com/KevinGong2013/apkgo/v4/pkg/progress"
)

// Transfer is one file-bearing request — the upload of a package or an
// image to a store — as it goes on the wire: the body is its segments
// concatenated. DoMultipart and DoUpload describe their request as a
// Transfer and hand it to the context's Offloader, if there is one, so
// the bytes can be sent from another machine that holds a copy of the
// files while the store's API calls (sign-in, upload URL, submit) stay
// here.
type Transfer struct {
	Method string
	URL    string // absolute, query included
	// Host, when set, is sent as the Host header in place of the URL's
	// host (presigned object-storage URLs can sign a different one).
	Host   string
	Header http.Header // Content-Type carries the multipart boundary
	Body   []Segment
	// Progress receives the file bytes as they are sent (Add only: the
	// store has already set Total). Never nil.
	Progress progress.Reporter
}

// Segment is a piece of a Transfer's body: literal bytes or a whole file.
type Segment struct {
	Data []byte
	File *FileRef
	// reader is a caller-supplied body part (FileField.Reader). A
	// Transfer holding one can only be sent from this process.
	reader io.Reader
}

// FileRef names a file whose bytes form part of a Transfer: the path the
// store was given (UploadRequest.FilePath, a listing image) and its size.
type FileRef struct {
	Path string
	Size int64
}

// ContentLength is the body's size in bytes, or -1 when a part's size is
// unknown.
func (t *Transfer) ContentLength() int64 {
	var n int64
	for _, s := range t.Body {
		switch {
		case s.File != nil:
			n += s.File.Size
		case s.reader != nil:
			sr, ok := s.reader.(*sizedReader)
			if !ok {
				return -1
			}
			n += sr.n
		default:
			n += int64(len(s.Data))
		}
	}
	return n
}

// Files lists the files in the body, in order.
func (t *Transfer) Files() []FileRef {
	var out []FileRef
	for _, s := range t.Body {
		if s.File != nil {
			out = append(out, *s.File)
		}
	}
	return out
}

// offloadable reports whether every part of the body can be described to
// another machine.
func (t *Transfer) offloadable() bool {
	for _, s := range t.Body {
		if s.reader != nil {
			return false
		}
	}
	return len(t.Files()) > 0
}

// An Offloader carries out Transfers on the caller's behalf, typically on
// another machine with a copy of the files. Install one on the context
// passed to a store's Upload with WithOffloader; every file-bearing
// request the store makes is then handed to it instead of being sent.
//
// A store never reads a package file it was given except through a
// Transfer, so with an Offloader (and store.FileFacts for what a store
// needs to know about the file) the package need not exist locally.
type Offloader interface {
	// Offload sends t and returns the store's response, whose body the
	// store reads and closes. Returning ErrSendLocally instead has the
	// request sent from this process through the store's own HTTP
	// client, with the files read through Open.
	Offload(ctx context.Context, t *Transfer) (*http.Response, error)
	// Open opens a file of a Transfer sent from this process.
	Open(ctx context.Context, f FileRef) (io.ReadCloser, error)
}

// ErrSendLocally is what an Offloader returns to have a Transfer sent
// from this process after all — a small local file, or no other machine
// available.
var ErrSendLocally = errors.New("httpx: send the transfer from this process")

type offloaderKey struct{}

// WithOffloader returns a context under which file-bearing requests go to
// o (see Offloader).
func WithOffloader(ctx context.Context, o Offloader) context.Context {
	if o == nil {
		return ctx
	}
	return context.WithValue(ctx, offloaderKey{}, o)
}

// OffloaderFrom returns the Offloader installed on ctx, or nil.
func OffloaderFrom(ctx context.Context) Offloader {
	if ctx == nil {
		return nil
	}
	o, _ := ctx.Value(offloaderKey{}).(Offloader)
	return o
}

// send performs t: through ctx's Offloader when it takes it, otherwise
// from this process through client. An offloaded exchange is still
// recorded by client's recorder, like the request it stands in for.
func send(ctx context.Context, client *http.Client, t *Transfer) (*http.Response, error) {
	if client == nil {
		client = defaultClient
	}
	if t.Progress == nil {
		t.Progress = progress.Nop{}
	}
	o := OffloaderFrom(ctx)
	if o != nil && t.offloadable() {
		start := time.Now()
		resp, err := o.Offload(ctx, t)
		if !errors.Is(err, ErrSendLocally) {
			if err == nil && resp == nil {
				err = errors.New("offloader returned no response")
			}
			if resp != nil && resp.Body == nil {
				resp.Body = http.NoBody
			}
			// Built only to describe the exchange; its body is never read.
			req, rerr := t.request(ctx, func(FileRef) (io.ReadCloser, error) {
				return nil, errors.New("offloaded body is not read")
			})
			if rerr != nil {
				return nil, rerr
			}
			resp = httptrace.Recorded(client, req, start, resp, err)
			if err != nil {
				return nil, RedactURLError(fmt.Errorf("offloaded upload: %w", err))
			}
			return resp, nil
		}
	}
	open := func(f FileRef) (io.ReadCloser, error) { return os.Open(f.Path) }
	if o != nil {
		open = func(f FileRef) (io.ReadCloser, error) { return o.Open(ctx, f) }
	}
	req, err := t.request(ctx, open)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, RedactURLError(err)
	}
	return resp, nil
}

// request builds the http.Request for t, its body streaming the segments
// and opening each file through open as it is reached.
func (t *Transfer) request(ctx context.Context, open func(FileRef) (io.ReadCloser, error)) (*http.Request, error) {
	body := &segmentReader{segs: t.Body, open: open, progress: t.Progress}
	req, err := http.NewRequestWithContext(ctx, t.Method, t.URL, body)
	if err != nil {
		return nil, err
	}
	if n := t.ContentLength(); n >= 0 {
		req.ContentLength = n
	} else {
		req.ContentLength = -1
	}
	if req.ContentLength == 0 {
		req.Body = http.NoBody
	}
	req.Header = t.Header.Clone()
	if req.Header == nil {
		req.Header = http.Header{}
	}
	if t.Host != "" {
		req.Host = t.Host
	}
	return req, nil
}

// segmentReader streams a Transfer's body.
type segmentReader struct {
	segs     []Segment
	open     func(FileRef) (io.ReadCloser, error)
	progress progress.Reporter

	cur      io.Reader
	curClose io.Closer
}

func (r *segmentReader) Read(p []byte) (int, error) {
	for {
		if r.cur == nil {
			if len(r.segs) == 0 {
				return 0, io.EOF
			}
			s := r.segs[0]
			r.segs = r.segs[1:]
			switch {
			case s.File != nil:
				f, err := r.open(*s.File)
				if err != nil {
					return 0, fmt.Errorf("open %s: %w", s.File.Path, err)
				}
				r.cur, r.curClose = &progress.Reader{R: io.LimitReader(f, s.File.Size), Reporter: r.progress}, f
			case s.reader != nil:
				r.cur = s.reader
			default:
				r.cur = bytesReader(s.Data)
			}
		}
		n, err := r.cur.Read(p)
		if err == io.EOF {
			r.closeCur()
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (r *segmentReader) closeCur() {
	if r.curClose != nil {
		r.curClose.Close()
	}
	r.cur, r.curClose = nil, nil
}

// Close releases the file being read, if any.
func (r *segmentReader) Close() error {
	r.closeCur()
	r.segs = nil
	return nil
}

type byteSliceReader struct{ b []byte }

func bytesReader(b []byte) io.Reader { return &byteSliceReader{b: b} }

func (r *byteSliceReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.b)
	r.b = r.b[n:]
	return n, nil
}

// FileRequest describes a request whose whole body is one file — a PUT
// to a presigned object-storage URL, a media upload.
type FileRequest struct {
	Method   string
	URL      string
	Query    url.Values
	Host     string            // optional Host header override
	Headers  map[string]string // Content-Type included
	Path     string            // the file sent as the body
	Size     int64             // its size in bytes (required)
	Progress progress.Reporter // receives the bytes as they are sent
	Client   *http.Client      // nil → default (30 min timeout)
}

// DoUpload sends r's file as the request body — through the context's
// Offloader when there is one (see Transfer). The caller reads and
// closes the response body.
func DoUpload(ctx context.Context, r FileRequest) (*http.Response, error) {
	if r.Size <= 0 {
		return nil, fmt.Errorf("upload %s: unknown file size", r.Path)
	}
	header := http.Header{}
	for k, v := range r.Headers {
		header.Set(k, v)
	}
	t := &Transfer{
		Method:   r.Method,
		URL:      withQuery(r.URL, r.Query),
		Host:     r.Host,
		Header:   header,
		Body:     []Segment{{File: &FileRef{Path: r.Path, Size: r.Size}}},
		Progress: progress.Safe(r.Progress),
	}
	return send(ctx, r.Client, t)
}
