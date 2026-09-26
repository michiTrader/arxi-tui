package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/theme"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

// caretCUP is the escape emitFrame writes to park the terminal caret at a
// (line, col). Building it the same way emitFrame does keeps the test honest
// about the exact bytes it is asserting on.
func caretCUP(line, col int) string {
	return fmt.Sprintf("\033[%d;%dH", line+1, col+1)
}

// The emit path must not touch the caret on a frame where the caret has not
// moved. An animated scene repaints ~12×/s; two escapes used to fire on every
// one of those frames and each is its own strobe. Re-showing (?25h) restarts the
// blink cadence — the first, fast strobe. Re-positioning (CUP) is read by the
// terminal as "the app moved the caret, light it solid", so at 12×/s the block
// sat lit ~85% of the time with only a ~15% dip — the second, slower strobe the
// demo showed once the first was fixed. An unchanged caret must therefore cost
// neither escape; the caret is instead returned home by the DECRC that brackets
// the paint.
//
// Counterfactual, run rather than argued: replacing the moved-guard with an
// unconditional CUP (the previous behaviour) puts the caret CUP back in the
// second frame and fails the position check below; dropping the DECSC/DECRC
// bracket leaves the caret stranded on the last painted row.
func TestEmitFrameLeavesAnUnchangedCaretAlone(t *testing.T) {
	th := theme.SOBRIA()
	// A caret past column 0 so its CUP is distinct from the row-0 paint CUP
	// (\033[1;1H), which every frame emits to draw the first row.
	caret := ui.Cursor{Line: 0, Col: 5}
	first := ui.Frame{Live: []ui.Line{{ui.Span{Text: "scrolling text one"}}}, Cursor: caret}
	// A different first row stands in for an animated repaint (a marquee that
	// advanced) while the caret itself did not move.
	second := ui.Frame{Live: []ui.Line{{ui.Span{Text: "scrolling text two"}}}, Cursor: caret}
	st := &emitState{}

	var one strings.Builder
	emitFrame(&one, first, th, 24, st)
	if !strings.Contains(one.String(), "\x1b[?25h") {
		t.Fatalf("the first frame hosting a caret must show it once; it did not.\ngot: %q", one.String())
	}
	if !strings.Contains(one.String(), caretCUP(0, 5)) {
		t.Fatalf("the first frame must position the caret; it did not.\ngot: %q", one.String())
	}

	var two strings.Builder
	emitFrame(&two, second, th, 24, st)
	if strings.Contains(two.String(), caretCUP(0, 5)) {
		t.Errorf("a second frame with an unchanged caret re-sent its position CUP; at 12×/s the\n"+
			"terminal keeps the block lit solid instead of blinking — the ~85%%-on strobe.\ngot: %q", two.String())
	}
	if strings.Contains(two.String(), "\x1b[?25h") {
		t.Errorf("a second visible frame re-sent the show-caret escape; that restarts the blink\n"+
			"cadence every frame — the fast strobe.\ngot: %q", two.String())
	}
	if strings.Contains(two.String(), "\x1b[?25l") {
		t.Errorf("a visible frame hid the caret mid-paint; the per-frame hide/show is a strobe.\ngot: %q", two.String())
	}
	// The bracket that makes the above safe: without DECRC the paint would leave
	// the cursor on the last row it drew, not on the caret.
	if !strings.Contains(two.String(), "\0338") {
		t.Errorf("the paint was not bracketed by DECRC; an unchanged caret with no CUP would be\n"+
			"stranded on the last painted row.\ngot: %q", two.String())
	}
}

// When the caret genuinely moves — a keystroke, an arrow — the emit must
// reposition it. This is the one moment the terminal's solid-on flash is wanted,
// and it is exactly what the unchanged-caret path above must not trigger.
func TestEmitFrameRepositionsAMovedCaret(t *testing.T) {
	th := theme.SOBRIA()
	rows := []ui.Line{{ui.Span{Text: "hello there"}}}
	first := ui.Frame{Live: rows, Cursor: ui.Cursor{Line: 0, Col: 5}}
	second := ui.Frame{Live: rows, Cursor: ui.Cursor{Line: 0, Col: 8}}
	st := &emitState{}

	var one strings.Builder
	emitFrame(&one, first, th, 24, st)

	var two strings.Builder
	emitFrame(&two, second, th, 24, st)
	if !strings.Contains(two.String(), caretCUP(0, 8)) {
		t.Errorf("a moved caret was not repositioned; the typist would watch the block sit on the\n"+
			"old column while their edits land elsewhere.\ngot: %q", two.String())
	}
}

// A frame that hosts no caret hides it, once, and a later visible frame shows it
// again — the visibility escape tracks the transition, not the frame count.
func TestEmitFrameTogglesCaretOnlyOnChange(t *testing.T) {
	th := theme.SOBRIA()
	visible := ui.Frame{Live: []ui.Line{{ui.Span{Text: "hi"}}}, Cursor: ui.Cursor{Line: 0, Col: 0}}
	hidden := ui.Frame{Live: []ui.Line{{ui.Span{Text: "hi"}}}, Cursor: ui.Cursor{Hidden: true}}
	st := &emitState{}

	var b strings.Builder
	emitFrame(&b, visible, th, 24, st) // false -> true: show
	if !st.shown {
		t.Fatalf("a visible frame must leave the caret marked shown")
	}

	var hide strings.Builder
	emitFrame(&hide, hidden, th, 24, st) // true -> false: hide once
	if !strings.Contains(hide.String(), "\x1b[?25l") {
		t.Errorf("a caret-less frame must hide the caret; it did not.\ngot: %q", hide.String())
	}
	if st.shown {
		t.Fatalf("a hidden frame must leave the caret marked not shown")
	}

	var reshow strings.Builder
	emitFrame(&reshow, visible, th, 24, st) // false -> true: show again, and reposition
	if !strings.Contains(reshow.String(), "\x1b[?25h") {
		t.Errorf("returning to a caret-hosting frame must show the caret again; it did not.\ngot: %q", reshow.String())
	}
	if !strings.Contains(reshow.String(), caretCUP(0, 0)) {
		t.Errorf("returning from a hidden caret must re-issue the position CUP; the caret's\n"+
			"whereabouts were forgotten while it was hidden.\ngot: %q", reshow.String())
	}
}
