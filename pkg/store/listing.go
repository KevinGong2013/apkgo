package store

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/KevinGong2013/apkgo/v4/pkg/imgcheck"
)

// Listing is the store-facing presentation (商店资料) submitted together
// with a new version: one-line intro, long description, icon and
// screenshots. It's already resolved for one store — defaults merged with
// that store's overrides, image paths pointing at local files. Empty
// fields mean "leave the store's current value unchanged".
//
// Only the store's default language is updated. The app name is never
// changed here: it must match the APK label, APP 备案 and 软著.
type Listing struct {
	Brief       string   `json:",omitempty"` // one-line intro (一句话介绍)
	Description string   `json:",omitempty"` // long description (长描述)
	Icon        string   `json:",omitempty"` // local icon file
	Screenshots []string `json:",omitempty"` // local portrait screenshots, in display order
}

// Listing field names, as reported in UploadResult.Listing.
const (
	ListingBrief       = "brief"
	ListingDescription = "description"
	ListingIcon        = "icon"
	ListingScreenshots = "screenshots"
)

// Empty reports whether l asks for no change at all. A nil Listing is empty.
func (l *Listing) Empty() bool {
	return l == nil || (l.Brief == "" && l.Description == "" && l.Icon == "" && len(l.Screenshots) == 0)
}

// Fields lists the non-empty fields of l in a fixed order.
func (l *Listing) Fields() []string {
	if l == nil {
		return nil
	}
	var out []string
	if l.Brief != "" {
		out = append(out, ListingBrief)
	}
	if l.Description != "" {
		out = append(out, ListingDescription)
	}
	if l.Icon != "" {
		out = append(out, ListingIcon)
	}
	if len(l.Screenshots) > 0 {
		out = append(out, ListingScreenshots)
	}
	return out
}

// ListingResult is the value for UploadResult.Listing: the fields that
// were submitted with a successful upload. An already-done result (the
// version was already on the store, nothing was submitted) reports none.
// Callers that drive Store.Upload directly (cloud workers) use it the
// same way pkg/uploader does.
func ListingResult(req *UploadRequest, res *UploadResult) []string {
	if req == nil || res == nil || !res.Success || res.Category == CategoryAlreadyDone {
		return nil
	}
	return req.Listing.Fields()
}

// Text length units for TextSpec.Unit.
const (
	// UnitChars counts Unicode characters (the default).
	UnitChars = "chars"
	// UnitBytes counts UTF-8 bytes (samsung).
	UnitBytes = "bytes"
	// UnitWidth counts a CJK/full-width character as 2 and anything else
	// as 1 — the "N 个汉字 / 2N 个字符" rule several Chinese stores use.
	UnitWidth = "width"
)

// TextSpec bounds a text field. Zero Min/Max means unbounded.
type TextSpec struct {
	Min  int    `json:"min,omitempty"`
	Max  int    `json:"max,omitempty"`
	Unit string `json:"unit,omitempty"` // UnitChars (default), UnitBytes or UnitWidth
}

// Size is an exact pixel size.
type Size struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ImageSpec constrains an image. Zero-valued constraints are unchecked.
type ImageSpec struct {
	// Formats lists accepted formats: "png", "jpeg", "webp".
	Formats []string `json:"formats,omitempty"`
	// Sizes lists the accepted exact pixel sizes. Empty = any size within
	// MinEdge/MaxEdge.
	Sizes []Size `json:"sizes,omitempty"`
	// MinEdge / MaxEdge bound both width and height, in pixels.
	MinEdge int `json:"min_edge,omitempty"`
	MaxEdge int `json:"max_edge,omitempty"`
	// Square requires width == height.
	Square   bool  `json:"square,omitempty"`
	MaxBytes int64 `json:"max_bytes,omitempty"`
}

// ListingSpec is what a store accepts for Listing. A store declares it in
// ConfigSchema.Listing; nil means the store can't update its listing and
// apkgo ignores any Listing for it.
type ListingSpec struct {
	Brief          TextSpec  `json:"brief"`
	Description    TextSpec  `json:"description"`
	Icon           ImageSpec `json:"icon"`
	Screenshot     ImageSpec `json:"screenshot"`
	MinScreenshots int       `json:"min_screenshots,omitempty"`
	MaxScreenshots int       `json:"max_screenshots,omitempty"`
	// Check adds store-specific rules the fields above can't express
	// (e.g. oppo's "no punctuation in brief"). Optional.
	Check func(l *Listing) []error `json:"-"`
}

// ErrListingUnsupported is returned (wrapped) by ValidateListing for a
// store that declared no ListingSpec.
var ErrListingUnsupported = errors.New("store does not support listing updates")

