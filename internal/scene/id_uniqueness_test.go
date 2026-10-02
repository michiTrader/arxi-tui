package scene

import (
	"errors"
	"strings"
	"testing"
)

// These tests pin the ADDRESSING.md §3 id-uniqueness invariant: within one
// document, no two nodes may carry the same non-empty id. F1/F2 first enforced
// uniqueness at the /ui add/move verb boundary; §3 says it is a *load-time*
// invariant because an id is an address that several read-path features resolve
// against — focus_glow keys on `id == ui.focus`, ui.max / `cmd:/max <pane>`
// address a pane by id — so a hand-written document with a duplicate is as unsafe
// as one a verb would have produced, independent of the write path. The refusal
// carries file:line and names both offending nodes.

// TestDuplicateIDIsRefusedAtLoadTime is the §3 sentence as a test: two nodes
// sharing a non-empty id fail Validate, and the refusal is addressed. The lines
// are asserted exactly — an address that points at the wrong node is worse than
// none, since in Phase 2 the eval corpus measures the repair loop and a
// misaddressed error trains the model to edit a line that was already correct.
func TestDuplicateIDIsRefusedAtLoadTime(t *testing.T) {
	src := []byte(`{
  "root": { "type": "stack", "children": [
    { "id": "dup", "type": "text" },
    { "id": "dup", "type": "text" }
  ]}
}`)
	doc, err := ParseNamed("scene.json", src)
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	err = doc.Validate()
	if err == nil {
		t.Fatal("Validate accepted two nodes with the same id\n" +
			"consequence: ADDRESSING.md §3 makes a duplicate id a load-time refusal because an id is an address (focus, ui.max, /ui move); accepting one leaves every id-addressed target ambiguous — two nodes double-glow or fight over the maximised slot.\n" +
			"remedy: keep the validateIDs pass in Validate.")
	}

	var sceneErr *Error
	if !errors.As(err, &sceneErr) {
		t.Fatalf("Validate returned %T, want *scene.Error\n"+
			"consequence: a bare error cannot carry an address, so invariant 4 degrades silently.\n"+
			"remedy: return *scene.Error from the uniqueness refusal.", err)
	}
	// The refusal points at the second occurrence (the node that introduces the
	// collision, line 4) and names the first (line 3) in the message.
	if got, want := sceneErr.Loc.Line, 4; got != want {
		t.Errorf("refusal points at line %d, want %d (the node that re-declares the id)\n"+
			"consequence: the reader is sent to the wrong line; the eval corpus then trains the model to patch a node that was already correct.\n"+
			"remedy: set the Error Loc to the second occurrence's path.", got, want)
	}
	for _, want := range []string{"dup", "scene.json:3", "scene.json:4"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("formatted refusal = %q, want it to contain %q\n"+
				"consequence: §3 requires the id and both offending nodes be named; a message missing either leaves the author guessing which pair collided.\n"+
				"remedy: name the id and both locations in the uniqueness message.", err.Error(), want)
		}
	}
}

// TestDuplicateIDAcrossTemplateIsRefused proves the uniqueness walk reaches
// beyond children into a row_template — a node whose id collides with a child is
// refused even though it is only reached through the template branch. An id
// inside a template is as much an address as a child's, and a children-only walk
// would silently miss it. (The list binds a real array-of-objects so the
// row_template is itself legal; the collision is the point, not the template.)
func TestDuplicateIDAcrossTemplateIsRefused(t *testing.T) {
	src := []byte(`{
  "root": { "type": "stack", "children": [
    { "id": "clash", "type": "text" },
    { "type": "list", "bind": "team.members",
      "row_template": { "id": "clash", "type": "text" } }
  ]}
}`)
	doc, err := ParseNamed("scene.json", src)
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	err = doc.Validate()
	if err == nil {
		t.Fatal("Validate accepted a duplicate id reached through a row_template\n" +
			"consequence: a node reached only through prefix/suffix/row_template carries an addressable id too; a children-only uniqueness walk lets a collision slip in through the template branch.\n" +
			"remedy: keep collectIDs recursing into prefix, suffix and row_template.")
	}
	if !strings.Contains(err.Error(), "clash") {
		t.Errorf("refusal = %q, want it to name the colliding id %q", err.Error(), "clash")
	}
}

// TestUniqueAndEmptyIDsValidate is the invariant's other half: distinct ids are
// fine, and the empty id is exempt — an unnamed node is not addressable, and most
// nodes legitimately carry none, so any number of them may coexist. A uniqueness
// check that treated "" as an id would reject nearly every real scene.
func TestUniqueAndEmptyIDsValidate(t *testing.T) {
	src := []byte(`{
  "root": { "type": "stack", "children": [
    { "type": "text" },
    { "type": "text" },
    { "id": "a", "type": "text" },
    { "id": "b", "type": "text" }
  ]}
}`)
	doc, err := ParseNamed("scene.json", src)
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("Validate rejected a document with distinct ids and several empty ids: %v\n"+
			"consequence: treating the empty id as a collidable address, or flagging genuinely distinct ids, would reject nearly every real scene.\n"+
			"remedy: skip the empty id and key uniqueness on the exact id string.", err)
	}
}
