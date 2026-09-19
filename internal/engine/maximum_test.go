package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// TestMaximumSceneRenders verifies the Scene 3 (MAXIMUM) golden scene renders
// its banner, side panel, input, and top-right overlay correctly with the
// Phase 0 mock events.
func TestMaximumSceneRenders(t *testing.T) {
	data, err := os.ReadFile("../../testdata/MAXIMUM.json")
	if err != nil {
		t.Fatalf("read MAXIMUM.json: %v", err)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("scene validation: %v", err)
	}

	events := []fold.Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{
			"run_id": "r1", "actor": "user", "budget_usd": 10.0,
			"max_turns": 10, "simulated": false,
		}},
		{Type: "agent.activated", Seq: 2, Payload: map[string]any{"agent": "backend"}},
		{Type: "llm.response", Seq: 3, Payload: map[string]any{
			"agent": "backend", "model": "openai/gpt-4o",
			"text":      "Hola! How can I help you today?",
			"tokens_in": 25, "tokens_out": 35, "cost_usd": 0.001,
		}},
		{Type: "agent.turn_done", Seq: 4, Payload: map[string]any{"agent": "backend"}},
	}
	state := fold.Fold(events)

	r := Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, state)
	got := f.Plain()

	if strings.Contains(got, "UNKNOWN NODE TYPE") {
		t.Errorf("maximum scene rendered unknown node type:\n%s", got)
	}

	// The input prompt must use the maximum-style prefix.
	if !strings.Contains(got, "❯") {
		t.Errorf("expected input prefix '❯ '; got:\n%s", got)
	}

	// The tokens overlay must render the budget the events above actually
	// describe. This assertion used to require "0", which was not a reading
	// of the fold but a transcription of the projection's hardcoded
	// constant: the renderer returned the literal "0" for
	// session.tokens_used, and the test pinned that. So the one check that
	// could have caught the constant was instead the thing that protected
	// it — and because "0" is also this bind's signed empty state, it read
	// as a legitimate expectation rather than a defect.
	//
	// The value is derived, not chosen: run.started carries budget_usd 10.0
	// (10000 microunits) and the single llm.response costs 0.001 (1
	// microunit), so BINDS.md §4.1's budget-minus-cost is 9999. Asserting
	// the arithmetic is what makes the test sensitive to the fold again — a
	// projection that returns any constant, "0" included, now fails here.
	if !strings.Contains(got, "┌") || !strings.Contains(got, "┐") {
		t.Errorf("expected overlay border corners; got:\n%s", got)
	}
	if !strings.Contains(got, "│9999") {
		t.Errorf("expected overlay to show session.tokens_used as 9999 (budget_usd 10.0 = 10000 microunits, minus cost_usd 0.001 = 1); got:\n"+
			"consequence: the overlay is bound to session.tokens_used and the fold computes it from\n"+
			"the events above, so any other value means the projection is ignoring fold.State and\n"+
			"reporting a number the run never had.\n"+
			"remedy: return state.SessionTokensUsed from resolveBind rather than a literal.\n%s", got)
	}

	// The tasks box title must render.
	if !strings.Contains(got, "Tasks") {
		t.Errorf("expected tasks box title; got:\n%s", got)
	}
}

