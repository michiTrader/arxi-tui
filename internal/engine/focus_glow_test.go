package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

// Scene 4's first animation property, and the scope decision behind taking one
// rather than all five.
//
// The progress audit reports the Scene 4 paragraph as five documented
// properties, zero honoured. The obvious reading is "implement five
// properties"; measuring first says otherwise, and the difference is the
// distinction this project paid for two turns ago — when the scope of a guard
// is justified by a risk, ask whether the risk wants *less scope* or a *more
// specific question asked of the full scope*. Here it wants a more specific
// question asked of each property, and the answers differ:
//
//	focus_glow  needs the focused node's id. ui.focus is signed in BINDS.md,
//	            maintained by the fold, and already projected by resolveBind.
//	            Nothing else is missing: a node whose id equals ui.focus
//	            renders under a different token. No clock.
//	transition  needs a previous frame to interpolate from, and a duration
//	            from the [anim] token.
//	reveal      needs elapsed time since the node appeared.
//	enter       needs per-row arrival times and a stagger interval.
//	scroll      needs a step per tick.
//
// Four of the five need a host clock, and the clock needs its timing
// vocabulary: Q8 assigns it to "a global `[anim]` token with per-node
// override", and grep says docs/TOKENS.md does not mention `anim` anywhere.
// The timing format is undesigned. Implementing those four now would mean
// inventing the token format in the renderer — which is exactly what the
// row_template, on_press and scroll refusals declined to do, on the grounds
// that building format ahead of the phase meant to design it is the expensive
// kind of progress. PLAN.md schedules the clock in Phase 4.
//
// So one property lands, because one property is what the existing vocabulary
// can express honestly, and the other four keep their addressed warning until
// the clock has a signed timing format. That is also what the progress audit
// will now report — 1 honoured, 4 warned — which is the point of having
// instrumented it: the number moves for a reason a reader can check.
//
// Why a token swap and not a brightness computation: TOKENS.md signs
// "brightening text only" as the emphasis mechanism, and the theme already
// defines `bright`. A glow that computed its own attributes would bypass the
// token layer every other visual decision in this engine goes through, and
// would be the one style in the product a user could not restyle — a closed
// vocabulary with a named owner, which is the arxi-sim mistake this repository
// exists to correct.
func TestFocusGlowRendersTheFocusedNodeUnderItsGlowToken(t *testing.T) {
	const src = `{"root":{"id":"root","type":"stack","children":[
		{"id":"first","type":"text","text":"alpha","style":{"style":"text"},"focus_glow":{"style":"bright"}},
		{"id":"second","type":"text","text":"beta","style":{"style":"text"},"focus_glow":{"style":"bright"}}
	]}}`

	doc, err := scene.ParseDocument([]byte(src))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if verr := doc.Validate(); verr != nil {
		t.Fatalf("premise broken: the probe must validate clean, or a missing glow below could\n"+
			"be a refusal of something else entirely; got %v", verr)
	}
	// The property must be inside the vocabulary now, or the assertions
	// below would be measuring a key encoding/json threw away — the exact
	// false pass PR #15 was written to stop.
	if warns := doc.Warnings(); len(warns) > 0 {
		t.Fatalf("premise broken: focus_glow must be in the parser vocabulary, or the frame below\n"+
			"cannot be attributed to the property; got warnings %v", warns)
	}

	r := Renderer{Width: 40, Height: 10}

	// Nothing focused: neither node glows. This is the control, and it is
	// what makes the two assertions below mean something — without it a
	// renderer that glowed every node unconditionally would pass.
	unfocused := r.RenderFrame(doc, fold.State{})
	if style := styleOfText(unfocused, "alpha"); style != "text" {
		t.Errorf("with nothing focused, %q renders under token %q, want its declared %q.\n"+
			"consequence: an unfocused node wearing the glow means the glow marks nothing —\n"+
			"every row is emphasised, so the property cannot show the user where focus is.",
			"alpha", style, "text")
	}

	// ui.focus names the second node: it glows, the first does not.
	focused := r.RenderFrame(doc, fold.State{UIFocus: "second"})

	if style := styleOfText(focused, "beta"); style != "bright" {
		t.Errorf("ui.focus is %q and that node renders under token %q, want %q from its focus_glow.\n"+
			"consequence: the property parsed, validated and changed nothing — a silent drop, the\n"+
			"class this suite has closed eight times. The author writing focus_glow gets a frame\n"+
			"identical to one that omits it, with no diagnostic anywhere.\n"+
			"remedy: renderNode must resolve the node's style through focus_glow when n.ID equals\n"+
			"state.UIFocus.", "second", style, "bright")
	}

	if style := styleOfText(focused, "alpha"); style != "text" {
		t.Errorf("ui.focus is %q but the unfocused node %q renders under token %q, want %q.\n"+
			"consequence: the glow is applied by node type or by declaration rather than by focus,\n"+
			"so it marks every candidate at once. That is the defect shape of the `when` gate two\n"+
			"turns ago — honoured on an axis that was not the one the property names.",
			"second", "alpha", style, "text")
	}
}

