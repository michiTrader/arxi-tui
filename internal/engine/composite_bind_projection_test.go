package engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// The fourth level of the same defect class, and the one the previous guard
// declined to reach — for a stated reason that turned out to be half right.
//
// TestEverySignedBindProjectionVariesWithItsFoldField perturbs one fold field
// and requires resolveBind to answer differently. It measured 23 of the 30
// signed binds. Two of the remaining seven are pulses with no fold field,
// exempted by name with their reason recorded, precisely so a bind that later
// vanishes from fold.State cannot be excused by the same gap.
//
// The other five were not exempted by name. They fell out of `perturbScalar`
// through a bare `continue`:
//
//	if !perturbScalar(perturbed.Field(idx)) {
//	        // Composite field; not this guard's axis.
//	        continue
//	}
//
// The comment is accurate about the axis — resolveBind returns a string and a
// list of todos is not a string — and the reach was reported honestly as 23.
// But the mechanism is the exact shape that guard's own second design point
// forbids: a silent skip bucket. The reason given for listing the two pulses
// by name was that skipping the unmappable quietly would let a future bind
// slip through the same crack. Five binds were already going through a crack
// one kind-switch wide, and nothing in the tree named them or asked anything
// of them. Measured with a throwaway probe against the real inventory:
//
//	signed=30  scalars checked=23
//	skipped silently (5): agent.blocked.blocked_ref, agent.todos,
//	                      chat.history, slash.matches, team.members
//	unmapped, exempted by name (2): session.new_milestone,
//	                                user.input.submitted
//
// 23 + 5 + 2 = 30, so the five are the entire remainder.
//
// # The first draft of this file was wrong, and the way it was wrong is the finding
//
// That draft carried a hand-written map from bind to the one node type that
// draws it — `list` for agent.todos, `markdown` for chat.history — and
// rendered each composite through its single recorded type. It passed, and it
// caught an injected defect that blanked agent.todos.
//
// Then the same injection was tried on chat.history: `resolveBind`'s case was
// changed to call ChatHistoryMarkdown() and throw the result away. **The
// entire suite stayed green, this guard included.** The reason is that
// chat.history has *two* independent projection paths — renderMarkdown draws
// state.History directly, and resolveBind returns ChatHistoryMarkdown() for
// the text/spinner path and for every `when` gate through evalWhen. Rendering
// the bind through its one recorded node type exercised the first and never
// touched the second.
//
// So the hand-written map was not a convenience. It was a second inventory,
// written by the same kind of judgement the scalar guard refused to let the
// corpus make, and it silently chose which half of a bind's surface got
// measured. Swept across every node type instead, the real shape came out:
//
//	chat.history    varies in [markdown text spinner]
//	thinking.text   varies in [markdown text marquee spinner]
//	user.input      varies in [input text spinner]
//	agent.todos     varies in [list]
//	slash.matches   varies in [list]
//	team.members    varies in []   (placeholder, on the record)
//	blocked_ref     varies in []   (placeholder, on the record)
//
// Two binds reach the frame through more than one path. A guard that picks one
// path per bind is guessing, and the guess was already wrong once before the
// file was committed.
//
// # What this guard therefore does
//
// For every signed composite bind, it perturbs only that fold field and
// renders the bind through **every node type the engine knows**, with the type
// list parsed out of renderNode's own switch rather than typed here. If no
// node type's frame changes, the bind is inert. That is the axis resolveBind
// cannot be asked about, because resolveBind returns a string and these fields
// are slices and maps.
//
// Then it asks the second question the first draft could not: for a composite
// that *does* have a resolveBind case, that case must vary too. chat.history
// is the only one today, and it is the exact path the injection hid in.
//
// # The two placeholders are checked, not excused
//
// team.members and agent.blocked.blocked_ref draw "[…]" in every node type,
// and that is correct today: both are in acceptedUnprojectedBinds with a real
// blocker — Scene 9's per-row templates over an unsigned relative-bind
// namespace, and a documented command-resolution rule that is not a value to
// print. So this guard does not demand all five draw. It demands each is in
// exactly one of two states, and that the state matches what the tree already
// claims in writing:
//
//   - not in acceptedUnprojectedBinds → some node type's frame must move;
//   - in acceptedUnprojectedBinds → no node type's frame may move.
//
// The second direction is the one that earns its keep. acceptedUnprojectedBinds
// is a list of prose claims, and the label audit only checks that a listed bind
// has not gained a case. Nothing checked that a listed bind still draws
// nothing. A half-finished rendering path that projects a value while its
// recorded justification goes on saying the work is blocked is the
// stale-comment failure that produced the fifth instance, and it is now checked
// from both sides.
//
// # What it deliberately does not check
//
// The same weakness as its siblings, for the same reason: it cannot tell a
// correct rendering from a plausible wrong one. A list that draws its todos in
// the wrong order, or the actor where the task belongs, passes here. The
// goldens and the behavioural scene tests own that. What this proves is the one
// property a placeholder cannot fake — that the frame depends on the state —
// for the five binds that had no check of any kind on that axis.

