package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// The placeholder for a node type the engine cannot draw must name the type.
//
// This is a diagnostic test, not a rendering one, and the distinction is the
// reason it exists as its own file. Every other test in this package asks
// "does the right thing appear on screen"; this one asks "when the wrong thing
// appears, does the screen say enough to fix it". The project has now paid
// four times for a layer that knew the answer and did not print it, and each
// time the finding was made by hand because no test asserted on the *content*
// of a failure message.
//
// The measurement that motivated it, taken on the tree with the whole suite
// green:
//
//	{"type": "button"}  -> parses, validates clean, draws [[UNKNOWN NODE TYPE]]
//	{"type": "buton"}   -> parses, validates clean, draws [[UNKNOWN NODE TYPE]]
//
// A documented primitive awaiting its phase and a two-letter transposition
// produced byte-identical output, and no other layer mentioned the type name
// either: Warnings() reports unknown *keys*, and `type` is a known key whose
// *value* nothing checks.
func TestUnknownNodeTypeNamesTheTypeItCouldNotDraw(t *testing.T) {
	render := func(t *testing.T, src string) string {
		t.Helper()
		doc, err := scene.ParseDocument([]byte(src))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		r := &Renderer{Width: 60, Height: 4}
		var b strings.Builder
		for _, line := range r.RenderFrame(doc, fold.State{}).Live {
			for _, span := range line {
				b.WriteString(span.Text)
			}
			b.WriteString("\n")
		}
		return b.String()
	}

	t.Run("the type name appears in the frame", func(t *testing.T) {
		// Four types, chosen to span the two populations the placeholder
		// must stop conflating: `button` and `sparkline` are documented in
		// SCENES.md and scheduled, `buton` and `sparklne` are what a typo
		// of each looks like.
		for _, typ := range []string{"button", "buton", "sparkline", "sparklne"} {
			got := render(t, `{"root":{"type":"stack","children":[{"id":"x","type":"`+typ+`"}]}}`)
			if !strings.Contains(got, typ) {
				t.Errorf("a node of type %q drew %q, which never names the type.\n"+
					"consequence: a typo and a documented-but-unbuilt primitive produce the same\n"+
					"pixel, so the author who misspelled a type is told exactly what the author who\n"+
					"used a correct one is told, and the six letters that would end the search are\n"+
					"printed nowhere.\n"+
					"remedy: include the offending type in the placeholder.", typ, strings.TrimSpace(got))
			}
		}
	})

	t.Run("two different unknown types do not draw the same frame", func(t *testing.T) {
		// The property the previous subtest implies but does not prove.
		// A placeholder could contain the type name and still be
		// degenerate — this asserts distinguishability directly, because
		// that is the thing that was actually broken.
		a := render(t, `{"root":{"type":"stack","children":[{"id":"x","type":"button"}]}}`)
		b := render(t, `{"root":{"type":"stack","children":[{"id":"x","type":"buton"}]}}`)
		if a == b {
			t.Errorf("a %q node and a %q node render byte-identically:\n%s\n"+
				"consequence: the frame cannot distinguish a documented primitive awaiting its\n"+
				"phase from a misspelling of it, which is the whole question LESSONS.md says\n"+
				"separates the two kinds of unknown construction.", "button", "buton", strings.TrimSpace(a))
		}
	})

	t.Run("a node with no type says so rather than quoting nothing", func(t *testing.T) {
		// `{"type": ""}` is a third population: not a future primitive and
		// not a misspelling, but a node whose type key never arrived. It
		// renders as `[[UNKNOWN NODE TYPE ""]]` under a naive %q, which
		// reads like a quoting bug in the engine and sends the author
		// looking in the wrong place.
		got := render(t, `{"root":{"type":"stack","children":[{"id":"x"}]}}`)
		if strings.Contains(got, `""`) {
			t.Errorf("a node with no type drew %q; the empty quotes read as an engine quoting bug\n"+
				"rather than as a missing key, which is what actually happened.", strings.TrimSpace(got))
		}
		if !strings.Contains(got, "type") {
			t.Errorf("a node with no type drew %q, which does not say that the \"type\" key is\n"+
				"the thing that is missing.", strings.TrimSpace(got))
		}
	})

	t.Run("the substring three goldens assert the absence of is preserved", func(t *testing.T) {
		// render_test.go, sobria_test.go, maximum_test.go and the boot loop
		// in cmd/arxi-tui all assert `!strings.Contains(out, "UNKNOWN NODE
		// TYPE")`. That is four guards keyed to a literal, and renaming the
		// placeholder switches all four off in silence — they would keep
		// passing while checking nothing. This test is the one that notices.
		got := render(t, `{"root":{"type":"stack","children":[{"id":"x","type":"buton"}]}}`)
		if !strings.Contains(got, "UNKNOWN NODE TYPE") {
			t.Errorf("the placeholder no longer contains %q (it drew %q).\n"+
				"consequence: four existing guards assert the absence of that exact substring;\n"+
				"none of them fails when the string stops existing, so all four silently stop\n"+
				"checking that the golden scenes render every node they declare.\n"+
				"remedy: keep the substring, or update all four call sites in the same commit.",
				"UNKNOWN NODE TYPE", strings.TrimSpace(got))
		}
	})
}
