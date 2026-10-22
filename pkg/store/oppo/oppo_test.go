package oppo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v4/pkg/internal/offloadtest"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

func TestParseError(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "envelope message with empty array data",
			body: `{"errno": 10001, "message": "应用不存在", "data": []}`,
			want: "应用不存在",
		},
		{
			name: "envelope msg with empty array data",
			body: `{"errno": 10002, "msg": "签名错误", "data": []}`,
			want: "签名错误",
		},
		{
			name: "data nested message",
			body: `{"errno": 10003, "data": {"message": "内部错误"}}`,
			want: "内部错误",
		},
		{
			name: "data nested msg",
			body: `{"errno": 10004, "data": {"msg": "参数错误"}}`,
			want: "参数错误",
		},
		{
			name: "no message fields, fallback to raw body",
			body: `{"errno": 10005, "data": []}`,
			want: `{"errno": 10005, "data": []}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseError([]byte(tt.body))
			if got != tt.want {
				t.Errorf("parseError() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestQueryApp_EmptyArrayData(t *testing.T) {
	// OPPO returns data: [] when no app exists under this developer account
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"errno": 0, "data": []}`))
	}))
	defer srv.Close()

	s := &Store{
		client:       resty.New().SetBaseURL(srv.URL),
		accessToken:  "test-token",
		clientSecret: "test-secret",
	}

	app, err := s.queryApp(context.Background(), "com.example.nonexistent")
	if err != nil {
		t.Fatalf("expected nil error on empty data array, got: %v", err)
	}
	if app != nil {
		t.Fatalf("expected nil app on empty data array, got: %+v", app)
	}
}

func TestQueryApp_ErrorWithArrayData(t *testing.T) {
	// OPPO returns errno != 0 along with data: []
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"errno": 10001, "message": "应用不存在或无权访问", "data": []}`))
	}))
	defer srv.Close()

	s := &Store{
		client:       resty.New().SetBaseURL(srv.URL),
		accessToken:  "test-token",
		clientSecret: "test-secret",
	}

	app, err := s.queryApp(context.Background(), "com.example.nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if app != nil {
		t.Fatalf("expected nil app on error, got: %+v", app)
	}
	expectedErr := "[10001] 应用不存在或无权访问"
	if err.Error() != expectedErr {
		t.Errorf("error = %q, want %q", err.Error(), expectedErr)
	}
}

func TestQueryApp_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"errno": 0, "data": {"app_name": "Test App", "version_name": "1.0.0", "version_code": "100"}}`))
	}))
	defer srv.Close()

	s := &Store{
		client:       resty.New().SetBaseURL(srv.URL),
		accessToken:  "test-token",
		clientSecret: "test-secret",
	}

	app, err := s.queryApp(context.Background(), "com.example.app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if app == nil {
		t.Fatal("expected non-nil app")
	}
	if app.AppName != "Test App" || app.VersionName != "1.0.0" || app.VersionCode != "100" {
		t.Errorf("app data mismatch: %+v", app)
	}
}

func TestUpload_TaskStillPending(t *testing.T) {
	// task-state can stay 1 (待处理) past the polling budget (2026-10-15).
	// app/info then decides: this version in review or live is a success;
	// anything else — such as the same version rejected earlier, which
	// app/info keeps showing while the resubmission's task runs — is
	// store_busy, since a retry waits on the same task (911216).
	for _, tc := range []struct {
		name           string
		version, audit string
		wantCat        store.Category
	}{
		{"in review", "2", "审核中", store.CategorySuccess},
		{"live", "2", "上线", store.CategorySuccess},
		{"same version rejected earlier", "2", "审核不通过", store.CategoryStoreBusy},
		{"previous version in review", "1", "审核中", store.CategoryStoreBusy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeOppo(t)
			old := taskPollBudget
			taskPollBudget = 20 * time.Millisecond
			t.Cleanup(func() { taskPollBudget = old })
			f.taskState = "1"
			f.infoVersion, f.infoAudit = tc.version, tc.audit

			res := f.store().Upload(context.Background(), uploadRequest(t, nil))
			if res.Category != tc.wantCat {
				t.Fatalf("category = %q (error %q), want %q", res.Category, res.Error, tc.wantCat)
			}
			if want := tc.wantCat == store.CategorySuccess; res.Success != want {
				t.Errorf("success = %v, want %v", res.Success, want)
			}
			if !res.Success && !strings.Contains(res.Error, "still pending") {
				t.Errorf("error = %q, want the still-pending message", res.Error)
			}
			calls, _ := f.seen()
			if !slices.Contains(calls, "task-state") || calls[len(calls)-1] != "info" {
				t.Errorf("calls = %v, want task-state polls then a closing app/info", calls)
			}
		})
	}
}

