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

			assertSpellingIsActuallyZero(t, key, zero, doc.Root)

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

// assertSpellingIsActuallyZero holds each entry of the `empties` map to the
// property its name claims: that the spelling is one omitempty drops, so the
// probe built from it really does exercise the union's declaredKeys half.
//
// # Why the map cannot be trusted to be what it is called
//
// `empties` is hand-written, and it is the last hand-written list in this
// chain. Every other inventory here was collapsed into a derivation over the
// struct — subtreeBranches, the vocabulary, the branch audit — for the reason
// this package has now recorded seven times. This one survived because it
// holds *json spellings*, which no derivation over Go types produces directly.
//
// But an untrusted spelling makes the audit vacuous in the silent direction.
// Measured, each line from a run, on `empties["on_press"]` changed from `""`
// to `"cmd:/help"` — one character class of edit, the kind a contributor makes
// while copying a fixture from the sibling audit next door:
//
//	the probe parses, the field is set, the validator refuses it
//	the subtest passes — via declaredUnrenderedFields, the value half
//	the union's declaredKeys half is never consulted for that key
//	whole suite green, including this file
//
// The audit still reports "on_press is refused when written empty". It is
// measuring the non-empty case under an empty case's name, which is worse
// than not measuring it: the passing subtest is what tells a reader the
// zero-value direction is covered.
//
// This is not hypothetical for `scroll`. Its spelling is `null`, and for a
// json.RawMessage that is four retained bytes, not a zero value — the entry
// passes through the value half today, exactly as the drifted `on_press`
// would. That is correct and must stay: `null` *is* how an author writes an
// empty scroll. So the check cannot demand every entry take the union path;
// it asserts what is actually true of each — whether the spelling survives
// re-serialisation — and fails only when a spelling that is supposed to
// vanish does not.
//
// # Why re-serialisation rather than reflect.Value.IsZero
//
// Because omitempty is what loses the key, and omitempty is a json rule, not
// a Go one. The two disagree on exactly the cases that matter: a RawMessage
// holding `null` is non-empty to json and would be a non-nil slice to
// reflect, while a `*int` pointing at 0 is non-empty to json and zero to
// nobody. Asking the same marshaller declaredUnrenderedFields asks is what
// keeps this check true when either rule changes.
func assertSpellingIsActuallyZero(t *testing.T, key, zero string, n *Node) {
	t.Helper()

	ok, err := spellingIsAcceptableZero(key, n)
	if err != nil {
		t.Fatalf("premise broken: the probe node for %q must round-trip to an object; got %v", key, err)
	}
	if ok {
		return
	}

	t.Errorf("the `empties` entry for %q is %s, which survives re-serialisation, so it is not a\n"+
		"zero value and this subtest is not measuring what its name says.\n\n"+
		"consequence: the probe is refused through declaredUnrenderedFields — the *value* half\n"+
		"of the union — so the declaredKeys half this file exists to pin is never consulted for\n"+
		"%q. Delete the union tomorrow and this subtest still passes. A green zero-value audit\n"+
		"measuring the non-zero case is worse than no audit: it is what tells the next reader\n"+
		"the direction is covered.\n"+
		"remedy: set empties[%q] to the json spelling omitempty drops for that field's type —\n"+
		"`\"\"` for a string, `0` for a number, `false` for a bool, `[]`/`{}` for a slice or map.\n"+
		"If the field is a json.RawMessage, it has no empty form and belongs in the raw list\n"+
		"rawBranchAccessors records, not here.", key, zero, key, key)
}

// spellingIsAcceptableZero is the decision assertSpellingIsActuallyZero
// reports on, split out so it can be called with no *testing.T.
//
// The split is the point, not tidiness. While it was inline, the guard's own
// counterfactual could not be run: replacing the raw exemption with `true`
// and drifting a spelling in the same edit left the whole suite green,
// because the only test that could have objected was the one being mutated.
// A decision reachable solely through a t.Errorf is a decision nothing can
// measure — the same reason this package derives its lists instead of
// writing them twice, one level up.
//
// It asks the marshaller, not reflect: omitempty is a json rule. A
// json.RawMessage holding `null` is non-empty to json and a non-nil slice to
// reflect; a `*int` pointing at 0 is non-empty to json and zero to nobody.
// Asking the same encoder declaredUnrenderedFields asks keeps this true when
// either rule moves.
func spellingIsAcceptableZero(key string, n *Node) (bool, error) {
	round, err := json.Marshal(n)
	if err != nil {
		return false, err
	}
	var back map[string]json.RawMessage
	if err := json.Unmarshal(round, &back); err != nil {
		return false, err
	}

	if _, survives := back[key]; !survives {
		// The spelling vanished: this entry exercises the union, which is
		// what the file is about.
		return true, nil
	}
	// It survived. Acceptable only for a field whose json encoding has no
	// empty form — the raw branches, where `null` is four real bytes.
	return isRawJSONField(key), nil
}

