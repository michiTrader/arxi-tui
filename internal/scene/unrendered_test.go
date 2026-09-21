package scene

import (
	"errors"
	"strings"
	"testing"
)

// The rule this file defends: a construction the validator accepts is a
// construction the engine draws. Where that is not true yet, the validator
// must refuse with an address — never accept in silence.
//
// This is the third instance of one defect class, and the first to reach the
// measuring instrument rather than a scene. The first two were a style key the
// validator accepted and styleName() dropped, and a border token the validator
// checked and both drawing paths ignored; each reported success and showed the
// wrong screen. `row_template` is the same shape one level up: the validator
// walks into it for binds *and* for tokens, loc.go gives it an address
// (templatePath), binds_audit_test.go collects through it, and eval/grade.go's
// CollectBinds walks it by name with a comment citing SCENES.md Q10 — five
// places that all say the field is live — while internal/engine reads it in
// exactly zero.
//
// Why refusing is the fix and implementing is not. `row_template`'s semantics
// are relative binds (`row.kind`, Q10), and the `row.*` namespace is signed
// nowhere in BINDS.md — it belongs to Scene 5, which is Phase 3 work. Drawing
// the field "somehow" now would invent format ahead of the phase meant to
// design it, the same mistake PLAN.md names about pinning a golden before the
// phase that needs it. The honest engine behaviour is a refusal that says the
// field is not rendered yet, and says where.
func TestRowTemplateIsRefusedWhileTheEngineCannotDrawIt(t *testing.T) {
	// A well-formed, fully signed document whose only sin is using a field
	// the render path does not read.
	body := `{ "root": { "type": "stack", "children": [
	  { "id": "cmds", "type": "list", "bind": "agent.todos",
	    "row_template": { "type": "text", "bind": "model.name" } }
	]}}`

	doc, err := ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: the document should parse; got %v", err)
	}

	verr := doc.Validate()
	if verr == nil {
		t.Fatalf("row_template validated clean.\n" +
			"consequence: the scene loads, reports success, and the engine draws the\n" +
			"list without the template — the outcome that shows the wrong screen while\n" +
			"every signal says the document is fine. Worse here than in a scene: the\n" +
			"eval grader counts a bind it finds inside the template, so a corpus answer\n" +
			"can score converged with the field never on screen.\n" +
			"remedy: refuse the field until internal/engine renders it.")
	}

	// The refusal has to carry an address, like every other refusal in this
	// package (Phase 1.6). A reason with no file:line is a reason the model
	// in Phase 2's repair loop cannot act on.
	var se *Error
	if !errors.As(verr, &se) {
		t.Fatalf("refusal is not a *scene.Error, so it carries no address: %v", verr)
	}
	if se.Loc == (Loc{}) {
		t.Errorf("refusal carries no address: %v", verr)
	}

	// The message must name the field and say it is unimplemented rather
	// than malformed. "invalid" would send the model hunting for a typo in
	// a field it spelled correctly.
	msg := verr.Error()
	if !strings.Contains(msg, "row_template") {
		t.Errorf("refusal does not name the offending field: %q", msg)
	}
	if !strings.Contains(msg, "not yet rendered") {
		t.Errorf("refusal does not say the field is unimplemented, so a reader cannot\n"+
			"tell a missing feature from a malformed document: %q", msg)
	}
}

