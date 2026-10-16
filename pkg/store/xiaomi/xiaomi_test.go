package xiaomi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"image/png"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KevinGong2013/apkgo/v4/pkg/internal/offloadtest"
	"github.com/KevinGong2013/apkgo/v4/pkg/progress"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
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

// sigEntry is one decrypted SIG list item: a part name and its md5.
type sigEntry struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

// decryptSIG reverses rsaEncrypt and returns the sig entry names.
func decryptSIG(t *testing.T, key *rsa.PrivateKey, sig string) []string {
	t.Helper()
	entries := decryptSIGEntries(t, key, sig)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
	}
	return names
}

// decryptSIGEntries reverses rsaEncrypt and returns the sig list in order.
func decryptSIGEntries(t *testing.T, key *rsa.PrivateKey, sig string) []sigEntry {
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
		Sig []sigEntry `json:"sig"`
	}
	if err := json.Unmarshal(plain, &payload); err != nil {
		t.Fatalf("decode sig payload %q: %v", plain, err)
	}
	for _, e := range payload.Sig {
		if e.Hash == "" {
			t.Errorf("sig entry %q has an empty hash", e.Name)
		}
	}
	return payload.Sig
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

// copyFixtureAPK copies testdata/helloworld.apk into dir, so a test can see
// what an upload leaves next to it.
func copyFixtureAPK(t *testing.T, dir string) string {
	t.Helper()
	apkPath := filepath.Join(dir, "helloworld.apk")
	raw, err := os.ReadFile(filepath.Join("testdata", "helloworld.apk"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(apkPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return apkPath
}

// TestExtractIconPicksDensestLauncherIcon pins the fix for #51: the fixture
// ships mdpi..xxxhdpi launcher icons (48..192px) and extraction must take
// the densest one. The old code requested density 0 (= mdpi) and pushed the
// 48px variant, which replaced the console's HD icon with a blurry one.
func TestExtractIconPicksDensestLauncherIcon(t *testing.T) {
	apkPath := copyFixtureAPK(t, t.TempDir())
	iconPath, err := extractIcon(store.PackageFile{Path: apkPath})
	if err != nil {
		t.Fatalf("extractIcon: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(iconPath))
	f, err := os.Open(iconPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatalf("decode extracted icon: %v", err)
	}
	if cfg.Width != 192 || cfg.Height != 192 {
		t.Errorf("extracted icon is %dx%d, want 192x192 (xxxhdpi)", cfg.Width, cfg.Height)
	}
}

// spyReporter records the byte total push announces and the bytes then
// reported as sent (Add runs on the request-writing goroutine).
type spyReporter struct{ total, added atomic.Int64 }

func (r *spyReporter) Phase(string)  {}
func (r *spyReporter) Total(n int64) { r.total.Store(n) }
func (r *spyReporter) Add(n int64)   { r.added.Add(n) }

// TestUploadRemotePackage runs a 双包 upload end to end (query, launcher
// icon, push) with the packages on disk, then the way a service that keeps
// them in object storage does: paths that don't exist here, FileFacts and
// an Offloader (offloadtest.Remote). /dev/push must get the same parts
// under the same names, signed with the same md5s, and progress must count
// exactly the bytes it announced.
func TestUploadRemotePackage(t *testing.T) {
	const queryResp = `{"result":0,"packageInfo":{"appName":"HelloWorld","packageName":"com.example.helloworld","versionCode":1,"versionName":"1.0"}}`
	dir := t.TempDir()
	apk32 := copyFixtureAPK(t, dir)
	raw, err := os.ReadFile(apk32)
	if err != nil {
		t.Fatal(err)
	}
	apk64 := filepath.Join(dir, "helloworld-arm64.apk")
	if err := os.WriteFile(apk64, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	var localSigs []sigEntry
	for _, remote := range []bool{false, true} {
		srv, got := newPushServer(t, queryResp)
		s, key := newTestStore(t, srv.URL)
		s.client.SetBaseURL(srv.URL)
		rep := &spyReporter{}
		req := &store.UploadRequest{
			PackageName: "com.example.helloworld",
			VersionCode: 2,
			FilePath:    apk32,
			File64Path:  apk64,
			Progress:    rep,
		}
		ctx := context.Background()
		var off *offloadtest.Offloader
		if remote {
			req, ctx, off = offloadtest.Remote(t, ctx, req)
		}
		if res := s.Upload(ctx, req); !res.Success {
			t.Fatalf("remote=%v: upload failed: %s", remote, res.Error)
		}

		wantParts := []string{"apk", "icon", "secondApk"}
		if parts := got.fileFields(); !slices.Equal(parts, wantParts) {
			t.Fatalf("remote=%v: multipart file parts = %v, want %v", remote, parts, wantParts)
		}
		for field, name := range map[string]string{"apk": "helloworld.apk", "secondApk": "helloworld-arm64.apk"} {
			if p := got.part(t, field); !bytes.Equal(p.Body, raw) || p.FileName != name {
				t.Errorf("remote=%v: %s = %d bytes as %q, want the %d-byte package as %q", remote, field, len(p.Body), p.FileName, len(raw), name)
			}
		}
		icon := got.part(t, "icon")
		if cfg, err := png.DecodeConfig(bytes.NewReader(icon.Body)); err != nil || cfg.Width != 192 || icon.FileName != "apkgo_icon_tmp.png" {
			t.Errorf("remote=%v: icon part %q = %dx%d (err %v), want the 192px launcher icon as apkgo_icon_tmp.png", remote, icon.FileName, cfg.Width, cfg.Height, err)
		}

		// Each signed md5 matches the part sent under its name — and the
		// remote run signs exactly what the local one did.
		sigs := decryptSIGEntries(t, key, got.field("SIG"))
		names := make([]string, 0, len(sigs))
		for _, e := range sigs {
			names = append(names, e.Name)
		}
		if want := []string{"RequestData", "apk", "secondApk", "icon"}; !slices.Equal(names, want) {
			t.Fatalf("remote=%v: SIG sig list = %v, want %v", remote, names, want)
		}
		for _, e := range sigs[1:] {
			if h := md5Of(got.part(t, e.Name).Body); e.Hash != h {
				t.Errorf("remote=%v: sig %s hash = %s, uploaded part md5 = %s", remote, e.Name, e.Hash, h)
			}
		}
		if !remote {
			localSigs = sigs
		} else if !slices.Equal(sigs, localSigs) {
			t.Errorf("remote SIG list = %v, local = %v", sigs, localSigs)
		}

		var sent int64
		for _, f := range wantParts {
			sent += int64(len(got.part(t, f).Body))
		}
		if total, added := rep.total.Load(), rep.added.Load(); total != sent || added != sent {
			t.Errorf("remote=%v: progress total %d, added %d, want both %d", remote, total, added, sent)
		}

		if remote && (!off.Sent(req.FilePath) || !off.Sent(req.File64Path)) {
			t.Errorf("the packages did not go out as a transfer")
		}
	}
}

func TestNewWithDefaultCert(t *testing.T) {
	s, err := New(map[string]string{
		"email":       "dev@example.com",
		"private_key": "secret",
	})
	if err != nil {
		t.Fatalf("New with default cert failed: %v", err)
	}
	if s.pubKey == nil {
		t.Fatal("expected pubKey to be loaded from defaultCertPEM")
	}
	if s.pubKey.N.BitLen() != 1024 {
		t.Errorf("got key size %d, want 1024", s.pubKey.N.BitLen())
	}
}

// rateLimitServer answers /dev/query with -20035 for the first limited
// calls and with the package after that, counting the calls.
func rateLimitServer(t *testing.T, limited int32, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dev/query" {
			t.Errorf("unexpected path %s", r.URL.Path)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) <= limited {
			_, _ = w.Write([]byte(`{"result":-20035,"message":"请求频率太快"}`))
			return
		}
		_, _ = w.Write([]byte(`{"result":0,"packageInfo":{"packageName":"com.example.helloworld","versionCode":1,"versionName":"1.0"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func shortRateLimitWaits(t *testing.T) {
	t.Helper()
	old := queryRateLimitWaits
	queryRateLimitWaits = []time.Duration{time.Millisecond, time.Millisecond}
	t.Cleanup(func() { queryRateLimitWaits = old })
}

// 2026-10-16: a /dev/query 54ms after another one for the same account got
// -20035, and the upload failed with it. query waits and asks again.
func TestQueryRetriesRateLimit(t *testing.T) {
	shortRateLimitWaits(t)
	var calls atomic.Int32
	srv := rateLimitServer(t, 2, &calls)
	s, _ := newTestStore(t, srv.URL)
	s.client.SetBaseURL(srv.URL)

	info, err := s.query(context.Background(), "com.example.helloworld")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if info == nil || info.VersionCode != 1 {
		t.Errorf("info = %+v, want versionCode 1", info)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3 (two -20035, then the answer)", got)
	}
}

// Still -20035 after every wait: the upload fails as store_busy, which
// orchestrators retry minutes later, instead of unknown.
func TestUploadRateLimitedIsStoreBusy(t *testing.T) {
	shortRateLimitWaits(t)
	var calls atomic.Int32
	srv := rateLimitServer(t, 100, &calls)
	s, _ := newTestStore(t, srv.URL)
	s.client.SetBaseURL(srv.URL)

	res := s.Upload(context.Background(), &store.UploadRequest{PackageName: "com.example.helloworld", VersionCode: 2})
	if res.Success {
		t.Fatal("upload succeeded, want failure")
	}
	if res.Category != store.CategoryStoreBusy {
		t.Errorf("category = %q, want store_busy (error %q)", res.Category, res.Error)
	}
	if want := int32(len(queryRateLimitWaits) + 1); calls.Load() != want {
		t.Errorf("calls = %d, want %d", calls.Load(), want)
	}
}

// A wrong private key is auth_failed and asked once: waiting won't fix it.
func TestQueryWrongPasswordIsAuthFailed(t *testing.T) {
	shortRateLimitWaits(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":-20014,"message":"密码错误"}`))
	}))
	defer srv.Close()
	s, _ := newTestStore(t, srv.URL)
	s.client.SetBaseURL(srv.URL)

	_, err := s.query(context.Background(), "com.example.helloworld")
	if got := store.CategoryOf(err); got != store.CategoryAuthFailed {
		t.Errorf("category = %q, want auth_failed (error %v)", got, err)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
}

// The wait between retries gives up as soon as the context is done.
func TestQueryRateLimitWaitHonoursContext(t *testing.T) {
	old := queryRateLimitWaits
	queryRateLimitWaits = []time.Duration{time.Hour}
	t.Cleanup(func() { queryRateLimitWaits = old })
	var calls atomic.Int32
	srv := rateLimitServer(t, 100, &calls)
	s, _ := newTestStore(t, srv.URL)
	s.client.SetBaseURL(srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := s.query(ctx, "com.example.helloworld")
	if store.CategoryOf(err) != store.CategoryStoreBusy {
		t.Errorf("err = %v, want the -20035 (store_busy)", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("query waited %s after the context was done", time.Since(start))
	}
}

// TestDiagnoseChecksKeyWithoutPackage: with no package hint, diagnose must
// still reach /dev/query — cert alone never talks to Xiaomi, and passing on
// cert alone is how a wrong private key got saved as verified and only failed
// at publish time with -20014 密码错误.
func TestDiagnoseChecksKeyWithoutPackage(t *testing.T) {
	for _, tc := range []struct {
		name, resp, wantStatus string
	}{
		{"accepted", `{"result":0,"create":true}`, "ok"},
		{"wrong key", `{"result":-20014,"message":"密码错误"}`, "fail"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotPkg string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/dev/query" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				var reqData struct {
					PackageName string `json:"packageName"`
				}
				_ = r.ParseForm()
				_ = json.Unmarshal([]byte(r.FormValue("RequestData")), &reqData)
				gotPkg = reqData.PackageName
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.resp))
			}))
			defer srv.Close()

			s, _ := newTestStore(t, srv.URL)
			s.client.SetBaseURL(srv.URL)
			probes := s.diagnose(context.Background(), store.DiagnoseHint{})
			if len(probes) != 2 || probes[1].Name != "query" {
				t.Fatalf("probes = %+v, want cert + query", probes)
			}
			if probes[1].Status != tc.wantStatus {
				t.Errorf("query status = %q (%+v), want %q", probes[1].Status, probes[1], tc.wantStatus)
			}
			if gotPkg != probePackage {
				t.Errorf("queried package %q, want %q", gotPkg, probePackage)
			}
		})
	}
}
