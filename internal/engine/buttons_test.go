package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// buttonsEvents folds a short exchange that ends on a decision the operator is
// asked to make: the agent proposes an action and the run blocks awaiting an
// answer. The three buttons in the fixture are static nodes, so the fold only
// has to populate the transcript above them — but a blocked run is the context
// Scene 8 exists for, so the events name it rather than leaving the buttons
// floating over an idle chat.
func buttonsEvents() []fold.Event {
	return []fold.Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "refactor the parser"}},
		{Type: "llm.response", Seq: 2, Payload: map[string]any{"text": "I plan to rename ParseDoc to ParseDocument across 4 files. Approve?"}},
		{Type: "agent.blocked", Seq: 3, Payload: map[string]any{"blocked_ref": map[string]any{"inbox_id": "req-1"}}},
	}
}

// TestButtonsSceneRenders holds the Scene 8 (BUTTONS) fixture to the one thing
// the node type exists for: three pressable controls, each drawing its framed
// label, above the input. A missing label means the button node drew nothing a
// user can read or Tab to, the silent-drop shape the placeholder rule refuses.
func TestButtonsSceneRenders(t *testing.T) {
	data, err := os.ReadFile("../../testdata/BUTTONS.json")
	if err != nil {
		t.Fatalf("read BUTTONS.json: %v", err)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("scene validation: %v", err)
	}

	state := fold.Fold(buttonsEvents())

	r := Renderer{Width: 80, Height: 24}
	got := r.RenderFrame(doc, state).Plain()

	if strings.Contains(got, "UNKNOWN NODE TYPE") {
		t.Errorf("buttons scene rendered unknown node type:\n%s\n"+
			"consequence: `button` is dispatched to renderUnknownType, so Scene 8's decision surface\n"+
			"draws the placeholder where its controls should be.\n"+
			"remedy: keep the `button` case in renderByType.", got)
	}

	// Each button draws its label framed. The frame is what tells the eye a
	// control from prose, so a bare label is as much a regression as a missing
	// one — Scene 8's whole subject is that a button looks pressable.
	for _, label := range []string{"Approve", "Reject", "Reply"} {
		want := "[ " + label + " ]"
		if !strings.Contains(got, want) {
			t.Errorf("the buttons scene never drew %q; got:\n%s\n"+
				"consequence: a decision button the operator must press is either absent or drawn as\n"+
				"prose, so the pressable surface Scene 8 documents is not on screen.\n"+
				"remedy: renderButton frames the label as %q; keep it.", want, got, want)
		}
	}
}

// TestButtonsSceneMatchesGolden freezes the plain frame. A change to the button
// frame, the decision row's layout, or the fold above it is a reviewable golden
// diff rather than a silent shift. UPDATE_GOLDEN=1 regenerates the fixture.
func TestButtonsSceneMatchesGolden(t *testing.T) {
	data, err := os.ReadFile("../../testdata/BUTTONS.json")
	if err != nil {
		t.Fatalf("read BUTTONS.json: %v", err)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	state := fold.Fold(buttonsEvents())

	r := Renderer{Width: 80, Height: 24}
	got := r.RenderFrame(doc, state).Plain()

	goldenPath := "../../testdata/BUTTONS.frame"
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
		t.Errorf("buttons scene does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

// TestButtonsSceneStyledGolden freezes the styled frame, so a dropped or changed
// style token on a button — including the `bright` the approve button declares —
// is a golden diff and not a silent restyle. UPDATE_GOLDEN=1 regenerates it.
func TestButtonsSceneStyledGolden(t *testing.T) {
	data, err := os.ReadFile("../../testdata/BUTTONS.json")
	if err != nil {
		t.Fatalf("read BUTTONS.json: %v", err)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	state := fold.Fold(buttonsEvents())

	r := Renderer{Width: 80, Height: 24}
	got := r.RenderFrame(doc, state).Styled()

	goldenPath := "../../testdata/BUTTONS.styled"
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
		t.Errorf("buttons scene styled output does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}
