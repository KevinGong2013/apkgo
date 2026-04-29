package store

import "context"

// Probe is one credential / permission check result.
//
// Detail is a PII-free brief shown by default (e.g. "account active",
// "version=1.2.3 build=4567"). Anything that could reveal email,
// real name, or otherwise be sensitive when copy-pasted into an issue
// goes into VerboseDetail and is only shown under `apkgo doctor -v`.
type Probe struct {
	Name          string `json:"name"`
	Status        string `json:"status"` // "ok", "fail", "skip"
	Detail        string `json:"detail,omitempty"`
	VerboseDetail string `json:"verbose_detail,omitempty"`
	Error         string `json:"error,omitempty"`
}

// MaskEmail produces a privacy-safe form of an email address, e.g.
//   "aoxianglele@icloud.com" → "a***@icloud.com"
// Length of the local part is intentionally not preserved so the
// output doesn't leak how long someone's username is.
func MaskEmail(email string) string {
	at := indexByte(email, '@')
	if at < 1 {
		return "***@***"
	}
	return email[:1] + "***" + email[at:]
}

// MaskName produces a privacy-safe form of a person's display name,
// preserving only the first rune followed by a fixed mask, e.g.
//   "Alice"      → "A***"
//   "刘洋"       → "刘***"
func MaskName(name string) string {
	for _, r := range name {
		return string(r) + "***"
	}
	return ""
}

// indexByte avoids importing strings just for this one lookup.
func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
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
