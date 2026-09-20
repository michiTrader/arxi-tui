package engine

import (
	"sort"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// The audit for the axis the nested sweep holds fixed: which *owner* a nested
// prefix or suffix is written on.
//
// # Why this is a different question again
//
// TestEveryUniversalPropertyIsHonouredOrRefusedWhenNested asks the right
// question about nested nodes and asks it well, across every universal in
// SCENES.md and both live branches. Every one of its probes is built by
// nestedOwner, which hardcodes `"type":"marquee"`. It varies the property and
// the branch with the owner fixed, so an owner that ignores a whole branch is
// outside everything it can see.
//
// That is the fourth appearance of one defect, and the progression is the
// entire point of writing it down:
//
//  1. `when` honoured per *container* — the same node hid under a row and drew
//     under a stack.
//  2. `when`, then a style token, honoured per *node type* — closed by moving
//     the work into renderNode, which withFocusGlow documents as making "some
//     node types obey and others do not" unrepresentable. True, and quantified
//     over node types.
//  3. a nested *position* — a marquee prefix ignored `when` entirely, because
//     renderMarquee reads prefix.Bind/.Text/.Style out of the struct and
//     nothing nested is dispatched. No widening of a type switch reaches it.
//     Closed by asking hiddenByWhen inside renderMarquee.
//  4. this: a nested *shape* on an owner with no reader for it. `prefix` is
//     polymorphic — string or node — and each owner reads exactly one form,
//     renderInput via PrefixText and renderMarquee via PrefixNode. Handing
//     either owner the other shape lands on a reader that does not exist, and
//     the validator walks the fragment in full either way.
//
// Each fix was correct and each was quantified over one axis. The lesson this
// file encodes is the one that keeps coming back: name the quantifier, then
// ask what the other axes are.
//
// # What it measures, and in both directions
//
// scene.nestedFormReaders is a written inventory — the fact it records is which
// *shape* an owner decodes, which is not a Go declaration to reflect over. This
// package has watched five hand-written inventories drift, so this one is not
// trusted: the audit renders both shapes of both branches under every signed
// node type and compares the frame against a control that omits the fragment.
//
//   - a pairing the map calls silent that in fact *draws* fails, because the
//     map is then warning about a construction the renderer honours — the
//     false-alarm direction R19h measured, and the direction that gets a guard
//     switched off.
//   - a pairing the map omits that in fact *drops* fails, because that is the
//     defect itself, unreported.
//
// So the map cannot drift from the renderer without this failing, and the
// warning cannot outlive the gap it describes: teach renderText to compose a
// prefix and this test demands the entry be removed.
func TestEveryNestedShapeIsDrawnOrWarnedOnEveryOwner(t *testing.T) {
	readers := scene.NestedFormReadersForAudit()

	// The floor. If the inventory is emptied, or the accessor renamed and
	// this left reading nothing, every subtest below would still run and
	// every one would expect a drop — turning the audit into a demand that
	// nothing works. Three forms are what scene declares today.
	if len(readers) < 3 {
		t.Fatalf("read only %d nested form(s) from scene; expected at least 3\n"+
			"consequence: with an empty inventory this audit expects every owner to drop every\n"+
			"shape, so it would pass on a renderer that draws nothing and fail on the one that\n"+
			"works — a checker inverted rather than merely silent.\n"+
			"remedy: confirm scene.NestedFormReadersForAudit still returns the prefix/suffix\n"+
			"inventory.", len(readers))
	}

	types := scene.SignedNodeTypes()
	if len(types) < 15 {
		t.Fatalf("read only %d signed node type(s); expected at least 15, so this audit would\n"+
			"sweep almost no owners and report success having asked nothing", len(types))
	}

	for _, form := range sortedFormNames(readers) {
		form := form
		branch, shape := splitNestedForm(t, form)

		t.Run(form, func(t *testing.T) {
			for _, owner := range types {
				owner := owner
				t.Run(owner, func(t *testing.T) {
					_, shouldDraw := readers[form][owner]
					assertNestedShapeOnOwner(t, owner, branch, shape, form, shouldDraw)
				})
			}
		})
	}
}

// assertNestedShapeOnOwner renders one owner/shape pairing against a control
// that omits the fragment, and holds it to whichever outcome the inventory
// claims.
func assertNestedShapeOnOwner(t *testing.T, owner, branch, shape, form string, shouldDraw bool) {
	t.Helper()

	const marker = "ZZNESTED"

	withSrc := nestedOwnerOfType(owner, branch, shape, marker)
	withoutSrc := nestedOwnerOfType(owner, branch, "", marker)

	withDoc, err := scene.ParseDocument([]byte(withSrc))
	if err != nil {
		t.Fatalf("premise broken: the %s probe on %q must parse; got %v\nsrc: %s", form, owner, err, withSrc)
	}
	withoutDoc, err := scene.ParseDocument([]byte(withoutSrc))
	if err != nil {
		t.Fatalf("premise broken: the %s control on %q must parse; got %v", form, owner, err)
	}

	// A refused document never reaches a frame, so the comparison below
	// would be between two empty strings and would agree with anything.
	// Refusal is a legitimate outcome — it is what row_template gets — so
	// this reports rather than fails, but it must not be mistaken for a
	// measured drop.
	if verr := withDoc.Validate(); verr != nil {
		t.Skipf("%s on %q is refused by the validator, so the author already gets an address\n"+
			"and there is no silent drop to measure: %v", form, owner, verr)
	}
	if verr := withoutDoc.Validate(); verr != nil {
		t.Fatalf("premise broken: the %s control on %q must validate clean, or a difference\n"+
			"below cannot be attributed to the fragment; got %v", form, owner, verr)
	}

	r := &Renderer{Width: 80, Height: 24}
	state := nestedStyleState()

	got := frameSignature(r.RenderFrame(withDoc, state))
	base := frameSignature(r.RenderFrame(withoutDoc, state))

	drew := got != base && strings.Contains(got, marker)

	if shouldDraw && !drew {
		t.Errorf("scene.nestedFormReaders says node type %q reads %s, and a document that sets it\n"+
			"renders without the fragment's text.\n\n"+
			"consequence: the inventory is describing a reader that is not there, so the warning\n"+
			"path stays quiet for a construction the engine in fact drops — the exact silence\n"+
			"this inventory exists to end.\n"+
			"remedy: either restore the read in the renderer, or remove %q from the entry so the\n"+
			"author is warned instead.\n\n"+
			"frame with fragment: %q\nframe without:       %q",
			owner, form, owner, strings.TrimSpace(got), strings.TrimSpace(base))
		return
	}

	if !shouldDraw && drew {
		t.Errorf("scene.nestedFormReaders does not list node type %q as reading %s, so a document\n"+
			"pairing them is warned about — and the renderer draws it anyway.\n\n"+
			"consequence: the false-alarm direction. The document is correct, the engine honours\n"+
			"it, and the author is told the span was ignored. borderVocabulary's comment records\n"+
			"what this costs: a guard that fires on working constructions is a guard people learn\n"+
			"to ignore, long before it finds a real defect.\n"+
			"remedy: add %q to the %q entry in scene.nestedFormReaders, with the accessor that\n"+
			"reads it.\n\n"+
			"frame with fragment: %q\nframe without:       %q",
			owner, form, owner, form, strings.TrimSpace(got), strings.TrimSpace(base))
		return
	}

	// A dropped pairing must warn, with an address. Silence here is the
	// defect itself; a warning with no Loc is a finding the author cannot
	// act on (PLAN.md invariant 4).
	if !shouldDraw {
		assertNestedDropWarns(t, withDoc, owner, branch, form)
	}
}

// assertNestedDropWarns is the other half of the claim. Measuring that the
// fragment does not draw only establishes the drop; what makes it survivable
// is that the author is told, at an address.
func assertNestedDropWarns(t *testing.T, doc *scene.Document, owner, branch, form string) {
	t.Helper()

	for _, w := range doc.Warnings() {
		if !strings.Contains(w.Msg, branch) {
			continue
		}
		if w.Loc == (scene.Loc{}) {
			t.Errorf("the warning about %s on %q carries no address, so the author cannot find the\n"+
				"node that declared it (PLAN.md invariant 4): %q", form, owner, w.Msg)
		}
		return
	}

	t.Errorf("node type %q declares %s, the renderer never draws it, and the document loads with\n"+
		"no warning naming %q.\n\n"+
		"consequence: the silent drop, at the axis the nested sweep holds fixed. That sweep\n"+
		"varies the property and the branch with the owner hardcoded to marquee, so this\n"+
		"pairing is outside everything it can see. Measured before this audit existed:\n"+
		"an input with a node-shaped prefix, a marquee with a string-shaped prefix, and a\n"+
		"text with either — all four validated clean and drew nothing.\n"+
		"remedy: read the shape where the owner composes its spans, or keep it in\n"+
		"scene.nestedFormReaders' complement so warnDroppedNestedForm reports it.",
		owner, form, branch)
}

// nestedOwnerOfType builds a document whose root is the named node type
// carrying a prefix or suffix in the named shape. shape == "" omits the
// fragment entirely, which is the control.
//
// The owner is given enough of its own content to draw: a node that renders
// nothing at all would make every comparison here vacuous, which is the
// failure mode this package keeps closing. The binds are the ones
// nestedStyleState populates.
func nestedOwnerOfType(owner, branch, shape, marker string) string {
	var b strings.Builder
	b.WriteString(`{"root":{"type":"`)
	b.WriteString(owner)
	b.WriteString(`"`)

	// Content, so the owner has something to compose the fragment beside.
	switch owner {
	case "marquee":
		b.WriteString(`,"bind":"thinking.text"`)
	case "input":
		b.WriteString(`,"bind":"user.input"`)
	case "list":
		b.WriteString(`,"bind":"tasks.items"`)
	default:
		b.WriteString(`,"text":"OWNERBODY"`)
	}

	switch shape {
	case "node":
		b.WriteString(`,"` + branch + `":{"type":"text","text":"` + marker + `"}`)
	case "string":
		b.WriteString(`,"` + branch + `":"` + marker + `"`)
	}

	b.WriteString(`}}`)
	return b.String()
}

// splitNestedForm turns "prefix.node" into ("prefix", "node").
//
// It fails rather than guessing, because a form name this cannot parse would
// otherwise silently become branch="prefix.node" and shape="", i.e. a control
// compared against itself — a subtest that passes by asking nothing.
func splitNestedForm(t *testing.T, form string) (branch, shape string) {
	t.Helper()
	parts := strings.Split(form, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		t.Fatalf("premise broken: nested form %q is not <branch>.<shape>, so this audit cannot\n"+
			"build a probe for it and would compare a control against itself", form)
	}
	return parts[0], parts[1]
}

func sortedFormNames(readers map[string]map[string]string) []string {
	out := make([]string, 0, len(readers))
	for form := range readers {
		out = append(out, form)
	}
	// Sorted so a failure names the same form every run; an address that
	// moves between runs is not an address.
	sort.Strings(out)
	return out
}

// Compile-time note: this file uses fold.State only through nestedStyleState,
// which lives in nested_node_style_test.go. The import is named here so a
// reader looking for the state's origin finds it.
var _ = func(s fold.State) {}
