package patch

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// locate returns the parent of the first node carrying id and its index in that
// parent's Children, or (nil, -1). The add tests assert on *position*, which is
// the whole content of the where vocabulary, so they need to read where a node
// landed rather than merely that it exists. Children-only is deliberate: every
// where form these tests exercise inserts into a children list, and searching
// the single-node slots too would only hide a bug that put a node somewhere it
// should not be.
func locate(n *scene.Node, id string) (*scene.Node, int) {
	for i, c := range n.Children {
		if c.ID == id {
			return n, i
		}
		if p, j := locate(c, id); p != nil {
			return p, j
		}
	}
	return nil, -1
}

// probe is the fragment the tests insert. It carries an id so locate can find
// it and a text so a mis-parse is visible, and nothing the validator refuses.
const probe = `{"type":"text","id":"probe","text":"inserted"}`

// applyAdd runs one add command against the shipped scene and fails loudly if
// it is refused, so a happy-path test reads as one assertion rather than three
// lines of error handling repeated six times.
func addOK(t *testing.T, line string) *scene.Document {
	t.Helper()
	name, src := sobria(t)
	res, err := Apply(name, src, line)
	if err != nil {
		t.Fatalf("%q must be accepted against the shipped scene, and it was refused: %v\nConsequence: the where form is unusable from the interface.\nRemedy: fix the resolver, not the test.", line, err)
	}
	if res.Doc == nil {
		t.Fatalf("%q returned no document to draw", line)
	}
	if res.Summary == "" {
		t.Fatalf("%q produced no summary; the change-diff view has nothing to show before the patch is trusted", line)
	}
	return res.Doc
}

// TestAddAboveInsertsAsThePreviousSibling pins the `above <id>` form: the
// fragment lands immediately before the anchor in the same children list.
func TestAddAboveInsertsAsThePreviousSibling(t *testing.T) {
	doc := addOK(t, `/ui add node above chat `+probe)
	parent, idx := locate(doc.Root, "probe")
	if parent == nil {
		t.Fatal("`above chat` inserted no addressable node.\nConsequence: the command reported success and changed nothing — the silent no-op this surface exists to prevent.\nRemedy: insert the fragment into the anchor's children list.")
	}
	if next := parent.Children[idx+1]; next.ID != "chat" {
		t.Errorf("`above chat` did not place the fragment immediately before chat.\n  node after the fragment: id=%q type=%q\nConsequence: `above` and `below` differ only in this offset, so an off-by-one here silently implements the opposite verb.\nRemedy: insert at the anchor's index, not after it.", next.ID, next.Type)
	}
}

// TestAddBelowInsertsAsTheNextSibling pins `below <id>`, the offset `above`'s
// test would pass under if the two were swapped.
func TestAddBelowInsertsAsTheNextSibling(t *testing.T) {
	doc := addOK(t, `/ui add node below chat `+probe)
	parent, idx := locate(doc.Root, "probe")
	if parent == nil {
		t.Fatal("`below chat` inserted no addressable node.\nRemedy: insert the fragment into the anchor's children list.")
	}
	if prev := parent.Children[idx-1]; prev.ID != "chat" {
		t.Errorf("`below chat` did not place the fragment immediately after chat.\n  node before the fragment: id=%q type=%q\nConsequence: it silently implements `above` instead.\nRemedy: insert at the anchor's index + 1.", prev.ID, prev.Type)
	}
}

// TestAddIntoAppendsAsTheLastChild pins `into <id>`: the fragment becomes the
// last child of the named container.
func TestAddIntoAppendsAsTheLastChild(t *testing.T) {
	doc := addOK(t, `/ui add node into menu `+probe)
	parent, idx := locate(doc.Root, "probe")
	if parent == nil || parent.ID != "menu" {
		t.Fatalf("`into menu` did not make the fragment a child of menu.\n  parent: %v\nConsequence: the node landed somewhere other than the container the user named.\nRemedy: append to the target's own children.", parent)
	}
	if idx != len(parent.Children)-1 {
		t.Errorf("`into menu` did not append the fragment last (index %d of %d).\nConsequence: `into` and `into top` differ only in this position; the wrong end silently implements the other.\nRemedy: append, do not prepend.", idx, len(parent.Children))
	}
}

// TestAddIntoTopPrependsAsTheFirstChild pins `into <id> top`.
func TestAddIntoTopPrependsAsTheFirstChild(t *testing.T) {
	doc := addOK(t, `/ui add node into menu top `+probe)
	parent, idx := locate(doc.Root, "probe")
	if parent == nil || parent.ID != "menu" {
		t.Fatalf("`into menu top` did not make the fragment a child of menu.\n  parent: %v", parent)
	}
	if idx != 0 {
		t.Errorf("`into menu top` did not prepend the fragment (index %d).\nConsequence: the `top` keyword was parsed but not honoured, so it silently behaves like plain `into`.\nRemedy: prepend when Where is into_top.", idx)
	}
}

