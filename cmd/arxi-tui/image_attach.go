package main

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// A picture is attached to the line being typed as a marker, "[Image #1 shot.png]", the way a
// big paste is. The marker stays in the text that is sent (so the user can write "compare
// #1 with #2" and the model reads the same words), and the pictures it stands for travel
// with that one message. They are not sent again with later messages: the history keeps the
// words, not the bytes.
//
// The limits are the core's (core/cmd/arxi/chat_images.go): a request line is capped at 1 MiB
// and a longer one closes the connection, so a message carries at most four pictures and
// about 600 KB of them once encoded. They are checked here too so the user hears it when
// attaching, not when sending.
const (
	imageMaxCount   = 4
	imageMaxEncoded = 600 * 1024
	// imageMaxFileBytes is the biggest file whose base64 form fits imageMaxEncoded alone.
	imageMaxFileBytes = imageMaxEncoded / 4 * 3
)

// imageUsage is the local warning a bare `/image` gets.
const imageUsage = "/image needs the path of a picture: nothing was attached. Type /image followed by a png, jpeg, gif or webp file, or paste the path."

type attachedImage struct {
	name string
	img  driver.ChatImage
}

// imageStore holds the pictures behind the markers of the current line. It belongs to the
// loop, like the paste store, and empties when the line is sent.
type imageStore struct {
	next   int
	byMark map[string]attachedImage
	marks  pasteStore // marker -> marker, so Backspace/Delete take a marker whole
}

// parseImageCmd recognises `/image <path>`. matched is true for the bare command too, which
// the host answers with imageUsage.
func parseImageCmd(line string) (path string, matched bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), "/image")
	if !ok {
		return "", false
	}
	if rest == "" {
		return "", true
	}
	if r := []rune(rest)[0]; !unicode.IsSpace(r) {
		return "", false
	}
	return unquotePath(rest), true
}

// unquotePath trims what a terminal adds when a file is dropped on it: spaces, and quotes
// around a path that has spaces in it.
func unquotePath(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		s = s[1 : len(s)-1]
	}
	return strings.TrimSpace(s)
}

var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}

// imagePathOf says whether pasted text is just the path of a picture file that exists: that is
// what dropping a file on the terminal pastes. Anything else stays an ordinary paste.
func imagePathOf(pasted string) (string, bool) {
	p := unquotePath(pasted)
	if p == "" || strings.ContainsAny(p, "\r\n") || !imageExts[strings.ToLower(filepath.Ext(p))] {
		return "", false
	}
	if st, err := os.Stat(p); err != nil || st.IsDir() {
		return "", false
	}
	return p, true
}

// safeName is a file name fit to sit inside a marker.
func safeName(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '[' || r == ']' {
			return '_'
		}
		return r
	}, s)
}

// attach reads a picture and returns the marker that stands for it. input is the line as it
// is now: the limits count the pictures it still holds. The format is read from the bytes,
// never from the file's extension.
func (s *imageStore) attach(input, path string) (string, error) {
	have := s.used(input)
	if len(have) >= imageMaxCount {
		return "", fmt.Errorf("a message takes at most %d pictures; nothing was attached", imageMaxCount)
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %v", filepath.Base(path), unwrapPathErr(err))
	}
	if st.IsDir() {
		return "", fmt.Errorf("%s is a folder, not a picture", filepath.Base(path))
	}
	if st.Size() > imageMaxFileBytes {
		return "", fmt.Errorf("%s is %d KB; a picture can be at most %d KB (a longer request closes the connection). Shrink it first", filepath.Base(path), st.Size()/1024, imageMaxFileBytes/1024)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %v", filepath.Base(path), unwrapPathErr(err))
	}
	mt := http.DetectContentType(raw)
	okType := false
	for _, t := range []string{"image/png", "image/jpeg", "image/gif", "image/webp"} {
		okType = okType || mt == t
	}
	if !okType {
		return "", fmt.Errorf("%s is not a png, jpeg, gif or webp picture; nothing was attached", filepath.Base(path))
	}
	enc := base64.StdEncoding.EncodeToString(raw)
	total := len(enc)
	for _, h := range have {
		total += len(h.Data)
	}
	if total > imageMaxEncoded {
		return "", fmt.Errorf("the pictures would weigh %d KB together; a message takes at most %d KB. Shrink them or send fewer", total/1024, imageMaxEncoded/1024)
	}
	if s.byMark == nil {
		s.byMark = map[string]attachedImage{}
		s.marks.text = map[string]string{}
	}
	if s.next < 1 {
		s.next = 1
	}
	mark := fmt.Sprintf("[Image #%d %s]", s.next, safeName(filepath.Base(path)))
	s.next++
	s.byMark[mark] = attachedImage{name: filepath.Base(path), img: driver.ChatImage{MediaType: mt, Data: enc}}
	s.marks.text[mark] = mark
	return mark, nil
}

func unwrapPathErr(err error) error {
	if pe, ok := err.(*os.PathError); ok {
		return pe.Err
	}
	return err
}

// has reports whether the line holds a marker of a picture.
func (s *imageStore) has(input string) bool { return s.marks.has(input) }

// used returns the pictures whose markers are still in the line, in the order they appear.
// A marker the user deleted takes its picture with it.
func (s *imageStore) used(input string) []driver.ChatImage {
	type hit struct {
		at  int
		img driver.ChatImage
	}
	var hits []hit
	for mark, a := range s.byMark {
		if at := strings.Index(input, mark); at >= 0 {
			hits = append(hits, hit{at, a.img})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].at < hits[j].at })
	out := make([]driver.ChatImage, len(hits))
	for i, h := range hits {
		out[i] = h.img
	}
	return out
}

// dropWhole makes Backspace/Delete take a marker as one character.
func (s *imageStore) dropWhole(input string, caret int, k term.Key) (string, int, bool) {
	return s.marks.dropWhole(input, caret, k)
}

// reset forgets every picture: the line was sent, the next picture is #1 again.
func (s *imageStore) reset() { s.next, s.byMark, s.marks = 0, nil, pasteStore{} }

// imageAttacher is what a driver offers when it can send pictures with the next message.
// AttachImages(nil) clears them. It reports false when this session cannot carry pictures.
type imageAttacher interface {
	AttachImages(imgs []driver.ChatImage) bool
}

// imagesNotCarried is the warning when pictures are attached but the session cannot send them.
const imagesNotCarried = "This session cannot send pictures, so the message went without them. Pictures only travel in the chat."

// attachPictures hands the pictures of the line being sent to the driver. It returns a warning
// when there are pictures and this session cannot carry them ("" otherwise). With none it
// clears whatever an earlier line left behind.
func attachPictures(drv Driver, imgs []driver.ChatImage) string {
	a, ok := drv.(imageAttacher)
	if !ok {
		if len(imgs) > 0 {
			return imagesNotCarried
		}
		return ""
	}
	if !a.AttachImages(imgs) && len(imgs) > 0 {
		return imagesNotCarried
	}
	return ""
}
