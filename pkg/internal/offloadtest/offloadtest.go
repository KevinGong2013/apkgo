// Package offloadtest runs a store's Upload the way a service that keeps
// packages in object storage does: the package paths don't exist
// locally, every store.FileFacts field is given, and an httpx.Offloader
// supplies the bytes. A store passes when the upload behaves exactly as
// it does with local files — proof that it reads the package only
// through store.PackageFile and sends it only through httpx.
package offloadtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/KevinGong2013/apkgo/v4/pkg/apk"
	"github.com/KevinGong2013/apkgo/v4/pkg/httpx"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// Offloader takes every transfer, then declines it so it is sent from
// this process with the bytes read from the real copy. It records what it
// was offered.
type Offloader struct {
	mu        sync.Mutex
	copies    map[string]string // remote path → real file
	transfers []*httpx.Transfer
}

var _ httpx.Offloader = (*Offloader)(nil)

func (o *Offloader) Offload(_ context.Context, t *httpx.Transfer) (*http.Response, error) {
	o.mu.Lock()
	o.transfers = append(o.transfers, t)
	o.mu.Unlock()
	return nil, httpx.ErrSendLocally
}

func (o *Offloader) Open(_ context.Context, f httpx.FileRef) (io.ReadCloser, error) {
	o.mu.Lock()
	real, ok := o.copies[f.Path]
	o.mu.Unlock()
	if !ok {
		real = f.Path // a local file (a listing image)
	}
	return os.Open(real)
}

// Transfers returns the transfers offered so far.
func (o *Offloader) Transfers() []*httpx.Transfer {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]*httpx.Transfer(nil), o.transfers...)
}

// Sent reports whether the file at remote path went out in a transfer,
// whole.
func (o *Offloader) Sent(path string) bool {
	for _, t := range o.Transfers() {
		for _, f := range t.Files() {
			if f.Path == path {
				return true
			}
		}
	}
	return false
}

// Remote returns a copy of req whose FilePath / File64Path name files
// that don't exist here, with their facts computed from the real files,
// a context carrying an Offloader that knows the real copies, and the
// Offloader itself.
func Remote(t testing.TB, ctx context.Context, req *store.UploadRequest) (*store.UploadRequest, context.Context, *Offloader) {
	t.Helper()
	o := &Offloader{copies: map[string]string{}}
	out := *req
	dir := remoteDir(t)
	if req.FilePath != "" {
		out.FilePath, out.FileFacts = remote(t, o, dir, "32", req.FilePath)
	}
	if req.File64Path != "" {
		out.File64Path, out.File64Facts = remote(t, o, dir, "64", req.File64Path)
	}
	return &out, httpx.WithOffloader(ctx, o), o
}

func remote(t testing.TB, o *Offloader, dir, slot, real string) (string, *store.FileFacts) {
	t.Helper()
	local := store.PackageFile{Path: real}
	size, err := local.Size()
	if err != nil {
		t.Fatal(err)
	}
	md5Hex, err := local.MD5()
	if err != nil {
		t.Fatal(err)
	}
	shaHex, err := local.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	facts := &store.FileFacts{Size: size, MD5: md5Hex, SHA256: shaHex}
	// A fixture that isn't a real APK has neither: say so, rather than
	// leave the store to look (it would fail on the missing file).
	if facts.ABIs, err = apk.ABIs(real); err != nil {
		facts.ABIs = []string{}
	}
	icon := filepath.Join(t.TempDir(), "icon-"+slot+".png")
	if f, err := os.Open(real); err == nil {
		if apk.WriteLauncherIconPNG(f, size, icon) == nil {
			facts.Icon = icon
		}
		f.Close()
	}
	path := filepath.Join(dir, slot, filepath.Base(real))
	o.copies[path] = real
	return path, facts
}

func remoteDir(t testing.TB) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(os.TempDir(), "apkgo-offloadtest-"+hex.EncodeToString(b[:]))
}
