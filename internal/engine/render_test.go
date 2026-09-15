package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// TestRawSceneRendersCorrectly renders RAW.scene with the Phase 0 mock events
// and verifies that chat.history shows the transcript and user.input shows the prompt.
func TestRawSceneRendersCorrectly(t *testing.T) {
	// Factory RAW scene JSON embedded directly (no file read in tests)
	jsonDoc := `{ "root": { "type": "stack", "children": [
		{ "id": "chat",   "type": "markdown", "bind": "chat.history", "grow": 1 },
		{ "id": "prompt", "type": "input",    "bind": "user.input", "placeholder": "> " }
	]}}`
	doc, err := scene.ParseDocument([]byte(jsonDoc))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	r := Renderer{Width: 80, Height: 24}

	// Phase 0 mock events: user asks, assistant answers, repeat.
	events := []fold.Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "hola"}},
		{Type: "llm.response", Seq: 2, Payload: map[string]any{"text": "Hola! ¿En qué puedo ayudarte?"}},
		{Type: "run.prompt", Seq: 3, Payload: map[string]any{"text": "gracias"}},
		{Type: "llm.response", Seq: 4, Payload: map[string]any{"text": "De nada."}},
	}
	state := fold.Fold(events)

	f := r.RenderFrame(doc, state)
	got := f.Plain()

	// The rendered output must:
	// 1. Contain the chat transcript in order
	// 2. End with the prompt "> "
	// 3. Have no "UNKNOWN NODE TYPE" errors
	if strings.Contains(got, "UNKNOWN NODE TYPE") {
		t.Errorf("render produced unknown node type; output:\n%s", got)
	}
	if !strings.Contains(got, "hola") {
		t.Errorf("expected 'hola' in chat history; got:\n%s", got)
	}
	if !strings.Contains(got, "Hola! ¿En qué puedo ayudarte?") {
		t.Errorf("expected assistant response; got:\n%s", got)
	}
	if !strings.HasSuffix(got, "> ") {
		t.Errorf("expected output to end with '> '; got:\n%s", got)
	}
	t.Logf("rendered frame:\n%s", got)
}
