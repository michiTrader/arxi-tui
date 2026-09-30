package main

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// twoMemberFold is the witness fold for the ring tests: two team members, so a
// row_template over team.members instantiates two rows and a per-row focus key
// must be produced for each. It mirrors the engine's twoMemberState, which the
// cmd package cannot reach across the test boundary.
func twoMemberFold() fold.State {
	return fold.State{
		TeamMembers: []fold.TeamMember{
			{ID: "be", Role: "backend", State: "thinking", Busy: true, Turns: 3},
			{ID: "fe", Role: "frontend", State: "idle", Busy: false, Turns: 1},
		},
	}
}

// The ring interleaves ordinary pressable ids with one rowFocusKey per pressable
// node of each instantiated row, in document order and row-major within the
// template. This is the whole point of the increment: a template's authored id
// repeats across rows, so it must expand to one distinct ring slot per row, not
// a single raw id that names all rows at once.
func TestFocusRingInterleavesPlainIDsAndPerRowKeys(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"stack","children":[
	  {"id":"top","type":"text","text":"Top","on_press":"cmd:/top"},
	  {"id":"list","type":"list","bind":"team.members",
	   "row_template":{"id":"go","type":"text","bind":"row.role","on_press":"cmd:/agent {row.id}"}},
	  {"id":"bottom","type":"text","text":"Bot","on_press":"cmd:/bottom"}
	]}}`)

	got, err := focusRing(doc, twoMemberFold(), nil)
	if err != nil {
		t.Fatalf("focusRing refused a document whose {row.id} is in every scope: %v\n"+
			"consequence: a subagent list no row of which can be Tabbed to.\n"+
			"remedy: enumerate RowPresses and key each into the ring.", err)
	}
	want := []string{"top", rowFocusKey("go", 0), rowFocusKey("go", 1), "bottom"}
	if strings.Join(got, "\x1e") != strings.Join(want, "\x1e") {
		t.Errorf("focusRing = %v, want %v\n"+
			"consequence: Tab either skips the instantiated rows (a visible button that cannot be\n"+
			"focused), collapses two rows onto one raw id (pressing row 1 dispatches row 0), or\n"+
			"reads out of document order.\n"+
			"remedy: emit each plain pressable id in place and one rowFocusKey per RowPress, row-major.",
			got, want)
	}
}

// A hidden subtree contributes nothing to the ring, template rows included: focus
// must not land on something not on screen (D3). Hiding the list by its id must
// drop both its raw id and every per-row key beneath it.
func TestFocusRingSkipsHiddenTemplate(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"stack","children":[
	  {"id":"top","type":"text","text":"Top","on_press":"cmd:/top"},
	  {"id":"list","type":"list","bind":"team.members",
	   "row_template":{"id":"go","type":"text","bind":"row.role","on_press":"cmd:/agent {row.id}"}}
	]}}`)

	got, err := focusRing(doc, twoMemberFold(), map[string]bool{"list": true})
	if err != nil {
		t.Fatalf("focusRing errored while skipping a hidden template: %v", err)
	}
	want := []string{"top"}
	if strings.Join(got, "\x1e") != strings.Join(want, "\x1e") {
		t.Errorf("focusRing = %v, want %v\n"+
			"consequence: Tab lands on a row inside a hidden list — a focus target the user cannot see.\n"+
			"remedy: a hidden node's whole subtree, template rows included, is skipped.", got, want)
	}
}

// A row_template on_press interpolating a field its scope lacks is a scope/schema
// drift RowPresses refuses, and focusRing must propagate that refusal rather than
// silently omit the row — an omitted ring slot is a button the user sees but can
// never Tab to. Built as a raw node so the load-time validator (which would
// refuse the field first) does not mask the runtime path.
func TestFocusRingPropagatesRowScopeError(t *testing.T) {
	doc := &scene.Document{Root: &scene.Node{
		Type: "stack",
		Children: []*scene.Node{
			{ID: "list", Type: "list", Bind: "team.members",
				RowTemplate: &scene.Node{ID: "go", Type: "text", OnPress: "cmd:/agent {row.nope}"}},
		},
	}}
	if _, err := focusRing(doc, twoMemberFold(), nil); err == nil {
		t.Error("focusRing swallowed a missing-field row interpolation and returned a ring anyway\n" +
			"consequence: the drifted row is silently dropped from the ring, so a button the user\n" +
			"can see is one Tab can never reach, and the schema drift goes unreported.\n" +
			"remedy: propagate RowPresses' refusal.")
	}
}

// rowPressOnPress recovers the already-expanded on_press for the (node, row) a
// rowFocusKey decoded to — the row-side twin of findPressable. Each row must
// resolve to its own row's command, or Enter on a focused row dispatches the
// wrong element.
func TestRowPressOnPressRecoversPerRowAction(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"list","bind":"team.members",
	  "row_template":{"id":"go","type":"text","bind":"row.role","on_press":"cmd:/agent {row.id}"}}}`)

	cases := []struct {
		row  int
		want string
	}{
		{0, "cmd:/agent be"},
		{1, "cmd:/agent fe"},
	}
	for _, c := range cases {
		got, found, err := rowPressOnPress(doc, twoMemberFold(), "go", c.row)
		if err != nil || !found || got != c.want {
			t.Errorf("rowPressOnPress(go,%d) = (%q,found=%v,err=%v); want (%q,true,nil)\n"+
				"consequence: Enter on a focused row dispatches the wrong row's on_press or none.\n"+
				"remedy: match the (NodeID, RowIndex) pair against RowPresses and return its expanded action.",
				c.row, got, found, err, c.want)
		}
	}
}

// A pair naming no instantiated row (a stale focus key, or an index past the
// array) resolves to found=false, not row 0: the dispatcher must be able to tell
// "this row target no longer exists" from "here is a real action", or a shrunk
// list dispatches against a row that is gone.
func TestRowPressOnPressReportsMissingPair(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"list","bind":"team.members",
	  "row_template":{"id":"go","type":"text","bind":"row.role","on_press":"cmd:/agent {row.id}"}}}`)

	for _, tc := range []struct {
		nodeID string
		row    int
	}{
		{"go", 5},      // index past the two members
		{"missing", 0}, // no such node in any template
	} {
		if got, found, err := rowPressOnPress(doc, twoMemberFold(), tc.nodeID, tc.row); found || err != nil {
			t.Errorf("rowPressOnPress(%q,%d) = (%q,found=%v,err=%v); want (\"\",false,nil)\n"+
				"consequence: a stale or out-of-range row target resolves to a real action, dispatching\n"+
				"a press against a row the user never focused.\n"+
				"remedy: only an exact (NodeID, RowIndex) match reports found.", tc.nodeID, tc.row, got, found, err)
		}
	}
}
