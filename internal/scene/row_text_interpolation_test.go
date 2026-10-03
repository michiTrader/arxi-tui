package scene

import (
	"errors"
	"strings"
	"testing"
)

// The first live eval run of the row_template case measured a model writing
// `"text": "{row.turns}•"`. Only on_press interpolates {row.<field>}, so the engine
// accepted the document and painted the braces literally: validation was clean, so
// the repair loop had nothing to repair and the case scored `incomplete` against the
// model for a mistake the engine let through. These tests hold the refusal that
// closes it, and its counterfactual halves, so the rule cannot be satisfied by
// refusing every brace.

const rowTextTemplate = `{"root":{"type":"stack","children":[
  {"type":"list","bind":"team.members","row_template":{"type":"row","children":[
    {"type":"text","bind":"row.role"},
    {"type":"text","text":"{row.turns}•"}
  ]}}
]}}`

// The core refusal: a {row.} reference in drawn text is an error with an address
// that names the remedy.
func TestRowInterpolationInDrawnTextIsRefusedWithAnAddress(t *testing.T) {
	doc, err := ParseDocument([]byte(rowTextTemplate))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	verr := doc.Validate()
	if verr == nil {
		t.Fatal("a text node containing {row.turns} validated clean; the braces would draw literally on screen and the repair loop would see no error to fix.\n" +
			"remedy: Validate must refuse {row. in a node's drawn text (validateNoRowInterpolationInText).")
	}
	var se *Error
	if !errors.As(verr, &se) {
		t.Fatalf("the refusal is not a *scene.Error, so it carries no address: %v", verr)
	}
	if se.Loc == (Loc{}) {
		t.Errorf("the refusal carries no file:line, so the repair loop cannot locate the node: %v", verr)
	}
	// Line 4 of the document is the offending node; pointing at a neighbour would
	// send the model to edit a node that is fine.
	if se.Loc.Line != 4 {
		t.Errorf("the refusal points at line %d, want 4 (the node holding {row.turns}): %v", se.Loc.Line, verr)
	}
	for _, want := range []string{"{row.", "on_press", `"bind": "row.<field>"`} {
		if !strings.Contains(verr.Error(), want) {
			t.Errorf("the refusal %q does not contain %q; it must say what is wrong and the working spelling so one edit repairs it", verr.Error(), want)
		}
	}
}

// The same mistake outside any template is equally dead -- there is no row at all --
// and is refused too, so the rule does not depend on where the node sits.
func TestRowInterpolationInTextOutsideATemplateIsRefused(t *testing.T) {
	doc, err := ParseDocument([]byte(`{"root":{"type":"text","text":"turns {row.turns}"}}`))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	if doc.Validate() == nil {
		t.Error("{row.turns} in the text of a node outside a template validated clean; it can never resolve")
	}
}

// A malformed (unclosed) reference is the same intent and the same literal on
// screen, so it must not slip past a refusal that waited for a closing brace.
func TestAnUnclosedRowReferenceInTextIsAlsoRefused(t *testing.T) {
	doc, err := ParseDocument([]byte(`{"root":{"type":"text","text":"{row.turns"}}`))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	if doc.Validate() == nil {
		t.Error("an unclosed {row.turns in text validated clean; it draws literally exactly like the closed form")
	}
}

// title and placeholder are painted as authored too, so they are held to the same rule.
func TestRowInterpolationInTitleAndPlaceholderIsRefused(t *testing.T) {
	for _, body := range []string{
		`{"root":{"type":"box","title":"{row.id}"}}`,
		`{"root":{"type":"input","bind":"user.input","placeholder":"{row.id}"}}`,
	} {
		doc, err := ParseDocument([]byte(body))
		if err != nil {
			t.Fatalf("premise broken for %s: %v", body, err)
		}
		if doc.Validate() == nil {
			t.Errorf("%s validated clean; title and placeholder draw verbatim, so the braces would show", body)
		}
	}
}

// Counterfactual: on_press interpolation is the one place {row.} works and must stay
// accepted, or the refusal would have broken Scene 9's `cmd:/agent {row.id}` and the
// PROVIDERS screen's enable/disable buttons.
func TestRowInterpolationInOnPressIsStillAccepted(t *testing.T) {
	body := `{"root":{"type":"list","bind":"team.members","row_template":{"type":"row","children":[
	  {"type":"text","bind":"row.role"},
	  {"type":"button","text":"go","on_press":"cmd:/agent {row.id}"}
	]}}}`
	doc, err := ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	if verr := doc.Validate(); verr != nil {
		t.Errorf("a {row.id} interpolation in on_press was refused: %v\n"+
			"consequence: every row button (Scene 9, PROVIDERS) stops loading.\n"+
			"remedy: the text rule must look at text/title/placeholder only, never at on_press.", verr)
	}
}

// Counterfactual: the working spelling the refusal recommends must validate, or the
// error would send the model to a second error.
func TestTheBindSpellingTheRefusalRecommendsValidates(t *testing.T) {
	body := `{"root":{"type":"list","bind":"team.members","row_template":{"type":"row","children":[
	  {"type":"text","bind":"row.turns"},
	  {"type":"text","text":"•"}
	]}}}`
	doc, err := ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	if verr := doc.Validate(); verr != nil {
		t.Errorf("the recommended bind spelling was refused: %v\n"+
			"consequence: the refusal's own advice leads to another error and the model cannot converge.", verr)
	}
}

// Counterfactual: ordinary braces are not row references, so text that merely
// contains a brace (a code sample, a JSON hint) must still load.
func TestOrdinaryBracesInTextAreNotRefused(t *testing.T) {
	doc, err := ParseDocument([]byte(`{"root":{"type":"text","text":"use {braces} freely, even {row} alone"}}`))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	if verr := doc.Validate(); verr != nil {
		t.Errorf("text with ordinary braces was refused: %v\nremedy: only the literal prefix \"{row.\" is the refused spelling.", verr)
	}
}
