package engine

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// Every property SCENES.md calls universal must be honoured or refused at a
// *nested* position too — inside `prefix`, `suffix` or `row_template` — and
// not only on a node the renderer reaches through `children`.
//
// # Why this is a different question from the audit it mirrors
//
// TestEveryUniversalPropertyIsHonouredOrRefused already asks exactly the right
// question — accepted-and-identical is a silent drop — and asks it well. Every
// one of its eight probe pairs sets the property on the document's **root**
// node, so every one of them travels the renderNode path. A nested node does
// not: renderMarquee reads `prefix.Bind`, `prefix.Text`, `prefix.Style` and the
// three matching fields of `n.Suffix` directly out of the struct. Nothing
// nested is dispatched, so anything renderNode does on a node's behalf simply
// does not happen there.
//
// This is the third recurrence of a single defect, and the progression is the
// point:
//
//  1. `when` was honoured by a row and an overlay, and dropped by every other
//     node type.
//  2. a style token was read by four node types and dropped by the rest.
//  3. the fix for both was to move the work into renderNode, which withFocusGlow
//     documents as making "some node types glow and others do not"
//     unrepresentable — true, and scoped to node *types*.
//
// A prefix is a node *position*. No type name identifies it, so no widening of
// renderNode's switch and no sweep over its cases ever arrives there, and the
// remedy that closed the defect twice cannot close it a third time. Measured
// before this file existed: a marquee prefix declaring `when` rendered
// byte-identically under `agent.working` and `!agent.working`, while the same
// node at root position drew under one and vanished under the other. The
// validator accepted both.
//
// # Why it sweeps rather than testing `when`
//
// Testing the one property that was broken would leave this file in the state
// the tree was already in: correct about the case that had been found, silent
// about the axis. The vocabulary is read out of docs/SCENES.md through
// universalsFromDocument — the same source the root audit uses — so a universal
// added to the documentation tomorrow is asked about at both positions on the
// day it lands, and a property nobody wrote a nested probe for fails rather
// than skipping.
//
// # The verdicts, and why "identical" is not always a defect
//
// Three of the eight cannot be answered by comparing frames at this position,
// and each is held to the strongest bar that is actually true of it rather
// than skipped, on the precedent the root audit set with `id`:
//
//   - `id` addresses, it does not draw. Held to a round-trip.
//   - `grow` and `weight` are instructions to a *layout* parent — renderStack
//     and renderHorizontal divide space among `children`. A prefix is a span
//     inside one line that its owner composes, so there is no space to
//     divide and identical frames are the correct answer. They are held to
//     the round-trip too, so the exemption dies with the field.
//
// Naming them here rather than in a skip-list is deliberate, and the reason is
// written at length in the root audit: a map of accepted gaps let two real
// defects out of this package, because the map answered on the engine's
// behalf.

// nestedUniversalProbe is the fragment that sets one universal on a nested
// node, plus the verdict this position can support for it.
type nestedUniversalProbe struct {
	// set is the JSON key/value pair merged into the nested node.
	set string
	// addressingOnly marks a property that correctly leaves the frame
	// unchanged at this position. It is held to a round-trip instead, never
	// skipped.
	addressingOnly bool
	// why documents the exemption at its use site, so a reader does not
	// have to trust the map.
	why string
}

