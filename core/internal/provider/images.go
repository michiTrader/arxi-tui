package provider

import (
	"fmt"

	"github.com/michiTrader/arxi/internal/turn"
)

// ImageTypes are the picture formats every provider that takes images
// accepts. Anything else is refused here, before a request is built.
var ImageTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}

// imageData checks an image block and returns its media type and base64
// data. Only inline data is supported: arxi never asks a provider to fetch a
// URL on the user's behalf.
func imageData(block turn.ContentBlock) (mediaType, data string, err error) {
	src := block.Source
	if src == nil {
		return "", "", fmt.Errorf("image block has no source")
	}
	if src.URL != "" {
		return "", "", fmt.Errorf("image by URL is not supported; send the image data")
	}
	if src.Data == "" {
		return "", "", fmt.Errorf("image block has no data")
	}
	for _, ok := range ImageTypes {
		if src.MediaType == ok {
			return src.MediaType, src.Data, nil
		}
	}
	return "", "", fmt.Errorf("image type %q is not supported (use png, jpeg, gif or webp)", src.MediaType)
}
