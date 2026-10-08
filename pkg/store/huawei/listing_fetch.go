package huawei

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// phoneDeviceType is AGC's deviceType for phones in deviceMaterials.
const phoneDeviceType = 4

// remoteLanguage is one entry of app-info's languages (LanguageInfo).
// icon / introPic are the language-level image URLs; deviceMaterials
// repeats them per device and is preferred when present. It's decoded
// leniently so an unexpected shape there can't hide the text fields.
type remoteLanguage struct {
	Lang            string          `json:"lang"`
	AppDesc         string          `json:"appDesc"`
	BriefInfo       string          `json:"briefInfo"`
	Icon            string          `json:"icon"`
	IntroPic        string          `json:"introPic"` // screenshot URLs, comma-separated
	DeviceMaterials json.RawMessage `json:"deviceMaterials"`
}

type deviceMaterial struct {
	DeviceType  int      `json:"deviceType"`
	AppIcon     string   `json:"appIcon"`
	ScreenShots []string `json:"screenShots"`
}

// fetchListing is registered with `apkgo listing`. The read-only app-info
// query returns every language's briefInfo / appDesc and image URLs; the
// app's default language is the one apkgo updates, so that's the one read.
func fetchListing(ctx context.Context, cfg map[string]string, q store.ListingQuery) store.RemoteListing {
	s, err := New(cfg)
	if err != nil {
		return store.RemoteListing{Store: "huawei", Error: err.Error()}
	}
	return s.remoteListing(ctx, q.Package)
}

func (s *Store) remoteListing(ctx context.Context, pkg string) store.RemoteListing {
	res := store.RemoteListing{Store: "huawei"}
	appID := s.configAppID
	if appID == "" {
		var err error
		if appID, err = s.fetchAppID(pkg); err != nil {
			res.Error = err.Error()
			return res
		}
	}
	var resp struct {
		Ret     retInfo `json:"ret"`
		AppInfo struct {
			DefaultLang string `json:"defaultLang"`
		} `json:"appInfo"`
		DefaultLang string           `json:"defaultLang"`
		Languages   []remoteLanguage `json:"languages"`
	}
	httpResp, err := s.client.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{"appId": appID, "releaseType": "1"}).
		SetResult(&resp).
		Get("/api/publish/v2/app-info")
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if httpResp.IsError() {
		res.Error = fmt.Sprintf("http %d: %s", httpResp.StatusCode(), strings.TrimSpace(string(httpResp.Body())))
		return res
	}
	if resp.Ret.Code != 0 {
		res.Error = fmt.Sprintf("[%d] %s", resp.Ret.Code, resp.Ret.text())
		return res
	}
	lang := pickLanguage(resp.Languages, cmp.Or(resp.AppInfo.DefaultLang, resp.DefaultLang, fallbackLang))
	if lang == nil {
		res.Error = "app-info returned no languages"
		return res
	}
	res.Brief = lang.BriefInfo
	res.Description = lang.AppDesc
	res.Icon = lang.Icon
	res.Screenshots = store.SplitURLs(lang.IntroPic, ",")

	var materials []deviceMaterial
	if len(lang.DeviceMaterials) > 0 && json.Unmarshal(lang.DeviceMaterials, &materials) == nil && len(materials) > 0 {
		m := materials[0]
		for _, c := range materials {
			if c.DeviceType == phoneDeviceType {
				m = c
				break
			}
		}
		res.Icon = cmp.Or(m.AppIcon, res.Icon)
		if len(m.ScreenShots) > 0 {
			res.Screenshots = m.ScreenShots
		}
	}
	return res
}

// pickLanguage returns the entry for want, else zh-CN, else the first.
func pickLanguage(langs []remoteLanguage, want string) *remoteLanguage {
	for _, l := range []string{want, fallbackLang} {
		for i := range langs {
			if langs[i].Lang == l {
				return &langs[i]
			}
		}
	}
	if len(langs) > 0 {
		return &langs[0]
	}
	return nil
}
