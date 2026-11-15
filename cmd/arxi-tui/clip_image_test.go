package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"os/exec"
	"strings"
	"testing"
)

func noisyPNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	r := rand.New(rand.NewSource(1))
	for i := range img.Pix {
		img.Pix[i] = uint8(r.Intn(256))
	}
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	_ = color.White
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// A big screenshot must shrink under the message limit: a request over it closes the
// connection. Remedy if this fails: fitPicture stopped re-encoding.
func TestFitPictureShrinksABigScreenshot(t *testing.T) {
	raw := noisyPNG(900, 700)
	if len(raw) <= imageMaxFileBytes {
		t.Fatalf("the fixture must exceed the limit, got %d", len(raw))
	}
	out, ext, note, err := fitPicture(raw)
	if err != nil || len(out) > imageMaxFileBytes || ext != ".jpg" || note == "" {
		t.Fatalf("fitPicture = %d bytes %q %q %v; want a jpeg under %d with a note", len(out), ext, note, err, imageMaxFileBytes)
	}
}

// With no clipboard tool installed the answer names what to install, not a bare failure.
func TestNoClipboardToolSaysWhatToInstall(t *testing.T) {
	look := func(string) (string, error) { return "", exec.ErrNotFound }
	_, ok, err := clipboardImage(context.Background(), "linux", look)
	if ok || err == nil || !strings.Contains(err.Error(), "wl-clipboard") {
		t.Fatalf("got ok=%v err=%v; want an error naming wl-clipboard/xclip", ok, err)
	}
}

// A tool that fails (xclip with a text clipboard) falls through to the next and ends as
// "no picture", never as an error.
func TestClipboardImageReadsThePictureOrNothing(t *testing.T) {
	look := func(string) (string, error) { return "/bin/x", nil }
	pic := noisyPNG(8, 8)
	old := clipRun
	defer func() { clipRun = old }()
	clipRun = func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "wl-paste" {
			return nil, errors.New("no image")
		}
		return pic, nil
	}
	got, ok, err := clipboardImage(context.Background(), "linux", look)
	if err != nil || !ok || !bytes.Equal(got, pic) {
		t.Fatalf("got ok=%v err=%v; want the picture from the second tool", ok, err)
	}
	clipRun = func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("none") }
	if _, ok, err := clipboardImage(context.Background(), "linux", look); ok || err != nil {
		t.Fatalf("got ok=%v err=%v; want no picture and no error", ok, err)
	}
}
