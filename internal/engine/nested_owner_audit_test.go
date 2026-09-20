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

	withSrc, ok := nestedOwnerOfType(owner, branch, shape, marker)
	if !ok {
		t.Fatalf("premise broken: this audit cannot build a probe for shape %q (form %s), so the\n"+
			"probe would be byte-identical to the control and every assertion below would\n"+
			"compare a frame against itself — a subtest that passes having measured nothing.\n"+
			"remedy: teach nestedOwnerOfType to write the %q shape.", shape, form, shape)
	}
	withoutSrc, ok := nestedOwnerOfType(owner, branch, "", marker)
	if !ok {
		t.Fatalf("premise broken: the %s control on %q could not be built", form, owner)
	}

	withDoc, err := scene.ParseDocument([]byte(withSrc))
	if err != nil {
		t.Fatalf("premise broken: the %s probe on %q must parse; got %v\nsrc: %s", form, owner, err, withSrc)
	}
	withoutDoc, err := scene.ParseDocument([]byte(withoutSrc))
	if err != nil {
		t.Fatalf("premise broken: the %s control on %q must parse; got %v", form, owner, err)
	}

	// The fragment must survive parsing, or the probe is the control and a
	// reported "drop" is the harness measuring itself.
	if !nestedFragmentReachedNode(withDoc.Root, branch) {
		t.Fatalf("premise broken: the %s probe on %q parsed, but %q did not reach the node.\n"+
			"consequence: the probe is then identical to the control, so this subtest would\n"+
			"report a silent drop for every owner — an accusation against the engine that\n"+
			"measures only the harness.\nsrc: %s", form, owner, branch, withSrc)
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
	//
	// The warning is looked for in a document that *also* carries an
	// unrelated misspelling, and that confounder is deliberate. Without it
	// the probe corpus is clean, the only warning quoting the branch is the
	// drop itself, and a matcher that searched the message prose would pass
	// for a reason that has nothing to do with being right — measured:
	// exactly that, latently, until this line. A guard should be exercised
	// against the case that can fool it, or its correctness lives in the
	// corpus instead of in the check.
	if !shouldDraw {
		confounded, ok := nestedOwnerOfType(owner, branch, shape, marker, withUnrelatedTypo())
		if !ok {
			t.Fatalf("premise broken: the confounded %s probe on %q could not be built", form, owner)
		}
		doc, err := scene.ParseDocument([]byte(confounded))
		if err != nil {
			t.Fatalf("premise broken: the confounded %s probe on %q must parse; got %v\nsrc: %s",
				form, owner, err, confounded)
		}
		if verr := doc.Validate(); verr != nil {
			t.Fatalf("premise broken: the confounded %s probe on %q must validate clean; got %v",
				form, owner, verr)
		}
		assertNestedDropWarns(t, doc, owner, branch, form)
	}
}

// assertNestedDropWarns is the other half of the claim. Measuring that the
// fragment does not draw only establishes the drop; what makes it survivable
// is that the author is told, at an address.
//
// It matches on Warning.Form, not on the message text. It used to ask
// strings.Contains(w.Msg, branch), and that is satisfiable by a warning it did
// not cause: the generic unknown-key message quotes `a misspelled "children"
// silently drops the whole subtree` in its own explanatory prose, so a
// document with an unrelated typo answers yes to "was the drop reported?".
//
// Measured honestly, because the distinction is the interesting part: in this
// audit's own probes that false positive was **latent, not active**. The
// probes are otherwise-clean documents, so the only warning quoting the branch
// really was the drop, and reverting the matcher alone fails nothing. What
// made the old matcher wrong was not a wrong answer today but where its
// correctness lived — in the probe corpus rather than in the check. Any probe
// that later grows a typo, or any rewording of the generic message, moves the
// answer without touching this function. A guard whose correctness is a
// property of its inputs is one input away from certifying the thing it exists
// to catch, and that is the same shape as the permanently-skipping test
// AGENTS.md records: it passes for a reason unrelated to the claim.
func assertNestedDropWarns(t *testing.T, doc *scene.Document, owner, branch, form string) {
	t.Helper()

	for _, w := range doc.Warnings() {
		if w.Form != form {
			continue
		}
		if w.Loc == (scene.Loc{}) {
			t.Errorf("the warning about %s on %q carries no address, so the author cannot find the\n"+
				"node that declared it (PLAN.md invariant 4): %q", form, owner, w.Msg)
		}
		return
	}

	t.Errorf("node type %q declares %s, the renderer never draws it, and the document loads with\n"+
		"no warning identifying itself as %q.\n\n"+
		"consequence: the silent drop, at the axis the nested sweep holds fixed. That sweep\n"+
		"varies the property and the branch with the owner hardcoded to marquee, so this\n"+
		"pairing is outside everything it can see. Measured before this audit existed:\n"+
		"an input with a node-shaped prefix, a marquee with a string-shaped prefix, a\n"+
		"text with either, and a child array under eleven of the fifteen signed types —\n"+
		"every one validated clean and drew nothing.\n"+
		"remedy: read the shape where the owner composes its spans, or keep it in\n"+
		"scene.nestedFormReaders' complement so warnDroppedNestedForm reports it.",
		owner, form, branch)
}

