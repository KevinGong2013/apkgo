package uploader

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/vbauerster/mpb/v8"
)

// syncBuf is a bytes.Buffer safe for concurrent writes from mpb's render
// goroutine and the test goroutine.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestManagerRendersBars exercises the full Manager → mpb pipeline with a
// buffer as the sink. Verifies that store names and phase labels actually
// reach the output, proving the decorators are wired up correctly.
func TestManagerRendersBars(t *testing.T) {
	var buf syncBuf
	p := mpb.New(mpb.WithOutput(&buf), mpb.WithAutoRefresh(), mpb.WithWidth(20))
	m := NewManager(p)

	r := m.ReporterFor("huawei")
	r.Phase("uploading")
	r.Total(1000)
	for range 10 {
		r.Add(100)
	}
	m.MarkDone("huawei", true, "", 0)
	p.Wait()

	out := buf.String()
	if !strings.Contains(out, "huawei") {
		t.Fatalf("rendered output missing store name 'huawei': %q", out)
	}
	if !strings.Contains(out, "uploading") && !strings.Contains(out, "done") {
		t.Fatalf("rendered output missing any phase label: %q", out)
	}
	// The bar should have produced at least one ANSI cursor control sequence.
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("no ANSI escape codes in output — did mpb render? %q", out)
	}
}

// TestManagerDisabled confirms the nil-path (no *mpb.Progress) returns Nop
// reporters that discard events silently.
func TestManagerDisabled(t *testing.T) {
	m := NewManager(nil)
	r := m.ReporterFor("whatever")
	r.Phase("x")
	r.Total(100)
	r.Add(50)
	m.MarkDone("whatever", false, "error", 0)
	m.Wait() // must not block / panic
}
