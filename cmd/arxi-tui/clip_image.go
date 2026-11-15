package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Pasting a picture from the clipboard (a screenshot taken with Win+PrtSc, a copied image).
//
// A terminal pastes TEXT. When the clipboard holds an image, Ctrl+V sends the terminal's
// paste with no text in it (or nothing at all), so the picture never reached arxi. The
// fix asks the operating system's own clipboard tool for an image when a paste arrives
// empty, or when Ctrl+V / Alt+V is pressed, and attaches what it finds the same way a
// dropped file is attached.
//
// Which tool depends on the system, and none of them is guaranteed to be installed, so
// the answer says what is missing instead of failing silently.

// clipRun runs a clipboard program and returns what it printed. A variable so tests
// supply a fake one and never touch the real clipboard.
var clipRun = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}

// clipImageSource is one way of asking the system for the clipboard's picture.
type clipImageSource struct {
	argv   []string
	base64 bool // the tool prints the PNG as base64 text instead of raw bytes
}

// powershellImage prints the clipboard image as base64 PNG, or nothing when the clipboard
// holds no image. -STA is needed: the clipboard is a single-thread-apartment API.
const powershellImage = `Add-Type -AssemblyName System.Windows.Forms; Add-Type -AssemblyName System.Drawing; ` +
	`$i=[System.Windows.Forms.Clipboard]::GetImage(); if($i){$m=New-Object System.IO.MemoryStream; ` +
	`$i.Save($m,[System.Drawing.Imaging.ImageFormat]::Png); [Convert]::ToBase64String($m.ToArray())}`

// clipImageSources lists the tools to try on a system, most likely first, keeping only
// those that are installed.
func clipImageSources(goos string, look func(string) (string, error)) []clipImageSource {
	var all []clipImageSource
	switch goos {
	case "windows":
		all = []clipImageSource{
			{argv: []string{"powershell", "-NoProfile", "-STA", "-Command", powershellImage}, base64: true},
			{argv: []string{"pwsh", "-NoProfile", "-STA", "-Command", powershellImage}, base64: true},
		}
	case "darwin":
		all = []clipImageSource{{argv: []string{"pngpaste", "-"}}}
	default:
		all = []clipImageSource{
			{argv: []string{"wl-paste", "--no-newline", "--type", "image/png"}},
			{argv: []string{"xclip", "-selection", "clipboard", "-t", "image/png", "-o"}},
			// WSL: the Windows clipboard is reachable through the Windows PowerShell.
			{argv: []string{"powershell.exe", "-NoProfile", "-STA", "-Command", powershellImage}, base64: true},
		}
	}
	var out []clipImageSource
	for _, c := range all {
		if _, err := look(c.argv[0]); err == nil {
			out = append(out, c)
		}
	}
	return out
}

// errNoClipboardTool names what to install, per system.
func errNoClipboardTool(goos string) error {
	switch goos {
	case "darwin":
		return errors.New("no picture was pasted: install pngpaste (brew install pngpaste) so arxi can read an image from the clipboard, or save the image and use /image <path>")
	case "windows":
		return errors.New("no picture was pasted: arxi could not run PowerShell to read the clipboard; save the image and use /image <path>")
	}
	return errors.New("no picture was pasted: install wl-clipboard (Wayland) or xclip (X11) so arxi can read an image from the clipboard, or save the image and use /image <path>")
}

// clipboardImage returns the picture on the clipboard as PNG bytes. ok is false with a nil
// error when the clipboard holds no picture (the usual case of an empty text paste).
func clipboardImage(ctx context.Context, goos string, look func(string) (string, error)) (png []byte, ok bool, err error) {
	srcs := clipImageSources(goos, look)
	if len(srcs) == 0 {
		return nil, false, errNoClipboardTool(goos)
	}
	for _, s := range srcs {
		raw, rerr := clipRun(ctx, s.argv[0], s.argv[1:]...)
		if rerr != nil {
			continue // this tool has no image to give (xclip exits 1 for a text clipboard)
		}
		if s.base64 {
			dec, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
			if derr != nil {
				continue
			}
			raw = dec
		}
		if len(raw) > 0 {
			return raw, true, nil
		}
	}
	return nil, false, nil
}

