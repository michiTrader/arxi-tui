package ui

import (
	"strings"
	"testing"
)

// Wrapping had no tests of its own until a newline got through it. Every check on it
// was a check on a whole frame, and a frame check cannot see a control character: it
// scores zero columns, so the width rule reads it as fitting, and it is invisible in
// a golden file. The bug it caused was three layers away — an open reasoning part
// ended its delta with "\n", the byte reached the terminal, the cursor dropped a row
// the emitter had not counted, and the next frame's walk up landed one row low and
// left a stale copy of the live region on screen.

func texts(lines []Line) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, l.Text())
	}
	return out
}

// TestWrapSpansBreaksOnNewlines states the rule as a table, because each row is a
// decision and not one of them is obvious. The trailing-newline row is the one that
// matters most in practice: every streaming part ends with one, so the alternative
// reading grows a phantom blank row under every paragraph in the transcript.
func TestWrapSpansBreaksOnNewlines(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want []string
	}{
		{"a newline breaks the line", "one\ntwo", []string{"one", "two"}},
		{"a trailing newline is a terminator", "one\n", []string{"one"}},
		{"so are several", "one\n\n\n", []string{"one"}},
		{"a blank line between paragraphs is kept", "one\n\ntwo", []string{"one", "", "two"}},
		{"a space before a break is not ink", "one \ntwo", []string{"one", "two"}},
		{"an indent after a break is the text's own", "one\n   two", []string{"one", "   two"}},
		{"so is one at the very start", "   one", []string{"   one"}},
		{"a tab is expanded, not passed through", "a\tb", []string{"a   b"}},
		{"a stray control byte is dropped", "a\x07b\rc", []string{"abc"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := texts(WrapText(tc.text, "text", 40, nil))
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("WrapText(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

// TestAHardBreakUnderAHangingIndentLeavesNoPadding is why the break trims. marked
// draws a marker and indents every line under it, so a paragraph break inside a
// notice or a reasoning part lands on a line whose only content is that indent — and
// a row made of spaces is exactly the trailing blank that fills the last cell of a
// row and talks the terminal into wrapping one we thought we owned. The indent still
// has to come back for the text after the break, which is the other half of the row.
func TestAHardBreakUnderAHangingIndentLeavesNoPadding(t *testing.T) {
	cont := Line{{Text: "  ", Style: "text"}}
	got := texts(WrapText("one\n\ntwo", "text", 40, cont))
	if len(got) != 3 {
		t.Fatalf("got %d lines, want 3: %q", len(got), got)
	}
	if got[1] != "" {
		t.Errorf("the blank line between paragraphs is %q, want empty", got[1])
	}
	if got[2] != "  two" {
		t.Errorf("the line after the break is %q, want the indent kept", got[2])
	}
}

// TestOnlyTheTextsOwnBreakKeepsItsIndent is the two halves of the space rule in one
// wrap, and they pull in opposite directions. A space starting a line the *width*
// broke is the break itself, and drawing it would leave a row of padding. A space
// starting a line a *newline* broke is indentation somebody typed — the four spaces
// under a `--- FAIL:` header are the only thing saying the line below belongs to it,
// and every tool result in the transcript was losing them.
func TestOnlyTheTextsOwnBreakKeepsItsIndent(t *testing.T) {
	got := texts(WrapText("--- FAIL: TestX\n    text_test.go:96: want 3", "text", 24, nil))
	want := []string{"--- FAIL: TestX", "    text_test.go:96:", "want 3"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestAnIndentWiderThanTheRoomIsDropped is the cost of keeping indentation, paid
// where it would be too expensive. Held back, an indent that leaves no room for a
// word fills the row on its own, the word behind it is truncated to nothing, and
// nothing fits ever again — so a wrapper that kept it would silently delete a word
// to preserve some spaces, which is the wrong way round.
func TestAnIndentWiderThanTheRoomIsDropped(t *testing.T) {
	got := texts(WrapText("one\n      two", "text", 6, nil))
	want := []string{"one", "two"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestASpaceAtTheEdgeIsABreakAndNotADeletion is the other side of that rule, and it
// was wrong: the give-way applied to every space and not only to an indent, so a
// space landing exactly on the last column was deleted, and the short word behind it
// then fit on the row the space had just left — "rather than a warning" came out as
// "rather thana warning". Nothing we had could see it. The row does not overflow and
// does not end in a blank, so both corpus sweeps pass; the width sweep compares the
// render to the source with the spaces taken out, so to that check the two are the
// same string. It took reading a real recording at 24 columns.
func TestASpaceAtTheEdgeIsABreakAndNotADeletion(t *testing.T) {
	for _, tc := range []struct {
		name  string
		text  string
		width int
		want  []string
	}{
		// The break is at the width, and the word after it is narrower than the space
		// that was holding it back — the one shape where deleting the space wins the
		// word a row it has no business being on.
		{"a one-column word behind the edge", "ab cd e", 6, []string{"ab cd", "e"}},
		{"the same with one word in front", "abcde f", 6, []string{"abcde", "f"}},
		// A run of spaces is one token, so the run has to break the line whole rather
		// than partly survive into it.
		{"a run of them behind the edge", "abcd  e", 6, []string{"abcd", "e"}},
		// A space the row still has room for is ink like anything else, which is what
		// says the fix did not simply move the break one column left.
		{"a space that fits is kept", "ab c", 4, []string{"ab c"}},
		{"and so is the word behind it", "abc de", 6, []string{"abc de"}},
		// An indent is still the thing that gives way, and it gives way whole: this is
		// TestAnIndentWiderThanTheRoomIsDropped's rule under a width the new one has
		// to leave alone.
		{"an indent with no room still goes", "a\n     b", 5, []string{"a", "b"}},
		{"an indent that fits its word stays", "a\n   b", 5, []string{"a", "   b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := texts(WrapText(tc.text, "text", tc.width, nil))
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("WrapText(%q, %d) = %q, want %q", tc.text, tc.width, got, tc.want)
			}
		})
	}
}

// TestWrappingLosesNoWord is the check that was missing, stated over the width every
// caller can be given rather than over the one where the bug happened to show. Any
// row a wrapper produces may lose spaces — that is what wrapping is — but the words
// on either side of one it drops have to stay two words.
func TestWrappingLosesNoWord(t *testing.T) {
	const text = "The loader is strict now, so a malformed line is an error rather " +
		"than a warning, and Load reports every problem in a file at once."
	want := strings.Fields(text)
	// The sweep starts at the longest word rather than at 1: a width narrower than a
	// word splits it, and a split word is two fields neither of which the author
	// wrote, so only the widths that can hold every word whole can be asked for the
	// words back.
	longest := 0
	for _, w := range want {
		if len(w) > longest {
			longest = len(w)
		}
	}
	for width := longest; width <= 80; width++ {
		got := strings.Fields(strings.Join(texts(WrapText(text, "text", width, nil)), " "))
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("at width %d the words are %q, want %q", width, got, want)
		}
	}
}
