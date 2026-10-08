package apkgo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/KevinGong2013/apkgo/v4/pkg/apk"
	"github.com/KevinGong2013/apkgo/v4/pkg/config"
	"github.com/KevinGong2013/apkgo/v4/pkg/httpx"
	"github.com/KevinGong2013/apkgo/v4/pkg/imgcheck"
	"github.com/KevinGong2013/apkgo/v4/pkg/listing"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// ListingFileName is the listing file FetchListing writes under OutDir.
const ListingFileName = "listing.yaml"

// listingImageMaxBytes caps one downloaded listing image. The largest any
// store accepts is 5MB; anything far beyond that isn't a listing image.
const listingImageMaxBytes = 20 << 20

// ListingJob mirrors AuditJob: read the listing (商店资料 — one-line intro,
// description, icon, screenshots) each configured store currently holds
// for an app. It never writes to a store; the result is the starting
// point for a listing file to edit and submit with the next version
// (`apkgo upload --listing`).
type ListingJob struct {
	// Config is the resolved store config. Required.
	Config *config.Config

	// Stores filters which configured stores to read. Empty means every
	// store the Config has credentials for.
	Stores []string

	// Package is the app to look up. Required unless APKFile is set.
	Package string

	// APKFile is an alternative source for Package: when set and Package
	// is empty, the APK is parsed (URL fetch supported via FetchHeaders)
	// and its package name is used.
	APKFile string

	// FetchHeaders attaches HTTP headers to URL fetches of APKFile.
	FetchHeaders map[string]string

	// OutDir, when set, also saves what was read as a listing file: the
	// images are downloaded into OutDir/assets and OutDir/listing.yaml
	// holds each store's values under `stores:`. It must not already
	// contain a listing.yaml.
	OutDir string
}

// ListingStoreResult is one store's section: what it reported plus
// whether the store has a listing fetcher registered at all.
type ListingStoreResult struct {
	store.RemoteListing
	Supported bool `json:"supported"`
	// Notes are set when saving to OutDir: what was left out of the
	// listing file, or needs changing before it can be uploaded, judged
	// against the store's own ListingSpec.
	Notes []string `json:"notes,omitempty"`
}

// ListingReport is the structured `apkgo listing` output.
type ListingReport struct {
	Package string               `json:"package,omitempty"`
	Stores  []ListingStoreResult `json:"stores"`
	// File is the listing file written when ListingJob.OutDir was set.
	File string `json:"file,omitempty"`
}

// AnyFailed reports whether any supported store's read (or image
// download) failed.
func (r *ListingReport) AnyFailed() bool {
	for _, s := range r.Stores {
		if s.Supported && s.Error != "" {
			return true
		}
	}
	return false
}

