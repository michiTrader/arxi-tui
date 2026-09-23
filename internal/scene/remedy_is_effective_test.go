package scene

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The audit for the axis every guard in this package holds fixed: the remedy
// it prints.
//
// # Why this is a different question again
//
// Every structural audit here ends in a `remedy:` line, and the whole value of
// a source-level audit is that a contributor who has never seen the defect can
// act on it. Three of them print the same instruction —
//
//	"add it to scene.unrenderedFields so the validator refuses it with an
//	 address"
//
// — and it is the *only* instruction any of them gives that is a code change
// to a data structure rather than a feature. It is therefore the one a reader
// under time pressure takes.
//
// That instruction was already false. Measured on the clean tree, before this
// file existed, with the whole suite green:
//
//	unrenderedFields["children"] = "…"
//	  {"type":"text","children":[{"type":"text","text":"X"}]}
//	    -> validates clean. The entry refuses nothing.
//
// The cause was one level in from the map: declaredUnrenderedFields cleared
// the node's subtree branches before re-serialising, and restored their names
// afterwards from a *second*, hand-written list. `children` was in the first
// list and missing from the second, so no node has ever reported declaring
// it, and an entry for it in unrenderedFields could never be consulted.
//
// # What makes this worth a test rather than a quiet correction
//
// The previous turn recorded a *composite*: two guards overlapping on one
// object, the first firing, and following the first one's printed remedy
// returning the suite to green with the defect intact. This is that shape one
// axis further in, and the difference is the part worth naming.
//
// Here the remedies chain. On a `Footer *Node` added to Node and cleared in
// the clone the way that function's own comment reasons a contributor would,
// each line measured from a run:
//
//	the field audit fires  -> "add it to scene.unrenderedFields"
//	following it           -> entry inert; the next guard fires
//	that guard             -> "add a minimal document declaring it"
//	following it           -> "the validator accepted it anyway", whose own
//	                          remedy names refuseUnrendered — which is already
//	                          correct and passes its own dedicated test
//
// Three correct actions for three accurate messages, ending at working code.
// Nothing in the chain names declaredUnrenderedFields. A reader who follows
// it faithfully arrives at a function that is not broken, and the honest
// conclusion available to them there is that the guard is confused. That is
// worse than a guard whose advice is a no-op: it is a guard that argues the
// reader out of the finding.
//
// # What this measures
//
// For every json key Node declares, adding it to unrenderedFields must make a
// document that sets that key fail to validate. That is the literal content of
// the printed remedy, asked of every key the remedy could ever be printed
// about — not of the three keys the map happens to hold today, which is what
// TestEveryUnrenderedFieldIsActuallyRefused checks and why that test could not
// see this.
//
// # Proved in both directions, by counterfactuals actually run
//
// Each line is a run, not an argument. The fix was reverted to the two
// hand-written lists it replaced, and the defects were constructed on top of
// the fixed tree:
//
//	children, with the fix reverted        -> caught, and only children:
//	  the four branches that already worked stayed green, so the finding
//	  is the drift and not a blanket failure
//	a `Footer *Node` cleared by hand and
//	  not restored, derivation blind to it -> caught, and its remedy names
//	  subtreeBranches — the function the old remedy chain never mentioned
//	a `Slots map[string]*Node`, a shape no
//	  spelling list contains               -> handled with no edit to the
//	  derivation, which is what keeps it from being a third spelling list
//	a branch cleared without being
//	  recorded (`children` skipped)        -> caught
//	an ordinary field cleared and not
//	  recorded (`title`)                   -> caught
//
// # Two negative findings, recorded because both cost this file a test
//
// The first widening tried was `typeBuildsOnNode` accepting strings and bools
// — a derivation far too broad, clearing ordinary fields. **It changed
// nothing.** With clear and restore reading one list, a field wrongly called a
// subtree is blanked *and* restored by name; the key still appears, merely
// twice. That is the fix's real content stated as a measurement: the classes
// of bug available to this function shrank from "the two lists disagree" to
// "the one list is wrong", and the second is not reachable by widening.
//
// So TestTheSubtreeDerivationLeavesOrdinaryFieldsAlone below does not guard
// what it was written to guard. The only injection that fails it is clearing a
// field *without recording it* — and measured on `title` and on `on_press`,
// something else always failed first: the audit above for `title`, and
// TestEveryUnrenderedFieldIsActuallyRefused for `on_press`, which the audit
// above skips. It is kept, because it names the consequence in terms of the
// refusal rather than of the key list, and because "another test happens to
// cover this today" is the argument that deleted coverage before. But it is
// not independent evidence, and saying so is the honest state.
//
// The same is true one step further: pointing the reflect walk at `n` instead
// of the copy — the mutation TestAskingWhatANodeDeclaresDoesNotStripItsSubtrees
// exists for — takes down nine tests across three packages, including the
// shipped-scene loop tests. That test is a precise name for a failure that is
// already loud, not the only thing standing between the tree and the defect.
func TestTheRemedyEveryFieldAuditPrintsActuallyRefuses(t *testing.T) {
	probes := remedyProbeDocuments(t)

	// The floor. If the fixture derivation stops producing documents, every
	// assertion below iterates an empty set and reports success having asked
	// nothing — the shape of a deleted test that still prints.
	if len(probes) < 15 {
		t.Fatalf("built only %d probe document(s) from Node's vocabulary; expected at least 15.\n"+
			"consequence: this audit would check almost no key and pass vacuously.\n"+
			"remedy: confirm remedyProbeDocuments still covers Node's json tags.", len(probes))
	}

	for _, p := range probes {
		t.Run(p.key, func(t *testing.T) {
			if _, already := unrenderedFields[p.key]; already {
				// Already refused, and TestEveryUnrenderedFieldIsActuallyRefused
				// holds that entry to its promise with a real reason string.
				return
			}

			// The premise: without the entry the document must be accepted,
			// or the run below proves nothing about the entry.
			doc, err := ParseDocument([]byte(p.body))
			if err != nil {
				t.Fatalf("premise broken: the probe for %q must parse; got %v\n"+
					"remedy: fix the fixture in remedyProbeDocuments — a probe that does not\n"+
					"parse measures the harness, which is the false-accusation direction.", p.key, err)
			}
			if verr := doc.Validate(); verr != nil {
				t.Fatalf("premise broken: the probe for %q must validate clean before the entry\n"+
					"is added, or its refusal afterwards cannot be attributed to the entry; got %v", p.key, verr)
			}

			unrenderedFields[p.key] = "audit probe: added at runtime to hold the printed remedy to its promise"
			defer delete(unrenderedFields, p.key)

			doc, err = ParseDocument([]byte(p.body))
			if err != nil {
				t.Fatalf("reparse: %v", err)
			}
			verr := doc.Validate()
			if verr == nil {
				t.Errorf("%q was added to unrenderedFields and a document declaring it still validates clean.\n\n"+
					"consequence: the remedy three audits in this package print — \"add it to\n"+
					"scene.unrenderedFields so the validator refuses it with an address\" — is a no-op\n"+
					"for this key. A contributor who finds a dead field, takes that advice and sees the\n"+
					"suite go green has closed the ticket with the field still dead: the document\n"+
					"validates, an unsigned bind inside it is never refused, nothing is drawn.\n"+
					"Worse, the guards downstream print further remedies that end at working code, so\n"+
					"the chain argues the reader out of a real finding.\n"+
					"remedy: refuseUnrendered asks declaredUnrenderedFields which fields the node\n"+
					"declares, and that function clears the node's subtree branches before\n"+
					"re-serialising. A branch it clears and does not restore by name is invisible to\n"+
					"the refusal. Check subtreeBranches covers %q — clearing and restoring must read\n"+
					"one derived list, never two written ones.", p.key, p.key)
				return
			}
			if !strings.Contains(verr.Error(), p.key) {
				t.Errorf("the refusal for %q names something else, so a reader cannot tell which\n"+
					"field was rejected: %q", p.key, verr.Error())
			}
		})
	}
}

