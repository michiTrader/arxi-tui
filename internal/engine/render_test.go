package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

const factoryRAWScene = `{ "root": { "type": "stack", "children": [
  { "id": "chat",   "type": "markdown", "bind": "chat.history", "grow": 1 },
  { "id": "prompt", "type": "input",    "bind": "user.input", "placeholder": "> " }
]}}`

// TestRawSceneRendersCorrectly renders RAW.scene with the Phase 0 mock events
// and verifies that chat.history shows the transcript and user.input shows the prompt.
func TestRawSceneRendersCorrectly(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factoryRAWScene))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	r := Renderer{Width: 80, Height: 24}

	events := []fold.Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "hola"}},
		{Type: "llm.response", Seq: 2, Payload: map[string]any{"text": "Hola!"}},
		{Type: "run.prompt", Seq: 3, Payload: map[string]any{"text": "gracias"}},
		{Type: "llm.response", Seq: 4, Payload: map[string]any{"text": "De nada."}},
	}
	state := fold.Fold(events)

	f := r.RenderFrame(doc, state)
	got := f.Plain()

	if strings.Contains(got, "UNKNOWN NODE TYPE") {
		t.Errorf("render produced unknown node type; output:\n%s", got)
	}
	if !strings.Contains(got, "hola") {
		t.Errorf("expected 'hola' in chat history; got:\n%s", got)
	}
	if !strings.Contains(got, "Hola!") {
		t.Errorf("expected assistant response; got:\n%s", got)
	}
	if !strings.HasSuffix(got, "> ") {
		t.Errorf("expected output to end with '> '; got:\n%s", got)
	}
	t.Logf("rendered frame:\n%s", got)
}

// TestRawSceneInputNeverContracts is the Q6 property in executable form:
// the input row is always exactly one line at the bottom, no matter how much
// transcript fills the frame. A community scene that overflows must contract
// the elastic panes first and never the typing flow.
func TestRawSceneInputNeverContracts(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factoryRAWScene))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	r := Renderer{Width: 40, Height: 6}

	// Ten turns: 2 lines each = 20 lines, but the frame is only 6 tall.
	events := []fold.Event{}
	for i := 1; i <= 10; i++ {
		events = append(events, fold.Event{
			Type:    "run.prompt",
			Seq:     int64(i) * 2,
			Payload: map[string]any{"text": fmtShort(i)},
		})
		events = append(events, fold.Event{
			Type:    "llm.response",
			Seq:     int64(i)*2 + 1,
			Payload: map[string]any{"text": fmtShort(i * 2)},
		})
	}
	state := fold.Fold(events)

	f := r.RenderFrame(doc, state)
	lines := f.Live

	// Exactly 6 lines: 4 scrollback + 1 blank + 1 prompt. Never 5 or 7.
	if len(lines) != r.Height {
		t.Errorf("expected %d lines in a %d-tall frame, got %d", r.Height, r.Height, len(lines))
	}
	// The last line must be the input prompt.
	last := lines[len(lines)-1].Text()
	if !strings.Contains(last, "> ") {
		t.Errorf("expected last line to be the prompt '> '; got %q", last)
	}
}

// TestRawSceneTranscriptFillsFromTail guarantees new lines arrive at the
// bottom, above the input — the user does not have to scroll to see the
// reply they are waiting for.
func TestRawSceneTranscriptFillsFromTail(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factoryRAWScene))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	r := Renderer{Width: 80, Height: 4}
	state := fold.Fold([]fold.Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "first"}},
		{Type: "llm.response", Seq: 2, Payload: map[string]any{"text": "second"}},
		{Type: "run.prompt", Seq: 3, Payload: map[string]any{"text": "third"}},
		{Type: "llm.response", Seq: 4, Payload: map[string]any{"text": "fourth"}},
	})

	f := r.RenderFrame(doc, state)
	got := f.Plain()

	// "fourth" is the newest line; it must not have been clipped by overflow.
	if !strings.Contains(got, "fourth") {
		t.Errorf("newest transcript line 'fourth' was clipped; got:\n%s", got)
	}
}

func fmtShort(n int) string {
	return string(rune('A' + n - 1))
}