// TestTheDropWarningIsFoundByIdentityNotByProse pins the fix above from the
// side that matters: that the search assertNestedDropWarns performs cannot be
// satisfied by a warning it did not cause.
//
// This is a guard on a guard, and it is here because the bug it describes was
// live and green. assertNestedDropWarns asked strings.Contains(w.Msg, branch);
// the generic unknown-key warning's own prose contains `a misspelled
// "children" silently drops the whole subtree`, so a document warning only
// about a typo answered "yes, the drop was reported". The audit would then
// certify that authors are told about a silent drop on the strength of an
// unrelated finding — the guard switching itself off, which AGENTS.md records
// as the failure mode a numerator over the *passing* state always has.
//
// The document below is the exact shape of that false positive: a node whose
// children are composed (a stack, so nothing is dropped and no Form warning is
// produced) carrying a misspelled key (so the prose warning appears). Under
// the old matcher the branch substring is present; under the new one no
// warning claims the form, which is the truth.
func TestTheDropWarningIsFoundByIdentityNotByProse(t *testing.T) {
	const src = `{"root":{"type":"stack","txet":"x","children":[{"type":"text","text":"base"}]}}`

	doc, err := scene.ParseDocument([]byte(src))
	if err != nil {
		t.Fatalf("premise broken: this probe must parse; got %v", err)
	}
	if verr := doc.Validate(); verr != nil {
		t.Fatalf("premise broken: this probe must validate clean, or the warning list below is\n"+
			"not the one an author would see; got %v", verr)
	}

	warnings := doc.Warnings()

	// The premise: the prose warning is present, and it does quote the
	// branch name. Without this the test passes for the wrong reason — an
	// empty warning list satisfies both assertions below while measuring
	// nothing, which is the vacuous-guard shape this package keeps closing.
	prose := false
	for _, w := range warnings {
		if w.Form == "" && strings.Contains(w.Msg, "children") {
			prose = true
		}
	}
	if !prose {
		t.Fatalf("premise broken: no unrelated warning quotes %q, so this probe no longer\n"+
			"reproduces the false positive it exists to pin. That is not a pass — it means the\n"+
			"generic message was reworded, and the next guard to grep a sentence will be\n"+
			"written believing prose is a safe thing to match.\nwarnings: %v", "children", warnings)
	}

	// The claim: no warning here identifies itself as the dropped-children
	// form, because nothing was dropped.
	for _, w := range warnings {
		if w.Form == "children.array" {
			t.Errorf("a warning claims to be the dropped-children finding on a document whose children\n"+
				"are composed by their owner.\n\n"+
				"consequence: the false-alarm direction — the author is told a subtree was ignored\n"+
				"when the engine drew it, and borderVocabulary's comment records what that costs.\n"+
				"remedy: only warnDroppedNestedForm may set Form.\nwarning: %q", w.Msg)
		}
	}
}

// nestedOwnerOfType builds a document whose root is the named node type
// carrying a nested branch in the named shape. shape == "" omits the fragment
// entirely, which is the control.
//
// The owner is given enough of its own content to draw: a node that renders
// nothing at all would make every comparison here vacuous, which is the
// failure mode this package keeps closing. The binds are the ones
// nestedStyleState populates.
//
// The shape switch has no default that emits nothing, and that is deliberate.
// It used to: a shape it did not recognise produced a probe byte-identical to
// the control, so the frames matched, `drew` came out false, and every subtest
// expecting a drop passed — having measured nothing at all. That is the trap
// AGENTS.md records as "a test's own probes are code, and untested code", and
// it was live the moment `children.array` joined the inventory. It now returns
// ok=false, and the caller fails rather than guessing.
// probeOption varies one aspect of a probe document. It exists so the
// confounded variant is built by the same function as the plain one: two
// builders would be two inventories of what a probe looks like, and this
// package has watched that shape drift repeatedly.
type probeOption func(*strings.Builder)

// withUnrelatedTypo adds a misspelled key to the owner, which makes the parser
// emit its generic unknown-key warning. That message quotes the word
// "children" inside its own advice, so a document carrying it is exactly the
// case that fools a matcher searching the warning prose.
func withUnrelatedTypo() probeOption {
	return func(b *strings.Builder) {
		b.WriteString(`,"zzunknownkey":"x"`)
	}
}

func nestedOwnerOfType(owner, branch, shape, marker string, opts ...probeOption) (string, bool) {
	var b strings.Builder
	b.WriteString(`{"root":{"type":"`)
	b.WriteString(owner)
	b.WriteString(`"`)
	for _, opt := range opts {
		opt(&b)
	}

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
	case "":
		// The control: the fragment is omitted on purpose.
	case "node":
		b.WriteString(`,"` + branch + `":{"type":"text","text":"` + marker + `"}`)
	case "string":
		b.WriteString(`,"` + branch + `":"` + marker + `"`)
	case "array":
		b.WriteString(`,"` + branch + `":[{"type":"text","text":"` + marker + `"}]`)
	default:
		return "", false
	}

	b.WriteString(`}}`)
	return b.String(), true
}

// nestedFragmentReachedNode reports whether the parsed node actually carries
// the branch the probe wrote.
//
// This is the second half of the same lesson. A probe can be well-formed JSON,
// parse without error, and still lose its fragment — `encoding/json` discards
// a key the struct does not declare, and a shape that does not match the
// field's type fails into the zero value. Either produces a node identical to
// the control, which this audit would report as an engine silent drop: a false
// accusation in the flattering direction, because it looks like coverage.
func nestedFragmentReachedNode(n *scene.Node, branch string) bool {
	switch branch {
	case "prefix":
		return len(n.PrefixRaw) > 0
	case "suffix":
		return n.Suffix != nil
	case "children":
		return len(n.Children) > 0
	default:
		return false
	}
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
