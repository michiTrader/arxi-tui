package main

import (
	"context"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// A newline gesture inserts a literal '\n' at the caret; plain Enter submits. The
// decoder proves the chords arrive distinctly (decode_test.go: shift+enter,
// ctrl+enter, ctrl+j); these pin that typeKey then routes them to a newline and
// keeps plain Enter on the submit path.

func newlineDriver() *testDriver { return &testDriver{evCh: make(chan fold.Event, 8)} }

// TestNewlineGesturesInsertNewline checks Shift+Enter, Ctrl+Enter and Ctrl+J each
// insert a newline at the caret without submitting.
func TestNewlineGesturesInsertNewline(t *testing.T) {
	gestures := []struct {
		name string
		key  term.Key
	}{
		{"shift+enter", term.Key{Type: term.KeyEnter, Mod: term.ModShift}},
		{"ctrl+enter", term.Key{Type: term.KeyEnter, Mod: term.ModCtrl}},
		{"ctrl+j", term.Key{Type: term.KeyRunes, Runes: []rune{'j'}, Mod: term.ModCtrl}},
	}
	for _, g := range gestures {
		drv := newlineDriver()
		// Caret at the end of "line one" (8 runes); the gesture appends a newline.
		out, caret := typeKey("line one", 8, g.key, context.Background(), drv)
		if out != "line one\n" {
			t.Errorf("%s over \"line one\" gave %q, want \"line one\\n\"; the gesture must insert a newline, not submit or drop", g.name, out)
		}
		if caret != 9 {
			t.Errorf("%s left the caret at %d, want 9 (just past the inserted newline)", g.name, caret)
		}
		if len(drv.submitted) != 0 {
			t.Errorf("%s submitted %q; a newline gesture must never submit", g.name, drv.submitted)
		}
	}
}

// TestNewlineGestureInsertsMidLine checks the newline lands at the caret, not the
// end: splitting a line is the point.
func TestNewlineGestureInsertsMidLine(t *testing.T) {
	drv := newlineDriver()
	out, caret := typeKey("abcd", 2, term.Key{Type: term.KeyEnter, Mod: term.ModShift}, context.Background(), drv)
	if out != "ab\ncd" {
		t.Errorf("Shift+Enter at caret 2 of \"abcd\" gave %q, want \"ab\\ncd\"", out)
	}
	if caret != 3 {
		t.Errorf("caret after the mid-line newline is %d, want 3", caret)
	}
}

// TestPlainEnterStillSubmits guards that adding the newline gestures did not
// disturb plain Enter: it submits the whole buffer (newlines and all) and clears
// the line.
func TestPlainEnterStillSubmits(t *testing.T) {
	drv := newlineDriver()
	out, caret := typeKey("first\nsecond", 12, term.Key{Type: term.KeyEnter}, context.Background(), drv)
	if out != "" || caret != 0 {
		t.Errorf("plain Enter left the buffer as %q caret %d, want cleared to \"\" 0", out, caret)
	}
	if len(drv.submitted) != 1 || drv.submitted[0] != "first\nsecond" {
		t.Errorf("plain Enter submitted %q, want one prompt \"first\\nsecond\"; a multi-line buffer submits whole", drv.submitted)
	}
}
