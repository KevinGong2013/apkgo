package huawei

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/KevinGong2013/apkgo/v4/pkg/httptrace"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// serviceAccountJSON is a throwaway Service Account credential: the
// replayed store never checks the JWT signed with it.
func serviceAccountJSON(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	sa, _ := json.Marshal(map[string]string{
		"key_id":      "kid",
		"sub_account": "100000001",
		"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
	})
	return string(sa)
}

// The fixture is a real appid-list + app-info session (recorded with
// --http-trace, account data replaced) for a submission the developer
// withdrew: releaseState 11, with an older version still on the shelf.
// app-info is also what a download-mode publish polls to confirm that the
// new versionCode went into review.
func TestAuditFromRecording(t *testing.T) {
	rp, err := httptrace.ReplayFile("testdata/audit-withdrawn.trace.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	ctx := httptrace.WithReplay(context.Background(), rp)
	res, ok := store.QueryAudit(ctx, "huawei", map[string]string{"service_account": serviceAccountJSON(t)},
		store.AuditQuery{Package: "com.example.app"})
	if !ok || res.Error != "" {
		t.Fatalf("audit: ok=%v error=%q", ok, res.Error)
	}
	if res.State != store.AuditWithdrawn || res.Detail != "releaseState=11" ||
		res.VersionName != "1.9.6" || res.VersionCode != 4235 ||
		res.LiveVersionName != "1.9.5" || res.LiveVersionCode != 4234 {
		t.Errorf("result = %+v", res)
	}

	// Look the app up by package, then read its info by the id returned.
	sent := rp.Sent()
	if len(sent) != 2 || len(rp.Remaining()) != 0 {
		t.Fatalf("%d requests sent, %d recorded unused", len(sent), len(rp.Remaining()))
	}
	if !strings.Contains(sent[0].URL, "/appid-list?") || !strings.Contains(sent[0].URL, "packageName=com.example.app") {
		t.Errorf("first request = %s", sent[0].URL)
	}
	if !strings.Contains(sent[1].URL, "/app-info?appId=100000000") {
		t.Errorf("second request = %s", sent[1].URL)
	}
	if auth := sent[1].Request.Headers["Authorization"]; !strings.HasPrefix(auth, "Bearer [REDACTED") {
		t.Errorf("app-info sent without the bearer token: %q", auth)
	}
}