// nestedUniversalProbes is one probe per universal in SCENES.md. Values are
// chosen to be observable: `when` uses a bind that is *true* in
// nestedStyleState, so an honoured gate draws and the control is what differs,
// and `bind` resolves to a value that is not the literal text.
var nestedUniversalProbes = map[string]nestedUniversalProbe{
	"id": {
		set:            `"id":"named"`,
		addressingOnly: true,
		why:            "`id` names a node for binds and focus; it draws nothing at any position",
	},
	"bind": {
		set: `"bind":"model.name"`,
	},
	"when": {
		// The gate must be *closed* for this sweep to observe anything.
		// A satisfied `when` correctly renders identically to a node with
		// no `when` at all — that is the property working, not failing —
		// so probing the true direction accuses the engine of a silent
		// drop whenever the gate is honoured. Measured: the first version
		// of this table used "agent.working" and failed on both branches
		// against a renderer that had just been fixed.
		//
		// It must also be a *signed* bind. The second version used
		// "!agent.working", which reads like the obvious negation and is
		// not: no `!` operator exists anywhere in this engine, evalWhen
		// resolves the whole string as a bind name and gets the
		// placeholder, and BINDS.md has no such row — so Validate refuses
		// the document and this case took the refusal branch without ever
		// rendering. It passed against the broken renderer, which is how
		// it was caught: the counterfactual run with the fix reverted
		// reported this subtest green.
		//
		// ui.max is signed and false in nestedStyleState, so an honoured
		// gate removes the node and the frame moves.
		set: `"when":"ui.max"`,
	},
	"style": {
		set: `"style":{"style":"dim"}`,
	},
	"grow": {
		set:            `"grow":1`,
		addressingOnly: true,
		why: "`grow` instructs a layout parent dividing space among `children`; a nested node is a " +
			"span inside one line its owner composes, so there is no space to divide",
	},
	"weight": {
		set:            `"weight":3`,
		addressingOnly: true,
		why: "`weight` instructs a layout parent dividing space among `children`; a nested node is a " +
			"span inside one line its owner composes, so there is no space to divide",
	},
	"on_press": {
		set: `"on_press":"cmd:/help"`,
	},
	"scroll": {
		set: `"scroll":{"speed":2,"pause_when":"agent.working"}`,
	},
}

// nestedOwner builds a document whose root is a marquee carrying a nested node
// on the named branch, with extra merged into that nested node.
//
// A marquee is used because it is the only owner the shipped scenes nest
// through and the only one the engine renders: `row_template` is refused by
// the validator (relative binds are Scene 5), so `prefix` and `suffix` are the
// live positions. If a second owner learns to nest, nestedOwnerBranches below
// is what has to grow, and the floor is what will say so.
func nestedOwner(branch, extra string) string {
	nested := `{"type":"text","text":"NESTEDPROBE"`
	if extra != "" {
		nested += "," + extra
	}
	nested += `}`
	return `{"root":{"type":"marquee","bind":"thinking.text","` + branch + `":` + nested + `}}`
}

// nestedOwnerBranches are the nested positions the engine actually draws.
var nestedOwnerBranches = []string{"prefix", "suffix"}

func TestEveryUniversalPropertyIsHonouredOrRefusedWhenNested(t *testing.T) {
	props := universalsFromDocument(t)

	// The floor. If SCENES.md is reworded, the reader is renamed, or the
	// document moves, an empty list would make every subtest below vanish
	// and this file would report success having asked nothing — the exact
	// state the tree was in while a nested `when` was being ignored. Eight
	// is what SCENES.md documents today.
	if len(props) < 8 {
		t.Fatalf("read only %d universal propert(ies) from SCENES.md; expected at least 8\n"+
			"consequence: a green result here would certify the nested styling and gating surface\n"+
			"without asking about a single property. This guard exists because that surface was\n"+
			"unmeasured while looking measured.\n"+
			"remedy: confirm docs/SCENES.md still lists the universal properties in the form\n"+
			"universalsFromDocument parses.", len(props))
	}

	for _, prop := range props {
		prop := prop
		t.Run(prop, func(t *testing.T) {
			probe, ok := nestedUniversalProbes[prop]
			if !ok {
				t.Fatalf("SCENES.md calls %q universal and this file has no nested probe for it,\n"+
					"so the property's behaviour inside prefix/suffix is unmeasured. \"Universal\" is a\n"+
					"claim about every node, and a nested node is one: renderMarquee reads its fields\n"+
					"directly, so nothing renderNode does on a node's behalf reaches it.\n"+
					"remedy: add %q to nestedUniversalProbes — a fragment that sets it, and, if the\n"+
					"property cannot draw at this position, addressingOnly with the reason why.", prop, prop)
			}

			for _, branch := range nestedOwnerBranches {
				branch := branch
				t.Run(branch, func(t *testing.T) {
					assertNestedUniversal(t, prop, branch, probe)
				})
			}
		})
	}
}

