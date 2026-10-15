package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// pngBytes is a real, tiny png.
func pngBytes(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func writePic(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseImageCmd(t *testing.T) {
	for _, c := range []struct {
		line, path string
		matched    bool
	}{
		{"/image a.png", "a.png", true},
		{"/image   a b.png  ", "a b.png", true},
		{`/image "C:\My Pics\a.png"`, `C:\My Pics\a.png`, true},
		{"/image 'x y.png'", "x y.png", true},
		{"/image", "", true},
		{"/image ", "", true},
		{"/imagex a.png", "", false},
		{"image a.png", "", false},
		{"/send /image a.png", "", false},
		{"hello", "", false},
	} {
		path, matched := parseImageCmd(c.line)
		if path != c.path || matched != c.matched {
			t.Errorf("parseImageCmd(%q) = (%q, %v), want (%q, %v)", c.line, path, matched, c.path, c.matched)
		}
	}
}

func TestImagePathOnlyForAPictureThatExists(t *testing.T) {
	good := writePic(t, "shot.PNG", pngBytes(t))
	txt := writePic(t, "notes.txt", []byte("hi"))
	dir := filepath.Join(t.TempDir(), "folder.png")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		in string
		ok bool
	}{
		{good, true},
		{`"` + good + `"`, true},
		{"  " + good + "\n", true},
		{txt, false},
		{dir, false},
		{filepath.Join(t.TempDir(), "missing.png"), false},
		{good + "\nsecond line", false},
		{"", false},
		{"just words", false},
	} {
		if _, ok := imagePathOf(c.in); ok != c.ok {
			t.Errorf("imagePathOf(%q) ok = %v, want %v", c.in, ok, c.ok)
		}
	}
}

func TestAttachMakesAMarkerAndKeepsThePicture(t *testing.T) {
	raw := pngBytes(t)
	p := writePic(t, "shot.png", raw)
	var s imageStore
	mark, err := s.attach("", p)
	if err != nil {
		t.Fatal(err)
	}
	if mark != "[Image #1 shot.png]" {
		t.Errorf("marker = %q", mark)
	}
	got := s.used("look at " + mark + " please")
	if len(got) != 1 || got[0].MediaType != "image/png" || got[0].Data != base64.StdEncoding.EncodeToString(raw) {
		t.Fatalf("used = %+v", got)
	}
	if !s.has("x " + mark) {
		t.Error("has() missed the marker")
	}
	mark2, _ := s.attach("", p)
	if mark2 != "[Image #2 shot.png]" {
		t.Errorf("second marker = %q", mark2)
	}
}

func TestTheFormatComesFromTheBytesNotTheName(t *testing.T) {
	var s imageStore
	// a png called .jpg is still a png; a text file called .png is not a picture.
	mark, err := s.attach("", writePic(t, "really-a-png.jpg", pngBytes(t)))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.used(mark); len(got) != 1 || got[0].MediaType != "image/png" {
		t.Errorf("media type = %+v, want image/png", got)
	}
	if _, err := s.attach("", writePic(t, "fake.png", []byte("not a picture at all"))); err == nil || !strings.Contains(err.Error(), "not a png, jpeg, gif or webp") {
		t.Errorf("a text file named .png was accepted: %v", err)
	}
}

func TestUsedFollowsTheMarkersLeftInTheLineInOrder(t *testing.T) {
	var s imageStore
	p1 := writePic(t, "a.png", pngBytes(t))
	var b2 bytes.Buffer
	_ = png.Encode(&b2, image.NewRGBA(image.Rect(0, 0, 3, 3)))
	p2 := writePic(t, "b.png", b2.Bytes())
	m1, _ := s.attach("", p1)
	m2, _ := s.attach(m1, p2)
	// reversed in the line: order is the line's, not the attach order
	got := s.used(m2 + " vs " + m1)
	if len(got) != 2 || got[0].Data != base64.StdEncoding.EncodeToString(b2.Bytes()) {
		t.Errorf("pictures are not in the order of the markers: %+v", got)
	}
	// a marker the user deleted takes its picture with it
	if got := s.used("only " + m1); len(got) != 1 {
		t.Errorf("a deleted marker still sent its picture: %d", len(got))
	}
	if got := s.used("no markers"); len(got) != 0 {
		t.Errorf("no marker, yet %d picture(s) sent", len(got))
	}
}

func TestAttachRefusesWhatTheCoreWouldRefuse(t *testing.T) {
	p := writePic(t, "a.png", pngBytes(t))
	var s imageStore
	line := ""
	for i := 0; i < imageMaxCount; i++ {
		m, err := s.attach(line, p)
		if err != nil {
			t.Fatalf("picture %d refused: %v", i+1, err)
		}
		line += m + " "
	}
	if _, err := s.attach(line, p); err == nil || !strings.Contains(err.Error(), "at most 4 pictures") {
		t.Errorf("a fifth picture was accepted: %v", err)
	}
	// a marker removed from the line frees its place
	if _, err := s.attach("", p); err != nil {
		t.Errorf("room freed by deleting markers was not given back: %v", err)
	}

	// one file too big for the line
	big := writePic(t, "big.png", append(pngBytes(t), bytes.Repeat([]byte{0}, imageMaxFileBytes)...))
	if _, err := (&imageStore{}).attach("", big); err == nil || !strings.Contains(err.Error(), "Shrink it first") {
		// the per-file check answers before the file is read into memory; the total check
		// would only catch it after
		t.Errorf("an over-size picture was not refused by size alone: %v", err)
	}
	// several that are fine alone but too much together
	half := append(pngBytes(t), bytes.Repeat([]byte{0}, imageMaxFileBytes*6/10)...)
	var s2 imageStore
	m, err := s2.attach("", writePic(t, "h1.png", half))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s2.attach(m, writePic(t, "h2.png", half)); err == nil || !strings.Contains(err.Error(), "together") {
		t.Errorf("two pictures over the total were accepted: %v", err)
	}
	// missing file and folder
	if _, err := (&imageStore{}).attach("", filepath.Join(t.TempDir(), "nope.png")); err == nil || !strings.Contains(err.Error(), "cannot read") {
		t.Errorf("missing file: %v", err)
	}
	if _, err := (&imageStore{}).attach("", t.TempDir()); err == nil || !strings.Contains(err.Error(), "folder") {
		t.Errorf("a folder: %v", err)
	}
}

func TestAMarkerNameCannotBreakTheMarker(t *testing.T) {
	var s imageStore
	m, err := s.attach("", writePic(t, "we[ir]d.png", pngBytes(t)))
	if err != nil {
		t.Fatal(err)
	}
	if m != "[Image #1 we_ir_d.png]" {
		t.Errorf("marker = %q", m)
	}
}

func TestBackspaceTakesAMarkerWholeAndResetStartsOver(t *testing.T) {
	var s imageStore
	m, _ := s.attach("", writePic(t, "a.png", pngBytes(t)))
	line := "see " + m
	next, caret, ok := s.dropWhole(line, len([]rune(line)), term.Key{Type: term.KeyBackspace})
	if !ok || next != "see " || caret != 4 {
		t.Errorf("backspace after the marker: %q %d %v", next, caret, ok)
	}
	if _, _, ok := s.dropWhole(line, 2, term.Key{Type: term.KeyBackspace}); ok {
		t.Error("backspace elsewhere must not take the marker")
	}
	s.reset()
	if s.has(line) || len(s.used(line)) != 0 {
		t.Error("reset kept the pictures")
	}
	m2, _ := s.attach("", writePic(t, "a.png", pngBytes(t)))
	if m2 != "[Image #1 a.png]" {
		t.Errorf("after reset the next marker is %q, want #1", m2)
	}
}

type attachDrv struct {
	Driver
	got [][]driver.ChatImage
	ok  bool
}

func (d *attachDrv) AttachImages(i []driver.ChatImage) bool { d.got = append(d.got, i); return d.ok }

func TestAttachPicturesWarnsOnlyWhenPicturesCannotTravel(t *testing.T) {
	img := []driver.ChatImage{{MediaType: "image/png", Data: "AA=="}}
	if w := attachPictures(&attachDrv{ok: true}, img); w != "" {
		t.Errorf("a session that carries pictures warned: %q", w)
	}
	if w := attachPictures(&attachDrv{ok: false}, img); w != imagesNotCarried {
		t.Errorf("warning = %q", w)
	}
	if w := attachPictures(&attachDrv{ok: false}, nil); w != "" {
		t.Errorf("nothing to carry, yet warned: %q", w)
	}
	if w := attachPictures(&testDriver{}, img); w != "" {
		t.Errorf("testDriver carries pictures: %q", w)
	}
	if w := attachPictures(&mockDriver{}, img); w != imagesNotCarried {
		t.Errorf("a driver that cannot carry pictures said %q", w)
	}
	if w := attachPictures(&mockDriver{}, nil); w != "" {
		t.Errorf("no pictures, yet warned: %q", w)
	}
	d := &attachDrv{ok: true}
	attachPictures(d, nil)
	if len(d.got) != 1 || d.got[0] != nil {
		t.Error("with no pictures the driver must still be told, so an earlier set is cleared")
	}
}

func TestImageIsInTheMenuAndFillsItsLine(t *testing.T) {
	found := false
	for _, c := range fold.Commands {
		found = found || c.Name == "image"
	}
	if !found {
		t.Fatal("/image is not in fold.Commands: the menu never lists it")
	}
	if classifySlash("/image a.png", nil).Kind != slashKnown {
		t.Error("/image is not a known command: it would be refused as unknown")
	}
}

// ---- through the real loop ----

func imageLoop(t *testing.T, script []scheduledEvent) (string, *testDriver) {
	t.Helper()
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatal(err)
	}
	tty := newFakeTTY(100, 24, append(script, exitKeys()...))
	drv := &testDriver{evCh: make(chan fold.Event, 64), seq: 1000}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	return stripANSI(tty.output()), drv
}

