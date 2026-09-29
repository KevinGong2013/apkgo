package harmony

import (
	"context"
	"fmt"

	"github.com/KevinGong2013/apkgo/v4/pkg/progress"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// AGC FileInfo slot the listing images are written to: phone (deviceType
// 4), portrait (showType 0). Other devices (tablet, PC/2in1, watch, …)
// keep their current assets.
const (
	deviceTypePhone  = 4
	showTypePortrait = 0
)

// WEBP byte caps; AGC allows far more for PNG/JPEG (see ImageSpec.MaxBytes).
const (
	iconWebPMaxBytes       = 100 << 10
	screenshotWebPMaxBytes = 200 << 10
)

// listingSpec is what AGC accepts for a HarmonyOS app's listing (商店资料).
// Sources: Publishing API v3 app-language-info (更新语言描述信息),
// app-file-info (更新应用文件信息) and the 应用文件要求 appendix.
//
//   - briefInfo (一句话简介) ≤80, appDesc (应用描述) ≤8000; appDesc,
//     briefInfo and newFeatures must all differ (checkListing, and the
//     release-notes check in publish).
//   - icon: one per device; phone PNG ≤3MB or WEBP ≤100KB, 216×216 or
//     1024×1024. AGC requires it to match the icon inside the .app.
//   - phone screenshots: 3–10, portrait, at least 1080×1920 at 9:16 (the
//     console's 最低尺寸), PNG/JPEG ≤5MB or WEBP ≤200KB.
var listingSpec = &store.ListingSpec{
	Brief:       store.TextSpec{Max: 80},
	Description: store.TextSpec{Max: 8000},
	Icon: store.ImageSpec{
		Formats:          []string{"png", "webp"},
		Sizes:            []store.Size{{Width: 216, Height: 216}, {Width: 1024, Height: 1024}},
		MaxBytes:         3 << 20,
		MaxBytesByFormat: map[string]int64{"webp": iconWebPMaxBytes},
	},
	Screenshot: store.ImageSpec{
		Formats:          []string{"png", "jpeg", "webp"},
		MinWidth:         1080,
		MinHeight:        1920,
		Aspect:           &store.Size{Width: 9, Height: 16},
		MaxBytes:         5 << 20,
		MaxBytesByFormat: map[string]int64{"webp": screenshotWebPMaxBytes},
	},
	MinScreenshots: 3,
	MaxScreenshots: 10,
	Check:          checkListing,
}

// checkListing adds the rule ListingSpec's fields can't express: brief
// and description must differ (AGC rejects identical briefInfo / appDesc).
func checkListing(l *store.Listing, _ store.ImageInspector) []error {
	if l.Brief != "" && l.Brief == l.Description {
		return []error{fmt.Errorf("%s and %s must differ, AGC rejects identical briefInfo / appDesc", store.ListingBrief, store.ListingDescription)}
	}
	return nil
}

// langFileInfo / fileInfo mirror AGC's LangFileInfo / FileInfo.
type langFileInfo struct {
	Lang         string     `json:"lang"`
	FileInfoList []fileInfo `json:"fileInfoList"`
}

type fileInfo struct {
	DeviceType   int      `json:"deviceType"`
	ObjectIDList []string `json:"objectIdList"`
	ShowType     int      `json:"showType"`
}

// phonePortrait wraps objectIds as one language's phone/portrait entry.
func phonePortrait(lang string, objectIDs ...string) []langFileInfo {
	return []langFileInfo{{
		Lang: lang,
		FileInfoList: []fileInfo{{
			DeviceType:   deviceTypePhone,
			ObjectIDList: objectIDs,
			ShowType:     showTypePortrait,
		}},
	}}
}

// updateListingFiles uploads the listing's icon and screenshots (same
// upload-url + signed PUT as the package) and binds the resulting
// objectIds to the draft version via app-file-info. Only the lists with
// new files are sent, so the other assets stay as they are.
func (s *Store) updateListingFiles(ctx context.Context, appID, lang string, l *store.Listing) error {
	rep := progress.Safe(nil)
	body := map[string]any{}
	if l.Icon != "" {
		id, err := s.uploadFile(ctx, appID, l.Icon, rep)
		if err != nil {
			return fmt.Errorf("upload icon: %w", err)
		}
		body["appIconList"] = phonePortrait(lang, id)
	}
	if len(l.Screenshots) > 0 {
		ids := make([]string, 0, len(l.Screenshots))
		for i, p := range l.Screenshots {
			id, err := s.uploadFile(ctx, appID, p, rep)
			if err != nil {
				return fmt.Errorf("upload screenshot %d: %w", i+1, err)
			}
			ids = append(ids, id)
		}
		body["screenShotList"] = phonePortrait(lang, ids...)
	}

	var resp struct {
		Ret retInfo `json:"ret"`
	}
	httpResp, err := s.client.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{"appId": appID, "releaseType": "1"}).
		SetBody(body).
		SetResult(&resp).
		Put("/api/publish/v3/app-file-info")
	if err != nil {
		return err
	}
	if httpResp.IsError() {
		return httpError(httpResp)
	}
	if resp.Ret.Code != 0 {
		return store.Categorize(classify(resp.Ret), fmt.Errorf("app-file-info: [%d] %s", resp.Ret.Code, resp.Ret.text()))
	}
	return nil
}
