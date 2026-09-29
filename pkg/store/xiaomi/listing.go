package xiaomi

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// listingSpec is what /dev/push accepts for the store listing (商店资料).
// Sources: dev.mi.com 应用自动发布接口操作指南 (aId=1134) for field names,
// 应用更新、修改操作指南 (aId=1248) for the text rules; the image sizes
// and the 5MB cap come from the console's upload hints, not the API doc.
//
//   - appInfo.brief (一句话简介): "保持在34个字符之内，句末勿加标点" — 17
//     汉字 / 34 characters, no trailing punctuation (checkListing).
//   - appInfo.desc (应用介绍): no documented limit.
//   - icon: PNG 512×512.
//   - screenshot_1..screenshot_5: at most 5, the first 3 required for a
//     new app (synchroType=0); 1080×1920 portrait or 1920×1080 landscape,
//     ≤5MB each. The console asks for 4–5, the API doc only requires 3.
var listingSpec = &store.ListingSpec{
	Brief: store.TextSpec{Max: 34, Unit: store.UnitWidth},
	Icon: store.ImageSpec{
		Formats: []string{"png"},
		Sizes:   []store.Size{{Width: 512, Height: 512}},
	},
	Screenshot: store.ImageSpec{
		Formats:  []string{"png", "jpeg"},
		Sizes:    []store.Size{{Width: 1080, Height: 1920}, {Width: 1920, Height: 1080}},
		MaxBytes: 5 << 20,
	},
	MinScreenshots: 3,
	MaxScreenshots: 5,
	Check:          checkListing,
}

// checkListing adds the rules ListingSpec's fields can't express:
//   - brief must not end with punctuation (句末勿加标点);
//   - screenshots share one orientation — the console has a single
//     截图方向 setting per app, so a portrait/landscape mix can't be shown.
func checkListing(l *store.Listing, inspect store.ImageInspector) []error {
	var errs []error
	if b := strings.TrimRightFunc(l.Brief, unicode.IsSpace); b != "" {
		if r, _ := utf8.DecodeLastRuneInString(b); unicode.IsPunct(r) {
			errs = append(errs, fmt.Errorf("%s: ends with %q, xiaomi forbids trailing punctuation (句末勿加标点)", store.ListingBrief, r))
		}
	}

	var portrait, landscape int
	for _, p := range l.Screenshots {
		info, err := inspect(p)
		if err != nil {
			continue // unreadable files are already reported by ValidateListing
		}
		if info.Width > info.Height {
			landscape++
		} else {
			portrait++
		}
	}
	if portrait > 0 && landscape > 0 {
		errs = append(errs, fmt.Errorf("%s: %d portrait and %d landscape, xiaomi needs one orientation (截图方向) for all", store.ListingScreenshots, portrait, landscape))
	}
	return errs
}

// screenshotFiles maps the listing's screenshots onto /dev/push's
// screenshot_1..screenshot_N parts, in display order. nil for no listing.
func screenshotFiles(l *store.Listing) []sigFile {
	if l == nil {
		return nil
	}
	files := make([]sigFile, 0, len(l.Screenshots))
	for i, p := range l.Screenshots {
		files = append(files, sigFile{name: fmt.Sprintf("screenshot_%d", i+1), path: p})
	}
	return files
}
