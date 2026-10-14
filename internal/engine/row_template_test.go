package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// twoMemberState is the witness fold for these tests: one busy backend member
// and one idle frontend member, so a per-row `when: "row.busy"` has both a true
// and a false row to gate, and the two members' fields are distinguishable in
// the frame.
func twoMemberState() fold.State {
	return fold.State{
		TeamMembers: []fold.TeamMember{
			{ID: "be", Role: "backend", State: "thinking", Busy: true, Turns: 3},
			{ID: "fe", Role: "frontend", State: "idle", Busy: false, Turns: 1},
		},
	}
}

func renderToText(t *testing.T, n *scene.Node, state fold.State) string {
	t.Helper()
	r := &Renderer{Width: 80, Height: 24}
	var b strings.Builder
	for _, l := range r.renderNode(n, state, 24).Live {
		b.WriteString(l.Text())
		b.WriteString("\n")
	}
	return b.String()
}

// TestRowTemplateDrawsOneRowPerElementWithItsFields is the positive half of E:
// a list's row_template is instantiated once per array element and each
// `row.<field>` resolves to that element's value (BINDS.md §4.7). It is the
// rendering the row_template refusal was lifted to permit.
func TestRowTemplateDrawsOneRowPerElementWithItsFields(t *testing.T) {
	list := &scene.Node{
		Type: "list", Bind: "team.members",
		RowTemplate: &scene.Node{Type: "text", Bind: "row.role"},
	}
	got := renderToText(t, list, twoMemberState())

	for _, want := range []string{"backend", "frontend"} {
		if !strings.Contains(got, want) {
			t.Errorf("row_template over team.members did not render member field %q; got:\n%s\n"+
				"consequence: the template is not being instantiated per element, or row.* is not\n"+
				"resolving against the element — the whole point of D1.\n"+
				"remedy: renderRowTemplate must set the row scope per element and resolveBindRow must read it.", want, got)
		}
	}
	if strings.Contains(got, placeholderValue) {
		t.Errorf("a row.<field> rendered as the placeholder %q; got:\n%s\n"+
			"consequence: the relative bind resolved to nothing, so the row is empty while the list\n"+
			"reports rows — the silent-drop shape row_template was refused to avoid.\n"+
			"remedy: rowScopesFor must supply the field under the row.<field> key resolveBindRow reads.", placeholderValue, got)
	}
}

// TestRowTemplateEmptyArrayDrawsNothing pins the signed empty state: a list over
// an empty array renders zero rows, not a placeholder row. team.members' empty
// state (BINDS.md §4.1) is "the subagent list does not render".
func TestRowTemplateEmptyArrayDrawsNothing(t *testing.T) {
	list := &scene.Node{
		Type: "list", Bind: "team.members",
		RowTemplate: &scene.Node{Type: "text", Bind: "row.role"},
	}
	frame := (&Renderer{Width: 80, Height: 24}).renderNode(list, fold.State{}, 24)
	if frame.Height != 0 {
		t.Errorf("an empty team.members rendered %d row(s); want 0\n"+
			"consequence: a list with no members drew chrome the fold says is not there.\n"+
			"remedy: rowScopesFor returns no scopes for an empty array, so the template draws nothing.", frame.Height)
	}
}

// TestPerRowWhenGatesAgainstTheRowsField proves a `when: "row.busy"` inside a
// template gates each row against its own element, not against host state
// (Scene 9's per-row spinner-or-glyph). The busy member shows the gated node;
// the idle one does not.
func TestPerRowWhenGatesAgainstTheRowsField(t *testing.T) {
	list := &scene.Node{
		Type: "list", Bind: "team.members",
		RowTemplate: &scene.Node{Type: "row", Children: []*scene.Node{
			{Type: "text", Bind: "row.role"},
			{Type: "text", When: "row.busy", Text: "WORKING"},
		}},
	}
	got := renderToText(t, list, twoMemberState())

	if n := strings.Count(got, "WORKING"); n != 1 {
		t.Errorf("the per-row `when: row.busy` node appeared %d time(s); want 1 (only the busy member)\ngot:\n%s\n"+
			"consequence: a per-row gate is reading host state instead of the row, so every row shows\n"+
			"the same thing — the row context is not reaching hiddenByWhenRow.\n"+
			"remedy: renderNode must gate through hiddenByWhenRow with the Renderer's current row.", n, got)
	}
}