// The derivation must not lose an ordinary field.
//
// Read the second negative finding above before trusting this test. It was
// written for the false-alarm direction — a derivation so broad it blanks
// ordinary fields — and measurement showed that injection changes nothing,
// because clear and restore now read one list: a field wrongly called a
// subtree is blanked and then restored by name.
//
// The only injection it does fail is a field cleared without being recorded,
// and on both keys tried something else failed alongside it. It stays because
// it states the consequence in terms of the refusal — the thing a reader of a
// failure needs — and because it costs nothing to keep. It is not independent
// evidence.
func TestTheSubtreeDerivationLeavesOrdinaryFieldsAlone(t *testing.T) {
	n := &Node{
		Type:    "text",
		Text:    "x",
		Title:   "t",
		OnPress: "cmd:/help",
		Count:   true,
		Anchor:  "bottom",
	}
	declared := make(map[string]bool)
	for _, key := range n.declaredUnrenderedFields() {
		declared[key] = true
	}

	for _, key := range []string{"type", "text", "title", "on_press", "count", "anchor"} {
		if !declared[key] {
			t.Errorf("a node setting %q does not report declaring it.\n\n"+
				"consequence: refuseUnrendered asks this function which fields the node declares, so\n"+
				"an entry for %q in unrenderedFields would be inert and the field could never be\n"+
				"refused with an address — silently, with the rest of the suite green.\n"+
				"remedy: subtreeBranches is clearing a field that carries no node. typeBuildsOnNode\n"+
				"must answer only for types built out of Node, plus json.RawMessage.", key, key)
		}
	}
}

