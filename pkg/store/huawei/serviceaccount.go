package huawei

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"time"
)

// serviceAccount mirrors the JSON file Huawei provides when you create a
// developer-level Service Account in AGC. Only the fields apkgo uses are
// declared; extra fields (auth_uri, *_cert_uri, project_id) are ignored.
type serviceAccount struct {
	KeyID      string `json:"key_id"`
	PrivateKey string `json:"private_key"`
	SubAccount string `json:"sub_account"`
	TokenURI   string `json:"token_uri"`
}

// loadServiceAccount accepts either:
//   - inline base64-encoded JSON (handy for env vars / CI secrets), or
//   - the raw JSON itself (string starts with `{`).
//
// It returns the parsed credential plus the decoded RSA private key.
func loadServiceAccount(raw string) (*serviceAccount, *rsa.PrivateKey, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil, fmt.Errorf("empty service_account")
	}
	var jsonBytes []byte
	if strings.HasPrefix(raw, "{") {
		jsonBytes = []byte(raw)
	} else {
		// Tolerate both std and url-safe base64, with or without padding.
		var err error
		for _, dec := range []*base64.Encoding{
			base64.StdEncoding, base64.RawStdEncoding,
			base64.URLEncoding, base64.RawURLEncoding,
		} {
			jsonBytes, err = dec.DecodeString(raw)
			if err == nil {
				break
			}
		}
		if err != nil {
			return nil, nil, fmt.Errorf("decode base64: %w", err)
		}
	}
	var sa serviceAccount
	if err := json.Unmarshal(jsonBytes, &sa); err != nil {
		return nil, nil, fmt.Errorf("parse service_account JSON: %w", err)
	}
	if sa.KeyID == "" || sa.PrivateKey == "" || sa.SubAccount == "" {
		return nil, nil, fmt.Errorf("service_account missing required field(s) key_id/private_key/sub_account")
	}
	if sa.TokenURI == "" {
		// Default per Huawei docs; aud is the audience claim, not a real call.
		sa.TokenURI = "https://oauth-login.cloud.huawei.com/oauth2/v3/token"
	}

	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil {
		return nil, nil, fmt.Errorf("private_key: invalid PEM block")
	}
	keyAny, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("private_key: parse PKCS8: %w", err)
	}
	rsaKey, ok := keyAny.(*rsa.PrivateKey)
	if !ok {
		return nil, nil, fmt.Errorf("private_key: expected RSA, got %T", keyAny)
	}
	return &sa, rsaKey, nil
}

// loadServiceAccountFromFile reads a Service Account credential file from disk.
func loadServiceAccountFromFile(path string) (*serviceAccount, *rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	return loadServiceAccount(string(data))
}

// signJWT produces a PS256-signed JWT per the AGC Service Account spec.
// Per Huawei docs the JWT itself is the bearer token — no exchange step is
// involved. The token is valid for one hour.
func signJWT(sa *serviceAccount, key *rsa.PrivateKey, now time.Time) (string, error) {
	header := map[string]any{"alg": "PS256", "typ": "JWT", "kid": sa.KeyID}
	payload := map[string]any{
		"aud": sa.TokenURI,
		"iss": sa.SubAccount,
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	signingInput := enc.EncodeToString(headerJSON) + "." + enc.EncodeToString(payloadJSON)
	digest := sha256.Sum256([]byte(signingInput))
	// JWT mandates salt length == hash length for PS256.
	sig, err := rsa.SignPSS(rand.Reader, key, crypto.SHA256, digest[:], &rsa.PSSOptions{
		SaltLength: rsa.PSSSaltLengthEqualsHash,
	})
	if err != nil {
		return "", fmt.Errorf("rsa sign: %w", err)
	}
	return signingInput + "." + enc.EncodeToString(sig), nil
}
