package store

import (
	"context"
	"time"
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
}

// UploadResult is the machine-readable outcome of a single store upload.
type UploadResult struct {
	Store      string `json:"store"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"duration_ms"`
}

// NewResult creates a success result with timing.
func NewResult(storeName string, start time.Time) *UploadResult {
	return &UploadResult{
		Store:      storeName,
		Success:    true,
		DurationMs: time.Since(start).Milliseconds(),
	}
}

// ErrResult creates a failure result with timing.
func ErrResult(storeName string, start time.Time, err error) *UploadResult {
	return &UploadResult{
		Store:      storeName,
		Success:    false,
		Error:      err.Error(),
		DurationMs: time.Since(start).Milliseconds(),
	}
}

// ConfigSchema describes the configuration fields a store requires.
// Used by `apkgo stores` for agent discoverability.
type ConfigSchema struct {
	Name       string        `json:"name"`
	Fields     []FieldSchema `json:"fields"`
	ConsoleURL string        `json:"console_url"` // developer console URL where credentials are managed
}

type FieldSchema struct {
	Key      string `json:"key"`
	Required bool   `json:"required"`
	Desc     string `json:"desc"`
}

// Factory creates a Store from a flat config map.
type Factory func(cfg map[string]string) (Store, error)
