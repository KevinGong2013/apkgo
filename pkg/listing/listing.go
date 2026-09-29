// Package listing reads the listing file (商店资料, `--listing listing.yaml`)
// and resolves it into a per-store store.Listing.
//
// A listing file holds defaults plus per-store overrides, because stores
// disagree on limits (oppo's one-line intro is ≤13 characters, huawei's
// ≤80; huawei wants a 216×216 icon, most others 512×512):
//
//	brief: 一句话介绍
//	description_file: desc.md
//	icon: assets/icon-512.png
//	screenshots: [assets/s1.png, assets/s2.png, assets/s3.png, assets/s4.png]
//	stores:
//	  oppo:   { brief: 十三字以内的简介 }
//	  huawei: { icon: assets/icon-216.png }
//
// File is JSON-serialisable and Resolve never touches the filesystem, so
// services can store a File per app and resolve it themselves; only Load
// (the CLI path) reads files and resolves relative paths.
package listing

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// Fields is one layer of listing values. Empty fields inherit from the
// layer below (defaults, then store type, then store instance).
type Fields struct {
	Brief       string `yaml:"brief,omitempty" json:"brief,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// DescriptionFile is read into Description by Load (relative to the
	// listing file). Setting both is an error.
	DescriptionFile string   `yaml:"description_file,omitempty" json:"-"`
	Icon            string   `yaml:"icon,omitempty" json:"icon,omitempty"`
	Screenshots     []string `yaml:"screenshots,omitempty" json:"screenshots,omitempty"`
}

// File is a parsed listing file.
type File struct {
	Fields `yaml:",inline"`
	// Stores holds per-store overrides keyed by store name ("oppo") or
	// instance name ("script.cdn"). An instance inherits its type's
	// override.
	Stores map[string]Fields `yaml:"stores,omitempty" json:"stores,omitempty"`
}

// Load reads a listing file. Unknown keys are rejected, relative image
// and description_file paths are made absolute against the file's directory,
// and description_file contents are read in.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("listing: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	f := &File{}
	// An empty (or comment-only) file is an empty listing, not an error.
	if err := dec.Decode(f); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("listing %s: %w", path, err)
	}

	dir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("listing: %w", err)
	}
	if err := f.Fields.load(dir, "listing"); err != nil {
		return nil, err
	}
	for name, fields := range f.Stores {
		if err := fields.load(dir, "listing stores."+name); err != nil {
			return nil, err
		}
		f.Stores[name] = fields
	}
	return f, nil
}

// load resolves paths relative to dir and reads DescriptionFile.
func (fs *Fields) load(dir, where string) error {
	abs := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(dir, p)
	}
	fs.Icon = abs(fs.Icon)
	for i, p := range fs.Screenshots {
		fs.Screenshots[i] = abs(p)
	}
	if fs.DescriptionFile != "" {
		if fs.Description != "" {
			return fmt.Errorf("%s: set description or description_file, not both", where)
		}
		data, err := os.ReadFile(abs(fs.DescriptionFile))
		if err != nil {
			return fmt.Errorf("%s: description_file: %w", where, err)
		}
		fs.Description = string(data)
		fs.DescriptionFile = ""
	}
	fs.Brief = strings.TrimSpace(fs.Brief)
	fs.Description = strings.TrimSpace(fs.Description)
	return nil
}

// UnknownStores returns override keys that can't apply, sorted: names
// that aren't a registered store type, and "type.instance" names that
// match none of the configured stores (the usual typo). A plain type name
// that isn't configured is fine, so one listing file can serve jobs with
// different store subsets. Stores must be registered (blank-imported)
// before calling it.
func (f *File) UnknownStores(configured []string) []string {
	var out []string
	for name := range f.Stores {
		if !store.Known(name) || (strings.Contains(name, ".") && !slices.Contains(configured, name)) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// Resolve returns the listing for storeName: defaults, overlaid with the
// store type's override, overlaid with the instance's override for
// "type.instance" names. Returns nil when nothing is set.
func (f *File) Resolve(storeName string) *store.Listing {
	if f == nil {
		return nil
	}
	merged := f.Fields
	if dot := strings.Index(storeName, "."); dot > 0 {
		merged = overlay(merged, f.Stores[storeName[:dot]])
	}
	merged = overlay(merged, f.Stores[storeName])

	l := &store.Listing{
		Brief:       merged.Brief,
		Description: merged.Description,
		Icon:        merged.Icon,
		Screenshots: merged.Screenshots,
	}
	if l.Empty() {
		return nil
	}
	return l
}

func overlay(base, top Fields) Fields {
	if top.Brief != "" {
		base.Brief = top.Brief
	}
	if top.Description != "" {
		base.Description = top.Description
	}
	if top.Icon != "" {
		base.Icon = top.Icon
	}
	if len(top.Screenshots) > 0 {
		base.Screenshots = top.Screenshots
	}
	return base
}
