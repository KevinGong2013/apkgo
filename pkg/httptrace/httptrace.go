// Package httptrace records the HTTP exchanges between apkgo and the app
// stores, so a failed or surprising publish can be examined afterwards —
// what was sent, what the store answered — and replays such recordings in
// tests.
//
// Recording is off unless the caller asks for it:
//
//	rec, _ := httptrace.NewFileRecorder("trace.jsonl")
//	defer rec.Close()
//	ctx = httptrace.WithRecorder(ctx, rec)
//	// … store.CreateContext(ctx, …), store.QueryAudit(ctx, …), apkgo.Run(ctx, …)
//
// A store constructor only receives its config map, so the recorder
// travels there as an opaque handle (ConfigKey, put in by Carry — the
// store registry does this) and the constructor picks it up with ForStore.
// Without a handle ForStore returns nil, every Tracer method is then a
// no-op, and the store's HTTP clients are left exactly as they were.
//
// What is recorded: method, URL, headers and bodies of both directions,
// status, timing and transport errors. Credentials are redacted (see
// redact.go) and file payloads are never read: an upload is recorded as
// its form fields plus each file's name and size. Redaction works on key
// names and is best-effort — treat a trace as sensitive and keep it local.
package httptrace

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MaxBody is how much of one text body is kept; the rest is dropped and
// the message marked Truncated.
const MaxBody = 256 << 10

// Recorder receives each exchange once it has completed (the response
// body was read to the end or closed, or the round trip failed). Record
// may be called from several goroutines at once.
type Recorder interface {
	Record(Exchange)
}

// RecorderFunc adapts a function to Recorder.
type RecorderFunc func(Exchange)

func (f RecorderFunc) Record(ex Exchange) { f(ex) }

type recorderKey struct{}

// WithRecorder returns a context that asks for the HTTP exchanges of the
// stores created or queried under it to be sent to rec.
func WithRecorder(ctx context.Context, rec Recorder) context.Context {
	if rec == nil {
		return ctx
	}
	return context.WithValue(ctx, recorderKey{}, rec)
}

// FromContext returns the recorder WithRecorder put in ctx, or nil.
func FromContext(ctx context.Context) Recorder {
	rec, _ := ctx.Value(recorderKey{}).(Recorder)
	return rec
}

// ConfigKey is the reserved store-config key under which Carry hands a
// recorder to a store constructor. Its value is an opaque handle, valid
// only until the release function Carry returned is called.
const ConfigKey = "_http_trace"

type replayKey struct{}

// WithReplay returns a context under which the stores created or queried
// answer their HTTP requests from rp instead of the network — including
// the sign-in a store constructor performs. This is how a recording
// drives a test:
//
//	rp := httptrace.Replay(exchanges)
//	res, _ := store.QueryAudit(httptrace.WithReplay(ctx, rp), "vivo", cfg, q)
//
// Combined with WithRecorder, what the store sends to rp is recorded.
func WithReplay(ctx context.Context, rp *Replayer) context.Context {
	if rp == nil {
		return ctx
	}
	return context.WithValue(ctx, replayKey{}, rp)
}

type binding struct {
	rec    Recorder
	replay *Replayer
	name   string
}

var (
	handles   sync.Map // handle → binding
	handleSeq atomic.Uint64
)

// Carry returns a copy of cfg holding a handle to ctx's recorder (and
// replayer), for a store constructor to pick up with ForStore; name is the
// store's configured name, used to label its exchanges. Call release once
// the store has been constructed. When ctx carries neither, cfg is
// returned unchanged.
func Carry(ctx context.Context, name string, cfg map[string]string) (out map[string]string, release func()) {
	rec := FromContext(ctx)
	rp, _ := ctx.Value(replayKey{}).(*Replayer)
	if rec == nil && rp == nil {
		return cfg, func() {}
	}
	handle := strconv.FormatUint(handleSeq.Add(1), 36)
	handles.Store(handle, binding{rec: rec, replay: rp, name: name})
	out = make(map[string]string, len(cfg)+1)
	for k, v := range cfg {
		out[k] = v
	}
	out[ConfigKey] = handle
	return out, func() { handles.Delete(handle) }
}

// Tracer records the exchanges of one store instance (and, under
// WithReplay, serves them from a recording). The nil Tracer does nothing;
// all its methods are safe to call.
type Tracer struct {
	store  string
	rec    Recorder  // nil: replay only
	replay *Replayer // nil: the real network
}

// ForStore returns the Tracer for a store being constructed from cfg, or
// nil when nothing was carried into cfg. kind labels the exchanges when
// the carrier gave no name.
func ForStore(kind string, cfg map[string]string) *Tracer {
	handle := cfg[ConfigKey]
	if handle == "" {
		return nil
	}
	v, ok := handles.Load(handle)
	if !ok {
		return nil
	}
	b := v.(binding)
	if b.name == "" {
		b.name = kind
	}
	return &Tracer{store: b.name, rec: b.rec, replay: b.replay}
}

// Client makes c's requests recorded (or replayed) and returns c. Call it
// after any setup that needs c's transport to be an *http.Transport
// (proxy, TLS settings): the transport is wrapped from here on.
func (t *Tracer) Client(c *http.Client) *http.Client {
	if t == nil || c == nil {
		return c
	}
	base := c.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	if t.replay != nil {
		base = t.replay
	}
	if t.rec == nil {
		c.Transport = base
		return c
	}
	c.Transport = &transport{base: base, tracer: t}
	return c
}

