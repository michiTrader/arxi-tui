package scene

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/theme"
)

// The graduation guard for transition (G1). It is the parallel of
// reveal_test.go and scroll_test.go with one deliberate difference: transition
// has no node-type refusal, because the SGR-intensity axis it rides is not
// type-specific the way reveal's character count (text) and scroll's horizontal
// offset (marquee) are. Every node that draws styled content can be dimmed, and
// the design places transition on both text nodes and containers (SCENES.md
// Scene 4, G-B). So the positive half here is *universal* — a transition is
// accepted on any node type — and the only load-time refusal is the timing
// token, checked against the active theme.

// A transition is accepted on every node type: the axis is universal, so unlike
// reveal (text only) and scroll (marquee only) there is nothing to refuse on the
// node type. This is the pair of the reveal refusal test, inverted: where reveal
// refuses off text, transition must accept everywhere, or the universal reach
// the engine relies on (honouring it at the renderNode chokepoint) would be a
// lie the validator tells.
func TestATransitionIsAcceptedOnEveryNodeType(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{
			name: "on a text node",
			body: `{ "root": { "type": "text", "text": "hello", "transition": { "anim": "default" } } }`,
		},
		{
			name: "on a marquee node",
			body: `{ "root": { "type": "marquee", "bind": "thinking.text", "transition": { "anim": "default" } } }`,
		},
		{
			name: "on a container (stack)",
			body: `{ "root": { "type": "stack", "transition": { "anim": "default" }, "children": [ { "type": "text", "text": "x" } ] } }`,
		},
		{
			name: "nested in a box",
			body: `{ "root": { "type": "stack", "children": [ { "type": "box", "border": "single", "transition": { "anim": "default" }, "children": [ { "type": "text", "text": "x" } ] } ] } }`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := ParseDocument([]byte(tc.body))
			if err != nil {
				t.Fatalf("premise broken: the document should parse; got %v", err)
			}
			if verr := doc.Validate(); verr != nil {
				t.Errorf("a transition %s was refused: %v\n"+
					"consequence: transition rides the SGR-intensity axis, which is not type-specific —\n"+
					"the engine honours it at the renderNode chokepoint on every node type, and the\n"+
					"design (G-B) places it on both text nodes and containers. A node-type refusal here\n"+
					"would contradict the axis and break `enter`'s row:false container entrance (G4).\n"+
					"remedy: transition must carry no node-type refusal; only its timing token is checked.",
					tc.name, verr)
			}
		})
	}
}

// A transition's timing token is checked against the theme's anim section, the
// same net a style token and a reveal token get (TOKENS.md): a named token the
// theme does not define is a load error with an address, and an empty token
// resolves anim.default (Q8), which the factory theme ships. ValidateTokens, not
// doc.Validate(), because the token check needs the active theme — doc.Validate()
// is theme-agnostic. This is transition's only load-time refusal, so it is the
// one the counterfactual disarms.
func TestATransitionTimingTokenIsCheckedAgainstTheTheme(t *testing.T) {
	thm := theme.SOBRIA()

	// A named token the theme defines, and the empty (→ default) case, both clean.
	for _, tc := range []struct {
		name string
		body string
	}{
		{"named token present", `{ "root": { "type": "text", "text": "x", "transition": { "anim": "reveal.fast" } } }`},
		{"empty token → default", `{ "root": { "type": "text", "text": "x", "transition": {} } }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := ParseDocument([]byte(tc.body))
			if err != nil {
				t.Fatalf("premise broken: %v", err)
			}
			if errs := ValidateTokens(doc, thm); len(errs) != 0 {
				t.Errorf("a transition (%s) was flagged though the theme defines its token: %v\n"+
					"remedy: an empty anim resolves anim.default (Q8), and reveal.fast is in the factory\n"+
					"anim section; neither should be reported undefined.", tc.name, errs)
			}
		})
	}

	// An undefined token is a load error naming the token.
	body := `{ "root": { "type": "text", "text": "x", "transition": { "anim": "no.such.token" } } }`
	doc, err := ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	errs := ValidateTokens(doc, thm)
	if len(errs) == 0 {
		t.Fatalf("a transition naming an anim token the theme does not define validated clean.\n" +
			"consequence: the entrance silently never animates and nothing says why — TOKENS.md signs\n" +
			"that a prop naming an absent timing token fails the load with an address.\n" +
			"remedy: collectTokenErrors must check the transition's token against thm.HasAnim.")
	}
	if !strings.Contains(errs[0].Token, "no.such.token") {
		t.Errorf("the token error names %q, want the undefined token so the author can act", errs[0].Token)
	}
	if !strings.Contains(errs[0].NodeType, "transition") {
		t.Errorf("the token error's node type is %q, want it to name transition so a reader can tell\n"+
			"which prop's token was undefined (a reveal and a transition can sit on the same node)",
			errs[0].NodeType)
	}
}
