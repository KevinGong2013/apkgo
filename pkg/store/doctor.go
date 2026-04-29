package store

import "context"

// Probe is one credential / permission check result.
type Probe struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "ok", "fail", "skip"
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

// DiagnoseHint carries optional context (e.g. package name) to enable
// deeper probes that need a specific app to test against.
type DiagnoseHint struct {
	Package string
}

// DiagnoseFn runs readiness probes for one store using its raw config.
// It is responsible for its own auth/setup so that auth failures are
// reported as a probe rather than aborting the whole command.
type DiagnoseFn func(ctx context.Context, cfg map[string]string, hint DiagnoseHint) []Probe

var diagnosers = map[string]DiagnoseFn{}

// RegisterDiagnoser opts a store into `apkgo doctor`.
// Stores without a registered diagnoser are reported as unsupported.
func RegisterDiagnoser(name string, fn DiagnoseFn) {
	diagnosers[name] = fn
}

// Diagnose runs the registered probes for a store. The second return
// value is false when the store has not registered a diagnoser.
func Diagnose(ctx context.Context, name string, cfg map[string]string, hint DiagnoseHint) ([]Probe, bool) {
	fn, ok := diagnosers[name]
	if !ok {
		return nil, false
	}
	return fn(ctx, cfg, hint), true
}
