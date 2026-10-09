package oppo

import (
	"context"
	"strings"
	"testing"

	"github.com/KevinGong2013/apkgo/v4/pkg/httptrace"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// The fixture is a real token + app/info session (recorded with
// --http-trace, account data replaced) for a submission the developer
// withdrew in the console. OPPO reports that as audit_status_name
// "审核不通过" with refuse_reason "开发者申请撤销审核"; it is a withdrawal, not
// a rejection.
func TestAuditFromRecording(t *testing.T) {
	rp, err := httptrace.ReplayFile("testdata/audit-withdrawn.trace.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	ctx := httptrace.WithReplay(context.Background(), rp)
	res, ok := store.QueryAudit(ctx, "oppo", map[string]string{"client_id": "id", "client_secret": "secret"},
		store.AuditQuery{Package: "com.example.app"})
	if !ok || res.Error != "" {
		t.Fatalf("audit: ok=%v error=%q", ok, res.Error)
	}
	if res.State != store.AuditWithdrawn || res.Detail != "审核不通过: 开发者申请撤销审核" || res.VersionName != "1.9.6" || res.VersionCode != 4235 {
		t.Errorf("result = %+v", res)
	}

	// Sign in (in the constructor), then one signed app/info for the package.
	sent := rp.Sent()
	if len(sent) != 2 || len(rp.Remaining()) != 0 {
		t.Fatalf("%d requests sent, %d recorded unused", len(sent), len(rp.Remaining()))
	}
	if !strings.Contains(sent[0].URL, "/developer/v1/token?client_id=id") {
		t.Errorf("first request = %s", sent[0].URL)
	}
	for _, want := range []string{"/resource/v1/app/info?", "pkg_name=com.example.app", "access_token=", "api_sign="} {
		if !strings.Contains(sent[1].URL, want) {
			t.Errorf("second request %s lacks %s", sent[1].URL, want)
		}
	}
}

func TestMapOppoAuditWithdrawnByDeveloper(t *testing.T) {
	for _, c := range []struct {
		name, refuse string
		want         store.AuditState
	}{
		{"审核不通过", "开发者申请撤销审核", store.AuditWithdrawn},
		{"审核不通过", "应用截图与实际功能不符", store.AuditRejected},
		{"审核不通过", "", store.AuditRejected},
		{"审核中", "", store.AuditReviewing},
	} {
		if got, _ := mapOppoAudit(c.name, c.refuse); got != c.want {
			t.Errorf("mapOppoAudit(%q, %q) = %s, want %s", c.name, c.refuse, got, c.want)
		}
	}
}
