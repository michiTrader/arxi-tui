package eval

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The runner is tested with scripted models, never a real one.
//
// This is not a convenience. The behaviour worth proving here is the loop's:
// that it stops on convergence, counts turns honestly, notices a model going
// in circles, and distinguishes a document the engine rejects from one the
// engine accepts that does not do the job. Each of those needs a model that
// behaves a specific way on a specific turn, which a real model cannot be
// asked to do reliably — and a suite that needed the network would be a suite
// nobody runs before pushing.
//
// It also keeps the measurement honest in the other direction: everything
// here exercises the real grader against the real validator. The only thing
// faked is the model.

// scriptedModel replays a fixed list of answers, one per turn.
type scriptedModel struct {
	answers [][]byte
	err     error
	// seenHistory records the history length the model was handed on each
	// turn, so a test can prove the refusal actually reaches the model
	// rather than trusting that it does.
	seenHistory []int
	// lastRequest keeps the final request for inspection.
	lastRequest PatchRequest
}

func (m *scriptedModel) Patch(_ context.Context, req PatchRequest) ([]byte, error) {
	m.seenHistory = append(m.seenHistory, len(req.History))
	m.lastRequest = req
	if m.err != nil {
		return nil, m.err
	}
	i := len(m.seenHistory) - 1
	if i >= len(m.answers) {
		// Running past the script means the loop asked for more turns
		// than the test intended. Returning a distinctive valid-but-wrong
		// document rather than repeating the last answer keeps that fault
		// from disguising itself as a loop.
		return []byte(`{"root":{"type":"stack","children":[{"type":"text","text":"script exhausted"}]}}`), nil
	}
	return m.answers[i], nil
}

// Documents used by the scripts below. They are written out rather than
// generated so a reader can see exactly what the engine is being asked about.
const (
	// docUnsignedBind is refused by the bind validator.
	docUnsignedBind = `{
  "root": {
    "type": "stack",
    "children": [
      { "type": "transcript", "bind": "chat.history" },
      { "type": "text", "bind": "model.current" }
    ]
  }
}`

	// docUndefinedToken has only signed binds but an invented token.
	docUndefinedToken = `{
  "root": {
    "type": "stack",
    "children": [
      { "type": "transcript", "bind": "chat.history" },
      { "type": "text", "bind": "model.name", "style": { "token": "grey" } }
    ]
  }
}`

	// docGood validates and binds model.name.
	docGood = `{
  "root": {
    "type": "stack",
    "children": [
      { "type": "transcript", "bind": "chat.history" },
      { "type": "text", "bind": "model.name", "style": { "token": "dim" } }
    ]
  }
}`

	// docValidButEmpty loads cleanly and does not do the job.
	docValidButEmpty = `{
  "root": {
    "type": "stack",
    "children": [
      { "type": "transcript", "bind": "chat.history" }
    ]
  }
}`

	// docBrokenJSON never reaches a validator.
	docBrokenJSON = `{ "root": { "type": "stack", "children": [ } }`
)

// probeCase is a case with the shape the runner needs, independent of the
// corpus on disk: these tests are about the loop, and coupling them to corpus
// content would make them fail for unrelated reasons whenever a case is added.
func probeCase() Case {
	return Case{
		ID:          "probe",
		Order:       "add a row under the input showing which model is answering",
		Base:        "SOARIA.json",
		Rationale:   "runner test",
		Convergence: Convergence{MustBind: []string{"model.name"}},
	}
}

func runOpts() Options {
	return Options{BaseDir: "../../testdata"}
}

