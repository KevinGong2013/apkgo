package tencent

import (
	"encoding/json"
	"testing"
)

// nextDataFixture mirrors the shape of 应用宝's __NEXT_DATA__ blob: the props
// tree embeds the target app plus unrelated recommendation records, and a stub
// reference to the same package that carries no version_name.
const nextDataFixture = `{
  "props": {
    "pageProps": {
      "appInfo": {
        "pkg_name": "com.example.app",
        "version_name": "10.12.2",
        "app_id": "52463570"
      },
      "recommends": [
        {"pkg_name": "com.example.other", "version_name": "9.2.3"},
        {"pkg_name": "com.example.app", "version_name": ""}
      ]
    }
  }
}`

func TestFindVersionName(t *testing.T) {
	var data any
	if err := json.Unmarshal([]byte(nextDataFixture), &data); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	if got := findVersionName(data, "com.example.app"); got != "10.12.2" {
		t.Errorf("com.example.app: got %q, want %q (stub with empty version_name must be skipped)", got, "10.12.2")
	}
	if got := findVersionName(data, "com.example.other"); got != "9.2.3" {
		t.Errorf("com.example.other: got %q, want %q", got, "9.2.3")
	}
	if got := findVersionName(data, "com.absent.pkg"); got != "" {
		t.Errorf("absent package: got %q, want empty", got)
	}
}

func TestNextDataRe(t *testing.T) {
	html := `<html><head></head><body>` +
		`<script id="__NEXT_DATA__" type="application/json" crossorigin="anonymous">{"a":1}</script>` +
		`</body></html>`
	m := nextDataRe.FindStringSubmatch(html)
	if m == nil {
		t.Fatal("regex did not match the __NEXT_DATA__ script tag")
	}
	if m[1] != `{"a":1}` {
		t.Errorf("captured %q, want %q", m[1], `{"a":1}`)
	}
}