func TestPollTaskState_LeavesRoomBeforeDeadline(t *testing.T) {
	// A caller timeout shorter than taskPollBudget (the CLI's default -t is
	// 10m, same as the budget) must still leave time for the closing
	// app/info check instead of ending in "polling cancelled".
	f := newFakeOppo(t)
	f.taskState = "1"
	f.infoVersion, f.infoAudit = "2", "审核中"
	ctx, cancel := context.WithTimeout(context.Background(), taskFinalCheckReserve+50*time.Millisecond)
	defer cancel()

	if err := f.store().pollTaskState(ctx, "com.example.app", "2"); err != nil {
		t.Fatalf("pollTaskState: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("polled until the caller's deadline")
	}
	if calls, _ := f.seen(); !slices.Contains(calls, "task-state") || calls[len(calls)-1] != "info" {
		t.Errorf("calls = %v, want task-state polls then a closing app/info", calls)
	}
}

// byteCount is a progress.Reporter that tallies an upload's bytes.
type byteCount struct {
	mu          sync.Mutex
	total, sent int64
}

func (c *byteCount) Phase(string) {}

func (c *byteCount) Total(n int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.total = n
}

func (c *byteCount) Add(n int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent += n
}

// is reports whether the total was n and all n bytes were sent.
func (c *byteCount) is(n int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total == n && c.sent == n
}

// TestUploadSendsThePackage runs a split upload whose stored icon fails
// OPPO's rule, with the APKs on local disk and, as apkgo-cloud does, with
// only their facts and an Offloader. OPPO gets the same bytes either way,
// and the same replacement icon: rebuilt from the APK's launcher icon here,
// from FileFacts.Icon there.
func TestUploadSendsThePackage(t *testing.T) {
	apk32, err := os.ReadFile(fixtureAPK)
	if err != nil {
		t.Fatal(err)
	}
	apk64 := []byte("fake 64-bit apk")
	path64 := filepath.Join(t.TempDir(), "app64.apk")
	if err := os.WriteFile(path64, apk64, 0o644); err != nil {
		t.Fatal(err)
	}

	var icons [][]byte // the replacement icon each run uploaded
	for _, remote := range []bool{false, true} {
		f := newFakeOppo(t)
		f.staleIcon = true
		rep := &byteCount{}
		req := &store.UploadRequest{
			FilePath:     fixtureAPK,
			File64Path:   path64,
			PackageName:  "com.example.app",
			VersionCode:  2,
			ReleaseNotes: "修复若干问题",
			Progress:     rep,
		}
		ctx := context.Background()
		var off *offloadtest.Offloader
		if remote {
			req, ctx, off = offloadtest.Remote(t, ctx, req)
		}
		if res := f.store().Upload(ctx, req); !res.Success {
			t.Fatalf("remote=%v: upload failed: %s", remote, res.Error)
		}

		got := f.received()
		if len(got) != 3 {
			t.Fatalf("remote=%v: %d files uploaded, want the two APKs and an icon", remote, len(got))
		}
		if got[0].typ != "apk" || got[0].name != "helloworld.apk" || !bytes.Equal(got[0].data, apk32) {
			t.Errorf("remote=%v: 32-bit upload got %d bytes of type %q as %q", remote, len(got[0].data), got[0].typ, got[0].name)
		}
		if got[1].typ != "apk" || got[1].name != "app64.apk" || !bytes.Equal(got[1].data, apk64) {
			t.Errorf("remote=%v: 64-bit upload got %d bytes of type %q as %q", remote, len(got[1].data), got[1].typ, got[1].name)
		}
		icon := got[2]
		if cfg, format, err := image.DecodeConfig(bytes.NewReader(icon.data)); icon.typ != "photo" || err != nil ||
			format != "png" || cfg.Width != oppoIconSize || cfg.Height != oppoIconSize {
			t.Errorf("remote=%v: replacement icon is a %q upload of %s %dx%d (%v)", remote, icon.typ, format, cfg.Width, cfg.Height, err)
		}
		icons = append(icons, icon.data)

		calls, upd := f.seen()
		wantCalls := []string{
			"info", "upload-url", "upload:apk:helloworld.apk", "upload-url", "upload:apk:app64.apk",
			"icon-check", "upload-url", "upload:photo:" + icon.name, "upd", "task-state",
		}
		if !slices.Equal(calls, wantCalls) {
			t.Errorf("remote=%v: calls:\n got %v\nwant %v", remote, calls, wantCalls)
		}
		var apks []apkInfo
		if err := json.Unmarshal([]byte(upd.Get("apk_url")), &apks); err != nil {
			t.Fatalf("remote=%v: apk_url %q: %v", remote, upd.Get("apk_url"), err)
		}
		wantAPKs := []apkInfo{
			{URL: "https://cdn.test/helloworld.apk", MD5: "md5", CpuCode: 32},
			{URL: "https://cdn.test/app64.apk", MD5: "md5", CpuCode: 64},
		}
		if !slices.Equal(apks, wantAPKs) {
			t.Errorf("remote=%v: apk_url = %+v, want %+v", remote, apks, wantAPKs)
		}
		if want := "https://cdn.test/" + icon.name; upd.Get("icon_url") != want {
			t.Errorf("remote=%v: icon_url = %q, want %q", remote, upd.Get("icon_url"), want)
		}

		// The bar covers both APKs (the icon stays off it).
		if want := int64(len(apk32) + len(apk64)); !rep.is(want) {
			t.Errorf("remote=%v: progress total %d, sent %d, want %d", remote, rep.total, rep.sent, want)
		}
		if remote && (!off.Sent(req.FilePath) || !off.Sent(req.File64Path)) {
			t.Errorf("the packages did not go out as transfers")
		}
	}
	if !bytes.Equal(icons[0], icons[1]) {
		t.Error("the icon rebuilt from FileFacts.Icon differs from the one rebuilt from the APK")
	}
}

// OPPO's gateway now and then answers 504 NoStackTraceTimeoutException
// after ~3 s (seen on get-upload-url in production). One such hiccup is
// re-asked and the upload goes through.
func TestGatewayTimeoutIsRetried(t *testing.T) {
	f := newFakeOppo(t)
	f.gateway504["/resource/v1/upload/get-upload-url"] = 1
	req := &store.UploadRequest{FilePath: fixtureAPK, PackageName: "com.example.app", VersionCode: 2, ReleaseNotes: "x"}
	if res := f.store().Upload(context.Background(), req); !res.Success {
		t.Fatalf("upload failed after one 504: %s (%s)", res.Error, res.Category)
	}
	calls, _ := f.seen()
	if !slices.Contains(calls, "504:/resource/v1/upload/get-upload-url") {
		t.Errorf("the 504 never happened: %v", calls)
	}
}

// A gateway that keeps timing out is given up on after three tries and
// reported as network_retry — the cloud retries those — not unknown.
func TestGatewayTimeoutKeepsFailing(t *testing.T) {
	f := newFakeOppo(t)
	f.gateway504["/resource/v1/upload/get-upload-url"] = 100
	req := &store.UploadRequest{FilePath: fixtureAPK, PackageName: "com.example.app", VersionCode: 2, ReleaseNotes: "x"}
	res := f.store().Upload(context.Background(), req)
	if res.Success || res.Category != store.CategoryNetworkRetry {
		t.Fatalf("got success=%v category=%q (%s), want a network_retry failure", res.Success, res.Category, res.Error)
	}
	calls, _ := f.seen()
	n := 0
	for _, c := range calls {
		if c == "504:/resource/v1/upload/get-upload-url" {
			n++
		}
	}
	if n != 3 {
		t.Errorf("get-upload-url tried %d times, want 3 (1 + 2 retries)", n)
	}
}

// The submit is never sent twice: a 504 on /app/upd may still have
// published, so it fails as is instead of being repeated.
func TestSubmitIsNotRepeated(t *testing.T) {
	f := newFakeOppo(t)
	f.gateway504["/resource/v1/app/upd"] = 1
	req := &store.UploadRequest{FilePath: fixtureAPK, PackageName: "com.example.app", VersionCode: 2, ReleaseNotes: "x"}
	res := f.store().Upload(context.Background(), req)
	if res.Success {
		t.Fatal("upload succeeded although the submit answered 504")
	}
	calls, _ := f.seen()
	submits := 0
	for _, c := range calls {
		if c == "504:/resource/v1/app/upd" || c == "upd" {
			submits++
		}
	}
	if submits != 1 {
		t.Errorf("submit sent %d times, want exactly 1: %v", submits, calls)
	}
	if res.Category == store.CategoryNetworkRetry {
		t.Errorf("a failed submit is tagged network_retry — the cloud would submit again")
	}
}

// The 2026-10-16 case: /app/upd answered "[911001] 版权证书无效" while the
// real cause, visible only in app/info, was that OPPO had rejected the
// app as a 马甲 of another one. The publish error now carries app/info's
// review status and refuse_reason.
func TestPublishErrorCarriesRefuseReason(t *testing.T) {
	const reason = "APP与示例应用存在套用马甲的嫌疑（经检测APK相似度0.92）"
	f := newFakeOppo(t)
	f.updErrno, f.updMessage = 911001, "版权证书无效"
	f.infoVersion, f.infoAudit, f.infoAuditCode, f.infoRefuse = "2", "审核不通过", "444", reason

	res := f.store().Upload(context.Background(), uploadRequest(t, nil))
	if res.Success {
		t.Fatal("upload succeeded although /app/upd answered 911001")
	}
	want := "publish: [911001] 版权证书无效；OPPO 后台审核结果：审核不通过，audit_status=444，版本 1.1.0 (2)，驳回原因：" + reason
	if res.Error != want {
		t.Errorf("error:\n got %q\nwant %q", res.Error, want)
	}
	if res.Category != store.CategoryUnknown {
		t.Errorf("category = %q, want it unchanged (unknown)", res.Category)
	}
	if calls, _ := f.seen(); calls[len(calls)-1] != "info" || !slices.Contains(calls, "upd") {
		t.Errorf("calls = %v, want the failed upd followed by an app/info lookup", calls)
	}
}

// A failed app/info lookup — an errno, or a gateway that keeps timing out
// (which on its own would be network_retry) — leaves the publish error
// exactly as OPPO gave it.
func TestPublishErrorWhenAuditLookupFails(t *testing.T) {
	for _, mode := range []string{"errno", "504"} {
		t.Run(mode, func(t *testing.T) {
			f := newFakeOppo(t)
			f.updErrno, f.updMessage = 911001, "版权证书无效"
			f.infoVersion, f.infoAudit, f.infoRefuse = "2", "审核不通过", "APP与示例应用存在套用马甲的嫌疑"
			f.laterInfo = mode

			res := f.store().Upload(context.Background(), uploadRequest(t, nil))
			if res.Success {
				t.Fatal("upload succeeded although /app/upd answered 911001")
			}
			if want := "publish: [911001] 版权证书无效"; res.Error != want {
				t.Errorf("error = %q, want %q", res.Error, want)
			}
			if res.Category != store.CategoryUnknown {
				t.Errorf("category = %q, want it unchanged (unknown)", res.Category)
			}
			calls, _ := f.seen()
			if n := slices.Index(calls, "upd"); n < 0 || len(calls) == n+1 {
				t.Errorf("calls = %v, want an app/info lookup after the failed upd", calls)
			}
		})
	}
}

// Only a rejection is added, and only to 911xxx errors: an app in review
// or live says nothing about why the publish failed, and an 800xxx error
// (a bad field, the token) has its own cause, so app/info isn't asked.
func TestPublishErrorWithoutRejection(t *testing.T) {
	for _, tc := range []struct {
		name               string
		errno              int
		msg, audit, refuse string
		wantInfoAfter      bool
	}{
		{"911001, latest version in review", 911001, "版权证书无效", "审核中", "", true},
		{"911001, latest version live", 911001, "版权证书无效", "上线", "", true},
		{"911001, withdrawn by the developer", 911001, "版权证书无效", "审核不通过", "开发者申请撤销审核", true},
		{"800002 bad field", 800002, "Summary只能包含至多13个字符。", "审核不通过", "应用截图与实际功能不符", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeOppo(t)
			f.updErrno, f.updMessage = tc.errno, tc.msg
			f.infoVersion, f.infoAudit, f.infoRefuse = "2", tc.audit, tc.refuse

			res := f.store().Upload(context.Background(), uploadRequest(t, nil))
			if want := fmt.Sprintf("publish: [%d] %s", tc.errno, tc.msg); res.Error != want {
				t.Errorf("error = %q, want %q", res.Error, want)
			}
			calls, _ := f.seen()
			if got := calls[len(calls)-1] == "info"; got != tc.wantInfoAfter {
				t.Errorf("calls = %v, app/info after upd = %v, want %v", calls, got, tc.wantInfoAfter)
			}
		})
	}
}

