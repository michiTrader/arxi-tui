package scene

import (
	"strings"
	"testing"
)

// An unsigned `type` value must warn, and a signed one must not.
//
// Every other warning this package emits is about a *key* the parser did not
// recognise, and that is precisely how the defect survived: `type` is a
// perfectly well-known key, so the whole warning machinery walked past
// `{"type": "buton"}` without a word. The renderer's placeholder was the only
// other diagnostic, and before the engine commit it said the same thing for
// `button`.
//
// Measured before the fix, on the tree with the suite green:
//
//	{"type": "button"} -> parses, validates clean, 0 warnings, placeholder
//	{"type": "buton"}  -> parses, validates clean, 0 warnings, placeholder
func TestUnsignedNodeTypeWarnsAndSignedOneDoesNot(t *testing.T) {
	warningsFor := func(t *testing.T, typ string) []Warning {
		t.Helper()
		src := `{"root":{"type":"stack","children":[{"id":"x","type":"` + typ + `"}]}}`
		doc, err := ParseDocument([]byte(src))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		return doc.Warnings()
	}

	t.Run("a signed but unbuilt type is silent", func(t *testing.T) {
		// This is the half that makes the warning worth having. `button`,
		// `switch`, `slider` and `sparkline` are settled primitives whose
		// renderers are later phases; PLAN.md's forward-compatibility rule
		// says a document using them keeps booting, and the placeholder is
		// the honest report. Warning about them too would put the scheduled
		// gap and the typo back into one bucket — the defect, relocated.
		for _, typ := range []string{"button", "switch", "slider", "sparkline"} {
			if w := warningsFor(t, typ); len(w) != 0 {
				t.Errorf("node type %q is signed in the v0 vocabulary and warned anyway: %v\n"+
					"consequence: the author of a correct document is told something is wrong,\n"+
					"which is the false-alarm direction — the one that gets a guard switched off.",
					typ, w)
			}
		}
	})

	t.Run("an unsigned type warns with an address", func(t *testing.T) {
		for _, typ := range []string{"buton", "sparklne", "wibble"} {
			w := warningsFor(t, typ)
			if len(w) == 0 {
				t.Errorf("node type %q is in no signed vocabulary and produced no warning;\n"+
					"the document reports success and the screen is wrong, which is the silent\n"+
					"drop this package has now paid for five times.", typ)
				continue
			}
			if !strings.Contains(w[0].Msg, typ) {
				t.Errorf("the warning for %q does not name it: %q", typ, w[0].Msg)
			}
			// An address is the difference between a finding and a hunt.
			// Phase 2's repair loop reads these messages, and a warning
			// with no file:line costs the model a turn the corpus then
			// charges to the model rather than to the message.
			if w[0].Loc.Line == 0 {
				t.Errorf("the warning for %q carries no line: %q", typ, w[0].String())
			}
		}
	})

	t.Run("a near miss names the type the author probably meant", func(t *testing.T) {
		// The whole point of the change. The six letters the author is
		// hunting for are known to this layer, and printing them is what
		// the previous instances of this defect class failed to do.
		for typ, want := range map[string]string{
			"buton":     "button",
			"sparklne":  "sparkline",
			"stak":      "stack",
			"markdwon":  "markdown", // the transposition class that cost 44 references
			"overlya":   "overlay",
			"swtich":    "switch",
			"inupt":     "input",
			"marqee":    "marquee",
			"spinnner":  "spinner",
			"sldier":    "slider",
			"rlue":      "rule",
			"makrdown":  "markdown",
			"contaienr": "",
		} {
			w := warningsFor(t, typ)
			if len(w) == 0 {
				t.Fatalf("%q produced no warning at all", typ)
			}
			got := w[0].Msg
			if want == "" {
				// Nothing close enough: the message must not guess. A
				// wrong suggestion sends the author to edit a line that
				// was correct, which is worse than the silence it
				// replaced.
				if strings.Contains(got, "did you mean") {
					t.Errorf("%q has no near neighbour in the vocabulary and the warning guessed\n"+
						"anyway: %q", typ, got)
				}
				continue
			}
			if !strings.Contains(got, `did you mean "`+want+`"`) {
				t.Errorf("a node of type %q should be told it probably meant %q; it got: %q",
					typ, want, got)
			}
		}
	})

	t.Run("a node with no type is described as missing one", func(t *testing.T) {
		doc, err := ParseDocument([]byte(`{"root":{"type":"stack","children":[{"id":"x"}]}}`))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		w := doc.Warnings()
		if len(w) == 0 {
			t.Fatal("a node with no \"type\" produced no warning; it cannot draw, and nothing said so")
		}
		if !strings.Contains(w[0].Msg, "no \"type\"") {
			t.Errorf("a typeless node should be told the key is missing, not that its type is\n"+
				"unsigned; it got: %q", w[0].Msg)
		}
	})

	t.Run("prefix and suffix are exempt because nothing dispatches on them", func(t *testing.T) {
		// The defect this work produced against itself, kept as a test
		// because the guard that caught it lives in another file and
		// tests a different thing (the shipped scenes), so nothing here
		// would notice if the exemption were removed.
		//
		// `prefix` and `suffix` are Nodes structurally, but renderMarquee
		// reads their fields directly and never calls renderNode on them —
		// so they legitimately carry no type, and both shipped factory
		// scenes omit it. Warning there fired on SOBRIA and MAXIMUM, which
		// is a check that fails on the product's own documents: noise
		// people learn to scroll past long before it finds anything real.
		src := `{"root":{"type":"stack","children":[
			{"id":"t","type":"marquee","bind":"thinking.text",
			 "prefix":{"text":"• ","style":{"style":"dim"}},
			 "suffix":{"bind":"usage.delta","style":{"style":"dim"}}}]}}`
		doc, err := ParseDocument([]byte(src))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if w := doc.Warnings(); len(w) != 0 {
			t.Errorf("an untyped prefix/suffix warned: %v\n"+
				"consequence: the two scenes this project ships trip their own vocabulary check.",
				w)
		}
	})
}