// assertNestedUniversal is the root audit's three outcomes, asked at a nested
// position: refused with an address, addressing-only and round-tripped, or
// honoured by changing the frame.
func assertNestedUniversal(t *testing.T, prop, branch string, probe nestedUniversalProbe) {
	t.Helper()

	withSrc := nestedOwner(branch, probe.set)
	withoutSrc := nestedOwner(branch, "")

	withDoc, err := scene.ParseDocument([]byte(withSrc))
	if err != nil {
		t.Fatalf("premise broken: the nested %q probe must parse; got %v", prop, err)
	}
	withoutDoc, err := scene.ParseDocument([]byte(withoutSrc))
	if err != nil {
		t.Fatalf("premise broken: the nested %q control must parse; got %v", prop, err)
	}

	// The control must validate clean, or a refusal below could be about the
	// marquee, the bind, or anything else, and the probe would prove nothing
	// about the property.
	if verr := withoutDoc.Validate(); verr != nil {
		t.Fatalf("premise broken: the nested %q control must validate clean, or a refusal of the\n"+
			"probe cannot be attributed to the property; got %v", prop, verr)
	}

	// Outcome 1: refused with an address. Checked first, because a refused
	// document never reaches a frame.
	if verr := withDoc.Validate(); verr != nil {
		var se *scene.Error
		if !errors.As(verr, &se) {
			t.Fatalf("nested %q is refused, but not as a *scene.Error, so the refusal carries no\n"+
				"address (PLAN.md invariant 4): %v", prop, verr)
		}
		if se.Loc == (scene.Loc{}) {
			t.Errorf("the refusal of nested %q carries no address, so the author cannot find the\n"+
				"node inside %s that caused it: %v", prop, branch, verr)
		}
		if !strings.Contains(verr.Error(), prop) {
			t.Errorf("the refusal does not name %q, so the author cannot tell which property was\n"+
				"rejected: %q", prop, verr.Error())
		}
		return
	}

	// Outcome 2: the property addresses rather than draws at this position.
	// Held to a round-trip so the exemption cannot outlive the field.
	if probe.addressingOnly {
		assertNestedRoundTrips(t, prop, branch, withDoc, withoutDoc, probe)
		return
	}

	// Outcome 3: accepted, so it must change what is drawn.
	r := &Renderer{Width: 80, Height: 24}
	state := nestedStyleState()

	got := frameSignature(r.RenderFrame(withDoc, state))
	base := frameSignature(r.RenderFrame(withoutDoc, state))

	// A probe whose owner draws nothing proves nothing either way, and
	// passing it silently is the vacuous pass this package keeps closing.
	// nestedStyleState is built to make the marquee draw; an empty frame
	// means the state has drifted, not that the property is exempt.
	if strings.TrimSpace(base) == "" {
		t.Fatalf("premise broken: the nested %q control drew an empty frame, so a difference could\n"+
			"not have been observed and a pass here would be vacuous.\n"+
			"remedy: nestedStyleState must keep the marquee's thinking.text bind populated.", prop)
	}

	if got == base {
		t.Errorf("SCENES.md calls %q universal, the validator accepts it on a node nested under\n"+
			"%q, and the frame is byte-identical to one that does not set it.\n\n"+
			"consequence: the silent drop, at the position no guard in this package reaches.\n"+
			"The root audit passes for this property because its probe sets it on the document\n"+
			"root, which travels renderNode; a nested node never does — renderMarquee reads\n"+
			"prefix.Bind/.Text/.Style and n.Suffix.* straight out of the struct, so renderNode's\n"+
			"gate, its focus glow, and anything else it does on a node's behalf never happen here.\n"+
			"Measured: this is exactly how a nested `when` came to render identically under\n"+
			"agent.working and !agent.working while the same node at root drew and then vanished.\n\n"+
			"remedy: honour the property where the owning renderer composes its nested spans, as\n"+
			"renderMarquee now does for `when` via hiddenByWhen; or, if it cannot be drawn yet,\n"+
			"add it to scene.unrenderedFields so it is refused with an address. Recording the gap\n"+
			"in a map in this file is not a remedy — a map here cannot change what the engine\n"+
			"does, so it would silence the audit and ship the defect.\n\n"+
			"frame with %s: %q\nframe without:  %q",
			prop, branch, prop, strings.TrimSpace(got), strings.TrimSpace(base))
	}
}

