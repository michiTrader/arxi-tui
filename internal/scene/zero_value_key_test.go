package scene

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// The audit for what a node reports about a key the author wrote *empty*.
//
// # The defect this was written from
//
// Every fixture in remedy_is_effective_test.go sets its key to a non-zero
// value, because a fixture that did not would have been caught by
// assertFixtureSetsTheKey as a harness bug. That is the right rule for that
// audit and it is exactly why that audit — 380 lines asking whether the
// printed remedy refuses anything, for every key in the vocabulary — could
// not see this:
//
//	{"root":{"type":"text","text":"x","on_press":""}}   -> validated clean
//	{"root":{"type":"text","text":"x","scroll":null}}   -> refused
//
// Both keys are in unrenderedFields. Both were written by the author. The
// only difference is that `Scroll` is a json.RawMessage, which keeps the four
// bytes `null`, while `OnPress` is a string whose zero value omitempty drops
// on the way back out — and `declaredUnrenderedFields` reconstructs its answer
// by re-serialising. So the node could not report a key it really had, and the
// field was silently dropped by the engine: the exact failure this package
// refuses everywhere else, on a key that was already listed as refused.
//
// The shape is worth naming beyond this instance. The previous turn collapsed
// two hand-written lists into one derivation and recorded that the available
// bug class shrank from "the two lists disagree" to "the one list is wrong".
// This is an instance of the second, and of a kind no widening or narrowing of
// the derivation reaches: the list was right about every field it named, and
// the *question it answers* was the wrong question. A derivation over values
// answers "which fields does this node hold a value for". The validator needs
// "which keys did the author write". Those coincide for every non-zero value,
// which is every fixture anyone writes by hand.
//
// # Why this is not covered by the fixture audit gaining an empty variant
//
// It nearly is, and that was the first fix attempted. But the fixture audit
// asserts each probe parses to a node with the field *set* — an empty-valued
// fixture fails that assertion by construction, and loosening it would delete
// the protection against a misspelled key looking like a defect in the code.
// The two questions need two audits.
func TestAKeyWrittenEmptyIsStillAKeyTheAuthorWrote(t *testing.T) {
	// One case per unrendered key, written with its type's zero value. These
	// are derived from the map rather than listed, so an entry added to
	// unrenderedFields tomorrow fails here until it has a case: a key with no
	// case is how this hole stayed open in the first place.
	empties := map[string]string{
		"on_press":     `""`,
		"scroll":       `null`,
		"row_template": `null`,
	}

	for key := range unrenderedFields {
		zero, ok := empties[key]
		if !ok {
			t.Errorf("unrenderedFields declares %q and this audit has no zero-value spelling for it.\n"+
				"consequence: the key could be written empty and dropped in silence, which is the\n"+
				"defect this file exists for, and the audit would report success having asked nothing.\n"+
				"remedy: add %q to the empties map above with the json spelling of its zero value.", key, key)
			continue
		}

		t.Run(key, func(t *testing.T) {
			body := fmt.Sprintf(`{"root":{"type":"text","text":"x",%q:%s}}`, key, zero)

			doc, err := ParseDocument([]byte(body))
			if err != nil {
				t.Fatalf("premise broken: the probe for %q must parse; got %v\n"+
					"remedy: fix the zero-value spelling in the empties map — a probe that does\n"+
					"not parse measures the harness.\ndocument: %s", key, err, body)
			}

			verr := doc.Validate()
			if verr == nil {
				t.Errorf("%q was written by the author with an empty value and the document validates clean.\n\n"+
					"document: %s\n\n"+
					"consequence: %q is in unrenderedFields, so the engine does not draw it. Written\n"+
					"with any other value it is refused with an address; written empty it is accepted\n"+
					"and then silently dropped — the failure this package refuses everywhere else,\n"+
					"on a key already listed as refused. An author clearing the property mid-edit\n"+
					"gets a clean validate and no drawing, with nothing naming the field.\n"+
					"remedy: refuseUnrendered must union declaredUnrenderedFields (which answers\n"+
					"\"what does this node hold a value for\", and loses zero values to omitempty)\n"+
					"with the parser's declaredKeys (which records what the source actually wrote).\n"+
					"Do not fix this inside declaredUnrenderedFields: a hand-built Document has no\n"+
					"declaredKeys, and that path must keep answering from values.", key, body, key)
				return
			}
			if !strings.Contains(verr.Error(), key) {
				t.Errorf("the refusal for an empty %q names something else, so a reader cannot tell\n"+
					"which field was rejected: %q", key, verr.Error())
			}
		})
	}
}

