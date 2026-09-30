package engine

import (
	"reflect"
	"sort"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// The class this file guards has now moved three levels in: field → name →
// body. Each guard was written at the level the previous defect lived at, and
// each next instance arrived one level deeper.
//
//   - TestEverySignedBindIsProjected reads the field: is there a case at all?
//     The first four instances had none, and drew "[…]".
//   - TestEveryBindCaseBodyReadsTheFoldState reads the body: does the case so
//     much as mention the fold? The fifth instance had a case that returned the
//     literal "0" under a stale comment, and the label audit read green.
//   - TestEveryCorpusMustBindFieldReachesTheFrame reads the frame, which is the
//     strongest check of the three — but only for the binds the corpus happens
//     to name.
//
// That last clause is this file's reason to exist, and it was measured rather
// than assumed: the corpus's `must_bind` lists cover 7 of the 30 signed binds,
// so 23 have no projection check that looks at a value at all. Worse, the
// coverage is incidental. session.tokens_used — the fifth instance — is inside
// those 7 only because TestDoingNothingDoesNotPass widened two weak cases and
// needed a bind SOBRIA did not already have. Had that widening chosen a
// different field, the frame test would have rendered right past the very
// defect it was written after.
//
// So this guard asks the same question from a direction the corpus cannot
// influence: for every signed bind that fold.State carries a field for,
// perturbing *only that field* must change what resolveBind returns.
//
// It is deliberately weak, in the same way and for the same reason as the body
// audit. It cannot tell a correct projection from one that returns a plausible
// wrong value, and it does not try — the behavioural tests and the goldens own
// that. What it can prove is the one property a constant cannot satisfy: that
// the output depends on the input. A case returning a literal fails here no
// matter how signed, plausible, or well-commented the literal is.
//
// Verified against the real defect rather than argued: with the fifth
// instance's `return "0"` reinjected into render.go, this test reports
// session.tokens_used inert; with the fold-reading projection restored, it
// passes.

// pulseBindsWithoutFoldFields are the signed binds that deliberately have no
// fold.State field, so there is nothing to perturb and their absence here is
// not a defect. Both are documented as such in docs/BINDS.md, and they are
// listed by name rather than skipped by silence: a bind that quietly vanishes
// from fold.State would otherwise be excused by the same gap.
//
//   - user.input.submitted is the enter-key pulse consumed by the host's submit
//     path, signed only to reserve the name (BINDS.md §4.3).
//
// session.new_milestone WAS here while its lifetime was undecided; Scene 11
// decided it (State.NewMilestone / deriveNewMilestone), so the entry is gone
// and the guard now perturbs the bool field and requires resolveBind to vary
// with it — the same standard as every other bind, exactly as the note below
// promises.
//
// If either remaining entry ever gains a fold field, delete it here — the guard
// will then hold it to the same standard as every other bind.
var pulseBindsWithoutFoldFields = map[string]string{
	"user.input.submitted": "enter-key pulse consumed by the host submit path; name reserved (BINDS.md §4.3)",
	// The community installer's live view state (Scene 7, J3 follow-up) WAS here
	// while it had no fold field, sitting for the same reason ui.hidden did before
	// F3 — "no fold field yet, not a defect". State.CommunityQuery/Matches/Selected
	// now exist, so community.query and community.selected map to scalar fields the
	// scalar guard perturbs directly and community.matches maps to a slice the
	// composite guard owns; leaving them here would be the stale exemption this
	// map's own comment warns against, so they were deleted the moment the fields
	// landed.
	// ui.hidden was here while it had no fold field. F3 added State.UIHidden and
	// the walk filter that consumes it, so it now maps to a field and is owned by
	// the composite guard (perturbScalar skips the map, walkConsumedBinds proves
	// the drop). Leaving it here would be the stale exemption this map's own
	// comment warns against.
}

// derivedBindsWithoutScalarField are signed scalar binds whose value is folded
// but NOT in a single json-tagged State field this guard can perturb: it is
// derived from composite state indexed by another field. This is a third
// category the two buckets above do not cover — a pulse has no folded value at
// all, a scalar bind has exactly one field, and these have a folded value that
// comes from two fields at once. Listing them here is the same on-the-record
// exemption walkConsumedBinds is in the signed-bind audit: not "cannot be
// projected" and not "no folded value", but "the folded value varies, proven by
// a behavioural test this reflection guard cannot express". Each entry names
// that test, so the exemption is a pointer to a live check and not a place a
// bind can hide.
//
//   - community.selected.{name,version,preview} resolve community.selected (an
//     int field) against community.matches (a slice field) to the selected
//     entry's field, in selectedCommunityMatch. Perturbing either field alone
//     misses the point — the value depends on the *pair* — so the single-field
//     perturbation this guard does cannot witness the dependence. That the
//     projection follows the selection is proven at the frame by
//     TestLiveInstallerPreviewPaneShowsTheSelectedEntry (moving the cursor
//     changes which blurb is drawn) and at the resolver by the three
//     community.selected.* cases in
//     TestEverySignedScalarBindTheFoldComputesReachesTheFrame (index 1 yields the
//     second entry's field, which fails on a projection that ignored the cursor).
var derivedBindsWithoutScalarField = map[string]string{
	"community.selected.name":    "resolves community.selected against community.matches (selectedCommunityMatch); varies by TestLiveInstallerPreviewPaneShowsTheSelectedEntry and the community.selected.* cases in TestEverySignedScalarBindTheFoldComputesReachesTheFrame",
	"community.selected.version": "resolves community.selected against community.matches (selectedCommunityMatch); varies by TestLiveInstallerPreviewPaneShowsTheSelectedEntry and the community.selected.* cases in TestEverySignedScalarBindTheFoldComputesReachesTheFrame",
	"community.selected.preview": "resolves community.selected against community.matches (selectedCommunityMatch); varies by TestLiveInstallerPreviewPaneShowsTheSelectedEntry and the community.selected.* cases in TestEverySignedScalarBindTheFoldComputesReachesTheFrame",
}

// perturbScalar sets f to a value distinct from its zero value and reports
// whether it knew how. It handles the scalar kinds only: composite fields
// (slices, maps) reach the frame through their own rendering paths — a list
// node bound to agent.todos draws one row per entry — and resolveBind is not
// the function that projects them. Claiming to check those here would be the
// guard overstating its reach.
func perturbScalar(f reflect.Value) bool {
	switch f.Kind() {
	case reflect.String:
		f.SetString("PROJECTIONWITNESS")
		return true
	case reflect.Bool:
		f.SetBool(!f.Bool())
		return true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		f.SetUint(7777)
		return true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		f.SetInt(7777)
		return true
	case reflect.Float32, reflect.Float64:
		f.SetFloat(77.77)
		return true
	}
	return false
}

func TestEverySignedBindProjectionVariesWithItsFoldField(t *testing.T) {
	stateType := reflect.TypeOf(fold.State{})

	// fold.State's json tags are the field→bind mapping, and they are already
	// load-bearing elsewhere. Reading them here rather than hand-listing the
	// pairs keeps this guard from becoming a copy that can drift from the
	// struct it describes — the same argument the prompt makes for reading
	// SignedBinds() instead of writing the bind list into it.
	fieldForBind := map[string]int{}
	for i := 0; i < stateType.NumField(); i++ {
		if tag := stateType.Field(i).Tag.Get("json"); tag != "" {
			fieldForBind[tag] = i
		}
	}

	var inert []string
	var unmapped []string
	checked := 0

	for _, bind := range scene.SignedBinds() {
		idx, ok := fieldForBind[bind]
		if !ok {
			if _, excused := pulseBindsWithoutFoldFields[bind]; excused {
				continue
			}
			if _, derived := derivedBindsWithoutScalarField[bind]; derived {
				continue
			}
			unmapped = append(unmapped, bind)
			continue
		}

		zero := fold.State{}
		before := resolveBind(bind, zero)

		perturbed := reflect.New(stateType).Elem()
		if !perturbScalar(perturbed.Field(idx)) {
			// Composite field; not this guard's axis. See perturbScalar.
			continue
		}

		after := resolveBind(bind, perturbed.Interface().(fold.State))
		checked++
		if before == after {
			inert = append(inert, bind+" (returns "+before+" for both the zero state and a perturbed one)")
		}
	}

	sort.Strings(inert)
	sort.Strings(unmapped)

	if len(inert) > 0 {
		t.Errorf("%d signed bind(s) return the same value whether or not their fold field changed:\n  %v\n\n"+
			"consequence: the bind is signed, accepted by validate.go, computed by the fold on every\n"+
			"event, and the screen shows the same thing regardless — which is the checked-but-never-drawn\n"+
			"class in its hardest-to-see form. A constant that happens to equal the bind's signed empty\n"+
			"state makes no frame look broken, and the label and body audits both read green.\n"+
			"remedy: the case body must project the fold field named by its json tag, not restate a\n"+
			"value. If the value genuinely cannot be derived yet, the case must not exist — a missing\n"+
			"case draws the placeholder, which is the honest empty state.",
			len(inert), inert)
	}

	if len(unmapped) > 0 {
		t.Errorf("%d signed bind(s) have no fold.State field and are not listed as pulses: %v\n\n"+
			"consequence: this guard silently skips any bind it cannot map, so an unlisted one is a hole\n"+
			"in the coverage rather than a passing check.\n"+
			"remedy: add the field to fold.State with its json tag, or — if it is genuinely a pulse with\n"+
			"no folded value — add it to pulseBindsWithoutFoldFields with the reason and its BINDS.md\n"+
			"reference, so the exemption is a decision on the record instead of an omission.",
			len(unmapped), unmapped)
	}

	// A guard that checks nothing passes loudest. If a refactor renames the
	// json tags or empties SignedBinds(), every bind lands in a skip bucket and
	// this file reports success while measuring zero binds — the same shape of
	// false pass the corpus's do-nothing model exposed in the grader.
	if checked == 0 {
		t.Fatal("this guard checked zero binds, so its passing means nothing\n" +
			"consequence: a green result here would certify a projection surface nobody measured.\n" +
			"remedy: confirm scene.SignedBinds() is non-empty and that fold.State's json tags still\n" +
			"carry the bind names this test maps by.")
	}

	t.Logf("checked %d scalar bind projection(s) for dependence on their fold field", checked)
}
