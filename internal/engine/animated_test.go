package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// animatedState folds a real stage.advanced event so session.new_milestone
// reads true, the way the host would after a stage boundary. It is built by
// folding rather than by setting the field directly (the way configState builds
// its host view-state) because NewMilestone is DERIVED — its whole contract is
// "the last event folded is a milestone event" — so a test that set the bool by
// hand would pass even if deriveNewMilestone were deleted. Folding the event is
// the only construction that also exercises the derivation the scene depends on.
func animatedState() fold.State {
	return fold.Fold([]fold.Event{{Type: "stage.advanced"}})
}

func animatedDoc(t *testing.T) *scene.Document {
	t.Helper()
	data, err := os.ReadFile("../../testdata/ANIMATED.json")
	if err != nil {
		t.Fatalf("read ANIMATED.json: %v", err)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	return doc
}

// TestAnimatedSceneRenders holds the Scene 11 (ANIMATED) golden to the claim it
// exists for: box + shine + marquee + when:session.new_milestone + on_press
// compose one interactive milestone banner with no new primitives. It asserts
// the composed node actually draws when the milestone pulse is true, and that
// nothing in it degrades to the unknown-type or unresolved-bind placeholder.
func TestAnimatedSceneRenders(t *testing.T) {
	doc := animatedDoc(t)
	r := Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, animatedState())
	got := f.Plain()

	if strings.Contains(got, "UNKNOWN NODE TYPE") {
		t.Fatalf("the scene drew an unknown node type — a primitive Scene 11 composes is missing:\n%s", got)
	}
	if strings.Contains(got, "[…]") {
		t.Errorf("the scene drew the unresolved-bind placeholder, so a bind the scene names\n"+
			"resolved to nothing:\n%s", got)
	}

	// The box drew: its double border and its title are both on screen. A box
	// gated on session.new_milestone that failed to draw would leave the frame
	// with neither, and the gating counterfactual below would then prove
	// nothing (an always-absent box passes a "box is absent" check for the
	// wrong reason).
	for _, want := range []string{"milestone", "═", "a new milestone"} {
		if !strings.Contains(got, want) {
			t.Errorf("the milestone banner is missing %q; it should draw while the pulse is true:\n%s", want, got)
		}
	}
}

// TestAnimatedMilestoneGatesTheBanner is the gating counterfactual: the same
// document, folded to a state whose LAST event is not a milestone event, hides
// the whole box. It is the direction session.new_milestone's lifetime decision
// turns on — the pulse clears on the next event of any kind — so a fold that
// ends on a run.prompt must read the pulse false and drop the gated banner.
func TestAnimatedMilestoneGatesTheBanner(t *testing.T) {
	doc := animatedDoc(t)
	r := Renderer{Width: 80, Height: 24}

	// stage.advanced fires the pulse, then a later run.prompt clears it: the
	// last event is not a milestone, so the banner must be gone.
	cleared := fold.Fold([]fold.Event{
		{Type: "stage.advanced"},
		{Type: "run.prompt", Payload: map[string]any{"text": "keep going"}},
	})
	got := r.RenderFrame(doc, cleared).Plain()

	// The banner text and the box's double border are unique to the gated box —
	// the word "milestone" alone also appears in the ungated footer, so it
	// cannot witness the box's absence. These two can.
	for _, absent := range []string{"a new milestone", "═"} {
		if strings.Contains(got, absent) {
			t.Errorf("the milestone banner drew %q after the pulse should have cleared: a later\n"+
				"event did not clear session.new_milestone, so the gate is stuck open:\n%s", absent, got)
		}
	}
	// The ungated chrome is unaffected — the header still draws — so the check
	// above is measuring the gate and not an empty frame.
	if !strings.Contains(got, "session") {
		t.Fatalf("the header vanished too, so this test is measuring a blank render rather than\n"+
			"the gate:\n%s", got)
	}
}

// TestAnimatedShineModulatesWithTheClock is shine's own witness, independent of
// the golden. The golden freezes the at-rest (lit) frame; this drives the host
// clock and proves the emphasis actually pulses — the lit even beat and the
// base odd beat render different styled frames. Only the box's shine tick is
// varied (the marquee's own tick is 0 in both), so any difference is shine and
// not the marquee scrolling.
func TestAnimatedShineModulatesWithTheClock(t *testing.T) {
	doc := animatedDoc(t)
	state := animatedState()

	lit := Renderer{Width: 80, Height: 24, AnimTicks: map[string]int{"milestone": 0}}
	off := Renderer{Width: 80, Height: 24, AnimTicks: map[string]int{"milestone": 1}}

	litFrame := lit.RenderFrame(doc, state).Styled()
	offFrame := off.RenderFrame(doc, state).Styled()

	if litFrame == offFrame {
		t.Fatalf("shine drew the same styled frame on the lit and base beats, so the emphasis is\n"+
			"not modulating with the clock — withShine either never applied the token or applied\n"+
			"it on both beats.\nlit beat:\n%s\nbase beat:\n%s", litFrame, offFrame)
	}
}

// TestAnimatedSceneMatchesGolden freezes the plain frame. Set UPDATE_GOLDEN=1 to
// regenerate after an intended change.
func TestAnimatedSceneMatchesGolden(t *testing.T) {
	doc := animatedDoc(t)
	r := Renderer{Width: 80, Height: 24}
	got := r.RenderFrame(doc, animatedState()).Plain()

	const golden = "../../testdata/ANIMATED.frame"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run UPDATE_GOLDEN=1 to create): %v", err)
	}
	if got != string(want) {
		t.Errorf("ANIMATED plain frame drifted from its golden.\ngot:\n%s\nwant:\n%s\n"+
			"remedy: if this change is intended, UPDATE_GOLDEN=1 go test ./internal/engine/", got, string(want))
	}
}

// TestAnimatedSceneStyledGolden freezes the styled frame — the one that carries
// the shine token and the banner style. It folds the pulse true so the box and
// its marquee actually draw: a styled golden folded to a hidden box would pin an
// empty frame and leave the shine styling unmeasured (the zero-row-marquee trap
// LESSONS.md records for SOBRIA.styled).
func TestAnimatedSceneStyledGolden(t *testing.T) {
	doc := animatedDoc(t)
	r := Renderer{Width: 80, Height: 24}
	got := r.RenderFrame(doc, animatedState()).Styled()

	const golden = "../../testdata/ANIMATED.styled"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run UPDATE_GOLDEN=1 to create): %v", err)
	}
	if got != string(want) {
		t.Errorf("ANIMATED styled frame drifted from its golden.\ngot:\n%s\nwant:\n%s\n"+
			"remedy: if this change is intended, UPDATE_GOLDEN=1 go test ./internal/engine/", got, string(want))
	}
}
