package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// The behavioural half of F3: ui.hidden (BINDS.md §4.3, D3) is a set of node
// ids the walk drops. The bind audits prove the frame *moves* with the set;
// this proves it moves the right way — the named node and its subtree vanish,
// a sibling that was not named stays, and the set distinguishes one id from
// another rather than hiding a scalar's worth of "something".
//
// It renders a hand-built tree through renderNode rather than a golden, because
// the empty set is the default and moves no golden by design; the property
// under test only appears once the set is non-empty, which no shipped scene
// produces. It reuses renderToText (row_template_test.go), which flattens a
// node's frame to plain text at a fixed 80×24.
func TestUIHiddenDropsTheNamedNodeAndKeepsItsSiblings(t *testing.T) {
	tree := &scene.Node{Type: "stack", Children: []*scene.Node{
		{Type: "text", ID: "alpha", Text: "ALPHATEXT"},
		{Type: "text", ID: "beta", Text: "BETATEXT"},
	}}

	// Empty set: both draw, exactly as today — this is the no-op default that
	// moves no golden.
	both := renderToText(t, tree, fold.State{})
	if !strings.Contains(both, "ALPHATEXT") || !strings.Contains(both, "BETATEXT") {
		t.Fatalf("with an empty ui.hidden set both nodes must render, and one is missing.\n  frame:\n%s\nConsequence: the default set is not a no-op, so signing ui.hidden moved a scene that hides nothing.\nRemedy: hiddenByWhenRow must treat a node whose id is absent from the set as visible.", both)
	}

	// Hiding alpha must remove ALPHATEXT and leave BETATEXT. A scalar flag would
	// fail the second half — the whole reason D3 signed a set and not a bool.
	hidAlpha := renderToText(t, tree, fold.State{UIHidden: map[string]bool{"alpha": true}})
	if strings.Contains(hidAlpha, "ALPHATEXT") {
		t.Errorf("`/ui hide alpha` did not drop the node it names.\n  frame:\n%s\nConsequence: the visibility filter is not reading ui.hidden, so hide reports success and the node stays on screen.\nRemedy: hiddenByWhenRow must return true when n.ID is a member of state.UIHidden.", hidAlpha)
	}
	if !strings.Contains(hidAlpha, "BETATEXT") {
		t.Errorf("hiding `alpha` also removed `beta`.\n  frame:\n%s\nConsequence: this is the scalar-flag defect D3 exists to prevent — `/ui hide a` unhides or hides `b` because the set does not distinguish ids.\nRemedy: filter on set membership of each node's own id, not on a single scalar.", hidAlpha)
	}
}

func TestUIHiddenDropsTheWholeSubtree(t *testing.T) {
	tree := &scene.Node{Type: "stack", Children: []*scene.Node{
		{Type: "stack", ID: "panel", Children: []*scene.Node{
			{Type: "text", Text: "CHILDONE"},
			{Type: "text", Text: "CHILDTWO"},
		}},
		{Type: "text", ID: "keep", Text: "KEEPTEXT"},
	}}

	out := renderToText(t, tree, fold.State{UIHidden: map[string]bool{"panel": true}})
	for _, gone := range []string{"CHILDONE", "CHILDTWO"} {
		if strings.Contains(out, gone) {
			t.Errorf("hiding container `panel` left its child %q on screen.\n  frame:\n%s\nConsequence: D3 signs that a hidden id drops the node *and its subtree*; a filter that hid only the container's own chrome would leak the children it was meant to remove.\nRemedy: renderNode returns an empty frame for a hidden node before it recurses into children, so the subtree never renders.", gone, out)
		}
	}
	if !strings.Contains(out, "KEEPTEXT") {
		t.Errorf("hiding `panel` also dropped the unrelated sibling `keep`.\n  frame:\n%s\nConsequence: the drop reached past the named subtree.\nRemedy: only the node whose id is in the set and its descendants are removed.", out)
	}
}

func TestUIHiddenComposesWithWhenByConjunction(t *testing.T) {
	// A node gated on a truthy `when` renders; hiding its id must still drop it,
	// because the two conditions compose by conjunction — either removes the
	// node. host.escape.armed is a signed bool bind the fold carries, used here
	// as a `when` that is satisfiable without a running agent.
	node := &scene.Node{Type: "text", ID: "armed", When: "host.escape.armed", Text: "ARMEDTEXT"}

	shown := renderToText(t, node, fold.State{EscapeArmed: true})
	if !strings.Contains(shown, "ARMEDTEXT") {
		t.Fatalf("the node's `when` is truthy and it did not render, so the test cannot isolate the hide.\n  frame:\n%s\nRemedy: check the chosen `when` bind is satisfiable.", shown)
	}

	hidden := renderToText(t, node, fold.State{EscapeArmed: true, UIHidden: map[string]bool{"armed": true}})
	if strings.Contains(hidden, "ARMEDTEXT") {
		t.Errorf("a node whose `when` is truthy still rendered after its id was hidden.\n  frame:\n%s\nConsequence: ui.hidden is being ignored when `when` passes, so `/ui hide` cannot override a node the scene chose to show — the manual override D3 signed does not compose with the scene's own condition.\nRemedy: hiddenByWhenRow returns true when the id is in the set regardless of the `when` result.", hidden)
	}
}
