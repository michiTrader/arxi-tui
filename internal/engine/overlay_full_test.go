package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

func frameText(r Renderer, doc *scene.Document, st fold.State) string {
	var b strings.Builder
	for _, l := range r.RenderFrame(doc, st).Live {
		for _, s := range l {
			b.WriteString(s.Text)
		}
		b.WriteString("\n")
	}
	return b.String()
}

const fullWindowDoc = `{"root":{"type":"stack","children":[
 {"id":"chat","type":"markdown","bind":"chat.history","grow":1},
 {"id":"prompt","type":"input","bind":"user.input","prefix":"> "},
 {"id":"win","type":"overlay","anchor":"full","when":"ui.max.is.win","children":[
   {"type":"text","text":"WINDOW TITLE"},
   {"type":"text","text":"window row"}]}
]}}`

// An overlay that spans the screen must replace the screen even when it has fewer rows than
// the screen has.
// Consequence: a window of three rows draws nothing at all and no message says why; a scene
// (or the agent) cannot make a window.
// Remedy: pad a full overlay to the height of the frame it replaces.
func TestAFullOverlayShorterThanTheScreenReplacesTheScreen(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(fullWindowDoc))
	if err != nil {
		t.Fatal(err)
	}
	r := Renderer{Width: 40, Height: 10}
	closed := frameText(r, doc, fold.State{UserInput: "typed"})
	if strings.Contains(closed, "WINDOW TITLE") {
		t.Fatalf("premise: the window is gated and must be absent when closed:\n%s", closed)
	}
	open := frameText(r, doc, fold.State{UIMax: "win", UserInput: "typed"})
	if !strings.Contains(open, "WINDOW TITLE") || !strings.Contains(open, "window row") {
		t.Errorf("the open window is not on screen. Consequence: an overlay shorter than the screen is ignored. Remedy: pad it to the frame height.\n%s", open)
	}
	if strings.Contains(open, "> typed") {
		t.Errorf("the input still shows under a full window. Consequence: the window shares the screen with the thing it should cover.\n%s", open)
	}
	if n := strings.Count(open, "\n"); n != 10 {
		t.Errorf("the window frame has %d rows, want the screen's 10. Consequence: the rows of the old frame are left under or the screen shrinks.\n%s", n, open)
	}
	// The input is under the window: a caret left blinking on a covered row sits on top of
	// whatever the window drew there.
	if r.RenderFrame(doc, fold.State{UserInput: "typed"}).Cursor.Hidden {
		t.Fatalf("premise: the caret must be shown while the window is closed")
	}
	if !r.RenderFrame(doc, fold.State{UIMax: "win", UserInput: "typed"}).Cursor.Hidden {
		t.Errorf("the caret is shown under a full window. Consequence: it blinks on a row the window covers. Remedy: hide the caret while a full overlay is open.")
	}
}