// TestAddBelowInputInsertsAfterTheInputByRole pins the semantic anchor: it
// resolves to the input node without naming its id, which is the property that
// lets a downloaded theme accept the command without the user reading it first.
func TestAddBelowInputInsertsAfterTheInputByRole(t *testing.T) {
	doc := addOK(t, `/ui add node below_input `+probe)
	parent, idx := locate(doc.Root, "probe")
	if parent == nil {
		t.Fatal("`below_input` inserted no addressable node.\nRemedy: resolve the anchor to the node whose type is input and insert beside it.")
	}
	if prev := parent.Children[idx-1]; prev.Type != "input" {
		t.Errorf("`below_input` did not place the fragment immediately after the input node.\n  node before the fragment: id=%q type=%q\nConsequence: the anchor addresses the wrong role, so a theme that renames its input id gets the fragment in the wrong place.\nRemedy: match on type==\"input\", not on an id.", prev.ID, prev.Type)
	}
}

// TestAddAboveInputInsertsBeforeTheInput pins the mirror anchor.
func TestAddAboveInputInsertsBeforeTheInput(t *testing.T) {
	doc := addOK(t, `/ui add node above_input `+probe)
	parent, idx := locate(doc.Root, "probe")
	if parent == nil {
		t.Fatal("`above_input` inserted no addressable node.")
	}
	if next := parent.Children[idx+1]; next.Type != "input" {
		t.Errorf("`above_input` did not place the fragment immediately before the input node.\n  node after the fragment: id=%q type=%q\nConsequence: it silently behaves like below_input.\nRemedy: insert at the input's index, not after it.", next.ID, next.Type)
	}
}

// TestAddPreservesKeysTheFragmentCarries is add's half of the property the
// set/style sweep pins: the edit rides the generic map tree, so a key the
// scene.Node struct does not declare survives the round-trip. If add ever built
// the fragment through scene.Node it would silently drop such keys — the worst
// bug in a surface whose promise is that the user's document is theirs.
func TestAddPreservesKeysTheFragmentCarries(t *testing.T) {
	name, src := sobria(t)
	// "data-plugin" is declared by no scene.Node field; it stands in for a
	// future field or a third-party plugin fragment's own property.
	res, err := Apply(name, src, `/ui add node below status {"type":"text","id":"probe","text":"x","data-plugin":"keep-me"}`)
	if err != nil {
		t.Fatalf("adding a node with an undeclared key must be accepted: %v", err)
	}
	if !strings.Contains(string(res.Source), "keep-me") {
		t.Errorf("the fragment's undeclared key was dropped from the patched source.\n  source: %s\nConsequence: every unknown property a plugin fragment or a future phase carries is erased the moment it is added, and the scene still parses and draws — so nothing reports it.\nRemedy: keep the insertion on the generic map tree; scene.Node cannot round-trip a key it does not declare.", res.Source)
	}
}

// addRefused runs an add command expected to fail and returns the message, or
// fails the test if the command was accepted. Every add refusal must reach the
// user, because an unclaimed control line falls through to the model as chat —
// so "accepted when it should be refused" is the failure that turns a typo into
// a conversation turn.
func addRefused(t *testing.T, name string, src []byte, line string) string {
	t.Helper()
	res, err := Apply(name, src, line)
	if err == nil {
		t.Fatalf("%q must be refused, and it was accepted.\n  produced: %s\nConsequence: an insertion the addressing rules forbid reached the drawn scene.\nRemedy: refuse it in the resolver or the parser.", line, res.Source)
	}
	return err.Error()
}

// mustContain checks a refusal names both the reason and, for the tree-level
// refusals, its address. A refusal without a location is a bug in this project
// (AGENTS.md), and the repair loop reads file:line to know which line to edit.
func mustContain(t *testing.T, got, want, why string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("the refusal does not mention %q.\n  got: %s\nConsequence: %s", want, got, why)
	}
}

// TestAddToAnUnknownIdIsRefusedWithTheIdAndTheAddress covers ADDRESSING §2.1.
func TestAddToAnUnknownIdIsRefusedWithTheIdAndTheAddress(t *testing.T) {
	name, src := sobria(t)
	got := addRefused(t, name, src, `/ui add node below nonesuch `+probe)
	mustContain(t, got, "nonesuch", "the user cannot tell a typo from an unnameable node without seeing the id they typed.")
	mustContain(t, got, "SOBRIA.json:", "a refusal without file:line sends the repair loop to guess which line is wrong.")
}

