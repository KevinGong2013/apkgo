package listing_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/KevinGong2013/apkgo/v4/pkg/listing"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

func init() {
	for _, name := range []string{"test-a", "test-b"} {
		store.Register(name, store.ConfigSchema{Name: name}, func(map[string]string) (store.Store, error) { return nil, nil })
	}
}

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadAndResolve(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "desc.md", "\n  长描述内容  \n")
	write(t, dir, "a-desc.md", "A 的描述")
	path := write(t, dir, "listing.yaml", `
brief: 默认介绍
description_file: desc.md
icon: assets/icon.png
screenshots: [assets/s1.png, /abs/s2.png]
stores:
  test-a:
    brief: A 的介绍
    description_file: a-desc.md
  test-a.cn:
    icon: cn.png
  test-b:
    screenshots: [b1.png]
`)
	f, err := listing.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	got := f.Resolve("test-b")
	want := &store.Listing{
		Brief:       "默认介绍",
		Description: "长描述内容",
		Icon:        filepath.Join(dir, "assets/icon.png"),
		Screenshots: []string{filepath.Join(dir, "b1.png")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("test-b = %+v, want %+v", got, want)
	}

	// Instance inherits the type's override, then applies its own.
	got = f.Resolve("test-a.cn")
	want = &store.Listing{
		Brief:       "A 的介绍",
		Description: "A 的描述",
		Icon:        filepath.Join(dir, "cn.png"),
		Screenshots: []string{filepath.Join(dir, "assets/s1.png"), "/abs/s2.png"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("test-a.cn = %+v, want %+v", got, want)
	}

	if unknown := f.UnknownStores(); unknown != nil {
		t.Errorf("UnknownStores = %v", unknown)
	}
}

func TestLoadErrors(t *testing.T) {
	cases := map[string]string{
		"unknown key":       "brief: x\nicons: a.png\n",
		"both descriptions": "description: x\ndescription_file: d.md\n",
		"missing desc file": "description_file: nope.md\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, "d.md", "d")
			if _, err := listing.Load(write(t, dir, "listing.yaml", content)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestUnknownStores(t *testing.T) {
	dir := t.TempDir()
	f, err := listing.Load(write(t, dir, "listing.yaml", "stores:\n  test-a: {brief: x}\n  tset-b: {brief: y}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.UnknownStores(), ","); got != "tset-b" {
		t.Errorf("UnknownStores = %q", got)
	}
}

func TestResolveNilAndEmpty(t *testing.T) {
	var f *listing.File
	if f.Resolve("test-a") != nil {
		t.Error("nil file should resolve to nil")
	}
	if (&listing.File{}).Resolve("test-a") != nil {
		t.Error("empty file should resolve to nil")
	}
}

// File round-trips through JSON (services persist it per app).
func TestFileJSON(t *testing.T) {
	in := &listing.File{
		Fields: listing.Fields{Brief: "b", Screenshots: []string{"s.png"}},
		Stores: map[string]listing.Fields{"test-a": {Icon: "i.png"}},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"brief":"b","screenshots":["s.png"],"stores":{"test-a":{"icon":"i.png"}}}`; string(data) != want {
		t.Errorf("json = %s, want %s", data, want)
	}
	var out listing.File
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(&out, in) {
		t.Errorf("round-trip = %+v", out)
	}
}