// compositeWitness is written into every string field of a composite's element
// and into its map values. It must not occur in any placeholder, border glyph
// or empty-state label, or a frame that draws nothing could match it by
// accident and be scored as a projection.
const compositeWitness = "COMPOSITEPROJECTIONWITNESS"

// perturbComposite fills f — a slice or map field of fold.State — with one
// element carrying the witness, and reports whether it knew how.
//
// It is the counterpart to perturbScalar and covers exactly the kinds that
// function returns false for. The element is built by reflection rather than by
// naming fold.TodoItem, fold.ChatLine and the rest, so it cannot drift when an
// element type gains a field.
func perturbComposite(f reflect.Value) bool {
	switch f.Kind() {
	case reflect.Slice:
		elem := reflect.New(f.Type().Elem()).Elem()
		if !fillStringFields(elem) {
			// An element with no string field has nowhere to put a
			// witness, so a rendered frame could not be searched for one.
			// Reporting false sends it to the hard failure below rather
			// than to a silent skip — the whole point of this file.
			return false
		}
		f.Set(reflect.Append(reflect.MakeSlice(f.Type(), 0, 1), elem))
		return true
	case reflect.Map:
		if f.Type().Key().Kind() != reflect.String {
			return false
		}
		val := reflect.New(f.Type().Elem()).Elem()
		switch val.Kind() {
		case reflect.Interface:
			val.Set(reflect.ValueOf(compositeWitness))
		case reflect.String:
			val.SetString(compositeWitness)
		default:
			return false
		}
		m := reflect.MakeMap(f.Type())
		m.SetMapIndex(reflect.ValueOf("witness"), val)
		f.Set(m)
		return true
	}
	return false
}

// fillStringFields writes the witness into every string field of a struct (or
// into the value itself, if it is a string) and reports whether it found at
// least one. Every field is filled rather than just the first, because which
// field a renderer chooses to draw is the renderer's business: filling one and
// guessing wrong would report a working projection as inert.
func fillStringFields(v reflect.Value) bool {
	if v.Kind() == reflect.String {
		v.SetString(compositeWitness)
		return true
	}
	if v.Kind() != reflect.Struct {
		return false
	}
	filled := false
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.CanSet() && f.Kind() == reflect.String {
			f.SetString(compositeWitness)
			filled = true
		}
	}
	return filled
}

// nodeTypesInRenderNode reads the node-type vocabulary out of renderNode's own
// switch. This is the correction the first draft of this file needed: a
// hand-written bind→node-type map is a second inventory, and it chose which
// half of chat.history's surface got measured — the half the injected defect
// was not in. Parsing the dispatch keeps the sweep as wide as the engine is,
// and a node type added to renderNode is swept from the moment it exists.
func nodeTypesInRenderNode(t *testing.T) []string {
	t.Helper()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse engine package: %v", err)
	}

	var types []string
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				fd, ok := n.(*ast.FuncDecl)
				if !ok || fd.Name.Name != "renderNode" {
					return true
				}
				ast.Inspect(fd, func(m ast.Node) bool {
					cc, ok := m.(*ast.CaseClause)
					if !ok {
						return true
					}
					for _, expr := range cc.List {
						lit, ok := expr.(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							continue
						}
						types = append(types, strings.Trim(lit.Value, "`\""))
					}
					return true
				})
				return false
			})
		}
	}
	sort.Strings(types)
	return types
}

