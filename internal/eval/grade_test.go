package eval

import (
	"fmt"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// The grader is the harness' judge, and everything downstream inherits its
// mistakes as facts about the model. These tests hold it to the three
// properties the scores depend on: it must not call a refused document
// accepted, it must not lose an address, and it must not let a document pass
// by deleting what the order asked for.

// parse is the two-step every caller makes before grading.
func parse(t *testing.T, name, body string) (*scene.Document, error) {
	t.Helper()
	return scene.ParseNamed(name, []byte(body))
}

// TestGradeBothCatchesTheValidatorTheCaseDidNotName is the reason GradeBoth
// exists at all.
//
// A case names the validator its attempt targets, because the author knows
// what they wrote. A model does not: asked to grey a footer it may equally
// return a document with an unsigned bind. If the runner graded a model's
// document through only the validator the case happened to name, a document
// the engine refuses to load would be scored as converged — and a false pass
// is the worst result this harness can produce, because in the output it is
// indistinguishable from real success.
func TestGradeBothCatchesTheValidatorTheCaseDidNotName(t *testing.T) {
	// A document whose only fault is an unsigned bind — invisible to the
	// token validator, which is the validator a "make it grey" case names.
	body := `{
  "root": {
    "type": "stack",
    "children": [
      { "type": "text", "bind": "model.current" }
    ]
  }
}`
	doc, parseErr := parse(t, "unsigned.json", body)

	if v := Grade(doc, parseErr, KindToken); v.Accepted {
		// This is not a bug, it is the premise: the token validator has
		// nothing to say about binds. Asserting it keeps the premise honest,
		// so the test below cannot quietly become tautological.
		t.Logf("premise holds: the token validator accepts a document with an unsigned bind")
	} else {
		t.Fatalf("premise broken: the token validator refused a bind problem: %s\n"+
			"consequence: this test no longer demonstrates that a single-validator grade can miss a fault, so GradeBoth's justification is untested.\n"+
			"remedy: pick a fault that is genuinely invisible to the token validator.", v.Message)
	}

	v := GradeBoth(doc, parseErr)
	if v.Accepted {
		t.Fatalf("GradeBoth accepted a document the engine refuses to load\n" +
			"consequence: the runner would score a case as converged on a document the product rejects, and a false pass reads exactly like a real one in the results.\n" +
			"remedy: GradeBoth must run both validators, not the one the case named.")
	}
	if !v.Addressed {
		t.Errorf("GradeBoth refused without an address: %s\n"+
			"consequence: invariant 4 requires file:line: on every refusal, and the repair turn has nothing to aim at without it.\n"+
			"remedy: preserve the Loc from the underlying *scene.Error.", v.Message)
	}
}

// TestGradeBothCatchesATokenFaultTheBindPathCannotSee is the other half of
// GradeBoth, and it exists because the first version of this file did not have
// it.
//
// The symmetry is easy to assume and wrong to skip. The test above proves
// GradeBoth is not blind to bind faults; deleting the token call from GradeBoth
// left the whole package green, which means the token half was asserted by
// nothing. A grader that silently stopped running ValidateTokens would mark
// every undefined-token document converged, and the corpus' one token case
// would keep passing because it grades through Grade(KindToken) directly.
//
// A guard that cannot fail is worth nothing; this is the injection that proved
// the gap.
func TestGradeBothCatchesATokenFaultTheBindPathCannotSee(t *testing.T) {
	// Every bind here is signed, so Validate has nothing to say: the only
	// fault is a token the factory theme does not define.
	body := `{
  "root": {
    "type": "stack",
    "children": [
      { "type": "text", "bind": "model.name", "style": { "token": "grey" } }
    ]
  }
}`
	doc, parseErr := parse(t, "token-only.json", body)

	if v := Grade(doc, parseErr, KindBind); !v.Accepted {
		t.Fatalf("premise broken: the bind validator refused this document: %s\n"+
			"consequence: the document must be bind-clean for this test to prove the token half runs.\n"+
			"remedy: use only signed binds here.", v.Message)
	}

	v := GradeBoth(doc, parseErr)
	if v.Accepted {
		t.Fatalf("GradeBoth accepted a document referencing an undefined token\n" +
			"consequence: the runner would score as converged a document the token validator refuses, so every undefined-token answer would count as a pass — and the token vocabulary is open by design, which makes this the most common scene error there is.\n" +
			"remedy: GradeBoth must run ValidateTokens after the bind path, not instead of nothing.")
	}
	if !v.Addressed {
		t.Errorf("token refusal came back without an address: %s\n"+
			"consequence: invariant 4 requires file:line: on every refusal, including the token path.\n"+
			"remedy: carry TokenError.Loc into the verdict.", v.Message)
	}
}

// TestGradeBothReportsTheBindRefusalFirst pins the order, because the order is
// what the user would see.
//
// Document.Validate is what load runs first, so when a document offends both
// validators the refusal a user actually meets is the bind one. A runner that
// reported the token refusal instead would feed the model an address for the
// second-order problem and score the repair against a message the product
// never shows.
func TestGradeBothReportsTheBindRefusalFirst(t *testing.T) {
	body := `{
  "root": {
    "type": "stack",
    "children": [
      { "type": "text", "bind": "model.current", "style": { "token": "grey" } }
    ]
  }
}`
	doc, parseErr := parse(t, "both.json", body)

	bind := Grade(doc, parseErr, KindBind)
	token := Grade(doc, parseErr, KindToken)
	if bind.Accepted || token.Accepted {
		t.Fatalf("premise broken: this document must offend both validators (bind accepted=%v, token accepted=%v)\n"+
			"consequence: the test cannot demonstrate precedence if only one validator objects.\n"+
			"remedy: restore a document with both an unsigned bind and an undefined token.",
			bind.Accepted, token.Accepted)
	}

	got := GradeBoth(doc, parseErr)
	if got.Message != bind.Message {
		t.Errorf("GradeBoth reported %q, the bind validator reports %q\n"+
			"consequence: the model would be handed the address of a fault the engine never gets far enough to report, so the repair turn is aimed at the wrong line.\n"+
			"remedy: run the bind path first, matching the engine's own load order.",
			got.Message, bind.Message)
	}
}

// TestGradeKeepsTheAddressThroughAWrappedError is the regression guard for the
// bug the addressing work was built to fix.
//
// encoding/json had already computed the offset and a %w threw it away. The
// grader sits at the point where that loss would become invisible again: if it
// used a type assertion instead of errors.As, a wrapped *scene.Error would
// grade as unaddressed, the corpus would record "refused without an address",
// and the natural reading of that result is that the engine regressed.
func TestGradeKeepsTheAddressThroughAWrappedError(t *testing.T) {
	inner := &scene.Error{
		Loc: scene.Loc{File: "wrapped.json", Line: 7, Col: 3},
		Msg: "unsigned bind \"model.current\"",
	}
	wrapped := fmt.Errorf("loading scene: %w", inner)

	v := Grade(nil, wrapped, KindBind)
	if !v.Addressed {
		t.Fatalf("a wrapped *scene.Error graded as unaddressed\n" +
			"consequence: the corpus would report that the engine refuses without a location — reading as an engine regression when it is the grader unwrapping badly — and the repair turn would lose the line it needs.\n" +
			"remedy: use errors.As, not a type assertion, so a wrapping verb between the caller and the error does not cost the position.")
	}
	if v.Line != 7 {
		t.Errorf("wrapped error graded at line %d, want 7\n"+
			"consequence: a misaddressed refusal sends the repair to a line that was already correct.\n"+
			"remedy: take the line from the unwrapped *scene.Error.", v.Line)
	}
}

// TestConvergedRejectsADocumentThatDeletedTheFeature holds the other end.
//
// Deleting the node an order asked for also makes a scene validate. A harness
// that scored "it loads" would record that deletion as a success and teach the
// next reader that the model can do a job it in fact refused to do.
func TestConvergedRejectsADocumentThatDeletedTheFeature(t *testing.T) {
	c := Case{
		ID:          "probe",
		Order:       "add a row showing which model is answering",
		Convergence: Convergence{MustBind: []string{"model.name"}},
	}

	// Valid, loadable, and it does not do the job.
	body := `{
  "root": {
    "type": "stack",
    "children": [
      { "type": "transcript", "bind": "chat.history" }
    ]
  }
}`
	doc, parseErr := parse(t, "deleted.json", body)

	converged, v, missing := c.Converged(doc, parseErr)
	if !v.Accepted {
		t.Fatalf("premise broken: the document must validate for this test to mean anything: %s\n"+
			"consequence: the test would pass for the wrong reason — rejected as invalid rather than rejected as incomplete.\n"+
			"remedy: use a document that genuinely loads.", v.Message)
	}
	if converged {
		t.Fatalf("Converged accepted a document that binds nothing the order asked for\n" +
			"consequence: the cheapest way to pass an eval is to delete the feature, and a harness that allows it reports a capability the model never demonstrated.\n" +
			"remedy: require every must_bind field to be bound.")
	}
	if len(missing) != 1 || missing[0] != "model.name" {
		t.Errorf("Converged reported missing=%v, want [model.name]\n"+
			"consequence: the runner cannot tell the model which field it dropped, so the repair turn is guesswork.\n"+
			"remedy: report exactly the unbound must_bind fields.", missing)
	}
}

// TestConvergedFindsABindInsideARowTemplate is the inverse failure, and the
// more insidious one: a harness that under-counts binds fails a correct answer.
//
// SCENES.md Q10 makes row_template the place relative binds live, so a list
// that binds its field per row is idiomatic, not exotic. A walk that skipped
// templates would mark the idiomatic answer as "deleted the feature" — and the
// result would read as a model failure on a document that is actually right.
func TestConvergedFindsABindInsideARowTemplate(t *testing.T) {
	c := Case{
		ID:          "probe-template",
		Order:       "list the todos",
		Convergence: Convergence{MustBind: []string{"agent.todos"}},
	}

	body := `{
  "root": {
    "type": "stack",
    "children": [
      {
        "type": "list",
        "row_template": { "type": "text", "bind": "agent.todos" }
      }
    ]
  }
}`
	doc, parseErr := parse(t, "template.json", body)

	converged, v, missing := c.Converged(doc, parseErr)
	if !v.Accepted {
		t.Fatalf("premise broken: document did not validate: %s", v.Message)
	}
	if !converged {
		t.Errorf("Converged rejected a document binding %q inside a row_template (missing=%v)\n"+
			"consequence: the walk misses the place SCENES.md Q10 says relative binds belong, so the idiomatic correct answer is scored as a failure to do the job — reported as a model deficiency on a document that is right.\n"+
			"remedy: CollectBinds must walk row_template, prefix and suffix as the validator does.",
			"agent.todos", missing)
	}
}

// TestMatchesDoesNotPunishARefusalForGainingAnAddress protects an improvement
// path.
//
// A case may pin a reason without a line (Line == 0), which means "I am not
// pinning the address here" — not "this refusal must stay unaddressed". If
// Matches treated the two as the same, then the day someone taught that
// refusal to carry a file:line, the corpus would fail and the fix would look
// like reverting the improvement.
func TestMatchesDoesNotPunishARefusalForGainingAnAddress(t *testing.T) {
	unpinned := Refusal{Reason: "unsigned bind"}

	addressed := Verdict{Message: "x.json:4:3: unsigned bind \"a.b\"", Line: 4, Addressed: true}
	if !unpinned.Matches(addressed) {
		t.Errorf("a case that pins no line rejected a refusal that carries one\n" +
			"consequence: adding an address to a diagnostic would break the corpus, so the cheapest fix would be to remove the address — the corpus would be voting against invariant 4.\n" +
			"remedy: Line == 0 means unpinned, not 'must be unaddressed'.")
	}

	// And an accepted document is never a match, however the reason reads.
	if unpinned.Matches(Verdict{Accepted: true}) {
		t.Errorf("an accepted document matched an expected refusal\n" +
			"consequence: a case would record a repair turn the engine never triggers, which is the imaginary-refusal failure the corpus exists to prevent.\n" +
			"remedy: Matches must return false when the verdict is Accepted.")
	}
}
