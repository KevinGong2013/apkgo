package store

import (
	"context"
	"strings"

	"github.com/KevinGong2013/apkgo/v4/pkg/httptrace"
)

// ListingQuery identifies the app whose current listing to read.
type ListingQuery struct {
	Package string
}

// RemoteListing is the listing (商店资料) a store currently holds for an
// app, as its query API reports it — the starting point for a listing
// file to edit and submit with the next version. Images are URLs on the
// store's side, not local files. Which version the values belong to
// (live, or the latest submitted one) is up to each store's API.
//
// Error is set when the query itself failed (auth / network / not-found);
// the other fields are meaningful only when it's empty.
type RemoteListing struct {
	Store       string   `json:"store"`
	Brief       string   `json:"brief,omitempty"`
	Description string   `json:"description,omitempty"`
	Icon        string   `json:"icon,omitempty"`        // image URL
	Screenshots []string `json:"screenshots,omitempty"` // image URLs, in display order
	// Unavailable names the listing fields (ListingBrief, …) this store's
	// API has no way to report, so an empty field there means "unknown",
	// not "not set" (tencent returns text but no images).
	Unavailable []string `json:"unavailable,omitempty"`
	Error       string   `json:"error,omitempty"`
}

// ListingFetchFn reads the current listing for one store from its raw
// config. Like AuditFn it owns its own auth/setup and never writes to
// the store.
type ListingFetchFn func(ctx context.Context, cfg map[string]string, q ListingQuery) RemoteListing

var listingFetchers = map[string]ListingFetchFn{}

// RegisterListingFetcher opts a store into `apkgo listing`. Stores
// without one are reported as unsupported (no query API for the listing,
// or not yet wired).
func RegisterListingFetcher(name string, fn ListingFetchFn) {
	listingFetchers[name] = fn
}

// FetchListing runs the registered fetcher for a store. The second
// return value is false when the store has not registered one.
func FetchListing(ctx context.Context, name string, cfg map[string]string, q ListingQuery) (RemoteListing, bool) {
	fn, ok := listingFetchers[name]
	if !ok {
		return RemoteListing{}, false
	}
	cfg, release := httptrace.Carry(ctx, name, cfg)
	defer release()
	return fn(ctx, cfg, q), true
}

// SplitURLs splits a store's delimiter-joined image URL list, dropping
// blanks. Several stores report screenshots as one "a,b,c" string.
func SplitURLs(joined, sep string) []string {
	var out []string
	for _, u := range strings.Split(joined, sep) {
		if u = strings.TrimSpace(u); u != "" {
			out = append(out, u)
		}
	}
	return out
}