// assertNestedRoundTrips is the bar for a property that correctly draws
// nothing at a nested position: the parsed document must still carry it.
//
// It cannot reuse assertRoundTrips from the root audit. That helper asserts
// `doc.Root.ID != ""` — it is written for one property at one position, and
// pointing it at a nested `grow` would check the root marquee's id, find it
// empty, and report a defect in the wrong property. Measured: the first
// version of this file called it and failed all three exempt cases for that
// reason.
//
// The check is made without naming a struct field, by comparing the probe
// document's nested node against the control's after a marshal round-trip. A
// property encoding/json discarded leaves the two identical, which is exactly
// the silence that let `on_press` and `scroll` go unrepresented on scene.Node
// until the root audit was rewritten. So the exemption cannot outlive the
// field: delete `grow` from scene.Node and this fails, rather than passing
// because there is nothing left to compare.
func assertNestedRoundTrips(t *testing.T, prop, branch string, withDoc, withoutDoc *scene.Document, probe nestedUniversalProbe) {
	t.Helper()

	nested := func(d *scene.Document) *scene.Node {
		if d.Root == nil {
			t.Fatalf("premise broken: the %q probe document has no root", prop)
		}
		switch branch {
		case "prefix":
			return d.Root.PrefixNode()
		case "suffix":
			return d.Root.Suffix
		default:
			t.Fatalf("premise broken: no accessor for nested branch %q", branch)
			return nil
		}
	}

	withNode, withoutNode := nested(withDoc), nested(withoutDoc)
	if withNode == nil || withoutNode == nil {
		t.Fatalf("premise broken: both %q documents must carry a node under %q, or the comparison\n"+
			"below is between a node and nothing", prop, branch)
	}

	withJSON, err := json.Marshal(withNode)
	if err != nil {
		t.Fatalf("marshal nested probe node: %v", err)
	}
	withoutJSON, err := json.Marshal(withoutNode)
	if err != nil {
		t.Fatalf("marshal nested control node: %v", err)
	}

	if string(withJSON) == string(withoutJSON) {
		t.Errorf("SCENES.md calls %q universal, and a node under %q that sets it parses into\n"+
			"something indistinguishable from one that does not.\n\n"+
			"consequence: encoding/json discarded the key in silence — the scene parses, validates\n"+
			"and renders with the property gone before any layer could have an opinion. This\n"+
			"property is exempt from the frame comparison because it addresses rather than draws\n"+
			"at this position (%s), not from existing.\n"+
			"remedy: keep the field on scene.Node.\n\n"+
			"nested node with %s: %s\nnested node without:  %s",
			prop, branch, probe.why, prop, withJSON, withoutJSON)
	}
}

// The two binds this file gates with: signed in BINDS.md, and opposite in
// nestedStyleState. Both halves are asserted before they are used, because
// both have already been wrong here — see nestedWhenShut's note.
const (
	// nestedWhenOpen is satisfied in nestedStyleState, so an honoured gate
	// draws the node.
	nestedWhenOpen = "agent.working"
	// nestedWhenShut is not satisfied, so an honoured gate removes it.
	//
	// It is a signed bind rather than the `!agent.working` that reads like
	// the obvious way to write "closed". There is no `!` operator in this
	// engine: evalWhen resolves the entire string through resolveBind, which
	// returns the placeholder for an unknown name, and the placeholder is
	// falsey — so the expression appears to work while meaning nothing, and
	// Validate refuses the document because BINDS.md signs no such row.
	// Measured: the first version of this file used it, and the
	// counterfactual with the engine fix reverted reported these subtests
	// green.
	nestedWhenShut = "ui.max"
)