// The raw exemption must stay an exemption, not an escape hatch.
//
// assertSpellingIsActuallyZero lets a surviving spelling pass when the field
// is a json.RawMessage, because `null` really is how an author empties one.
// That branch is the only way to pass the check while holding a non-zero
// spelling, which makes it the place a later edit turns the whole audit off —
// and it would do so in silence, because widening an exemption never fails a
// test that is already green.
//
// Measured, from a run with the guard's `isRawJSONField(key)` replaced by
// `true` and `empties["on_press"]` drifted to `"cmd:/help"` in the same pass:
//
//	assertSpellingIsActuallyZero exempts every key
//	the drifted non-zero spelling sails through
//	whole suite green, this file included
//
// So the exemption is pinned from the other side: it must say no to the
// ordinary fields. This asks about `on_press` — a string, the field the
// original defect was found on — rather than iterating, because a loop here
// would re-derive the same classification the function under test makes and
// agree with it by construction, which is the shape this package has paid for
// seven times.
func TestTheRawExemptionDoesNotCoverOrdinaryFields(t *testing.T) {
	if !isRawJSONField("scroll") {
		t.Errorf("isRawJSONField says %q is not raw, but Node declares it json.RawMessage.\n"+
			"consequence: the zero-value audit will demand a spelling omitempty drops for a field\n"+
			"that has none, and `null` — the way an author actually empties it — starts failing.\n"+
			"remedy: the lookup must match on the json tag name, which is what the empties map is\n"+
			"keyed by.", "scroll")
	}
	if isRawJSONField("on_press") {
		t.Errorf("isRawJSONField says %q is raw, but Node declares it a string.\n\n"+
			"consequence: this is the exemption swallowing an ordinary field, and it is the one\n"+
			"failure mode of the zero-value audit that cannot announce itself. A non-zero spelling\n"+
			"in the empties map would then pass, the subtest would go on reporting that the empty\n"+
			"case is covered, and the union's declaredKeys half — the entire subject of this file —\n"+
			"would be pinned by nothing.\n"+
			"remedy: exempt a key only when Node's field for it is literally json.RawMessage.", "on_press")
	}
	if isRawJSONField("nonexistent_key") {
		t.Errorf("isRawJSONField says an unknown key is raw. A key Node does not declare cannot be\n" +
			"exempt from anything; treating it as raw means a typo in the empties map silently\n" +
			"skips its own check.")
	}

	// The decision itself, not just its input. This is the assertion the
	// inline version could not carry: a non-zero string spelling must be
	// rejected, and it must be rejected by the same function the audit calls,
	// so widening the exemption fails here rather than passing everywhere.
	nonZero, err := ParseDocument([]byte(`{"root":{"type":"text","text":"x","on_press":"cmd:/help"}}`))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	if ok, err := spellingIsAcceptableZero("on_press", nonZero.Root); err != nil {
		t.Fatalf("premise broken: %v", err)
	} else if ok {
		t.Errorf("spellingIsAcceptableZero accepts a non-zero string spelling for %q.\n\n"+
			"consequence: the zero-value audit stops checking that its probes are zero-valued, so\n"+
			"an entry drifted to a non-zero spelling passes through declaredUnrenderedFields and\n"+
			"reports the empty case as covered while never exercising it. This is the failure that\n"+
			"cannot announce itself: it makes a test greener, not redder.\n"+
			"remedy: a surviving spelling is acceptable only when Node's field is json.RawMessage.", "on_press")
	}

	// And the direction that must keep working: `null` on a raw field.
	rawNull, err := ParseDocument([]byte(`{"root":{"type":"text","text":"x","scroll":null}}`))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	if ok, err := spellingIsAcceptableZero("scroll", rawNull.Root); err != nil {
		t.Fatalf("premise broken: %v", err)
	} else if !ok {
		t.Errorf("spellingIsAcceptableZero rejects `null` for %q, which is a json.RawMessage.\n"+
			"consequence: the audit would demand a spelling that does not exist for the field, and\n"+
			"the honest entry has nowhere to go.\n"+
			"remedy: keep the raw exemption — for a RawMessage, `null` is how an author empties it.", "scroll")
	}
}

// isRawJSONField reports whether Node's field for this json key is a
// json.RawMessage, derived from the struct rather than listed. A raw field
// keeps whatever bytes the author wrote — `null` included — so it has no
// spelling omitempty drops, and the check above must not demand one.
func isRawJSONField(jsonKey string) bool {
	t := reflect.TypeOf(Node{})
	raw := reflect.TypeOf(json.RawMessage(nil))
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == jsonKey {
			return f.Type == raw
		}
	}
	return false
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
