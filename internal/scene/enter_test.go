package scene

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/theme"
)

// The graduation guard for enter (G4). It is the parallel of transition_test.go
// and reveal_test.go, with the shape enter's own axis dictates: enter is the
// per-row scheduler (SCENES.md Scene 4, G-B), so its refusals are about the two
// things a schedule needs — an interval to stagger against (the stagger token)
// and rows to stagger (children or a row_template). row:false is the degenerate
// whole-container entrance and, like transition, carries no refusal at all.

// row:false is universal: the whole-container entrance dims a node's subtree as
// one unit, which any node can do (a leaf's subtree is itself), so it is
// accepted on every node type with no stagger and no children required. This is
// the pair of transition's universal acceptance — enter's degenerate case is
// transition on the container — and refusing it anywhere would contradict G-B's
// "row:false is identical to putting transition on the container itself".
func TestEnterRowFalseIsAcceptedEverywhere(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{
			name: "on a leaf text node",
			body: `{ "root": { "type": "text", "text": "x", "enter": { "row": false } } }`,
		},
		{
			name: "omitted row (zero value) on a text node",
			body: `{ "root": { "type": "text", "text": "x", "enter": {} } }`,
		},
		{
			name: "on a container with children",
			body: `{ "root": { "type": "stack", "enter": { "row": false }, "children": [ { "type": "text", "text": "x" } ] } }`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := ParseDocument([]byte(tc.body))
			if err != nil {
				t.Fatalf("premise broken: the document should parse; got %v", err)
			}
			if verr := doc.Validate(); verr != nil {
				t.Errorf("an enter %s was refused: %v\n"+
					"consequence: row:false is the whole-container entrance — G-B signs it as identical to\n"+
					"putting transition on the container itself, and transition is universal. A refusal here\n"+
					"would make the degenerate case a special path the design named both forms to avoid.\n"+
					"remedy: validateEnter must return nil whenever Row is false; only row:true is checked.",
					tc.name, verr)
			}
		})
	}
}

// row:true is accepted when it names a stagger and has rows to stagger: a
// container with children, or a list with a row_template. This is the positive
// half of the scheduler — the axis it rides (row count) applies wherever there
// are rows — and it must be accepted there or the engine honouring it at those
// nodes would be a validator lie.
func TestEnterRowTrueIsAcceptedWhereThereAreRows(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{
			name: "on a stack with children",
			body: `{ "root": { "type": "stack", "enter": { "row": true, "stagger": "default" }, "children": [ { "type": "text", "text": "a" }, { "type": "text", "text": "b" } ] } }`,
		},
		{
			name: "on a row with children",
			body: `{ "root": { "type": "row", "enter": { "row": true, "stagger": "default" }, "children": [ { "type": "text", "text": "a" } ] } }`,
		},
		{
			name: "on a list with a row_template",
			body: `{ "root": { "type": "list", "bind": "agent.todos", "enter": { "row": true, "stagger": "default" }, "row_template": { "type": "text", "bind": "row.task" } } }`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := ParseDocument([]byte(tc.body))
			if err != nil {
				t.Fatalf("premise broken: the document should parse; got %v", err)
			}
			if verr := doc.Validate(); verr != nil {
				t.Errorf("a row:true enter %s was refused though it names a stagger and has rows: %v\n"+
					"consequence: the row-count axis applies wherever there are rows, and the engine staggers\n"+
					"them at those nodes; a refusal here would leave a scene the renderer honours failing to\n"+
					"load.\n"+
					"remedy: validateEnter must accept row:true when a stagger token is present and the node\n"+
					"has children or a row_template.",
					tc.name, verr)
			}
		})
	}
}