// TestRelativeBindOutsideATemplateIsFalsy is the counterfactual for the scope:
// a row.* bind rendered with no row in scope must degrade to the placeholder,
// never to a value borrowed from some ambient row. This is what makes a
// relative bind that leaks outside a template a visible gap rather than a wrong
// value, and it fails the moment resolveBindRow stops treating a nil row as
// falsy.
func TestRelativeBindOutsideATemplateIsFalsy(t *testing.T) {
	got := resolveBindRow("row.role", twoMemberState(), nil, nil, nil)
	if got != placeholderValue {
		t.Errorf("row.role with no row in scope resolved to %q; want the placeholder %q\n"+
			"consequence: a relative bind outside a template would render an ambient value, which is a\n"+
			"wrong value rather than an honest gap.\n"+
			"remedy: resolveBindRow returns the placeholder for a row.* bind when the row is nil.", got, placeholderValue)
	}
}

// D1, option b: {row.<field>} in drawn text is substituted per row. It is the case the
// first live eval run measured, `"text": "{row.turns}•"`, drawing its value now instead
// of the braces.
func TestRowReferenceInTextDrawsEachRowsOwnValue(t *testing.T) {
	list := &scene.Node{
		Type: "list", Bind: "team.members",
		RowTemplate: &scene.Node{Type: "text", Text: "{row.role}:{row.turns}•"},
	}
	got := renderToText(t, list, twoMemberState())
	if got != "backend:3•\nfrontend:1•\n" {
		t.Errorf("rows = %q, want each row to carry its own role and turns", got)
	}
	if strings.Contains(got, "{row.") {
		t.Errorf("braces reached the screen: %q", got)
	}
	// Rendering must not edit the document: the second frame is the same as the first.
	if again := renderToText(t, list, twoMemberState()); again != got {
		t.Errorf("a second frame differs: %q vs %q (the template node was mutated)", again, got)
	}
	if list.RowTemplate.Text != "{row.role}:{row.turns}•" {
		t.Errorf("the template's own text was rewritten to %q", list.RowTemplate.Text)
	}
}

// The reference works in a title, and inside a nested node of the row.
func TestRowReferenceInATitleAndInANestedNodeIsSubstituted(t *testing.T) {
	list := &scene.Node{
		Type: "list", Bind: "team.members",
		RowTemplate: &scene.Node{Type: "box", Title: "{row.id}", BorderRaw: []byte(`"single"`), Children: []*scene.Node{
			{Type: "text", Text: "turns {row.turns}"},
		}},
	}
	got := renderToText(t, list, twoMemberState())
	for _, want := range []string{"be", "fe", "turns 3", "turns 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "{row.") {
		t.Errorf("braces reached the screen:\n%s", got)
	}
}

// The doubled brace draws the characters, and a text without references is untouched.
func TestTheEscapeDrawsTheBracesAndPlainTextIsUntouched(t *testing.T) {
	list := &scene.Node{
		Type: "list", Bind: "team.members",
		RowTemplate: &scene.Node{Type: "text", Text: "{{row.id}}={row.id} {x}"},
	}
	got := renderToText(t, list, twoMemberState())
	if got != "{row.id}=be {x}\n{row.id}=fe {x}\n" {
		t.Errorf("got %q", got)
	}
}

// With no row in scope the text is drawn as authored (a scene without templates must not
// move), which is also why the validator refuses a reference there.
func TestTextOutsideARowIsDrawnAsAuthored(t *testing.T) {
	got := renderToText(t, &scene.Node{Type: "text", Text: "{row.id} {{row.id}}"}, twoMemberState())
	if got != "{row.id} {{row.id}}\n" {
		t.Errorf("got %q", got)
	}
}
