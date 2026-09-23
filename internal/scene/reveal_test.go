package scene

import (
	"errors"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/theme"
)

// The graduation guard for reveal (G3), the parallel of scroll_test.go: a text
// node draws it, every other node type is refused with an address, and the
// timing token it names is checked against the active theme. This is the
// positive-and-negative pair that lifts reveal out of the parsed-and-warned
// state, the review event a graduation is.
//
// reveal rides the character-count axis — a growing prefix of the node's own
// text — and only a text node draws that axis (SCENES.md Scene 4, G-B). The
// signed scope is "honoured on text, refused elsewhere with an address", exactly
// parallel to scroll being honoured only on a marquee.

// A well-formed reveal on a text node validates — the positive half. Without it
// the refusal tests could pass while the validator refused every reveal, the
// failure the graduation is written to end.
func TestARevealOnATextNodeValidates(t *testing.T) {
	body := `{ "root": { "type": "text", "text": "hello", "reveal": { "anim": "reveal.fast" } } }`
	doc, err := ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: the document should parse; got %v", err)
	}
	if verr := doc.Validate(); verr != nil {
		t.Errorf("a well-formed reveal on a text node was refused: %v\n"+
			"consequence: G3 signed reveal's render semantics (SCENES.md Scene 4) and the engine\n"+
			"draws it on a text node, so a text node carrying { anim } must load.\n"+
			"remedy: validateReveal must accept a reveal on a text node.", verr)
	}
}

// A reveal on any node type other than text is refused, with an address and
// naming the field. A marquee is included on purpose: it owns the
// horizontal-offset axis (scroll), and composing a second motion on it is a
// fifth-axis question G-B does not sign, so a reveal there is a defect, not a
// combination. A nested position is included because that is the position no
// type-switch widening reaches.
func TestARevealOffATextNodeIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{
			name: "on a markdown node",
			body: `{ "root": { "type": "markdown", "bind": "chat.history", "reveal": { "anim": "reveal.fast" } } }`,
		},
		{
			name: "on a marquee node",
			body: `{ "root": { "type": "marquee", "bind": "thinking.text", "reveal": { "anim": "reveal.fast" } } }`,
		},
		{
			name: "nested in a stack",
			body: `{ "root": { "type": "stack", "children": [ { "type": "box", "border": "single", "reveal": { "anim": "reveal.fast" }, "children": [ { "type": "text", "text": "x" } ] } ] } }`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := ParseDocument([]byte(tc.body))
			if err != nil {
				t.Fatalf("premise broken: %v", err)
			}
			verr := doc.Validate()
			if verr == nil {
				t.Fatalf("reveal %s validated clean.\n"+
					"consequence: the node cannot draw the character-count axis reveal rides, so the\n"+
					"prop is silently ignored — the silent-drop outcome the format refuses everywhere.\n"+
					"remedy: validateReveal must refuse a reveal on any node type but text, with an\n"+
					"address.", tc.name)
			}
			var se *Error
			if !errors.As(verr, &se) {
				t.Fatalf("the refusal for reveal %s is not a *scene.Error, so it carries no address: %v", tc.name, verr)
			}
			if se.Loc == (Loc{}) {
				t.Errorf("the refusal for reveal %s carries no address: %v", tc.name, verr)
			}
			if !strings.Contains(verr.Error(), "reveal") {
				t.Errorf("the refusal for reveal %s does not name the field, so a reader cannot tell\n"+
					"which prop was rejected: %q", tc.name, verr.Error())
			}
		})
	}
}

// A reveal's timing token is checked against the theme's anim section, the same
// net a style token gets (TOKENS.md): a named token the theme does not define is
// a load error with an address, and an empty token resolves anim.default (Q8),
// which the factory theme ships. ValidateTokens, not doc.Validate(), because the
// token check needs the active theme — doc.Validate() is theme-agnostic.
func TestARevealTimingTokenIsCheckedAgainstTheTheme(t *testing.T) {
	thm := theme.SOBRIA()

	// A named token the theme defines, and the empty (→ default) case, both clean.
	for _, tc := range []struct {
		name string
		body string
	}{
		{"named token present", `{ "root": { "type": "text", "text": "x", "reveal": { "anim": "reveal.fast" } } }`},
		{"empty token → default", `{ "root": { "type": "text", "text": "x", "reveal": {} } }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := ParseDocument([]byte(tc.body))
			if err != nil {
				t.Fatalf("premise broken: %v", err)
			}
			if errs := ValidateTokens(doc, thm); len(errs) != 0 {
				t.Errorf("a reveal (%s) was flagged though the theme defines its token: %v\n"+
					"remedy: an empty anim resolves anim.default (Q8), and reveal.fast is in the factory\n"+
					"anim section; neither should be reported undefined.", tc.name, errs)
			}
		})
	}

	// An undefined token is a load error naming the token.
	body := `{ "root": { "type": "text", "text": "x", "reveal": { "anim": "no.such.token" } } }`
	doc, err := ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	errs := ValidateTokens(doc, thm)
	if len(errs) == 0 {
		t.Fatalf("a reveal naming an anim token the theme does not define validated clean.\n" +
			"consequence: the typewriter silently never animates and nothing says why — TOKENS.md\n" +
			"signs that a prop naming an absent timing token fails the load with an address.\n" +
			"remedy: collectTokenErrors must check the reveal's token against thm.HasAnim.")
	}
	if !strings.Contains(errs[0].Token, "no.such.token") {
		t.Errorf("the token error names %q, want the undefined token so the author can act", errs[0].Token)
	}
}
