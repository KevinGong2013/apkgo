package pgyer

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v4/pkg/internal/offloadtest"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// fakePgyer serves getCOSToken, the COS form upload and buildInfo.
type fakePgyer struct {
	srv      *httptest.Server
	mu       sync.Mutex
	uploaded []byte
	fileName string
	fields   map[string]string
}

func newFakePgyer(t *testing.T) *fakePgyer {
	f := &fakePgyer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/apiv2/app/getCOSToken", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"key": "build-key", "endpoint": f.srv.URL + "/cos", "params": map[string]string{"signature": "sig"},
		}})
	})
	mux.HandleFunc("/cos", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		file, hdr, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		data, _ := io.ReadAll(file)
		f.mu.Lock()
		f.uploaded, f.fileName = data, hdr.Filename
		f.fields = map[string]string{"key": r.FormValue("key"), "signature": r.FormValue("signature")}
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/apiv2/app/buildInfo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"buildUpdated": "2026-10-16"}})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakePgyer) store() *Store {
	return &Store{client: resty.New().SetBaseURL(f.srv.URL + "/apiv2"), apiKey: "k"}
}

func writeAPK(t *testing.T) (string, []byte) {
	t.Helper()
	data := bytes.Repeat([]byte("pgyer-apk "), 3000)
	p := filepath.Join(t.TempDir(), "demo.apk")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p, data
}

func TestUploadSendsThePackage(t *testing.T) {
	old := buildPollInterval
	buildPollInterval = time.Millisecond
	t.Cleanup(func() { buildPollInterval = old })

	path, data := writeAPK(t)
	for _, remote := range []bool{false, true} {
		f := newFakePgyer(t)
		req := &store.UploadRequest{FilePath: path, ReleaseNotes: "notes"}
		ctx := context.Background()
		var off *offloadtest.Offloader
		if remote {
			req, ctx, off = offloadtest.Remote(t, ctx, req)
		}
		res := f.store().Upload(ctx, req)
		if !res.Success {
			t.Fatalf("remote=%v: upload failed: %s", remote, res.Error)
		}
		if !bytes.Equal(f.uploaded, data) || f.fileName != "demo.apk" || f.fields["key"] != "build-key" || f.fields["signature"] != "sig" {
			t.Errorf("remote=%v: COS got %d bytes as %q, fields %v", remote, len(f.uploaded), f.fileName, f.fields)
		}
		if remote && !off.Sent(req.FilePath) {
			t.Errorf("the package did not go out as a transfer")
		}
	}
}
