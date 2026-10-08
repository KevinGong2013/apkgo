package vivo

import (
	"context"
	"fmt"

	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// fetchListing is registered with `apkgo listing`. app.query.details
// (doc 346) returns simpleDesc, detailDesc, icon (URL) and screenshot
// (URLs, comma-separated).
func fetchListing(ctx context.Context, cfg map[string]string, q store.ListingQuery) store.RemoteListing {
	s, err := New(cfg)
	if err != nil {
		return store.RemoteListing{Store: "vivo", Error: err.Error()}
	}
	return s.remoteListing(ctx, q.Package)
}

func (s *Store) remoteListing(ctx context.Context, pkg string) store.RemoteListing {
	res := store.RemoteListing{Store: "vivo"}
	app, err := s.queryApp(ctx, pkg)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if app == nil {
		res.Error = fmt.Sprintf("no app found for package %s under this developer account", pkg)
		return res
	}
	res.Brief = app.SimpleDesc
	res.Description = app.DetailDesc
	res.Icon = app.Icon
	res.Screenshots = store.SplitURLs(app.Screenshot, ",")
	return res
}
