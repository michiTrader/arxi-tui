package main

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

func withPhone(t *testing.T, arrows, guard bool) {
	t.Helper()
	pa, pg := scrollArrows, flickerGuard
	scrollArrows, flickerGuard = arrows, guard
	t.Cleanup(func() { scrollArrows, flickerGuard = pa, pg })
}

// TestTheTermuxArrangement is the tripwire for the set of legs that only hold together:
// no mouse tracking, plain arrows scroll, history on ctrl+p/ctrl+n, one row per notch.
// Changing one without the others leaves a phone with no keyboard or no scroll.
func TestTheTermuxArrangement(t *testing.T) {
	yes, no := true, false
	if wantMouse(true, nil) {
		t.Errorf("Termux must not claim the mouse by default: a tracked tap never raises the keyboard again")
	}
	if !wantMouse(false, nil) {
		t.Errorf("every other terminal claims the mouse by default")
	}
	if !wantMouse(true, &yes) || wantMouse(false, &no) {
		t.Errorf("an explicit -mouse must win in both directions")
	}
	if !phoneArrows(true, false) || phoneArrows(true, true) || phoneArrows(false, false) {
		t.Errorf("plain arrows scroll only on Termux without a mouse")
	}

	withPhone(t, true, true)
	if phoneScroll(term.Key{Type: term.KeyUp}) != 1 || phoneScroll(term.Key{Type: term.KeyDown}) != -1 {
		t.Errorf("plain up/down must scroll the chat on the phone arrangement")
	}
	if phoneScroll(term.Key{Type: term.KeyUp, Mod: term.ModCtrl}) != 0 {
		t.Errorf("a modified arrow is not a swipe")
	}
	if historyStep(term.Key{Type: term.KeyRunes, Mod: term.ModCtrl, Runes: []rune{'p'}}) != -1 ||
		historyStep(term.Key{Type: term.KeyRunes, Mod: term.ModCtrl, Runes: []rune{'n'}}) != 1 {
		t.Errorf("the history must stay reachable on ctrl+p / ctrl+n")
	}
	if phoneWheelStep() != 1 {
		t.Errorf("a phone notch is one row")
	}
	withPhone(t, false, false)
	if phoneScroll(term.Key{Type: term.KeyUp}) != 0 || phoneWheelStep() != 3 {
		t.Errorf("off the phone the arrows belong to the history and a notch is three rows")
	}
}

func TestPhoneFrameHidesTheCaretWhileItPaints(t *testing.T) {
	withPhone(t, true, true)
	th := theme.SOBRIA()
	f := ui.Frame{Live: []ui.Line{{ui.Span{Text: "one"}}, {ui.Span{Text: "two"}}}, Cursor: ui.Cursor{Line: 1, Col: 2}}
	st := &emitState{}
	emitFrame(&strings.Builder{}, f, th, 24, st, true)
	if !st.shown {
		t.Fatalf("an on-phase frame ends with the caret shown")
	}
	var out strings.Builder
	f.Live[0] = ui.Line{ui.Span{Text: "uno"}}
	emitFrame(&out, f, th, 24, st, true)
	s := out.String()
	hide, show, paint := strings.Index(s, "\x1b[?25l"), strings.LastIndex(s, "\x1b[?25h"), strings.Index(s, "uno")
	if hide < 0 || show < 0 || !(hide < paint && paint < show) {
		t.Errorf("the caret must be hidden before the rows paint and shown after them: hide=%d paint=%d show=%d\n%q", hide, paint, show, s)
	}
}

func TestPhoneFrameSendsOnlyTheRowsThatChanged(t *testing.T) {
	withPhone(t, true, true)
	th := theme.SOBRIA()
	mk := func(a, b string) ui.Frame {
		return ui.Frame{Live: []ui.Line{{ui.Span{Text: a}}, {ui.Span{Text: b}}}, Cursor: ui.Cursor{Hidden: true}}
	}
	st := &emitState{}
	emitFrame(&strings.Builder{}, mk("steady", "tick 1"), th, 24, st, true)
	var out strings.Builder
	emitFrame(&out, mk("steady", "tick 2"), th, 24, st, true)
	s := out.String()
	if strings.Contains(s, "steady") || !strings.Contains(s, "tick 2") {
		t.Errorf("only the changed row may be repainted: %q", s)
	}
	st.invalidate()
	out.Reset()
	emitFrame(&out, mk("steady", "tick 2"), th, 24, st, true)
	if !strings.Contains(out.String(), "steady") {
		t.Errorf("after invalidate (a resize) every row is painted again: %q", out.String())
	}
}

func TestDesktopFrameStillPaintsEveryRow(t *testing.T) {
	withPhone(t, false, false)
	th := theme.SOBRIA()
	f := ui.Frame{Live: []ui.Line{{ui.Span{Text: "steady"}}}, Cursor: ui.Cursor{Hidden: true}}
	st := &emitState{}
	emitFrame(&strings.Builder{}, f, th, 24, st, true)
	var out strings.Builder
	emitFrame(&out, f, th, 24, st, true)
	if !strings.Contains(out.String(), "steady") {
		t.Errorf("off the phone every frame paints every row")
	}
}