// The document must not be mutated by being asked what it declares.
//
// The derivation zeroes fields on a copy, and `shallow := *n` is a shallow
// copy: the reflect walk writes through it. If the copy were ever dropped —
// or the walk pointed at `n` — the validator would strip every subtree from
// the document it is validating, and the walk that visits those subtrees runs
// after this call.
//
// Measured, and recorded as the second negative finding above: pointing the
// walk at `n` fails nine tests across three packages, the shipped-scene loop
// tests among them. This one is the precise name for that failure, not the
// only thing that catches it. Worth its four lines for the name alone — a
// reader who sees eight unrelated scene tests go red learns nothing about
// where to look — but it is not load-bearing.
func TestAskingWhatANodeDeclaresDoesNotStripItsSubtrees(t *testing.T) {
	child := &Node{Type: "text", Text: "c"}
	n := &Node{
		Type:        "box",
		Children:    []*Node{child},
		Suffix:      child,
		RowTemplate: child,
		PrefixRaw:   json.RawMessage(`"> "`),
		BorderRaw:   json.RawMessage(`"single"`),
	}

	_ = n.declaredUnrenderedFields()

	if len(n.Children) == 0 || n.Suffix == nil || n.RowTemplate == nil ||
		len(n.PrefixRaw) == 0 || len(n.BorderRaw) == 0 {
		t.Fatalf("declaredUnrenderedFields emptied the node it was asked about: %+v\n\n"+
			"consequence: the validator calls this on every node before walking into its\n"+
			"branches, so a mutating version deletes the subtree it is about to check — every\n"+
			"bind and token inside it would go unvalidated, the containment failure this\n"+
			"package refuses everywhere else.\n"+
			"remedy: the reflect walk must write through the local copy, never through n.", n)
	}
}

// remedyProbe is one json key of Node and a minimal document that sets it.
type remedyProbe struct {
	key  string
	body string
}