// The union must not turn the guard off for documents with no source text.
//
// This is the counterfactual that decided the fix's shape, kept because it is
// the one a later reader is most likely to undo. Reading only declaredKeys is
// simpler, strictly more accurate for parsed documents, and refuses *nothing*
// for a Document built in memory — a test fixture today, and the output of the
// patch path Phase 2 designs. A guard that silently stops applying to the
// callers a whole phase is about to add is worse than the hole it closed.
func TestAHandBuiltDocumentWithNoSourceStillRefuses(t *testing.T) {
	doc := &Document{Root: &Node{Type: "text", Text: "x", OnPress: "cmd:/help"}}

	if len(doc.declaredKeys) != 0 {
		t.Fatalf("premise broken: a hand-built Document is supposed to have no declaredKeys; got %v\n"+
			"consequence: this test would pass through the parser's record and prove nothing\n"+
			"about the value-derived half of the union.", doc.declaredKeys)
	}

	verr := doc.Validate()
	if verr == nil {
		t.Fatalf("a Document built in memory declaring on_press validates clean.\n\n" +
			"consequence: refuseUnrendered is reading only the parser's declaredKeys, so every\n" +
			"caller that does not come from a file — tests, and the Phase 2 patch path — has no\n" +
			"unrendered-field refusal at all. The guard was not loosened, it was switched off\n" +
			"for a whole class of caller, silently.\n" +
			"remedy: keep the union. declaredUnrenderedFields answers from the node's values and\n" +
			"is the only half that works without source text.")
	}
	if !strings.Contains(verr.Error(), "on_press") {
		t.Errorf("the refusal does not name on_press: %q", verr.Error())
	}
}

// A key the author did not write must stay unrefused.
//
// The false-alarm direction, and the cheap one to get wrong: a union of two
// sources is one bad key away from refusing a field nobody mentioned, and the
// cost asymmetry recorded in LESSONS.md runs against us here — a missed defect
// weakens this audit, a false alarm gets the whole refusal deleted as noise.
func TestAKeyNobodyWroteIsNotRefused(t *testing.T) {
	for _, body := range []string{
		`{"root":{"type":"text","text":"x"}}`,
		`{"root":{"type":"box","border":"single","children":[{"type":"text","text":"x"}]}}`,
		`{"root":{"type":"list","bind":"slash.matches","count":true}}`,
	} {
		doc, err := ParseDocument([]byte(body))
		if err != nil {
			t.Fatalf("premise broken: %q must parse; got %v", body, err)
		}
		if verr := doc.Validate(); verr != nil {
			t.Errorf("a document mentioning no unrendered key was refused: %v\n\n"+
				"document: %s\n\n"+
				"consequence: refuseUnrendered now reads the parser's key record as well as the\n"+
				"node's values. If that record is consulted at the wrong path, a node is refused\n"+
				"for a key written on a different node — an accusation the author cannot act on,\n"+
				"and the direction that gets a guard deleted rather than merely doubted.\n"+
				"remedy: the declaredKeys lookup must use the same path refuseUnrendered was\n"+
				"called with, not the root's.", verr, body)
		}
	}
}

// The two derivations of "does this type carry a Node" must agree.
//
// # Why this exists, measured rather than assumed
//
// `typeBuildsOnNode` in node.go decides which fields the shallow clone clears
// and restores. `typeContainsNode` in nested_branch_audit_test.go decides
// which fields the nested-branch audit walks. They are the same question,
// written twice, and node.go's comment already says they "only agree
// permanently when they ask the same question of the same type" — but nothing
// held them to it.
//
// The previous turn collapsed the clear list and the restore list into one
// derivation and measured *widening* it (accepting strings and bools), which
// changed nothing. The opposite direction was not run. It was, here:
//
//	case reflect.Pointer, reflect.Slice, reflect.Array:  ->  case reflect.Pointer:
//	  subtreeBranches drops to 5 entries; `children` is never cleared
//	  a node with a cyclic child makes json.Marshal fail, so
//	    declaredUnrenderedFields returns nil — fail-open, every key unrefused
//	  the whole suite, including the 380-line remedy audit, stays green
//
// So the narrowing that reintroduces exactly the `children` drift the last
// turn fixed was invisible. Widening is caught because clear and restore read
// one list; narrowing is not caught by anything, because a field never cleared
// is never missing. This pins the two walks to each other instead: whichever
// one is edited, the other disagrees and says so.
func TestTheTwoNodeCarryingDerivationsAgree(t *testing.T) {
	rt := reflect.TypeOf(Node{})
	raw := reflect.TypeOf(json.RawMessage(nil))

	cleared := make(map[string]bool)
	for _, b := range subtreeBranches() {
		cleared[b.jsonName] = true
	}

	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}

		// node.go clears a field if it carries a Node *or* is raw; the audit's
		// walk answers only the Node half, so raw fields are compared against
		// that half alone.
		byAudit := typeContainsNode(f.Type, make(map[reflect.Type]bool)) || f.Type == raw
		if byAudit != cleared[name] {
			t.Errorf("the two derivations of \"can this type hold a Node\" disagree about %q (%s).\n"+
				"  typeBuildsOnNode (node.go, clears and restores it): %v\n"+
				"  typeContainsNode (nested_branch_audit_test.go):     %v\n\n"+
				"consequence: these decide different things off the same question. If node.go's\n"+
				"walk is the narrower one, the field is never cleared by declaredUnrenderedFields\n"+
				"and never restored either — so nothing looks missing and the suite stays green,\n"+
				"which is how `children` drifted before. Narrowing is invisible to every other\n"+
				"guard here: widening is caught because clear and restore read one list, but a\n"+
				"field that is never cleared cannot be reported missing.\n"+
				"remedy: make the two walks ask the same question of the same type. They are\n"+
				"structurally identical today — pointer, slice, array, map (keys included),\n"+
				"recursing with a seen set — and any edit to one belongs in the other.", name, f.Type, cleared[name], byAudit)
		}
	}
}

