package apkgo

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/KevinGong2013/apkgo/v3/pkg/apk"
	"github.com/KevinGong2013/apkgo/v3/pkg/config"
	"github.com/KevinGong2013/apkgo/v3/pkg/httpx"
	"github.com/KevinGong2013/apkgo/v3/pkg/store"
)

// DiagnoseJob describes a `doctor`-style run: probe each configured
// store's credentials and (when a package name is supplied) deeper
// permissions, returning a structured report instead of upload work.
//
// Cloud workers typically call Diagnose before scheduling a real
// upload — fail fast on bad credentials, save the user from a
// half-finished publish.
type DiagnoseJob struct {
	// Config is the resolved store + hooks config. Required.
	Config *config.Config

	// Stores filters which configured stores to probe. Empty means
	// every store the Config has credentials for.
	Stores []string

	// Package is an optional package-name hint. When set, stores
	// run their package-specific probes (appid lookup, app-release
	// permission, app-info binding, etc.) in addition to the
	// auth-only probes.
	Package string

	// APKFile is an optional alternative source for Package: when
	// set and Package is empty, the APK is parsed (URL fetch
	// supported via FetchHeaders) and its package name is used.
	APKFile string

	// FetchHeaders attaches HTTP headers to URL fetches of APKFile.
	FetchHeaders map[string]string
}

// StoreReport is one store's section in a DiagnoseResult.
type StoreReport struct {
	Store     string        `json:"store"`
	Supported bool          `json:"supported"`
	Probes    []store.Probe `json:"probes,omitempty"`
}

// DiagnoseResult is the structured doctor output.
type DiagnoseResult struct {
	// Package, if set, is the resolved package name used for the
	// deeper probes — useful for cloud callers to log/correlate.
	Package string `json:"package,omitempty"`

	// Stores is one report per probed store, in alphabetical order.
	Stores []StoreReport `json:"stores"`
}

// AnyFailed reports whether any probe failed across all stores.
// Convenience for cloud orchestrators that just want a yes/no signal.
func (r *DiagnoseResult) AnyFailed() bool {
	for _, s := range r.Stores {
		for _, p := range s.Probes {
			if p.Status == "fail" {
				return true
			}
		}
	}
	return false
}

// Diagnose runs readiness probes against the configured stores. It
// performs no uploads and creates no temp files (URL APK fetch is the
// one exception, when APKFile is a URL — same temp-file lifecycle as
// Run).
//
// Pre-config errors (URL fetch failure, APK parse failure) surface as
// the returned error. Per-store probe failures land in the StoreReport
// — Diagnose itself returns nil error in that case so a cloud caller
// can collect a complete report even when some stores are broken.
func Diagnose(ctx context.Context, job DiagnoseJob) (*DiagnoseResult, error) {
	if job.Config == nil {
		return nil, fmt.Errorf("apkgo.DiagnoseJob.Config is required")
	}

	pkg := job.Package
	if pkg == "" && job.APKFile != "" {
		paths, cleanup, err := httpx.FetchToTempBatch(ctx, []string{job.APKFile}, job.FetchHeaders)
		if err != nil {
			return nil, fmt.Errorf("fetch apk: %w", err)
		}
		defer cleanup()
		apkPath := paths[0]
		if _, err := os.Stat(apkPath); err != nil {
			return nil, fmt.Errorf("apk file: %w", err)
		}
		// AABs can't be parsed for package name; the operator needs to
		// pass --package explicitly. Probes that need it will report
		// skip with a clear reason.
		if !apk.IsAAB(apkPath) {
			info, err := apk.Parse(apkPath)
			if err != nil {
				return nil, fmt.Errorf("parse apk: %w", err)
			}
			pkg = info.PackageName
		}
	}

	hint := store.DiagnoseHint{Package: pkg}

	var filter map[string]bool
	if len(job.Stores) > 0 {
		filter = make(map[string]bool, len(job.Stores))
		for _, n := range job.Stores {
			if n = strings.TrimSpace(n); n != "" {
				filter[n] = true
			}
		}
	}

	names := make([]string, 0, len(job.Config.Stores))
	for name := range job.Config.Stores {
		if filter != nil && !filter[name] {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("no matching stores configured")
	}

	out := &DiagnoseResult{Package: pkg}
	for _, name := range names {
		scfg := cleanStoreCfg(job.Config.Stores[name])
		// "type.instance" naming (e.g. script.cdn): the diagnoser
		// is keyed by the type prefix, not the instance name.
		key := name
		if dot := strings.Index(name, "."); dot > 0 {
			key = name[:dot]
		}
		probes, supported := store.Diagnose(ctx, key, scfg, hint)
		out.Stores = append(out.Stores, StoreReport{Store: name, Supported: supported, Probes: probes})
	}
	return out, nil
}

// cleanStoreCfg strips hook + per-store-timeout keys so they are not
// handed to a diagnoser as if they were credentials.
func cleanStoreCfg(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		if k == "before" || k == "after" || k == "timeout" {
			continue
		}
		out[k] = v
	}
	return out
}
