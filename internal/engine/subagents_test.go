package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// subagentsEvents folds a small team into being: two agents activate (backend
// then frontend, so memberOrder is deterministic), and frontend finishes its
// turn. The result is one busy member (backend, still thinking) and one idle
// member (frontend, one turn done) — the witness the per-row `when: row.busy`
// gate needs a true and a false row for, and the two roles/turns the row
// template renders distinctly.
func subagentsEvents() []fold.Event {
	return []fold.Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{
			"run_id": "r1", "actor": "user", "budget_usd": 10.0,
			"max_turns": 10, "simulated": false,
		}},
		{Type: "agent.activated", Seq: 2, Payload: map[string]any{"agent": "backend", "role": "backend"}},
		{Type: "agent.activated", Seq: 3, Payload: map[string]any{"agent": "frontend", "role": "frontend"}},
		{Type: "agent.turn_done", Seq: 4, Payload: map[string]any{"agent": "frontend"}},
	}
}

// TestSubagentsSceneRenders verifies the Scene 9 (SUBAGENTS) golden scene draws
// its subagent list below the input: one row per team member, each resolving
// `row.role` against its own element (BINDS.md §4.7). The per-row `when` gate
// and the empty-state are unit-tested in row_template_test.go; this holds a real
// shipped document to the per-element instantiation.
func TestSubagentsSceneRenders(t *testing.T) {
	data, err := os.ReadFile("../../testdata/SUBAGENTS.json")
	if err != nil {
		t.Fatalf("read SUBAGENTS.json: %v", err)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("scene validation: %v", err)
	}

	state := fold.Fold(subagentsEvents())

	r := Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, state)
	got := f.Plain()

	if strings.Contains(got, "UNKNOWN NODE TYPE") {
		t.Errorf("subagents scene rendered unknown node type:\n%s", got)
	}

	// Both members' roles must appear, each on its own row: the row_template
	// instantiates once per element of team.members. A missing role means the
	// template is not being instantiated per element — the silent-drop shape the
	// row_template refusal was lifted to permit only once it could not happen.
	for _, want := range []string{"backend", "frontend"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected the subagent list to render member role %q; got:\n%s\n"+
				"consequence: the row_template over team.members is not instantiating a row for that\n"+
				"member, so a subagent the fold reports is invisible on screen.\n"+
				"remedy: rowScopesFor must supply one scope per element and the template must read row.role.", want, got)
		}
	}

	// A relative bind that lost its row scope resolves to the placeholder, and a
	// placeholder where a role should be is the exact wrong-value-vs-honest-gap
	// distinction §4.6 signs. The list must not render one.
	if strings.Contains(got, "[…]") {
		t.Errorf("a subagent row rendered the placeholder [\u2026] instead of a role; got:\n%s\n"+
			"consequence: row.role resolved with no element in scope, so a row is present but empty —\n"+
			"the sub-renderer lost the row scope, the bug the per-row `when` counterfactual caught.\n"+
			"remedy: rowScopesFor must supply row.role and renderRowTemplate must set r.curRow per element.", got)
	}
}

// TestSubagentsSceneMatchesGolden compares the rendered plain frame against the
// golden file. UPDATE_GOLDEN=1 regenerates the fixture. This is the durable pin
// E4 adds: the scene's shape is frozen so a later change to the list, the
// row_template scope or the team fold shows up as a reviewable golden diff.
func TestSubagentsSceneMatchesGolden(t *testing.T) {
	data, err := os.ReadFile("../../testdata/SUBAGENTS.json")
	if err != nil {
		t.Fatalf("read SUBAGENTS.json: %v", err)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	state := fold.Fold(subagentsEvents())

	r := Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, state)
	got := f.Plain()

	goldenPath := "../../testdata/SUBAGENTS.frame"
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
		t.Errorf("subagents scene does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

// TestSubagentsSceneStyledGolden pins the styled frame, so a dropped or changed
// style token on the list rows is a golden diff rather than a silent regression.
// UPDATE_GOLDEN=1 regenerates the fixture.
func TestSubagentsSceneStyledGolden(t *testing.T) {
	data, err := os.ReadFile("../../testdata/SUBAGENTS.json")
	if err != nil {
		t.Fatalf("read SUBAGENTS.json: %v", err)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	state := fold.Fold(subagentsEvents())

	r := Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, state)
	got := f.Styled()

	goldenPath := "../../testdata/SUBAGENTS.styled"
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
		t.Errorf("subagents scene styled output does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}
