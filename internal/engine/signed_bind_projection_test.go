package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// The fourth instance of the checked-but-never-drawn class, one level up from
// the three before it.
//
// The first three were *fields*: a style key, a border token, `row_template`.
// This one is a *bind* — the vocabulary itself. docs/BINDS.md §4 signs the
// name, validate.go's signedBinds accepts it, internal/fold computes it into a
// State field on every event, and resolveBind has no case for it, so it falls
// to the default and draws "[…]".
//
// The signature is the one all four share: the thing is checked, which is
// exactly what makes it look connected. An author who misspells `todos.cout`
// gets a refusal with an address, which is positive evidence the bind is
// wired. Spelling it correctly gets silence and a placeholder.
//
// What makes this instance worse than `row_template` is that the fold already
// computes every one of these. There is no missing design here and no
// unsigned namespace to invent: State.TodosCount is maintained by
// deriveTodosCount() on every fold, and the projection layer simply never
// reads it. The value exists, is correct, and is thrown away one function
// short of the screen.
//
// It is also the first instance to reach the measuring instrument rather than
// a scene. The corpus case maximum-count-the-tasks lists todos.count in
// must_bind, and its own recorded convergence document binds it — so the
// grader scored that document converged while the tasks panel rendered "[…]".
// Unlike row_template, which was reachable from a case, this one was already
// inside the corpus's ground truth.
func TestEverySignedScalarBindTheFoldComputesReachesTheFrame(t *testing.T) {
	// Each case pins a bind whose value the fold maintains, against a state
	// where that value is distinguishable from both the empty state and the
	// placeholder. A test that asserted only "not […]" would pass on a case
	// that returned the wrong field.
	for _, tc := range []struct {
		bind  string
		state fold.State
		want  string
	}{
		{"todos.count", fold.State{TodosCount: 7}, "7"},
		{"run.quiescent.diagnosis", fold.State{QuiescentDiag: "stage X advances with no owner"}, "stage X advances with no owner"},
		{"agent.blocked.blocked_on", fold.State{BlockedOn: "approval"}, "approval"},
		{"agent.blocked.actor", fold.State{BlockedActor: "backend"}, "backend"},
		{"slash.typed", fold.State{SlashTyped: "max"}, "max"},
		{"slash.selected", fold.State{SlashSelected: 3}, "3"},
		{"ui.focus", fold.State{UIFocus: "prompt"}, "prompt"},
		{"ui.max", fold.State{UIMax: "tasks"}, "tasks"},
		{"ui.surface", fold.State{UISurface: "config"}, "config"},
	} {
		t.Run(tc.bind, func(t *testing.T) {
			doc, err := scene.ParseDocument([]byte(fmt.Sprintf(
				`{ "root": { "type": "stack", "children": [
				   { "type": "text", "bind": %q }
				 ]}}`, tc.bind)))
			if err != nil {
				t.Fatalf("ParseDocument: %v", err)
			}
			// The bind must be accepted first: this test is about a bind the
			// product commits to, not about smuggling in a new one.
			if verr := doc.Validate(); verr != nil {
				t.Fatalf("the validator refuses %q, so this case is not testing the defect it claims: %v", tc.bind, verr)
			}

			r := Renderer{Width: 60, Height: 4}
			plain := r.RenderFrame(doc, tc.state).Plain()

			if !strings.Contains(plain, tc.want) {
				t.Errorf("bind %q draws no value; the frame does not contain %q\n"+
					"consequence: docs/BINDS.md §4 signs this bind, validate.go accepts it, and internal/fold\n"+
					"computes it on every event — so a scene author who spells it correctly gets a placeholder\n"+
					"and no diagnostic, while one who misspells it gets an addressed refusal. Success is reported\n"+
					"and the wrong screen is shown, which is the outcome with no error anywhere. This is the same\n"+
					"class as the style key, the border token and row_template, and unlike row_template the value\n"+
					"already exists in fold.State: it is dropped one function short of the frame.\n"+
					"remedy: add the case to resolveBind reading the fold field the document already names;\n"+
					"or, if the bind genuinely cannot be projected, refuse it so the author gets an address.\n"+
					"frame:\n%s", tc.bind, tc.want, plain)
			}
		})
	}
}

// TestAnUnresolvedBindDoesNotTurnOnAWhenGatedNode is the other half of the
// defect, and the half that is not merely a silent drop.
//
// resolveBind answers an unknown bind with "[…]", and evalWhen calls anything
// that is not "", "0" or "false" truthy. So a node gated on a bind the engine
// cannot resolve renders *visible*. For `ui.max` that inverts the signed
// default exactly: BINDS.md gives "null — no pane is maximized", and Scene 10
// gates each pane on it, so the unresolved state shows maximized chrome on a
// screen where nothing is maximized.
//
// A silent drop hides something the author asked for. This shows something the
// author asked to hide, which is worse: the drop degrades toward the empty
// state, and this degrades toward noise the user cannot turn off.
//
// The placeholder itself stays — ADR-0003 wants an unresolvable bind to draw
// rather than crash. What changes is that a placeholder is not evidence of
// truth: "I could not resolve this" must not read as "yes".
func TestAnUnresolvedBindDoesNotTurnOnAWhenGatedNode(t *testing.T) {
	// A deliberately unresolvable name. It goes straight to evalWhen rather
	// than through a document, because the validator refuses unsigned binds —
	// the point here is the projection layer's own contract, which the
	// forward-compatibility rule in ADR-0003 can expose to names this build
	// does not know.
	if evalWhen("some.bind.this.build.cannot.resolve", fold.State{}) {
		t.Errorf("a bind the engine cannot resolve reads as truthy, so a `when` gated on it renders visible\n" +
			"consequence: the placeholder \"[…]\" is not \"\", \"0\" or \"false\", so an unresolved gate turns its\n" +
			"node ON. For ui.max that is an exact inversion of the signed default (\"null — no pane is\n" +
			"maximized\"), and Scene 10 gates every pane on it: the unresolved state paints maximized chrome\n" +
			"over a screen with nothing maximized. A silent drop degrades toward the empty state; this\n" +
			"degrades toward chrome the user cannot dismiss.\n" +
			"remedy: treat an unresolved bind as falsy in evalWhen. The placeholder stays for display — an\n" +
			"unknown bind must still draw rather than crash (ADR-0003) — but \"I cannot resolve this\" is not\n" +
			"an affirmative answer to a visibility question.")
	}
}
