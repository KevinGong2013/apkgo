package store

import "context"

// AuditState is the unified review state of a submitted version,
// normalised from each store's own status codes so callers don't have to
// learn every vendor's enum.
type AuditState string

const (
	AuditReviewing AuditState = "reviewing" // 审核中 — submitted, pending review
	AuditApproved  AuditState = "approved"  // 审核通过 — approved (live or pending release)
	AuditRejected  AuditState = "rejected"  // 审核驳回 — not approved
	AuditWithdrawn AuditState = "withdrawn" // 已撤回 — submission cancelled
	AuditUnknown   AuditState = "unknown"   // store returned a state we don't map
)

// Resolved reports whether the review has finished (terminal state).
// `apkgo audit --watch` stops polling once every store is Resolved.
func (s AuditState) Resolved() bool {
	return s == AuditApproved || s == AuditRejected || s == AuditWithdrawn
}

// AuditQuery identifies the app/version whose review status to look up.
// VersionName/VersionCode are best-effort hints; most stores key off the
// package name and report the latest submission.
type AuditQuery struct {
	Package     string
	VersionName string
	VersionCode int32
}

// AuditResult is one store's review status. Error is set when the query
// itself failed (auth / network / not-found); State is meaningful only
// when Error is empty.
type AuditResult struct {
	Store  string     `json:"store"`
	State  AuditState `json:"state,omitempty"`
	Detail string     `json:"detail,omitempty"`
	Error  string     `json:"error,omitempty"`
}

// AuditFn looks up the current review status for one store from its raw
// config. Like DiagnoseFn it owns its own auth/setup, so it runs on an
// independent context fully decoupled from the upload flow — which is the
// point: upload finishes at "submitted (审核中)" and review progress is
// polled separately, on its own schedule.
type AuditFn func(ctx context.Context, cfg map[string]string, q AuditQuery) AuditResult

var auditors = map[string]AuditFn{}

// RegisterAuditor opts a store into `apkgo audit`. Stores without one are
// reported as unsupported (no review-status API, or not yet wired).
func RegisterAuditor(name string, fn AuditFn) {
	auditors[name] = fn
}

// QueryAudit runs the registered auditor for a store. The second return
// value is false when the store has not registered one.
func QueryAudit(ctx context.Context, name string, cfg map[string]string, q AuditQuery) (AuditResult, bool) {
	fn, ok := auditors[name]
	if !ok {
		return AuditResult{}, false
	}
	return fn(ctx, cfg, q), true
}
