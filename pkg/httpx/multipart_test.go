package httpx

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestRedactURLError(t *testing.T) {
	ue := &url.Error{
		Op:  "Put",
		URL: "https://bucket.cos.ap-guangzhou.myqcloud.com/apk/123.apk?x-cos-security-token=SECRET&q-signature=ALSOSECRET",
		Err: errors.New("context deadline exceeded"),
	}
	wrapped := RedactURLError(ue)
	msg := wrapped.Error()
	if strings.Contains(msg, "SECRET") {
		t.Errorf("query not redacted: %s", msg)
	}
	if !strings.Contains(msg, "https://bucket.cos.ap-guangzhou.myqcloud.com/apk/123.apk") {
		t.Errorf("base URL lost: %s", msg)
	}
	if !strings.Contains(msg, "context deadline exceeded") {
		t.Errorf("underlying error lost: %s", msg)
	}

	// Non-url.Error and no-query URLs pass through untouched.
	plain := errors.New("plain")
	if got := RedactURLError(plain); got != plain {
		t.Error("plain error should be returned as-is")
	}
	noQuery := &url.Error{Op: "Post", URL: "https://api.example.com/upload", Err: errors.New("eof")}
	if got := RedactURLError(noQuery).(*url.Error).URL; got != "https://api.example.com/upload" {
		t.Errorf("no-query URL mutated: %s", got)
	}
}
