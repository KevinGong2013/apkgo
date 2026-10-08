package oppo

import (
	"context"
	"fmt"

	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// fetchListing is registered with `apkgo listing`. /app/info (doc 11004)
// returns the listing fields /app/upd round-trips: summary, detail_desc,
// icon_url and pic_url (portrait screenshots, comma-separated).
func fetchListing(ctx context.Context, cfg map[string]string, q store.ListingQuery) store.RemoteListing {
	s, err := New(cfg)
	if err != nil {
		return store.RemoteListing{Store: "oppo", Error: err.Error()}
	}
	return s.remoteListing(ctx, q.Package)
}

func (s *Store) remoteListing(ctx context.Context, pkg string) store.RemoteListing {
	res := store.RemoteListing{Store: "oppo"}
	app, err := s.queryApp(ctx, pkg)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if app == nil {
		res.Error = fmt.Sprintf("no app found for package %s under this developer account", pkg)
		return res
	}
	res.Brief = app.Summary
	res.Description = app.DetailDesc
	res.Icon = app.IconURL
	res.Screenshots = store.SplitURLs(app.PicURL, ",")
	return res
}
