package httptrace

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Replayer is an http.RoundTripper that answers requests from a recording
// instead of the network, so a session captured against a real store can
// drive a test of that store's code: install it as the transport of the
// store's HTTP clients, run the flow, then check Sent and Remaining.
//
// Each request is answered by the first recorded exchange not yet used
// that has the same method, host and path (and the same values for the
// query parameters named with MatchQuery). Recorded secrets are redaction
// markers; the code under test just passes them along.
type Replayer struct {
	mu        sync.Mutex
	exchanges []Exchange
	used      []bool
	queryKeys []string
	sent      []Exchange
}

// Replay returns a Replayer over exchanges, in recorded order.
func Replay(exchanges []Exchange) *Replayer {
	return &Replayer{exchanges: exchanges, used: make([]bool, len(exchanges))}
}

// MatchQuery makes the named query parameters part of what identifies a
// request — for RPC-style APIs that serve every call from one path and
// tell them apart by a parameter (vivo's `method`). It returns r.
func (r *Replayer) MatchQuery(keys ...string) *Replayer {
	r.queryKeys = append(r.queryKeys, keys...)
	return r
}

// Sent returns what was requested through r so far, recorded the same way
// a live trace is (redacted), with the status that was replayed.
func (r *Replayer) Sent() []Exchange {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Exchange(nil), r.sent...)
}

// Remaining returns the recorded exchanges no request has used.
func (r *Replayer) Remaining() []Exchange {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Exchange
	for i, ex := range r.exchanges {
		if !r.used[i] {
			out = append(out, ex)
		}
	}
	return out
}

func (r *Replayer) RoundTrip(req *http.Request) (*http.Response, error) {
	sent := Exchange{Time: time.Now(), Method: req.Method, URL: redactURL(req.URL)}
	sent.Request.Headers = redactHeaders(req.Header)
	if mp, ok := req.Context().Value(multipartKey{}).(*Multipart); ok {
		sent.Request.Multipart = mp.redacted()
	}
	if req.Body != nil && req.Body != http.NoBody {
		data, err := io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, err
		}
		sent.Request.Bytes = int64(len(data))
		if sent.Request.Multipart == nil {
			sent.Request.setBody(req.Header.Get("Content-Type"), data, false)
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for i, ex := range r.exchanges {
		if r.used[i] || !r.matches(ex, req) {
			continue
		}
		r.used[i] = true
		sent.Store, sent.Status, sent.Error = ex.Store, ex.Status, ex.Error
		r.sent = append(r.sent, sent)
		if ex.Error != "" {
			return nil, errors.New(ex.Error)
		}
		return ex.response(req), nil
	}
	sent.Error = "no recorded exchange"
	r.sent = append(r.sent, sent)
	return nil, fmt.Errorf("httptrace: no recorded exchange left for %s %s", req.Method, sent.URL)
}

func (r *Replayer) matches(ex Exchange, req *http.Request) bool {
	if ex.Method != req.Method {
		return false
	}
	u, err := url.Parse(ex.URL)
	if err != nil || u.Host != req.URL.Host || u.Path != req.URL.Path {
		return false
	}
	recorded, sent := u.Query(), req.URL.Query()
	for _, k := range r.queryKeys {
		if recorded.Get(k) != sent.Get(k) {
			return false
		}
	}
	return true
}

// response rebuilds the recorded response. A body that was truncated or
// omitted when recorded comes back as what was kept (possibly nothing).
func (ex Exchange) response(req *http.Request) *http.Response {
	resp := &http.Response{
		StatusCode: ex.Status,
		Status:     fmt.Sprintf("%d %s", ex.Status, http.StatusText(ex.Status)),
		Proto:      "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header:  http.Header{},
		Request: req,
	}
	var body []byte
	if m := ex.Response; m != nil {
		for k, v := range m.Headers {
			// The body below is neither compressed nor of the recorded length.
			if strings.EqualFold(k, "Content-Length") || strings.EqualFold(k, "Content-Encoding") || strings.EqualFold(k, "Transfer-Encoding") {
				continue
			}
			resp.Header.Set(k, v)
		}
		switch {
		case len(m.JSON) > 0:
			body = m.JSON
		case m.Form != nil:
			body = []byte(url.Values(m.Form).Encode())
		default:
			body = []byte(m.Text)
		}
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	return resp
}

// ReplayFile returns a Replayer over the trace file at path — typically a
// fixture under a store's testdata/, cut from a real recording.
func ReplayFile(path string) (*Replayer, error) {
	exchanges, err := ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(exchanges) == 0 {
		return nil, fmt.Errorf("%s: no exchanges", path)
	}
	return Replay(exchanges), nil
}
