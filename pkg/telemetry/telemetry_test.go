package telemetry

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestFlushWaitsForInflightSend(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond) // slower than an immediate os.Exit
		received.Add(1)
	}))
	defer srv.Close()

	old := endpoint
	endpoint = srv.URL
	defer func() { endpoint = old }()

	Send(Event{Event: "upload", Source: "cli"})
	Flush(2 * time.Second)

	if got := received.Load(); got != 1 {
		t.Fatalf("received %d events after Flush, want 1", got)
	}
}

func TestFlushGivesUpAfterTimeout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer srv.Close()
	defer close(release)

	old := endpoint
	endpoint = srv.URL
	defer func() { endpoint = old }()

	Send(Event{Event: "upload", Source: "cli"})
	start := time.Now()
	Flush(100 * time.Millisecond)
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Flush blocked %v, want ~100ms", d)
	}
}
