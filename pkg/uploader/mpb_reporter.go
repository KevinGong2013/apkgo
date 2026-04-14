package uploader

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"

	"github.com/KevinGong2013/apkgo/pkg/progress"
)


// storeReporter is a progress.Reporter backed by an mpb bar.
//
// A single bar is created upfront for each store. When Phase() is called
// it updates an atomic string shown in the bar's prepend decorator.
// When Total(n) is called with n > 0, the bar switches from spinner mode
// to byte-progress mode. On completion (Finish) the bar is marked done.
type storeReporter struct {
	store   string
	bar     *mpb.Bar
	phase   atomic.Value // string — current phase label
	total   atomic.Int64 // bytes declared by Total()
	hasSize atomic.Bool
}

func (r *storeReporter) Phase(name string) {
	r.phase.Store(name)
}

func (r *storeReporter) Total(bytes int64) {
	if bytes <= 0 {
		return
	}
	r.total.Store(bytes)
	r.hasSize.Store(true)
	// mpb: SetTotal with final=false keeps the bar running
	r.bar.SetTotal(bytes, false)
	// reset current counter when a new phase starts
	r.bar.SetCurrent(0)
}

func (r *storeReporter) Add(bytes int64) {
	r.bar.IncrBy(int(bytes))
}

// markDone finalizes the bar with success or failure text.
func (r *storeReporter) markDone(success bool, errMsg string, duration time.Duration) {
	if success {
		r.phase.Store("✓ done")
	} else {
		if errMsg != "" && len(errMsg) > 60 {
			errMsg = errMsg[:57] + "..."
		}
		r.phase.Store(fmt.Sprintf("✗ %s", errMsg))
	}
	total := r.total.Load()
	if total <= 0 {
		total = 1
	}
	r.bar.SetTotal(total, true)
}

// Manager owns the mpb.Progress container and hands out a Reporter per store.
// A Manager with nil Progress is a no-op (non-TTY / verbose mode fallback).
type Manager struct {
	p         *mpb.Progress
	mu        sync.Mutex
	reporters map[string]*storeReporter
}

// NewManager wraps an existing *mpb.Progress. Pass nil to disable bars.
func NewManager(p *mpb.Progress) *Manager {
	return &Manager{p: p, reporters: map[string]*storeReporter{}}
}

// ReporterFor creates (or returns) the storeReporter for a given store name.
// Safe for concurrent use. When the manager has no *mpb.Progress attached,
// returns a progress.Nop so callers don't need nil checks.
func (m *Manager) ReporterFor(storeName string) progress.Reporter {
	if m == nil || m.p == nil {
		return progress.Nop{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.reporters[storeName]; ok {
		return r
	}

	r := &storeReporter{store: storeName}
	r.phase.Store("pending")

	// Prepend: store name (fixed width) + phase label (fixed width).
	// Append: byte counters / percentage / elapsed. When the upload phase
	// hasn't started yet (no byte total set), the counters slot shows
	// elapsed time so the user can see the store is alive.
	bar := m.p.New(0,
		mpb.BarStyle().Lbound("[").Filler("=").Tip(">").Padding(" ").Rbound("]"),
		mpb.PrependDecorators(
			decor.Name(padRight(storeName, 10), decor.WC{W: 10, C: decor.DindentRight}),
			decor.Any(func(decor.Statistics) string {
				return padRight(r.phase.Load().(string), 14)
			}, decor.WC{W: 14}),
		),
		mpb.AppendDecorators(
			decor.Any(func(s decor.Statistics) string {
				if !r.hasSize.Load() {
					return ""
				}
				return fmt.Sprintf("%7s / %-7s",
					formatBytes(s.Current), formatBytes(s.Total))
			}, decor.WC{W: 18}),
			decor.Any(func(s decor.Statistics) string {
				if !r.hasSize.Load() || s.Total == 0 {
					return "     "
				}
				return fmt.Sprintf("%4.0f%%", float64(s.Current)*100/float64(s.Total))
			}, decor.WC{W: 6}),
			decor.Elapsed(decor.ET_STYLE_MMSS, decor.WC{W: 7}),
		),
	)
	r.bar = bar
	m.reporters[storeName] = r
	return r
}

// MarkDone finalizes the bar for the given store. If the store never had a
// reporter allocated (e.g. early hook failure), MarkDone is a no-op.
func (m *Manager) MarkDone(storeName string, success bool, errMsg string, duration time.Duration) {
	if m == nil || m.p == nil {
		return
	}
	m.mu.Lock()
	r, ok := m.reporters[storeName]
	m.mu.Unlock()
	if !ok {
		return
	}
	r.markDone(success, errMsg, duration)
}

// Wait blocks until all bars are done. No-op when disabled.
func (m *Manager) Wait() {
	if m == nil || m.p == nil {
		return
	}
	m.p.Wait()
}

// LogWriter returns an io.Writer that mpb guarantees will render above the
// progress bars without corrupting them. Callers should route slog output
// through this writer while bars are active.
func (m *Manager) LogWriter() io.Writer {
	if m == nil || m.p == nil {
		return nil
	}
	return m.p
}

// ---- formatting helpers ----

func padRight(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}
	return s + spaces(n-len(s))
}

func spaces(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGT"[exp])
}

