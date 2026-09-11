package harmony

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v3/pkg/store"
)

// fakeAGC is an httptest server scripting the HarmonyOS Publishing API.
type fakeAGC struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	calls   []string
	putBody []byte
	putHdr  http.Header
	putHost string

	compilePolls int // how many status polls report "still compiling"
	submitBusy   int // how many submits report "still compiling"
	notesLang    string
	notesText    string
	submitBody   map[string]any
	uploadQuery  map[string]string
}

func newFakeAGC(t *testing.T) *fakeAGC {
	f := &fakeAGC{t: t}
	mux := http.NewServeMux()
	ok := func(w http.ResponseWriter, extra map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		body := map[string]any{"ret": map[string]any{"code": 0, "msg": "success"}}
		for k, v := range extra {
			body[k] = v
		}
		json.NewEncoder(w).Encode(body)
	}
	mux.HandleFunc("/api/publish/v2/appid-list", func(w http.ResponseWriter, r *http.Request) {
		f.record("appid-list")
		if r.URL.Query().Get("packageTypes") != "7" {
			t.Errorf("appid-list packageTypes = %q, want 7", r.URL.Query().Get("packageTypes"))
		}
		if r.URL.Query().Get("packageName") != "com.example.harmony" {
			ok(w, map[string]any{"appids": []any{}})
			return
		}
		ok(w, map[string]any{"appids": []map[string]string{{"key": "com.example.harmony", "value": "app-1"}}})
	})
	mux.HandleFunc("/api/publish/v2/upload-url/for-obs", func(w http.ResponseWriter, r *http.Request) {
		f.record("upload-url")
		f.mu.Lock()
		f.uploadQuery = map[string]string{}
		for k := range r.URL.Query() {
			f.uploadQuery[k] = r.URL.Query().Get(k)
		}
		f.mu.Unlock()
		ok(w, map[string]any{"urlInfo": map[string]any{
			"objectId": "CN/2026091101/obj.app",
			"url":      f.srv.URL + "/obs/CN/2026091101/obj.app",
			"method":   "PUT",
			"headers": map[string]string{
				"Authorization":        "AWS4-HMAC-SHA256 sig",
				"x-amz-content-sha256": "UNSIGNED-PAYLOAD",
				"x-amz-date":           "20260911T000000Z",
				"Host":                 "obs.example.com",
				"user-agent":           "agc",
				"Content-Type":         "application/octet-stream",
			},
		}})
	})
	mux.HandleFunc("/obs/", func(w http.ResponseWriter, r *http.Request) {
		f.record("obs-put")
		if r.Method != http.MethodPut {
			t.Errorf("obs method = %s", r.Method)
		}
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.putBody = b
		f.putHdr = r.Header.Clone()
		f.putHost = r.Host
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/publish/v3/app-package-info", func(w http.ResponseWriter, r *http.Request) {
		f.record("app-package-info")
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["objectId"] != "CN/2026091101/obj.app" || !strings.HasSuffix(body["fileName"], ".app") {
			t.Errorf("app-package-info body = %v", body)
		}
		ok(w, map[string]any{"packageId": "pkg-9"})
	})
	mux.HandleFunc("/api/publish/v3/app-info", func(w http.ResponseWriter, r *http.Request) {
		f.record("app-info")
		ok(w, map[string]any{"appInfo": map[string]any{
			"releaseState": 5, "defaultLang": "zh-CN",
			"versionNumber": "1.0.3", "versionCode": 1000003,
			"onShelfVersionNumber": "1.0.2", "onShelfVersionCode": 1000002,
		}})
	})
	mux.HandleFunc("/api/publish/v3/app-language-info", func(w http.ResponseWriter, r *http.Request) {
		f.record("app-language-info")
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.notesLang, f.notesText = body["lang"], body["newFeatures"]
		f.mu.Unlock()
		ok(w, nil)
	})
	mux.HandleFunc("/api/publish/v3/package/compile/status", func(w http.ResponseWriter, r *http.Request) {
		f.record("compile-status")
		f.mu.Lock()
		state := 0
		if f.compilePolls > 0 {
			f.compilePolls--
			state = 1
		}
		f.mu.Unlock()
		ok(w, map[string]any{"pkgStateList": []map[string]any{{"pkgId": r.URL.Query().Get("pkgIds"), "successStatus": state}}})
	})
	mux.HandleFunc("/api/publish/v3/app-submit", func(w http.ResponseWriter, r *http.Request) {
		f.record("app-submit")
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.submitBody = body
		busy := f.submitBusy > 0
		if busy {
			f.submitBusy--
		}
		f.mu.Unlock()
		if busy {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"ret": map[string]any{"code": 204144727, "msg": "软件包正在编译中，应用提交审核请3至5分钟之后再试。"}})
			return
		}
		ok(w, nil)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAGC) record(name string) {
	f.mu.Lock()
	f.calls = append(f.calls, name)
	f.mu.Unlock()
}

