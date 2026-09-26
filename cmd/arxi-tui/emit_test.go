package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/theme"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

// epochAt is a deterministic wall time d after the Unix epoch, so blinkOn's
// UnixMilli-based phase is fixed and reproducible in the test.
func epochAt(d time.Duration) time.Time {
	return time.UnixMilli(0).Add(d)
}

// caretCUP is the escape emitFrame writes to park the terminal caret at a
// (line, col). Building it the same way emitFrame does keeps the test honest
// about the exact bytes it is asserting on.
func caretCUP(line, col int) string {
	return fmt.Sprintf("\033[%d;%dH", line+1, col+1)
}

// The caret blink is host-driven, not the terminal's own: the cursor shape is
// steady (DECSCUSR 2) and emitFrame shows or hides the block according to the
// blink phase it is handed. Two earlier fixes kept the native blink and tried to
// stop the emit path from disturbing it — first not re-showing, then not
// re-positioning the caret — and both failed on an animated scene, because the
// ~12×/s repaint walks the cursor down every painted row and the terminal
// restarts its blink on any cursor motion. Owning the blink is what makes it one
// clean on/off square wave on the idle and animated scene alike.
//
// So the visibility escape must follow the phase: on -> shown, off -> hidden, and
// only on a transition (the block does not flicker within a phase). This is the
// blink itself; getting it wrong is the strobe.
func TestEmitFrameBlinksTheCaretWithThePhase(t *testing.T) {
	th := theme.SOBRIA()
	f := ui.Frame{Live: []ui.Line{{ui.Span{Text: "hi"}}}, Cursor: ui.Cursor{Line: 0, Col: 5}}
	st := &emitState{}

	// Phase on, from not-yet-shown: the caret must be shown.
	var on1 strings.Builder
	emitFrame(&on1, f, th, 24, st, true)
	if !strings.Contains(on1.String(), "\x1b[?25h") {
		t.Fatalf("the on phase must show the caret; it did not.\ngot: %q", on1.String())
	}
	if !st.shown {
		t.Fatalf("the on phase must leave the caret marked shown")
	}

	// Phase still on: no visibility escape at all — re-showing every frame is the
	// fast strobe the earlier fixes chased.
	var on2 strings.Builder
	emitFrame(&on2, f, th, 24, st, true)
	if strings.Contains(on2.String(), "\x1b[?25h") || strings.Contains(on2.String(), "\x1b[?25l") {
		t.Errorf("a second on-phase frame toggled the caret's visibility; the block must hold\n"+
			"steady within a phase, or it strobes.\ngot: %q", on2.String())
	}

	// Phase off: the caret must be hidden, once.
	var off1 strings.Builder
	emitFrame(&off1, f, th, 24, st, false)
	if !strings.Contains(off1.String(), "\x1b[?25l") {
		t.Errorf("the off phase must hide the caret; it did not.\ngot: %q", off1.String())
	}
	if st.shown {
		t.Errorf("the off phase must leave the caret marked not shown")
	}

	// Phase still off: no escape.
	var off2 strings.Builder
	emitFrame(&off2, f, th, 24, st, false)
	if strings.Contains(off2.String(), "\x1b[?25h") || strings.Contains(off2.String(), "\x1b[?25l") {
		t.Errorf("a second off-phase frame toggled visibility; the block must stay dark within\n"+
			"the phase.\ngot: %q", off2.String())
	}
}

// The steady cursor is repositioned every frame — that is safe precisely because
// the shape does not blink, so a CUP restarts nothing. This is the invariant the
// two failed fixes lacked: with a native blink they had to avoid the CUP, and
// could not, because the paint issues one per row regardless.
func TestEmitFramePositionsTheCaretEveryOnFrame(t *testing.T) {
	th := theme.SOBRIA()
	// Distinct column so the caret CUP is not the row-0 paint CUP (\033[1;1H).
	first := ui.Frame{Live: []ui.Line{{ui.Span{Text: "scrolling one"}}}, Cursor: ui.Cursor{Line: 0, Col: 5}}
	second := ui.Frame{Live: []ui.Line{{ui.Span{Text: "scrolling two"}}}, Cursor: ui.Cursor{Line: 0, Col: 5}}
	st := &emitState{}

	emitFrame(&strings.Builder{}, first, th, 24, st, true)

	// A later on-phase frame (an animation advanced the row) must still park the
	// caret at its column: an unpositioned steady cursor would sit wherever the
	// row paint left it.
	var two strings.Builder
	emitFrame(&two, second, th, 24, st, true)
	if !strings.Contains(two.String(), caretCUP(0, 5)) {
		t.Errorf("a repainted frame did not reposition the caret; the steady block would drift\n"+
			"to the end of the last painted row.\ngot: %q", two.String())
	}
}

// A frame that hosts no caret hides it regardless of the blink phase, and never
// re-hides once hidden. Returning to a caret-hosting frame on an on phase shows
// it again — the visibility tracks both the caret's presence and the phase.
func TestEmitFrameHidesAnAbsentCaret(t *testing.T) {
	th := theme.SOBRIA()
	visible := ui.Frame{Live: []ui.Line{{ui.Span{Text: "hi"}}}, Cursor: ui.Cursor{Line: 0, Col: 0}}
	hidden := ui.Frame{Live: []ui.Line{{ui.Span{Text: "hi"}}}, Cursor: ui.Cursor{Hidden: true}}
	st := &emitState{}

	emitFrame(&strings.Builder{}, visible, th, 24, st, true) // shown
	if !st.shown {
		t.Fatalf("a visible on-phase frame must leave the caret marked shown")
	}

	// Absent caret, even on an on phase: hide it.
	var hide strings.Builder
	emitFrame(&hide, hidden, th, 24, st, true)
	if !strings.Contains(hide.String(), "\x1b[?25l") {
		t.Errorf("a caret-less frame must hide the caret even on the on phase; it did not.\ngot: %q", hide.String())
	}
	if st.shown {
		t.Fatalf("a hidden frame must leave the caret marked not shown")
	}

	// Back to a caret on an on phase: show it again.
	var reshow strings.Builder
	emitFrame(&reshow, visible, th, 24, st, true)
	if !strings.Contains(reshow.String(), "\x1b[?25h") {
		t.Errorf("returning to a caret-hosting on-phase frame must show it again; it did not.\ngot: %q", reshow.String())
	}
}

// blinkOn is a pure function of wall time: it is true for one half-period and
// false for the next, so any two repaints landing in the same half agree on the
// phase without a shared counter. This is what lets a keystroke, an animation
// tick and the blink ticker all paint a consistent caret.
func TestBlinkOnSquareWave(t *testing.T) {
	base := blinkHalfPeriod
	// Two instants inside the same half-period agree.
	if blinkOn(epochAt(0)) != blinkOn(epochAt(base/2)) {
		t.Errorf("two instants in the same blink half-period disagreed on the phase")
	}
	// Crossing into the next half-period flips the phase.
	if blinkOn(epochAt(0)) == blinkOn(epochAt(base+base/2)) {
		t.Errorf("the phase did not flip across the blink half-period boundary")
	}
	// Two full periods later, the phase matches again.
	if blinkOn(epochAt(0)) != blinkOn(epochAt(2*base)) {
		t.Errorf("the phase did not repeat after a full blink period")
	}
}
