package samsung

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
)

// TestParsePrivateKey covers the credential-entry failure modes seen in the
// cloud: a single-line web form strips the PEM's newlines, and some config
// files carry literal "\n" escapes. parsePrivateKey must recover the key in
// every case (PKCS#8 and PKCS#1), and still reject genuine garbage.
func TestParsePrivateKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	pkcs8PEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}))
	pkcs1PEM := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))

	flatten := func(s string) string { return strings.ReplaceAll(s, "\n", "") }
	escape := func(s string) string { return strings.ReplaceAll(s, "\n", `\n`) }

	good := map[string]string{
		"pkcs8 canonical":      pkcs8PEM,
		"pkcs8 flattened":      flatten(pkcs8PEM),                // single-line web form (the prod bug)
		"pkcs8 literal escape": escape(pkcs8PEM),                 // "\n" escapes from a config file
		"pkcs8 padded spaces":  "  " + pkcs8PEM + "\n\n",         // stray surrounding whitespace
		"pkcs1 canonical":      pkcs1PEM,
		"pkcs1 flattened":      flatten(pkcs1PEM),
	}
	for name, in := range good {
		t.Run(name, func(t *testing.T) {
			got, err := parsePrivateKey(in)
			if err != nil {
				t.Fatalf("parsePrivateKey() error = %v", err)
			}
			if got.N.Cmp(key.N) != 0 {
				t.Fatalf("recovered a different key than the input")
			}
		})
	}

	bad := map[string]string{
		"empty":      "",
		"no armor":   "this is not a pem at all",
		"empty body": "-----BEGIN PRIVATE KEY----------END PRIVATE KEY-----",
		"non-key":    "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----",
	}
	for name, in := range bad {
		t.Run("reject/"+name, func(t *testing.T) {
			if _, err := parsePrivateKey(in); err == nil {
				t.Fatalf("parsePrivateKey() expected error, got nil")
			}
		})
	}
}