// TestAddIntoANonContainerIsRefusedByType covers ADDRESSING §2.3: only
// stack/row/box/overlay take children.
func TestAddIntoANonContainerIsRefusedByType(t *testing.T) {
	name, src := sobria(t)
	// chat is a markdown node, not a container.
	got := addRefused(t, name, src, `/ui add node into chat `+probe)
	mustContain(t, got, "markdown", "the user needs to know which node type was rejected and why.")
	mustContain(t, got, "containers", "the remedy — use a container — is the whole content of the refusal.")
}

// TestAddOfAFragmentReusingAnIdIsRefused covers ADDRESSING §3 at the add
// boundary: an id is an address, so a fragment cannot introduce a second node
// carrying one that already exists.
func TestAddOfAFragmentReusingAnIdIsRefused(t *testing.T) {
	name, src := sobria(t)
	got := addRefused(t, name, src, `/ui add node below status {"type":"text","id":"chat","text":"x"}`)
	mustContain(t, got, `"chat"`, "the user must be told which id clashed.")
	mustContain(t, got, "unique", "the reason — ids are addresses — is why every above/below/into would otherwise become ambiguous.")
}

// TestBelowInputIsRefusedWhenThereIsNoInput covers ADDRESSING §2.5, zero side.
// The refusal fires on the raw source before re-validation, so the crafted
// scene need not be otherwise valid — the anchor simply has no role to bind to.
func TestBelowInputIsRefusedWhenThereIsNoInput(t *testing.T) {
	src := []byte(`{"type":"stack","children":[{"type":"text","id":"a","text":"x"}]}`)
	got := addRefused(t, "crafted.json", src, `/ui add node below_input `+probe)
	mustContain(t, got, "no input node", "the anchor names the node the user types into; if there is none, the command has no meaning and must say so.")
}

// TestBelowInputIsRefusedWhenThereAreTwoInputs covers ADDRESSING §2.5, the
// ambiguity side: two inputs make "the input" name no single position.
func TestBelowInputIsRefusedWhenThereAreTwoInputs(t *testing.T) {
	src := []byte(`{"type":"stack","children":[{"type":"input","id":"a"},{"type":"input","id":"b"}]}`)
	got := addRefused(t, "crafted.json", src, `/ui add node below_input `+probe)
	mustContain(t, got, "ambiguous", "guessing which input the user meant is the scene-defect-hiding behaviour ADDRESSING §2 forbids.")
}

// TestAddAboveTheRootIsRefusedWithNoSiblingSlot covers the resolved-but-
// unplaceable case: the anchor exists but is in no children list, so it has no
// siblings. It must be distinguished from an unknown id — the id was found.
func TestAddAboveTheRootIsRefusedWithNoSiblingSlot(t *testing.T) {
	src := []byte(`{"type":"stack","id":"root","children":[{"type":"text","id":"a","text":"x"}]}`)
	got := addRefused(t, "crafted.json", src, `/ui add node above root `+probe)
	mustContain(t, got, "no siblings", "the anchor was found, so 'unknown id' would be a wrong diagnosis; the truth is it has no sibling list.")
	mustContain(t, got, "into", "the remedy — add a child with `into` instead — is the actionable half of the message.")
}

// TestAddParseRefusals sweeps the grammar refusals in one place: each names the
// full shape it wanted, because a command surface that rejects an input without
// showing the form costs the user a turn spent guessing.
func TestAddParseRefusals(t *testing.T) {
	name, src := sobria(t)
	cases := []struct {
		line string
		want string
		why  string
	}{
		{`/ui add sideways`, "add node", "the missing `node` keyword must be shown, not just rejected."},
		{`/ui add node`, "where clause", "an add with no where has nowhere to go and must say the forms that exist."},
		{`/ui add node diagonally ` + probe, "unknown where", "an unknown where must list the legal forms rather than silently doing nothing."},
		{`/ui add node below`, "needs a node id", "above/below/into need an anchor id; the missing one must be named."},
		{`/ui add node below status`, "needs a JSON fragment", "a where with no fragment must ask for the node to insert."},
		{`/ui add node below status notjson`, "not a JSON object", "a fragment that is not JSON must be refused as such, not parsed as chat."},
		{`/ui add node below status {"text":"x"}`, `no "type"`, "a node with no type is not a node; the fragment must be refused before insertion."},
	}
	for _, tc := range cases {
		got := addRefused(t, name, src, tc.line)
		mustContain(t, got, tc.want, tc.why)
	}
}
