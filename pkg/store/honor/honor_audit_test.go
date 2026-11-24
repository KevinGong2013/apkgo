package honor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// TestAuditByReleaseUsesGetAuditResult pins the detail query: given a
// releaseId, auditByRelease must call get-audit-result scoped to that exact
// submission — and must surface honor's rejection detail verbatim.
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

// TestAuditUsesCurrentRelease pins the new flow: audit asks
// get-app-current-release for the app's actual current submission, then pins
// get-audit-result to that releaseId — not to the older ExternalID captured at
// upload time — and reports the version actually under review.
func TestAuditUsesCurrentRelease(t *testing.T) {
	var paths []string
	var detailBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/openapi/v1/publish/get-app-current-release":
			io.WriteString(w, `{"code":0,"data":{"releaseId":"rel-new","auditResult":0,"auditMessage":null,"versionName":"4.1.8","versionCode":20261123}}`)
		case "/openapi/v1/publish/get-audit-result":
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &detailBody)
			io.WriteString(w, `{"code":0,"data":[{"releaseId":"rel-new","auditResult":2,"auditMessage":"隐私检测未通过"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	s := &Store{client: resty.New().SetBaseURL(srv.URL).SetHeader("Content-Type", "application/json")}
	res := auditWithStore(context.Background(), s, "900789390", store.AuditQuery{
		Package:     "com.anshibo.app",
		VersionName: "4.1.5",
		VersionCode: 20261113,
		ExternalID:  "rel-old",
	})

	if len(paths) != 2 ||
		paths[0] != "/openapi/v1/publish/get-app-current-release" ||
		paths[1] != "/openapi/v1/publish/get-audit-result" {
		t.Fatalf("paths = %v, want current-release then get-audit-result", paths)
	}
	appIDList, _ := detailBody["appId"].([]any)
	if len(appIDList) != 1 {
		t.Fatalf("detail request appId list = %v, want 1 entry", detailBody["appId"])
	}
	entry, _ := appIDList[0].(map[string]any)
	if entry["releaseId"] != "rel-new" {
		t.Fatalf("detail releaseId = %v, want current release rel-new", entry["releaseId"])
	}
	if res.State != store.AuditRejected || res.Detail != "隐私检测未通过" {
		t.Fatalf("State/Detail = %q/%q, want rejected with the detail message", res.State, res.Detail)
	}
	if res.VersionName != "4.1.8" || res.VersionCode != 20261123 {
		t.Fatalf("Version = %q/%d, want 4.1.8/20261123", res.VersionName, res.VersionCode)
	}
	if res.Error != "" {
		t.Fatalf("Error = %q, want empty", res.Error)
	}
}

// TestAuditFallsBackToExternalID pins the fallback: when current-release
// returns no releaseId, the query must fall back to the upload-time
// ExternalID instead of failing the snapshot.
func TestAuditFallsBackToExternalID(t *testing.T) {
	var detailBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/openapi/v1/publish/get-app-current-release":
			io.WriteString(w, `{"code":0,"data":{"releaseId":"","auditResult":0}}`)
		case "/openapi/v1/publish/get-audit-result":
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &detailBody)
			io.WriteString(w, `{"code":0,"data":[{"releaseId":"rel-old","auditResult":2,"auditMessage":"隐私检测未通过"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	s := &Store{client: resty.New().SetBaseURL(srv.URL).SetHeader("Content-Type", "application/json")}
	res := auditWithStore(context.Background(), s, "900789390", store.AuditQuery{
		ExternalID: "rel-old",
	})

	appIDList, _ := detailBody["appId"].([]any)
	if len(appIDList) != 1 {
		t.Fatalf("detail request appId list = %v, want 1 entry", detailBody["appId"])
	}
	entry, _ := appIDList[0].(map[string]any)
	if entry["releaseId"] != "rel-old" {
		t.Fatalf("detail releaseId = %v, want fallback ExternalID rel-old", entry["releaseId"])
	}
	if res.State != store.AuditRejected || res.Detail != "隐私检测未通过" || res.Error != "" {
		t.Fatalf("State/Detail/Error = %q/%q/%q, want rejected/隐私检测未通过/empty", res.State, res.Detail, res.Error)
	}
}

// TestAuditFallsBackToExternalIDOnCurrentReleaseError pins the other fallback
// trigger: an HTTP failure on current-release must not surface as an error
// when the upload-time ExternalID can still be queried.
func TestAuditFallsBackToExternalIDOnCurrentReleaseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/openapi/v1/publish/get-app-current-release":
			w.WriteHeader(http.StatusBadGateway)
			io.WriteString(w, `{"code":20000,"msg":"gateway"}`)
		case "/openapi/v1/publish/get-audit-result":
			io.WriteString(w, `{"code":0,"data":[{"releaseId":"rel-old","auditResult":0}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	s := &Store{client: resty.New().SetBaseURL(srv.URL).SetHeader("Content-Type", "application/json")}
	res := auditWithStore(context.Background(), s, "900789390", store.AuditQuery{ExternalID: "rel-old"})

	if res.State != store.AuditReviewing || res.Error != "" {
		t.Fatalf("State/Error = %q/%q, want reviewing/empty", res.State, res.Error)
	}
}

// TestAuditFallsBackToLiveVersionWithoutAnyReleaseID pins the last resort:
// current-release failed and there is no ExternalID, so report the live
// version only and make no review-state claim.
func TestAuditFallsBackToLiveVersionWithoutAnyReleaseID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/openapi/v1/publish/get-app-current-release":
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `{"code":20000,"msg":"boom"}`)
		case "/openapi/v1/publish/get-app-detail":
			io.WriteString(w, `{"code":0,"data":{"releaseInfo":{"versionName":"1.0.39","versionCode":39}}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	s := &Store{client: resty.New().SetBaseURL(srv.URL).SetHeader("Content-Type", "application/json")}
	res := auditWithStore(context.Background(), s, "900789390", store.AuditQuery{})

	if res.State != "" || res.Error != "" {
		t.Fatalf("State/Error = %q/%q, want empty/empty", res.State, res.Error)
	}
	if res.LiveVersionName != "1.0.39" || res.LiveVersionCode != 39 {
		t.Fatalf("LiveVersion = %q/%d, want 1.0.39/39", res.LiveVersionName, res.LiveVersionCode)
	}
}

// TestAuditUsesCurrentReleaseVerdictWhenDetailFails pins that a failing
// get-audit-result does not discard a verdict current-release already gave.
func TestAuditUsesCurrentReleaseVerdictWhenDetailFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/openapi/v1/publish/get-app-current-release":
			io.WriteString(w, `{"code":0,"data":{"releaseId":"rel-new","auditResult":2,"auditMessage":"隐私检测未通过","versionName":"4.1.8","versionCode":20261123}}`)
		case "/openapi/v1/publish/get-audit-result":
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `{"code":20000,"msg":"boom"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	s := &Store{client: resty.New().SetBaseURL(srv.URL).SetHeader("Content-Type", "application/json")}
	res := auditWithStore(context.Background(), s, "900789390", store.AuditQuery{ExternalID: "rel-old"})

	if res.State != store.AuditRejected || res.Detail != "隐私检测未通过" || res.Error != "" {
		t.Fatalf("State/Detail/Error = %q/%q/%q, want rejected/隐私检测未通过/empty", res.State, res.Detail, res.Error)
	}
	if res.VersionName != "4.1.8" || res.VersionCode != 20261123 {
		t.Fatalf("Version = %q/%d, want 4.1.8/20261123", res.VersionName, res.VersionCode)
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