// renderBindToText renders one node of the given type, bound to bind, against
// state, and returns the frame's text with styling forgotten. The width is wide
// enough that no witness is word-wrapped: a wrapped witness is on screen and a
// substring search misses it anyway — a false alarm shaped exactly like the
// real defect, which is the kind that teaches a reader to ignore a guard.
func renderBindToText(t *testing.T, bind, nodeType string, state fold.State) string {
	t.Helper()
	r := &Renderer{Width: 120, Height: 24}
	frame := r.renderNode(&scene.Node{Type: nodeType, Bind: bind}, state, 24)
	var b strings.Builder
	for _, l := range frame.Live {
		b.WriteString(l.Text())
		b.WriteString("\n")
	}
	return b.String()
}

// templateProjectedBinds are collection binds whose projection is one
// row_template instance per element (D1 / BINDS.md §4.7), not a scalar a bare
// `bind: X` node can draw. The sweep over bare node types cannot witness them —
// a `list` with no row_template shows the placeholder by design — so this guard
// witnesses each through a template render instead, which is the path that
// actually draws it. The witness is a `row.<field>` the perturbed fold fills,
// so a template over it varies between the empty and witnessed states.
var templateProjectedBinds = map[string]struct {
	witness string
	reason  string
}{
	"team.members": {
		witness: "row.state",
		reason:  "one row_template instance per member (Scene 9); a bare bind has no scalar projection",
	},
}

// rowTemplateVaries reports whether a list whose row_template draws `witness`
// (a `row.<field>`) over `bind` produces different frames for two fold states.
// It is the template-aware counterpart to the bare-node sweep: the mechanism
// that draws a collection is a template, so that is where its projection is
// witnessed.
func rowTemplateVaries(t *testing.T, bind, witness string, a, b fold.State) bool {
	t.Helper()
	r := &Renderer{Width: 120, Height: 24}
	list := &scene.Node{Type: "list", Bind: bind, RowTemplate: &scene.Node{Type: "text", Bind: witness}}
	render := func(st fold.State) string {
		var sb strings.Builder
		for _, l := range r.renderNode(list, st, 24).Live {
			sb.WriteString(l.Text())
			sb.WriteString("\n")
		}
		return sb.String()
	}
	return render(a) != render(b)
}

// hiddenFilterVaries reports whether the ui.hidden walk filter actually removes
// a node whose id enters the set. It is the witness for a walk-consumed bind:
// ui.hidden is never drawn, so the frame's dependence on it is the drop itself.
// A node with a fixed id renders its text with the empty set and must render
// nothing once its id is a member — if reverting hiddenByWhenRow's membership
// test leaves the node on screen, this returns false and the composite guard
// reports ui.hidden inert, which is the counterfactual that keeps the filter
// honest.
func hiddenFilterVaries(t *testing.T) bool {
	t.Helper()
	r := &Renderer{Width: 120, Height: 24}
	n := &scene.Node{Type: "text", ID: "witnessnode", Text: "VISIBLEWITNESS"}
	render := func(st fold.State) string {
		var sb strings.Builder
		for _, l := range r.renderNode(n, st, 24).Live {
			sb.WriteString(l.Text())
			sb.WriteString("\n")
		}
		return sb.String()
	}
	visible := render(fold.State{})
	hidden := render(fold.State{UIHidden: map[string]bool{"witnessnode": true}})
	return visible != hidden
}

