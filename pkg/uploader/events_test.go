package uploader_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/KevinGong2013/apkgo/v3/pkg/apk"
	"github.com/KevinGong2013/apkgo/v3/pkg/store"
	"github.com/KevinGong2013/apkgo/v3/pkg/uploader"
)

// fakeStore is a minimal Store implementation that returns a
// configurable result after an optional sleep.
type fakeStore struct {
	name   string
	delay  time.Duration
	result *store.UploadResult
	err    error
}

func (f *fakeStore) Name() string { return f.name }
func (f *fakeStore) Upload(ctx context.Context, req *store.UploadRequest) *store.UploadResult {
	start := time.Now()
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return store.ErrResult(f.name, start, ctx.Err())
		}
	}
	if f.err != nil {
		return store.ErrResult(f.name, start, f.err)
	}
	if f.result != nil {
		return f.result
	}
	return store.NewResult(f.name, start)
}

func TestUploader_Events(t *testing.T) {
	stores := []uploader.StoreEntry{
		{Store: &fakeStore{name: "s1"}},
		{Store: &fakeStore{name: "s2", err: errors.New("boom")}},
	}

	var (
		mu     sync.Mutex
		events []uploader.Event
	)
	rec := uploader.EventRecorder(func(ev uploader.Event) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	})

	u := &uploader.Uploader{Stores: stores, Events: rec}
	u.Run(context.Background(), &store.UploadRequest{}, &apk.Info{})

	mu.Lock()
	defer mu.Unlock()

	starts, ends := 0, 0
	endsByStore := map[string]*uploader.Event{}
	for i := range events {
		switch events[i].Type {
		case uploader.EventStoreStart:
			starts++
		case uploader.EventStoreEnd:
			ends++
			ev := events[i]
			endsByStore[ev.Store] = &ev
		}
	}
	if starts != 2 {
		t.Errorf("got %d store.start events, want 2", starts)
	}
	if ends != 2 {
		t.Errorf("got %d store.end events, want 2", ends)
	}
	if r := endsByStore["s1"].Result; r == nil || !r.Success {
		t.Errorf("s1 expected success, got %+v", r)
	}
	if r := endsByStore["s2"].Result; r == nil || r.Success {
		t.Errorf("s2 expected failure, got %+v", r)
	}
}

func TestUploader_PerStoreTimeout(t *testing.T) {
	stores := []uploader.StoreEntry{
		{Store: &fakeStore{name: "slow", delay: 200 * time.Millisecond}, Timeout: 50 * time.Millisecond},
	}
	u := &uploader.Uploader{Stores: stores}
	results := u.Run(context.Background(), &store.UploadRequest{}, &apk.Info{})

	if len(results) != 1 || results[0].Success {
		t.Fatalf("expected timeout failure, got %+v", results)
	}
	if results[0].Error == "" {
		t.Error("expected non-empty error message")
	}
}