// The 2026-10-21 case: the token is voided mid-upload (another use of the
// same key) and OPPO answers 800003. Each signed call fetches a new token
// and goes again, the submit included — OPPO refused it unread — so the
// upload carries on with exactly one accepted submit.
func TestVoidedTokenIsReplaced(t *testing.T) {
	for _, path := range []string{
		"/resource/v1/app/info",
		"/resource/v1/upload/get-upload-url",
		"/resource/v1/app/upd",
		"/resource/v1/app/task-state",
	} {
		t.Run(path, func(t *testing.T) {
			f := newFakeOppo(t)
			f.staleTokenAt = path
			req := &store.UploadRequest{FilePath: fixtureAPK, PackageName: "com.example.app", VersionCode: 2, ReleaseNotes: "x"}
			res := f.store().Upload(context.Background(), req)
			if !res.Success {
				t.Fatalf("upload failed: %s (%s)", res.Error, res.Category)
			}
			calls, upd := f.seen()
			i := slices.Index(calls, "800003:"+path)
			if i < 0 || i+1 >= len(calls) || calls[i+1] != "token" {
				t.Fatalf("calls = %v, want the 800003 followed by a token fetch", calls)
			}
			if n := countOf(calls, "token"); n != 1 {
				t.Errorf("tokens fetched %d times, want 1: %v", n, calls)
			}
			if n := countOf(calls, "upd"); n != 1 {
				t.Errorf("submit accepted %d times, want 1: %v", n, calls)
			}
			want := "fresh-token"
			if path == "/resource/v1/app/task-state" {
				want = "test-token" // the submit went out before the token was voided
			}
			if got := upd.Get("access_token"); got != want {
				t.Errorf("submit signed with %q, want %q", got, want)
			}
			if sum := upd.Get("api_sign"); sum == "" || len(upd["api_sign"]) != 1 {
				t.Errorf("api_sign = %v, want exactly one", upd["api_sign"])
			}
		})
	}
}

// When no new token can be had, OPPO's 800003 is what the user sees.
func TestVoidedTokenNotReplaceable(t *testing.T) {
	f := newFakeOppo(t)
	f.staleTokenAt = "/resource/v1/app/task-state"
	f.tokenErrno = 1001
	req := &store.UploadRequest{FilePath: fixtureAPK, PackageName: "com.example.app", VersionCode: 2, ReleaseNotes: "x"}
	res := f.store().Upload(context.Background(), req)
	if res.Success || !strings.Contains(res.Error, "[800003]") {
		t.Fatalf("got success=%v error %q, want the 800003", res.Success, res.Error)
	}
}

func countOf(calls []string, call string) int {
	n := 0
	for _, c := range calls {
		if c == call {
			n++
		}
	}
	return n
}
