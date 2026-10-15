package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/term"
)

func testBar() engine.ChatBar {
	return engine.ChatBar{Col: 99, Top: 2, Height: 10, ThumbTop: 6, ThumbLen: 4, Max: 60}
}

func TestBarGrab(t *testing.T) {
	b := testBar()
	cases := []struct {
		name     string
		col, row int
		grab     int
		ok       bool
	}{
		{"on the thumb keeps the offset inside it", 99, 2 + 7, 1, true},
		{"on the track takes the middle of the thumb", 99, 2 + 1, 2, true},
		{"first row of the thumb", 99, 2 + 6, 0, true},
		{"last row of the thumb", 99, 2 + 9, 3, true},
		{"the row just above the thumb is track, not thumb", 99, 2 + 5, 2, true},
		{"last row of the bar", 99, 2 + 9, 3, true},
		{"beside the bar is not a grab", 98, 2 + 7, 0, false},
		{"the column after the bar", 100, 2 + 7, 0, false},
		{"above the pane", 99, 1, 0, false},
		{"below the pane", 99, 12, 0, false},
	}
	for _, c := range cases {
		g, ok := barGrab(b, c.col, c.row)
		if g != c.grab || ok != c.ok {
			t.Errorf("%s: got (%d,%v), want (%d,%v)", c.name, g, ok, c.grab, c.ok)
		}
	}
	// A thumb in the middle: the rows either side of it are track (they take the middle of
	// the thumb), the thumb's own rows keep the offset.
	mid := engine.ChatBar{Col: 99, Top: 2, Height: 10, ThumbTop: 3, ThumbLen: 3, Max: 60}
	for _, c := range []struct {
		row, grab int
		what      string
	}{
		{2 + 2, 1, "just above the thumb"}, {2 + 3, 0, "first row of the thumb"},
		{2 + 5, 2, "last row of the thumb"}, {2 + 6, 1, "just under the thumb"},
	} {
		if g, ok := barGrab(mid, 99, c.row); !ok || g != c.grab {
			t.Errorf("%s: got (%d,%v), want (%d,true)", c.what, g, ok, c.grab)
		}
	}
	if _, ok := barGrab(mid, 99, 2+10); ok {
		t.Error("the row after the last one is not on the bar")
	}
	if _, ok := barGrab(engine.ChatBar{}, 0, 0); ok {
		t.Error("a frame with no bar must never be grabbed")
	}
}

func TestBarScrollEndsAndRoundTrip(t *testing.T) {
	b := testBar()
	if got := barScroll(b, b.Top, 0); got != b.Max {
		t.Errorf("thumb at the top = %d, want the oldest line %d", got, b.Max)
	}
	if got := barScroll(b, b.Top+b.Height-1, 0); got != 0 {
		t.Errorf("thumb dragged to the bottom = %d, want 0", got)
	}
	if got := barScroll(b, -50, 0); got != b.Max {
		t.Errorf("pointer far above = %d, want %d", got, b.Max)
	}
	if got := barScroll(b, 500, 0); got != 0 {
		t.Errorf("pointer far below = %d, want 0", got)
	}
	// One row past either end is pinned, not extrapolated beyond the chat.
	if got := barScroll(b, b.Top-1, 0); got != b.Max {
		t.Errorf("one row above the bar = %d, want %d", got, b.Max)
	}
	if got := barScroll(b, b.Top+b.Height-b.ThumbLen+1, 0); got != 0 {
		t.Errorf("thumb one row past the bottom = %d, want 0", got)
	}
	// The offset is rounded to the nearest thumb position (the renderer places the thumb
	// with the inverse): span 6, Max 61, thumb row 4 is 4*61/6 = 40.67 lines from the tail,
	// which is 41, so the chat sits 61-41 = 20 lines from the oldest.
	odd := b
	odd.Max = 61
	if got := barScroll(odd, odd.Top+4, 0); got != 20 {
		t.Errorf("thumb at row 4 of 6 with 61 lines = %d, want 20 (rounded, not cut)", got)
	}
	span := b.Height - b.ThumbLen
	for scroll := 0; scroll <= b.Max; scroll++ {
		bb := b
		bb.ThumbTop = (b.Max - scroll) * span / b.Max
		if got := barScroll(bb, b.Top+bb.ThumbTop+1, 1); got > scroll+b.Max/span || got < scroll-b.Max/span {
			t.Fatalf("offset %d: grab-and-hold gave %d (more than one thumb step away)", scroll, got)
		}
	}
}

