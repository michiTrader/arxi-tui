package main

import (
	"testing"

	"github.com/michiTrader/arxi_tui/internal/term"
)

// keyFromBytes decodes what a terminal really sends, so these tests cover the
// whole path from the wire to the editor, not just a hand-built Key.
func keyFromBytes(t *testing.T, b string) term.Key {
	t.Helper()
	evs, _ := term.Decode([]byte(b))
	if len(evs) != 1 || evs[0].Kind != term.EventKey {
		t.Fatalf("%q decoded to %+v, want one key", b, evs)
	}
	return evs[0].Key
}

// Alt+Backspace (ESC DEL) deletes the word before the caret.
func TestAltBackspaceDeletesWordBackward(t *testing.T) {
	k := keyFromBytes(t, "\x1b\x7f")
	cases := []struct {
		name      string
		in        string
		caret     int
		want      string
		wantCaret int
	}{
		{"last word", "hello world", 11, "hello ", 6},
		{"trailing spaces go with the word", "hello world  ", 13, "hello ", 6},
		{"mid-line keeps the tail", "one two three", 7, "one  three", 4},
		{"first word", "hello", 5, "", 0},
		{"at start nothing happens", "hello", 0, "hello", 0},
		{"multibyte runes", "añb ñandú", 9, "añb ", 4},
	}
	for _, c := range cases {
		got, caret, ok := applyEdit(c.in, c.caret, k)
		if !ok || got != c.want || caret != c.wantCaret {
			t.Errorf("%s: applyEdit(%q,%d) = (%q,%d,%v), want (%q,%d,true)", c.name, c.in, c.caret, got, caret, ok, c.want, c.wantCaret)
		}
	}
}

// A plain Backspace still removes exactly one rune.
func TestPlainBackspaceStillDeletesOneRune(t *testing.T) {
	got, caret, _ := applyEdit("hello world", 11, keyFromBytes(t, "\x7f"))
	if got != "hello worl" || caret != 10 {
		t.Errorf("plain backspace = (%q,%d)", got, caret)
	}
}

// Ctrl+Left/Right and Alt+Left/Right, as the terminals spell them (CSI 1;5D,
// CSI 1;3D, and the ESC b / ESC f form macOS terminals send for Alt+arrows).
func TestWordMotionFromRealSequences(t *testing.T) {
	const line = "hello world foo"
	for _, seq := range []string{"\x1b[1;5D", "\x1b[1;3D", "\x1bb"} {
		_, c, ok := applyEdit(line, 15, keyFromBytes(t, seq))
		if !ok || c != 12 {
			t.Errorf("%q: caret from end = %d (ok=%v), want 12", seq, c, ok)
		}
	}
	for _, seq := range []string{"\x1b[1;5C", "\x1b[1;3C", "\x1bf"} {
		_, c, ok := applyEdit(line, 0, keyFromBytes(t, seq))
		if !ok || c != 5 {
			t.Errorf("%q: caret from start = %d (ok=%v), want 5", seq, c, ok)
		}
	}
}
