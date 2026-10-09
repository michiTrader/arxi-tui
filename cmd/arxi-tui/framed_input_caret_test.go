package main

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/patch"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// Round the input bar with the guide's own recipe: the terminal cursor must sit inside
// the frame, on the input's row, and not wherever the last frame row left it.
func TestTheCursorStaysInTheInputBarWhenAFrameIsRoundIt(t *testing.T) {
	doc := builtinDoc(t)
	src := doc.Source()
	for _, line := range []string{
		`/ui add node above prompt {"id":"input_frame","type":"box","border":"round"}`,
		`/ui move prompt into input_frame`,
	} {
		res, err := patch.Apply(doc.Name(), src, line)
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		src = res.Source
	}
	d, err := scene.ParseNamed(doc.Name(), src)
	if err != nil {
		t.Fatal(err)
	}
	r := &engine.Renderer{Width: 80, Height: 24}
	st := fold.State{}
	st.UserInput = "hola"
	st.UserInputCaret = 2
	f := r.RenderFrame(d, st)
	if f.Cursor.Hidden {
		t.Fatalf("the caret is hidden: with the input framed the terminal cursor is left on the last row painted, away from the text being typed")
	}
	row := f.Live[f.Cursor.Line].Text()
	if !strings.Contains(row, "hola") {
		t.Fatalf("the caret is on row %d %q, which is not the input's row", f.Cursor.Line, row)
	}
	if want := utf8.RuneCountInString(row[:strings.Index(row, "hola")]) + 2; f.Cursor.Col != want {
		t.Fatalf("the caret is at column %d, want %d (two letters into the text, inside the frame)", f.Cursor.Col, want)
	}
}

// The factory interface draws no frame round the input bar: a frame is something a user
// asks the agent for (and /ui reset takes away), never a default.
func TestTheFactoryInterfaceHasNoFrameRoundTheInputBar(t *testing.T) {
	doc := builtinDoc(t)
	r := &engine.Renderer{Width: 80, Height: 24}
	st := fold.State{}
	st.UserInput = "x"
	for _, l := range r.RenderFrame(doc, st).Live {
		if strings.ContainsAny(l.Text(), "╭╮╰╯┌┐└┘╔╗╚╝┏┓┗┛") {
			t.Fatalf("the factory interface draws a frame: %q", l.Text())
		}
	}
}
