package engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// The structural audit for the checked-but-never-drawn class at the level of
// the bind vocabulary.
//
// internal/scene already audits the *fields* of Node this way, and that test
// was written to stop the class from being found by hand a fourth time. It
// did not, because the fourth instance was not a field: `todos.count` and
// eight others were signed binds — names, not struct members — so an audit
// enumerating Node's fields could not see them. The class had moved one level
// up and the guard was still looking at the old level.
//
// So this is the same question asked of the other axis: docs/BINDS.md §4 signs
// a closed vocabulary, validate.go accepts exactly that vocabulary, and every
// name in it must be in one of two states here — handled by the engine, or
// justified in writing. What it may not be is accepted by the validator and
// unknown to the renderer, because that combination is the silent one: the
// author who spells the bind correctly gets a placeholder and no diagnostic,
// while the author who misspells it gets an addressed refusal.
//
// The audit reads case labels out of the source rather than calling
// resolveBind, because it exists to catch a *newly signed* bind nobody wired
// up, and a behavioural check would need a hand-written state per bind —
// which is the same hand-enumeration that missed the first four instances.
// The behavioural half, for the binds that do resolve, is
// TestEverySignedScalarBindTheFoldComputesReachesTheFrame.
func TestEverySignedBindIsHandledOrJustified(t *testing.T) {
	signed := scene.SignedBinds()
	// The floor is what stops a silent vacuous pass: if SignedBinds ever
	// returns a truncated or empty inventory, every bind would be "handled"
	// by not being asked about, and the audit would report success while
	// checking nothing.
	if len(signed) < 20 {
		t.Fatalf("scene.SignedBinds() returned only %d binds; the audit is reading the wrong inventory and would pass vacuously", len(signed))
	}

	handled := bindCaseLabelsInEngine(t)
	if len(handled) == 0 {
		t.Fatal("found no bind case labels in the engine source; the audit cannot tell an\n" +
			"unhandled bind from a parse failure, and reporting the whole vocabulary as\n" +
			"unhandled would be a wall of false alarms.")
	}

	for _, bind := range signed {
		t.Run(bind, func(t *testing.T) {
			if reason, ok := acceptedUnprojectedBinds[bind]; ok {
				// A justification that has gone stale is worse than none: it
				// tells the next reader the gap is understood when the code
				// has moved on. If the engine learned to handle it, say so.
				if handled[bind] {
					t.Errorf("%q is listed in acceptedUnprojectedBinds (%q) but the engine now handles it;\n"+
						"the list has become a comment that is false. Remove the entry.", bind, reason)
				}
				return
			}

			if reason, ok := walkConsumedBinds[bind]; ok {
				// Handled, but not by a switch case this audit can read. A
				// walk-consumed bind is turned into no text — it filters the
				// tree instead — so the label scan below cannot see it and
				// would wrongly report it unhandled. The stale direction still
				// matters: if such a bind ALSO gained a resolveBind case, the
				// two would be independent projections of one name and the
				// entry would be hiding that. So the exemption is refused the
				// moment a label appears.
				if handled[bind] {
					t.Errorf("%q is listed in walkConsumedBinds (%q) but also has a resolveBind case;\n"+
						"the walk filter and a resolved value are two projections of one bind, and this entry now\n"+
						"conceals the second. Reconcile them: either the value is real and the entry is wrong, or\n"+
						"the case is spurious and should be removed.", bind, reason)
				}
				return
			}

			if !handled[bind] {
				t.Errorf("bind %q is signed in docs/BINDS.md and accepted by validate.go, and the engine has no case for it.\n"+
					"consequence: a scene naming it validates clean and draws the placeholder, so spelling the bind\n"+
					"correctly produces silence while misspelling it produces an addressed refusal. Worse, until the\n"+
					"placeholder was made falsy, a `when` gated on it also rendered its node visible. Success is\n"+
					"reported and the screen is wrong, with no diagnostic anywhere — the same class as the style key,\n"+
					"the border token, row_template, and the nine scalar binds before this.\n"+
					"remedy: handle it in resolveBind (or renderList, for a collection bind) reading the fold field;\n"+
					"or, if it genuinely cannot be projected yet, add it to acceptedUnprojectedBinds with the reason\n"+
					"in writing, so the gap is recorded rather than rediscovered.", bind)
			}
		})
	}
}