func (f *fakeAGC) store() *Store {
	return &Store{
		client:         resty.New().SetBaseURL(f.srv.URL).SetHeader("Content-Type", "application/json"),
		upload:         f.srv.Client(),
		pollInterval:   time.Millisecond,
		compileTimeout: time.Second,
	}
}

func writeApp(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("fake harmony app pack bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUploadHappyPath(t *testing.T) {
	f := newFakeAGC(t)
	f.compilePolls = 2
	f.submitBusy = 1
	s := f.store()
	s.mainlandFlag = "1"
	path := writeApp(t, "demo-default-signed.app")
	rt := time.Date(2026, 10, 1, 10, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))

	res := s.Upload(context.Background(), &store.UploadRequest{
		FilePath:     path,
		PackageName:  "com.example.harmony",
		VersionCode:  1000003,
		VersionName:  "1.0.3",
		ReleaseNotes: "修复若干问题",
		ReleaseTime:  &rt,
	})
	if !res.Success {
		t.Fatalf("upload failed: %s (category %s)", res.Error, res.Category)
	}
	if res.ExternalID != "pkg-9" {
		t.Errorf("ExternalID = %q, want pkg-9", res.ExternalID)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	want := []string{"appid-list", "upload-url", "obs-put", "app-package-info", "app-info", "app-language-info",
		"compile-status", "compile-status", "compile-status", "app-submit", "app-submit"}
	if strings.Join(f.calls, ",") != strings.Join(want, ",") {
		t.Errorf("calls = %v\nwant    %v", f.calls, want)
	}
	if string(f.putBody) != "fake harmony app pack bytes" {
		t.Errorf("uploaded body = %q", f.putBody)
	}
	if f.putHdr.Get("x-amz-date") != "20260911T000000Z" || f.putHdr.Get("Authorization") != "AWS4-HMAC-SHA256 sig" {
		t.Errorf("signed headers not forwarded: %v", f.putHdr)
	}
	if f.putHost != "obs.example.com" {
		t.Errorf("Host = %q, want obs.example.com", f.putHost)
	}
	sum := sha256.Sum256([]byte("fake harmony app pack bytes"))
	if f.uploadQuery["sha256"] != hex.EncodeToString(sum[:]) || f.uploadQuery["contentLength"] != "27" ||
		f.uploadQuery["fileName"] != "demo-default-signed.app" || f.uploadQuery["chineseMainlandFlag"] != "1" {
		t.Errorf("upload-url query = %v", f.uploadQuery)
	}
	if f.notesLang != "zh-CN" || f.notesText != "修复若干问题" {
		t.Errorf("release notes = %q/%q", f.notesLang, f.notesText)
	}
	if f.submitBody["releaseTime"] != "2026-10-01T10:00:00+0800" {
		t.Errorf("submit releaseTime = %v", f.submitBody["releaseTime"])
	}
}

func TestUploadRejectsHapAndApk(t *testing.T) {
	s := newFakeAGC(t).store()
	for _, name := range []string{"entry-default-signed.hap", "app-release.apk"} {
		res := s.Upload(context.Background(), &store.UploadRequest{FilePath: writeApp(t, name), PackageName: "com.example.harmony"})
		if res.Success || res.Category != store.CategoryConfigInvalid {
			t.Errorf("%s: expected config_invalid failure, got %+v", name, res)
		}
	}
}

func TestUploadUnknownBundleName(t *testing.T) {
	s := newFakeAGC(t).store()
	res := s.Upload(context.Background(), &store.UploadRequest{FilePath: writeApp(t, "x.app"), PackageName: "com.example.other"})
	if res.Success || res.Category != store.CategoryConfigInvalid || !strings.Contains(res.Error, "no HarmonyOS app found") {
		t.Errorf("got %+v", res)
	}
}

func TestUploadCompileFailureIsPolicyBlock(t *testing.T) {
	f2 := newFakeAGC(t)
	orig := f2.srv.Config.Handler
	f2.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/publish/v3/package/compile/status" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"ret": map[string]any{"code": 0}, "pkgStateList": []map[string]any{{"pkgId": "pkg-9", "successStatus": 2}}})
			return
		}
		orig.ServeHTTP(w, r)
	})
	res := f2.store().Upload(context.Background(), &store.UploadRequest{FilePath: writeApp(t, "x.app"), PackageName: "com.example.harmony"})
	if res.Success || res.Category != store.CategoryPolicyBlock {
		t.Errorf("got %+v", res)
	}
	if !strings.Contains(res.Error, consoleURL) {
		t.Errorf("error should carry the console hint: %s", res.Error)
	}
}