// NewClient returns a recorded client with the given timeout — for a
// store's raw (non-API) requests such as file uploads — or nil for the nil
// Tracer, so the caller's untraced default stays in use.
func (t *Tracer) NewClient(timeout time.Duration) *http.Client {
	if t == nil {
		return nil
	}
	return t.Client(&http.Client{Timeout: timeout})
}

type multipartKey struct{}

// WithMultipart notes on ctx what a streamed multipart request is made
// of, so it can be recorded without reading the stream: the form fields,
// and each file's field name, file name and size.
func WithMultipart(ctx context.Context, fields map[string]string, files []FilePart) context.Context {
	return context.WithValue(ctx, multipartKey{}, &Multipart{Fields: fields, Files: files})
}

type transport struct {
	base   http.RoundTripper
	tracer *Tracer
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	ex := Exchange{
		Time:   time.Now(),
		Store:  t.tracer.store,
		Method: req.Method,
		URL:    redactURL(req.URL),
	}
	ex.Request.Headers = redactHeaders(req.Header)
	sent, body := captureRequest(req, &ex.Request)
	if body != nil {
		// A RoundTripper must not modify the caller's request: send a
		// shallow copy carrying the capturing body instead.
		r2 := new(http.Request)
		*r2 = *req
		r2.Body = body
		req = r2
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		sent.finish(&ex.Request)
		ex.DurationMS = time.Since(ex.Time).Milliseconds()
		ex.Error = redactError(err)
		t.tracer.rec.Record(ex)
		return nil, err
	}

	ex.Status = resp.StatusCode
	ex.Response = &Message{Headers: redactHeaders(resp.Header)}
	got := newCapture(resp.Header.Get("Content-Type"), resp.ContentLength)
	var once sync.Once
	resp.Body = &tee{ReadCloser: resp.Body, capture: got, done: func() {
		once.Do(func() {
			sent.finish(&ex.Request)
			got.finish(ex.Response)
			ex.DurationMS = time.Since(ex.Time).Milliseconds()
			t.tracer.rec.Record(ex)
		})
	}}
	return resp, nil
}

// captureRequest decides how req's body is recorded. It returns the
// capture to finish once the request has been sent, and — when the body
// has to be observed on its way out — the body to send in its place.
func captureRequest(req *http.Request, msg *Message) (*capture, io.ReadCloser) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	contentType := req.Header.Get("Content-Type")
	if mp, ok := req.Context().Value(multipartKey{}).(*Multipart); ok {
		msg.Multipart = mp.redacted()
		msg.Bytes = req.ContentLength
		return nil, nil
	}
	c := newCapture(contentType, req.ContentLength)
	if !c.wanted {
		// A file or other binary payload: never read, only described.
		c.finish(msg)
		return nil, nil
	}
	return c, &tee{ReadCloser: req.Body, capture: c}
}

// capture collects the start of a body as it streams by.
type capture struct {
	contentType string
	declared    int64 // Content-Length, -1 when unknown
	wanted      bool  // false: the body is not looked at

	mu    sync.Mutex
	buf   bytes.Buffer
	total int64
}

func newCapture(contentType string, declared int64) *capture {
	c := &capture{contentType: contentType, declared: declared}
	switch kind := bodyKind(contentType); {
	case kind == kindText:
		c.wanted = true
	case kind == kindUnknown:
		// No usable Content-Type (several stores answer JSON that way):
		// look at it unless it is known to be large.
		c.wanted = declared <= MaxBody
	}
	return c
}

func (c *capture) write(p []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.total += int64(len(p))
	if room := MaxBody - c.buf.Len(); room > 0 {
		if len(p) > room {
			p = p[:room]
		}
		c.buf.Write(p)
	}
}

// finish fills msg from what was captured. Safe on a nil capture.
func (c *capture) finish(msg *Message) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.wanted {
		msg.Bytes = c.declared
		msg.Omitted = "binary"
		return
	}
	msg.Bytes = c.total
	msg.setBody(c.contentType, c.buf.Bytes(), c.total > int64(c.buf.Len()))
}

// tee passes a body through while feeding a capture, and calls done once
// when the body is exhausted or closed.
type tee struct {
	io.ReadCloser
	capture *capture
	done    func()
}

func (t *tee) Read(p []byte) (int, error) {
	n, err := t.ReadCloser.Read(p)
	if n > 0 && t.capture.wanted {
		t.capture.write(p[:n])
	}
	if err != nil && t.done != nil {
		t.done()
	}
	return n, err
}

func (t *tee) Close() error {
	err := t.ReadCloser.Close()
	if t.done != nil {
		t.done()
	}
	return err
}

const (
	kindUnknown = iota
	kindText
	kindBinary
)

// bodyKind classifies a Content-Type: text is recorded, binary is only
// described, unknown is decided by looking at the bytes.
func bodyKind(contentType string) int {
	if strings.TrimSpace(contentType) == "" {
		return kindUnknown
	}
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return kindUnknown
	}
	switch {
	case strings.HasPrefix(mt, "text/"),
		mt == "application/json", strings.HasSuffix(mt, "+json"),
		mt == "application/xml", strings.HasSuffix(mt, "+xml"),
		mt == "application/x-www-form-urlencoded",
		mt == "application/javascript":
		return kindText
	}
	return kindBinary
}

// redactError renders a transport error without the request URL's query
// (a *url.Error prints the whole URL, signatures included).
func redactError(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		u := ue.URL
		if parsed, perr := url.Parse(u); perr == nil {
			u = redactURL(parsed)
		} else if i := strings.IndexByte(u, '?'); i >= 0 {
			u = u[:i]
		}
		return ue.Op + " " + strconv.Quote(u) + ": " + scrubText(ue.Err.Error())
	}
	return scrubText(err.Error())
}
