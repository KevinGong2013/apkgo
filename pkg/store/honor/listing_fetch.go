package honor

import (
	"cmp"
	"context"
	"slices"

	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// fetchListing is registered with `apkgo listing`. get-app-detail returns
// the language entry (intro, briefIntro) plus the app's bound files, each
// with a viewable fileUrl: fileType 1 is the icon, 3 the portrait
// screenshots (2 the landscape ones), ordered by `order`.
func fetchListing(_ context.Context, cfg map[string]string, q store.ListingQuery) store.RemoteListing {
	s, err := New(cfg)
	if err != nil {
		return store.RemoteListing{Store: "honor", Error: err.Error()}
	}
	return s.remoteListing(q.Package)
}

func (s *Store) remoteListing(pkg string) store.RemoteListing {
	res := store.RemoteListing{Store: "honor"}
	appID := s.configAppID
	if appID == "" {
		var err error
		if appID, err = s.getAppID(pkg); err != nil {
			res.Error = err.Error()
			return res
		}
	}
	lang, files, err := s.getAppDetail(appID)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Brief = lang.BriefIntro
	res.Description = lang.Intro

	// Per-language files carry the languageId; keep the chosen language's
	// (and any the API left untagged).
	ofType := func(fileType int) []pubFileInfo {
		var out []pubFileInfo
		for _, f := range files {
			if f.FileType == fileType && f.FileURL != "" && (f.LanguageID == "" || f.LanguageID == lang.LanguageID) {
				out = append(out, f)
			}
		}
		slices.SortStableFunc(out, func(a, b pubFileInfo) int { return cmp.Compare(a.Order, b.Order) })
		return out
	}
	if icons := ofType(fileTypeIcon); len(icons) > 0 {
		res.Icon = icons[0].FileURL
	}
	shots := ofType(fileTypePortraitScreenshot)
	if len(shots) == 0 {
		shots = ofType(fileTypeLandscapeScreenshot)
	}
	for _, f := range shots {
		res.Screenshots = append(res.Screenshots, f.FileURL)
	}
	return res
}
