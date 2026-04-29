package uploader

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/KevinGong2013/apkgo/v3/pkg/apk"
	"github.com/KevinGong2013/apkgo/v3/pkg/ctxlog"
	"github.com/KevinGong2013/apkgo/v3/pkg/hooks"
	"github.com/KevinGong2013/apkgo/v3/pkg/store"
)

// StoreEntry pairs a store with its per-store hook commands and an
// optional per-store timeout that overrides the parent ctx deadline.
// This matters because some stores (huawei AGC submit polling, oppo
// task-state polling, tencent audit polling) routinely take 5+
// minutes while others are sub-second; one global timeout has to be
// pessimistic enough for the slowest store, which wastes time when
// the fast stores hang for unrelated reasons.
type StoreEntry struct {
	Store   store.Store
	Before  string
	After   string
	Timeout time.Duration // zero means inherit parent ctx
}

// Uploader orchestrates concurrent uploads to multiple stores.
type Uploader struct {
	Stores   []StoreEntry
	Progress ProgressManager // never nil; use NopManager when no output is desired
	Events   EventRecorder   // optional; called on lifecycle events for metrics
}

// Run uploads to all stores concurrently and returns all results.
func (u *Uploader) Run(ctx context.Context, req *store.UploadRequest, info *apk.Info) []*store.UploadResult {
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		results = make([]*store.UploadResult, 0, len(u.Stores))
	)

	if u.Progress == nil {
		u.Progress = NopManager
	}
	// Pre-allocate progress bars for every store (in configured order)
	// so the initial display has a stable layout regardless of the
	// goroutines' schedule.
	for _, e := range u.Stores {
		u.Progress.ReporterFor(e.Store.Name())
	}

	envVars := map[string]string{
		"APKGO_PACKAGE": req.PackageName,
		"APKGO_VERSION": req.VersionName,
	}

	for _, e := range u.Stores {
		wg.Add(1)
		go func(e StoreEntry) {
			defer wg.Done()

			name := e.Store.Name()
			log := ctxlog.FromContext(ctx).With("store", name)

			storeEnv := make(map[string]string, len(envVars)+1)
			for k, v := range envVars {
				storeEnv[k] = v
			}
			storeEnv["APKGO_STORE"] = name

			// Build a per-store upload request carrying its own progress
			// reporter. A shallow copy is enough since UploadRequest has no
			// pointer fields the store should mutate.
			storeReq := *req
			storeReq.Progress = u.Progress.ReporterFor(name)

			storeStart := time.Now()
			u.Events.emit(Event{Type: EventStoreStart, Store: name})

			// Per-store before hook
			if e.Before != "" {
				log.Info("running before hook")
				hookStart := time.Now()
				payload := hooks.BeforeStorePayload{
					FilePath: req.FilePath,
					APK:      info,
					Store:    name,
				}
				hookErr := hooks.RunHook(ctx, e.Before, payload, storeEnv)
				u.Events.emit(Event{
					Type:     EventHookRun,
					Store:    name,
					Hook:     "before",
					Duration: time.Since(hookStart),
					Err:      hookErr,
				})
				if hookErr != nil {
					log.Error("before hook failed, skipping store", "error", hookErr)
					res := store.ErrResult(name, hookStart, fmt.Errorf("before hook: %w", hookErr))
					u.Progress.MarkDone(name, false, res.Error, time.Duration(res.DurationMs)*time.Millisecond)
					u.Events.emit(Event{Type: EventStoreEnd, Store: name, Duration: time.Since(storeStart), Result: res, Err: hookErr})
					mu.Lock()
					results = append(results, res)
					mu.Unlock()
					return
				}
			}

			log.Info("uploading")
			storeCtx := ctx
			if e.Timeout > 0 {
				var cancel context.CancelFunc
				storeCtx, cancel = context.WithTimeout(ctx, e.Timeout)
				defer cancel()
			}
			result := e.Store.Upload(storeCtx, &storeReq)
			if result.Success {
				log.Info("upload succeeded", "duration_ms", result.DurationMs, "category", result.Category)
			} else {
				log.Error("upload failed", "error", result.Error, "duration_ms", result.DurationMs, "category", result.Category)
			}
			u.Progress.MarkDone(name, result.Success, result.Error, time.Duration(result.DurationMs)*time.Millisecond)

			// Per-store after hook
			if e.After != "" {
				log.Info("running after hook")
				hookStart := time.Now()
				payload := hooks.AfterStorePayload{
					FilePath: req.FilePath,
					APK:      info,
					Store:    name,
					Result:   result,
				}
				hookErr := hooks.RunHook(ctx, e.After, payload, storeEnv)
				u.Events.emit(Event{
					Type:     EventHookRun,
					Store:    name,
					Hook:     "after",
					Duration: time.Since(hookStart),
					Err:      hookErr,
				})
				if hookErr != nil {
					log.Warn("after hook failed", "error", hookErr)
				}
			}

			u.Events.emit(Event{Type: EventStoreEnd, Store: name, Duration: time.Since(storeStart), Result: result})

			mu.Lock()
			results = append(results, result)
			mu.Unlock()
		}(e)
	}

	wg.Wait()
	return results
}