func TestEverySignedCompositeBindIsDrawnOrRecordedUnprojected(t *testing.T) {
	stateType := reflect.TypeOf(fold.State{})

	// Same mapping source as the scalar guard: fold.State's own json tags.
	// Two guards reading the bind→field relation from two places could
	// disagree about which binds exist, and a bind visible to neither is the
	// gap both were written to close.
	fieldForBind := map[string]int{}
	for i := 0; i < stateType.NumField(); i++ {
		if tag := stateType.Field(i).Tag.Get("json"); tag != "" {
			fieldForBind[tag] = i
		}
	}

	nodeTypes := nodeTypesInRenderNode(t)
	// The floor on the sweep itself. If the parse stops finding renderNode's
	// cases, every bind would be rendered through no node type at all, no
	// frame could move, and the file would report every composite inert —
	// or, with the inert check inverted, report success having drawn
	// nothing.
	if len(nodeTypes) < 5 {
		t.Fatalf("found only %d node types in renderNode's switch (%v); the sweep is reading the wrong dispatch\n"+
			"consequence: a bind rendered through no node type cannot move a frame, so this guard would be\n"+
			"measuring the parser rather than the renderer.", len(nodeTypes), nodeTypes)
	}

	var inert, stale, unwitnessed []string
	checked := 0

	for _, bind := range scene.SignedBinds() {
		idx, ok := fieldForBind[bind]
		if !ok {
			// No fold field: a pulse. The scalar guard owns that case and
			// holds the named exemption list; duplicating it here would
			// mean two places to update and one of them going stale.
			continue
		}

		field := stateType.Field(idx)
		probe := reflect.New(stateType).Elem()
		if perturbScalar(probe.Field(idx)) {
			// A scalar: the sibling guard measures it on resolveBind.
			continue
		}

		// A walk-consumed bind (ui.hidden, D3) is a composite the engine reads
		// as a visibility filter, never draws as a value — so the witness sweep
		// below, which searches a rendered frame for a string, cannot see it: it
		// produces no text. It is measured instead through the mechanism that
		// consumes it, exactly as templateProjectedBinds are measured through the
		// template that draws them. Handling it here rather than in the sweep
		// keeps the witness/inert machinery from flagging it "unwitnessed" for a
		// value it will never render.
		if reason, ok := walkConsumedBinds[bind]; ok {
			if !hiddenFilterVaries(t) {
				inert = append(inert, bind+" (walk-consumed but hiding a node's id did not change the frame — "+reason+")")
			}
			checked++
			continue
		}

		// From here down the bind is a composite and this file owns it.
		// Nothing below may `continue` past a problem.
		if !perturbComposite(probe.Field(idx)) {
			unwitnessed = append(unwitnessed, bind+" (fold field type "+field.Type.String()+")")
			continue
		}
		checked++

		perturbed := probe.Interface().(fold.State)

		// Sweep every node type. Which one draws the bind is the engine's
		// business, not this test's: recording one per bind is what let the
		// chat.history injection through the first draft.
		var variesIn []string
		for _, nt := range nodeTypes {
			if renderBindToText(t, bind, nt, fold.State{}) != renderBindToText(t, bind, nt, perturbed) {
				variesIn = append(variesIn, nt)
			}
		}

		// The second path. A composite with a resolveBind case reaches the
		// frame through text and spinner nodes and through every `when`
		// gate via evalWhen, and that path can be blanked while the node
		// type that dispatches directly on the bind keeps drawing.
		hasResolveCase := resolveBind(bind, perturbed) != placeholderValue
		resolveVaries := hasResolveCase && resolveBind(bind, fold.State{}) != resolveBind(bind, perturbed)

		reason, recordedUnprojected := acceptedUnprojectedBinds[bind]
		tp, templateProjected := templateProjectedBinds[bind]

		switch {
		case recordedUnprojected && len(variesIn) > 0:
			stale = append(stale, bind+" (recorded unprojected — "+reason+" — but its frame moves in node type(s) "+strings.Join(variesIn, ", ")+")")

		case recordedUnprojected:
			// Correct today: the placeholder, with the blocker on record.

		case templateProjected && rowTemplateVaries(t, bind, tp.witness, fold.State{}, perturbed):
			// Projected through a row_template, not as a bare bind: a
			// collection has no scalar value, so the sweep over bare node types
			// above cannot witness it. It is witnessed here through the
			// mechanism that actually draws it (BINDS.md §4.7), and it varies.

		case templateProjected:
			inert = append(inert, bind+" (template-projected but a row_template over it did not vary with its fold field — "+tp.reason+")")

		case len(variesIn) == 0:
			inert = append(inert, bind+" (no node type's frame changed when its fold field did)")

		case hasResolveCase && !resolveVaries:
			inert = append(inert, bind+" (draws in node type(s) "+strings.Join(variesIn, ", ")+", but its resolveBind case returns the same value for both states)")
		}
	}

	sort.Strings(inert)
	sort.Strings(stale)
	sort.Strings(unwitnessed)

	if len(inert) > 0 {
		t.Errorf("%d composite bind projection(s) do not depend on their fold field:\n  %v\n\n"+
			"consequence: this is the checked-but-never-drawn class on the axis the scalar guard cannot\n"+
			"reach. resolveBind returns a string, so a list of todos or a chat transcript is never asked\n"+
			"the question at all. The bind is signed, accepted by validate.go, folded on every event, and\n"+
			"the panel shows its empty state at every state of the run.\n"+
			"Note the second form above: a bind can keep drawing through its own node type while its\n"+
			"resolveBind case is blanked. That path feeds text and spinner nodes and every `when` gate\n"+
			"through evalWhen, and a gate that reads empty hides a node the scene asked to show.\n"+
			"remedy: draw the fold field in the rendering path that is inert, or — if it genuinely cannot\n"+
			"be drawn yet — leave it unhandled so it shows the placeholder and record it in\n"+
			"acceptedUnprojectedBinds with the blocker in writing. A gap on the record is recoverable; a\n"+
			"gap that renders like a finished empty panel is not.",
			len(inert), inert)
	}

	if len(stale) > 0 {
		t.Errorf("%d bind(s) are recorded in acceptedUnprojectedBinds but their frame responds to the fold state:\n  %v\n\n"+
			"consequence: the justification has outlived the condition it describes, which is exactly how\n"+
			"the fifth instance survived — session.tokens_used carried a comment saying the budget was not\n"+
			"yet wired, long after it was. A reader trusting that list now believes a gap exists where the\n"+
			"work is done, and the entry argues against finishing something already finished.\n"+
			"remedy: delete the entry. If the rendering is only partial, say what remains in its reason\n"+
			"rather than leaving a claim that reads as untouched.",
			len(stale), stale)
	}

	if len(unwitnessed) > 0 {
		t.Errorf("%d composite bind(s) could not be given a witness value:\n  %v\n\n"+
			"consequence: an unexercised composite is a silent skip, and a silent skip is the hole this\n"+
			"file was written to close — the previous guard passed five binds through a bare `continue`\n"+
			"while its own doctrine required naming every exemption.\n"+
			"remedy: extend fillStringFields or perturbComposite so the element type can carry a witness.\n"+
			"Do not add a skip.",
			len(unwitnessed), unwitnessed)
	}

	// The floor every guard in this family carries. If SignedBinds() were
	// truncated, or fold.State's composite fields renamed or flattened,
	// every composite would fall out of the loop and this file would report
	// success having rendered nothing — the same false-pass shape the
	// do-nothing model exposed in the grader.
	if checked == 0 {
		t.Fatal("this guard rendered zero composite binds, so its passing means nothing\n" +
			"consequence: a green result here would certify a rendering surface nobody measured.\n" +
			"remedy: confirm scene.SignedBinds() is non-empty and that fold.State still carries its\n" +
			"slice and map fields with the bind names in their json tags.")
	}

	t.Logf("swept %d composite bind(s) across %d node type(s): %v", checked, len(nodeTypes), nodeTypes)
}