// TestRunnerConvergesAndCountsTheRepairTurns is the measurement of record.
//
// Two refusals then a correct document must score as converged in three turns.
// The turn count is the number EVAL.md reports alongside converged/not, so an
// off-by-one here would misreport every case in the corpus — and in the
// direction that flatters the model, since the natural mistake is to count
// only the successful turn.
func TestRunnerConvergesAndCountsTheRepairTurns(t *testing.T) {
	m := &scriptedModel{answers: [][]byte{
		[]byte(docUnsignedBind),
		[]byte(docUndefinedToken),
		[]byte(docGood),
	}}

	res := Run(context.Background(), m, probeCase(), runOpts())

	if !res.Converged() {
		t.Fatalf("outcome %q, want converged (err=%v)\n"+
			"consequence: a loop that cannot recognise a correct document scores every model as failing, and the corpus would report that /ui is unshippable on the harness' own bug.\n"+
			"remedy: Converged must accept a document that validates and binds must_bind.", res.Outcome, res.Err)
	}
	if res.Turns != 3 {
		t.Errorf("turns=%d, want 3\n"+
			"consequence: turns-to-convergence is the metric of record next to converged/not; miscounting it misreports every case, and the natural error flatters the model by counting only the winning turn.\n"+
			"remedy: count every request made to the model, including the refused ones.", res.Turns)
	}
	if len(res.History) != 3 {
		t.Errorf("history has %d turns, want 3\n"+
			"consequence: the report cannot show the repair path it claims to measure.\n"+
			"remedy: append every turn, refused ones included.", len(res.History))
	}
}

// TestTheRefusalActuallyReachesTheModel is the test this whole phase is for.
//
// PLAN.md: the corpus measures "order → patch → validator error → retry", and
// the validator error is the only new information the model gets on the retry.
// A loop that re-asked with an empty history would still converge whenever the
// model guessed right on a later turn, and every score would silently be
// measuring guesswork instead of repair — while the suite stayed green.
//
// So this asserts the plumbing directly: that the second request carries the
// first refusal, with its address, in the model's own hands.
func TestTheRefusalActuallyReachesTheModel(t *testing.T) {
	m := &scriptedModel{answers: [][]byte{
		[]byte(docUnsignedBind),
		[]byte(docGood),
	}}

	res := Run(context.Background(), m, probeCase(), runOpts())
	if !res.Converged() {
		t.Fatalf("setup: expected convergence, got %q", res.Outcome)
	}

	if len(m.seenHistory) < 2 {
		t.Fatalf("model was asked %d time(s), want at least 2", len(m.seenHistory))
	}
	if m.seenHistory[0] != 0 {
		t.Errorf("first turn was handed %d history entries, want 0\n"+
			"consequence: the model would be shown a refusal for a document it has not written yet.\n"+
			"remedy: the first request carries no history.", m.seenHistory[0])
	}
	if m.seenHistory[1] != 1 {
		t.Fatalf("second turn was handed %d history entries, want 1\n"+
			"consequence: the retry would not see the validator error, so the corpus would measure the model's guessing rather than its repair — the exact metric PLAN.md rejects — and it would do so invisibly, because guessing right still converges.\n"+
			"remedy: pass the accumulated turns on every request after the first.", m.seenHistory[1])
	}

	prior := m.lastRequest.History[0]
	if prior.Verdict.Accepted {
		t.Fatalf("the history entry handed back records an accepted document")
	}
	if !strings.Contains(prior.Verdict.Message, "model.current") {
		t.Errorf("refusal handed to the model does not name the offending bind: %q\n"+
			"consequence: the model is told something failed but not what, so the retry is a guess.\n"+
			"remedy: pass the engine's message through unedited.", prior.Verdict.Message)
	}
	if !prior.Verdict.Addressed || prior.Verdict.Line <= 0 {
		t.Errorf("refusal handed to the model carries no address (line=%d)\n"+
			"consequence: invariant 4 exists so the repair turn has a line to read; without it this corpus measures address-guessing.\n"+
			"remedy: preserve Loc through the grader into the turn history.", prior.Verdict.Line)
	}
	if string(prior.Document) != docUnsignedBind {
		t.Errorf("the model was not shown the exact document it produced\n"+
			"consequence: it is being asked to repair a document it never wrote, and the line numbers in the refusal would address different text than the model is looking at.\n"+
			"remedy: keep the exact bytes the model returned in the turn, not a re-serialisation.\n  got: %q", string(prior.Document))
	}
}