// TestNestedWhenClosesAsWellAsOpens is the direction the sweep above cannot
// ask on its own. A gate that is *satisfied* correctly renders the same as no
// gate at all, so passing that direction proves nothing about whether `when`
// is read; the observable claim is that an unsatisfied gate removes the node.
//
// The two documents differ only in which signed bind they gate on, and the
// state makes exactly one of them true, so the assertion does not depend on
// what the marquee draws — only that the gate distinguishes them. That
// matters: the first probe written for this file stamped one expression alone,
// saw no movement, and could not tell an ignored gate from an expression that
// happened to be true.
func TestNestedWhenClosesAsWellAsOpens(t *testing.T) {
	r := &Renderer{Width: 80, Height: 24}
	state := nestedStyleState()

	for _, branch := range nestedOwnerBranches {
		t.Run(branch, func(t *testing.T) {
			open := nestedOwner(branch, `"when":"`+nestedWhenOpen+`"`)
			shut := nestedOwner(branch, `"when":"`+nestedWhenShut+`"`)

			openDoc, err := scene.ParseDocument([]byte(open))
			if err != nil {
				t.Fatalf("premise broken: the open-gate document must parse; got %v", err)
			}
			shutDoc, err := scene.ParseDocument([]byte(shut))
			if err != nil {
				t.Fatalf("premise broken: the shut-gate document must parse; got %v", err)
			}

			// Both documents must validate clean, or the shut one is
			// being rejected rather than gated and this test measures the
			// validator. That is not hypothetical: it is exactly what the
			// first version of this file did.
			if verr := openDoc.Validate(); verr != nil {
				t.Fatalf("premise broken: the open-gate document must validate clean; got %v", verr)
			}
			if verr := shutDoc.Validate(); verr != nil {
				t.Fatalf("premise broken: the shut-gate document must validate clean, or this test is\n"+
					"measuring the validator instead of the renderer. %q must be a bind signed in\n"+
					"BINDS.md §4.5; got %v", nestedWhenShut, verr)
			}

			// The premise the whole test rests on: the state satisfies
			// exactly one of the two gates.
			if !evalWhen(nestedWhenOpen, state) || evalWhen(nestedWhenShut, state) {
				t.Fatalf("premise broken: nestedStyleState must satisfy %q and not %q, or the two\n"+
					"documents below are not opposites and a difference between them means nothing.\n"+
					"Got %s=%v, %s=%v",
					nestedWhenOpen, nestedWhenShut,
					nestedWhenOpen, evalWhen(nestedWhenOpen, state),
					nestedWhenShut, evalWhen(nestedWhenShut, state))
			}

			openFrame := frameSignature(r.RenderFrame(openDoc, state))
			shutFrame := frameSignature(r.RenderFrame(shutDoc, state))

			if strings.TrimSpace(openFrame) == "" {
				t.Fatalf("premise broken: the open-gate document drew nothing, so the closed gate\n" +
					"cannot be distinguished from it and a pass would be vacuous.")
			}

			if openFrame == shutFrame {
				t.Errorf("a node nested under %q renders identically whether its `when` is satisfied\n"+
					"or not, so the gate is being ignored at this position.\n\n"+
					"consequence: a scene author gates a prefix or suffix, the validator accepts it,\n"+
					"and the node draws unconditionally. `when` is the format's only way to say \"not\n"+
					"now\", and a silently ignored gate shows content the document explicitly excluded\n"+
					"— with no refusal and no warning, so the author's only evidence is a wrong screen.\n"+
					"This is the third position-scoped recurrence of one defect: `when` was honoured by\n"+
					"a row and an overlay only, the fix moved the gate into renderNode to cover every\n"+
					"node *type*, and a nested node is a *position* that never reaches renderNode.\n\n"+
					"remedy: call hiddenByWhen on the nested node where the owning renderer composes\n"+
					"its spans, as renderMarquee does for both of its.\n\n"+
					"open gate: %q\nshut gate: %q",
					branch, strings.TrimSpace(openFrame), strings.TrimSpace(shutFrame))
			}
		})
	}
}

