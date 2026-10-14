package scene

import (
	"errors"
	"strings"
	"testing"
)

// D1, option b. The first live eval run measured a model writing
// `"text": "{row.turns}•"`. That used to be refused (the braces drew literally); it now
// works as written: {row.<field>} in text, title and placeholder is substituted per row
// by the renderer, and held at load to the same row schema as on_press. These tests hold
// the validation half and the escape; the drawing half is in internal/engine.

const rowTextTemplate = `{"root":{"type":"stack","children":[
  {"type":"list","bind":"team.members","row_template":{"type":"row","children":[
    {"type":"text","bind":"row.role"},
    {"type":"text","text":"{row.turns}•"}
  ]}}
]}}`

func validate(t *testing.T, body string) error {
	t.Helper()
	doc, err := ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	return doc.Validate()
}

// The reported case now loads: a field of the row's schema inside its template.
func TestRowReferenceInDrawnTextInsideATemplateIsAccepted(t *testing.T) {
	if err := validate(t, rowTextTemplate); err != nil {
		t.Fatalf("{row.turns} inside a team.members template was refused: %v", err)
	}
}

// Title and placeholder are drawn text too.
func TestRowReferenceInTitleAndPlaceholderIsAcceptedInATemplate(t *testing.T) {
	for _, node := range []string{
		`{"type":"box","title":"{row.id}"}`,
		`{"type":"input","bind":"user.input","placeholder":"{row.id}"}`,
	} {
		body := `{"root":{"type":"list","bind":"team.members","row_template":` + node + `}}`
		if err := validate(t, body); err != nil {
			t.Errorf("%s: %v", node, err)
		}
	}
}

// A field the element type does not declare is a load-time refusal with an address,
// exactly as for on_press: the typo must not become braces on screen.
func TestAnUnknownRowFieldInTextIsRefusedWithAnAddress(t *testing.T) {
	body := strings.Replace(rowTextTemplate, "{row.turns}", "{row.nope}", 1)
	verr := validate(t, body)
	if verr == nil {
		t.Fatal("{row.nope} validated clean; it would never resolve")
	}
	var se *Error
	if !errors.As(verr, &se) || se.Loc.Line != 4 {
		t.Errorf("want a *scene.Error at line 4, got %v (%#v)", verr, se)
	}
	for _, want := range []string{"{row.nope}", "row schema"} {
		if !strings.Contains(verr.Error(), want) {
			t.Errorf("the refusal %q does not contain %q", verr.Error(), want)
		}
	}
}

// Outside any template there is no row, so the reference can never resolve. The refusal
// names the escape for anyone who meant the characters.
func TestRowReferenceInTextOutsideATemplateIsRefusedAndNamesTheEscape(t *testing.T) {
	verr := validate(t, `{"root":{"type":"text","text":"turns {row.turns}"}}`)
	if verr == nil {
		t.Fatal("{row.turns} outside a template validated clean")
	}
	if !strings.Contains(verr.Error(), "{{row.turns}}") {
		t.Errorf("the refusal must name the escape: %v", verr)
	}
}

// An unclosed reference is the same intent and would draw literally.
func TestAnUnclosedRowReferenceInTextIsRefused(t *testing.T) {
	body := strings.Replace(rowTextTemplate, "{row.turns}•", "{row.turns", 1)
	if validate(t, body) == nil {
		t.Error("an unclosed {row.turns validated clean")
	}
}

// The escape: {{row.x}} is the characters, needs no template and no schema.
func TestTheDoubledBraceEscapeIsAcceptedAnywhere(t *testing.T) {
	for _, body := range []string{
		`{"root":{"type":"text","text":"write {{row.turns}} to show a field"}}`,
		strings.Replace(rowTextTemplate, "{row.turns}•", "{{row.nope}} stays", 1),
	} {
		if err := validate(t, body); err != nil {
			t.Errorf("the escape was refused: %v\n%s", err, body)
		}
	}
}

// on_press interpolation is untouched.
func TestRowInterpolationInOnPressIsStillAccepted(t *testing.T) {
	body := `{"root":{"type":"list","bind":"team.members","row_template":{"type":"row","children":[
	  {"type":"text","bind":"row.role"},
	  {"type":"button","text":"go","on_press":"cmd:/agent {row.id}"}
	]}}}`
	if err := validate(t, body); err != nil {
		t.Errorf("a {row.id} interpolation in on_press was refused: %v", err)
	}
}

// Ordinary braces are not row references.
func TestOrdinaryBracesInTextAreNotRefused(t *testing.T) {
	if err := validate(t, `{"root":{"type":"text","text":"use {braces} freely, even {row} alone, and {rows.x}"}}`); err != nil {
		t.Errorf("text with ordinary braces was refused: %v", err)
	}
}

func TestRowTextRefsFindsReferencesAndSkipsTheEscape(t *testing.T) {
	cases := []struct {
		in       string
		tokens   string
		unclosed bool
	}{
		{"plain", "", false},
		{"{row.id}", "row.id", false},
		{"{row.a} and {row.b}", "row.a,row.b", false},
		{"{{row.a}} and {row.b}", "row.b", false},
		{"{row.a", "", true},
		{"{{row.a", "", false},
		{"x{row.a}y{row.", "row.a", true},
	}
	for _, c := range cases {
		tokens, unclosed := rowTextRefs(c.in)
		if got := strings.Join(tokens, ","); got != c.tokens || unclosed != c.unclosed {
			t.Errorf("%q: tokens %q unclosed %v, want %q %v", c.in, got, unclosed, c.tokens, c.unclosed)
		}
	}
}

func TestExpandRowText(t *testing.T) {
	row := map[string]string{"row.id": "7", "row.role": "dev"}
	cases := map[string]string{
		"{row.role}#{row.id}":      "dev#7",
		"{{row.id}} is {row.id}":   "{row.id} is 7",
		"no braces":                "no braces",
		"{braces} {row} {rows.id}": "{braces} {row} {rows.id}",
		"{row.missing} stays":      "{row.missing} stays",
		"tail {row.id":             "tail {row.id",
		"{{row.id":                 "{row.id",
		"{row.id}{row.id}":         "77",
		"•{row.role}•":             "•dev•",
	}
	for in, want := range cases {
		if got := ExpandRowText(in, row); got != want {
			t.Errorf("ExpandRowText(%q) = %q, want %q", in, got, want)
		}
	}
	if got := ExpandRowText("{row.id}", nil); got != "{row.id}" {
		t.Errorf("with no row the text is left as written: %q", got)
	}
}
