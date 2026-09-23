package scene

import (
	"errors"
	"strings"
	"testing"
)

// The graduation guard for scroll (G2): the marquee draws it, and every other
// node type is refused with an address. This is the positive-and-negative pair
// that lifts the old blanket unrenderedFields refusal — a review event, per the
// rule that a graduation rewrites the guards that encoded the refused design
// rather than slamming through them.
//
// scroll rides the horizontal-offset axis, and only a marquee draws that axis
// (SCENES.md Scene 4, G-B). The signed scope decision is therefore "honoured on
// marquee, refused elsewhere with an address", exactly parallel to a row.* bind
// being legal only inside a template: a prop on the wrong node type is a scene
// defect the author is told about, not a silent no-op.

// A well-formed scroll on a marquee validates — the positive half. Without this
// the refusal tests could all pass while the validator refused every scroll,
// which is the failure the D1 row_template graduation was written to end.
func TestAScrollOnAMarqueeValidates(t *testing.T) {
	body := `{ "root": { "type": "marquee", "bind": "thinking.text",
	  "scroll": { "speed": 2, "pause_when": "agent.working" } } }`
	doc, err := ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: the document should parse; got %v", err)
	}
	if verr := doc.Validate(); verr != nil {
		t.Errorf("a well-formed scroll on a marquee was refused: %v\n"+
			"consequence: G2 signed scroll's render semantics (SCENES.md Scene 4) and the engine\n"+
			"draws it on a marquee, so a marquee carrying { speed, pause_when } must load.\n"+
			"remedy: validateScroll must accept a positive speed and a signed pause_when on a\n"+
			"marquee node.", verr)
	}
}

// A scroll on any node type other than marquee is refused, with an address and
// naming the field — the same net a row.* bind outside a template gets. The
// cases include a nested position (prefix/suffix of a marquee) because that is
// the position no type-switch widening reaches, and it is how the shipped
// scenes nest.
func TestAScrollOffAMarqueeIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{
			name: "on a text node",
			body: `{ "root": { "type": "text", "text": "x",
			  "scroll": { "speed": 2 } } }`,
		},
		{
			name: "on a markdown node",
			body: `{ "root": { "type": "markdown", "bind": "chat.history",
			  "scroll": { "speed": 2 } } }`,
		},
		{
			name: "nested in a marquee prefix",
			body: `{ "root": { "type": "marquee", "bind": "thinking.text",
			  "prefix": { "type": "text", "text": "~", "scroll": { "speed": 2 } } } }`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := ParseDocument([]byte(tc.body))
			if err != nil {
				t.Fatalf("premise broken: %v", err)
			}
			verr := doc.Validate()
			if verr == nil {
				t.Fatalf("scroll %s validated clean.\n"+
					"consequence: the node cannot draw the horizontal-offset axis scroll rides, so the\n"+
					"prop is silently ignored — the silent-drop outcome the format refuses everywhere.\n"+
					"remedy: validateScroll must refuse a scroll on any node type but marquee, with an\n"+
					"address.", tc.name)
			}
			var se *Error
			if !errors.As(verr, &se) {
				t.Fatalf("the refusal for scroll %s is not a *scene.Error, so it carries no address: %v", tc.name, verr)
			}
			if se.Loc == (Loc{}) {
				t.Errorf("the refusal for scroll %s carries no address: %v", tc.name, verr)
			}
			if !strings.Contains(verr.Error(), "scroll") {
				t.Errorf("the refusal for scroll %s does not name the field, so a reader cannot tell\n"+
					"which prop was rejected: %q", tc.name, verr.Error())
			}
		})
	}
}

// speed is refused when non-positive rather than clamped: a zero or negative
// speed marquee ticks forever without advancing, which is a defect worth naming
// (pause_when is the way to hold a marquee, not speed 0). Refused for the
// marquee itself, so the node-type check does not mask the speed check.
func TestAScrollWithNonPositiveSpeedIsRefused(t *testing.T) {
	for _, speed := range []string{"0", "-1"} {
		t.Run("speed_"+speed, func(t *testing.T) {
			body := `{ "root": { "type": "marquee", "bind": "thinking.text",
			  "scroll": { "speed": ` + speed + ` } } }`
			doc, err := ParseDocument([]byte(body))
			if err != nil {
				t.Fatalf("premise broken: %v", err)
			}
			verr := doc.Validate()
			if verr == nil {
				t.Fatalf("a marquee with scroll speed %s validated clean.\n"+
					"consequence: the marquee ticks forever without moving, burning the animation clock\n"+
					"to draw a frame that never changes — a defect the author is not told about.\n"+
					"remedy: validateScroll must refuse a non-positive speed with an address.", speed)
			}
			if !strings.Contains(verr.Error(), "scroll") || !strings.Contains(verr.Error(), "speed") {
				t.Errorf("the refusal for speed %s must name scroll and speed so the author can act:\n"+
					"%q", speed, verr.Error())
			}
		})
	}
}

// pause_when is a bind, and a misspelled one must be refused here rather than
// silently never pausing. The signed direction must keep validating, or the
// property is unusable; the unsigned direction must be refused, or a typo ships.
func TestAScrollPauseWhenMustBeASignedBind(t *testing.T) {
	signed := `{ "root": { "type": "marquee", "bind": "thinking.text",
	  "scroll": { "speed": 2, "pause_when": "agent.working" } } }`
	doc, err := ParseDocument([]byte(signed))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	if verr := doc.Validate(); verr != nil {
		t.Errorf("a scroll with a signed pause_when bind was refused: %v\n"+
			"remedy: validateScroll must accept a pause_when that appears in BINDS.md §4.5.", verr)
	}

	unsigned := `{ "root": { "type": "marquee", "bind": "thinking.text",
	  "scroll": { "speed": 2, "pause_when": "totally.invented" } } }`
	doc, err = ParseDocument([]byte(unsigned))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	verr := doc.Validate()
	if verr == nil {
		t.Fatalf("a scroll whose pause_when names an unsigned bind validated clean.\n" +
			"consequence: the marquee never pauses and nothing says why; a misspelled pause bind\n" +
			"is the silent drop this package refuses everywhere else.\n" +
			"remedy: validateScroll must check pause_when as a bind, the same net a `when` gets.")
	}
	if !strings.Contains(verr.Error(), "totally.invented") {
		t.Errorf("the refusal must name the unsigned bind so the author can find the typo: %q", verr.Error())
	}
}
