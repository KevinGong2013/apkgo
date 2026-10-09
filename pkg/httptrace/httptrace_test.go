package httptrace

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// memRecorder keeps exchanges in memory.
type memRecorder struct {
	mu  sync.Mutex
	got []Exchange
}

func (m *memRecorder) Record(ex Exchange) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.got = append(m.got, ex)
}

func (m *memRecorder) all() []Exchange {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Exchange(nil), m.got...)
}

// traced returns a client whose exchanges go to the returned recorder,
// wired the way a store constructor does it.
func traced(t *testing.T) (*http.Client, *memRecorder) {
	t.Helper()
	rec := &memRecorder{}
	cfg, release := Carry(WithRecorder(context.Background(), rec), "demo", map[string]string{"client_id": "id"})
	defer release()
	tr := ForStore("kind", cfg)
	if tr == nil {
		t.Fatal("ForStore returned nil for a carried recorder")
	}
	return tr.Client(&http.Client{}), rec
}

func do(t *testing.T, c *http.Client, req *http.Request) string {
	t.Helper()
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestRecordsJSONExchangeRedacted(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Set-Cookie", "sid=abc")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"ret":{"code":0},"access_token":"tok-123","urlInfo":{"url":"https://obs.example/app.apk?AccessKeyId=AK&Signature=sig%3D&Expires=9"},"list":[{"sign":""},{"sign":"s3cr3t"}]}`)
	}))
	defer srv.Close()
	c, rec := traced(t)

	sent := `{"client_id":"id-1","client_secret":"hunter2","nested":"{\"password\":\"p\",\"keep\":1}","n":12345678901234567890}`
	req, _ := http.NewRequest("POST", srv.URL+"/oauth2/v1/token?appId=42&sign=abcdef", strings.NewReader(sent))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer eyJhbGciOi")
	req.Header.Set("client_id", "id-1")
	body := do(t, c, req)

	// The request and the response reach their ends untouched.
	if gotBody != sent {
		t.Errorf("server got %q", gotBody)
	}
	if !strings.Contains(body, `"access_token":"tok-123"`) {
		t.Errorf("caller got %q", body)
	}

	all := rec.all()
	if len(all) != 1 {
		t.Fatalf("%d exchanges recorded, want 1", len(all))
	}
	ex := all[0]
	if ex.Store != "demo" || ex.Method != "POST" || ex.Status != http.StatusCreated || ex.Error != "" || ex.Time.IsZero() {
		t.Errorf("exchange = %+v", ex)
	}
	if u, _ := url.Parse(ex.URL); u.Query().Get("appId") != "42" || u.Query().Get("sign") != "[REDACTED 6]" || u.Path != "/oauth2/v1/token" {
		t.Errorf("url = %s", ex.URL)
	}
	if h := ex.Request.Headers; h["Authorization"] != "Bearer [REDACTED 10]" || h["Client_id"] != "id-1" {
		t.Errorf("request headers = %v", h)
	}
	if h := ex.Response.Headers; h["Set-Cookie"] != "[REDACTED 7]" || h["Content-Type"] != "application/json" {
		t.Errorf("response headers = %v", h)
	}

	var reqDoc, respDoc map[string]any
	if err := json.Unmarshal(ex.Request.JSON, &reqDoc); err != nil {
		t.Fatalf("request json %s: %v", ex.Request.JSON, err)
	}
	if reqDoc["client_id"] != "id-1" || reqDoc["client_secret"] != "[REDACTED 7]" || reqDoc["nested"] != `{"keep":1,"password":"[REDACTED 1]"}` {
		t.Errorf("request json = %s", ex.Request.JSON)
	}
	// Numbers keep their digits.
	if !strings.Contains(string(ex.Request.JSON), "12345678901234567890") {
		t.Errorf("big number mangled: %s", ex.Request.JSON)
	}
	if err := json.Unmarshal(ex.Response.JSON, &respDoc); err != nil {
		t.Fatal(err)
	}
	if respDoc["access_token"] != "[REDACTED 7]" {
		t.Errorf("response token = %v", respDoc["access_token"])
	}
	signed := respDoc["urlInfo"].(map[string]any)["url"].(string)
	if u, _ := url.Parse(signed); u.Query().Get("Signature") != "[REDACTED 4]" || u.Query().Get("AccessKeyId") != "AK" || u.Query().Get("Expires") != "9" {
		t.Errorf("signed url = %s", signed)
	}
	// An empty secret stays visibly empty.
	list := respDoc["list"].([]any)
	if list[0].(map[string]any)["sign"] != "" || list[1].(map[string]any)["sign"] != "[REDACTED 6]" {
		t.Errorf("signs = %v", list)
	}
	if ex.Request.Bytes != int64(len(sent)) || ex.Response.Bytes == 0 {
		t.Errorf("sizes = %d / %d", ex.Request.Bytes, ex.Response.Bytes)
	}
	for _, leak := range []string{"hunter2", "tok-123", "eyJhbGciOi", "s3cr3t", "abcdef"} {
		if line, _ := json.Marshal(ex); strings.Contains(string(line), leak) {
			t.Errorf("secret %q leaked into the record: %s", leak, line)
		}
	}
}

