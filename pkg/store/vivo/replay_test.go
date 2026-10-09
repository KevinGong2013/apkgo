package vivo

import (
	"context"
	"strings"
	"testing"

	"github.com/KevinGong2013/apkgo/v4/pkg/httptrace"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// The fixture is a real app.query.details answer (recorded with
// --http-trace, account data replaced) for a submission the developer
// withdrew: status 5.
func TestAuditFromRecording(t *testing.T) {
	rp, err := httptrace.ReplayFile("testdata/audit-withdrawn.trace.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	ctx := httptrace.WithReplay(context.Background(), rp.MatchQuery("method"))
	res, ok := store.QueryAudit(ctx, "vivo", map[string]string{"access_key": "key", "access_secret": "secret"},
		store.AuditQuery{Package: "com.example.app"})
	if !ok || res.Error != "" {
		t.Fatalf("audit: ok=%v error=%q", ok, res.Error)
	}
	if res.State != store.AuditWithdrawn || res.VersionName != "1.9.6" || res.VersionCode != 4235 {
		t.Errorf("result = %+v", res)
	}

	sent := rp.Sent()
	if len(sent) != 1 || len(rp.Remaining()) != 0 {
		t.Fatalf("%d requests sent, %d recorded unused", len(sent), len(rp.Remaining()))
	}
	for _, want := range []string{"method=app.query.details", "packageName=com.example.app", "access_key=key", "sign_method=HMAC-SHA256"} {
		if !strings.Contains(sent[0].URL, want) {
			t.Errorf("request %s lacks %s", sent[0].URL, want)
		}
	}
}
