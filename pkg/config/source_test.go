package config_test

import (
	"strings"
	"testing"

	"github.com/KevinGong2013/apkgo/v3/pkg/config"
)

func TestParseCredsSource(t *testing.T) {
	cases := []struct {
		spec string
		ok   bool
	}{
		{"stdin", true},
		{"fd:0", true},
		{"fd:3", true},
		{"fd:42", true},
		{"", false},
		{"file:foo.yaml", false},
		{"fd:abc", false},
		{"fd:-1", false},
	}
	for _, c := range cases {
		_, err := config.ParseCredsSource(c.spec)
		if c.ok && err != nil {
			t.Errorf("ParseCredsSource(%q) unexpected err: %v", c.spec, err)
		}
		if !c.ok && err == nil {
			t.Errorf("ParseCredsSource(%q) expected error, got nil", c.spec)
		}
	}
}

func TestLoadFromJSON(t *testing.T) {
	body := `{
		"stores": {
			"pgyer": {"api_key": "k1"},
			"huawei": {"service_account": "blob"}
		},
		"hooks": {"before": "echo hi"}
	}`
	cfg, err := config.LoadFromJSON(strings.NewReader(body))
	if err != nil {
		t.Fatalf("LoadFromJSON: %v", err)
	}
	if got := cfg.Stores["pgyer"]["api_key"]; got != "k1" {
		t.Errorf("pgyer api_key = %q, want k1", got)
	}
	if got := cfg.Stores["huawei"]["service_account"]; got != "blob" {
		t.Errorf("huawei service_account = %q, want blob", got)
	}
	if got := cfg.Hooks.Before; got != "echo hi" {
		t.Errorf("hooks.before = %q, want %q", got, "echo hi")
	}
}

func TestLoadFromJSON_EmptyStores(t *testing.T) {
	body := `{"stores": {}}`
	if _, err := config.LoadFromJSON(strings.NewReader(body)); err == nil {
		t.Error("expected error for empty stores, got nil")
	}
}

func TestLoadFromJSON_BadJSON(t *testing.T) {
	if _, err := config.LoadFromJSON(strings.NewReader("not json")); err == nil {
		t.Error("expected error for bad json, got nil")
	}
}