// TestNestedUniversalProbesCoverEveryDocumentedUniversal fails when the probe
// table and SCENES.md disagree in the direction the sweep cannot see. The
// sweep iterates the *document*, so a probe for a property SCENES.md no longer
// calls universal is never run and never noticed — dead weight that reads as
// coverage, which is how a hand-copied list has already drifted in this package
// in both directions.
func TestNestedUniversalProbesCoverEveryDocumentedUniversal(t *testing.T) {
	documented := map[string]bool{}
	for _, p := range universalsFromDocument(t) {
		documented[p] = true
	}

	var orphans []string
	for p := range nestedUniversalProbes {
		if !documented[p] {
			orphans = append(orphans, p)
		}
	}
	sort.Strings(orphans)

	if len(orphans) > 0 {
		t.Errorf("nestedUniversalProbes carries %d probe(s) for propert(ies) SCENES.md no longer calls\n"+
			"universal: %s\n\n"+
			"consequence: the sweep iterates the document, so these never run. A probe that cannot\n"+
			"execute is not coverage, and its presence makes the table look complete.\n"+
			"remedy: delete the entr(ies), or restore the propert(ies) to SCENES.md if the removal\n"+
			"was accidental.", len(orphans), strings.Join(orphans, ", "))
	}
}

// assertNestedJSONShape is a guard on this file's own premise rather than on
// the engine. Every probe above is a JSON fragment spliced into a node, and a
// fragment with a typo'd key or a wrong-typed value produces a node that does
// not carry the property at all — which renders identically to the control and
// would be reported as a silent drop in the engine.
//
// That is not hypothetical. Writing this file, `"grow":true` was spliced in and
// json.Unmarshal rejected it: grow is an int. Had the probe been built with a
// map and marshalled, the error would have been swallowed and `grow` would have
// been reported as a dropped universal.
func TestNestedProbeFragmentsActuallySetTheirProperty(t *testing.T) {
	for prop, probe := range nestedUniversalProbes {
		prop, probe := prop, probe
		t.Run(prop, func(t *testing.T) {
			var withNode, withoutNode scene.Node

			withRaw := extractNested(t, nestedOwner("prefix", probe.set))
			withoutRaw := extractNested(t, nestedOwner("prefix", ""))

			if err := json.Unmarshal(withRaw, &withNode); err != nil {
				t.Fatalf("the %q probe fragment does not decode into a scene.Node: %v\n"+
					"consequence: the probe would build a node without the property, render identically\n"+
					"to the control, and be reported as an engine defect. Measured: `\"grow\":true` failed\n"+
					"exactly this way while this file was being written — grow is an int.\n"+
					"remedy: fix the fragment's key or value type.", prop, err)
			}
			if err := json.Unmarshal(withoutRaw, &withoutNode); err != nil {
				t.Fatalf("premise broken: the control fragment must decode; got %v", err)
			}

			withJSON, err := json.Marshal(withNode)
			if err != nil {
				t.Fatalf("marshal probe node: %v", err)
			}
			withoutJSON, err := json.Marshal(withoutNode)
			if err != nil {
				t.Fatalf("marshal control node: %v", err)
			}

			// The decoded node must differ from the control. A fragment
			// whose key scene.Node does not declare is discarded by
			// encoding/json without a word, and the two nodes come out
			// identical — the exact silence that let on_press and scroll
			// go unrepresented until the root audit was rewritten.
			if string(withJSON) == string(withoutJSON) {
				t.Errorf("the %q probe sets a key scene.Node does not carry, so encoding/json discarded\n"+
					"it and the probe node is identical to the control.\n\n"+
					"consequence: the sweep would compare two identical documents and report the engine\n"+
					"silently dropping %q, when in fact the property never reached it. A probe that\n"+
					"cannot set its property is a false accusation in the flattering direction — it\n"+
					"looks like coverage and it measures the test harness.\n"+
					"remedy: declare the field on scene.Node, or correct the fragment %q.",
					prop, prop, probe.set)
			}
		})
	}
}

// extractNested pulls the nested node back out of a document built by
// nestedOwner, so the probe fragment can be checked in isolation.
func extractNested(t *testing.T, doc string) json.RawMessage {
	t.Helper()
	var outer struct {
		Root map[string]json.RawMessage `json:"root"`
	}
	if err := json.Unmarshal([]byte(doc), &outer); err != nil {
		t.Fatalf("premise broken: nestedOwner must produce a parseable document; got %v", err)
	}
	nested, ok := outer.Root["prefix"]
	if !ok {
		t.Fatalf("premise broken: nestedOwner must place the probe under \"prefix\"")
	}
	return nested
}
