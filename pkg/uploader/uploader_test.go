package uploader

import (
	"context"
	"testing"

	"github.com/KevinGong2013/apkgo/pkg/apk"
	"github.com/KevinGong2013/apkgo/pkg/progress"
	"github.com/KevinGong2013/apkgo/pkg/store"
)

// fakeStore is a zero-I/O store used to verify the uploader pipeline.
// It records whether it received a non-nil progress.Reporter so we can
// assert the uploader substitutes progress.Nop when Manager is nil.
type fakeStore struct {
	name       string
	sawReport  bool
	sawNopOnly bool
}

func (f *fakeStore) Name() string { return f.name }
func (f *fakeStore) Upload(_ context.Context, req *store.UploadRequest) *store.UploadResult {
	if req.Progress != nil {
		f.sawReport = true
		if _, ok := req.Progress.(progress.Nop); ok {
			f.sawNopOnly = true
		}
		// Exercise every method — must not panic under Nop.
		req.Progress.Phase("uploading")
		req.Progress.Total(100)
		req.Progress.Add(50)
		req.Progress.Add(50)
	}
	return &store.UploadResult{Store: f.name, Success: true}
}

// TestUploaderNilManager confirms that an Uploader with a nil Progress
// manager still runs without panicking and that stores receive a Nop
// reporter rather than a nil one. This is the code path the CLI takes
// when stderr is not a TTY or --verbose is set.
func TestUploaderNilManager(t *testing.T) {
	fs := &fakeStore{name: "fake"}
	u := &Uploader{
		Stores:   []StoreEntry{{Store: fs}},
		Progress: nil,
	}

	results := u.Run(context.Background(),
		&store.UploadRequest{FilePath: "dummy.apk", PackageName: "com.x"},
		&apk.Info{PackageName: "com.x", VersionName: "1.0"})

	if len(results) != 1 || !results[0].Success {
		t.Fatalf("unexpected results: %+v", results)
	}
	if !fs.sawReport {
		t.Fatal("store did not receive a progress reporter")
	}
	if !fs.sawNopOnly {
		t.Fatal("store should have received progress.Nop when Manager is nil")
	}
}
