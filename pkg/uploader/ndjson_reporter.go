package uploader

import (
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/KevinGong2013/apkgo/pkg/apk"
	"github.com/KevinGong2013/apkgo/pkg/progress"
	"github.com/KevinGong2013/apkgo/pkg/store"
)

// NDJSONManager emits newline-delimited JSON progress events to a
// writer (typically os.Stdout). Designed for parent processes that
// fork apkgo and read its output line-by-line.
//
// Event shapes (one JSON object per line):
//
//	{"type":"start", ...caller-supplied metadata}
//	{"type":"phase","store":"huawei","phase":"uploading"}
//	{"type":"total","store":"huawei","total_bytes":62914560}
//	{"type":"bytes","store":"huawei","sent":1048576,"total":62914560}
//	{"type":"result","store":"huawei","success":true,"duration_ms":34570}
//	{"type":"done", ...caller-supplied summary}
//
// "bytes" events are throttled to at most one per ~100 ms per store
// (plus a final event on each Total transition) so consumers don't
// drown in high-frequency updates from large file copies.
type NDJSONManager struct {
	enc       *json.Encoder
	mu        sync.Mutex
	reporters map[string]*ndjsonReporter
}

// NewNDJSONManager wraps w. Concurrency-safe.
func NewNDJSONManager(w io.Writer) *NDJSONManager {
	return &NDJSONManager{
		enc:       json.NewEncoder(w),
		reporters: map[string]*ndjsonReporter{},
	}
}

// Emit publishes an arbitrary event verbatim. Callers use this for
// "start" / "done" / custom event types that aren't tied to a single
// store's progress.
func (m *NDJSONManager) Emit(v any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_ = m.enc.Encode(v)
}

func (m *NDJSONManager) ReporterFor(storeName string) progress.Reporter {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.reporters[storeName]; ok {
		return r
	}
	r := &ndjsonReporter{store: storeName, mgr: m}
	m.reporters[storeName] = r
	return r
}

func (m *NDJSONManager) MarkDone(storeName string, success bool, errMsg string, duration time.Duration) {
	evt := map[string]any{
		"type":        "result",
		"store":       storeName,
		"success":     success,
		"duration_ms": duration.Milliseconds(),
	}
	if errMsg != "" {
		evt["error"] = errMsg
	}
	m.Emit(evt)
}

// Wait is a no-op — events emit synchronously as they arrive, so by
// the time the upload returns there's nothing pending to flush.
func (m *NDJSONManager) Wait() {}

// Start emits the run's opening event with apk metadata + target
// store list. Called once before any per-store work begins.
func (m *NDJSONManager) Start(info *apk.Info, stores []string) {
	m.Emit(map[string]any{
		"type":   "start",
		"apk":    info,
		"stores": stores,
	})
}

// Done emits the terminal event with the same payload the legacy
// post-completion JSON dump would have. Consumers tracking for the
// terminal event look for type=="done".
func (m *NDJSONManager) Done(info *apk.Info, results []*store.UploadResult) {
	m.Emit(map[string]any{
		"type":    "done",
		"apk":     info,
		"results": results,
	})
}

// ndjsonReporter is a progress.Reporter that publishes events through
// the parent NDJSONManager's encoder. Throttling state is per-store
// and protected by ndjsonReporter.mu (the manager-level mutex only
// guards the json.Encoder, not the throttle bookkeeping).
type ndjsonReporter struct {
	store string
	mgr   *NDJSONManager

	mu       sync.Mutex
	phase    string
	total    int64
	sent     int64
	lastEmit time.Time
}

const bytesEventInterval = 100 * time.Millisecond

func (r *ndjsonReporter) Phase(name string) {
	r.mu.Lock()
	r.phase = name
	r.sent = 0
	r.total = 0
	r.lastEmit = time.Time{}
	r.mu.Unlock()
	r.mgr.Emit(map[string]any{
		"type":  "phase",
		"store": r.store,
		"phase": name,
	})
}

func (r *ndjsonReporter) Total(n int64) {
	r.mu.Lock()
	r.total = n
	r.mu.Unlock()
	if n <= 0 {
		return
	}
	r.mgr.Emit(map[string]any{
		"type":        "total",
		"store":       r.store,
		"total_bytes": n,
	})
}

func (r *ndjsonReporter) Add(n int64) {
	r.mu.Lock()
	r.sent += n
	now := time.Now()
	// Emit on first byte (lastEmit is zero), every ~100 ms after that,
	// and again when the total is reached so consumers see a final
	// 100% event.
	shouldEmit := r.lastEmit.IsZero() ||
		now.Sub(r.lastEmit) >= bytesEventInterval ||
		(r.total > 0 && r.sent >= r.total)
	if shouldEmit {
		r.lastEmit = now
	}
	sent, total := r.sent, r.total
	r.mu.Unlock()

	if !shouldEmit {
		return
	}
	evt := map[string]any{
		"type":  "bytes",
		"store": r.store,
		"sent":  sent,
	}
	if total > 0 {
		evt["total"] = total
	}
	r.mgr.Emit(evt)
}
