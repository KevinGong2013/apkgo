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

// AuditJob mirrors DiagnoseJob: look up each configured store's review
// (审核) status for a package or APK, decoupled from uploading. Upload
// itself now finishes at "submitted (审核中)"; callers poll review
// progress with QueryAudit on their own schedule (e.g. a cron or a
// `apkgo audit --watch` loop).
type AuditJob struct {
	// Config is the resolved store + hooks config. Required.
	Config *config.Config

	// Stores filters which configured stores to query. Empty means
	// every store the Config has credentials for.
	Stores []string

	// Package is the app to look up. Required unless APKFile is set.
	Package string

	// APKFile is an alternative source for Package: when set and Package
	// is empty, the APK is parsed (URL fetch supported via FetchHeaders)
	// and its package name + version are used.
	APKFile string

	// FetchHeaders attaches HTTP headers to URL fetches of APKFile.
	FetchHeaders map[string]string
}

// AuditStoreResult is one store's section: the unified result plus
// whether the store has an auditor registered at all.
type AuditStoreResult struct {
	store.AuditResult
	Supported bool `json:"supported"`
}

// AuditReport is the structured `apkgo audit` output.
type AuditReport struct {
	Package string             `json:"package,omitempty"`
	Stores  []AuditStoreResult `json:"stores"`
}

// AllResolved reports whether every supported store has reached a terminal
// review state (or errored). `apkgo audit --watch` stops once this is true.
func (r *AuditReport) AllResolved() bool {
	for _, s := range r.Stores {
		if !s.Supported {
			continue
		}
		if s.Error == "" && !s.State.Resolved() {
			return false
		}
	}
	return true
}

// QueryAudit looks up review status against the configured stores. It
// performs no uploads (URL APK fetch is the one exception, when APKFile is
// a URL — same temp-file lifecycle as Run/Diagnose).
//
// Pre-config errors (URL fetch / APK parse / no package) surface as the
// returned error. Per-store query failures land in the AuditStoreResult's
// Error field — QueryAudit itself returns nil so a caller can collect a
// complete report even when some stores are unreachable.
func QueryAudit(ctx context.Context, job AuditJob) (*AuditReport, error) {
	if job.Config == nil {
		return nil, fmt.Errorf("apkgo.AuditJob.Config is required")
	}

	q := store.AuditQuery{Package: job.Package}
	if q.Package == "" && job.APKFile != "" {
		paths, cleanup, err := httpx.FetchToTempBatch(ctx, []string{job.APKFile}, job.FetchHeaders)
		if err != nil {
			return nil, fmt.Errorf("fetch apk: %w", err)
		}
		defer cleanup()
		apkPath := paths[0]
		if _, err := os.Stat(apkPath); err != nil {
			return nil, fmt.Errorf("apk file: %w", err)
		}
		// AABs can't be parsed for a package name; the operator must pass
		// --package explicitly.
		if !apk.IsAAB(apkPath) {
			info, err := apk.Parse(apkPath)
			if err != nil {
				return nil, fmt.Errorf("parse apk: %w", err)
			}
			q.Package = info.PackageName
			q.VersionName = info.VersionName
			q.VersionCode = info.VersionCode
		}
	}
	if q.Package == "" {
		return nil, fmt.Errorf("a package name is required: pass --package or an APK via --file")
	}

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

	out := &AuditReport{Package: q.Package}
	for _, name := range names {
		scfg := cleanStoreCfg(job.Config.Stores[name])
		// "type.instance" naming (e.g. script.cdn): the auditor is keyed
		// by the type prefix, not the instance name.
		key := name
		if dot := strings.Index(name, "."); dot > 0 {
			key = name[:dot]
		}
		res, supported := store.QueryAudit(ctx, key, scfg, q)
		res.Store = name
		out.Stores = append(out.Stores, AuditStoreResult{AuditResult: res, Supported: supported})
	}
	return out, nil
}
