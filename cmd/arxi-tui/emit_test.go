package main

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/theme"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

// The emit path must not restart the terminal's caret blink on every frame. An
// animated scene repaints ~12×/s; the old emit hid the caret at the top of each
// frame and showed it again at the bottom, so on those 12 frames a second the
// terminal kept re-arming its blink from zero — the fast, ugly strobe reported
// on the demo, in place of the caret's normal gentle blink. cursorShown makes
// the show/hide escape fire only on a visibility change, so an unchanged visible
// caret is repositioned but never re-shown.
//
// Counterfactual, run rather than argued: restoring the per-frame hide/show
// (an unconditional "\x1b[?25l" at the top and "\x1b[?25h" at the bottom) puts
// both escapes in the second frame and fails both checks below.
func TestEmitFrameDoesNotRestartTheBlinkEachFrame(t *testing.T) {
	f := ui.Frame{Live: []ui.Line{{ui.Span{Text: "hi"}}}, Cursor: ui.Cursor{Line: 0, Col: 0}}
	th := theme.SOBRIA()
	shown := false

	var first strings.Builder
	emitFrame(&first, f, th, 24, &shown)
	if !strings.Contains(first.String(), "\x1b[?25h") {
		t.Fatalf("the first frame hosting a caret must show it once; it did not.\ngot: %q", first.String())
	}

	var second strings.Builder
	emitFrame(&second, f, th, 24, &shown)
	if strings.Contains(second.String(), "\x1b[?25h") {
		t.Errorf("a second visible frame re-sent the show-caret escape; on an animated scene this\n"+
			"restarts the terminal blink every frame — the reported strobe.\ngot: %q", second.String())
	}
	if strings.Contains(second.String(), "\x1b[?25l") {
		t.Errorf("a visible frame hid the caret mid-paint; the per-frame hide/show is the strobe.\ngot: %q", second.String())
	}
	// The caret is still repositioned every frame — only the visibility escape is
	// suppressed, not the CUP that keeps the caret on the input line.
	if !strings.Contains(second.String(), "\x1b[1;1H") {
		t.Errorf("the caret was not repositioned on the second frame; suppressing the blink restart\n"+
			"must not also drop the CUP that parks it.\ngot: %q", second.String())
	}
}

// A frame that hosts no caret hides it, once, and a later visible frame shows it
// again — the visibility escape tracks the transition, not the frame count.
func TestEmitFrameTogglesCaretOnlyOnChange(t *testing.T) {
	th := theme.SOBRIA()
	visible := ui.Frame{Live: []ui.Line{{ui.Span{Text: "hi"}}}, Cursor: ui.Cursor{Line: 0, Col: 0}}
	hidden := ui.Frame{Live: []ui.Line{{ui.Span{Text: "hi"}}}, Cursor: ui.Cursor{Hidden: true}}
	shown := false

	var b strings.Builder
	emitFrame(&b, visible, th, 24, &shown) // false -> true: show
	if !shown {
		t.Fatalf("a visible frame must leave the caret marked shown")
	}

	var hide strings.Builder
	emitFrame(&hide, hidden, th, 24, &shown) // true -> false: hide once
	if !strings.Contains(hide.String(), "\x1b[?25l") {
		t.Errorf("a caret-less frame must hide the caret; it did not.\ngot: %q", hide.String())
	}
	if shown {
		t.Fatalf("a hidden frame must leave the caret marked not shown")
	}

	var reshow strings.Builder
	emitFrame(&reshow, visible, th, 24, &shown) // false -> true: show again
	if !strings.Contains(reshow.String(), "\x1b[?25h") {
		t.Errorf("returning to a caret-hosting frame must show the caret again; it did not.\ngot: %q", reshow.String())
	}
}
