package store

import (
	"context"
	"time"

	"github.com/KevinGong2013/apkgo/v3/pkg/progress"
)

// Store is the interface every app store must implement.
type Store interface {
	Name() string
	Upload(ctx context.Context, req *UploadRequest) *UploadResult
}

// UploadRequest carries the APK and metadata for a single upload.
type UploadRequest struct {
	FilePath     string // path to primary APK
	File64Path   string // optional 64-bit APK for split-arch uploads
	AppName      string
	PackageName  string
	VersionCode  int32
	VersionName  string
	ReleaseNotes string

	// ReleaseTime, when non-nil, requests a scheduled (定时/timed) release
	// at that instant instead of the default "go live immediately after
	// review". It carries a timezone offset: epoch-based stores (xiaomi,
	// tencent) use it as an absolute instant; local-datetime stores (oppo,
	// vivo, samsung) render it in Beijing time via BeijingLocalTime;
	// huawei/honor send it with its offset. Stores that can't schedule
	// (googleplay, pgyer, fir, script) ignore it and release immediately.
	// nil = immediate (the default, unchanged behaviour).
	ReleaseTime *time.Time `json:",omitempty"`

	// SourceURL / Source64URL carry the original public http(s) URL the
	// APK (and optional 64-bit APK) were given as, when no auth headers
	// were needed to fetch them. Stores that support "download mode"
	// (huawei, honor, vivo — see SupportsURLPush) hand this URL to the
	// store so it pulls the binary from your OSS instead of apkgo
	// re-uploading the bytes. Empty when the input was a local file or
	// required --fetch-header auth (the store must be able to GET it
	// anonymously). FilePath is always set regardless, so stores that
	// can't (or choose not to) URL-push still upload the local copy.
	SourceURL   string `json:",omitempty"`
	Source64URL string `json:",omitempty"`

	// Progress receives phase and byte-count events during upload.
	// May be nil; stores must use progress.Safe() to guard against that.
	// Tagged json:"-" so it's excluded when script-store marshals the
	// request as stdin for hook scripts.
	Progress progress.Reporter `json:"-"`
}

// UploadResult is the machine-readable outcome of a single store upload.
//
// Category is intended for cloud orchestrators: a small enum bucketing
// the outcome into "retryable network blip" vs "auth wrong, surface to
// user" vs "already done, no-op", etc. See category.go for values.
type UploadResult struct {
	Store      string   `json:"store"`
	Success    bool     `json:"success"`
	Error      string   `json:"error,omitempty"`
	Category   Category `json:"category,omitempty"`
	DurationMs int64    `json:"duration_ms"`
	// ExternalID is an opaque per-store identifier for this specific
	// submission (e.g. honor's releaseId from submit-audit), for stores that
	// expose one. Callers that persist upload results can feed it back into
	// a later AuditQuery.ExternalID to pin a review-status query to this
	// exact submission instead of the store's ambiguous "current" state.
	ExternalID string `json:"external_id,omitempty"`
}

// NewResult creates a success result with timing.
func NewResult(storeName string, start time.Time) *UploadResult {
	return &UploadResult{
		Store:      storeName,
		Success:    true,
		Category:   CategorySuccess,
		DurationMs: time.Since(start).Milliseconds(),
	}
}

// NewResultC creates a success result with an explicit category.
// Used for "already done" outcomes — the upload didn't actually run
// because the version was already on the store side.
func NewResultC(storeName string, start time.Time, cat Category) *UploadResult {
	return &UploadResult{
		Store:      storeName,
		Success:    true,
		Category:   cat,
		DurationMs: time.Since(start).Milliseconds(),
	}
}

// ErrResult creates a failure result with timing. Category is
// extracted from the error chain via CategoryOf — stores producing
// classified errors via Categorize() automatically populate it.
func ErrResult(storeName string, start time.Time, err error) *UploadResult {
	return &UploadResult{
		Store:      storeName,
		Success:    false,
		Error:      err.Error(),
		Category:   CategoryOf(err),
		DurationMs: time.Since(start).Milliseconds(),
	}
}

// ConfigSchema describes the configuration fields a store requires.
// Used by `apkgo stores` for agent discoverability.
type ConfigSchema struct {
	Name       string        `json:"name"`
	Fields     []FieldSchema `json:"fields"`
	ConsoleURL string        `json:"console_url"`           // developer console URL where credentials are managed
	AcceptsAAB bool          `json:"accepts_aab,omitempty"` // true if the store accepts .aab in addition to .apk
	// SupportsScheduledRelease is true if the store's API can schedule a
	// release for a future time (定时发布). Surfaced by `apkgo stores`;
	// also drives the warning when --release-time targets a store that
	// can't honor it.
	SupportsScheduledRelease bool `json:"supports_scheduled_release,omitempty"`
	// SupportsURLPush is true if the store's API can pull the APK from a
	// developer-hosted URL (download mode) instead of requiring a byte
	// upload. apkgo uses it to skip re-uploading when -f is a public URL.
	// Surfaced by `apkgo stores`.
	SupportsURLPush bool `json:"supports_url_push,omitempty"`
}

type FieldSchema struct {
	Key      string `json:"key"`
	Required bool   `json:"required"`
	Desc     string `json:"desc"`
}

// Factory creates a Store from a flat config map.
type Factory func(cfg map[string]string) (Store, error)

// beijing is China Standard Time (UTC+8, no DST). Built as a fixed zone
// so scheduled-release formatting never depends on the host having the
// IANA tzdata database installed.
var beijing = time.FixedZone("UTC+8", 8*60*60)

// BeijingLocalTime renders t as "2006-01-02 15:04:05" in Beijing time.
// Used by stores whose scheduled-release field is a local datetime
// string with no timezone of its own (oppo, vivo, samsung).
func BeijingLocalTime(t time.Time) string {
	return t.In(beijing).Format("2006-01-02 15:04:05")
}