// TestFocusGlowIsInertWithoutAGlowToken pins the other half of the contract: a
// node that declares no focus_glow must not change when it gains focus.
//
// This is the vacuity guard for the test above. A renderer that brightened
// whatever ui.focus names, ignoring focus_glow entirely, would satisfy every
// assertion there — the property would look implemented while the document's
// actual declaration was still being dropped. That is the "projection that
// cannot vary is a constant" finding from PR #8, reappearing one level up.
func TestFocusGlowIsInertWithoutAGlowToken(t *testing.T) {
	const src = `{"root":{"id":"root","type":"stack","children":[
		{"id":"plain","type":"text","text":"gamma","style":{"style":"text"}}
	]}}`

	doc, err := scene.ParseDocument([]byte(src))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	r := Renderer{Width: 40, Height: 10}
	got := styleOfText(r.RenderFrame(doc, fold.State{UIFocus: "plain"}), "gamma")
	if got != "text" {
		t.Errorf("a node declaring no focus_glow renders under token %q when focused, want its\n"+
			"declared %q.\n"+
			"consequence: the engine is brightening whatever ui.focus names instead of reading the\n"+
			"document's focus_glow, so the scene cannot decline the effect and cannot choose the\n"+
			"token. The property would read as implemented while the declaration it is named after\n"+
			"is still dropped.", got, "text")
	}
}

// TestFocusGlowAppliesToEveryNodeType is the axis guard, and it is here
// because of a defect this engine has now produced twice.
//
// `when` was honoured in exactly two places — a row filtered its children and
// an overlay gated itself — so a gated node drew unconditionally anywhere
// else, and the axis that decided the outcome was the *parent* rather than the
// node. A style token had the same shape one turn later: four node types read
// their own style and the rest dropped it.
//
// focus_glow resolves a style, and twelve call sites in this renderer resolve
// n.Style. Implementing it at any of them would reproduce that defect exactly
// — the property would work on `text` because `text` is what a test reaches
// for first, and be silently absent on the other ten types. So the assertion
// is across node types, and the implementation belongs at the one function
// every node passes through.
func TestFocusGlowAppliesToEveryNodeType(t *testing.T) {
	// One document per type, each a focusable leaf that carries visible
	// text under a declared token. The types are the text-bearing ones:
	// a container has no text of its own to restyle, which is a different
	// question (its children keep their own tokens) and not this one.
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
				`{"id":"target","type":"` + tc.nodeType + `","style":{"style":"text"}` +
				`,"focus_glow":{"style":"bright"}` + tc.extra + `}]}}`

			doc, err := scene.ParseDocument([]byte(src))
			if err != nil {
				t.Fatalf("ParseDocument: %v", err)
			}
			if warns := doc.Warnings(); len(warns) > 0 {
				t.Fatalf("premise broken: the %s probe must carry no warnings, or the frame cannot be\n"+
					"attributed to focus_glow; got %v", tc.nodeType, warns)
			}

			r := Renderer{Width: 40, Height: 10}
			got := styleOfText(r.RenderFrame(doc, fold.State{UIFocus: "target"}), tc.want)
			if got != "bright" {
				t.Errorf("a focused %s node renders under token %q, want %q.\n"+
					"consequence: focus_glow is honoured on some node types and not others, so the\n"+
					"property works on whichever type the first test reached for and is silently absent\n"+
					"on the rest. That is the `when` defect of PR #12 and the style-token defect of\n"+
					"PR #11, both of which were honoured per node type instead of once.\n"+
					"remedy: resolve the glow in renderNode, the single function every node passes\n"+
					"through, rather than at any of the twelve sites that read n.Style.",
					tc.nodeType, got, "bright")
			}
		})
	}
}