func TestLoopAPastedPicturePathBecomesAMarkerAndTravelsWithTheLine(t *testing.T) {
	p := writePic(t, "shot.png", pngBytes(t))
	var script []scheduledEvent
	script = append(script, typed("what is ")...)
	script = append(script, at(10, pasteEvent(p)))
	script = append(script, typed("this?")...)
	script = append(script, at(100, enterEvent()))
	frames, d := imageLoop(t, script)
	if !strings.Contains(frames, "[Image #1 shot.png]") {
		t.Error("the bar never showed the marker")
	}
	if len(d.submitted) != 1 || !strings.Contains(d.submitted[0], "[Image #1 shot.png]") || !strings.Contains(d.submitted[0], "what is") {
		t.Fatalf("submitted %q", d.submitted)
	}
	if len(d.attached) == 0 || len(d.attached[len(d.attached)-1]) != 1 || d.attached[len(d.attached)-1][0].MediaType != "image/png" {
		t.Fatalf("the picture did not go with the line: %+v", d.attached)
	}
}

func TestLoopTheImageCommandAttachesAndDoesNotReachTheModel(t *testing.T) {
	p := writePic(t, "cmd.png", pngBytes(t))
	var script []scheduledEvent
	script = append(script, typed("/image "+p)...)
	script = append(script, at(100, enterEvent()))
	script = append(script, typed("describe it")...)
	script = append(script, at(100, enterEvent()))
	_, d := imageLoop(t, script)
	if len(d.submitted) != 1 || strings.Contains(d.submitted[0], p) || !strings.Contains(d.submitted[0], "[Image #1 cmd.png]") || !strings.Contains(d.submitted[0], "describe it") {
		t.Fatalf("submitted %q: the command line itself must not be sent, its marker must be", d.submitted)
	}
	if last := d.attached[len(d.attached)-1]; len(last) != 1 {
		t.Fatalf("pictures with the message: %d, want 1", len(last))
	}
}