// A branch that is cleared must be restorable, or the clone fails open.
//
// The second half of the same finding. declaredUnrenderedFields marshals the
// cleared clone and returns nil on error — and that error is reachable: a node
// whose subtree contains a cycle makes encoding/json fail outright. Today the
// clearing prevents it, so `return nil` is dead code that is only dead because
// of the derivation above. Narrow that derivation and the fail-open path comes
// alive, refusing nothing at all rather than refusing less.
//
// This test pins the property that keeps it dead, rather than the dead line:
// every field that can carry a Node is cleared before the marshal.
func TestEveryNodeCarryingFieldIsClearedBeforeMarshalling(t *testing.T) {
	cleared := make(map[string]bool)
	for _, b := range subtreeBranches() {
		cleared[b.jsonName] = true
	}

	rt := reflect.TypeOf(Node{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		if !typeContainsNode(f.Type, make(map[reflect.Type]bool)) {
			continue
		}
		if !cleared[name] {
			t.Errorf("%q (%s) can hold a Node and is not cleared before the clone is marshalled.\n\n"+
				"consequence: declaredUnrenderedFields serialises the clone and returns nil if that\n"+
				"fails. A document whose tree contains a cycle through this field makes\n"+
				"json.Marshal fail — measured: \"json: unsupported value: encountered a cycle via\n"+
				"*scene.Node\" — and nil means *no field is refused at all* on that node. The guard\n"+
				"does not weaken, it switches off, and the caller cannot tell.\n"+
				"remedy: subtreeBranches must include every Node-bearing field. If this field is\n"+
				"genuinely not a subtree, the type is lying about what it can hold.", name, f.Type)
		}
	}
}

// The refusal must name the node that carries the key, not an ancestor.
//
// declaredKeys is indexed by access path, so a union that looks up the wrong
// path can still refuse the right key from the wrong place. An address that
// points at the parent sends the reader to a node whose source line does not
// contain the field — PLAN.md invariant 4 is that a refusal names a position,
// and a position that is wrong is worse than the file-only fallback.
func TestTheRefusalAddressesTheNodeThatCarriesTheKey(t *testing.T) {
	body := `{"root":{"type":"box","children":[{"type":"text","text":"x","on_press":""}]}}`

	doc, err := ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	verr := doc.Validate()
	if verr == nil {
		t.Fatalf("the nested empty on_press was not refused at all: %s", body)
	}

	serr, ok := verr.(*Error)
	if !ok {
		t.Fatalf("the refusal is not a *scene.Error, so it carries no address: %v", verr)
	}
	// The child begins at the offset of its own `{`, well past the root's.
	if serr.Loc.Col <= 10 {
		t.Errorf("the refusal for a key on the child node is addressed at column %d, which is the\n"+
			"root object, not the node that wrote it.\n\n"+
			"document: %s\nrefusal:  %v\n\n"+
			"consequence: the reader is sent to a line that does not contain on_press. A wrong\n"+
			"address is worse than no address: the file-only fallback makes the reader search,\n"+
			"while a confident wrong column makes them conclude the validator is broken.\n"+
			"remedy: refuseUnrendered receives the node's path; the declaredKeys lookup must\n"+
			"use it rather than nodePathRoot.", serr.Loc.Col, body, verr)
	}
}
