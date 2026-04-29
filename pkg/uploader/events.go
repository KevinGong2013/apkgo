package uploader

import (
	"time"

	"github.com/KevinGong2013/apkgo/pkg/store"
)

// EventType discriminates the lifecycle moment an Event represents.
// Cloud orchestrators wire EventRecorder to a metrics backend
// (Prometheus, OTel, Datadog, etc.) and switch on Type.
type EventType string

const (
	// EventStoreStart fires once per store at the beginning of the
	// uploader's per-store goroutine, after the before-hook has
	// finished (success or skipped). Useful for "in-flight uploads"
	// gauge increments.
	EventStoreStart EventType = "store.start"

	// EventStoreEnd fires once per store with the final UploadResult
	// after the after-hook has finished. Useful for completion
	// counters and duration histograms.
	EventStoreEnd EventType = "store.end"

	// EventHookRun fires when a per-store before/after hook completes
	// (failure or success). Hook is which one; Err is non-nil on
	// failure.
	EventHookRun EventType = "hook.run"
)

// Event is a single lifecycle event the uploader emits.
type Event struct {
	Type     EventType
	Store    string             // store name, always set
	Hook     string             // "before" or "after", set for EventHookRun
	Duration time.Duration      // wall-clock duration of the phase
	Result   *store.UploadResult // set for EventStoreEnd
	Err      error              // set when an action failed
}

// EventRecorder is a callback the uploader fires on lifecycle events.
// Implementations should be non-blocking (fire-and-forget into a
// channel or atomic counter) — the uploader runs the recorder
// inline on its goroutines, so a slow recorder slows uploads.
//
// nil is a no-op.
type EventRecorder func(ev Event)

func (r EventRecorder) emit(ev Event) {
	if r == nil {
		return
	}
	r(ev)
}
