package xiaomi

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/KevinGong2013/apkgo/v3/pkg/progress"
	"github.com/KevinGong2013/apkgo/v3/pkg/store"
)

// newTestStore returns a Store wired to url, plus the RSA private key whose
// certificate the store encrypts SIG with (so tests can read the sig list back).
func newTestStore(t *testing.T, url string) (*Store, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "xiaomi-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	s, err := New(map[string]string{
		"email":       "dev@example.com",
		"private_key": "secret",
		"cert":        string(certPEM),
	})
	if err != nil {
		t.Fatal(err)
	}
	s.baseURL = url
	return s, key
}

// decryptSIG reverses rsaEncrypt and returns the sig entry names.
func decryptSIG(t *testing.T, key *rsa.PrivateKey, sig string) []string {
	t.Helper()
	raw, err := hex.DecodeString(sig)
	if err != nil {
		t.Fatalf("SIG is not hex: %v", err)
	}
	size := key.PublicKey.Size()
	var plain []byte
	for len(raw) > 0 {
		if len(raw) < size {
			t.Fatalf("SIG tail is %d bytes, want a multiple of %d", len(raw), size)
		}
		chunk, err := rsa.DecryptPKCS1v15(rand.Reader, key, raw[:size])
		if err != nil {
			t.Fatalf("decrypt SIG block: %v", err)
		}
		plain = append(plain, chunk...)
		raw = raw[size:]
	}
	var payload struct {
		Sig []struct {
			Name string `json:"name"`
			Hash string `json:"hash"`
		} `json:"sig"`
	}
	if err := json.Unmarshal(plain, &payload); err != nil {
		t.Fatalf("decode sig payload %q: %v", plain, err)
	}
	names := make([]string, 0, len(payload.Sig))
	for _, e := range payload.Sig {
		if e.Hash == "" {
			t.Errorf("sig entry %q has an empty hash", e.Name)
		}
		names = append(names, e.Name)
	}
	return names
}

// TestPushDualAPKFieldNames pins the /dev/push field names for a 双包 (32/64-bit)
// upload. Xiaomi names the second package "secondApk" — both as the multipart
// file field and in the SIG hash list; "secondApkPath" was silently ignored, so
// only the 32-bit APK reached the store (issue #46).
func TestPushDualAPKFieldNames(t *testing.T) {
	dir := t.TempDir()
	apk32 := filepath.Join(dir, "app-armeabi.apk")
	apk64 := filepath.Join(dir, "app-arm64.apk")
	icon := filepath.Join(dir, "icon.png")
	for _, f := range []string{apk32, apk64, icon} {
		if err := os.WriteFile(f, []byte("content of "+filepath.Base(f)), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var gotFiles map[string]string
	var gotSIG string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
		}
		gotFiles = map[string]string{}
		for field, headers := range r.MultipartForm.File {
			gotFiles[field] = headers[0].Filename
		}
		gotSIG = r.FormValue("SIG")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":0}`))
	}))
	defer srv.Close()

	s, key := newTestStore(t, srv.URL)
	req := &store.UploadRequest{
		AppName:     "Demo",
		PackageName: "com.example.demo",
		FilePath:    apk32,
		File64Path:  apk64,
	}
	if err := s.push(context.Background(), 1, req, icon, progress.Safe(nil)); err != nil {
		t.Fatalf("push: %v", err)
	}

	want := map[string]string{
		"apk":       filepath.Base(apk32),
		"secondApk": filepath.Base(apk64),
		"icon":      filepath.Base(icon),
	}
	for field, filename := range want {
		if gotFiles[field] != filename {
			t.Errorf("multipart field %q = %q, want %q", field, gotFiles[field], filename)
		}
	}
	if len(gotFiles) != len(want) {
		t.Errorf("unexpected multipart file fields: %v", gotFiles)
	}

	// The sig list is built from a fixed slice, so the order is stable run to
	// run (it used to come from map iteration).
	names := decryptSIG(t, key, gotSIG)
	wantSig := []string{"RequestData", "apk", "secondApk", "icon"}
	if !slices.Equal(names, wantSig) {
		t.Errorf("SIG sig list = %v, want %v", names, wantSig)
	}
}