// TestALoopingModelIsNotJustExhaustion separates two results that call for
// opposite responses.
//
// EVAL.md: "producing the same refused document twice is a failure to read the
// address, which is precisely what the file:line: work exists to make
// possible". Reporting that as exhaustion would suggest a bigger turn budget
// as the fix, when a model going in circles will use the extra turns to go in
// more circles.
func TestALoopingModelIsNotJustExhaustion(t *testing.T) {
	m := &scriptedModel{answers: [][]byte{
		[]byte(docUnsignedBind),
		[]byte(docUnsignedBind),
	}}

	res := Run(context.Background(), m, probeCase(), runOpts())

	if res.Outcome != OutcomeLooped {
		t.Errorf("outcome %q, want %q\n"+
			"consequence: a model ignoring the address reads as one that merely ran out of turns, and the suggested fix becomes a larger budget — which cannot help, and hides the finding the file:line work exists to surface.\n"+
			"remedy: fingerprint refused documents and stop on a repeat.", res.Outcome, OutcomeLooped)
	}
	if res.Turns != 2 {
		t.Errorf("turns=%d, want 2: the loop should stop on the repeat, not drain the budget", res.Turns)
	}
}

// TestAReformattedRepeatStillCountsAsALoop closes the obvious hole in the one
// above.
//
// A model that returns the same wrong document with different whitespace has
// not read the address either. Hashing raw bytes would score that as a fresh
// attempt, and the loop detector would only ever catch models that repeat
// themselves byte-for-byte — which is the easy case, and not the one that
// happens.
func TestAReformattedRepeatStillCountsAsALoop(t *testing.T) {
	compact := []byte(`{"root":{"type":"stack","children":[{"type":"transcript","bind":"chat.history"},{"type":"text","bind":"model.current"}]}}`)

	m := &scriptedModel{answers: [][]byte{
		[]byte(docUnsignedBind),
		compact,
	}}

	res := Run(context.Background(), m, probeCase(), runOpts())

	if res.Outcome != OutcomeLooped {
		t.Errorf("outcome %q, want %q for a semantically identical repeat\n"+
			"consequence: loop detection would only catch byte-identical repeats, so any model that reformats its output appears to be making progress while repeating the same mistake.\n"+
			"remedy: fingerprint the canonicalised document, not the raw bytes.", res.Outcome, OutcomeLooped)
	}
}

// TestAValidDocumentThatDoesNotDoTheJobIsItsOwnOutcome keeps two different
// failures apart.
//
// A document that validates but does not bind what the order asked for has no
// validator message to repair from — asking again would re-put an identical
// question. Calling that "exhausted" points the reader at the turn budget,
// when the real finding is that the model satisfied the engine and not the
// user.
func TestAValidDocumentThatDoesNotDoTheJobIsItsOwnOutcome(t *testing.T) {
	m := &scriptedModel{answers: [][]byte{[]byte(docValidButEmpty)}}

	res := Run(context.Background(), m, probeCase(), runOpts())

	if res.Outcome != OutcomeIncomplete {
		t.Fatalf("outcome %q, want %q\n"+
			"consequence: the cheapest way to satisfy a validator is to delete the feature; if that reads as exhaustion, the reader is pointed at the turn budget instead of at a model that answered a different question.\n"+
			"remedy: score an accepted-but-unbound document as incomplete.", res.Outcome, OutcomeIncomplete)
	}
	if res.Turns != 1 {
		t.Errorf("turns=%d, want 1: there is no refusal to repair from, so the loop must not spend more turns", res.Turns)
	}
	if len(res.MissingBinds) != 1 || res.MissingBinds[0] != "model.name" {
		t.Errorf("MissingBinds=%v, want [model.name]\n"+
			"consequence: the report cannot say which part of the order went unanswered.\n"+
			"remedy: carry the unbound must_bind fields into the result.", res.MissingBinds)
	}
}

// TestMalformedJSONIsGradedNotCrashed proves the runner survives the answer a
// model most often gives badly, and that the refusal it produces is one a
// model could actually repair from.
//
// This is why Model returns bytes rather than a parsed document: making the
// type system reject unparseable output would hide the single most common
// model failure from the measurement instead of scoring it.
func TestMalformedJSONIsGradedNotCrashed(t *testing.T) {
	m := &scriptedModel{answers: [][]byte{
		[]byte(docBrokenJSON),
		[]byte(docGood),
	}}

	res := Run(context.Background(), m, probeCase(), runOpts())

	if !res.Converged() {
		t.Fatalf("outcome %q, want converged after recovering from bad JSON (err=%v)", res.Outcome, res.Err)
	}
	first := res.History[0].Verdict
	if first.Accepted {
		t.Fatalf("malformed JSON was accepted")
	}
	if !first.Addressed || first.Line <= 0 {
		t.Errorf("syntax refusal carried no address: %q (line=%d)\n"+
			"consequence: encoding/json already computed the offset; dropping it leaves the model to find a brace by eye, and invariant 4 calls an unaddressed refusal a bug.\n"+
			"remedy: keep the offset through the parse path into the verdict.", first.Message, first.Line)
	}
}

