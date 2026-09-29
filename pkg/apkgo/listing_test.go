package apkgo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KevinGong2013/apkgo/v4/pkg/config"
	"github.com/KevinGong2013/apkgo/v4/pkg/listing"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
	"github.com/KevinGong2013/apkgo/v4/pkg/uploader"
)

// listingTestStore records the Listing each upload received.
type listingTestStore struct {
	name string
	mu   *sync.Mutex
	got  map[string]*store.Listing
}

func (s *listingTestStore) Name() string { return s.name }

func (s *listingTestStore) Upload(_ context.Context, req *store.UploadRequest) *store.UploadResult {
	s.mu.Lock()
	s.got[s.name] = req.Listing
	s.mu.Unlock()
	return store.NewResult(s.name, time.Now())
}

var (
	listingMu  sync.Mutex
	listingGot = map[string]*store.Listing{}
)

func init() {
	factory := func(name string) store.Factory {
		return func(map[string]string) (store.Store, error) {
			return &listingTestStore{name: name, mu: &listingMu, got: listingGot}, nil
		}
	}
	store.Register("test-listing-yes", store.ConfigSchema{
		Name:       "test-listing-yes",
		AcceptsAAB: true,
		Listing:    &store.ListingSpec{Brief: store.TextSpec{Max: 5}},
	}, factory("test-listing-yes"))
	store.Register("test-listing-no", store.ConfigSchema{
		Name:       "test-listing-no",
		AcceptsAAB: true,
	}, factory("test-listing-no"))
}

func listingJob(t *testing.T, l *listing.File) Job {
	t.Helper()
	aab := filepath.Join(t.TempDir(), "app.aab")
	if err := os.WriteFile(aab, []byte("bundle"), 0o600); err != nil {
		t.Fatal(err)
	}
	return Job{
		APKFile:  aab,
		Listing:  l,
		Progress: uploader.NopManager,
		Config: &config.Config{Stores: map[string]map[string]string{
			"test-listing-yes": {},
			"test-listing-no":  {},
		}},
	}
}

func byStore(results []*store.UploadResult) map[string]*store.UploadResult {
	out := make(map[string]*store.UploadResult, len(results))
	for _, r := range results {
		out[r.Store] = r
	}
	return out
}

func TestRunListingValidationFailsBeforeUpload(t *testing.T) {
	job := listingJob(t, &listing.File{Fields: listing.Fields{Brief: "太长的一句话介绍"}})
	job.DryRun = true
	_, err := Run(context.Background(), job)
	if err == nil || !strings.Contains(err.Error(), "test-listing-yes: listing brief: length 8 chars, at most 5 allowed") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunListingUnknownStore(t *testing.T) {
	job := listingJob(t, &listing.File{Stores: map[string]listing.Fields{"nope": {Brief: "x"}}})
	if _, err := Run(context.Background(), job); err == nil || !strings.Contains(err.Error(), "unknown stores [nope]") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunListingDryRun(t *testing.T) {
	job := listingJob(t, &listing.File{Fields: listing.Fields{Brief: "介绍"}})
	job.DryRun = true
	res, err := Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	got := byStore(res.Results)
	if l := got["test-listing-yes"].Listing; len(l) != 1 || l[0] != "brief" {
		t.Errorf("yes: listing = %v", l)
	}
	if l := got["test-listing-no"].Listing; l != nil {
		t.Errorf("unsupported store should report no listing, got %v", l)
	}
}

func TestRunListingPassedPerStore(t *testing.T) {
	job := listingJob(t, &listing.File{
		Fields: listing.Fields{Brief: "默认"},
		Stores: map[string]listing.Fields{"test-listing-yes": {Brief: "覆盖"}},
	})
	res, err := Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	listingMu.Lock()
	yes, no := listingGot["test-listing-yes"], listingGot["test-listing-no"]
	listingMu.Unlock()
	if yes == nil || yes.Brief != "覆盖" {
		t.Errorf("yes got listing %+v, want brief 覆盖", yes)
	}
	if no != nil {
		t.Errorf("unsupported store got listing %+v", no)
	}
	got := byStore(res.Results)
	if l := got["test-listing-yes"].Listing; len(l) != 1 || l[0] != "brief" {
		t.Errorf("yes result listing = %v", l)
	}
	if l := got["test-listing-no"].Listing; l != nil {
		t.Errorf("no result listing = %v", l)
	}
}

func TestRunListingFileAndListingExclusive(t *testing.T) {
	job := listingJob(t, &listing.File{})
	job.ListingFile = "listing.yaml"
	if _, err := Run(context.Background(), job); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("err = %v", err)
	}
}
