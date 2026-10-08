package tencent

import (
	"context"
	"fmt"
	"net/url"

	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// fetchListing is registered with `apkgo listing`. /query_app_detail
// returns the text fields (one_word_summary, introduce) but no icon or
// screenshots, so those are reported as unavailable.
func fetchListing(_ context.Context, cfg map[string]string, q store.ListingQuery) store.RemoteListing {
	s, err := New(cfg)
	if err != nil {
		return store.RemoteListing{Store: "tencent", Error: err.Error()}
	}
	return s.remoteListing(q.Package)
}

func (s *Store) remoteListing(hint string) store.RemoteListing {
	res := store.RemoteListing{
		Store:       "tencent",
		Unavailable: []string{store.ListingIcon, store.ListingScreenshots},
	}
	pkg, err := s.resolvePackage(hint)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	appID, err := s.resolveAppID(pkg)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	var resp struct {
		tencentResp
		Introduce      string `json:"introduce"`
		OneWordSummary string `json:"one_word_summary"`
	}
	params := url.Values{}
	params.Set("pkg_name", pkg)
	params.Set("app_id", appID)
	if err := s.post("/query_app_detail", params, &resp); err != nil {
		res.Error = err.Error()
		return res
	}
	if resp.Ret != 0 {
		res.Error = fmt.Sprintf("[%d] %s", resp.Ret, resp.text())
		return res
	}
	res.Brief = resp.OneWordSummary
	res.Description = resp.Introduce
	return res
}