// ValidateListing checks l against the named store's ListingSpec. It
// reads image headers from disk, so it also catches missing or
// undecodable files. Every problem is returned, each prefixed with the
// store name and field, so a caller can show them all at once. A nil or
// empty Listing is always valid.
func ValidateListing(storeName string, l *Listing) []error {
	if l.Empty() {
		return nil
	}
	spec := ListingSpecFor(storeName)
	if spec == nil {
		return []error{fmt.Errorf("%s: %w", storeName, ErrListingUnsupported)}
	}

	var errs []error
	add := func(field string, err error) {
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: listing %s: %w", storeName, field, err))
		}
	}
	if l.Brief != "" {
		add(ListingBrief, checkText(l.Brief, spec.Brief))
	}
	if l.Description != "" {
		add(ListingDescription, checkText(l.Description, spec.Description))
	}
	if l.Icon != "" {
		add(ListingIcon, checkImage(l.Icon, spec.Icon))
	}
	if n := len(l.Screenshots); n > 0 {
		if spec.MinScreenshots > 0 && n < spec.MinScreenshots {
			add(ListingScreenshots, fmt.Errorf("got %d, need at least %d", n, spec.MinScreenshots))
		}
		if spec.MaxScreenshots > 0 && n > spec.MaxScreenshots {
			add(ListingScreenshots, fmt.Errorf("got %d, at most %d allowed", n, spec.MaxScreenshots))
		}
		for i, p := range l.Screenshots {
			add(fmt.Sprintf("%s[%d]", ListingScreenshots, i), checkImage(p, spec.Screenshot))
		}
	}
	if spec.Check != nil {
		for _, err := range spec.Check(l) {
			errs = append(errs, fmt.Errorf("%s: listing: %w", storeName, err))
		}
	}
	return errs
}

// TextLength measures s in the given unit (see TextSpec.Unit).
func TextLength(s, unit string) int {
	switch unit {
	case UnitBytes:
		return len(s)
	case UnitWidth:
		n := 0
		for _, r := range s {
			if isWide(r) {
				n += 2
			} else {
				n++
			}
		}
		return n
	default:
		return utf8.RuneCountInString(s)
	}
}

// isWide reports whether r occupies two columns: CJK ideographs, kana,
// hangul, CJK/full-width punctuation and full-width forms.
func isWide(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F, // hangul jamo
		r >= 0x2E80 && r <= 0xA4CF, // CJK radicals … yi
		r >= 0xAC00 && r <= 0xD7A3, // hangul syllables
		r >= 0xF900 && r <= 0xFAFF, // CJK compatibility ideographs
		r >= 0xFE30 && r <= 0xFE4F, // CJK compatibility forms
		r >= 0xFF00 && r <= 0xFF60, // full-width forms
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x20000 && r <= 0x3FFFD: // CJK extensions B+
		return true
	}
	return false
}

func checkText(s string, spec TextSpec) error {
	unit := spec.Unit
	if unit == "" {
		unit = UnitChars
	}
	n := TextLength(s, unit)
	if spec.Min > 0 && n < spec.Min {
		return fmt.Errorf("length %d %s, need at least %d", n, unit, spec.Min)
	}
	if spec.Max > 0 && n > spec.Max {
		return fmt.Errorf("length %d %s, at most %d allowed", n, unit, spec.Max)
	}
	return nil
}

func checkImage(path string, spec ImageSpec) error {
	info, err := imgcheck.Inspect(path)
	if err != nil {
		return err
	}
	var problems []string
	if len(spec.Formats) > 0 && !slices.Contains(spec.Formats, info.Format) {
		problems = append(problems, fmt.Sprintf("format %s, want %s", info.Format, strings.Join(spec.Formats, "/")))
	}
	if len(spec.Sizes) > 0 && !slices.Contains(spec.Sizes, Size{info.Width, info.Height}) {
		want := make([]string, len(spec.Sizes))
		for i, sz := range spec.Sizes {
			want[i] = fmt.Sprintf("%dx%d", sz.Width, sz.Height)
		}
		problems = append(problems, fmt.Sprintf("size %dx%d, want %s", info.Width, info.Height, strings.Join(want, " or ")))
	}
	if spec.Square && info.Width != info.Height {
		problems = append(problems, fmt.Sprintf("size %dx%d, must be square", info.Width, info.Height))
	}
	if spec.MinEdge > 0 && min(info.Width, info.Height) < spec.MinEdge {
		problems = append(problems, fmt.Sprintf("size %dx%d, edges must be at least %dpx", info.Width, info.Height, spec.MinEdge))
	}
	if spec.MaxEdge > 0 && max(info.Width, info.Height) > spec.MaxEdge {
		problems = append(problems, fmt.Sprintf("size %dx%d, edges must be at most %dpx", info.Width, info.Height, spec.MaxEdge))
	}
	if spec.MaxBytes > 0 && info.Bytes > spec.MaxBytes {
		problems = append(problems, fmt.Sprintf("%d bytes, at most %d allowed", info.Bytes, spec.MaxBytes))
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s: %s", path, strings.Join(problems, "; "))
	}
	return nil
}
