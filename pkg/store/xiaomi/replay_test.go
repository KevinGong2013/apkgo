package xiaomi

import (
	"context"
	"strings"
	"testing"

	"github.com/KevinGong2013/apkgo/v4/pkg/httptrace"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// The fixture is a real dev/query answer (recorded with --http-trace,
// account data replaced) for an app whose live version is the latest one.
func TestAuditFromRecording(t *testing.T) {
	rp, err := httptrace.ReplayFile("testdata/audit-live.trace.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	cfg := map[string]string{"email": "dev@example.com", "private_key": "password"}
	q := store.AuditQuery{Package: "com.example.app"}
	res, ok := store.QueryAudit(httptrace.WithReplay(context.Background(), rp), "xiaomi", cfg, q)
	if !ok || res.Error != "" {
		t.Fatalf("audit: ok=%v error=%q", ok, res.Error)
	}
	if res.State != store.AuditApproved || res.VersionName != "1.9.5" || res.VersionCode != 4234 || res.LiveVersionCode != 4234 {
		t.Errorf("result = %+v", res)
	}

	sent := rp.Sent()
	if len(sent) != 1 || len(rp.Remaining()) != 0 {
		t.Fatalf("%d requests sent, %d recorded unused", len(sent), len(rp.Remaining()))
	}
	form := sent[0].Request.Form
	if !strings.Contains(strings.Join(form["RequestData"], ""), `"packageName":"com.example.app"`) || len(form["SIG"]) != 1 {
		t.Errorf("request form = %v", form)
	}

	// The same answer for a newer submission means it is still in review.
	rp, _ = httptrace.ReplayFile("testdata/audit-live.trace.jsonl")
	q.VersionName, q.VersionCode = "1.9.6", 4235
	if res, _ = store.QueryAudit(httptrace.WithReplay(context.Background(), rp), "xiaomi", cfg, q); res.State != store.AuditReviewing || res.VersionCode != 4235 || res.LiveVersionCode != 4234 {
		t.Errorf("newer submission: %+v", res)
	}
}
