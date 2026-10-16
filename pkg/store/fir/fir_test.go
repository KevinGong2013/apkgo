package fir

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v4/pkg/internal/offloadtest"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// fakeFir serves /apps and the binary upload, which is the whole release.
type fakeFir struct {
	srv      *httptest.Server
	uploaded []byte
	fileName string
	fields   map[string]string
}

func newFakeFir(t *testing.T) *fakeFir {
	f := &fakeFir{}
	mux := http.NewServeMux()
	mux.HandleFunc("/apps", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": "app-1", "cert": map[string]any{"binary": map[string]any{
			"key": "k1", "token": "t1", "upload_url": f.srv.URL + "/upload",
		}}})
	})
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		file, hdr, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		f.uploaded, _ = io.ReadAll(file)
		f.fileName = hdr.Filename
		f.fields = map[string]string{"key": r.FormValue("key"), "token": r.FormValue("token"), "x:build": r.FormValue("x:build")}
		io.WriteString(w, `{"is_completed":true}`)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func TestUploadSendsThePackage(t *testing.T) {
	data := bytes.Repeat([]byte("fir-apk "), 3000)
	path := filepath.Join(t.TempDir(), "demo.apk")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, remote := range []bool{false, true} {
		f := newFakeFir(t)
		s := &Store{client: resty.New().SetBaseURL(f.srv.URL), apiToken: "tok"}
		req := &store.UploadRequest{FilePath: path, PackageName: "com.example", VersionCode: 7}
		ctx := context.Background()
		var off *offloadtest.Offloader
		if remote {
			req, ctx, off = offloadtest.Remote(t, ctx, req)
		}
		if res := s.Upload(ctx, req); !res.Success {
			t.Fatalf("remote=%v: upload failed: %s", remote, res.Error)
		}
		if !bytes.Equal(f.uploaded, data) || f.fileName != "demo.apk" || f.fields["token"] != "t1" || f.fields["x:build"] != "7" {
			t.Errorf("remote=%v: got %d bytes as %q, fields %v", remote, len(f.uploaded), f.fileName, f.fields)
		}
		if remote && !off.Sent(req.FilePath) {
			t.Errorf("the package did not go out as a transfer")
		}
	}
}
