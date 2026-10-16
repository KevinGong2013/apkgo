package httpx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KevinGong2013/apkgo/v4/pkg/httptrace"
)

// countingReporter totals the bytes reported to it.
type countingReporter struct{ n atomic.Int64 }

func (c *countingReporter) Phase(string) {}
func (c *countingReporter) Total(int64)  {}
func (c *countingReporter) Add(n int64)  { c.n.Add(n) }

func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// multipartServer answers 200 with {"ok":true} and keeps what it was sent.
type multipartServer struct {
	*httptest.Server
	mu            sync.Mutex
	fields        map[string]string
	files         map[string][]byte // field → bytes
	fileNames     map[string]string
	contentLength int64
	host          string
	hits          int
}

func newMultipartServer(t *testing.T) *multipartServer {
	s := &multipartServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.hits++
		s.contentLength = r.ContentLength
		s.host = r.Host
		s.fields, s.files, s.fileNames = map[string]string{}, map[string][]byte{}, map[string]string{}
		mt, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if mt == "multipart/form-data" {
			mr := multipart.NewReader(r.Body, params["boundary"])
			for {
				p, err := mr.NextPart()
				if err != nil {
					break
				}
				data, _ := io.ReadAll(p)
				if p.FileName() != "" {
					s.files[p.FormName()] = data
					s.fileNames[p.FormName()] = p.FileName()
				} else {
					s.fields[p.FormName()] = string(data)
				}
			}
		} else {
			data, _ := io.ReadAll(r.Body)
			s.files["body"] = data
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(s.Close)
	return s
}

func readAll(t *testing.T, resp *http.Response, err error) string {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDoMultipartSendsPathAndReaderParts(t *testing.T) {
	srv := newMultipartServer(t)
	apk := bytes.Repeat([]byte("apk-bytes-"), 5000)
	path := writeFile(t, "app.apk", apk)
	rep := &countingReporter{}

	resp, err := DoMultipart(context.Background(), MultipartRequest{
		Method: http.MethodPost,
		URL:    srv.URL + "/upload",
		Fields: map[string]string{"b": "2", "a": "1"},
		Files: []FileField{
			{Field: "file", FileName: "app.apk", Path: path, Size: int64(len(apk))},
			{Field: "icon", FileName: "icon.png", Reader: strings.NewReader("png"), Size: 3},
		},
		Progress: rep,
	})
	readAll(t, resp, err)

	if string(srv.files["file"]) != string(apk) || string(srv.files["icon"]) != "png" {
		t.Errorf("file parts not received intact")
	}
	if srv.fields["a"] != "1" || srv.fields["b"] != "2" {
		t.Errorf("fields = %v", srv.fields)
	}
	if srv.contentLength <= int64(len(apk)) {
		t.Errorf("Content-Length = %d, want a declared length", srv.contentLength)
	}
	// Only Path parts count towards Progress; Reader parts report themselves.
	if got := rep.n.Load(); got != int64(len(apk)) {
		t.Errorf("progress = %d, want %d", got, len(apk))
	}
}

func TestDoMultipartUnknownReaderSizeIsChunked(t *testing.T) {
	srv := newMultipartServer(t)
	resp, err := DoMultipart(context.Background(), MultipartRequest{
		Method: http.MethodPost,
		URL:    srv.URL,
		Files:  []FileField{{Field: "file", FileName: "x.bin", Reader: strings.NewReader("abc")}},
	})
	readAll(t, resp, err)
	if srv.contentLength != -1 || string(srv.files["file"]) != "abc" {
		t.Errorf("Content-Length = %d, file = %q", srv.contentLength, srv.files["file"])
	}
}

// fakeOffloader sends transfers itself from a map of known paths, the way
// a remote machine holding copies would.
type fakeOffloader struct {
	copies    map[string]string // path in the Transfer → real file
	decline   bool
	transfers []*Transfer
	opened    []string
}

func (f *fakeOffloader) Offload(ctx context.Context, t *Transfer) (*http.Response, error) {
	f.transfers = append(f.transfers, t)
	if f.decline {
		return nil, ErrSendLocally
	}
	var body bytes.Buffer
	for _, s := range t.Body {
		if s.File == nil {
			body.Write(s.Data)
			continue
		}
		data, err := os.ReadFile(f.copies[s.File.Path])
		if err != nil {
			return nil, err
		}
		body.Write(data)
		t.Progress.Add(int64(len(data)))
	}
	req, err := http.NewRequestWithContext(ctx, t.Method, t.URL, &body)
	if err != nil {
		return nil, err
	}
	req.Header = t.Header.Clone()
	if t.Host != "" {
		req.Host = t.Host
	}
	if int64(body.Len()) != t.ContentLength() {
		return nil, errors.New("segments do not add up to ContentLength")
	}
	return http.DefaultClient.Do(req)
}

func (f *fakeOffloader) Open(ctx context.Context, ref FileRef) (io.ReadCloser, error) {
	f.opened = append(f.opened, ref.Path)
	return os.Open(f.copies[ref.Path])
}

func TestDoMultipartOffloadsWithoutTheLocalFile(t *testing.T) {
	srv := newMultipartServer(t)
	apk := bytes.Repeat([]byte{1, 2, 3}, 4000)
	real := writeFile(t, "real.apk", apk)
	missing := filepath.Join(t.TempDir(), "elsewhere", "app.apk") // never exists here
	off := &fakeOffloader{copies: map[string]string{missing: real}}
	rep := &countingReporter{}

	ctx := WithOffloader(context.Background(), off)
	resp, err := DoMultipart(ctx, MultipartRequest{
		Method:   http.MethodPost,
		URL:      srv.URL + "/up",
		Fields:   map[string]string{"sign": "s1"},
		Files:    []FileField{{Field: "file", FileName: "app.apk", Path: missing, Size: int64(len(apk))}},
		Progress: rep,
	})
	if body := readAll(t, resp, err); body != `{"ok":true}` {
		t.Errorf("response = %q", body)
	}
	if len(off.transfers) != 1 {
		t.Fatalf("offloader saw %d transfers", len(off.transfers))
	}
	tr := off.transfers[0]
	if files := tr.Files(); len(files) != 1 || files[0].Path != missing || files[0].Size != int64(len(apk)) {
		t.Errorf("files = %+v", files)
	}
	if !strings.HasPrefix(tr.Header.Get("Content-Type"), "multipart/form-data; boundary=") {
		t.Errorf("Content-Type = %q", tr.Header.Get("Content-Type"))
	}
	if string(srv.files["file"]) != string(apk) || srv.fields["sign"] != "s1" || srv.fileNames["file"] != "app.apk" {
		t.Errorf("store did not receive the request the transfer described")
	}
	if srv.contentLength != tr.ContentLength() {
		t.Errorf("Content-Length %d, transfer says %d", srv.contentLength, tr.ContentLength())
	}
	if rep.n.Load() != int64(len(apk)) {
		t.Errorf("progress = %d", rep.n.Load())
	}
}

func TestDoMultipartDeclinedTransferReadsThroughOpen(t *testing.T) {
	srv := newMultipartServer(t)
	real := writeFile(t, "real.apk", []byte("payload"))
	missing := filepath.Join(t.TempDir(), "elsewhere.apk")
	off := &fakeOffloader{copies: map[string]string{missing: real}, decline: true}

	resp, err := DoMultipart(WithOffloader(context.Background(), off), MultipartRequest{
		Method: http.MethodPost,
		URL:    srv.URL,
		Files:  []FileField{{Field: "file", FileName: "a.apk", Path: missing, Size: 7}},
	})
	readAll(t, resp, err)
	if len(off.opened) != 1 || off.opened[0] != missing || string(srv.files["file"]) != "payload" {
		t.Errorf("opened %v, server got %q", off.opened, srv.files["file"])
	}
}

func TestReaderPartsAreNeverOffloaded(t *testing.T) {
	srv := newMultipartServer(t)
	off := &fakeOffloader{}
	resp, err := DoMultipart(WithOffloader(context.Background(), off), MultipartRequest{
		Method: http.MethodPost,
		URL:    srv.URL,
		Files:  []FileField{{Field: "file", FileName: "a.png", Reader: strings.NewReader("img"), Size: 3}},
	})
	readAll(t, resp, err)
	if len(off.transfers) != 0 || srv.hits != 1 {
		t.Errorf("transfers %d, hits %d", len(off.transfers), srv.hits)
	}
}

func TestDoUploadOffloadedWithHostOverride(t *testing.T) {
	srv := newMultipartServer(t)
	real := writeFile(t, "pack.app", []byte("harmony-pack"))
	missing := "/nowhere/pack.app"
	off := &fakeOffloader{copies: map[string]string{missing: real}}

	resp, err := DoUpload(WithOffloader(context.Background(), off), FileRequest{
		Method:  http.MethodPut,
		URL:     srv.URL + "/obj",
		Host:    "bucket.obs.example.com",
		Headers: map[string]string{"Content-Type": "application/octet-stream", "x-amz-date": "20261016T000000Z"},
		Path:    missing,
		Size:    12,
	})
	readAll(t, resp, err)
	tr := off.transfers[0]
	if tr.Host != "bucket.obs.example.com" || tr.Header.Get("x-amz-date") != "20261016T000000Z" {
		t.Errorf("transfer = %+v", tr)
	}
	if srv.host != "bucket.obs.example.com" || string(srv.files["body"]) != "harmony-pack" {
		t.Errorf("server got host %q body %q", srv.host, srv.files["body"])
	}
}

func TestDoUploadLocal(t *testing.T) {
	srv := newMultipartServer(t)
	path := writeFile(t, "x.apk", []byte("0123456789"))
	rep := &countingReporter{}
	resp, err := DoUpload(context.Background(), FileRequest{
		Method: http.MethodPut, URL: srv.URL, Path: path, Size: 10, Progress: rep,
		Headers: map[string]string{"Content-Type": "application/octet-stream"},
	})
	readAll(t, resp, err)
	if string(srv.files["body"]) != "0123456789" || srv.contentLength != 10 || rep.n.Load() != 10 {
		t.Errorf("body %q, length %d, progress %d", srv.files["body"], srv.contentLength, rep.n.Load())
	}
	if _, err := DoUpload(context.Background(), FileRequest{Method: http.MethodPut, URL: srv.URL, Path: path}); err == nil {
		t.Error("an upload without a size should be refused")
	}
}

func TestOffloadedExchangeIsRecorded(t *testing.T) {
	srv := newMultipartServer(t)
	real := writeFile(t, "real.apk", []byte("bytes"))
	missing := "/remote/app.apk"
	off := &fakeOffloader{copies: map[string]string{missing: real}}

	var (
		mu  sync.Mutex
		got []httptrace.Exchange
	)
	rec := httptrace.RecorderFunc(func(ex httptrace.Exchange) { mu.Lock(); got = append(got, ex); mu.Unlock() })
	cfg, release := httptrace.Carry(httptrace.WithRecorder(context.Background(), rec), "teststore", nil)
	defer release()
	client := httptrace.ForStore("teststore", cfg).NewClient(time.Minute)

	resp, err := DoMultipart(WithOffloader(context.Background(), off), MultipartRequest{
		Method: http.MethodPost,
		URL:    srv.URL + "/up?token=secret-token",
		Fields: map[string]string{"sign": "secret-sign", "type": "apk"},
		Files:  []FileField{{Field: "file", FileName: "app.apk", Path: missing, Size: 5}},
		Client: client,
	})
	readAll(t, resp, err)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("%d exchanges recorded", len(got))
	}
	ex := got[0]
	if ex.Store != "teststore" || ex.Status != 200 || string(ex.Response.JSON) != `{"ok":true}` {
		t.Errorf("exchange = %+v", ex)
	}
	if mp := ex.Request.Multipart; mp == nil || len(mp.Files) != 1 || mp.Files[0].Name != "app.apk" || mp.Fields["type"] != "apk" {
		t.Errorf("multipart = %+v", ex.Request.Multipart)
	}
	if strings.Contains(ex.URL, "secret-token") || ex.Request.Multipart.Fields["sign"] == "secret-sign" {
		t.Errorf("secrets recorded: %s %v", ex.URL, ex.Request.Multipart.Fields)
	}
}

func TestOffloadErrorIsReturned(t *testing.T) {
	off := offloaderFunc(func(context.Context, *Transfer) (*http.Response, error) {
		return nil, errors.New("worker lost the connection")
	})
	_, err := DoUpload(WithOffloader(context.Background(), off), FileRequest{
		Method: http.MethodPut, URL: "https://example.com/x", Path: "/remote/x", Size: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "worker lost the connection") {
		t.Errorf("err = %v", err)
	}
}

type offloaderFunc func(context.Context, *Transfer) (*http.Response, error)

func (f offloaderFunc) Offload(ctx context.Context, t *Transfer) (*http.Response, error) {
	return f(ctx, t)
}

func (f offloaderFunc) Open(context.Context, FileRef) (io.ReadCloser, error) {
	return nil, errors.New("not here")
}
