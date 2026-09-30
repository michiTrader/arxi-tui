package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// renderButtonNode renders a single button in isolation and returns its one
// styled span, so a test can assert on both the text drawn and the token it
// carries. It renders through RenderFrame (not renderButton directly) because
// the focus glow this file also checks is applied at the renderNode chokepoint,
// not in renderButton, and a test that bypassed the chokepoint would report a
// glow that never reaches a real frame.
func renderButtonSpan(t *testing.T, src string, state fold.State) (text, style string) {
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
	t.Fatalf("button rendered no non-blank span from %q", src)
	return "", ""
}

// A button frames its label so the eye can tell a control from prose.
//
// The whole reason `button` is a node type rather than a `text` with an
// on_press is the affordance: on_press is honoured on any node and the focus
// ring Tabs onto anything pressable, so nothing the press path does is unique
// to a button. What a button adds is the frame the renderer draws around the
// label — the mark that says "this word is pressable" — and if the engine drew
// the bare label the type would be indistinguishable from a styled text node,
// which is the thing Scene 8 exists to make impossible.
func TestButtonFramesItsLabel(t *testing.T) {
	text, _ := renderButtonSpan(t,
		`{"root":{"type":"button","id":"ok","text":"Approve","on_press":"answer:approve"}}`,
		fold.State{})
	if !strings.Contains(text, "Approve") {
		t.Errorf("a button labelled \"Approve\" drew %q, which drops the label; a button with no\n"+
			"visible label is a control the user cannot read.", text)
	}
	if text == "Approve" {
		t.Errorf("a button drew its label %q with no frame around it, so it renders byte-identically\n"+
			"to a text node carrying the same string.\n"+
			"consequence: the one thing a button adds over a text-with-on_press is the affordance that\n"+
			"marks it pressable; without it the type is decoration and Scene 8's buttons read as prose.\n"+
			"remedy: keep the bracket frame renderButton draws around the label.", text)
	}
}

// The label reads from bind before text, the precedence renderText keeps, so a
// button labelled by the fold (a row field, a live count) draws the resolved
// value rather than a stale literal.
func TestButtonReadsBindBeforeText(t *testing.T) {
	state := fold.State{ModelName: "opus"}
	text, _ := renderButtonSpan(t,
		`{"root":{"type":"button","id":"m","text":"placeholder","bind":"model.name","on_press":"cmd:/model"}}`,
		state)
	if strings.Contains(text, "placeholder") {
		t.Errorf("a button with both bind and text drew %q, which used the literal text; a bound\n"+
			"button must draw the fold's value the way renderText resolves bind before text, or a\n"+
			"button label can never track live state.", text)
	}
	if !strings.Contains(text, "opus") {
		t.Errorf("a button bound to model.name drew %q, which never resolved the bind to \"opus\".", text)
	}
}

// The focus glow reaches a button because renderButton reads n.Style, and the
// glow arrives as a rewritten n.Style at the renderNode chokepoint every node
// passes through. This is the property withFocusGlow's comment protects — "some
// node types glow and others do not" must stay unrepresentable — verified on the
// type Scene 8 exists to make focusable.
func TestButtonHonoursItsFocusGlow(t *testing.T) {
	src := `{"root":{"type":"button","id":"ok","text":"Approve","on_press":"answer:approve",` +
		`"focus_glow":{"style":"glow"}}}`

	_, unfocused := renderButtonSpan(t, src, fold.State{})
	if unfocused == "glow" {
		t.Fatalf("an unfocused button already wore its focus_glow token %q; the glow is keyed to\n"+
			"ui.focus and must not be applied when nothing is focused, or every button glows always.", unfocused)
	}

	_, focused := renderButtonSpan(t, src, fold.State{UIFocus: "ok"})
	if focused != "glow" {
		t.Errorf("a focused button's label span carried style %q, not the focus_glow token \"glow\".\n"+
			"consequence: a button is the node type Scene 8 makes focusable, and if the glow the\n"+
			"chokepoint writes into n.Style is dropped by renderButton the focused button looks\n"+
			"identical to an unfocused one — the user cannot see which control Enter will press.\n"+
			"remedy: apply styleName(n.Style) to the framed span, so the rewritten glow token lands.", focused)
	}
}
