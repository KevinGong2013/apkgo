package samsung

import (
	"crypto/rand"
	"crypto/rsa"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
)

// TestGetAccessTokenSendsBearerJWT pins the auth contract: Samsung's
// /auth/accessToken wants the signed JWT in the Authorization header, not the
// body. Sending it in the body earns a 401 AUTH_REQUIRE from the gateway.
func TestGetAccessTokenSendsBearerJWT(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	var gotAuth string
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true,"createdItem":{"accessToken":"tok-123"}}`)
	}))
	defer srv.Close()

	s := &Store{
		client:           resty.New().SetBaseURL(srv.URL).SetHeader("Content-Type", "application/json"),
		serviceAccountID: "svc-acct",
		privateKey:       key,
	}

	tok, err := s.getAccessToken()
	if err != nil {
		t.Fatalf("getAccessToken() error = %v", err)
	}
	if tok != "tok-123" {
		t.Fatalf("token = %q, want tok-123", tok)
	}

	jwt, ok := strings.CutPrefix(gotAuth, "Bearer ")
	if !ok {
		t.Fatalf("Authorization = %q, want a Bearer token", gotAuth)
	}
	if strings.Count(jwt, ".") != 2 {
		t.Fatalf("Bearer value is not a 3-part JWT: %q", jwt)
	}
	// The JWT must not be smuggled in the body — that's the bug this guards.
	if strings.Contains(gotBody, jwt) {
		t.Fatalf("JWT leaked into request body: %q", gotBody)
	}
}
