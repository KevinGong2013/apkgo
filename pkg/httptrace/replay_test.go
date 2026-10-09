package httptrace

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A session recorded against a server replays without it, and tells what
// the code under test sent.
func TestReplayRecordedSession(t *testing.T) {
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("method") {
		case "app.update":
			io.WriteString(w, `{"code":0}`)
		case "task.status":
			polls++
			io.WriteString(w, map[bool]string{false: `{"status":2}`, true: `{"status":3}`}[polls > 1])
		}
	}))
	c, rec := traced(t)
	// flow is the "store code": submit, then poll until status 3.
	flow := func(c *http.Client) (string, error) {
		call := func(method string) (string, error) {
			req, _ := http.NewRequest("POST", srv.URL+"/router/rest?method="+method+"&sign=abc", strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			resp, err := c.Do(req)
			if err != nil {
				return "", err
			}
			defer resp.Body.Close()
			b, err := io.ReadAll(resp.Body)
			return string(b), err
		}
		if _, err := call("app.update"); err != nil {
			return "", err
		}
		for {
			body, err := call("task.status")
			if err != nil || strings.Contains(body, `"status":3`) {
				return body, err
			}
		}
	}
	if _, err := flow(c); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	recorded := rec.all()
	if len(recorded) != 3 {
		t.Fatalf("%d exchanges recorded, want 3", len(recorded))
	}

	// Offline now: the same flow gets the same answers, in order.
	rp := Replay(recorded).MatchQuery("method")
	last, err := flow(&http.Client{Transport: rp})
	if err != nil || last != `{"status":3}` {
		t.Fatalf("replayed flow = %q, %v", last, err)
	}
	if left := rp.Remaining(); len(left) != 0 {
		t.Errorf("%d recorded exchanges unused", len(left))
	}
	sent := rp.Sent()
	if len(sent) != 3 || !strings.Contains(sent[0].URL, "method=app.update") || sent[2].Status != 200 || string(sent[1].Request.JSON) != "{}" {
		t.Errorf("sent = %+v", sent)
	}
	if strings.Contains(sent[0].URL, "sign=abc") {
		t.Errorf("replayed request not redacted: %s", sent[0].URL)
	}

	// One call more than was recorded is an error, not a guess.
	req, _ := http.NewRequestWithContext(context.Background(), "POST", srv.URL+"/router/rest?method=task.status", strings.NewReader("{}"))
	if _, err := (&http.Client{Transport: rp}).Do(req); err == nil || !strings.Contains(err.Error(), "no recorded exchange") {
		t.Errorf("extra request: err = %v", err)
	}
}

func TestReplayMatchesAndRebuilds(t *testing.T) {
	rp := Replay([]Exchange{
		{Method: "GET", URL: "https://api.store.test/app/info?pkg=a", Status: 200,
			Response: &Message{Headers: map[string]string{"Content-Type": "application/json", "Content-Length": "999"}, JSON: []byte(`{"v":1}`)}},
		{Method: "POST", URL: "https://api.store.test/app/upd", Status: 403, Response: &Message{Text: "denied"}},
		{Method: "GET", URL: "https://api.store.test/down", Error: "dial tcp: i/o timeout"},
	})
	c := &http.Client{Transport: rp}
	get := func(method, u string) (int, string, error) {
		req, _ := http.NewRequest(method, u, nil)
		resp, err := c.Do(req)
		if err != nil {
			return 0, "", err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b), nil
	}

	// Path decides; the query does unless asked for; order doesn't.
	if code, body, err := get("POST", "https://api.store.test/app/upd"); err != nil || code != 403 || body != "denied" {
		t.Errorf("upd = %d %q %v", code, body, err)
	}
	if code, body, err := get("GET", "https://api.store.test/app/info?pkg=other"); err != nil || code != 200 || body != `{"v":1}` {
		t.Errorf("info = %d %q %v", code, body, err)
	}
	if _, _, err := get("GET", "https://api.store.test/down"); err == nil || !strings.Contains(err.Error(), "i/o timeout") {
		t.Errorf("recorded failure not replayed: %v", err)
	}
	for _, miss := range [][2]string{{"GET", "https://api.store.test/app/info"}, {"GET", "https://other.test/app/upd"}, {"PUT", "https://api.store.test/app/upd"}} {
		if _, _, err := get(miss[0], miss[1]); err == nil {
			t.Errorf("%s %s answered, want no match", miss[0], miss[1])
		}
	}
}