// acceptedUnprojectedBinds are signed binds the engine does not handle, and
// that is currently correct. Each entry is a claim that had to be measured.
//
// Unlike the nine scalars fixed alongside this audit, none of these is a value
// sitting finished in fold.State waiting for a switch case. Each is missing
// something real — a shape decision, a phase, or a host mechanism — and
// projecting one "somehow" would invent format ahead of the work meant to
// design it. That was the argument for refusing row_template rather than
// drawing it, and it applies unchanged here.
var acceptedUnprojectedBinds = map[string]string{
	// team.members WAS here — a collection blocked on the unsigned `row.*`
	// namespace. D1 signed that namespace (BINDS.md §4.7) and renderList now
	// instantiates a row_template over it (rowScopesFor's "team.members" case),
	// so it is handled and this entry was removed: keeping it would be the false
	// comment this audit's own remedy warns about.

	// A set of node ids consumed by the engine walk as a visibility filter, not
	// a scalar resolveBind can return and not a value to print. D3 signed the
	// name and its set semantics (BINDS.md §4.3); F3 built the walk filter that
	// consumes it. It is therefore handled — but by the walk, not by a switch
	// case this audit's label scan can see — so it lives in walkConsumedBinds
	// below rather than here. Keeping it in this "cannot be projected yet" list
	// after the filter shipped would be the stale-comment failure this audit's
	// own remedy warns about.

	// An object, not text. BINDS.md is explicit that the remedy is not a
	// separate bind: the engine is meant to read blocked_on together with
	// blocked_ref and resolve the pair into a command string per the
	// blocked_ref rule in spec/events.md (approval -> `arxi inbox approve
	// <inbox_id>`, budget -> `arxi run unpause --budget <higher>`). That is a
	// projection with real rules behind it, and inventing a rendering for the
	// raw object now would pre-empt them. blocked_on and actor, the two
	// scalars of this group, are projected.
	"agent.blocked.blocked_ref": "an object whose projection is a documented command-resolution rule, not a value to print",

	// A pulse rather than a value, and the fold does not compute it: no
	// State field exists, because a pulse that persists is not a pulse. Scene
	// 11 gates a banner on it, so it needs a decision about how long a pulse
	// stays true — one frame, a duration, until dismissed — which is
	// animation-adjacent and belongs with that scene rather than ahead of it.
	"session.new_milestone": "an event pulse with no fold field; how long a pulse reads true is a Scene 11 decision",

	// BINDS.md §4.3 says it outright: consumed by the host's submit path,
	// "listed so the name is reserved". It is the enter-key pulse, not
	// something a scene displays. It is signed to stop the name being taken
	// by something else, which is a reservation working exactly as intended.
	"user.input.submitted": "a reserved name for the host's enter-key pulse; signed so it cannot be reused, never displayed",
}

// walkConsumedBinds are signed binds the engine consumes structurally in the
// render walk as a filter, rather than resolving them to a value or drawing
// them as a collection. They are handled — the frame changes with the fold
// field — but the two label/witness audits cannot see them, because a bind that
// is never turned into text has no switch case to read and no rendered witness
// to search for. Listing them here is the same on-the-record exemption
// acceptedUnprojectedBinds is, for the opposite reason: not "cannot be projected
// yet" but "projected by the walk, proven by a behavioural drop test rather than
// a label". Each entry names the guard that actually measures it, so the
// exemption is a pointer to a live check and not a place a bind can hide.
//
//   - ui.hidden: a set of node ids; a node renders iff its id is not a member
//     (BINDS.md §4.3, D3; filter in hiddenByWhenRow). Proven by
//     hiddenFilterVaries in composite_bind_projection_test.go, which drops a
//     node when its id enters the set and would fail if the walk stopped
//     reading it.
var walkConsumedBinds = map[string]string{
	"ui.hidden": "a set of node ids consumed by the render walk as a visibility filter (BINDS.md §4.3, D3); proven by hiddenFilterVaries",
}

// bindCaseLabelsInEngine collects the string literals the engine switches on,
// across every switch in the package's non-test source. resolveBind is the
// main one, but collection binds are dispatched in renderList and elsewhere,
// so restricting the scan to a single function would report a handled bind as
// missing.
//
// Only dotted labels are kept. The engine switches on plenty of other strings
// — border shapes ("double", "ascii"), anchors ("top-right", "full") — and a
// bind name is a dotted path by construction, which separates the two cleanly
// without having to enumerate the others.
func bindCaseLabelsInEngine(t *testing.T) map[string]bool {
	t.Helper()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse engine package: %v", err)
	}

	labels := make(map[string]bool)
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				cc, ok := n.(*ast.CaseClause)
				if !ok {
					return true
				}
				for _, expr := range cc.List {
					lit, ok := expr.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					value := strings.Trim(lit.Value, "`\"")
					if strings.Contains(value, ".") {
						labels[value] = true
					}
				}
				return true
			})
		}
	}
	return labels
}