func TestAuditMapsAppInfo(t *testing.T) {
	f := newFakeAGC(t)
	s := f.store()
	// audit() constructs via New(); exercise the internal path instead.
	ai, err := s.getAppInfo(context.Background(), "app-1")
	if err != nil {
		t.Fatal(err)
	}
	st, _ := mapReleaseState(ai.ReleaseState)
	if st != store.AuditReviewing || ai.VersionNumber != "1.0.3" || ai.OnShelfVersionCode != 1000002 {
		t.Errorf("unexpected app-info: %+v → %s", ai, st)
	}
}

func TestMapReleaseState(t *testing.T) {
	cases := map[int]store.AuditState{
		4: store.AuditReviewing, 5: store.AuditReviewing, 12: store.AuditReviewing,
		0: store.AuditApproved, 3: store.AuditApproved,
		1: store.AuditRejected, 8: store.AuditRejected, 13: store.AuditRejected,
		2: store.AuditWithdrawn, 10: store.AuditWithdrawn, 11: store.AuditWithdrawn,
		7: store.AuditUnknown, 99: store.AuditUnknown,
	}
	for state, want := range cases {
		if got, _ := mapReleaseState(state); got != want {
			t.Errorf("mapReleaseState(%d) = %q, want %q", state, got, want)
		}
	}
}

func TestIsCompiling(t *testing.T) {
	if !isCompiling(retInfo{Code: 204144727, Msg: "编译中"}) || !isCompiling(retInfo{Code: 204144719}) {
		t.Error("compile codes should be retryable")
	}
	if !isCompiling(retInfo{Code: 204144660, Msg: "app package is parsing"}) {
		t.Error("204144660 parsing message should be retryable")
	}
	if isCompiling(retInfo{Code: 204144660, Msg: "registeredEntity can not be empty"}) {
		t.Error("204144660 config message must not be retried")
	}
}

func TestRegisteredInStoreRegistry(t *testing.T) {
	var found *store.ConfigSchema
	for _, sc := range store.Schemas() {
		if sc.Name == StoreName {
			s := sc
			found = &s
		}
	}
	if found == nil {
		t.Fatal("harmony not registered")
	}
	if found.Platform != store.PlatformHarmony || !found.SupportsScheduledRelease || found.SupportsURLPush {
		t.Errorf("schema flags wrong: %+v", found)
	}
	if store.Platform("harmony") != store.PlatformHarmony || store.Platform("harmony.cn") != store.PlatformHarmony || store.Platform("huawei") != store.PlatformAndroid {
		t.Error("store.Platform resolution wrong")
	}
	if _, ok := store.QueryAudit(context.Background(), StoreName, map[string]string{}, store.AuditQuery{Package: "x"}); !ok {
		t.Error("auditor not registered")
	}
	if _, ok := store.Diagnose(context.Background(), StoreName, map[string]string{}, store.DiagnoseHint{}); !ok {
		t.Error("diagnoser not registered")
	}
	if _, err := New(map[string]string{}); err == nil || !strings.Contains(err.Error(), "service_account") {
		t.Errorf("New without creds should explain the credential options, got %v", err)
	}
}