// TestPushHonoursContext checks push aborts on a cancelled context — it used
// to hand context.Background() to the multipart upload, so the global --timeout
// never reached the part that actually takes the time.
func TestPushHonoursContext(t *testing.T) {
	dir := t.TempDir()
	apk32 := filepath.Join(dir, "app.apk")
	icon := filepath.Join(dir, "icon.png")
	for _, f := range []string{apk32, icon} {
		if err := os.WriteFile(f, []byte("content of "+filepath.Base(f)), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release // hang until the test cancels
	}))
	defer srv.Close()
	defer close(release)

	s, _ := newTestStore(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()

	req := &store.UploadRequest{AppName: "Demo", PackageName: "com.example.demo", FilePath: apk32}
	err := s.push(ctx, 1, req, icon, progress.Safe(nil))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("push error = %v, want context.Canceled", err)
	}
}

// TestPushSingleAPKOmitsSecondApk guards the common single-package path: no
// secondApk part, and no stray sig entry for it.
func TestPushSingleAPKOmitsSecondApk(t *testing.T) {
	dir := t.TempDir()
	apk32 := filepath.Join(dir, "app.apk")
	icon := filepath.Join(dir, "icon.png")
	for _, f := range []string{apk32, icon} {
		if err := os.WriteFile(f, []byte("content of "+filepath.Base(f)), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var gotFields []string
	var gotSIG string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
		}
		for field := range r.MultipartForm.File {
			gotFields = append(gotFields, field)
		}
		gotSIG = r.FormValue("SIG")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":0}`))
	}))
	defer srv.Close()

	s, key := newTestStore(t, srv.URL)
	req := &store.UploadRequest{
		AppName:     "Demo",
		PackageName: "com.example.demo",
		FilePath:    apk32,
	}
	if err := s.push(context.Background(), 0, req, icon, progress.Safe(nil)); err != nil {
		t.Fatalf("push: %v", err)
	}

	for _, f := range gotFields {
		if f == "secondApk" {
			t.Errorf("single-APK push sent a secondApk part: %v", gotFields)
		}
	}
	for _, n := range decryptSIG(t, key, gotSIG) {
		if n == "secondApk" {
			t.Error("single-APK push signed a secondApk entry")
		}
	}
}

// TestUploadKeepsStoreAppName pins that an update (synchroType=1) pushes the
// app name already registered on the console (/dev/query result), not the APK
// label — uploading must never rename the store listing (#48). The fixture
// testdata/helloworld.apk comes from shogo82148/androidbinary's MIT-licensed
// testdata; its label is "HelloWorld".
func TestUploadKeepsStoreAppName(t *testing.T) {
	dir := t.TempDir()
	apkPath := filepath.Join(dir, "helloworld.apk")
	raw, err := os.ReadFile(filepath.Join("testdata", "helloworld.apk"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(apkPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	var pushedAppName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/dev/query":
			_, _ = w.Write([]byte(`{"result":0,"packageInfo":{"appName":"控制台注册名","packageName":"com.example.helloworld","versionCode":1,"versionName":"1.0"}}`))
		case "/dev/push":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("parse multipart: %v", err)
			}
			var reqData struct {
				AppInfo struct {
					AppName string `json:"appName"`
				} `json:"appInfo"`
			}
			if err := json.Unmarshal([]byte(r.FormValue("RequestData")), &reqData); err != nil {
				t.Errorf("decode RequestData: %v", err)
			}
			pushedAppName = reqData.AppInfo.AppName
			_, _ = w.Write([]byte(`{"result":0}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	s, _ := newTestStore(t, srv.URL)
	s.client.SetBaseURL(srv.URL)
	req := &store.UploadRequest{
		AppName:     "HelloWorld", // APK label — must not reach the store on update
		PackageName: "com.example.helloworld",
		VersionCode: 2,
		FilePath:    apkPath,
	}
	if err := s.upload(context.Background(), req); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if pushedAppName != "控制台注册名" {
		t.Errorf("pushed appName = %q, want existing store name %q", pushedAppName, "控制台注册名")
	}
}