func TestRecordsOtherBodyShapes(t *testing.T) {
	big := strings.Repeat("好", MaxBody) // 3 bytes each: well over the cap
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		switch r.URL.Path {
		case "/form":
			// JSON with no Content-Type, as several stores answer.
			w.Header()["Content-Type"] = nil
			io.WriteString(w, `{"errno":0,"data":{"token":"t"}}`)
		case "/png":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte{0x89, 'P', 'N', 'G', 0xff, 0xfe})
		case "/big":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			io.WriteString(w, big)
		case "/html":
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, `<a href="/x?access_token=abc123&page=2">next</a>`)
		}
	}))
	defer srv.Close()
	c, rec := traced(t)

	form := url.Values{"client_secret": {"s"}, "pkg_name": {"com.example"}, "apk_url": {`[{"url":"https://cdn.example/a.apk?sign=zz","md5":"m"}]`}}
	req, _ := http.NewRequest("POST", srv.URL+"/form", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	do(t, c, req)

	// An APK going up: binary, never read into the record.
	apk := bytes.Repeat([]byte{0, 1, 2, 3}, 1024)
	req, _ = http.NewRequest("PUT", srv.URL+"/png", bytes.NewReader(apk))
	req.Header.Set("Content-Type", "application/octet-stream")
	do(t, c, req)

	req, _ = http.NewRequest("GET", srv.URL+"/big", nil)
	if got := do(t, c, req); got != big {
		t.Errorf("caller got %d bytes of the big body, want %d", len(got), len(big))
	}
	req, _ = http.NewRequest("GET", srv.URL+"/html", nil)
	do(t, c, req)

	all := rec.all()
	if len(all) != 4 {
		t.Fatalf("%d exchanges, want 4", len(all))
	}
	f := all[0]
	if got := f.Request.Form; got["client_secret"][0] != "[REDACTED 1]" || got["pkg_name"][0] != "com.example" ||
		!strings.Contains(got["apk_url"][0], "sign=%5BREDACTED+2%5D") || !strings.Contains(got["apk_url"][0], `"md5":"m"`) {
		t.Errorf("form = %v", got)
	}
	if string(f.Response.JSON) != `{"data":{"token":"[REDACTED 1]"},"errno":0}` {
		t.Errorf("untyped json response = %s / %q", f.Response.JSON, f.Response.Text)
	}

	p := all[1]
	if p.Request.Omitted != "binary" || p.Request.Bytes != int64(len(apk)) || p.Request.Text != "" || p.Request.JSON != nil {
		t.Errorf("binary request = %+v", p.Request)
	}
	if p.Response.Omitted != "binary" || p.Response.Text != "" {
		t.Errorf("binary response = %+v", p.Response)
	}

	b := all[2]
	if !b.Response.Truncated || b.Response.Bytes != int64(len(big)) || len(b.Response.Text) > MaxBody || len(b.Response.Text) < MaxBody-4 {
		t.Errorf("big response: truncated=%v bytes=%d kept=%d", b.Response.Truncated, b.Response.Bytes, len(b.Response.Text))
	}
	if !strings.HasSuffix(b.Response.Text, "好") {
		t.Error("truncated text ends inside a character")
	}

	if h := all[3].Response.Text; h != `<a href="/x?access_token=[REDACTED]&page=2">next</a>` {
		t.Errorf("html = %s", h)
	}
}