// FetchListing reads the current listing from the configured stores. It
// performs no uploads and changes nothing on any store.
//
// Pre-config errors (URL fetch / APK parse / no package / OutDir already
// holding a listing file) surface as the returned error. Per-store
// failures land in the ListingStoreResult's Error field — FetchListing
// itself returns nil so a caller gets a complete report even when some
// stores are unreachable.
func FetchListing(ctx context.Context, job ListingJob) (*ListingReport, error) {
	if job.Config == nil {
		return nil, fmt.Errorf("apkgo.ListingJob.Config is required")
	}

	pkg := job.Package
	if pkg == "" && job.APKFile != "" {
		paths, cleanup, err := httpx.FetchToTempBatch(ctx, []string{job.APKFile}, job.FetchHeaders)
		if err != nil {
			return nil, fmt.Errorf("fetch apk: %w", err)
		}
		defer cleanup()
		apkPath := paths[0]
		if _, err := os.Stat(apkPath); err != nil {
			return nil, fmt.Errorf("apk file: %w", err)
		}
		// AABs can't be parsed for a package name; the operator must pass
		// --package explicitly.
		if !apk.IsAAB(apkPath) {
			info, err := parsePackage(apkPath)
			if err != nil {
				return nil, err
			}
			pkg = info.PackageName
		}
	}
	if pkg == "" {
		return nil, fmt.Errorf("a package name is required: pass --package or an APK via --file")
	}

	var listingPath string
	if job.OutDir != "" {
		listingPath = filepath.Join(job.OutDir, ListingFileName)
		if _, err := os.Stat(listingPath); err == nil {
			return nil, fmt.Errorf("%s already exists (pick another output directory)", listingPath)
		}
	}

	var filter map[string]bool
	if len(job.Stores) > 0 {
		filter = make(map[string]bool, len(job.Stores))
		for _, n := range job.Stores {
			if n = strings.TrimSpace(n); n != "" {
				filter[n] = true
			}
		}
	}

	names := make([]string, 0, len(job.Config.Stores))
	for name := range job.Config.Stores {
		if filter != nil && !filter[name] {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("no matching stores configured")
	}

	out := &ListingReport{Package: pkg}
	q := store.ListingQuery{Package: pkg}
	for _, name := range names {
		scfg := cleanStoreCfg(job.Config.Stores[name])
		// "type.instance" naming (e.g. script.cdn): the fetcher is keyed
		// by the type prefix, not the instance name.
		key := name
		if dot := strings.Index(name, "."); dot > 0 {
			key = name[:dot]
		}
		res, supported := store.FetchListing(ctx, key, scfg, q)
		res.Store = name
		out.Stores = append(out.Stores, ListingStoreResult{RemoteListing: res, Supported: supported})
	}

	if job.OutDir != "" {
		if err := saveListing(ctx, job.OutDir, listingPath, out); err != nil {
			return nil, err
		}
		out.File = listingPath
	}
	return out, nil
}

// saveListing downloads every image the report names into dir/assets and
// writes the listing file. Every store's values go under its own
// `stores:` entry with no shared defaults: a store whose listing couldn't
// be read then has no entry and is left untouched by an upload, instead
// of inheriting another store's values. An image that fails to download
// is recorded in that store's Error and left out of the file.
//
// What a store hands back isn't always what it would accept: some serve
// re-encoded previews instead of the originals (honor: WEBP at reduced
// size). Images that fail the store's own ListingSpec are therefore left
// out of the file (the download stays in assets for reference) so the
// store keeps what it has; text is always written, with a note when it
// wouldn't pass. Either way the store's Notes say so.
func saveListing(ctx context.Context, dir, listingPath string, report *ListingReport) error {
	assets := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		return err
	}
	dl := &imageDownloader{dir: assets, bySHA: map[string]string{}, count: map[string]int{}, client: &http.Client{Timeout: 60 * time.Second}}

	file := listing.File{Stores: map[string]listing.Fields{}}
	for i := range report.Stores {
		s := &report.Stores[i]
		if !s.Supported || (s.Error != "" && s.Brief == "" && s.Description == "" && s.Icon == "" && len(s.Screenshots) == 0) {
			continue
		}
		// unfit says why l wouldn't pass this store's spec ("" = fits, or
		// the store declares none): the first problem, plus how many more.
		unfit := func(l *store.Listing) string {
			key := s.Store
			if dot := strings.Index(key, "."); dot > 0 {
				key = key[:dot]
			}
			if store.ListingSpecFor(key) == nil {
				return ""
			}
			errs := store.ValidateListing(key, l)
			if len(errs) == 0 {
				return ""
			}
			why := strings.ReplaceAll(errs[0].Error(), assets+string(filepath.Separator), "assets/")
			if len(errs) > 1 {
				why += fmt.Sprintf(" (and %d more)", len(errs)-1)
			}
			return why
		}

		fields := listing.Fields{Brief: s.Brief, Description: s.Description}
		var problems []string
		if s.Icon != "" {
			name, err := dl.fetch(ctx, s.Icon, "icon")
			if err != nil {
				problems = append(problems, fmt.Sprintf("download icon: %v", err))
			} else if why := unfit(&store.Listing{Icon: filepath.Join(assets, name)}); why != "" {
				s.Notes = append(s.Notes, "icon left out of the listing file — the store's copy doesn't meet its own upload spec: "+why)
			} else {
				fields.Icon = path.Join("assets", name)
			}
		}
		var shots []string
		for n, u := range s.Screenshots {
			name, err := dl.fetch(ctx, u, "screenshot")
			if err != nil {
				// A partial screenshot set would replace the store's full
				// one on upload; keep none rather than some.
				problems = append(problems, fmt.Sprintf("download screenshot %d: %v", n+1, err))
				shots = nil
				break
			}
			shots = append(shots, name)
		}
		if len(shots) > 0 {
			local := make([]string, len(shots))
			for i, name := range shots {
				local[i] = filepath.Join(assets, name)
			}
			if why := unfit(&store.Listing{Screenshots: local}); why != "" {
				s.Notes = append(s.Notes, "screenshots left out of the listing file — the store's copies don't meet its own upload spec: "+why)
			} else {
				for _, name := range shots {
					fields.Screenshots = append(fields.Screenshots, path.Join("assets", name))
				}
			}
		}
		if why := unfit(&store.Listing{Brief: fields.Brief, Description: fields.Description}); why != "" {
			s.Notes = append(s.Notes, "text needs changing before upload (or delete the field to leave it as is): "+why)
		}
		if len(problems) > 0 {
			s.Error = strings.Join(append(nonEmpty(s.Error), problems...), "; ")
		}
		if fields.Brief != "" || fields.Description != "" || fields.Icon != "" || len(fields.Screenshots) > 0 {
			file.Stores[s.Store] = fields
		}
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, `# Store listing (商店资料) of %s, read by `+"`apkgo listing`"+` on %s.
#
# Each store's current values sit under its own entry. A store or field
# that is missing here could not be read, or the store's copy can't be
# uploaded back (see the command's output), and is left unchanged on
# upload. Edit what you want to change, then:
#
#   apkgo upload -f app.apk --listing %s --dry-run
#
# Image paths are relative to this file.

`, report.Package, time.Now().Format("2006-01-02"), ListingFileName)
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(file); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return os.WriteFile(listingPath, buf.Bytes(), 0o644)
}

