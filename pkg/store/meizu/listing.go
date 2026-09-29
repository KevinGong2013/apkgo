package meizu

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/KevinGong2013/apkgo/v4/pkg/progress"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// listingSpec is what Meizu accepts for the listing (商店资料). Sources:
// API doc open.flyme.cn/docs?id=333 (v1.07) and the review guideline
// id=110.
//
//   - Brief → recommendDesc (推荐语): no length limit is documented, so
//     it's unbounded.
//   - Description → appDesc: 100–1000 characters (review guideline).
//   - Icon / screenshots: app/image/upload only takes jpg/png/jpeg. No
//     pixel size, byte size or screenshot count is documented, so only
//     the format is checked.
var listingSpec = &store.ListingSpec{
	Description: store.TextSpec{Min: 100, Max: 1000},
	Icon:        store.ImageSpec{Formats: []string{"png", "jpeg"}},
	Screenshot:  store.ImageSpec{Formats: []string{"png", "jpeg"}},
	Check:       checkListing,
}

// imageExts are the file name extensions app/image/upload accepts. The
// check is by name — error 113007 lists "JPG", "jpg", "PNG", "png",
// "JPEG", "jpeg" — so a valid PNG saved as e.g. "icon" is still refused.
var imageExts = []string{".png", ".jpg", ".jpeg"}

// descForbidden are the special characters the review guideline (§一.4
// 应用描述和关键词) explicitly bans from 应用描述.
const descForbidden = "@#*&"

// checkListing adds the rules ListingSpec's fields can't express: the
// guideline's banned description characters and the upload endpoint's
// extension check.
func checkListing(l *store.Listing) []error {
	var errs []error
	if i := strings.IndexAny(l.Description, descForbidden); i >= 0 {
		errs = append(errs, fmt.Errorf("%s: must not contain %q (Meizu review bans @ # * &)", store.ListingDescription, l.Description[i:i+1]))
	}
	checkExt := func(field, path string) {
		if !slices.Contains(imageExts, strings.ToLower(filepath.Ext(path))) {
			errs = append(errs, fmt.Errorf("%s: %s: file name must end in .png, .jpg or .jpeg (Meizu checks the extension)", field, path))
		}
	}
	if l.Icon != "" {
		checkExt(store.ListingIcon, l.Icon)
	}
	for i, p := range l.Screenshots {
		checkExt(fmt.Sprintf("%s[%d]", store.ListingScreenshots, i), p)
	}
	return errs
}

// listingUpdate is a store.Listing mapped onto publish-body fields, with
// images replaced by their app/image/upload destFileName. Empty fields
// keep the value echoed from app/detail.
type listingUpdate struct {
	recommendDesc string
	appDesc       string
	icon          string
	screenShots   []string
}

// resolveListing uploads the listing's icon and screenshots (in display
// order) and returns the fields to override in the publish body. Meizu
// has no separate listing endpoint: publish / failapp/update carry the
// full listing, so this runs after the APK upload and before submit.
func (s *Store) resolveListing(ctx context.Context, l *store.Listing) (*listingUpdate, error) {
	lu := &listingUpdate{recommendDesc: l.Brief, appDesc: l.Description}
	rep := progress.Safe(nil)
	if l.Icon != "" {
		name, err := s.uploadFile(ctx, imageUploadURI, l.Icon, rep)
		if err != nil {
			return nil, fmt.Errorf("upload icon: %w", err)
		}
		lu.icon = name
	}
	for i, p := range l.Screenshots {
		name, err := s.uploadFile(ctx, imageUploadURI, p, rep)
		if err != nil {
			return nil, fmt.Errorf("upload %s[%d]: %w", store.ListingScreenshots, i, err)
		}
		lu.screenShots = append(lu.screenShots, name)
	}
	return lu, nil
}

// apply overrides body's listing fields with lu's non-empty ones. A nil
// lu leaves body untouched.
func (lu *listingUpdate) apply(body map[string]any) {
	if lu == nil {
		return
	}
	if lu.recommendDesc != "" {
		body["recommendDesc"] = lu.recommendDesc
	}
	if lu.appDesc != "" {
		body["appDesc"] = lu.appDesc
	}
	if lu.icon != "" {
		body["icon"] = lu.icon
	}
	if len(lu.screenShots) > 0 {
		body["screenShots"] = lu.screenShots
	}
}
