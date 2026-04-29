// Package ctxlog provides a lightweight context-attached *slog.Logger
// so library callers (cloud workers, multi-tenant CI, etc.) can route
// per-job log lines through a logger that carries job_id / tenant_id /
// trace_id without mutating slog.Default.
//
// The pattern:
//
//	logger := slog.With("job_id", id, "tenant", tenant)
//	ctx = ctxlog.With(ctx, logger)
//	apkgo.Run(ctx, job)  // every uploader log line carries the labels
//
// Stores not yet refactored to use ctxlog.FromContext(ctx) fall back
// to slog.Default; introducing this is incremental and backward-
// compatible.
package ctxlog

import (
	"context"
	"log/slog"
)

type ctxKey struct{}

// With returns a copy of ctx that carries logger. Subsequent
// FromContext calls on derived contexts will return logger.
func With(ctx context.Context, logger *slog.Logger) context.Context {
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, logger)
}

// FromContext returns the logger attached to ctx, falling back to
// slog.Default when none was set. Always returns a non-nil logger.
func FromContext(ctx context.Context) *slog.Logger {
	if ctx == nil {
		return slog.Default()
	}
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}
