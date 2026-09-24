package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// A prompt whose text is wider than the pane must wrap onto more rows, and the
// caret must land on a row that exists. The reported bug: a single-line input
// ran the caret column off the right edge, the terminal clamped it into the
// bottom-right corner, and the arrow keys could then move it nowhere. These
// tests pin the wrap and the caret mapping that fix it.

// TestInputWrapsWhenWiderThanPane checks that a line longer than the room the
// pane leaves it produces more than one visual row, and that none of those rows
// overflows the width — an overflowing input row is exactly what pushed the
// caret past the edge.
func TestInputWrapsWhenWiderThanPane(t *testing.T) {
	// room = width - prefix. With width 12 and the two-column "┃ " prefix the
	// text has 10 columns per row, so 25 characters must occupy three rows.
	prefixRaw := json.RawMessage(`"┃ "`)
	node := &scene.Node{Type: "input", Bind: "user.input", PrefixRaw: prefixRaw}
	r := Renderer{Width: 12, Height: 24}

	text := strings.Repeat("x", 25)
	f := r.renderInput(node, fold.State{UserInput: text, UserInputCaret: len([]rune(text))})

	if len(f.Live) < 3 {
		t.Fatalf("25 chars in a 10-column body wrapped to %d rows, want at least 3; the input is not wrapping and the caret will run off the edge", len(f.Live))
	}
	for i, l := range f.Live {
		if w := l.Width(); w > r.Width {
			t.Errorf("wrapped input row %d is %d columns wide, wider than the %d-column pane; a row that overflows corrupts the caret column", i, w, r.Width)
		}
	}
}

// TestMultilineCaretIsOnAPaintedRow is the direct guard for the reported bug: on
// a wrapped input the caret must sit on a row the frame actually painted and at a
// column within the pane, never clamped into the corner. It walks the caret to
// the end of a three-row line and to a point in the middle.
func TestMultilineCaretIsOnAPaintedRow(t *testing.T) {
	prefixRaw := json.RawMessage(`"┃ "`)
	node := &scene.Node{Type: "input", Bind: "user.input", PrefixRaw: prefixRaw}
	r := Renderer{Width: 12, Height: 24} // 10-column body after the prefix

	text := strings.Repeat("x", 25)
	runes := len([]rune(text))

	for _, caret := range []int{0, 5, 12, 20, runes} {
		f := r.renderInput(node, fold.State{UserInput: text, UserInputCaret: caret})
		if f.Cursor.Hidden {
			t.Fatalf("caret %d: cursor is hidden on an input with text; the user cannot see where they are editing", caret)
		}
		if f.Cursor.Line < 0 || f.Cursor.Line >= len(f.Live) {
			t.Errorf("caret %d: reported on row %d, but the frame painted rows 0..%d; a caret off the painted rows is the bottom-right-corner clamp", caret, f.Cursor.Line, len(f.Live)-1)
		}
		if f.Cursor.Col < 0 || f.Cursor.Col > r.Width {
			t.Errorf("caret %d: reported at column %d, outside the %d-column pane; the terminal would clamp it into the corner", caret, f.Cursor.Col, r.Width)
		}
	}
}

// TestMultilineCaretMovesRowWithText checks the caret actually changes row as it
// crosses a wrap boundary: index 0 is on the first row, and an index past the
// first row's worth of characters is on a later row. Counterfactual: a
// single-line renderer reports row 0 for every caret and fails the second half.
func TestMultilineCaretMovesRowWithText(t *testing.T) {
	prefixRaw := json.RawMessage(`"┃ "`)
	node := &scene.Node{Type: "input", Bind: "user.input", PrefixRaw: prefixRaw}
	r := Renderer{Width: 12, Height: 24} // 10-column body

	text := strings.Repeat("x", 25)

	head := r.renderInput(node, fold.State{UserInput: text, UserInputCaret: 3})
	if head.Cursor.Line != 0 {
		t.Errorf("caret at index 3 is on row %d, want 0 (still on the first wrapped row)", head.Cursor.Line)
	}
	// Column 3 of the body, past the two-column prefix.
	if head.Cursor.Col != 5 {
		t.Errorf("caret at index 3 is at column %d, want 5 (prefix 2 + three glyphs)", head.Cursor.Col)
	}

	tail := r.renderInput(node, fold.State{UserInput: text, UserInputCaret: 22})
	if tail.Cursor.Line == 0 {
		t.Errorf("caret at index 22 is still on row 0; it must descend to a later wrapped row as the text does")
	}
}

// TestInputCaretVerticalMove pins the up/down keys the host binds on a wrapped
// input: down moves the caret one wrapped row lower keeping the column, up
// returns it, and a vertical key at the edge is a no-op rather than a jump. With
// a 10-column body, index 3 sits on row 0 column 3; one row down is column 3 of
// row 1, which is rune index 13.
func TestInputCaretVerticalMove(t *testing.T) {
	room := 10
	text := strings.Repeat("x", 25) // three wrapped rows: 0-9, 10-19, 20-24

	down := InputCaretVerticalMove(text, 3, room, +1)
	if down != 13 {
		t.Errorf("down from index 3 (row 0 col 3) went to %d, want 13 (row 1 col 3); the column must be kept across the row", down)
	}

	up := InputCaretVerticalMove(text, down, room, -1)
	if up != 3 {
		t.Errorf("up from index 13 went to %d, want 3; up must undo the down it mirrors", up)
	}

	// Up from the first row has nowhere to go: the caret stays, it does not
	// collapse to the start of the line.
	if got := InputCaretVerticalMove(text, 4, room, -1); got != 4 {
		t.Errorf("up from row 0 moved the caret to %d, want 4 (unchanged); a vertical key at the top edge is a no-op", got)
	}

	// Down from the last row likewise stays put rather than jumping to the end.
	last := len([]rune(text)) // index 25, on the final row
	if got := InputCaretVerticalMove(text, last, room, +1); got != last {
		t.Errorf("down from the last row moved the caret to %d, want %d (unchanged)", got, last)
	}
}
