package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// Scene 4 (ANIMATION) is the one golden scene whose subject is motion, so it is
// the one scene a static nil-phase fixture cannot pin: a nil AnimPhase/AnimTicks
// draws every prop settled, which is the frame with the animation switched off.
// A golden that froze that would name Scene 4 and cover none of it — the exact
// "a golden that discards what you are asserting about is not covering it" trap
// AGENTS.md records against SOBRIA.styled's zero-row marquee. So this golden is
// pinned at a *chosen non-nil phase*, the discipline DESIGN-BLOCK-G.md signs for
// every prop, and the frame it freezes witnesses all four moving props at once:
//   - a scroll marquee windowed at its tick offset,
//   - a reveal text clipped to a phase-wide prefix,
//   - a transition heading wearing the dim intensity mid-entrance,
//   - a staggered enter list mid-flight — one settled row, one dim row, and the
//     third not drawn at all, the row-count axis that distinguishes enter from
//     transition-on-every-row.
// It is the composed pin no per-prop render test carries: those each drive one
// prop in isolation, and this is the shipped document that drives them together.

// animationDoc reads, parses and validates the Scene 4 golden document. It
// validates the tokens against the factory theme as well as the structure,
// because the fixture's claim is that it is a *shippable* scene — an enter, a
// reveal and a transition all resolve real anim tokens under the shipped look
// (default/marquee/reveal.fast), not that it merely parses.
func animationDoc(t *testing.T) *scene.Document {
	t.Helper()
	data, err := os.ReadFile("../../testdata/ANIMATION.json")
	if err != nil {
		t.Fatalf("read ANIMATION.json: %v", err)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if verr := doc.Validate(); verr != nil {
		t.Fatalf("premise broken: the Scene 4 golden must validate structurally; got %v", verr)
	}
	if errs := scene.ValidateTokens(doc, theme.Factory()); len(errs) > 0 {
		t.Fatalf("premise broken: the Scene 4 golden names an anim/style token the factory theme does\n"+
			"not ship, so it is not a shippable scene; got %v", errs)
	}
	return doc
}

// animationState and animationRenderer are the fold and the host-computed motion
// the golden is pinned against. The three todos give the enter list a settled
// row, a dim row and a not-yet-reached row; the phase/tick map places every prop
// mid-flight. Both are one source of truth shared by the witness test and the
// two golden tests so they cannot drift on what frame is being asserted.
func animationState() fold.State {
	return fold.State{
		Todos: []fold.TodoItem{
			{Task: "read the plan"},
			{Task: "freeze the golden"},
			{Task: "close the block"},
		},
	}
}

func animationRenderer() Renderer {
	return Renderer{
		Width:  64,
		Height: 12,
		// The marquee's clock has advanced 8 ticks; at speed 2 that windows the
		// overflowing banner 16 cells in.
		AnimTicks: map[string]int{"banner": 8},
		// streaming half-revealed, heading mid-entrance (dim), and the enter list
		// with row 0 settled, row 1 dim, row 2 absent (no entry).
		AnimPhase: map[string]float64{
			"streaming":             0.5,
			"heading":               0.5,
			enterRowKey("todos", 0): 1.0,
			enterRowKey("todos", 1): 0.5,
		},
	}
}

// TestAnimationSceneWitnessesEveryMovingProp asserts, before the byte-for-byte
// golden, that the chosen frame actually shows each prop moving — so a later
// edit that settles the whole scene (the failure the nil-phase trap hides)
// fails here with a named consequence, not only as an opaque golden diff.
func TestAnimationSceneWitnessesEveryMovingProp(t *testing.T) {
	doc := animationDoc(t)
	r := animationRenderer()
	f := r.RenderFrame(doc, animationState())
	got := f.Plain()

	if strings.Contains(got, "UNKNOWN NODE TYPE") {
		t.Fatalf("the Scene 4 golden rendered an unknown node type:\n%s", got)
	}

	// reveal: the streaming line is clipped to a phase-wide prefix, so its full
	// text must not be present — a whole line would mean reveal drew settled.
	if strings.Contains(got, "one grapheme at a time") {
		t.Errorf("the reveal line rendered its whole text; the typewriter is not clipping to the\n"+
			"phase, so reveal is settled when the golden claims it mid-stream. frame:\n%s", got)
	}

	// transition: the heading wears the dim token while its entrance runs.
	if st := styleOfText(f, "a heading"); st != transitionDimToken {
		t.Errorf("the transition heading renders under %q, want %q; a settled heading here means the\n"+
			"golden is pinning the entrance-over frame, not the entrance.", st, transitionDimToken)
	}

	// enter: the row-count axis mid-flight — settled row in its own token, the
	// next row dim, and the third not drawn at all.
	if st := styleOfText(f, "read the plan"); st != "header" {
		t.Errorf("the settled enter row renders under %q, want its own %q; the finished row is not\n"+
			"arriving.", st, "header")
	}
	if st := styleOfText(f, "freeze the golden"); st != transitionDimToken {
		t.Errorf("the mid-entrance enter row renders under %q, want %q; the arriving row is not dim.",
			st, transitionDimToken)
	}
	if strings.Contains(got, "close the block") {
		t.Errorf("the not-yet-reached enter row is drawn; the row count is not growing with the\n"+
			"stagger, which is the whole point of the enter half of Scene 4. frame:\n%s", got)
	}
}

// TestAnimationSceneMatchesGolden freezes the plain frame. UPDATE_GOLDEN=1
// regenerates it. This is the durable pin: a change to any of the four props'
// render rules, the enter scheduler, the marquee window or the fold projection
// shows up here as a reviewable golden diff.
func TestAnimationSceneMatchesGolden(t *testing.T) {
	doc := animationDoc(t)
	r := animationRenderer()
	f := r.RenderFrame(doc, animationState())
	got := f.Plain()

	goldenPath := "../../testdata/ANIMATION.frame"
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
		t.Errorf("Scene 4 animation frame does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

// TestAnimationSceneStyledGolden freezes the styled frame, so a dropped or
// changed style token — the dim override an entrance rides, a settled row's own
// token — is a golden diff rather than a silent regression. UPDATE_GOLDEN=1
// regenerates it.
func TestAnimationSceneStyledGolden(t *testing.T) {
	doc := animationDoc(t)
	r := animationRenderer()
	f := r.RenderFrame(doc, animationState())
	got := f.Styled()

	goldenPath := "../../testdata/ANIMATION.styled"
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
		t.Errorf("Scene 4 animation styled output does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}
