package honor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v3/pkg/store"
)

// TestAuditByReleaseUsesGetAuditResult pins the fix: with a releaseId
// (ExternalID) available, the audit path must call get-audit-result scoped
// to that exact submission, not the ambiguous appId-only
// get-app-current-release — and must surface honor's rejection detail
// verbatim.
func TestAuditByReleaseUsesGetAuditResult(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"code":0,"data":[{"releaseId":"rel-1","auditResult":2,"auditMessage":"存在开发者同版本或高版本任务"}]}`)
	}))
	defer srv.Close()

	s := &Store{client: resty.New().SetBaseURL(srv.URL).SetHeader("Content-Type", "application/json")}
	var res store.AuditResult
	auditByRelease(context.Background(), s, "123", "rel-1", &res)

	if gotPath != "/openapi/v1/publish/get-audit-result" {
		t.Fatalf("path = %q, want get-audit-result", gotPath)
	}
	appIDList, _ := gotBody["appId"].([]any)
	if len(appIDList) != 1 {
		t.Fatalf("request body appId list = %v, want 1 entry", gotBody["appId"])
	}
	entry, _ := appIDList[0].(map[string]any)
	if entry["releaseId"] != "rel-1" {
		t.Fatalf("request releaseId = %v, want rel-1", entry["releaseId"])
	}
	if entry["appId"].(float64) != 123 {
		t.Fatalf("request appId = %v, want 123", entry["appId"])
	}
	if res.State != store.AuditRejected {
		t.Fatalf("State = %q, want rejected", res.State)
	}
	if res.Detail != "存在开发者同版本或高版本任务" {
		t.Fatalf("Detail = %q, want the honor rejection message", res.Detail)
	}
}

// TestAuditLiveVersionOnlyDoesNotClaimReviewState pins the fallback: with
// no releaseId available, the query must not guess a review state from the
// ambiguous get-app-current-release — only report the already-live version
// via get-app-detail.
func TestAuditLiveVersionOnlyDoesNotClaimReviewState(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"code":0,"data":{"releaseInfo":{"versionName":"1.0.39","versionCode":39}}}`)
	}))
	defer srv.Close()

	s := &Store{client: resty.New().SetBaseURL(srv.URL).SetHeader("Content-Type", "application/json")}
	var res store.AuditResult
	auditLiveVersionOnly(context.Background(), s, "123", &res)

	if gotPath != "/openapi/v1/publish/get-app-detail" {
		t.Fatalf("path = %q, want get-app-detail", gotPath)
	}
	if res.State != "" {
		t.Fatalf("State = %q, want empty (no review-state claim without a releaseId)", res.State)
	}
	if res.LiveVersionName != "1.0.39" || res.LiveVersionCode != 39 {
		t.Fatalf("LiveVersion = %q/%d, want 1.0.39/39", res.LiveVersionName, res.LiveVersionCode)
	}
}

// TestSubmitAuditReturnsReleaseID pins that submit-audit's bare-string
// `data` field is captured as the releaseId, not discarded.
func TestSubmitAuditReturnsReleaseID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"code":0,"msg":"","data":"rel-42"}`)
	}))
	defer srv.Close()

	s := &Store{client: resty.New().SetBaseURL(srv.URL).SetHeader("Content-Type", "application/json")}
	releaseID, err := s.submitAudit("123", nil)
	if err != nil {
		t.Fatalf("submitAudit() error = %v", err)
	}
	if releaseID != "rel-42" {
		t.Fatalf("releaseID = %q, want rel-42", releaseID)
	}
}