// styleOfText returns the style token of the first span whose text contains
// want, or "" when no span does.
//
// It searches spans rather than asserting on a golden because the question is
// about one node's token, and a golden diff would answer it with the whole
// frame — which makes a failure report the layout rather than the property.
func styleOfText(f ui.Frame, want string) string {
	for _, line := range f.Live {
		for _, span := range line {
			if strings.Contains(span.Text, want) {
				return span.Style
			}
		}
	}
	return ""
}

// TestFocusGlowHonoursEveryStyleTokenSpelling closes the gap injection R20g
// found: every assertion above spells the token under "style", and the
// validator accepts two spellings.
//
// scene.StyleTokenKeys() is {"token", "style"} and styleName returns the first
// key that is set, so a glow that wrote only the canonical "style" key would
// leave a node spelling its token "token" wearing its original value. The
// property would work for the spelling the tests happened to use and be
// silently absent for the other — which is the two-spelling defect of PR #4,
// where the token validator read only "token" while every golden scene and
// the render path used "style". The lesson then was that the format's two
// spellings must be honoured wherever a token is read; this is the first place
// a token is *written*, and it inherits the same obligation.
//
// The cases are derived from StyleTokenKeys rather than listed, so a third
// spelling added to the format joins this test by existing. A hand-listed pair
// here would be the second inventory this package has watched drift five
// times.
func TestFocusGlowHonoursEveryStyleTokenSpelling(t *testing.T) {
	keys := scene.StyleTokenKeys()
	if len(keys) < 2 {
		t.Fatalf("scene.StyleTokenKeys() returned %v; this test exists because the format accepts\n"+
			"more than one spelling, and with fewer than two it would pass vacuously", keys)
	}

	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			src := `{"root":{"id":"root","type":"stack","children":[` +
				`{"id":"target","type":"text","text":"alpha","style":{"` + key + `":"text"},` +
				`"focus_glow":{"style":"bright"}}]}}`

			doc, err := scene.ParseDocument([]byte(src))
			if err != nil {
				t.Fatalf("ParseDocument: %v", err)
			}
			if warns := doc.Warnings(); len(warns) > 0 {
				t.Fatalf("premise broken: the %q probe must carry no warnings; got %v", key, warns)
			}

			r := Renderer{Width: 40, Height: 10}
			got := styleOfText(r.RenderFrame(doc, fold.State{UIFocus: "target"}), "alpha")
			if got != "bright" {
				t.Errorf("a focused node spelling its token under %q renders under %q, want %q.\n"+
					"consequence: the glow is written under one spelling while styleName reads the\n"+
					"first of %v that is set, so the property works for one spelling of a reference\n"+
					"the rest of the engine accepts in two — silently inert for the other. That is\n"+
					"the defect PR #4 fixed in the validator, arriving at the first place the engine\n"+
					"writes a token rather than reads one.\n"+
					"remedy: withFocusGlow must write the glow token under every key\n"+
					"scene.StyleTokenKeys() names.", key, got, "bright", keys)
			}
		})
	}
}
