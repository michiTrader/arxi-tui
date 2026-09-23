package patch

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// moveOK runs one move command against the shipped scene and fails loudly if it
// is refused, so a happy-path test reads as one assertion rather than repeated
// error handling.
func moveOK(t *testing.T, line string) *scene.Document {
	t.Helper()
	name, src := sobria(t)
	res, err := Apply(name, src, line)
	if err != nil {
		t.Fatalf("%q must be accepted against the shipped scene, and it was refused: %v\nConsequence: the move form is unusable from the interface.\nRemedy: fix the resolver, not the test.", line, err)
	}
	if res.Doc == nil {
		t.Fatalf("%q returned no document to draw", line)
	}
	if res.Summary == "" {
		t.Fatalf("%q produced no summary; the change-diff view has nothing to show before the patch is trusted", line)
	}
	return res.Doc
}

// TestMoveAbovePlacesTheNodeAsThePreviousSibling pins `above <id>` for move: the
// moved node lands immediately before the anchor in the anchor's children list.
func TestMoveAbovePlacesTheNodeAsThePreviousSibling(t *testing.T) {
	doc := moveOK(t, `/ui move status above chat`)
	parent, idx := locate(doc.Root, "status")
	if parent == nil {
		t.Fatal("`move status above chat` left status unaddressable.\nConsequence: the command reported success and the node vanished or did not move — the silent failure this surface exists to prevent.\nRemedy: reinsert the detached node into the anchor's children list.")
	}
	if next := parent.Children[idx+1]; next.ID != "chat" {
		t.Errorf("`move status above chat` did not place status immediately before chat.\n  node after status: id=%q type=%q\nConsequence: `above` and `below` differ only in this offset, so an off-by-one silently implements the opposite verb.\nRemedy: insert at the anchor's index, not after it.", next.ID, next.Type)
	}
}

// TestMoveBelowPlacesTheNodeAsTheNextSibling pins `below <id>`, the offset the
// `above` test would pass under if the two were swapped.
func TestMoveBelowPlacesTheNodeAsTheNextSibling(t *testing.T) {
	doc := moveOK(t, `/ui move notice below chat`)
	parent, idx := locate(doc.Root, "notice")
	if parent == nil {
		t.Fatal("`move notice below chat` left notice unaddressable.\nRemedy: reinsert the detached node into the anchor's children list.")
	}
	if prev := parent.Children[idx-1]; prev.ID != "chat" {
		t.Errorf("`move notice below chat` did not place notice immediately after chat.\n  node before notice: id=%q type=%q\nConsequence: it silently implements `above` instead.\nRemedy: insert at the anchor's index + 1.", prev.ID, prev.Type)
	}
}

// TestMoveIntoAppendsAsTheLastChild pins `into <id>`: the moved node becomes the
// last child of the named container.
func TestMoveIntoAppendsAsTheLastChild(t *testing.T) {
	doc := moveOK(t, `/ui move notice into status`)
	parent, idx := locate(doc.Root, "notice")
	if parent == nil || parent.ID != "status" {
		t.Fatalf("`move notice into status` did not make notice a child of status.\n  parent: %v\nConsequence: the node landed somewhere other than the container the user named.\nRemedy: append to the target's own children.", parent)
	}
	if idx != len(parent.Children)-1 {
		t.Errorf("`move notice into status` did not append notice last (index %d of %d).\nConsequence: `into` and `into top` differ only in this position; the wrong end silently implements the other.\nRemedy: append, do not prepend.", idx, len(parent.Children))
	}
}

// TestMoveIntoTopPrependsAsTheFirstChild pins `into <id> top`.
func TestMoveIntoTopPrependsAsTheFirstChild(t *testing.T) {
	doc := moveOK(t, `/ui move notice into status top`)
	parent, idx := locate(doc.Root, "notice")
	if parent == nil || parent.ID != "status" {
		t.Fatalf("`move notice into status top` did not make notice a child of status.\n  parent: %v", parent)
	}
	if idx != 0 {
		t.Errorf("`move notice into status top` did not prepend notice (index %d).\nConsequence: the `top` keyword was parsed but not honoured, so it silently behaves like plain `into`.\nRemedy: prepend when Where is into_top.", idx)
	}
}

