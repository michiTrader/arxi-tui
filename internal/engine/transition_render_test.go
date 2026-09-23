package engine

import (
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// The render half of G1: a node with transition wears the theme's dim intensity
// while its entrance runs and its own settled style once the host clock's phase
// reaches 1 (SCENES.md Scene 4, G-B — the SGR dim→bright intensity axis), and
// the frame is a pure function of that phase (ADR-0005) so these can pin the
// start, mid-entrance and settled states. The assertion is on the span's style
// token, not its text, because transition rides the intensity axis: the text is
// unchanged, only its token moves.
//
// Counterfactual, run rather than argued: reverting withTransition so it always
// returns the node unchanged leaves the settled and nil cases green and fails
// the running cases (they draw the settled token instead of dim). A version that
// always dimmed, ignoring the phase, fails the settled and nil cases the same
// way — the two directions that isolate the phase→intensity mapping.

func transitionDoc(t *testing.T) *scene.Document {
	t.Helper()
	// The settled token is "bright", not "text", on purpose: a renderer that
	// hardcoded the settled style would pass a "text" probe by accident, so the
	// probe uses a token the dim override is visibly different from.
	body := `{ "root": { "id": "tr", "type": "text", "text": "hello",
	  "style": { "style": "bright" }, "transition": { "anim": "default" } } }`
	doc, err := scene.ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: transition doc must parse; got %v", err)
	}
	if verr := doc.Validate(); verr != nil {
		t.Fatalf("premise broken: transition doc must validate; got %v", verr)
	}
	return doc
}

// The intensity moves with the phase: while the entrance runs the node draws
// dim, and once the phase settles it draws its own token. The axis is discrete,
// so 0.0 and 0.5 are the same dim frame — there is no intermediate, unlike
// reveal's continuous prefix — and only the crossing to settled changes the
// token.
func TestTransitionDimsWhileRunningAndSettlesWhenDone(t *testing.T) {
	doc := transitionDoc(t)
	state := fold.Fold(nil)

	cases := []struct {
		phase float64
		want  string
	}{
		{phase: 0.0, want: transitionDimToken}, // the dim start of the entrance
		{phase: 0.5, want: transitionDimToken}, // still running: intensity is discrete, no mid frame
		{phase: 1.0, want: "bright"},           // settled: the node's own token
	}
	for _, tc := range cases {
		r := Renderer{Width: 40, Height: 1, AnimPhase: map[string]float64{"tr": tc.phase}}
		got := styleOfText(r.RenderFrame(doc, state), "hello")
		if got != tc.want {
			t.Errorf("phase %v: %q renders under token %q, want %q.\n"+
				"consequence: the entrance's intensity is not being taken from the host clock's phase,\n"+
				"so either the phase is ignored (the node draws settled throughout, no entrance) or it\n"+
				"never settles (the node stays dim forever). Either way the dim→bright ramp ADR-0005/G-B\n"+
				"sign does not happen.\n"+
				"remedy: withTransition must draw the node dim while AnimPhase[id] < 1 and its own style\n"+
				"once it reaches 1.",
				tc.phase, "hello", got, tc.want)
		}
	}
}

// A nil AnimPhase is the pure/golden path with no clock, and a transition there
// draws its settled style — the no-op guarantee that keeps a document with a
// transition rendering fully when nothing drives time, so no non-motion golden
// moves. A non-nil map with no entry for the node is a different state: its first
// appearance in the live loop, phase 0, the dim start. The map's own nil-ness is
// the only thing that distinguishes them, so this pins both — the same seam
// revealPrefix has.
func TestTransitionNilPhaseIsSettledButAbsentEntryIsTheStart(t *testing.T) {
	doc := transitionDoc(t)
	state := fold.Fold(nil)

	// nil map: no clock, settled, the node's own token.
	settled := Renderer{Width: 40, Height: 1}
	if got := styleOfText(settled.RenderFrame(doc, state), "hello"); got != "bright" {
		t.Errorf("a transition rendered with no clock (nil AnimPhase) drew token %q, want its settled\n"+
			"%q; a document with a transition must render fully when nothing is animating it, or every\n"+
			"non-motion golden would move.", got, "bright")
	}

	// non-nil map, no entry for "tr": first appearance, phase 0, dim.
	started := Renderer{Width: 40, Height: 1, AnimPhase: map[string]float64{}}
	if got := styleOfText(started.RenderFrame(doc, state), "hello"); got != transitionDimToken {
		t.Errorf("a transition on its first live-loop frame (non-nil map, no entry) drew token %q, want\n"+
			"%q; the clock has not seen it yet, which is the dim start of the entrance, not the settled\n"+
			"end — the two absent states are told apart by whether the map itself is nil.",
			got, transitionDimToken)
	}
}

