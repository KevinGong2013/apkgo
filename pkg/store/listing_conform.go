package store

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"slices"

	"github.com/KevinGong2013/apkgo/v4/pkg/imgcheck"
)

// conformJPEGQualities are tried in order when an image has to become a
// JPEG, until one fits the spec's byte limit.
var conformJPEGQualities = []int{92, 85, 75}

// ConformListingImage prepares a listing image read back from a store
// (RemoteListing) to be submitted to that store again. field is
// ListingIcon or ListingScreenshots.
//
// A store doesn't always serve its images in a format it takes back: oppo
// may return WEBP but takes only PNG (icon) and JPEG/PNG (screenshots) on
// upload. When the pixel size fits the store's spec and only the format or
// byte size doesn't, the image is re-encoded into an accepted format — PNG
// when allowed and small enough, else JPEG — and converted is true.
// Otherwise data comes back unchanged and the store's spec reports why it
// can't be used: an image that already fits, one of the wrong pixel size
// (honor's reduced previews), a store without a ListingSpec, or bytes that
// aren't a png/jpeg/webp image.
func ConformListingImage(storeName, field string, data []byte) (out []byte, converted bool) {
	spec := ListingSpecFor(storeName)
	if spec == nil {
		return data, false
	}
	var is ImageSpec
	switch field {
	case ListingIcon:
		is = spec.Icon
	case ListingScreenshots:
		is = spec.Screenshot
	default:
		return data, false
	}
	info, err := imgcheck.InspectBytes(data)
	if err != nil || fitsImage(info, is) {
		return data, false
	}
	// Re-encoding changes only the format and byte size; anything else
	// that's off can't be fixed here.
	pixels := is
	pixels.Formats, pixels.MaxBytes, pixels.MaxBytesByFormat = nil, 0, nil
	if !fitsImage(info, pixels) {
		return data, false
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data, false
	}
	if slices.Contains(is.Formats, "png") {
		var buf bytes.Buffer
		if png.Encode(&buf, img) == nil && fitsBytes(buf.Bytes(), is) {
			return buf.Bytes(), true
		}
	}
	if slices.Contains(is.Formats, "jpeg") {
		flat := opaque(img)
		for _, q := range conformJPEGQualities {
			var buf bytes.Buffer
			if jpeg.Encode(&buf, flat, &jpeg.Options{Quality: q}) == nil && fitsBytes(buf.Bytes(), is) {
				return buf.Bytes(), true
			}
		}
	}
	return data, false
}

// fitsImage reports whether an image described by info passes spec.
func fitsImage(info imgcheck.Info, spec ImageSpec) bool {
	return checkImage("", spec, func(string) (imgcheck.Info, error) { return info, nil }) == nil
}

// fitsBytes reports whether freshly encoded data passes spec.
func fitsBytes(data []byte, spec ImageSpec) bool {
	info, err := imgcheck.InspectBytes(data)
	return err == nil && fitsImage(info, spec)
}

// opaque flattens img onto white, since JPEG has no alpha channel and the
// encoder would otherwise turn transparent pixels black.
func opaque(img image.Image) image.Image {
	b := img.Bounds()
	out := image.NewRGBA(b)
	draw.Draw(out, b, image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(out, b, img, b.Min, draw.Over)
	return out
}
