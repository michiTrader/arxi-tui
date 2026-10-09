package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// A box around the input with "border":"round" draws the rounded corners, and the
// default and the other shapes are untouched. This is the frame a user asked for in
// words ("╭ instead of ┌─") and never got.
func TestRoundBorderDrawsRoundedCornersAroundTheInput(t *testing.T) {
	cases := map[string][4]string{
		"round":  {"╭", "╮", "╰", "╯"},
		"single": {"┌", "┐", "└", "┘"},
		"heavy":  {"┏", "┓", "┗", "┛"},
		"double": {"╔", "╗", "╚", "╝"},
	}
	for shape, corners := range cases {
		d := menuDoc(t, `{"type":"stack","children":[{"type":"box","border":"`+shape+`","children":[{"type":"input","bind":"user.input","prefix":"┃ "}]}]}`)
		f := render(t, d, fold.State{}, 30)
		plain := f.Plain()
		for i, c := range corners {
			if !strings.Contains(plain, c) {
				t.Errorf("border %q: corner %d should be %q.\n%s", shape, i, c, plain)
			}
		}
		if shape == "round" && strings.ContainsAny(plain, "┌┐└┘") {
			t.Errorf("a round frame must have no square corner:\n%s", plain)
		}
	}
}

// The round frame is accepted and the words people reach for instead are refused with the
// remedy, instead of drawing a square frame and saying nothing.
func TestBorderShapesAreClosedAndTheRefusalNamesTheRemedy(t *testing.T) {
	ok := `{"root":{"type":"stack","children":[{"type":"box","border":"round","children":[{"type":"text","text":"x"}]}]}}`
	if d, err := scene.ParseDocument([]byte(ok)); err != nil || d.Validate() != nil {
		t.Fatalf("round must load: %v", err)
	}
	for _, shape := range []string{"rounded", "curved", "{type:round}"} {
		src := `{"root":{"type":"stack","children":[{"type":"box","border":"` + shape + `","children":[{"type":"text","text":"x"}]}]}}`
		d, err := scene.ParseDocument([]byte(src))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		err = d.Validate()
		if err == nil || !strings.Contains(err.Error(), `"border": "round"`) {
			t.Errorf("shape %q should be refused with the remedy naming round, got %v", shape, err)
		}
	}
	// A border on a node that draws none is refused too: the agent put one on the input
	// node itself, which validated and drew nothing.
	src := `{"root":{"type":"stack","children":[{"type":"input","bind":"user.input","border":"round"}]}}`
	d, _ := scene.ParseDocument([]byte(src))
	if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "wrap it") {
		t.Errorf("a border on an input must be refused with how to wrap it, got %v", err)
	}
}