// A row:true enter with no stagger token is refused with an address: the
// scheduler has no interval to run against. This is enter's first load-time
// refusal, and it is a refusal rather than a default because a zero interval is
// the row:false case the author did not ask for.
func TestEnterRowTrueWithoutStaggerIsRefused(t *testing.T) {
	body := `{ "root": { "type": "stack", "enter": { "row": true }, "children": [ { "type": "text", "text": "x" } ] } }`
	doc, err := ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	verr := doc.Validate()
	if verr == nil {
		t.Fatalf("a row:true enter with no stagger token validated clean.\n" +
			"consequence: the scheduler has no inter-row delay, so it either draws all rows at once\n" +
			"(the row:false case, silently) or the engine invents an interval — either way the author\n" +
			"got behaviour they did not write with nothing saying so.\n" +
			"remedy: validateEnter must refuse row:true with an empty stagger, naming the node.")
	}
	if !strings.Contains(verr.Error(), "stagger") {
		t.Errorf("the refusal does not name %q, so the author cannot tell which field is missing: %v", "stagger", verr)
	}
}

// A row:true enter on a node with no rows — neither children nor a row_template
// — is refused with an address: the row-count axis is a container's, and a leaf
// has no rows to bring in one at a time. This is the pair of reveal's refusal
// off a text node: the axis a prop rides is part of its signature.
func TestEnterRowTrueWithoutRowsIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{
			name: "on a leaf text node",
			body: `{ "root": { "type": "text", "text": "x", "enter": { "row": true, "stagger": "default" } } }`,
		},
		{
			name: "on a marquee (no children, no template)",
			body: `{ "root": { "type": "marquee", "bind": "thinking.text", "enter": { "row": true, "stagger": "default" } } }`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := ParseDocument([]byte(tc.body))
			if err != nil {
				t.Fatalf("premise broken: %v", err)
			}
			verr := doc.Validate()
			if verr == nil {
				t.Fatalf("a row:true enter %s validated clean though the node has no rows.\n"+
					"consequence: the scheduler has nothing to stagger, so it silently does nothing — the\n"+
					"drop this project refuses everywhere.\n"+
					"remedy: validateEnter must refuse row:true on a node with neither children nor a\n"+
					"row_template, naming the node.", tc.name)
			}
			if !strings.Contains(verr.Error(), "row") {
				t.Errorf("the refusal for %s does not mention rows, so the diagnosis is unclear: %v", tc.name, verr)
			}
		})
	}
}

// An enter's stagger token is checked against the theme's anim section, the
// same net a style token, a reveal token and a transition token get (TOKENS.md):
// a named token the theme does not define is a load error with an address. Only
// row:true consults a stagger, and validateEnter has already refused a row:true
// with an empty one, so ValidateTokens sees only non-empty names to resolve.
// ValidateTokens, not doc.Validate(), because the token check needs the active
// theme.
func TestEnterStaggerTokenIsCheckedAgainstTheTheme(t *testing.T) {
	thm := theme.SOBRIA()

	// A named token the theme defines validates clean.
	body := `{ "root": { "type": "stack", "enter": { "row": true, "stagger": "reveal.fast" }, "children": [ { "type": "text", "text": "x" } ] } }`
	doc, err := ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	if errs := ValidateTokens(doc, thm); len(errs) != 0 {
		t.Errorf("an enter naming a defined stagger token was flagged: %v\n"+
			"remedy: reveal.fast is in the factory anim section; it must not be reported undefined.", errs)
	}

	// An undefined stagger token is a load error naming the token and the prop.
	bad := `{ "root": { "type": "stack", "enter": { "row": true, "stagger": "no.such.token" }, "children": [ { "type": "text", "text": "x" } ] } }`
	doc, err = ParseDocument([]byte(bad))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	errs := ValidateTokens(doc, thm)
	if len(errs) == 0 {
		t.Fatalf("an enter naming a stagger token the theme does not define validated clean.\n" +
			"consequence: the entrance silently never staggers and nothing says why — TOKENS.md signs\n" +
			"that a prop naming an absent timing token fails the load with an address.\n" +
			"remedy: collectTokenErrors must check the enter's stagger against thm.HasAnim.")
	}
	if !strings.Contains(errs[0].Token, "no.such.token") {
		t.Errorf("the token error names %q, want the undefined token so the author can act", errs[0].Token)
	}
	if !strings.Contains(errs[0].NodeType, "enter") {
		t.Errorf("the token error's node type is %q, want it to name enter so a reader can tell which\n"+
			"prop's token was undefined (a transition and an enter can sit on the same node)",
			errs[0].NodeType)
	}
}
