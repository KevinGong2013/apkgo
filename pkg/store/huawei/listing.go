package huawei

import (
	"cmp"
	"context"
	"fmt"
	"strings"

	"github.com/KevinGong2013/apkgo/v4/pkg/imgcheck"
	"github.com/KevinGong2013/apkgo/v4/pkg/progress"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// Listing images go to the phone slot (deviceType 4) in portrait
// (imgShowType 0); other devices keep their current assets.
const (
	deviceTypePhone     = 4
	imgShowTypePortrait = 0
)

// fallbackLang is used when app-info reports no defaultLang.
const fallbackLang = "zh-CN"

// listingSpec is what AGC accepts for an Android app's listing (商店资料).
// Sources: Publishing API v2 更新语言描述信息 (app-language-info) and the
// Connect API 附录「应用文件要求」→ Android应用 → 手机 (agcapi-file-requirement,
// consistent with the console's 应用素材规范 → APK应用（手机）):
//
//   - briefInfo (一句话简介) ≤80, appDesc (应用介绍) ≤8000 characters;
//   - icon: exactly one, PNG 216×216, ≤2MB;
//   - phone screenshots: 3–5, portrait 450×800, JPG/JPEG/PNG, ≤2MB each.
var listingSpec = &store.ListingSpec{
	Brief:       store.TextSpec{Max: 80},
	Description: store.TextSpec{Max: 8000},
	Icon: store.ImageSpec{
		Formats:  []string{"png"},
		Sizes:    []store.Size{{Width: 216, Height: 216}},
		MaxBytes: 2 << 20,
	},
	Screenshot: store.ImageSpec{
		Formats:  []string{"png", "jpeg"},
		Sizes:    []store.Size{{Width: 450, Height: 800}},
		MaxBytes: 2 << 20,
	},
	MinScreenshots: 3,
	MaxScreenshots: 5,
}

// updateListing submits l's non-empty fields for the app's default
// language, as part of the draft version that app-submit sends to review:
// text via app-language-info, icon / screenshots via upload +
// app-file-info. l is already validated against listingSpec.
func (s *Store) updateListing(ctx context.Context, appID string, l *store.Listing) error {
	lang, err := s.defaultLang(ctx, appID)
	if err != nil {
		return fmt.Errorf("query default language: %w", err)
	}

	if l.Brief != "" || l.Description != "" {
		if err := s.updateLanguageInfo(ctx, appID, lang, l); err != nil {
			return fmt.Errorf("language info: %w", err)
		}
	}

	if l.Icon != "" {
		f, err := s.uploadImage(ctx, appID, l.Icon)
		if err != nil {
			return fmt.Errorf("upload %s: %w", store.ListingIcon, err)
		}
		if err := s.bindImages(ctx, appID, lang, fileTypeIcon, []uploadedFile{f}); err != nil {
			return fmt.Errorf("%s: %w", store.ListingIcon, err)
		}
	}

	if len(l.Screenshots) > 0 {
		files := make([]uploadedFile, 0, len(l.Screenshots))
		for i, p := range l.Screenshots {
			f, err := s.uploadImage(ctx, appID, p)
			if err != nil {
				return fmt.Errorf("upload %s[%d]: %w", store.ListingScreenshots, i, err)
			}
			files = append(files, f)
		}
		if err := s.bindImages(ctx, appID, lang, fileTypeScreenshot, files); err != nil {
			return fmt.Errorf("%s: %w", store.ListingScreenshots, err)
		}
	}
	return nil
}

// defaultLang reads the app's default language (appInfo.defaultLang)
// from app-info, falling back to zh-CN when none is reported.
func (s *Store) defaultLang(ctx context.Context, appID string) (string, error) {
	var resp struct {
		Ret     retInfo `json:"ret"`
		AppInfo struct {
			DefaultLang string `json:"defaultLang"`
		} `json:"appInfo"`
		// Tolerate a flattened shape too, as audit does for releaseState.
		DefaultLang string `json:"defaultLang"`
	}
	httpResp, err := s.client.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{"appId": appID, "releaseType": "1"}).
		SetResult(&resp).
		Get("/api/publish/v2/app-info")
	if err != nil {
		return "", err
	}
	if httpResp.IsError() {
		return "", fmt.Errorf("http %d: %s", httpResp.StatusCode(), strings.TrimSpace(string(httpResp.Body())))
	}
	if resp.Ret.Code != 0 {
		return "", fmt.Errorf("[%d] %s", resp.Ret.Code, resp.Ret.text())
	}
	return cmp.Or(resp.AppInfo.DefaultLang, resp.DefaultLang, fallbackLang), nil
}

// updateLanguageInfo writes the brief (briefInfo) and description
// (appDesc) for lang via PUT app-language-info. Only non-empty fields
// are sent, so the others keep their current values; appName is never
// sent (the language already exists, and the name must match the APK).
func (s *Store) updateLanguageInfo(ctx context.Context, appID, lang string, l *store.Listing) error {
	body := map[string]any{"lang": lang}
	if l.Brief != "" {
		body["briefInfo"] = l.Brief
	}
	if l.Description != "" {
		body["appDesc"] = l.Description
	}
	var resp struct {
		Ret retInfo `json:"ret"`
	}
	httpResp, err := s.client.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{"appId": appID, "releaseType": "1"}).
		SetBody(body).
		SetResult(&resp).
		Put("/api/publish/v2/app-language-info")
	if err != nil {
		return err
	}
	if httpResp.IsError() {
		return fmt.Errorf("http %d: %s", httpResp.StatusCode(), strings.TrimSpace(string(httpResp.Body())))
	}
	if resp.Ret.Code != 0 {
		return fmt.Errorf("[%d] %s", resp.Ret.Code, resp.Ret.text())
	}
	return nil
}

// uploadImage uploads one listing image, deriving the upload suffix from
// its actual format (png / jpg) rather than the file extension.
func (s *Store) uploadImage(ctx context.Context, appID, path string) (uploadedFile, error) {
	info, err := imgcheck.Inspect(path)
	if err != nil {
		return uploadedFile{}, err
	}
	suffix := info.Format
	if suffix == "jpeg" {
		suffix = "jpg"
	}
	return s.uploadFile(ctx, appID, path, suffix, progress.Safe(nil))
}

// bindImages binds uploaded images to lang's phone assets via
// app-file-info. Screenshots are bound as one portrait set, replacing the
// current ones in the given order.
func (s *Store) bindImages(ctx context.Context, appID, lang string, fileType int, files []uploadedFile) error {
	body := map[string]any{
		"fileType":   fileType,
		"lang":       lang,
		"deviceType": deviceTypePhone,
		"files":      files,
	}
	if fileType == fileTypeScreenshot {
		body["imgShowType"] = imgShowTypePortrait
	}
	ret, err := s.updateFileInfo(ctx, appID, body)
	if err != nil {
		return err
	}
	if ret.Code != 0 {
		return fmt.Errorf("update file info: [%d] %s", ret.Code, ret.text())
	}
	return nil
}
