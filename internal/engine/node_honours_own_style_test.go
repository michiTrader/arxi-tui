package engine

import (
	"sort"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

// The other direction of the style axis: a node's token on its own content.
//
// The container guard next door asks whether a parent preserves the token its
// *child* declared. It holds the child fixed at `text` — the one type nobody
// doubted — and sweeps the containers. That leaves the mirror-image question
// unasked: whether a node honours the token it declared for the content it
// draws *itself*. A container cannot discard a declaration that the leaf never
// applied in the first place, so a leaf that ignores its own `style` is
// invisible to every guard in this package, including that one.
//
// # Why this is the same class, and the fifth instance of it
//
// The repeated shape in this tree is a construction the validator accepts and
// the engine does not draw: the style key that validated under one spelling
// and rendered under another, the border token that was refused when wrong and
// ignored when right, `row_template` checked in five places and read in zero,
// and nine signed binds computed by the fold and projected nowhere. Each was
// silent in the same specific way — passing validation is exactly the signal
// that says the document is fine.
//
// `style` is signed as a **universal** property in SCENES.md's vocabulary
// ("Universal: `id`, `bind` …, `when`, `style`, `grow`/`weight` …"), not as a
// property of `text`. `collectTokenErrors` agrees and is type-agnostic: it
// reads `n.Style` on every node it walks and refuses an undefined token
// wherever it appears. So the format and the validator both say every node may
// name a token, and four content types drew their own content while discarding
// it.
//
// # What was measured before anything was written
//
// Each node type in renderNode's switch, rendered childless so only its own
// content is in the frame, declaring a token defined in a purpose-built theme
// and named so that no renderer could mint it by accident (the first draft of
// this probe used `dim`, and `list` reported a pass for a token it hardcodes —
// the measurement was wrong before the guard was):
//
//	type      draws own content   honours its own token
//	input     yes                 no    <-- here
//	list      yes                 no    <-- here
//	markdown  yes                 no    <-- here
//	rule      yes                 no    <-- here
//	marquee   yes                 yes
//	spinner   yes                 yes
//	text      yes                 yes
//	box       only its frame      yes (border glyphs, via borderStyleName)
//	overlay   no                  n/a
//	row       no                  n/a
//	stack     no                  n/a
//
// Three of seven content types honoured the declaration. As with the bordered
// box, a rule kept in some places and not others is not a rule, and which
// places is found by enumeration or not at all.
//
// # Reachable from a shipped scene, and it produces a false pass
//
// SOBRIA contains one node of each of the four: the `markdown` transcript, the
// `input` prompt, the `rule` above the slash menu and the `list` of matches.
// Styling all four — the obvious patch for an order like *"grey out the
// command menu"*, which is almost exactly the order `sobria-dim-the-footer`
// already carries — is **accepted by both validators** and leaves the rendered
// frame **byte-identical**.
//
// That combination is the worst one this project has a name for. The model is
// not refused, so there is no `file:line` to repair from and the repair loop
// has nothing to work with; `converged` asks only that the document validates
// and binds what `must_bind` names, so the case scores as a win; and the screen
// is unchanged. It is the `row_template` shape again — the corpus crediting an
// answer that draws nothing — in the direction EVAL.md names as the dangerous
// one, because a number that looks like good news is the one nobody audits.
//
// # What this guard does not decide
//
// It does not require a container to style its children — that is the
// container guard's business, and the two must not overlap into each other's
// territory. It sweeps childless nodes precisely so that "the token reached
// the frame" can only mean the node applied it to content of its own.
//
// It also does not check that the token is the *right* one, the same deliberate
// limit its three sibling guards carry. A node asking for `warn` and drawing
// `warn` passes here even if the theme maps `warn` to something illegible. The
// goldens own appearance; this owns the property that a declaration a validator
// accepted is not silently dropped on the floor.

// ownStyleWitness is the token the probe node declares.
//
// Deliberately not a token any renderer mints on its own. The first draft used
// `dim` and `list` passed — not because it honoured the declaration, but
// because its placeholder row is hardcoded `dim` and the search found that.
// A witness that collides with a hardcoded name measures the hardcoding.
const ownStyleWitness = "OWNSTYLEWITNESS"

// nodeDrawsOwnContent reports whether a node type puts content of its own in
// the frame, and whether that content carries the token the node declared.
//
// The first return value is the axis that decides who this guard may hold to
// account, and it is measured rather than listed: the node is rendered
// childless, so any span carrying text came from the node itself. A pure
// container draws nothing without children, has no content of its own to apply
// a token to, and demanding one would be demanding that chrome restyle its
// content — the exact thing the container guard forbids.
func nodeDrawsOwnContent(t *testing.T, ty string, bordered bool) (drew bool, honoured bool) {
	t.Helper()

	n := &scene.Node{
		Type:        ty,
		Text:        "own content",
		Placeholder: "own placeholder",
		Style:       map[string]string{"style": ownStyleWitness},
	}
	if bordered {
		n.BorderRaw = []byte(`"single"`)
	}

	// Binds that make the content-bearing types produce their real content
	// rather than an empty state. A type left unbound still draws (markdown
	// and marquee fall back to n.Text), so this only widens what is measured.
	switch ty {
	case "input":
		n.Bind = "user.input"
	case "list":
		n.Bind = "slash.matches"
	}

	// Two matches, not one, and the reason is a defect this probe had while
	// it was being written. With a single match, the only row a list can
	// draw *is* the selected row — and the selected row keeps "text"
	// unconditionally, because its brightness is the sole indication of what
	// Enter will submit (BINDS.md §4.3). So a one-match probe measures the
	// highlight the engine is right to protect, concludes the declaration was
	// dropped, and reports a defect in the one piece of this behaviour that
	// is deliberate. A guard that cries wolf is a guard that gets deleted.
	state := fold.State{
		UserInput:     "typed text",
		ThinkingText:  "thinking",
		SlashActive:   true,
		SlashSelected: 0,
		SlashMatches: []fold.SlashMatch{
			{Name: "/help", Description: "show help", Category: "core"},
			{Name: "/quit", Description: "leave the session", Category: "core"},
		},
	}

	r := &Renderer{Width: 40, Height: 8}
	frame := r.renderNode(n, state, 8)

	for _, line := range frame.Live {
		for _, span := range line {
			if strings.TrimSpace(span.Text) != "" {
				drew = true
			}
			if span.Style == ownStyleWitness {
				honoured = true
			}
		}
	}
	return drew, honoured
}

func TestEveryNodeDrawsItsOwnContentUnderItsOwnToken(t *testing.T) {
	types := nodeTypesInRenderNode(t)

	// The floor every guard in this package carries. If the parse stops
	// finding renderNode's cases, nothing is swept and this file reports
	// success having measured nothing.
	if len(types) < 5 {
		t.Fatalf("found only %d node types in renderNode's switch (%v); the sweep is reading the wrong dispatch\n"+
			"consequence: with no node types, nothing is rendered and this guard passes vacuously.",
			len(types), types)
	}

	var dropped []string
	contentTypes := 0

	for _, ty := range types {
		// A type counts as content-bearing if it draws something childless
		// in either border arm: `box` draws only its frame, and that frame
		// is chrome it is entitled to style through borderStyleName.
		var drewAny bool
		var offences []string

		for _, bordered := range []bool{false, true} {
			drew, honoured := nodeDrawsOwnContent(t, ty, bordered)
			if !drew {
				continue
			}
			drewAny = true
			if !honoured {
				arm := "borderless"
				if bordered {
					arm = "bordered"
				}
				offences = append(offences, arm)
			}
		}

		if !drewAny {
			// A pure container. It has no content of its own, so there is
			// nothing here for a token to land on, and the question of what
			// happens to its children's tokens belongs to the container guard.
			continue
		}
		contentTypes++

		if len(offences) > 0 {
			dropped = append(dropped, ty+" ("+strings.Join(offences, ", ")+")")
		}
	}

	sort.Strings(dropped)

	if len(dropped) > 0 {
		t.Errorf("%d node type(s) draw their own content and discard the style token they declared:\n  %s\n\n"+
			"consequence: the document validates — `style` is a universal property in SCENES.md and\n"+
			"ValidateTokens refuses an undefined token on any node type — and the frame is unchanged.\n"+
			"There is no refusal to repair from, so Phase 2's repair loop has no input, and `converged`\n"+
			"scores the answer as correct because it only asks that the document validates and binds\n"+
			"what must_bind names. Reachable from the shipped default: SOBRIA carries one node of each\n"+
			"of these types, and styling them changes nothing on screen (see\n"+
			"TestStylingAShippedSceneChangesItsFrame). That is the row_template shape, in the direction\n"+
			"EVAL.md names as the dangerous one: a corpus over-crediting the model is the number nobody\n"+
			"audits.\n"+
			"remedy: apply styleName(n.Style) to the spans the node emits for its own content, as\n"+
			"renderText, renderMarquee and renderSpinner already do. Where the node mints a token of\n"+
			"its own for a reason (a list's `dim` empty state, the input's placeholder), the declared\n"+
			"token is the one the author asked for and the minted one is the default it replaces.",
			len(dropped), strings.Join(dropped, "\n  "))
	}

	// A guard that swept only containers would find no content types and
	// report success. This is the same floor the container guard carries,
	// pointed at the opposite population.
	if contentTypes == 0 {
		t.Fatal("no node type drew content of its own, so this guard's passing means nothing\n" +
			"consequence: a green result here would certify a styling surface nobody measured.\n" +
			"remedy: confirm renderNode still dispatches the content types (text, markdown, input,\n" +
			"list, rule, spinner, marquee) to renderers that emit spans without needing children.")
	}

	t.Logf("swept %d node type(s), %d of which draw content of their own", len(types), contentTypes)
}

// TestStylingAShippedSceneChangesItsFrame is the reachability half, and it is
// separate on purpose.
//
// The sweep above is a property over constructed nodes, and a reader is
// entitled to ask whether any of it can happen to a real document. This
// answers with the factory scene: it takes SOBRIA, applies the token a user
// would apply, and requires the screen to respond. If styling nodes of the
// shipped default changes nothing, the format's promise that `style` is
// universal is false for the interface the product actually ships.
//
// It asserts the weakest possible thing — that *something* moved — for the
// same reason its siblings do. Which token, and whether the result is legible,
// is the goldens' question.
//
// # The Skip this used to take, and why it was the finding
//
// The population was `restyleOffendingNodes`: the nodes the sweep above
// measures as *dropping* their token. That set is empty whenever the engine is
// correct, so on a clean tree the test skipped, and the only states it had were
//
//	                    sweep    reachability
//	R12a injected       FAIL     FAIL
//	clean tree          PASS     SKIP
//
// There is no state in which it caught something its sibling did not — 100%
// overlap by R10c — and it announced that every run through a Skip line, which
// next to `ok` reads as housekeeping rather than as a guard reporting it has
// nothing to do. That is the third severity a finding has hidden behind in
// this tree, after a t.Errorf on an unreachable decision and a t.Logf on a
// passing run.
//
// Two things were wrong underneath it. The population was defined by the
// defect's *presence*, which is the one thing a reachability question must not
// depend on: "can a user reach this surface" is a fact about the scene, and it
// stays true after the bug is fixed. And the probe fold was empty — no history,
// no todos, no thinking text — so the content-bearing nodes drew their empty
// states and the question was asked of placeholders.
//
// Rebuilt on both counts: the population is every node type the *sweep*
// classifies as content-bearing, stamped in SOBRIA under a populated fold, and
// the assertion is per type. Measured that way, with content in the fold:
//
//	input yes · list yes · markdown yes · marquee yes · rule yes · text yes
//	overlay no · row no · stack no   (containers: no content of their own)
//
// Six of six content types present in SOBRIA respond, so the property is
// asserted rather than skipped, and a type that stops responding is named.
func TestStylingAShippedSceneChangesItsFrame(t *testing.T) {
	// A populated fold, and that is the second half of the finding. With an
	// empty fold the transcript, the todo list and the marquee all draw their
	// empty states, which are different code paths under different tokens —
	// the reachability of the real content would go unmeasured behind a
	// placeholder that happened to move.
	state := fold.State{
		UserInput:     "typed text",
		ThinkingText:  "thinking about it",
		AgentWorking:  true,
		SlashActive:   true,
		SlashSelected: 0,
		SlashMatches: []fold.SlashMatch{
			{Name: "/help", Description: "show help", Category: "core"},
			{Name: "/quit", Description: "leave the session", Category: "core"},
		},
		History: []fold.ChatLine{
			{Role: "user", Text: "a turn of the transcript"},
			{Role: "assistant", Text: "the turn after it"},
		},
		Todos: []fold.TodoItem{{Task: "first task"}},
	}
	r := &Renderer{Width: 60, Height: 20}

	// The population is every type the sweep classifies as drawing content of
	// its own — measured by nodeDrawsOwnContent, not listed here, so the two
	// halves cannot drift apart. Crucially it is *not* conditioned on the type
	// currently dropping its token: that was the old Skip's mistake, and it
	// made the check evaporate exactly when the engine was healthy.
	var contentTypes []string
	for _, ty := range nodeTypesInRenderNode(t) {
		drewPlain, _ := nodeDrawsOwnContent(t, ty, false)
		drewBordered, _ := nodeDrawsOwnContent(t, ty, true)
		if drewPlain || drewBordered {
			contentTypes = append(contentTypes, ty)
		}
	}

	var inert, present []string

	for _, ty := range contentTypes {
		doc, err := scene.ParseFile("../../testdata/SOBRIA.json")
		if err != nil {
			t.Fatalf("parse SOBRIA: %v", err)
		}

		before := frameSignature(r.RenderFrame(doc, state))
		if restyleNodesOfType(doc.Root, ty) == 0 {
			// SOBRIA carries no node of this type. `box` and `spinner` are in
			// the engine's vocabulary and not in the factory scene, which is a
			// fact about the scene and not a defect.
			continue
		}
		present = append(present, ty)

		// The witness has to be a token the theme defines, or the check would
		// be measuring the validator instead: an undefined token is refused,
		// and a refusal is the outcome this whole class of defect never
		// produces. The factory tokens are carried over too, because SOBRIA
		// references `dim` and `header` on nodes this patch does not touch — a
		// theme holding only the witness would refuse the scene for reasons
		// unrelated to what is being measured.
		factory := theme.Factory()
		tokens := map[string]ui.Style{ownStyleWitness: {Attrs: ui.AttrDim}}
		for _, name := range factory.Tokens() {
			tokens[name] = factory.Resolve(name)
		}
		if errs := scene.ValidateTokens(doc, theme.FromMap(tokens)); len(errs) > 0 {
			t.Fatalf("the restyled SOBRIA (%s) was refused by the token validator: %v\n"+
				"consequence: this test can only demonstrate the silent failure if the document is accepted.", ty, errs)
		}

		if frameSignature(r.RenderFrame(doc, state)) == before {
			inert = append(inert, ty)
		}
	}

	sort.Strings(inert)

	// The floor. If SOBRIA stopped carrying any content-bearing node — or the
	// classifier stopped finding them — every type would be `continue`d and
	// this would report success having stamped nothing. That is the vacuous
	// pass the Skip used to take openly, and it must not come back silently.
	if len(present) == 0 {
		t.Fatal("SOBRIA carries no node of any content-bearing type, so this reachability check stamped nothing\n" +
			"consequence: a green result would certify that the shipped scene responds to styling without\n" +
			"having styled a single node of it.\n" +
			"remedy: confirm nodeDrawsOwnContent still classifies the content types, and that SOBRIA still\n" +
			"carries its markdown transcript, input prompt, rule and slash list.")
	}

	if len(inert) > 0 {
		t.Errorf("%d content-bearing type(s) in the shipped default scene ignore a declared token: %s\n\n"+
			"consequence: the user (or the agent patching on their behalf) declared a token on a node\n"+
			"the format says may carry one, both validators accepted it, and the screen did not change.\n"+
			"Nothing refuses, so there is no address to repair from and the eval corpus scores the\n"+
			"unchanged screen as converged.\n"+
			"remedy: see TestEveryNodeDrawsItsOwnContentUnderItsOwnToken — the node types that draw\n"+
			"their own content must apply their declared token to it.",
			len(inert), strings.Join(inert, ", "))
	}

	t.Logf("stamped %d content-bearing type(s) present in SOBRIA: %s", len(present), strings.Join(present, ", "))
}

// restyleNodesOfType stamps the witness token onto every node of one type and
// reports how many it touched.
//
// Unlike restyleOffendingNodes, which it replaces, the population is not
// conditioned on the node currently dropping its token. Reachability is a
// question about the scene, and the answer must not change when the engine is
// repaired.
func restyleNodesOfType(n *scene.Node, ty string) int {
	if n == nil {
		return 0
	}
	count := 0
	if n.Type == ty {
		if n.Style == nil {
			n.Style = map[string]string{}
		}
		n.Style["style"] = ownStyleWitness
		count++
	}
	for _, c := range n.Children {
		count += restyleNodesOfType(c, ty)
	}
	return count
}

// TestADeclaredTokenDoesNotEraseTheSlashHighlight is the limit on the rule the
// two tests above enforce, and it exists because nothing else in the tree did.
//
// Those tests say a declared token replaces the one the renderer minted. Taken
// without a boundary that is too strong: `slash.selected` is signed in
// BINDS.md §4.3 as *"the list renders this row bright and every other row
// dim"*, so the highlight is not a default the author is overriding, it is the
// only answer on screen to "what will Enter do". A list styled `dim` by its
// scene must still distinguish the live row — "a menu with no highlight while
// Enter still acts is a menu that lies".
//
// renderList draws that line by taking the declared token for the resting rows
// and holding the selected row at "text". That was an argued decision, and
// arguing is not measuring: the injection that removes the distinction —
// letting the declared token win on the selected row too — was run against the
// whole suite and **nothing failed**. A part of a fix that no test defends is
// the part a later simplification deletes, with the commit message "make the
// token handling uniform".
func TestADeclaredTokenDoesNotEraseTheSlashHighlight(t *testing.T) {
	n := &scene.Node{
		Type:  "list",
		Bind:  "slash.matches",
		Style: map[string]string{"style": ownStyleWitness},
	}
	state := fold.State{
		SlashActive:   true,
		SlashSelected: 1,
		SlashMatches: []fold.SlashMatch{
			{Name: "/help", Description: "show help", Category: "core"},
			{Name: "/quit", Description: "leave the session", Category: "core"},
			{Name: "/undo", Description: "undo the last turn", Category: "core"},
		},
	}

	r := &Renderer{Width: 60, Height: 10}
	frame := r.renderNode(n, state, 10)

	if len(frame.Live) < 3 {
		t.Fatalf("the list drew %d row(s) for 3 matches; this guard cannot compare a highlight it cannot see\n"+
			"consequence: a green result would certify a distinction that was never rendered.", len(frame.Live))
	}

	tokensOf := func(line ui.Line) map[string]bool {
		out := map[string]bool{}
		for _, span := range line {
			out[span.Style] = true
		}
		return out
	}

	selected := tokensOf(frame.Live[1])
	resting := tokensOf(frame.Live[0])

	// The selected row must not be drawn in the scene's token, and the
	// resting rows must be — together those say the declaration was honoured
	// *and* the highlight survived it. Checking only the first half would
	// pass a list that ignored the declaration entirely.
	if selected[ownStyleWitness] {
		t.Errorf("the selected row was drawn under the token the scene declared for the list\n\n" +
			"consequence: a scene that styles its command menu erases the highlight, and the row\n" +
			"Enter will submit becomes indistinguishable from the rest. BINDS.md §4.3 signs the\n" +
			"opposite: the list renders the selected row bright and every other row dim, and the\n" +
			"host keeps that index clamped precisely so the menu always shows what it will act on.\n" +
			"remedy: in renderList, the declared token replaces the resting rows' minted token and\n" +
			"the selected row stays \"text\". A declaration may replace a default, not a semantic.")
	}
	if !resting[ownStyleWitness] {
		t.Errorf("no resting row was drawn under the token the scene declared\n\n" +
			"consequence: this guard would pass on a list that ignored the declaration outright,\n" +
			"since an ignored token is also a token the selected row does not carry — the pass\n" +
			"would certify the defect its sibling test exists to catch.\n" +
			"remedy: see TestEveryNodeDrawsItsOwnContentUnderItsOwnToken.")
	}
}

// frameSignature renders a frame down to text plus the token every span is
// drawn under, so a comparison catches a styling change that moves no cell.
// Comparing rendered text alone would report "identical" for exactly the
// defect this file is about.
func frameSignature(f ui.Frame) string {
	var b strings.Builder
	for _, line := range f.Live {
		for _, span := range line {
			b.WriteString("«")
			b.WriteString(span.Style)
			b.WriteString(":")
			b.WriteString(span.Text)
			b.WriteString("»")
		}
		b.WriteString("\n")
	}
	return b.String()
}
