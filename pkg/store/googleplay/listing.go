package googleplay

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/go-resty/resty/v2"

	"github.com/KevinGong2013/apkgo/v4/pkg/imgcheck"
	"github.com/KevinGong2013/apkgo/v4/pkg/store"
)

// listingSpec mirrors Play Console's store-listing limits: short
// description ≤80, full description ≤4000, a 512×512 PNG hi-res icon
// ≤1 MB, and 2–8 PNG/JPEG phone screenshots with edges in 320–3840 px.
var listingSpec = &store.ListingSpec{
	Brief:       store.TextSpec{Max: 80},
	Description: store.TextSpec{Max: 4000},
	Icon: store.ImageSpec{
		Formats:  []string{"png"},
		Sizes:    []store.Size{{Width: 512, Height: 512}},
		MaxBytes: 1 << 20,
	},
	Screenshot: store.ImageSpec{
		Formats: []string{"png", "jpeg"},
		MinEdge: 320,
		MaxEdge: 3840,
	},
	MinScreenshots: 2,
	MaxScreenshots: 8,
	Check:          checkListing,
}

// checkListing enforces Play's screenshot aspect rule, which ImageSpec
// can't express: the long edge may be at most twice the short edge.
// Unreadable files are skipped — ValidateListing already reports them.
func checkListing(l *store.Listing) []error {
	var errs []error
	for i, p := range l.Screenshots {
		info, err := imgcheck.Inspect(p)
		if err != nil {
			continue
		}
		if max(info.Width, info.Height) > 2*min(info.Width, info.Height) {
			errs = append(errs, fmt.Errorf("%s[%d]: %s: size %dx%d, long edge must be at most twice the short edge",
				store.ListingScreenshots, i, p, info.Width, info.Height))
		}
	}
	return errs
}

// Play image types (AppImageType) used for the listing.
const (
	imageIcon             = "icon"
	imagePhoneScreenshots = "phoneScreenshots"
)

// updateListing writes l into the edit's default-language store listing.
// Only non-empty fields are sent: text via listings.patch (title and
// other fields are kept), the icon via images.upload (Play keeps a single
// icon, so the upload replaces it), and screenshots by clearing the
// phone set (images.deleteall) and uploading l.Screenshots in order.
func (s *Store) updateListing(ctx context.Context, editID string, l *store.Listing) error {
	var details struct {
		DefaultLanguage string `json:"defaultLanguage"`
	}
	resp, err := s.client.R().
		SetContext(ctx).
		SetResult(&details).
		Get(fmt.Sprintf("/edits/%s/details", editID))
	if err := checkResp("get details", resp, err); err != nil {
		return err
	}
	lang := details.DefaultLanguage
	if lang == "" {
		return fmt.Errorf("get details: empty defaultLanguage")
	}

	if l.Brief != "" || l.Description != "" {
		body := map[string]string{}
		if l.Brief != "" {
			body["shortDescription"] = l.Brief
		}
		if l.Description != "" {
			body["fullDescription"] = l.Description
		}
		resp, err := s.client.R().
			SetContext(ctx).
			SetBody(body).
			Patch(fmt.Sprintf("/edits/%s/listings/%s", editID, lang))
		if err := checkResp("patch "+lang+" listing", resp, err); err != nil {
			return err
		}
	}

	if l.Icon != "" {
		if err := s.uploadImage(ctx, editID, lang, imageIcon, l.Icon); err != nil {
			return err
		}
	}

	if len(l.Screenshots) > 0 {
		resp, err := s.client.R().
			SetContext(ctx).
			Delete(fmt.Sprintf("/edits/%s/listings/%s/%s", editID, lang, imagePhoneScreenshots))
		if err := checkResp("clear "+lang+" "+imagePhoneScreenshots, resp, err); err != nil {
			return err
		}
		for i, p := range l.Screenshots {
			if err := s.uploadImage(ctx, editID, lang, imagePhoneScreenshots, p); err != nil {
				return fmt.Errorf("%s[%d]: %w", store.ListingScreenshots, i, err)
			}
		}
	}
	return nil
}

// uploadImage adds the image at path to the edit's listing for lang
// (edits.images.upload, simple media upload).
func (s *Store) uploadImage(ctx context.Context, editID, lang, imageType, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", imageType, err)
	}
	resp, err := s.client.R().
		SetContext(ctx).
		SetHeader("Content-Type", http.DetectContentType(data)).
		SetQueryParam("uploadType", "media").
		SetBody(data).
		Post(fmt.Sprintf("%s/edits/%s/listings/%s/%s", s.uploadBase, editID, lang, imageType))
	return checkResp("upload "+lang+" "+imageType, resp, err)
}

// checkResp turns a failed call into an error: transport errors as-is,
// non-2xx responses with Google's error message, categorized by status.
func checkResp(op string, resp *resty.Response, err error) error {
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if !resp.IsError() {
		return nil
	}
	msg := strings.TrimSpace(resp.String())
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(resp.Body(), &body) == nil && body.Error.Message != "" {
		msg = body.Error.Message
	}
	err = fmt.Errorf("%s: HTTP %d: %s", op, resp.StatusCode(), msg)
	switch code := resp.StatusCode(); {
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return store.Categorize(store.CategoryAuthFailed, err)
	case code == http.StatusTooManyRequests:
		return store.Categorize(store.CategoryStoreBusy, err)
	case code >= http.StatusInternalServerError:
		return store.Categorize(store.CategoryNetworkRetry, err)
	}
	return err
}
