// Package imgcheck reads the format, pixel size and byte size of a local
// image without decoding its pixels. Store listing validation uses it to
// check icons and screenshots against each store's spec before any upload.
package imgcheck

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // register decoders for image.DecodeConfig
	_ "image/png"
	"os"

	_ "golang.org/x/image/webp"
)

// Info describes one image file.
type Info struct {
	// Format is the decoder name: "png", "jpeg" or "webp".
	Format string
	Width  int
	Height int
	Bytes  int64
}

// Inspect reads the header of the image at path. Only the header is
// decoded, so this is cheap even for large screenshots.
func Inspect(path string) (Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return Info{}, err
	}
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return Info{}, fmt.Errorf("%s: not a png/jpeg/webp image: %w", path, err)
	}
	return Info{Format: format, Width: cfg.Width, Height: cfg.Height, Bytes: st.Size()}, nil
}

// InspectBytes is Inspect for an image already in memory, e.g. one a
// service received over HTTP before storing it.
func InspectBytes(data []byte) (Info, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Info{}, fmt.Errorf("not a png/jpeg/webp image: %w", err)
	}
	return Info{Format: format, Width: cfg.Width, Height: cfg.Height, Bytes: int64(len(data))}, nil
}
