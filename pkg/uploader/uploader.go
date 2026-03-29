package uploader

import (
	"context"
	"log/slog"
	"sync"

	"github.com/KevinGong2013/apkgo/pkg/store"
)

// Uploader orchestrates concurrent uploads to multiple stores.
type Uploader struct {
	Stores []store.Store
}

// Run uploads to all stores concurrently and returns all results.
func (u *Uploader) Run(ctx context.Context, req *store.UploadRequest) []*store.UploadResult {
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		results = make([]*store.UploadResult, 0, len(u.Stores))
	)

	for _, s := range u.Stores {
		wg.Add(1)
		go func(s store.Store) {
			defer wg.Done()
			slog.Info("uploading", "store", s.Name())
			result := s.Upload(ctx, req)
			if result.Success {
				slog.Info("upload succeeded", "store", s.Name(), "duration_ms", result.DurationMs)
			} else {
				slog.Error("upload failed", "store", s.Name(), "error", result.Error, "duration_ms", result.DurationMs)
			}
			mu.Lock()
			results = append(results, result)
			mu.Unlock()
		}(s)
	}

	wg.Wait()
	return results
}
