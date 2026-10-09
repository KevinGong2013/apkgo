package httptrace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Redaction keeps credentials out of a trace while leaving it useful: a
// secret is replaced by a marker that still tells whether it was there and
// how long it was ("[REDACTED 32]"); an empty value stays empty, because
// "the signature was empty" is exactly what one may be looking for.
//
// What counts as a secret is decided by the name it travels under — a
// JSON key, form field, query parameter or header. Stores name these
// consistently (client_secret, access_token, sign, …); a secret under an
// unremarkable name would pass, which is why a trace is still to be
// treated as sensitive.

// sensitiveWords mark a name as carrying a credential wherever they occur
// in it (compared lowercase, punctuation removed).
var sensitiveWords = []string{
	"secret", "token", "password", "passwd", "authorization", "credential",
	"cookie", "privatekey", "apikey", "assertion", "jwt", "signature",
}

// sensitiveNames are whole names that carry one.
var sensitiveNames = map[string]bool{"sig": true, "sign": true, "pwd": true, "auth": true}

// IsSensitive reports whether a value travelling under name (a JSON key,
// form field, query parameter or header) is redacted.
func IsSensitive(name string) bool {
	n := strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		}
		return -1
	}, name)
	if sensitiveNames[n] {
		return true
	}
	// Names that merely mention a credential: "token_type": "Bearer",
	// the CORS header Access-Control-Allow-Credentials.
	if n == "tokentype" || strings.HasPrefix(n, "accesscontrol") {
		return false
	}
	// "api_sign", "x-sign" — but not "design".
	if strings.HasSuffix(n, "sign") && n != "design" {
		return true
	}
	for _, w := range sensitiveWords {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

func mask(v string) string {
	if v == "" {
		return ""
	}
	return fmt.Sprintf("[REDACTED %d]", len(v))
}

// redactValue is v as it may appear under name: masked when the name is
// sensitive, otherwise with any credentials inside it (a signed URL, an
// embedded JSON document) redacted.
func redactValue(name, v string) string {
	if IsSensitive(name) {
		return mask(v)
	}
	return redactString(v)
}

// redactString redacts what a string value may carry inside: the query of
// a URL, or a JSON document passed as a string.
func redactString(v string) string {
	if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
		if u, err := url.Parse(v); err == nil && (u.RawQuery != "" || u.User != nil) {
			return redactURL(u)
		}
		return v
	}
	if len(v) > 1 && (v[0] == '{' || v[0] == '[') {
		if out, ok := redactJSON([]byte(v)); ok {
			return string(out)
		}
	}
	return v
}

func redactValues(vals url.Values) map[string][]string {
	out := make(map[string][]string, len(vals))
	for k, vs := range vals {
		red := make([]string, len(vs))
		for i, v := range vs {
			red[i] = redactValue(k, v)
		}
		out[k] = red
	}
	return out
}

// redactURL renders u without its userinfo and with sensitive query
// parameters masked.
func redactURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	c := *u
	c.User = nil
	if c.RawQuery != "" {
		vals, err := url.ParseQuery(c.RawQuery)
		if err != nil {
			c.RawQuery = "[REDACTED]"
			return c.String()
		}
		for k, vs := range vals {
			for i, v := range vs {
				vs[i] = redactValue(k, v)
			}
		}
		c.RawQuery = vals.Encode()
	}
	return c.String()
}

func redactHeaders(h http.Header) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for name, vs := range h {
		v := strings.Join(vs, ", ")
		switch {
		case !IsSensitive(name):
			v = redactString(v)
		case strings.EqualFold(name, "Authorization") && strings.Contains(v, " "):
			// Keep the scheme: "Bearer [REDACTED 812]".
			scheme, rest, _ := strings.Cut(v, " ")
			v = scheme + " " + mask(rest)
		default:
			v = mask(v)
		}
		out[name] = v
	}
	return out
}

// redactJSON re-encodes a JSON document with the values under sensitive
// keys masked, at any depth. ok is false when data isn't valid JSON.
func redactJSON(data []byte) (out []byte, ok bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	if dec.More() {
		return nil, false
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(redactTree("", v)); err != nil {
		return nil, false
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), true
}

func redactTree(name string, v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			x[k] = redactTree(k, child)
		}
		return x
	case []any:
		for i, child := range x {
			x[i] = redactTree(name, child)
		}
		return x
	case string:
		return redactValue(name, x)
	case nil:
		return nil
	}
	// Numbers and booleans under a sensitive name (a numeric PIN, say).
	if IsSensitive(name) {
		return "[REDACTED]"
	}
	return v
}

// scrubRe finds name=value / "name":"value" pairs in free text whose name
// looks sensitive.
var scrubRe = regexp.MustCompile(`(?i)([\w.\-]*(?:secret|token|password|passwd|signature|credential|authorization|api_?key|assertion)[\w.\-]*"?\s*[=:]\s*"?)([^"&\s,;<}]+)`)

// scrubText masks credentials in text that has no structure to walk.
func scrubText(s string) string {
	return scrubRe.ReplaceAllString(s, "${1}[REDACTED]")
}