func TestLoopABareImageCommandWarnsAndSendsNothing(t *testing.T) {
	script := append(typed("/image"), at(100, enterEvent()))
	frames, d := imageLoop(t, script)
	if len(d.submitted) != 0 {
		t.Errorf("a bare /image sent %q", d.submitted)
	}
	if !strings.Contains(frames, "/image needs the path of a picture") {
		t.Errorf("no usage warning; frames:\n%s", frames)
	}
}

func TestLoopABadPictureIsSaidAndNothingIsAttached(t *testing.T) {
	bad := writePic(t, "fake.png", []byte("nope"))
	script := append(typed("/image "+bad), at(100, enterEvent()))
	frames, d := imageLoop(t, script)
	if len(d.submitted) != 0 {
		t.Errorf("submitted %q", d.submitted)
	}
	if !strings.Contains(frames, "is not a png, jpeg, gif or webp") {
		t.Errorf("the refusal was not shown:\n%s", frames)
	}
}

func TestLoopPicturesDoNotStickToTheNextMessage(t *testing.T) {
	p := writePic(t, "once.png", pngBytes(t))
	var script []scheduledEvent
	script = append(script, at(10, pasteEvent(p)))
	script = append(script, typed("look")...)
	script = append(script, at(100, enterEvent()))
	script = append(script, typed("and now words only")...)
	script = append(script, at(100, enterEvent()))
	_, d := imageLoop(t, script)
	if len(d.attached) < 2 {
		t.Fatalf("attached calls = %d", len(d.attached))
	}
	if len(d.attached[0]) != 1 || len(d.attached[len(d.attached)-1]) != 0 {
		t.Errorf("first line carried %d, second %d; want 1 then 0", len(d.attached[0]), len(d.attached[len(d.attached)-1]))
	}
	if strings.Contains(d.submitted[1], "[Image") {
		t.Errorf("second message holds a marker: %q", d.submitted[1])
	}
}

