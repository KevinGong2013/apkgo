package uploader

import (
	"time"

	"github.com/KevinGong2013/apkgo/pkg/progress"
)

// ProgressManager is the surface used by Uploader to publish progress
// events to whatever output channel is in effect (mpb terminal bars,
// NDJSON stream on stdout, or nothing). Both *Manager (mpb) and
// *NDJSONManager satisfy it.
type ProgressManager interface {
	// ReporterFor returns the progress.Reporter that the named store
	// should pass into UploadRequest.Progress. The same store must
	// receive the same reporter across calls.
	ReporterFor(storeName string) progress.Reporter

	// MarkDone is called once per store after the upload (and any
	// after-hook) completes. It carries the final success/error state
	// for the store.
	MarkDone(storeName string, success bool, errMsg string, duration time.Duration)

	// Wait blocks until all per-store output has flushed. mpb needs
	// this to render the final frame; NDJSON / nop are no-ops.
	Wait()
}

// NopManager swallows every event. Used when the operator hasn't
// asked for any progress output (e.g. piped JSON output mode).
var NopManager ProgressManager = nopManager{}

type nopManager struct{}

func (nopManager) ReporterFor(string) progress.Reporter                    { return progress.Nop{} }
func (nopManager) MarkDone(string, bool, string, time.Duration)            {}
func (nopManager) Wait()                                                   {}