func TestRecordsMultipartAsDescription(t *testing.T) {
	var gotLen int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotLen = len(b)
		io.WriteString(w, "ok")
	}))
	defer srv.Close()
	c, rec := traced(t)

	payload := bytes.Repeat([]byte("x"), 100_000)
	ctx := WithMultipart(context.Background(),
		map[string]string{"packageName": "com.example", "access_token": "tok"},
		[]FilePart{{Field: "file", Name: "app.apk", Size: 99_000}})
	req, _ := http.NewRequestWithContext(ctx, "POST", srv.URL+"/upload", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xyz")
	if do(t, c, req) != "ok" || gotLen != len(payload) {
		t.Fatalf("upload disturbed: server got %d bytes", gotLen)
	}

	m := rec.all()[0].Request
	if m.Multipart == nil || m.Text != "" || m.Omitted != "" {
		t.Fatalf("request = %+v", m)
	}
	if m.Multipart.Fields["packageName"] != "com.example" || m.Multipart.Fields["access_token"] != "[REDACTED 3]" {
		t.Errorf("fields = %v", m.Multipart.Fields)
	}
	if len(m.Multipart.Files) != 1 || m.Multipart.Files[0] != (FilePart{Field: "file", Name: "app.apk", Size: 99_000}) {
		t.Errorf("files = %+v", m.Multipart.Files)
	}

	// Multipart nobody described is not looked at either.
	req, _ = http.NewRequest("POST", srv.URL+"/upload", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xyz")
	do(t, c, req)
	if m := rec.all()[1].Request; m.Omitted != "binary" || m.Bytes != int64(len(payload)) || m.Multipart != nil {
		t.Errorf("undescribed multipart = %+v", m)
	}
}

func TestRecordsTransportFailure(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := srv.URL
	srv.Close() // nothing listens there any more
	c, rec := traced(t)

	req, _ := http.NewRequest("POST", addr+"/x?client_secret=topsecret", strings.NewReader(`{"a":1}`))
	req.Header.Set("Content-Type", "application/json")
	if _, err := c.Do(req); err == nil {
		t.Fatal("request to a closed server succeeded")
	}
	all := rec.all()
	if len(all) != 1 || all[0].Error == "" || all[0].Status != 0 || all[0].Response != nil {
		t.Fatalf("exchanges = %+v", all)
	}
	if strings.Contains(all[0].Error, "topsecret") || strings.Contains(all[0].URL, "topsecret") {
		t.Errorf("secret in the failure record: %s / %s", all[0].Error, all[0].URL)
	}
}

func TestNoRecorderMeansNoTracing(t *testing.T) {
	cfg := map[string]string{"client_id": "id"}
	out, release := Carry(context.Background(), "demo", cfg)
	release()
	if _, carried := out[ConfigKey]; carried || len(out) != 1 {
		t.Errorf("config changed without a recorder: %v", out)
	}
	tr := ForStore("demo", out)
	if tr != nil {
		t.Fatal("tracer without a recorder")
	}
	// Every method of the nil Tracer leaves things as they were.
	c := &http.Client{}
	if tr.Client(c) != c || c.Transport != nil || tr.NewClient(time.Second) != nil {
		t.Error("nil Tracer touched the client")
	}

	// A handle is only good until it is released.
	out, release = Carry(WithRecorder(context.Background(), &memRecorder{}), "demo", cfg)
	if ForStore("demo", out) == nil {
		t.Error("live handle not honoured")
	}
	release()
	if ForStore("demo", out) != nil {
		t.Error("released handle still honoured")
	}
	if _, touched := cfg[ConfigKey]; touched {
		t.Error("Carry wrote into the caller's config")
	}

	// The key is reserved: a config can't name somebody else's handle.
	live, releaseLive := Carry(WithRecorder(context.Background(), &memRecorder{}), "victim", cfg)
	defer releaseLive()
	forged := map[string]string{"client_id": "id", ConfigKey: live[ConfigKey]}
	out, release = Carry(context.Background(), "attacker", forged)
	release()
	if _, kept := out[ConfigKey]; kept || ForStore("attacker", out) != nil {
		t.Error("a handle supplied in the config reached the constructor")
	}
	if len(live[ConfigKey]) < 32 {
		t.Errorf("handle %q is guessable", live[ConfigKey])
	}
}

// Callers that pass a nil context (the store dispatchers never required
// one) get no tracing — not a panic.
func TestNilContext(t *testing.T) {
	//nolint:staticcheck // nil contexts are the point
	var ctx context.Context
	if FromContext(ctx) != nil {
		t.Error("recorder from a nil context")
	}
	cfg := map[string]string{"client_id": "id"}
	out, release := Carry(ctx, "demo", cfg)
	release()
	if len(out) != 1 || ForStore("demo", out) != nil {
		t.Errorf("Carry(nil ctx) = %v", out)
	}
	if WithMultipart(ctx, nil, nil) != nil {
		t.Error("WithMultipart made a context out of nil")
	}
}

func TestIsSensitive(t *testing.T) {
	for _, name := range []string{
		"client_secret", "access_token", "accessToken", "Authorization", "api_sign", "sign", "SIG",
		"X-Amz-Signature", "q-signature", "x-cos-security-token", "_api_key", "api_token", "password",
		"Set-Cookie", "assertion", "private_key", "X-Amz-Credential", "id_token",
	} {
		if !IsSensitive(name) {
			t.Errorf("%q should be redacted", name)
		}
	}
	for _, name := range []string{
		"client_id", "appId", "packageName", "versionCode", "access_key", "AccessKeyId", "design",
		"timestamp", "method", "downloadFileName", "key", "imageResolutionSingature", "Content-Type",
		"token_type", "Access-Control-Allow-Credentials",
	} {
		if IsSensitive(name) {
			t.Errorf("%q should be kept", name)
		}
	}
}

func TestFileRecorderRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traces", "job.jsonl")
	rec, err := NewFileRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	rec.Record(Exchange{Store: "oppo", Method: "GET", URL: "https://x/a", Status: 200, Response: &Message{JSON: json.RawMessage(`{"a":1}`)}})
	rec.Record(Exchange{Store: "vivo", Method: "POST", URL: "https://x/b", Error: "dial tcp: refused"})
	if err := rec.Close(); err != nil || rec.Err() != nil {
		t.Fatalf("close: %v, write: %v", err, rec.Err())
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("trace file mode = %v, %v; want 0600", fi.Mode().Perm(), err)
	}

	// Appending continues the same file.
	rec, _ = NewFileRecorder(path)
	rec.Record(Exchange{Store: "oppo", Method: "GET", URL: "https://x/c"})
	rec.Close()

	got, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Seq != 1 || got[1].Seq != 2 || got[1].Error == "" || string(got[0].Response.JSON) != `{"a":1}` || got[2].URL != "https://x/c" {
		t.Errorf("read back %+v", got)
	}
}