func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

// imageDownloader saves listing images into dir, once per distinct
// content: stores often hold the very same icon and screenshots, so files
// are numbered per kind (icon-1.png, screenshot-3.jpg) rather than named
// after a store.
type imageDownloader struct {
	dir    string
	bySHA  map[string]string // content sha256 → file name already written
	count  map[string]int    // kind → files written so far
	client *http.Client
}

// fetch downloads url and returns the name of the file holding it — an
// earlier file when the bytes were already saved, else the next
// "<kind>-<n>" with the extension of the image's real format.
func (d *imageDownloader) fetch(ctx context.Context, url, kind string) (string, error) {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return "", fmt.Errorf("not an http(s) URL: %q", url)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return "", httpx.RedactURLError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, listingImageMaxBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > listingImageMaxBytes {
		return "", fmt.Errorf("larger than %d MB", listingImageMaxBytes>>20)
	}
	info, err := imgcheck.InspectBytes(data)
	if err != nil {
		return "", fmt.Errorf("not a png/jpeg/webp image")
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	if name, ok := d.bySHA[sha]; ok {
		return name, nil
	}
	ext := map[string]string{"jpeg": "jpg"}[info.Format]
	if ext == "" {
		ext = info.Format
	}
	name := fmt.Sprintf("%s-%d.%s", kind, d.count[kind]+1, ext)
	if err := os.WriteFile(filepath.Join(d.dir, name), data, 0o644); err != nil {
		return "", err
	}
	d.count[kind]++
	d.bySHA[sha] = name
	return name, nil
}