func mouseEvent(a term.MouseAction, col, row int) term.Event {
	return term.Event{Kind: term.EventMouse, Mouse: term.Mouse{Action: a, Button: term.MouseLeft, Col: col, Row: row}}
}

func anyHint(frames []string) bool {
	for _, f := range frames {
		if strings.Contains(f, scrollHint) {
			return true
		}
	}
	return false
}

// Pressing the bar scrolls the chat; after the release, motion no longer follows the pointer.
func TestLoopDraggingTheScrollbarScrollsTheChat(t *testing.T) {
	keys := []scheduledEvent{
		{400 * time.Millisecond, mouseEvent(term.MousePress, 99, 3)},
		{200 * time.Millisecond, mouseEvent(term.MouseRelease, 99, 3)},
		{200 * time.Millisecond, mouseEvent(term.MouseDrag, 99, 15)}, // after release: ignored
	}
	frames := scrollLoop(t, append(keys, scrollQuit()...))
	if !anyHint(frames[:len(frames)-1]) {
		t.Fatal("pressing the scrollbar did not scroll the chat")
	}
	if !strings.Contains(strings.Join(frames, "\n"), "┃") {
		t.Error("no scrollbar thumb was ever drawn")
	}
	if !strings.Contains(frames[len(frames)-1], scrollHint) {
		t.Error("a drag after the button was released moved the chat")
	}
}

func TestLoopDragFollowsThePointerDownToTheTail(t *testing.T) {
	keys := []scheduledEvent{
		{400 * time.Millisecond, mouseEvent(term.MousePress, 99, 3)},
		{200 * time.Millisecond, mouseEvent(term.MouseDrag, 99, 16)},
		{200 * time.Millisecond, mouseEvent(term.MouseRelease, 99, 16)},
	}
	frames := scrollLoop(t, append(keys, scrollQuit()...))
	if !anyHint(frames) {
		t.Fatal("never scrolled up by the press")
	}
	if strings.Contains(frames[len(frames)-1], scrollHint) {
		t.Error("dragging the thumb to the bottom did not bring the chat back to its latest line")
	}
}

func TestPressOutsideTheBarDoesNothing(t *testing.T) {
	keys := []scheduledEvent{{400 * time.Millisecond, mouseEvent(term.MousePress, 10, 5)}}
	if anyHint(scrollLoop(t, append(keys, scrollQuit()...))) {
		t.Fatal("a click in the text scrolled the chat")
	}
}

// chat.scrollbar is the user's switch: "no" leaves the full width to the text and draws no bar,
// "yes" (the factory value) draws it while there is more transcript than window.
func TestTheScrollbarCanBeTurnedOffWithAText(t *testing.T) {
	barCells := func(frames []string) bool {
		for _, f := range frames {
			if strings.Contains(f, "│") { // the track; ┃ is also the input prefix
				return true
			}
		}
		return false
	}
	if !barCells(scrollLoop(t, scrollQuit())) {
		t.Fatal("the factory setting must draw the bar on a long chat")
	}
	// scrollLoop points the config at a fresh folder; the loop reads texts.json from it, so the
	// setting is written where the user's would be.
	t.Setenv(configDirEnv, t.TempDir())
	if err := os.WriteFile(filepath.Join(os.Getenv(configDirEnv), "texts.json"), []byte(`{"chat.scrollbar":"no"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if barCells(scrollLoopKeepingConfig(t, scrollQuit())) {
		t.Error("chat.scrollbar no still drew a bar")
	}
}

// A value that is neither yes nor no is refused when written, not silently read as "yes".
func TestTheScrollbarTextAcceptsOnlyYesOrNo(t *testing.T) {
	for _, ok := range []string{"yes", "no", "off", "On"} {
		if err := validChatLook("chat.scrollbar", ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	if validChatLook("chat.scrollbar", "maybe") == nil {
		t.Error("\"maybe\" must be refused")
	}
}
