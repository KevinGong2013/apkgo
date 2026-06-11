package store_test

import (
	"testing"
	"time"

	"github.com/KevinGong2013/apkgo/v3/pkg/store"
)

// TestBeijingLocalTime locks in the timezone conversion used by the
// scheduled-release fields of oppo, vivo and samsung, whose datetime
// strings carry no timezone of their own and must be rendered in
// Beijing time (UTC+8) regardless of the offset the caller supplied.
func TestBeijingLocalTime(t *testing.T) {
	cases := []struct {
		name string
		in   string // RFC3339 input
		want string
	}{
		{"already Beijing offset", "2026-06-20T10:00:00+08:00", "2026-06-20 10:00:00"},
		{"from UTC", "2026-06-20T02:00:00Z", "2026-06-20 10:00:00"},
		{"from US Eastern", "2026-06-20T00:00:00-05:00", "2026-06-20 13:00:00"},
		{"crosses date boundary", "2026-06-20T23:30:00Z", "2026-06-21 07:30:00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			parsed, err := time.Parse(time.RFC3339, c.in)
			if err != nil {
				t.Fatalf("parse %q: %v", c.in, err)
			}
			if got := store.BeijingLocalTime(parsed); got != c.want {
				t.Errorf("BeijingLocalTime(%s) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestSupportsScheduledRelease verifies the capability lookup, including
// the "type.instance" name resolution (e.g. "script.cdn") and the
// unknown-name-is-false fallback that apkgo.Run relies on when deciding
// which targeted stores to warn about.
func TestSupportsScheduledRelease(t *testing.T) {
	store.Register("test-sched-yes", store.ConfigSchema{
		Name:                     "test-sched-yes",
		SupportsScheduledRelease: true,
	}, func(map[string]string) (store.Store, error) { return nil, nil })
	store.Register("test-sched-no", store.ConfigSchema{
		Name: "test-sched-no",
	}, func(map[string]string) (store.Store, error) { return nil, nil })

	cases := []struct {
		name string
		want bool
	}{
		{"test-sched-yes", true},
		{"test-sched-no", false},
		{"test-sched-yes.instance", true}, // type.instance resolves to base type
		{"test-sched-no.instance", false},
		{"unregistered", false},
	}
	for _, c := range cases {
		if got := store.SupportsScheduledRelease(c.name); got != c.want {
			t.Errorf("SupportsScheduledRelease(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestSupportsURLPush mirrors TestSupportsScheduledRelease for the
// download-mode (pull-from-URL) capability flag that apkgo.Run uses to
// decide whether a store can take a developer-hosted URL instead of an
// uploaded binary.
func TestSupportsURLPush(t *testing.T) {
	store.Register("test-urlpush-yes", store.ConfigSchema{
		Name:            "test-urlpush-yes",
		SupportsURLPush: true,
	}, func(map[string]string) (store.Store, error) { return nil, nil })
	store.Register("test-urlpush-no", store.ConfigSchema{
		Name: "test-urlpush-no",
	}, func(map[string]string) (store.Store, error) { return nil, nil })

	cases := []struct {
		name string
		want bool
	}{
		{"test-urlpush-yes", true},
		{"test-urlpush-no", false},
		{"test-urlpush-yes.instance", true}, // type.instance resolves to base type
		{"unregistered", false},
	}
	for _, c := range cases {
		if got := store.SupportsURLPush(c.name); got != c.want {
			t.Errorf("SupportsURLPush(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}
