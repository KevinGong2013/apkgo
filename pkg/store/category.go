package store

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
)

// Category classifies an upload outcome into one of a small,
// orchestrator-friendly set of buckets so cloud schedulers can decide
// whether to retry, mark done, or surface to a human without parsing
// the (often Chinese, often store-specific) human-readable error text.
//
// Stores set the category at the point of error generation via
// Categorize(cat, err); the top-level UploadResult carries it on the
// Category field. Callers should treat any unknown Category value as
// CategoryUnknown for forward compatibility — apkgo may add new
// categories in the future.
type Category string

const (
	// CategorySuccess: the upload completed and the version is now
	// either live or in the store's normal review queue. No action
	// needed.
	CategorySuccess Category = "success"

	// CategoryAlreadyDone: the upload was a no-op because this exact
	// version already exists on the store side (already published,
	// already in review, etc.). Cloud should mark the job as done
	// without re-attempting.
	CategoryAlreadyDone Category = "already_done"

	// CategoryAuthFailed: credentials are wrong, expired, or missing
	// scope. Retrying with the same credentials will not help; a
	// human needs to fix the secret.
	CategoryAuthFailed Category = "auth_failed"

	// CategoryNetworkRetry: transient network or 5xx server error.
	// Cloud should retry with backoff.
	CategoryNetworkRetry Category = "network_retry"

	// CategoryStoreBusy: the store accepted the request but is rate-
	// limiting / has a previous async task in flight. Retry after a
	// longer wait (typically minutes).
	CategoryStoreBusy Category = "store_busy"

	// CategoryPolicyBlock: store-side rules rejected the upload —
	// signature mismatch, content audit failure, prohibited category,
	// etc. Retrying is futile; surface to a human.
	CategoryPolicyBlock Category = "policy_block"

	// CategoryConfigInvalid: app-level metadata on the store's
	// console is incomplete (missing intro, missing classification,
	// missing publisher entity, etc.). Cloud should surface to the
	// app owner so they fix the console form.
	CategoryConfigInvalid Category = "config_invalid"

	// CategoryUnknown: we don't yet have enough information to
	// classify this outcome. Cloud should treat it as not
	// auto-retryable and surface to a human.
	CategoryUnknown Category = "unknown"
)

// CategorizedError wraps an error with a Category for cloud-level
// retry/decision logic. Use Categorize() to construct one.
type CategorizedError struct {
	Cat Category
	Err error
}

func (e *CategorizedError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *CategorizedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Category returns the wrapped category.
func (e *CategorizedError) Category() Category {
	if e == nil {
		return CategoryUnknown
	}
	return e.Cat
}

// Categorize tags err with cat. Returns nil when err is nil so the
// helper can be used at error-return sites without explicit nil-check.
//
//	return store.Categorize(store.CategoryAuthFailed, err)
//
// If err is already a *CategorizedError, the outer category wins —
// the deeper site has more context than the wrapper.
func Categorize(cat Category, err error) error {
	if err == nil {
		return nil
	}
	return &CategorizedError{Cat: cat, Err: err}
}

// CategoryOf walks the error chain and returns the first
// CategorizedError's category. Returns CategorySuccess for nil err.
// Uncategorized errors get a transport-level fallback: anything that
// looks like a network/timeout failure is classified network_retry, so
// individual stores don't have to wrap every HTTP call site; everything
// else is CategoryUnknown.
func CategoryOf(err error) Category {
	if err == nil {
		return CategorySuccess
	}
	var ce *CategorizedError
	if errors.As(err, &ce) {
		return ce.Cat
	}
	if isNetworkError(err) {
		return CategoryNetworkRetry
	}
	return CategoryUnknown
}

// isNetworkError reports whether err's chain is a transport-level
// failure — timeout, connection drop, DNS — as opposed to an
// application-level rejection by the store. Store APIs return their
// rejections as parsed response envelopes (plain fmt.Errorf), so a
// net.Error / url.Error in the chain reliably means the request never
// completed at the HTTP layer.
//
// The string checks cover cases the typed checks miss: the http
// transport reports a peer closing the connection mid-upload as a raw
// errors.New("use of closed network connection") that implements no
// net.Error, and resty sometimes flattens transport errors into plain
// strings when retrying.
func isNetworkError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return true
	}
	msg := err.Error()
	for _, marker := range []string{
		"use of closed network connection",
		"connection reset by peer",
		"broken pipe",
		"Client.Timeout exceeded",
		"TLS handshake timeout",
		"unexpected EOF",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// AlreadyDoneError is a sentinel that store implementations return
// from their internal upload() function when they detect "this
// version is already on the store side" (e.g. oppo 911215 "应用审核
// 中"). The store's outer Upload() wrapper recognises it and returns
// a success result with Category=already_done — semantically a
// success (no work to retry) but flagged for orchestrators that
// want to distinguish a real upload from a no-op.
type AlreadyDoneError struct{ Reason string }

func (e *AlreadyDoneError) Error() string {
	if e == nil || e.Reason == "" {
		return "already done"
	}
	return "already done: " + e.Reason
}

// IsAlreadyDone reports whether err's chain contains an AlreadyDoneError.
func IsAlreadyDone(err error) bool {
	var ad *AlreadyDoneError
	return errors.As(err, &ad)
}