// TestAModelTransportFailureIsNotAModelScore keeps the harness' problems out
// of the model's results.
//
// A timeout is not evidence about whether a model can patch a scene. Scoring
// it as a failed case would let a flaky network depress the number a shipping
// decision is made on, and the resulting report would be indistinguishable
// from a genuine model deficiency.
func TestAModelTransportFailureIsNotAModelScore(t *testing.T) {
	m := &scriptedModel{err: errors.New("connection reset")}

	res := Run(context.Background(), m, probeCase(), runOpts())

	if res.Outcome != OutcomeModelError {
		t.Errorf("outcome %q, want %q\n"+
			"consequence: transport noise would be recorded as the model failing the case, so a flaky network could argue /ui is unshippable.\n"+
			"remedy: report a model error as its own outcome, never as a refusal.", res.Outcome, OutcomeModelError)
	}
	if res.Converged() {
		t.Errorf("a case that never produced a document was scored as converged")
	}
	if res.Err == nil {
		t.Errorf("model error outcome carries no error to report")
	}
}

// TestACancelledContextStopsTheLoop protects the operator's ability to stop a
// run that is costing money against a paid API.
func TestACancelledContextStopsTheLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := &scriptedModel{answers: [][]byte{[]byte(docGood)}}
	res := Run(ctx, m, probeCase(), runOpts())

	if res.Outcome != OutcomeModelError {
		t.Errorf("outcome %q, want %q on a cancelled context\n"+
			"consequence: a cancelled run would keep calling a paid API, and its partial results would be reported as scores.\n"+
			"remedy: check ctx.Err() before each turn.", res.Outcome, OutcomeModelError)
	}
	if len(m.seenHistory) != 0 {
		t.Errorf("the model was called %d time(s) after cancellation", len(m.seenHistory))
	}
}

// TestTheTurnBudgetIsEnforced stops a non-converging model running forever
// against a paid API.
func TestTheTurnBudgetIsEnforced(t *testing.T) {
	// Distinct refused documents: never converges, never repeats, so only
	// the budget can stop it.
	answers := [][]byte{
		[]byte(docUnsignedBind),
		[]byte(docUndefinedToken),
		[]byte(`{"root":{"type":"stack","children":[{"type":"text","bind":"model.missing"}]}}`),
		[]byte(`{"root":{"type":"stack","children":[{"type":"text","bind":"model.absent"}]}}`),
		[]byte(docGood),
	}
	m := &scriptedModel{answers: answers}

	res := Run(context.Background(), m, probeCase(), Options{BaseDir: "../../testdata", MaxTurns: 2})

	if res.Outcome != OutcomeExhausted {
		t.Errorf("outcome %q, want %q", res.Outcome, OutcomeExhausted)
	}
	if res.Turns != 2 {
		t.Errorf("turns=%d, want 2: the budget must bound the loop\n"+
			"consequence: an unbounded loop against a paid API is a cost incident, and against a looping model it never terminates.\n"+
			"remedy: stop at MaxTurns.", res.Turns)
	}
	if len(m.seenHistory) != 2 {
		t.Errorf("the model was asked %d time(s) under a 2-turn budget", len(m.seenHistory))
	}
}

