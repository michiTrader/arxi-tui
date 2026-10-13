package main

import (
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
		{"beside the bar is not a grab", 98, 2 + 7, 0, false},
		{"above the pane", 99, 1, 0, false},
		{"below the pane", 99, 12, 0, false},
	}
	for _, c := range cases {
		g, ok := barGrab(b, c.col, c.row)
		if g != c.grab || ok != c.ok {
			t.Errorf("%s: got (%d,%v), want (%d,%v)", c.name, g, ok, c.grab, c.ok)
		}
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
