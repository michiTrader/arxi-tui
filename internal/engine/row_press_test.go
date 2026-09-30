package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// RowPresses is the pure per-row on_press resolver the deferred focus-ring wiring
// consumes. These tests pin what it enumerates and, above all, that it expands
// {row.field} against the right row and refuses a field the scope lacks rather
// than dropping the press -- the silent-drop shape row_template exists to avoid.

// One pressable node in a template yields one RowPress per instantiated row, each
// carrying its own row's expanded on_press. The two members resolve to two
// distinct commands, so a regression that shared one row's scope across all rows
// (or expanded against nothing) is caught by the per-row id in the command.
func TestRowPressesResolvesOnePressPerRow(t *testing.T) {
	list := &scene.Node{
		Type: "list", Bind: "team.members",
		RowTemplate: &scene.Node{Type: "text", ID: "go", OnPress: "cmd:/agent {row.id}"},
	}
	got, err := RowPresses(list, twoMemberState())
	if err != nil {
		t.Fatalf("RowPresses refused a template whose {row.id} is in every scope: %v\n"+
			"consequence: a subagent list no row of which can be pressed.\n"+
			"remedy: expand each row's on_press against that row's scope.", err)
	}
	want := []RowPress{
		{RowIndex: 0, NodeID: "go", OnPress: "cmd:/agent be"},
		{RowIndex: 1, NodeID: "go", OnPress: "cmd:/agent fe"},
	}
	if len(got) != len(want) {
		t.Fatalf("RowPresses returned %d press(es), want %d: one pressable node over two members "+
			"is two row presses, and a wrong count means a row was dropped or duplicated\ngot: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("RowPresses[%d] = %+v, want %+v: the (row, node) identity and the expanded on_press "+
				"must name the element the press dispatches against", i, got[i], want[i])
		}
	}
}

// Several pressable nodes in one row (Scene 8's approve/reject/reply) are all
// enumerated, in document order within a row and row-major across rows. The same
// authored id repeats across rows, so the (RowIndex, NodeID) pair is what
// distinguishes row 0's approve from row 1's -- a regression to node id alone
// would collapse them. answer:* carries no interpolation and must pass through
// unchanged beside the interpolated cmd:.
func TestRowPressesWalksEveryPressableInRowMajorDocumentOrder(t *testing.T) {
	list := &scene.Node{
		Type: "list", Bind: "team.members",
		RowTemplate: &scene.Node{Type: "row", Children: []*scene.Node{
			{Type: "text", Bind: "row.role"}, // not pressable: no on_press, must be skipped
			{Type: "button", ID: "approve", OnPress: "answer:approve"},
			{Type: "button", ID: "open", OnPress: "cmd:/agent {row.id}"},
		}},
	}
	got, err := RowPresses(list, twoMemberState())
	if err != nil {
		t.Fatalf("RowPresses refused a multi-button row: %v", err)
	}
	want := []RowPress{
		{RowIndex: 0, NodeID: "approve", OnPress: "answer:approve"},
		{RowIndex: 0, NodeID: "open", OnPress: "cmd:/agent be"},
		{RowIndex: 1, NodeID: "approve", OnPress: "answer:approve"},
		{RowIndex: 1, NodeID: "open", OnPress: "cmd:/agent fe"},
	}
	if len(got) != len(want) {
		t.Fatalf("RowPresses returned %d press(es), want %d: two pressable nodes over two members is four, "+
			"and the non-pressable text node must not be counted\ngot: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("RowPresses[%d] = %+v, want %+v: presses must read row-major, in document order within "+
				"a row, so the focus ring Tabs through them in the order they are drawn", i, got[i], want[i])
		}
	}
}

// A node with no row_template has no rows to press and yields nothing (no error):
// RowPresses is asked of every list, and a plain list without a template is the
// common case, so it must be a clean empty answer rather than a refusal.
func TestRowPressesNoTemplateYieldsNothing(t *testing.T) {
	got, err := RowPresses(&scene.Node{Type: "list", Bind: "team.members"}, twoMemberState())
	if err != nil {
		t.Fatalf("RowPresses errored on a template-less list: %v; a list with no row_template has no row "+
			"presses, which is an empty answer, not a failure", err)
	}
	if len(got) != 0 {
		t.Fatalf("RowPresses returned %d press(es) for a template-less list; want 0\ngot: %+v", len(got), got)
	}
}

// An empty array yields no presses: a list over no members has no rows, so it
// contributes nothing to the ring rather than a phantom press keyed to a row that
// is not drawn.
func TestRowPressesEmptyArrayYieldsNothing(t *testing.T) {
	list := &scene.Node{
		Type: "list", Bind: "team.members",
		RowTemplate: &scene.Node{Type: "text", ID: "go", OnPress: "cmd:/agent {row.id}"},
	}
	got, err := RowPresses(list, fold.State{})
	if err != nil {
		t.Fatalf("RowPresses errored on an empty team.members: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("RowPresses returned %d press(es) for an empty array; want 0: no members, no row presses\ngot: %+v", len(got), got)
	}
}

// A {row.<field>} the scope does not carry is an error that names the field, not
// a dropped press. This is the load-bearing guard: rowScopesFor supplies every
// signed field, so the only way to reach it is an on_press interpolating an
// unsigned field -- which the validator refuses at load, but RowPresses must not
// silently swallow, because a press that resolves to nothing is a button that
// looks present and does nothing.
func TestRowPressesMissingFieldIsAnError(t *testing.T) {
	list := &scene.Node{
		Type: "list", Bind: "team.members",
		RowTemplate: &scene.Node{Type: "text", ID: "go", OnPress: "cmd:/agent {row.nope}"},
	}
	got, err := RowPresses(list, twoMemberState())
	if err == nil {
		t.Fatalf("RowPresses resolved {row.nope} against team.members and returned %+v with no error\n"+
			"consequence: a press whose field the scope lacks is expanded to the empty string or dropped, so a\n"+
			"button that looks present dispatches nothing and the scope/schema drift is invisible.\n"+
			"remedy: propagate ExpandRowInterpolation's error naming the missing field.", got)
	}
	if got != nil {
		t.Fatalf("RowPresses returned %+v alongside its error; a refused enumeration must yield no presses so a "+
			"caller that ignores the error cannot Tab onto a half-resolved row", got)
	}
	if !strings.Contains(err.Error(), "row.nope") {
		t.Fatalf("RowPresses error %q does not name the missing field; the message must say which {row.<field>} "+
			"drifted so the scene author or the scope builder can be fixed", err)
	}
}