// A transitioning node reports itself active with its one-shot marker and token,
// so the loop drives the clock and resolves the token's timing against the
// theme. This is what makes transition ride the same one-shot machinery reveal
// built (G3) with no clock change: the loop cannot tell a transition from a
// reveal, and does not need to.
func TestTransitionReportsOneShotActivity(t *testing.T) {
	doc := transitionDoc(t)
	state := fold.Fold(nil)

	r := Renderer{Width: 40, Height: 1, AnimPhase: map[string]float64{"tr": 0.3}}
	_, active := r.RenderFrameActive(doc, state)
	if len(active) != 1 || active[0].NodeID != "tr" {
		t.Fatalf("a transitioning node must report exactly itself active; got %+v", active)
	}
	if !active[0].OneShot {
		t.Errorf("the transition does not report OneShot; the loop would drive it as a continuous\n" +
			"scroll (a tick count) instead of a phase that settles.")
	}
	if active[0].Token != "default" {
		t.Errorf("the transition reports token %q, want %q; the loop resolves the token's duration and\n"+
			"curve by this name.", active[0].Token, "default")
	}
}

// Transition is honoured on every node type, because the intensity axis is
// universal and withTransition sits at the renderNode chokepoint — the same
// reach focus_glow has. This is the engine-side pair of the scene test's
// universal acceptance: a validator that accepts transition everywhere while the
// renderer honoured it on one type would be the per-node-type defect this engine
// has produced twice (`when`, then style tokens). The container case is a
// different question (its children keep their own tokens, exactly as for a
// glow), so the probes here are the text-bearing types a transition visibly
// moves.
func TestTransitionAppliesToEveryTextBearingNodeType(t *testing.T) {
	cases := []struct {
		nodeType string
		extra    string
		want     string
	}{
		{nodeType: "text", extra: `,"text":"alpha"`, want: "alpha"},
		{nodeType: "markdown", extra: `,"text":"alpha"`, want: "alpha"},
		{nodeType: "marquee", extra: `,"text":"alpha"`, want: "alpha"},
		{nodeType: "rule", extra: ``, want: "─"},
	}

	for _, tc := range cases {
		t.Run(tc.nodeType, func(t *testing.T) {
			src := `{"root":{"id":"root","type":"stack","children":[` +
				`{"id":"target","type":"` + tc.nodeType + `","style":{"style":"bright"}` +
				`,"transition":{"anim":"default"}` + tc.extra + `}]}}`

			doc, err := scene.ParseDocument([]byte(src))
			if err != nil {
				t.Fatalf("ParseDocument: %v", err)
			}
			if warns := doc.Warnings(); len(warns) > 0 {
				t.Fatalf("premise broken: the %s probe must carry no warnings, or the frame cannot be\n"+
					"attributed to transition; got %v", tc.nodeType, warns)
			}

			// Mid-entrance, so the dim override is what proves the property fired.
			r := Renderer{Width: 40, Height: 10, AnimPhase: map[string]float64{"target": 0.4}}
			got := styleOfText(r.RenderFrame(doc, fold.Fold(nil)), tc.want)
			if got != transitionDimToken {
				t.Errorf("a mid-entrance %s node renders under token %q, want %q.\n"+
					"consequence: transition is honoured on some node types and not others, so it works on\n"+
					"whichever type the first test reached for and is silently absent on the rest — the\n"+
					"`when` defect and the style-token defect this engine already produced.\n"+
					"remedy: resolve the transition in renderNode, the single function every node passes\n"+
					"through, rather than at any one node type's renderer.",
					tc.nodeType, got, transitionDimToken)
			}
		})
	}
}