// TestMoveBelowInputPlacesTheNodeAfterTheInput pins the semantic anchor for
// move: it resolves to the input node by role, without naming its id.
func TestMoveBelowInputPlacesTheNodeAfterTheInput(t *testing.T) {
	doc := moveOK(t, `/ui move status below_input`)
	parent, idx := locate(doc.Root, "status")
	if parent == nil {
		t.Fatal("`move status below_input` left status unaddressable.\nRemedy: resolve the anchor to the node whose type is input and reinsert beside it.")
	}
	if prev := parent.Children[idx-1]; prev.Type != "input" {
		t.Errorf("`move status below_input` did not place status immediately after the input node.\n  node before status: id=%q type=%q\nConsequence: the anchor addresses the wrong role, so a theme that renames its input id gets the node in the wrong place.\nRemedy: match on type==\"input\", not on an id.", prev.ID, prev.Type)
	}
}

// TestMoveAboveInputPlacesTheNodeBeforeTheInput pins the mirror anchor.
func TestMoveAboveInputPlacesTheNodeBeforeTheInput(t *testing.T) {
	doc := moveOK(t, `/ui move status above_input`)
	parent, idx := locate(doc.Root, "status")
	if parent == nil {
		t.Fatal("`move status above_input` left status unaddressable.")
	}
	if next := parent.Children[idx+1]; next.Type != "input" {
		t.Errorf("`move status above_input` did not place status immediately before the input node.\n  node after status: id=%q type=%q\nConsequence: it silently behaves like below_input.\nRemedy: insert at the input's index, not after it.", next.ID, next.Type)
	}
}

// TestMovePreservesKeysTheNodeCarries is move's half of the generic-tree
// property: the relocated node keeps every key scene.Node does not declare. If
// move rebuilt the node through the typed tree it would strip such keys off the
// one node it touches — invisible, because the scene still parses and draws.
func TestMovePreservesKeysTheNodeCarries(t *testing.T) {
	name := "custom.json"
	src := []byte(`{"root":{"type":"stack","id":"root","children":[
	  {"type":"box","id":"box","children":[]},
	  {"type":"text","id":"leaf","text":"x","x_plugin_field":"keep-me"}
	]}}`)
	res, err := Apply(name, src, `/ui move leaf into box`)
	if err != nil {
		t.Fatalf("moving a node with an undeclared key must be accepted: %v", err)
	}
	if !strings.Contains(string(res.Source), "keep-me") {
		t.Errorf("moving a node dropped a key it was not asked to touch.\n  patched source: %s\nConsequence: every field a future phase or a plugin fragment carries is erased the first time the node is moved, and the scene still parses, validates and draws — so nothing reports it.\nRemedy: keep the move on the generic map tree; scene.Node cannot round-trip a key it does not declare.", res.Source)
	}
}

// TestMoveOfAnUnknownSubjectIsRefusedWithTheIdAndTheAddress covers §2.1 on the
// subject side: the node to move does not exist.
func TestMoveOfAnUnknownSubjectIsRefusedWithTheIdAndTheAddress(t *testing.T) {
	name, src := sobria(t)
	got := addRefused(t, name, src, `/ui move nonesuch above chat`)
	mustContain(t, got, "nonesuch", "the user cannot tell a typo from an unnameable node without seeing the id they typed.")
	mustContain(t, got, "SOBRIA.json:", "a refusal without file:line sends the repair loop to guess which line is wrong.")
}

// TestMoveToAnUnknownAnchorIsRefusedWithTheIdAndTheAddress covers §2.1 on the
// anchor side: the subject exists, the where clause's anchor does not.
func TestMoveToAnUnknownAnchorIsRefusedWithTheIdAndTheAddress(t *testing.T) {
	name, src := sobria(t)
	got := addRefused(t, name, src, `/ui move status above nonesuch`)
	mustContain(t, got, "nonesuch", "the anchor of the where clause is a separate id from the subject, and a wrong one must be named.")
	mustContain(t, got, "SOBRIA.json:", "the anchor refusal is as much a tree-level refusal as the subject one, so it too must be addressed.")
}

// TestMoveIntoANonContainerIsRefusedByType covers §2.3 for move: only
// stack/row/box/overlay take children.
func TestMoveIntoANonContainerIsRefusedByType(t *testing.T) {
	name, src := sobria(t)
	// chat is a markdown node, not a container.
	got := addRefused(t, name, src, `/ui move status into chat`)
	mustContain(t, got, "markdown", "the user needs to know which node type was rejected and why.")
	mustContain(t, got, "containers", "the remedy — use a container — is the whole content of the refusal.")
}

// TestMoveCreatingACycleIsRefused is the refusal unique to move (§2.4): a node
// cannot be placed inside its own subtree. menu is an overlay whose child cmds
// is a descendant, so `move menu into cmds` would reattach menu below one of its
// own descendants.
func TestMoveCreatingACycleIsRefused(t *testing.T) {
	name, src := sobria(t)
	got := addRefused(t, name, src, `/ui move menu into cmds`)
	mustContain(t, got, "descendant", "the move would place a node inside its own subtree; the reason it is forbidden is the whole content of the refusal.")
	mustContain(t, got, "§2.4", "the refusal must cite the addressing rule it enforces so the reader can tell a bug from a rule.")
}

