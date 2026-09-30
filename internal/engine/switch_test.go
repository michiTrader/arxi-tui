package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// renderSwitchSpan renders a single switch in isolation and returns its one
// styled span. Like renderButtonSpan it goes through RenderFrame, not
// renderSwitch directly, so the focus glow this file checks — applied at the
// renderNode chokepoint — is exercised on the real path rather than bypassed.
func renderSwitchSpan(t *testing.T, src string, state fold.State) (text, style string) {
	t.Helper()
	doc, err := scene.ParseDocument([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	r := &Renderer{Width: 40, Height: 4}
	for _, line := range r.RenderFrame(doc, state).Live {
		for _, span := range line {
			if strings.TrimSpace(span.Text) != "" {
				return span.Text, span.Style
			}
		}
	}
	t.Fatalf("switch rendered no non-blank span from %q", src)
	return "", ""
}

// A switch shows whether it is set, and its state comes from its bind through
// the same rule a `when` gate uses. That shared rule is the decision this test
// protects: a switch bound to a boolean and a gate reading the same boolean must
// agree on what "on" means, or a settings row could draw a checked box for a
// value a `when` two lines away treats as off.
func TestSwitchReflectsBoundState(t *testing.T) {
	src := `{"root":{"type":"switch","id":"w","bind":"agent.working"}}`

	off, _ := renderSwitchSpan(t, src, fold.State{AgentWorking: false})
	if !strings.Contains(off, "[ ]") {
		t.Errorf("a switch bound to a false value drew %q, not the empty box [ ]; an off toggle that\n"+
			"does not read empty tells the user a setting is on when it is off.", off)
	}

	on, _ := renderSwitchSpan(t, src, fold.State{AgentWorking: true})
	if !strings.Contains(on, "[x]") {
		t.Errorf("a switch bound to a true value drew %q, not the checked box [x].\n"+
			"consequence: the switch does not read its state through evalWhenRow, so it cannot track\n"+
			"the boolean it binds — a settings toggle frozen on one state is not a toggle.\n"+
			"remedy: resolve n.Bind through evalWhenRow, the same truthiness a `when` gate uses.", on)
	}
	if off == on {
		t.Errorf("a switch drew the same span %q for a true and a false bind; the two states are\n"+
			"indistinguishable, so the toggle shows nothing.", off)
	}
}

// The optional text label is drawn before the box, so a single switch node can
// name its own setting rather than always needing a sibling text node to label
// it. A row_template that composes the label elsewhere leaves text empty and
// gets the bare indicator — the property the bare-box arm of this test pins.
func TestSwitchDrawsLabelBeforeBox(t *testing.T) {
	labelled, _ := renderSwitchSpan(t,
		`{"root":{"type":"switch","id":"w","text":"Dark mode","bind":"agent.working"}}`,
		fold.State{AgentWorking: true})
	if !strings.Contains(labelled, "Dark mode") {
		t.Errorf("a switch with text \"Dark mode\" drew %q, dropping its label; a labelled switch\n"+
			"that shows only a box is a setting the user cannot name.", labelled)
	}
	if strings.Index(labelled, "Dark mode") > strings.Index(labelled, "[x]") {
		t.Errorf("a switch drew its box before its label (%q); the label reads as belonging to the\n"+
			"next row rather than this toggle.", labelled)
	}

	bare, _ := renderSwitchSpan(t,
		`{"root":{"type":"switch","id":"w","bind":"agent.working"}}`,
		fold.State{AgentWorking: false})
	if strings.TrimSpace(bare) != "[ ]" {
		t.Errorf("a switch with no text drew %q, not the bare box [ ]; a template that labels its\n"+
			"rows separately must be able to place a switch that is only the indicator.", bare)
	}
}

// The focus glow reaches a switch for the same reason it reaches a button:
// renderSwitch reads n.Style, and the glow arrives as a rewritten n.Style at the
// renderNode chokepoint. A settings screen Tabs between switch and input rows,
// so a switch that dropped the glow would leave the user unable to see which row
// Enter (or a toggle key) will act on.
func TestSwitchHonoursItsFocusGlow(t *testing.T) {
	src := `{"root":{"type":"switch","id":"w","bind":"agent.working","focus_glow":{"style":"glow"}}}`

	_, unfocused := renderSwitchSpan(t, src, fold.State{})
	if unfocused == "glow" {
		t.Fatalf("an unfocused switch already wore its focus_glow token %q; the glow is keyed to\n"+
			"ui.focus and must not apply when nothing is focused.", unfocused)
	}

	_, focused := renderSwitchSpan(t, src, fold.State{UIFocus: "w"})
	if focused != "glow" {
		t.Errorf("a focused switch's span carried style %q, not the focus_glow token \"glow\".\n"+
			"consequence: the switch drops the glow the chokepoint writes into n.Style, so a focused\n"+
			"settings toggle looks identical to an unfocused one.\n"+
			"remedy: apply styleName(n.Style) to the switch's span.", focused)
	}
}
