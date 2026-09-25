package main

import (
	"testing"

	"github.com/michiTrader/arxi_tui/internal/term"
)

// Ctrl+Arrow (and Alt+Arrow) jump the caret a word at a time. The decoder already
// reports the arrow with its modifier (CSI 1;5D etc.); the gap was in applyEdit,
// which moved one rune regardless. These pin the word jump and that a plain arrow
// still moves a single rune.

func caretLeft(input string, caret int, mod term.Mod) int {
	_, c, ok := applyEdit(input, caret, term.Key{Type: term.KeyLeft, Mod: mod})
	if !ok {
		panic("applyEdit did not handle KeyLeft")
	}
	return c
}

func caretRight(input string, caret int, mod term.Mod) int {
	_, c, ok := applyEdit(input, caret, term.Key{Type: term.KeyRight, Mod: mod})
	if !ok {
		panic("applyEdit did not handle KeyRight")
	}
	return c
}

// TestCtrlArrowJumpsByWord walks a three-word line with Ctrl+Left/Right and
// checks each stop lands on a word boundary, not one rune over. Counterfactual: a
// single-rune move (the old behaviour, i.e. ignoring the modifier) lands one away
// from every one of these and fails.
func TestCtrlArrowJumpsByWord(t *testing.T) {
	// "hello world foo": indices h0..o4, space5, w6..d10, space11, f12..o14, len 15.
	const line = "hello world foo"

	// Ctrl+Left from the end walks back over the word starts.
	if got := caretLeft(line, 15, term.ModCtrl); got != 12 {
		t.Errorf("Ctrl+Left from end went to %d, want 12 (start of \"foo\")", got)
	}
	if got := caretLeft(line, 12, term.ModCtrl); got != 6 {
		t.Errorf("Ctrl+Left from 12 went to %d, want 6 (start of \"world\")", got)
	}
	if got := caretLeft(line, 6, term.ModCtrl); got != 0 {
		t.Errorf("Ctrl+Left from 6 went to %d, want 0 (start of \"hello\")", got)
	}

	// Ctrl+Right from the start walks forward over the word ends.
	if got := caretRight(line, 0, term.ModCtrl); got != 5 {
		t.Errorf("Ctrl+Right from 0 went to %d, want 5 (end of \"hello\")", got)
	}
	if got := caretRight(line, 5, term.ModCtrl); got != 11 {
		t.Errorf("Ctrl+Right from 5 went to %d, want 11 (end of \"world\")", got)
	}
	if got := caretRight(line, 11, term.ModCtrl); got != 15 {
		t.Errorf("Ctrl+Right from 11 went to %d, want 15 (end of \"foo\")", got)
	}
}

// TestAltArrowJumpsByWordToo checks the Alt modifier drives the same jump, so a
// terminal that sends Alt+Arrow for word motion behaves like one that sends Ctrl.
func TestAltArrowJumpsByWordToo(t *testing.T) {
	const line = "alpha beta"
	if got := caretRight(line, 0, term.ModAlt); got != 5 {
		t.Errorf("Alt+Right from 0 went to %d, want 5 (end of \"alpha\")", got)
	}
	if got := caretLeft(line, 10, term.ModAlt); got != 6 {
		t.Errorf("Alt+Left from end went to %d, want 6 (start of \"beta\")", got)
	}
}

// TestWordJumpCrossesNewline checks the jump treats a literal newline as a word
// boundary, so Ctrl+Arrow walks across the lines of a multi-line prompt.
func TestWordJumpCrossesNewline(t *testing.T) {
	const text = "ab\ncd" // a0 b1 \n2 c3 d4
	if got := caretRight(text, 0, term.ModCtrl); got != 2 {
		t.Errorf("Ctrl+Right from 0 over %q went to %d, want 2 (end of first line)", text, got)
	}
	if got := caretRight(text, 2, term.ModCtrl); got != 5 {
		t.Errorf("Ctrl+Right from the line break went to %d, want 5 (end of second line)", got)
	}
	if got := caretLeft(text, 5, term.ModCtrl); got != 3 {
		t.Errorf("Ctrl+Left from end went to %d, want 3 (start of \"cd\")", got)
	}
}

// TestPlainArrowStillMovesOneRune guards that adding the word jump did not change
// an unmodified arrow: it must still step a single rune.
func TestPlainArrowStillMovesOneRune(t *testing.T) {
	const line = "abcdef"
	if got := caretRight(line, 2, 0); got != 3 {
		t.Errorf("plain Right from 2 went to %d, want 3 (one rune)", got)
	}
	if got := caretLeft(line, 2, 0); got != 1 {
		t.Errorf("plain Left from 2 went to %d, want 1 (one rune)", got)
	}
}