// The refusal must reach every arm the walk already covers. A guard that only
// checked the root would let the same silent drop through one level down —
// and nesting a list inside a box or an overlay is how real scenes are written
// (it is exactly how the shipped SOBRIA menu is built).
func TestRowTemplateIsRefusedWhereverItAppears(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{
			name: "nested in a box",
			body: `{ "root": { "type": "stack", "children": [
			  { "type": "box", "border": "single", "children": [
			    { "type": "list", "bind": "agent.todos",
			      "row_template": { "type": "text", "bind": "model.name" } } ] }
			]}}`,
		},
		{
			name: "inside an overlay",
			body: `{ "root": { "type": "stack", "children": [
			  { "type": "overlay", "anchor": "bottom", "children": [
			    { "type": "list", "bind": "slash.matches",
			      "row_template": { "type": "text", "bind": "model.name" } } ] }
			]}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := ParseDocument([]byte(tc.body))
			if err != nil {
				t.Fatalf("premise broken: %v", err)
			}
			if err := doc.Validate(); err == nil {
				t.Errorf("row_template %s validated clean; the guard only covers the root", tc.name)
			}
		})
	}
}

// A refusal must not cost the refusals that already worked. The unsigned-bind
// check inside a template predates this guard, and it is the more specific
// complaint: if both apply, the reader is better served by the one naming the
// bind. This pins the order rather than leaving it to walk sequence.
func TestAnUnsignedBindInsideATemplateStillNamesTheBind(t *testing.T) {
	body := `{ "root": { "type": "stack", "children": [
	  { "type": "list", "bind": "agent.todos",
	    "row_template": { "type": "text", "bind": "totally.invented" } }
	]}}`

	doc, err := ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	verr := doc.Validate()
	if verr == nil {
		t.Fatal("an unsigned bind inside a row_template validated clean")
	}
	if !strings.Contains(verr.Error(), "totally.invented") {
		t.Errorf("the more specific refusal was lost behind the unimplemented-field\n"+
			"guard; a reader who mistyped a bind is told only that the field is\n"+
			"unsupported: %q", verr.Error())
	}
}

// The guard for the guard: every entry in unrenderedFields must actually
// refuse the field it names.
//
// This is the fourth instance of the project's recurring defect class, and the
// first one located in the instrument rather than in the engine. The three
// before it were a style key the validator accepted and styleName() dropped, a
// border token both drawing paths ignored, and row_template — each a
// construction that cleared every check and drew nothing. This one is the same
// shape applied to the check itself: unrenderedFields is written as a general
// map from json field name to reason, and refuseUnrendered read exactly one
// key out of it as a constant, from the one call site that already knew the
// answer. Both halves agreed because there was only ever one entry, so the
// map's generality was decoration and a second entry would be inert.
//
// What makes it worth a test rather than a quiet correction is where the
// inertness surfaces. TestEveryNodeFieldIsEitherRenderedRefusedOrJustified
// tells a contributor who finds a dead field to "add it to
// scene.unrenderedFields so the validator refuses it with an address". Taking
// that advice satisfied the audit — the field is skipped as refused — while
// the validator went on accepting it in silence. The audit exists because
// three dead fields slipped past human reading; its documented remedy turned
// the fourth into a closed ticket with the defect still shipping. A guard that
// launders a finding into a false clearance is worse than no guard, because
// the suite now argues against the person who was right.
//
// Measured, not assumed: injecting a second entry ("categories") and asking
// the validator about a list that declares it returned nil, and the audit
// passed. Both halves of that observation are the failure.
func TestEveryUnrenderedFieldIsActuallyRefused(t *testing.T) {
	// A document per entry, built so the only thing it does wrong is
	// declare the field under test. Anything an entry cannot be exercised
	// from is a fixture gap, and it fails loudly rather than skipping:
	// silently not testing an entry is how the map became decoration.
	fixtures := map[string]string{
		"row_template": `{ "root": { "type": "list", "bind": "agent.todos",
		  "row_template": { "type": "text", "bind": "model.name" } } }`,

		// on_press and scroll are universal properties SCENES.md promises
		// and no layer implemented. Before they were declared on Node they
		// could not be refused at all: encoding/json dropped the key, so
		// these two documents rendered byte-identically to the same scene
		// without them. Their fixtures are ordinary nodes, because that is
		// the point — the format says any node may carry these, so the
		// refusal must not depend on picking an exotic node type.
		"on_press": `{ "root": { "type": "text", "bind": "model.name",
		  "on_press": "cmd:/help" } }`,
		"scroll": `{ "root": { "type": "markdown", "bind": "chat.history",
		  "scroll": { "speed": 2, "pause_when": "agent.working" } } }`,
	}

	if len(unrenderedFields) == 0 {
		t.Skip("no unrendered fields declared; nothing to hold to its promise")
	}

	for field, because := range unrenderedFields {
		t.Run(field, func(t *testing.T) {
			body, ok := fixtures[field]
			if !ok {
				t.Fatalf("unrenderedFields declares %q (%s) but this test has no document that\n"+
					"exercises it, so the entry's promise is unverified. An entry nobody can\n"+
					"test is how this map became decoration in the first place.\n"+
					"remedy: add a minimal document declaring %q to the fixtures map above.",
					field, because, field)
			}

			doc, err := ParseDocument([]byte(body))
			if err != nil {
				t.Fatalf("premise broken: the fixture for %q should parse; got %v", field, err)
			}

			verr := doc.Validate()
			if verr == nil {
				t.Fatalf("unrenderedFields declares %q unrendered, and the validator accepted it anyway.\n"+
					"consequence: the entry is inert. A scene setting %q loads, reports success, and the\n"+
					"engine drops it — the silent-drop outcome this map exists to convert into an\n"+
					"addressed refusal. Worse, the audit test treats a listed field as handled, so\n"+
					"adding the entry is what stopped anything from complaining.\n"+
					"remedy: make refuseUnrendered consult the whole map, keyed by the fields the\n"+
					"node actually declares, rather than reading one key as a constant.",
					field, field)
			}

			// Same address discipline the rest of the package obeys
			// (invariant 4). A refusal Phase 2's repair loop cannot
			// locate is a refusal the model must guess at.
			var se *Error
			if !errors.As(verr, &se) {
				t.Fatalf("the refusal for %q is not a *scene.Error, so it carries no address: %v", field, verr)
			}
			if se.Loc == (Loc{}) {
				t.Errorf("the refusal for %q carries no address: %v", field, verr)
			}
			if !strings.Contains(verr.Error(), field) {
				t.Errorf("the refusal does not name %q, so a reader cannot tell which field was\n"+
					"rejected: %q", field, verr.Error())
			}
			if !strings.Contains(verr.Error(), "not yet rendered") {
				t.Errorf("the refusal for %q does not say the field is unimplemented, so a reader\n"+
					"cannot tell a missing feature from a malformed document: %q", field, verr.Error())
			}
		})
	}
}

// The map must be consulted by key, not by coincidence.
//
// The test above passes as long as every *current* entry refuses, and with one
// entry that is also true of the hardcoded version it replaced: reading
// `unrenderedFields["row_template"]` refuses row_template correctly. So that
// test alone cannot distinguish a general lookup from a constant one, and the
// defect it is meant to pin would survive it. This is the mutual-masking
// lesson from the `when` work, applied before the fact rather than after: two
// checks that can only be satisfied together measure nothing individually.
//
// So this one adds an entry at runtime and asserts the validator's behaviour
// changes. It is the only form of the question that fails against a constant
// lookup, because a constant cannot see a key it was not written with.
func TestUnrenderedFieldsIsReadAsAMapNotAConstant(t *testing.T) {
	const probe = "min_width"

	// The field must be accepted before the entry exists, or the test
	// proves nothing about the entry.
	body := `{ "root": { "type": "overlay", "anchor": "top-right", "min_width": 12,
	  "children": [ { "type": "text", "bind": "model.name" } ] } }`

	doc, err := ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	if verr := doc.Validate(); verr != nil {
		t.Fatalf("premise broken: %q is rendered today and must validate clean before the\n"+
			"probe adds it to the map; got %v", probe, verr)
	}

	unrenderedFields[probe] = "test probe: added at runtime to prove the map is read by key"
	defer delete(unrenderedFields, probe)

	doc, err = ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	verr := doc.Validate()
	if verr == nil {
		t.Fatalf("a new entry in unrenderedFields changed nothing: %q is still accepted.\n"+
			"consequence: the map is not consulted — the refusal is wired to one hardcoded\n"+
			"field name, so every entry after the first is inert, and the audit test's\n"+
			"advertised remedy ('add it to scene.unrenderedFields') silently does nothing.\n"+
			"remedy: look the node's declared fields up in the map.", probe)
	}
	if !strings.Contains(verr.Error(), probe) {
		t.Errorf("the refusal names something other than the field that was added: %q", verr.Error())
	}
}

// TestTheRefusedFieldIsChosenByRuleAndNotByArrivalOrder closes a hole the
// first mutation sweep of this package found, and the measurement corrected
// the weld's own premise on the way.
//
// refuseUnrendered sorts its candidates, and the comment gives the reason:
// "an address that moves between runs is not an address." Deleting the sort
// left all 59 tests here green.
//
// The first test written for it asserted the wrong property. It declared
// row_template and on_press with real values and looped 200 times expecting
// the unsorted version to vary -- and it did not vary, so the test passed
// with the weld applied. Measuring instead of assuming explains why:
// declaredUnrenderedFields ALREADY sorts what it returns, so on that path the
// second sort is redundant and removing it changes nothing.
//
// The sort earns its place on the other path. refuseUnrendered unions that
// sorted list with the parser's declaredKeys, APPENDING them, and
// declaredKeys is in source order. A field reaches it only through there when
// omitempty dropped it from the marshal -- i.e. when it is written with its
// type's zero value, which the comment above refuseUnrendered records as a
// real authoring shape: `"on_press": ""` while clearing a property.
//
// Measured, on a node declaring `"on_press": ""` and `"scroll": null`:
//
//	with sort:     on_press  (300/300, and 100/100 with the source keys swapped)
//	without sort:  scroll    (200/200, both source orderings)
//
// So the failure is not the instability the weld's description predicted --
// it is stable and WRONG, which is worse. It silently changes which of two
// fields the author is told about, and Phase 2's repair loop charges the
// model a turn per attempt against whichever answer it gets. The assertion is
// therefore on the identity of the chosen field, not on run-to-run
// repeatability: the version with no rule repeats perfectly well.
func TestTheRefusedFieldIsChosenByRuleAndNotByArrivalOrder(t *testing.T) {
	// Both fields written with their ZERO values, so omitempty drops them
	// from the marshal and they can only arrive via declaredKeys -- the
	// unsorted, source-ordered half of the union. A node declaring them with
	// real values cannot distinguish the two implementations at all.
	const src = `{"root":{"type":"list","bind":"team.members",
		"on_press":"", "scroll":null}}`

	doc, err := ParseDocument([]byte(src))
	if err != nil {
		t.Fatalf("premise broken: the document should parse; got %v", err)
	}
	verr := doc.Validate()
	if verr == nil {
		t.Fatalf("premise broken: a node declaring two unrendered fields with zero\n" +
			"values must still be refused -- that is the omitempty hole\n" +
			"refuseUnrendered's declaredKeys union exists to close -- or this test\n" +
			"measures nothing")
	}

	// on_press is the alphabetically first of the two, so it is the answer a
	// rule produces. scroll is the answer arrival order produces. Naming
	// both in the assertion is what makes the failure legible.
	msg := verr.Error()
	if !strings.Contains(msg, "on_press") {
		t.Errorf("refusal names %q.\n"+
			"want it to name on_press, the alphabetically first candidate.\n\n"+
			"consequence: the field is being chosen by the order the parser recorded the\n"+
			"source keys rather than by a rule. That is stable -- so it will not look\n"+
			"flaky -- and it is stable on the WRONG field: the author is told about\n"+
			"scroll while on_press sits equally unrendered beside it, and re-ordering two\n"+
			"keys in the source file silently changes the diagnosis. Phase 2's repair loop\n"+
			"charges the model a turn per attempt against whichever answer it is handed.\n"+
			"remedy: refuseUnrendered must sort the union of declaredUnrenderedFields and\n"+
			"declaredKeys before choosing, not just rely on the first being sorted already.",
			msg)
	}

	// The other field must still be refusable: if scroll were unreachable
	// the test above would pass for the wrong reason -- a single-candidate
	// list cannot demonstrate a choice.
	const onlyScroll = `{"root":{"type":"list","bind":"team.members","scroll":null}}`
	d2, err := ParseDocument([]byte(onlyScroll))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	e2 := d2.Validate()
	if e2 == nil || !strings.Contains(e2.Error(), "scroll") {
		t.Fatalf("control failed: scroll alone is not refused (%v), so the assertion\n"+
			"above could be satisfied by a list that never contained it", e2)
	}
}