// TestMoveIntoItselfIsRefusedAsACycle covers the anchor==subject degenerate: a
// node is trivially inside its own subtree, so `move status into status` is the
// same refusal as any deeper cycle.
func TestMoveIntoItselfIsRefusedAsACycle(t *testing.T) {
	name, src := sobria(t)
	got := addRefused(t, name, src, `/ui move status into status`)
	mustContain(t, got, "itself", "moving a node into itself is the smallest cycle, and the message should name that directly rather than reporting a generic descendant.")
	mustContain(t, got, "§2.4", "it is the same rule as any other cycle and must cite it.")
}

// TestMoveBelowInputIsRefusedWhenTheInputIsInsideTheSubject covers the cycle on
// the semantic-anchor side: the subject contains the one input node, so moving
// the subject beside "the input" would place it beside its own descendant.
func TestMoveBelowInputIsRefusedWhenTheInputIsInsideTheSubject(t *testing.T) {
	src := []byte(`{"root":{"type":"stack","id":"root","children":[
	  {"type":"stack","id":"wrap","children":[{"type":"input","id":"in","bind":"user.input"}]}
	]}}`)
	got := addRefused(t, "crafted.json", src, `/ui move wrap below_input`)
	mustContain(t, got, "subtree", "the input the anchor resolves to lives inside the moved node, so beside-the-input is inside-itself.")
	mustContain(t, got, "§2.4", "it is the cycle rule reached through the semantic anchor and must cite it.")
}

// TestMoveANodeInASingleNodeSlotIsRefusedWithNoParentList covers the subject
// that is in no children list: a node occupying a prefix/suffix slot has no
// siblings and no parent list to leave, so it cannot be moved. It must be
// distinguished from an unknown id — the id was found. (The scene root hits the
// cycle refusal first, since every other node is inside it, so this uses a
// single-node slot to isolate the no-parent-list case.)
func TestMoveANodeInASingleNodeSlotIsRefusedWithNoParentList(t *testing.T) {
	src := []byte(`{"root":{"type":"stack","id":"root","children":[
	  {"type":"marquee","id":"m","prefix":{"type":"text","id":"pfx","text":"x"}},
	  {"type":"box","id":"box","children":[]}
	]}}`)
	got := addRefused(t, "crafted.json", src, `/ui move pfx into box`)
	mustContain(t, got, "children list", "the subject was found, so 'unknown id' would be a wrong diagnosis; the truth is it has no parent list to leave.")
}

// TestMoveOfAnAmbiguousSubjectIsRefused covers §3 on the subject side: two nodes
// with one id make "move that node" name no single node.
func TestMoveOfAnAmbiguousSubjectIsRefused(t *testing.T) {
	src := []byte(`{"root":{"type":"stack","id":"root","children":[
	  {"type":"text","id":"dup","text":"x"},
	  {"type":"text","id":"dup","text":"y"},
	  {"type":"box","id":"box","children":[]}
	]}}`)
	got := addRefused(t, "crafted.json", src, `/ui move dup into box`)
	mustContain(t, got, `"dup"`, "the user must be told which id is ambiguous.")
	mustContain(t, got, "unique", "the reason — ids are addresses — is why the move cannot resolve.")
}

// TestMoveParseRefusals sweeps the grammar refusals in one place: each names the
// full shape it wanted, because a command surface that rejects an input without
// showing the form costs the user a turn spent guessing.
func TestMoveParseRefusals(t *testing.T) {
	name, src := sobria(t)
	cases := []struct {
		line string
		want string
		why  string
	}{
		{`/ui move`, "/ui move <id> <where>", "a move with no id has nothing to relocate and must show the shape it wanted."},
		{`/ui move status`, "where clause", "a move with no where has nowhere to go and must say the forms that exist."},
		{`/ui move status sideways`, "unknown where", "an unknown where must list the legal forms rather than silently doing nothing."},
		{`/ui move status above`, "needs a node id", "above/below/into need an anchor id; the missing one must be named."},
		{`/ui move status above chat extra`, "trailing input", "move takes only <id> <where>; a trailing token is not a fragment and must not be silently dropped."},
	}
	for _, tc := range cases {
		got := addRefused(t, name, src, tc.line)
		mustContain(t, got, tc.want, tc.why)
	}
}