// TestTheModelIsToldTheVocabularyTheValidatorEnforces closes the loop with the
// exported inventory.
//
// The runner builds the prompt's bind list from scene.SignedBinds(). If it
// ever built it from a literal instead, the drift would be invisible here
// unless something asserts the link — and the failure mode is a model told
// that signed vocabulary does not exist, scored as the model's fault.
func TestTheModelIsToldTheVocabularyTheValidatorEnforces(t *testing.T) {
	m := &scriptedModel{answers: [][]byte{[]byte(docGood)}}
	_ = Run(context.Background(), m, probeCase(), runOpts())

	if len(m.lastRequest.SignedBinds) == 0 {
		t.Fatalf("the model was given no bind vocabulary\n" +
			"consequence: the first turn of every case is spent guessing which binds exist, so the corpus measures recall of an undocumented list instead of the repair loop.\n" +
			"remedy: populate PatchRequest.SignedBinds from scene.SignedBinds().")
	}
	has := func(list []string, want string) bool {
		for _, v := range list {
			if v == want {
				return true
			}
		}
		return false
	}
	if !has(m.lastRequest.SignedBinds, "model.name") {
		t.Errorf("signed bind %q missing from the model's vocabulary: %v\n"+
			"consequence: the model avoids vocabulary the product documents and the corpus records that as a model failure rather than a prompt defect.\n"+
			"remedy: source the list from the validator's own inventory.", "model.name", m.lastRequest.SignedBinds)
	}
	if has(m.lastRequest.SignedBinds, "model.current") {
		t.Errorf("unsigned bind %q offered to the model\n"+
			"consequence: the runner would invite a patch the engine refuses at load time, producing refusals no case predicted.\n"+
			"remedy: the list must be exactly the validator's inventory.", "model.current")
	}
	if len(m.lastRequest.Tokens) == 0 {
		t.Errorf("the model was given no token vocabulary\n" +
			"consequence: token names are the most common scene error because the namespace is open; without the list, every styling order starts with a guess.\n" +
			"remedy: populate PatchRequest.Tokens from the corpus theme.")
	}
	if !has(m.lastRequest.Tokens, "dim") {
		t.Errorf("token %q missing from the model's vocabulary: %v", "dim", m.lastRequest.Tokens)
	}
	// The base scene must reach the model too: an order like "dim the
	// footer" is meaningless without the document it refers to.
	if len(m.lastRequest.Base) == 0 {
		t.Errorf("the model was given no base scene\n" +
			"consequence: every order refers to a scene the model cannot see, so it would be inventing documents rather than patching one.\n" +
			"remedy: read the case's base scene and pass it in the request.")
	}
}

// TestSummarizeSeparatesConvergenceFromItsCauses guards the report.
//
// A summary that collapsed outcomes into a pass rate would hide the
// distinction the runner works to preserve — and EVAL.md deliberately sets no
// threshold, because PLAN.md's instruction when the model cannot do this is
// not "tune the threshold" but "the document must say so".
func TestSummarizeSeparatesConvergenceFromItsCauses(t *testing.T) {
	results := []Result{
		{CaseID: "a", Outcome: OutcomeConverged, Turns: 1},
		{CaseID: "b", Outcome: OutcomeConverged, Turns: 3},
		{CaseID: "c", Outcome: OutcomeLooped, Turns: 2},
		{CaseID: "d", Outcome: OutcomeIncomplete, Turns: 1},
		{CaseID: "e", Outcome: OutcomeModelError, Turns: 1},
	}

	s := Summarize(results)

	if s.Total != 5 || s.Converged != 2 {
		t.Errorf("Total=%d Converged=%d, want 5 and 2", s.Total, s.Converged)
	}
	if s.FirstShot != 1 {
		t.Errorf("FirstShot=%d, want 1\n"+
			"consequence: EVAL.md calls a case that always converges in one turn a weak case; without this count they cannot be identified and the corpus drifts toward measuring the vanity metric.\n"+
			"remedy: count converged cases with Turns == 1.", s.FirstShot)
	}
	if s.TurnsHistogram[3] != 1 {
		t.Errorf("histogram missing the 3-turn convergence: %v", s.TurnsHistogram)
	}
	// A model error must never be counted as a case the model failed.
	if s.Outcomes[OutcomeModelError] != 1 {
		t.Errorf("model errors not reported separately: %v", s.Outcomes)
	}
	got := s.String()
	for _, want := range []string{"2/5 converged", "looped=1", "incomplete=1", "model_error=1"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary line %q does not report %q\n"+
				"consequence: a bare pass rate invites a threshold, and the interesting finding — how it failed — is the part that decides whether /ui ships.\n"+
				"remedy: render every non-converged outcome alongside the count.", got, want)
		}
	}
}
