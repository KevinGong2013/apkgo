package cmd

import (
	"reflect"
	"testing"

	"github.com/KevinGong2013/apkgo/v3/pkg/config"
)

func TestConfiguredStoreNamesAreNormalizedAndSorted(t *testing.T) {
	cfg := &config.Config{Stores: map[string]map[string]string{
		"Vivo.foo": {},
		"huawei":   {},
		"OPPO":     {},
	}}

	got, err := configuredStoreNames(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"huawei", "oppo", "vivo.foo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("configuredStoreNames() = %#v, want %#v", got, want)
	}
}

func TestConfiguredStoreNamesRejectCaseInsensitiveDuplicates(t *testing.T) {
	cfg := &config.Config{Stores: map[string]map[string]string{
		"Huawei": {},
		"huawei": {},
	}}

	if _, err := configuredStoreNames(cfg); err == nil {
		t.Fatal("expected duplicate configured store error")
	}
}
