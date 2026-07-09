package meizu

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

// TestSignedHeaders verifies the header signature against the documented
// algorithm (open.flyme.cn/docs?id=333): sorted "k=v" pairs of traceId,
// clientId, timestamp and uri joined with "&", then ":"+clientSecret,
// hashed with SHA-256. Alphabetically the order is clientId, timestamp,
// traceId, uri — a regression here would produce 113033 签名错误 on
// every call.
func TestSignedHeaders(t *testing.T) {
	s := &Store{clientID: "cid-123", clientSecret: "secret-xyz", accessToken: "tok"}
	h := s.signedHeaders("/open/api/v1/app/publish")

	for _, k := range []string{"traceId", "clientId", "timestamp", "sign", "accessToken"} {
		if h[k] == "" {
			t.Fatalf("header %s is empty", k)
		}
	}
	if h["clientId"] != "cid-123" || h["accessToken"] != "tok" {
		t.Fatalf("credential headers wrong: %v", h)
	}

	want := sha256.Sum256([]byte(
		"clientId=cid-123" +
			"&timestamp=" + h["timestamp"] +
			"&traceId=" + h["traceId"] +
			"&uri=/open/api/v1/app/publish" +
			":secret-xyz"))
	if h["sign"] != hex.EncodeToString(want[:]) {
		t.Errorf("sign = %s, want %s", h["sign"], hex.EncodeToString(want[:]))
	}
}

// TestEnvelopeCode covers the tolerant code parsing: the docs specify an
// integer, but Meizu's sibling APIs return the code as a quoted string,
// and both must map onto the same success/error check.
func TestEnvelopeCode(t *testing.T) {
	cases := []struct {
		name string
		body string
		code int
		msg  string
	}{
		{"integer code", `{"code":200,"msg":""}`, 200, ""},
		{"string code", `{"code":"113033","message":"签名错误"}`, 113033, "签名错误"},
		{"null code", `{"code":null,"msg":"x"}`, 0, "x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var e envelope
			if err := json.Unmarshal([]byte(c.body), &e); err != nil {
				t.Fatal(err)
			}
			if int(e.Code) != c.code {
				t.Errorf("code = %d, want %d", e.Code, c.code)
			}
			if got := e.errOf().Error(); c.msg != "" && !strings.Contains(got, c.msg) {
				t.Errorf("errOf() = %q, want it to contain %q", got, c.msg)
			}
		})
	}
}

// TestPublishBody checks the detail→publish round-trip transforms: release
// notes override, comma-separated certificates become a list, and the
// fixed top-level category defaults when detail omits it.
func TestPublishBody(t *testing.T) {
	d := &appDetail{
		Name:           "MyApp",
		VerDescription: "old notes",
		Certificates:   "a.png, b.png,,",
	}
	body := d.publishBody("pkg-file-name", "new notes")
	if body["verDesc"] != "new notes" {
		t.Errorf("verDesc = %v, want new notes", body["verDesc"])
	}
	if body["packageUrl"] != "pkg-file-name" {
		t.Errorf("packageUrl = %v", body["packageUrl"])
	}
	if body["catid"] != int64(1) {
		t.Errorf("catid = %v, want 1", body["catid"])
	}
	certs, ok := body["certificates"].([]string)
	if !ok || len(certs) != 2 || certs[0] != "a.png" || certs[1] != "b.png" {
		t.Errorf("certificates = %v, want [a.png b.png]", body["certificates"])
	}

	// Empty release notes keep the currently-listed version description.
	if got := d.publishBody("p", "")["verDesc"]; got != "old notes" {
		t.Errorf("verDesc fallback = %v, want old notes", got)
	}
}