// fitPicture makes a picture fit what a message may carry (imageMaxFileBytes). A picture
// already small enough is returned untouched. A bigger one is re-encoded: first the same
// size as JPEG, then shrunk by a quarter at a time, because a 4K screenshot is megabytes
// as PNG and a request longer than the limit closes the connection. It returns the bytes,
// the file extension for the marker and a note when it had to shrink.
func fitPicture(raw []byte) (out []byte, ext, note string, err error) {
	if len(raw) <= imageMaxFileBytes {
		return raw, ".png", "", nil
	}
	img, _, derr := image.Decode(bytes.NewReader(raw))
	if derr != nil {
		return nil, "", "", fmt.Errorf("the clipboard picture cannot be read (%v); save it and use /image <path>", derr)
	}
	flat := onWhite(img)
	w, h := flat.Bounds().Dx(), flat.Bounds().Dy()
	for scale := 1.0; scale >= 0.1; scale -= 0.15 {
		cur := flat
		if scale < 1 {
			cur = resizeArea(flat, int(float64(w)*scale), int(float64(h)*scale))
		}
		var buf bytes.Buffer
		if jerr := jpeg.Encode(&buf, cur, &jpeg.Options{Quality: 82}); jerr != nil {
			return nil, "", "", jerr
		}
		if buf.Len() <= imageMaxFileBytes {
			return buf.Bytes(), ".jpg", fmt.Sprintf("the clipboard picture was %d KB: it was shrunk to %dx%d (jpeg) to fit a message", len(raw)/1024, cur.Bounds().Dx(), cur.Bounds().Dy()), nil
		}
	}
	return nil, "", "", fmt.Errorf("the clipboard picture is %d KB and could not be shrunk enough to fit a message; crop it or use /image <path>", len(raw)/1024)
}

// onWhite flattens transparency over white: JPEG has no alpha, and black would hide text.
func onWhite(src image.Image) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Over)
	return dst
}

// resizeArea shrinks an image by averaging the source pixels under each target pixel,
// which keeps small text readable where dropping pixels would not.
func resizeArea(src *image.RGBA, w, h int) *image.RGBA {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		y0, y1 := y*sh/h, (y+1)*sh/h
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < w; x++ {
			x0, x1 := x*sw/w, (x+1)*sw/w
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, b, n uint32
			for yy := y0; yy < y1 && yy < sh; yy++ {
				for xx := x0; xx < x1 && xx < sw; xx++ {
					c := src.RGBAAt(xx, yy)
					r, g, b, n = r+uint32(c.R), g+uint32(c.G), b+uint32(c.B), n+1
				}
			}
			if n > 0 {
				dst.SetRGBA(x, y, color.RGBA{uint8(r / n), uint8(g / n), uint8(b / n), 255})
			}
		}
	}
	return dst
}

// pasteClipboardImage reads the clipboard's picture and attaches it to the line. It
// returns the marker to insert, a note to show (what was shrunk), and an error to show as
// a warning. A clipboard with no picture returns "", "", nil: the caller decides whether
// that deserves a word (an empty text paste does, a deliberate Ctrl+V does too).
func pasteClipboardImage(ctx context.Context, store *imageStore, input string) (mark, note string, err error) {
	raw, ok, err := clipboardImage(ctx, runtime.GOOS, lookPath)
	if err != nil || !ok {
		return "", "", err
	}
	data, ext, note, err := fitPicture(raw)
	if err != nil {
		return "", "", err
	}
	mark, err = store.attachBytes(input, "clipboard"+ext, data)
	if err != nil {
		return "", "", err
	}
	return mark, note, nil
}

// noClipboardPicture is the warning when a paste held no text and the clipboard no picture.
const noClipboardPicture = "nothing to paste: the clipboard holds neither text nor a picture arxi can read"