// remedyProbeDocuments builds one probe per key in Node's vocabulary.
//
// The documents are written rather than generated, and the reason is a defect
// this package has already paid for: a fragment whose key `scene.Node` does
// not declare is discarded by encoding/json in silence, producing a node
// identical to the control, which a sweep then reports as an engine defect.
// So each fixture is pinned two ways — every key of the vocabulary must have
// one (a new field on Node fails here rather than being skipped in silence),
// and each fixture must actually set the key it claims, checked by parsing it
// and reading the field back.
//
// Keys whose value cannot be set without making the document invalid on some
// *other* axis are declared here with the reason, not omitted: an entry
// nobody can test is how unrenderedFields became decoration in the first
// place.
func remedyProbeDocuments(t *testing.T) []remedyProbe {
	t.Helper()

	fixtures := map[string]string{
		"footer":      `{"root":{"type":"text","text":"x","footer":{"type":"text","text":"f"}}}`,
		"slots":       `{"root":{"type":"text","text":"x","slots":{"f":{"type":"text","text":"c"}}}}`,
		"id":          `{"root":{"type":"text","text":"x","id":"probe"}}`,
		"type":        `{"root":{"type":"text","text":"x"}}`,
		"bind":        `{"root":{"type":"text","bind":"model.name"}}`,
		"when":        `{"root":{"type":"text","text":"x","when":"agent.working"}}`,
		"placeholder": `{"root":{"type":"input","bind":"user.input","placeholder":"ask"}}`,
		"text":        `{"root":{"type":"text","text":"x"}}`,
		"children":    `{"root":{"type":"stack","children":[{"type":"text","text":"x"}]}}`,
		"style":       `{"root":{"type":"text","text":"x","style":{"style":"banner"}}}`,
		"grow":        `{"root":{"type":"stack","grow":1,"children":[{"type":"text","text":"x"}]}}`,
		"weight":      `{"root":{"type":"stack","weight":1,"children":[{"type":"text","text":"x"}]}}`,
		"prefix":      `{"root":{"type":"marquee","bind":"thinking.text","prefix":{"type":"text","text":"~"}}}`,
		"suffix":      `{"root":{"type":"marquee","bind":"thinking.text","suffix":{"type":"text","text":"~"}}}`,
		"anchor":      `{"root":{"type":"overlay","anchor":"bottom","children":[{"type":"text","text":"x"}]}}`,
		"filter_by":   `{"root":{"type":"list","bind":"slash.matches","filter_by":"typed"}}`,
		"count":       `{"root":{"type":"list","bind":"slash.matches","count":true}}`,
		"categories":  `{"root":{"type":"list","bind":"slash.matches","categories":["general"]}}`,
		"border":      `{"root":{"type":"box","border":"single","children":[{"type":"text","text":"x"}]}}`,
		"title":       `{"root":{"type":"box","border":"single","title":"T","children":[{"type":"text","text":"x"}]}}`,
		"min_width":   `{"root":{"type":"overlay","anchor":"bottom","min_width":12,"children":[{"type":"text","text":"x"}]}}`,
		"focus_glow":  `{"root":{"type":"text","text":"x","focus_glow":{"style":"banner"}}}`,
		// scroll graduated (G2): it is rendered on a marquee and refused
		// elsewhere, so it is no longer in unrenderedFields. Its fixture is a
		// marquee with a valid scroll, which validates clean — the premise this
		// audit needs before it adds the runtime entry and checks the printed
		// remedy actually refuses.
		"scroll": `{"root":{"type":"marquee","bind":"thinking.text","scroll":{"speed":2}}}`,

		// reveal graduated (G3): it is rendered on a text node and refused
		// elsewhere, so like scroll it is no longer in unrenderedFields. Its
		// fixture is a text node with a reveal, which validates clean —
		// doc.Validate() is theme-agnostic and only checks the axis (node type),
		// and the anim token is checked separately by ValidateTokens.
		"reveal": `{"root":{"type":"text","text":"x","reveal":{"anim":"reveal.fast"}}}`,

		// transition graduated (G1): the engine draws it at the renderNode
		// chokepoint on every node type, so like scroll and reveal it is not in
		// unrenderedFields. Its axis is universal (no node-type refusal), so the
		// fixture is a plain text node with a transition, which validates clean —
		// doc.Validate() is theme-agnostic and the anim token is checked separately
		// by ValidateTokens.
		"transition": `{"root":{"type":"text","text":"x","transition":{"anim":"default"}}}`,

		// Already in unrenderedFields, so the subtest returns early — their
		// promise is checked by TestEveryUnrenderedFieldIsActuallyRefused with
		// the real reason string. They are listed so a key cannot be missing
		// here by omission.
		"row_template": `{"root":{"type":"list","bind":"agent.todos","row_template":{"type":"text","bind":"model.name"}}}`,
		"on_press":     `{"root":{"type":"text","text":"x","on_press":"cmd:/help"}}`,
	}

	vocab := Vocabulary()
	var out []remedyProbe
	for _, key := range vocab {
		body, ok := fixtures[key]
		if !ok {
			t.Errorf("Node declares the json key %q and this audit has no probe document for it,\n"+
				"so the remedy \"add it to scene.unrenderedFields\" is unverified for that key.\n"+
				"remedy: add a minimal document setting %q to the fixtures map above.", key, key)
			continue
		}
		assertFixtureSetsTheKey(t, key, body)
		out = append(out, remedyProbe{key: key, body: body})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

// assertFixtureSetsTheKey parses a probe and reads the field back off the
// node, so a fixture with a misspelled or wrongly typed key fails here rather
// than looking like a defect in the code under test.
//
// This is the lesson from the universal-property sweep, arriving in a
// different file: `"grow":true` failed to decode because grow is an int, and
// encoding/json discards a key the struct does not declare in silence —
// producing a node identical to the control. A harness bug in the flattering
// direction looks exactly like coverage.
func assertFixtureSetsTheKey(t *testing.T, key, body string) {
	t.Helper()

	doc, err := ParseDocument([]byte(body))
	if err != nil {
		t.Errorf("the probe for %q does not parse: %v", key, err)
		return
	}
	if doc.Root == nil {
		t.Errorf("the probe for %q parsed to no root", key)
		return
	}
	if key == "type" {
		return // every node has one; the fixture cannot be written without it
	}

	rv := reflect.ValueOf(doc.Root).Elem()
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		name, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		if name != key {
			continue
		}
		if rv.Field(i).IsZero() {
			t.Errorf("the probe for %q parsed to a node with that field unset, so it is\n"+
				"byte-identical to a document that never mentioned the key.\n"+
				"consequence: this audit would be measuring its own harness — the false\n"+
				"accusation that looks like coverage.\n"+
				"remedy: fix the key's spelling or its value type in the fixtures map.\n"+
				"document: %s", key, body)
		}
		return
	}
	t.Errorf("no field of Node carries the json tag %q, so the fixtures map and the\n"+
		"vocabulary disagree: %s", key, body)
}
