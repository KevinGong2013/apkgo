package oppo

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/KevinGong2013/apkgo/v4/pkg/progress"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// oppoScreenshotMaxSize is OPPO's per-screenshot limit ("单张图片不能超过 1M").
const oppoScreenshotMaxSize = 1 << 20

// listingSpec is what /app/upd accepts for the listing fields it already
// round-trips from /app/info (OPPO doc 10999; the updm doc 11000 matches):
//
//   - summary (一句话简介): ≤13 characters, no punctuation or spaces (checkListing)
//   - detail_desc (软件介绍): ≥20 characters
//   - icon_url: 512×512 PNG, <1MB
//   - pic_url (竖版截图): 1080×1920 JPG/PNG, ≤1MB each, 2–5 of them
var listingSpec = &store.ListingSpec{
	Brief:       store.TextSpec{Max: 13},
	Description: store.TextSpec{Min: 20},
	Icon: store.ImageSpec{
		Formats:  []string{"png"},
		Sizes:    []store.Size{{Width: oppoIconSize, Height: oppoIconSize}},
		MaxBytes: oppoIconMaxSize,
	},
	Screenshot: store.ImageSpec{
		Formats:  []string{"jpeg", "png"},
		Sizes:    []store.Size{{Width: 1080, Height: 1920}},
		MaxBytes: oppoScreenshotMaxSize,
	},
	MinScreenshots: 2,
	MaxScreenshots: 5,
	Check:          checkListing,
}

// checkListing adds the rule TextSpec can't express: OPPO's summary must not
// contain any punctuation or whitespace. ASCII symbols like ~ + | count too,
// since OPPO's "任何标点符号" is meant loosely.
func checkListing(l *store.Listing) []error {
	for _, r := range l.Brief {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.In(r, unicode.Sm, unicode.Sc, unicode.Sk) {
			return []error{fmt.Errorf("%s must not contain punctuation or spaces (found %q)", store.ListingBrief, r)}
		}
	}
	return nil
}

// applyListing swaps the non-empty fields of l into app, whose listing
// fields /app/upd otherwise echoes back from /app/info unchanged — so the
// new listing is submitted and reviewed together with this version. Images
// are uploaded first (type=photo); screenshots become pic_url's
// comma-separated list in display order. On error app is left untouched.
func (s *Store) applyListing(ctx context.Context, app *appData, l *store.Listing) error {
	var iconURL, picURL string
	if l.Icon != "" {
		u, err := s.uploadPhoto(ctx, l.Icon)
		if err != nil {
			return fmt.Errorf("upload icon: %w", err)
		}
		iconURL = u
	}
	if len(l.Screenshots) > 0 {
		urls := make([]string, len(l.Screenshots))
		for i, p := range l.Screenshots {
			u, err := s.uploadPhoto(ctx, p)
			if err != nil {
				return fmt.Errorf("upload screenshot %d: %w", i+1, err)
			}
			urls[i] = u
		}
		picURL = strings.Join(urls, ",")
	}

	if l.Brief != "" {
		app.Summary = l.Brief
	}
	if l.Description != "" {
		app.DetailDesc = l.Description
	}
	if iconURL != "" {
		app.IconURL = iconURL
	}
	if picURL != "" {
		app.PicURL = picURL
	}
	return nil
}

// uploadPhoto uploads a listing image and returns its OPPO-hosted URL.
// Images are small; they don't disturb the APK's progress bar.
func (s *Store) uploadPhoto(ctx context.Context, path string) (string, error) {
	res, err := s.uploadFile(ctx, path, "photo", progress.Safe(nil))
	if err != nil {
		return "", err
	}
	return res.URL, nil
}
