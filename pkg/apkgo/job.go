// Package apkgo is the embeddable, headless core of the apkgo CLI.
//
// External callers (cloud workers, custom tooling, CI helpers) drive a
// publish workflow by constructing a Job and calling Run:
//
//	cfg, _ := config.Load("apkgo.yaml") // or build programmatically
//	result, err := apkgo.Run(ctx, apkgo.Job{
//	    APKFile: "https://artifacts.example.com/v1.2.0.apk",
//	    Stores:  []string{"huawei", "tencent"},
//	    Notes:   "Bug fixes",
//	    Config:  cfg,
//	})
//
// Run touches no global state (no slog handler swap, no exit code) so
// it's safe to embed inside a long-lived process. The CLI in cmd/ is a
// thin shell over this package.
package apkgo

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/KevinGong2013/apkgo/pkg/apk"
	"github.com/KevinGong2013/apkgo/pkg/config"
	"github.com/KevinGong2013/apkgo/pkg/hooks"
	"github.com/KevinGong2013/apkgo/pkg/httpx"
	"github.com/KevinGong2013/apkgo/pkg/store"
	"github.com/KevinGong2013/apkgo/pkg/uploader"
)

// DefaultTimeout is applied to a Job whose Timeout field is zero. The
// upload pipeline can take many minutes (cross-China network, audit
// polling), so we err on the side of patience.
const DefaultTimeout = 10 * time.Minute

// Job describes a single publish run from APK input through to all
// configured stores receiving (or rejecting) the upload.
type Job struct {
	// APKFile is the primary APK. Accepts a local filesystem path or
	// an http(s) URL; URLs are fetched into a temp file, parsed, and
	// cleaned up automatically.
	APKFile string

	// APKFile64 is the optional 64-bit APK for split-arch uploads
	// (xiaomi, vivo, tencent support split). Same path/URL semantics
	// as APKFile.
	APKFile64 string

	// Stores filters which configured stores to publish to. Empty
	// means "every store the Config has credentials for".
	Stores []string

	// Notes is the release notes text. Most stores require some notes
	// when publishing an update.
	Notes string

	// NotesFile, when set, takes precedence over Notes — its contents
	// (trimmed) are used instead.
	NotesFile string

	// Config is the resolved store + hooks config. Required.
	Config *config.Config

	// FetchHeaders attaches HTTP headers to URL fetches of APKFile /
	// APKFile64. Useful for private artifact servers with bearer auth.
	FetchHeaders map[string]string

	// Progress is where per-store progress events are reported.
	// Pass uploader.NopManager for a silent run; the CLI passes an
	// mpb-backed manager or NDJSONManager based on flags.
	Progress uploader.ProgressManager

	// Timeout is a hard deadline for the upload phase (after URL
	// fetch + APK parse). Zero means DefaultTimeout.
	Timeout time.Duration

	// DryRun validates everything (config, APK metadata, store
	// instantiation) without making any upload calls. Returns a
	// Result whose per-store entries are all Success: true.
	DryRun bool
}

// Result is the outcome of a single Run.
type Result struct {
	// APK is the parsed metadata of the input APK.
	APK *apk.Info

	// DryRun is true if the run was a dry-run (Job.DryRun was set).
	DryRun bool

	// Results is one entry per target store, in the order Config
	// declared them. Each entry has Success / Error / DurationMs.
	Results []*store.UploadResult
}

// Run executes the job synchronously and returns the result. It
// honours ctx cancellation throughout (URL fetch, hook execution,
// per-store uploads, audit polling).
//
// Returned errors are limited to *pre-upload* failures: invalid
// config, unreachable APK URL, missing required field, etc. Once the
// upload pipeline starts, every per-store outcome (including network
// failures) is captured in Result.Results — Run itself returns nil.
func Run(ctx context.Context, job Job) (*Result, error) {
	if job.Config == nil {
		return nil, fmt.Errorf("apkgo.Job.Config is required")
	}

	// Resolve URL inputs to local paths.
	paths, cleanup, err := httpx.FetchToTempBatch(ctx, []string{job.APKFile, job.APKFile64}, job.FetchHeaders)
	if err != nil {
		return nil, fmt.Errorf("fetch apk: %w", err)
	}
	defer cleanup()
	apkPath, apk64Path := paths[0], paths[1]

	if _, err := os.Stat(apkPath); err != nil {
		return nil, fmt.Errorf("apk file: %w", err)
	}
	if apk64Path != "" {
		if _, err := os.Stat(apk64Path); err != nil {
			return nil, fmt.Errorf("64-bit apk file: %w", err)
		}
	}

	info, err := apk.Parse(apkPath)
	if err != nil {
		return nil, fmt.Errorf("parse apk: %w", err)
	}

	// Resolve release notes (file wins over inline).
	notes := strings.TrimSpace(job.Notes)
	if job.NotesFile != "" {
		data, err := os.ReadFile(job.NotesFile)
		if err != nil {
			return nil, fmt.Errorf("notes-file: %w", err)
		}
		notes = strings.TrimSpace(string(data))
	}

	storesWithHooks, err := job.Config.CreateStores(job.Stores)
	if err != nil {
		return nil, err
	}

	storeNames := make([]string, len(storesWithHooks))
	entries := make([]uploader.StoreEntry, len(storesWithHooks))
	for i, swh := range storesWithHooks {
		storeNames[i] = swh.Store.Name()
		entries[i] = uploader.StoreEntry{Store: swh.Store, Before: swh.Before, After: swh.After}
	}

	if job.DryRun {
		results := make([]*store.UploadResult, len(storeNames))
		for i, name := range storeNames {
			results[i] = &store.UploadResult{Store: name, Success: true}
		}
		return &Result{APK: info, DryRun: true, Results: results}, nil
	}

	timeout := job.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pm := job.Progress
	if pm == nil {
		pm = uploader.NopManager
	}
	pm.Start(info, storeNames)

	req := &store.UploadRequest{
		FilePath:     apkPath,
		File64Path:   apk64Path,
		AppName:      info.AppName,
		PackageName:  info.PackageName,
		VersionCode:  info.VersionCode,
		VersionName:  info.VersionName,
		ReleaseNotes: notes,
	}

	hookEnv := map[string]string{
		"APKGO_PACKAGE": info.PackageName,
		"APKGO_VERSION": info.VersionName,
	}

	if job.Config.Hooks.Before != "" {
		payload := hooks.BeforeAllPayload{FilePath: apkPath, APK: info, Stores: storeNames}
		if err := hooks.RunHook(runCtx, job.Config.Hooks.Before, payload, hookEnv); err != nil {
			return nil, fmt.Errorf("global before hook: %w", err)
		}
	}

	u := &uploader.Uploader{Stores: entries, Progress: pm}
	results := u.Run(runCtx, req, info)
	pm.Wait()

	// After-hook failures don't abort the run — the upload itself has
	// already completed by the time this fires. The CLI logs a warning;
	// library callers can introspect job.Config.Hooks.After themselves.
	if job.Config.Hooks.After != "" {
		payload := hooks.AfterAllPayload{FilePath: apkPath, APK: info, Results: results}
		_ = hooks.RunHook(runCtx, job.Config.Hooks.After, payload, hookEnv)
	}

	pm.Done(info, results)
	return &Result{APK: info, Results: results}, nil
}