// The numbering starts over with every message sent, like the paste markers.
func TestLoopTheNextMessageNumbersItsPicturesFromOneAgain(t *testing.T) {
	p := writePic(t, "n.png", pngBytes(t))
	var script []scheduledEvent
	for i := 0; i < 2; i++ {
		script = append(script, typed("go ")...)
		script = append(script, at(10, pasteEvent(p)))
		script = append(script, at(100, enterEvent()))
	}
	_, d := imageLoop(t, script)
	if len(d.submitted) != 2 || !strings.Contains(d.submitted[1], "[Image #1 n.png]") {
		t.Errorf("submitted %q; the second message's picture should be #1 again", d.submitted)
	}
}

func TestLoopPickingImageFromTheMenuFillsTheLineForAPath(t *testing.T) {
	p := writePic(t, "menu.png", pngBytes(t))
	var script []scheduledEvent
	script = append(script, typed("/imag")...)
	script = append(script, at(50, enterEvent())) // the menu pick: fills "/image ", runs nothing
	script = append(script, typed(p)...)
	script = append(script, at(100, enterEvent()))
	script = append(script, typed("what")...)
	script = append(script, at(100, enterEvent()))
	frames, d := imageLoop(t, script)
	if strings.Contains(frames, "/image needs the path") {
		t.Error("picking /image from the menu ran it bare instead of waiting for the path")
	}
	if len(d.submitted) != 1 || !strings.Contains(d.submitted[0], "[Image #1 menu.png]") {
		t.Errorf("submitted %q", d.submitted)
	}
}

func TestTheServeDriverHandsPicturesToItsChat(t *testing.T) {
	core := &picChat{}
	sd := &serveDriver{chat: newChatSession(core, make(chan fold.Event, 16))}
	if !sd.AttachImages([]driver.ChatImage{{MediaType: "image/png", Data: "AA=="}}) {
		t.Fatal("a session with a chat refused pictures")
	}
	if err := sd.chat.send(context.Background(), "look"); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return !sd.chat.busyNow() && len(core.got) == 1 })
	if len(core.got[0].Images) != 1 {
		t.Errorf("the chat got %d pictures, want 1", len(core.got[0].Images))
	}
	none := &serveDriver{}
	if none.AttachImages([]driver.ChatImage{{MediaType: "image/png", Data: "AA=="}}) {
		t.Error("a session with no chat said it carries pictures")
	}
	if !none.AttachImages(nil) {
		t.Error("clearing pictures on a session with no chat must be fine")
	}
}

func TestLoopDeletingTheMarkerDropsThePicture(t *testing.T) {
	p := writePic(t, "gone.png", pngBytes(t))
	var script []scheduledEvent
	script = append(script, at(10, pasteEvent(p)))
	script = append(script, at(20, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyBackspace}}))
	script = append(script, at(20, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyBackspace}}))
	script = append(script, typed("plain")...)
	script = append(script, at(100, enterEvent()))
	_, d := imageLoop(t, script)
	if len(d.submitted) != 1 || strings.Contains(d.submitted[0], "[Image") {
		t.Fatalf("submitted %q", d.submitted)
	}
	if last := d.attached[len(d.attached)-1]; len(last) != 0 {
		t.Errorf("a deleted marker still sent %d picture(s)", len(last))
	}
}

