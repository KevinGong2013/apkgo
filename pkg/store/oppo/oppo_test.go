package oppo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"

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
