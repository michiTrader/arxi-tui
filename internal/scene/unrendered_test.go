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
// (it is exactly how the shipped SOARIA menu is built).
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
