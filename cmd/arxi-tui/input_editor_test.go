package main

import (
	"testing"

	"github.com/michiTrader/arxi_tui/internal/term"
)

// The line editor is the reason the input can be edited in the middle and not
// only at the tail, so its motion and splice behaviour is pinned here. Each
// case names the consequence a regression would have; the counterfactual for
// the whole feature is that reverting applyEdit to append/backspace-only (the
// prior behaviour) fails every case below except the plain append.
func TestApplyEditMovesCaretAndSplicesMidString(t *testing.T) {
	rune1 := func(r rune) term.Key { return term.Key{Type: term.KeyRunes, Runes: []rune{r}} }

	cases := []struct {
		name      string
		input     string
		caret     int
		key       term.Key
		wantText  string
		wantCaret int
		wantOK    bool
	}{
		{"left moves caret back without touching text", "abc", 3, term.Key{Type: term.KeyLeft}, "abc", 2, true},
		{"left clamps at start", "abc", 0, term.Key{Type: term.KeyLeft}, "abc", 0, true},
		{"right moves caret forward", "abc", 1, term.Key{Type: term.KeyRight}, "abc", 2, true},
		{"right clamps at end", "abc", 3, term.Key{Type: term.KeyRight}, "abc", 3, true},
		{"home jumps to start", "abc", 2, term.Key{Type: term.KeyHome}, "abc", 0, true},
		{"end jumps past last rune", "abc", 0, term.Key{Type: term.KeyEnd}, "abc", 3, true},
		{"insert splices at the caret, not the tail", "ac", 1, rune1('b'), "abc", 2, true},
		{"backspace deletes the rune before the caret", "abc", 2, term.Key{Type: term.KeyBackspace}, "ac", 1, true},
		{"backspace at start is a no-op", "abc", 0, term.Key{Type: term.KeyBackspace}, "abc", 0, true},
		{"delete removes the rune under the caret", "abc", 1, term.Key{Type: term.KeyDelete}, "ac", 1, true},
		{"delete at end is a no-op", "abc", 3, term.Key{Type: term.KeyDelete}, "abc", 3, true},
		// A multi-byte glyph must be spliced as one rune, not one byte: a
		// byte-indexed editor would cut "café" between the accent bytes and
		// corrupt the line. The caret is a rune index throughout.
		{"backspace deletes a whole multi-byte rune", "café", 4, term.Key{Type: term.KeyBackspace}, "caf", 3, true},
		{"insert before a multi-byte rune keeps it intact", "café", 3, rune1('X'), "cafXé", 4, true},
		// Enter is not the editor's key; the loop owns submission. ok=false lets
		// the caller keep its own Enter handling.
		{"enter is not handled here", "abc", 1, term.Key{Type: term.KeyEnter}, "abc", 1, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotText, gotCaret, gotOK := applyEdit(c.input, c.caret, c.key)
			if gotText != c.wantText || gotCaret != c.wantCaret || gotOK != c.wantOK {
				t.Fatalf("applyEdit(%q, %d, %v) = (%q, %d, %v); want (%q, %d, %v)\n"+
					"Consequence: the input line cannot be edited as specified — a wrong caret leaves the cursor off the glyph being changed, and a wrong splice corrupts or drops text.",
					c.input, c.caret, c.key.Type, gotText, gotCaret, gotOK, c.wantText, c.wantCaret, c.wantOK)
			}
		})
	}
}
