package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// A text with line breaks is that many rows. Counterfactual: it used to be one row, which
// is why an ASCII banner showed only its first line.
func TestATextWithLineBreaksDrawsEveryRow(t *testing.T) {
	js := `{"root":{"type":"stack","children":[
  {"id":"art","type":"text","text":"╔══╗\n║ar║\n╚══╝"},
  {"id":"prompt","type":"input","bind":"user.input","prefix":"> "}]}}`
	r := &Renderer{Width: 20, Height: 10}
	out := r.RenderFrame(mustDoc(t, js), fold.Fold(nil)).Plain()
	for _, want := range []string{"╔══╗", "║ar║", "╚══╝"} {
		if !strings.Contains(out, want) {
			t.Errorf("row %q is missing:\n%s", want, out)
		}
	}
}

func historyOf(n int) []fold.Event {
	var evs []fold.Event
	for i := 0; i < n; i++ {
		evs = append(evs, fold.Event{Type: "run.prompt", Seq: int64(i + 1), Payload: map[string]any{"text": "msg"}})
	}
	return evs
}

const floatingScene = `{"root":{"type":"stack","children":[
  {"id":"floating","type":"text","text":"FLOATING"},
  {"id":"scrolling","type":"text","text":"SCROLLING","in_chat":true},
  {"id":"chat","type":"markdown","bind":"chat.history","grow":1},
  {"id":"prompt","type":"input","bind":"user.input","prefix":"> "}]}}`

// A banner without in_chat stays put; one with it leaves with the conversation. Both can
// be in the same scene.
func TestABannerCanFloatOrScrollAwayAndBothCanCoexist(t *testing.T) {
	draw := func(turns int) string {
		r := &Renderer{Width: 30, Height: 8}
		return r.RenderFrame(mustDoc(t, floatingScene), fold.Fold(historyOf(turns))).Plain()
	}
	few := draw(1)
	if !strings.Contains(few, "FLOATING") || !strings.Contains(few, "SCROLLING") {
		t.Fatalf("a short conversation must show both:\n%s", few)
	}
	if strings.Index(few, "FLOATING") > strings.Index(few, "SCROLLING") {
		t.Errorf("the scrolling banner opens the chat, under the floating one:\n%s", few)
	}
	many := draw(40)
	if !strings.Contains(many, "FLOATING") {
		t.Errorf("the floating banner must stay however long the chat is:\n%s", many)
	}
	if strings.Contains(many, "SCROLLING") {
		t.Errorf("the scrolling banner must scroll away with the chat:\n%s", many)
	}
}

// in_chat on a stack with no chat pane is an ordinary child, not a refusal and not a loss.
func TestInChatWithoutAChatPaneStillDraws(t *testing.T) {
	js := `{"root":{"type":"stack","children":[
  {"id":"a","type":"text","text":"HERE","in_chat":true},
  {"id":"prompt","type":"input","bind":"user.input","prefix":"> "}]}}`
	r := &Renderer{Width: 20, Height: 6}
	if out := r.RenderFrame(mustDoc(t, js), fold.Fold(nil)).Plain(); !strings.Contains(out, "HERE") {
		t.Errorf("the node vanished:\n%s", out)
	}
}
