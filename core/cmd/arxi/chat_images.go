package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/michiTrader/arxi/internal/model"
	"github.com/michiTrader/arxi/internal/provider"
	"github.com/michiTrader/arxi/internal/turn"
)

// Pictures ride inside the request line, and a line over maxLineBytes closes the
// whole connection. So the pictures of one message are held well under that: what is
// left of the line is for the text and the conversation sent with it.
const (
	maxChatImages     = 4
	maxChatImageBytes = 600 * 1024 // base64 characters, all pictures of one message
)

type imagesKey struct{}

// wireImage is one picture as a client sends it: the format and its bytes in base64.
type wireImage struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// withImages reads the `images` parameter (a JSON list of {media_type, data}) and
// carries the pictures on the context. An empty string means none. Anything that
// could not be sent is refused here, before a model is chosen or billed.
func withImages(ctx context.Context, raw string) (context.Context, error) {
	if strings.TrimSpace(raw) == "" {
		return ctx, nil
	}
	var list []wireImage
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return ctx, badInvocation{fmt.Errorf("images is not a JSON list of {media_type, data}: %w", err)}
	}
	if len(list) == 0 {
		return ctx, nil
	}
	if len(list) > maxChatImages {
		return ctx, badInvocation{fmt.Errorf("%d images in one message; at most %d are taken", len(list), maxChatImages)}
	}
	total := 0
	blocks := make([]turn.ContentBlock, 0, len(list))
	for i, im := range list {
		okType := false
		for _, t := range provider.ImageTypes {
			okType = okType || im.MediaType == t
		}
		if !okType {
			return ctx, badInvocation{fmt.Errorf("image %d is %q; use png, jpeg, gif or webp", i+1, im.MediaType)}
		}
		if _, err := base64.StdEncoding.DecodeString(im.Data); err != nil || im.Data == "" {
			return ctx, badInvocation{fmt.Errorf("image %d is not valid base64 data", i+1)}
		}
		total += len(im.Data)
		blocks = append(blocks, turn.ContentBlock{Type: turn.BlockImage,
			Source: &turn.MediaSource{MediaType: im.MediaType, Data: im.Data}})
	}
	if total > maxChatImageBytes {
		return ctx, badInvocation{fmt.Errorf("the images weigh %d KB once encoded; one message takes at most %d KB (a longer line closes the connection). Send smaller ones",
			total/1024, maxChatImageBytes/1024)}
	}
	return context.WithValue(ctx, imagesKey{}, blocks), nil
}

func imagesFrom(ctx context.Context) []turn.ContentBlock {
	b, _ := ctx.Value(imagesKey{}).([]turn.ContentBlock)
	return b
}

// checkImages refuses pictures for a model not declared to take them, naming how to
// say so, before anything is billed.
func checkImages(res model.Resolution, n int) error {
	if n == 0 || res.Images {
		return nil
	}
	return badInvocation{errors.New(res.Provider + "/" + res.Model + " is not set to take images, so nothing was sent. " +
		"If it does, say so with: arxi model update " + res.Provider + "/" + res.Model + " --images true")}
}