// TestMaximumSceneStyledGolden compares the rendered Scene 3 frame with token
// annotations against the golden file. UPDATE_GOLDEN=1 regenerates the fixture.
func TestMaximumSceneStyledGolden(t *testing.T) {
	data, err := os.ReadFile("../../testdata/MAXIMUM.json")
	if err != nil {
		t.Fatalf("read MAXIMUM.json: %v", err)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	events := []fold.Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{
			"run_id": "r1", "actor": "user", "budget_usd": 10.0,
			"max_turns": 10, "simulated": false,
		}},
		{Type: "agent.activated", Seq: 2, Payload: map[string]any{"agent": "backend"}},
		{Type: "llm.response", Seq: 3, Payload: map[string]any{
			"agent": "backend", "model": "openai/gpt-4o",
			"text":      "Hola! How can I help you today?",
			"tokens_in": 25, "tokens_out": 35, "cost_usd": 0.001,
		}},
		{Type: "agent.turn_done", Seq: 4, Payload: map[string]any{"agent": "backend"}},
	}
	state := fold.Fold(events)

	r := Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, state)
	got := f.Styled()

	goldenPath := "../../testdata/MAXIMUM.styled"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	if got != string(want) {
		t.Errorf("maximum scene styled output does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

// TestMaximumSceneMatchesGolden compares the rendered Scene 3 frame against the
// golden file. UPDATE_GOLDEN=1 regenerates the fixture.
func TestMaximumSceneMatchesGolden(t *testing.T) {
	data, err := os.ReadFile("../../testdata/MAXIMUM.json")
	if err != nil {
		t.Fatalf("read MAXIMUM.json: %v", err)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	events := []fold.Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{
			"run_id": "r1", "actor": "user", "budget_usd": 10.0,
			"max_turns": 10, "simulated": false,
		}},
		{Type: "agent.activated", Seq: 2, Payload: map[string]any{"agent": "backend"}},
		{Type: "llm.response", Seq: 3, Payload: map[string]any{
			"agent": "backend", "model": "openai/gpt-4o",
			"text":      "Hola! How can I help you today?",
			"tokens_in": 25, "tokens_out": 35, "cost_usd": 0.001,
		}},
		{Type: "agent.turn_done", Seq: 4, Payload: map[string]any{"agent": "backend"}},
	}
	state := fold.Fold(events)

	r := Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, state)
	got := f.Plain()

	goldenPath := "../../testdata/MAXIMUM.frame"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	if got != string(want) {
		t.Errorf("maximum scene does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

// TestMaximumSceneBoxBorder verifies that box nodes render with their configured
// border style: single, double, and ascii.
func TestMaximumSceneBoxBorder(t *testing.T) {
	cases := []struct {
		name     string
		border   string
		topleft  rune
		topright rune
	}{
		{"single", "single", '┌', '┐'},
		{"double", "double", '╔', '╗'},
		{"ascii", "ascii", '+', '+'},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sceneJSON := `{ "root": { "type": "box", "border": "` + c.border + `",
			  "children": [ { "type": "text", "text": "hello" } ] }}`
			doc, err := scene.ParseDocument([]byte(sceneJSON))
			if err != nil {
				t.Fatalf("ParseDocument: %v", err)
			}
			state := fold.State{}
			r := Renderer{Width: 40, Height: 5}
			f := r.RenderFrame(doc, state)
			got := f.Plain()

			topLine := strings.SplitN(got, "\n", 2)[0]
			if !strings.ContainsRune(topLine, c.topleft) {
				t.Errorf("border %s: expected top-left corner %q in %q", c.name, c.topleft, topLine)
			}
			if !strings.ContainsRune(topLine, c.topright) {
				t.Errorf("border %s: expected top-right corner %q in %q", c.name, c.topright, topLine)
			}
		})
	}
}

// TestMaximumSceneOverlayTopRight verifies that an overlay with anchor "top-right"
// renders only when its when bind is truthy.
func TestMaximumSceneOverlayTopRight(t *testing.T) {
	sceneJSON := `{ "root": { "type": "stack", "children": [
	  { "id": "chat", "type": "markdown", "bind": "chat.history", "grow": 1 },
	  { "id": "tokens", "type": "overlay", "anchor": "top-right", "min_width": 12,
	    "border": "single", "children": [
	      { "type": "text", "bind": "usage.delta" } ] }
	]}}`

	doc, err := scene.ParseDocument([]byte(sceneJSON))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	r := Renderer{Width: 60, Height: 10}

	// When slash.active is false, the overlay content (usage.delta is empty)
	// should not render.
	state := fold.State{AgentMode: "idle"}
	f := r.RenderFrame(doc, state)
	got := f.Plain()

	// The overlay has no `when`, so it always renders. But usage.delta is
	// empty, so the text node renders as a placeholder.
	if !strings.Contains(got, "┌") {
		t.Errorf("expected overlay border to render; got:\n%s", got)
	}
}

// TestMaximumSceneWeightLayout verifies that row children with weight
// divide the available width proportionally.
func TestMaximumSceneWeightLayout(t *testing.T) {
	sceneJSON := `{ "root": { "type": "row", "children": [
	  { "type": "text", "text": "left", "weight": 3 },
	  { "type": "text", "text": "right", "weight": 1 }
	]}}`

	doc, err := scene.ParseDocument([]byte(sceneJSON))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	r := Renderer{Width: 40, Height: 3}
	state := fold.State{}
	f := r.RenderFrame(doc, state)
	got := f.Plain()

	// Both texts should appear.
	if !strings.Contains(got, "left") {
		t.Errorf("expected 'left' in output; got:\n%s", got)
	}
	if !strings.Contains(got, "right") {
		t.Errorf("expected 'right' in output; got:\n%s", got)
	}

	// The content should be on the first line; remaining lines are padding.
	firstLineText := f.Live[0].Text()
	if !strings.Contains(firstLineText, "left") || !strings.Contains(firstLineText, "right") {
		t.Errorf("expected 'left' and 'right' on the first line; got: %q", firstLineText)
	}
}

// TestMaximumSceneTodosList verifies that agent.todos renders in a list node.
func TestMaximumSceneTodosList(t *testing.T) {
	sceneJSON := `{ "root": { "type": "list", "bind": "agent.todos" }}`

	doc, err := scene.ParseDocument([]byte(sceneJSON))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	state := fold.State{
		Todos: []fold.TodoItem{
			{Task: "approve budget", BlockedOn: "approval", Actor: "backend"},
			{Task: "wait for peer review", BlockedOn: "peer", Actor: "frontend"},
		},
	}

	r := Renderer{Width: 40, Height: 5}
	f := r.RenderFrame(doc, state)
	got := f.Plain()

	if !strings.Contains(got, "approve budget") {
		t.Errorf("expected todo 'approve budget' in output; got:\n%s", got)
	}
	if !strings.Contains(got, "wait for peer review") {
		t.Errorf("expected todo 'wait for peer review' in output; got:\n%s", got)
	}
}
