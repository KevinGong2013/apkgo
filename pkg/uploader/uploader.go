package uploader

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/KevinGong2013/apkgo/pkg/apk"
	"github.com/KevinGong2013/apkgo/pkg/hooks"
	"github.com/KevinGong2013/apkgo/pkg/store"
)

// StoreEntry pairs a store with its per-store hook commands.
type StoreEntry struct {
	Store  store.Store
	Before string
	After  string
}

// Uploader orchestrates concurrent uploads to multiple stores.
type Uploader struct {
	Stores []StoreEntry
}

// Run uploads to all stores concurrently and returns all results.
func (u *Uploader) Run(ctx context.Context, req *store.UploadRequest, info *apk.Info) []*store.UploadResult {
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		results = make([]*store.UploadResult, 0, len(u.Stores))
	)

	envVars := map[string]string{
		"APKGO_PACKAGE": req.PackageName,
		"APKGO_VERSION": req.VersionName,
	}

	for _, e := range u.Stores {
		wg.Add(1)
		go func(e StoreEntry) {
			defer wg.Done()

			name := e.Store.Name()
			storeEnv := make(map[string]string, len(envVars)+1)
			for k, v := range envVars {
				storeEnv[k] = v
			}
			storeEnv["APKGO_STORE"] = name

			// Per-store before hook
			if e.Before != "" {
				slog.Info("running before hook", "store", name)
				payload := hooks.BeforeStorePayload{
					FilePath: req.FilePath,
					APK:      info,
					Store:    name,
				}
				if err := hooks.RunHook(ctx, e.Before, payload, storeEnv); err != nil {
					slog.Error("before hook failed, skipping store", "store", name, "error", err)
					start := time.Now()
					mu.Lock()
					results = append(results, store.ErrResult(name, start, fmt.Errorf("before hook: %w", err)))
					mu.Unlock()
					return
				}
			}

			slog.Info("uploading", "store", name)
			result := e.Store.Upload(ctx, req)
			if result.Success {
				slog.Info("upload succeeded", "store", name, "duration_ms", result.DurationMs)
			} else {
				slog.Error("upload failed", "store", name, "error", result.Error, "duration_ms", result.DurationMs)
			}

			// Per-store after hook
			if e.After != "" {
				slog.Info("running after hook", "store", name)
				payload := hooks.AfterStorePayload{
					FilePath: req.FilePath,
					APK:      info,
					Store:    name,
					Result:   result,
				}
				if err := hooks.RunHook(ctx, e.After, payload, storeEnv); err != nil {
					slog.Warn("after hook failed", "store", name, "error", err)
				}
			}

			mu.Lock()
			results = append(results, result)
			mu.Unlock()
		}(e)
	}

	wg.Wait()
	return results
}
