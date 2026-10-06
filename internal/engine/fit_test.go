package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

const fitScene = `{"root":{"type":"stack","children":[
  {"id":"chat","type":"markdown","bind":"chat.history","grow":1,"fit":true},
  {"id":"prompt","type":"input","bind":"user.input","prefix":"> "},
  {"id":"status","type":"text","text":"status"}]}}`

const pinnedScene = `{"root":{"type":"stack","children":[
  {"id":"chat","type":"markdown","bind":"chat.history","grow":1},
  {"id":"prompt","type":"input","bind":"user.input","prefix":"> "},
  {"id":"status","type":"text","text":"status"}]}}`

func promptRow(t *testing.T, js string, height int, said int) int {
	t.Helper()
	var evs []fold.Event
	for i := 0; i < said; i++ {
		evs = append(evs, fold.Event{Type: "run.prompt", Seq: int64(i + 1), Payload: map[string]any{"text": "hi"}})
	}
	r := &Renderer{Width: 40, Height: height}
	rows := strings.Split(strings.TrimRight(r.RenderFrame(mustDoc(t, js), fold.Fold(evs)).Plain(), "\n"), "\n")
	for i, row := range rows {
		if strings.HasPrefix(row, "> ") {
			return i
		}
	}
	t.Fatalf("no prompt row:\n%s", strings.Join(rows, "\n"))
	return -1
}

// With `fit` the prompt rides right under the transcript and sinks as it grows,
// until it reaches the bottom and the transcript scrolls instead.
func TestFitPaneLetsTheInputFollowTheChat(t *testing.T) {
	if got := promptRow(t, fitScene, 20, 0); got != 0 {
		t.Errorf("empty chat: prompt on row %d, want 0", got)
	}
	one := promptRow(t, fitScene, 20, 1)
	two := promptRow(t, fitScene, 20, 2)
	if one == 0 || two <= one {
		t.Errorf("the prompt should sink as the chat grows: 1 turn row %d, 2 turns row %d", one, two)
	}
	if got := promptRow(t, fitScene, 20, 60); got != 18 {
		t.Errorf("a full chat: prompt on row %d, want the second to last (18)", got)
	}
}

// Without `fit` the old behaviour holds: the input is pinned to the bottom.
func TestGrowWithoutFitStaysPinnedToTheBottom(t *testing.T) {
	if got := promptRow(t, pinnedScene, 20, 0); got != 18 {
		t.Errorf("pinned scene: prompt on row %d, want 18", got)
	}
}