func TestLoopAnOrdinaryPastedPathStaysText(t *testing.T) {
	p := writePic(t, "notes.txt", []byte("x"))
	// words first: a line that starts with "/" is a command, not text
	script := append(typed("see "), at(10, pasteEvent(p)), at(100, enterEvent()))
	_, d := imageLoop(t, script)
	if len(d.submitted) != 1 || d.submitted[0] != "see "+p {
		t.Errorf("submitted %q, want the path as text", d.submitted)
	}
}

// ---- the chat ----

type picChat struct {
	got []driver.ChatSendParams
	err error
}

func (*picChat) Hello() *driver.Hello { return &driver.Hello{Implemented: []string{"chat.send"}} }
func (c *picChat) SubmitChatSend(_ context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error) {
	c.got = append(c.got, p)
	if c.err != nil && len(p.Images) > 0 {
		return nil, c.err
	}
	return &driver.ChatSendResult{Text: "ok"}, nil
}

func TestChatSendsThePicturesOnceAndClearsThem(t *testing.T) {
	core := &picChat{}
	c := newChatSession(core, make(chan fold.Event, 64))
	imgs := []driver.ChatImage{{MediaType: "image/png", Data: "AA=="}}
	c.AttachImages(imgs)
	if err := c.send(context.Background(), "look"); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return !c.busyNow() })
	if err := c.send(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return !c.busyNow() && len(core.got) == 2 })
	if len(core.got[0].Images) != 1 || core.got[0].Images[0].Data != "AA==" {
		t.Errorf("first call images = %+v", core.got[0].Images)
	}
	if len(core.got[1].Images) != 0 {
		t.Errorf("the pictures were sent again: %+v", core.got[1].Images)
	}
}

func TestChatPicturesOfARefusedSendDoNotWaitForTheNextOne(t *testing.T) {
	core := &picChat{}
	c := newChatSession(core, make(chan fold.Event, 64))
	c.AttachImages([]driver.ChatImage{{MediaType: "image/png", Data: "AA=="}})
	if err := c.send(context.Background(), "   "); err != nil { // empty text: nothing is sent
		t.Fatal(err)
	}
	if err := c.send(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return !c.busyNow() && len(core.got) == 1 })
	if len(core.got[0].Images) != 0 {
		t.Errorf("pictures of an empty send leaked into the next message: %+v", core.got[0].Images)
	}
}

func TestChatAnOldCoreGetsTheWordsWithoutThePictures(t *testing.T) {
	core := &picChat{err: driver.ErrImagesUnsupported}
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	c.AttachImages([]driver.ChatImage{{MediaType: "image/png", Data: "AA=="}})
	if err := c.send(context.Background(), "look"); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return !c.busyNow() && len(core.got) == 2 })
	if len(core.got[1].Images) != 0 || core.got[1].Prompt != "look" {
		t.Errorf("the retry = %+v", core.got[1])
	}
	warned := false
	for len(out) > 0 {
		ev := <-out
		if s, _ := ev.Payload["text"].(string); ev.Type == "chat.warn" && strings.Contains(s, "cannot take pictures") {
			warned = true
		}
	}
	if !warned {
		t.Error("the user was not told the pictures did not go")
	}
}

func TestChatErrorTextPointsAtTheModelMenu(t *testing.T) {
	err := errors.New("model openai/gpt-x is not set to take images; set it with: arxi model update openai/gpt-x --images")
	got := chatErrorText(err)
	if !strings.Contains(got, "open /model") || !strings.Contains(got, "Accepts images") || strings.Contains(got, "arxi model update") {
		t.Errorf("text = %q", got)
	}
	if plain := chatErrorText(errors.New("boom")); !strings.Contains(plain, "boom") || strings.Contains(plain, "/model") {
		t.Errorf("an unrelated error was rewritten: %q", plain)
	}
}
