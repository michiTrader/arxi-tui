package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// The model runner: the half of the corpus that needs a model.
//
// What it measures is fixed by PLAN.md and restated in EVAL.md — the repair
// loop, not the first shot: "order → patch → validator error → retry, until
// convergence; first-shot accuracy is a vanity metric, because the real use is
// exactly the case where the engine said file:line: and the model had to read
// it."
//
// Everything model-specific is behind Model. The loop, the scoring and the
// stopping rules are ordinary code with no network in them, which is what lets
// the interesting behaviour — convergence, turn counting, looping, a model
// that returns prose instead of JSON — be proven by scripted models in tests
// rather than asserted and hoped for.

// Model is one patch attempt. Implementations are adapters; the loop is here.
//
// Patch receives the order, the scene it starts from, and the transcript of
// what has already been refused. It returns a scene document as raw bytes:
// bytes rather than a *scene.Document on purpose, because a model that emits
// malformed JSON is a case the repair loop must handle and score, not a case
// the type system should hide. The parse failure is a refusal like any other,
// and it is the one that most needs an address.
type Model interface {
	Patch(ctx context.Context, req PatchRequest) ([]byte, error)
}

// PatchRequest is everything a model is told on one turn.
type PatchRequest struct {
	// Order is the user's words, unedited.
	Order string
	// Base is the scene document the order is applied to.
	Base []byte
	// BaseName is the base scene's filename, for addressing.
	BaseName string
	// SignedBinds is the vocabulary the validator will accept. It comes
	// from scene.SignedBinds() rather than a list written here, so the
	// prompt cannot drift from the thing that refuses patches.
	SignedBinds []string
	// Tokens is the style vocabulary the active theme defines.
	Tokens []string
	// History is every previous turn of this case, oldest first. On the
	// first turn it is empty. This is the input the whole exercise is
	// about: PLAN.md's measurement is whether the model reads the
	// file:line it was given and repairs, so the refusal text has to reach
	// the model intact.
	History []Turn
}

// Turn is one completed exchange: what the model produced and what the engine
// said about it.
type Turn struct {
	// Document is exactly the bytes the model returned, not a
	// re-serialisation. A model that emitted invalid JSON must see its own
	// invalid JSON on the retry, or it is being asked to repair a document
	// it never wrote.
	Document []byte
	// Verdict is the engine's answer.
	Verdict Verdict
	// ModelErr is set when the model itself failed (transport, refusal to
	// answer). It is kept distinct from a refusal: a model that errored
	// was never graded, and counting that as a failed repair would blame
	// the model for the harness' network.
	ModelErr error
}

// Outcome is why a case's loop stopped. Each value is a distinct conclusion a
// reader may draw, which is why "did not converge" is not one value.
type Outcome string

const (
	// OutcomeConverged: the model produced a document that validates and
	// binds everything must_bind names.
	OutcomeConverged Outcome = "converged"
	// OutcomeExhausted: still refused when the turn budget ran out. The
	// model was making progress, or at least making different mistakes.
	OutcomeExhausted Outcome = "exhausted"
	// OutcomeLooped: the model returned a document it had already been
	// refused for. EVAL.md counts this as not converged and names the
	// reason: "producing the same refused document twice is a failure to
	// read the address, which is precisely what the file:line: work exists
	// to make possible". It is kept apart from exhaustion because the two
	// call for opposite responses — a looping model will not be fixed by a
	// larger budget, and reporting them together would hide that.
	OutcomeLooped Outcome = "looped"
	// OutcomeModelError: the model never produced a gradeable answer. This
	// is a harness/transport result, not a score, and it is separate so a
	// flaky network cannot quietly depress a model's measured ability.
	OutcomeModelError Outcome = "model_error"
	// OutcomeIncomplete: the document validates but does not do the job —
	// must_bind is unsatisfied and there is nothing to repair from.
	// Distinct from exhausted because the failure is the opposite kind:
	// the engine was satisfied and the order was not, so no validator
	// message exists to feed a retry. That is a prompt or corpus problem,
	// not a repair-loop problem, and merging it into "exhausted" would
	// point the reader at the wrong fix.
	OutcomeIncomplete Outcome = "incomplete"
)

// Result is one case's score.
type Result struct {
	CaseID  string
	Order   string
	Outcome Outcome
	// Turns is how many times the model was asked. EVAL.md's metric of
	// record alongside converged/not: "One turn means the first patch
	// validated; the interesting cases take two or three."
	Turns int
	// History is every turn, for a report that can show the repair path
	// rather than just its length.
	History []Turn
	// MissingBinds is what an otherwise-valid final document failed to
	// bind. Populated for OutcomeIncomplete.
	MissingBinds []string
	// Err is a harness failure (the model transport), never a score.
	Err error
}

// Converged is the boolean half of the score.
func (r Result) Converged() bool { return r.Outcome == OutcomeConverged }

// Options configures a run.
type Options struct {
	// MaxTurns bounds the repair loop. Zero means DefaultMaxTurns.
	MaxTurns int
	// BaseDir is where base scenes named by cases are read from.
	BaseDir string
}

// DefaultMaxTurns is the budget when Options does not set one.
//
// Four is chosen against EVAL.md's own description of the measurement — "the
// interesting cases take two or three" — with one turn of headroom so that a
// case needing three is scored as converged rather than truncated at the
// interesting moment. It is deliberately not large: the metric is
// turns-to-convergence, and a generous budget converts a model that cannot
// read addresses into one that eventually stumbles onto the answer, which is
// the failure mode PLAN.md is trying to detect.
const DefaultMaxTurns = 4

// Run executes the repair loop for one case and scores it.
//
// The loop is: ask, grade, and either stop or hand the refusal back. What it
// must never do is grade against the validator the case named — the case's
// attempts describe what a human predicted the model would get wrong, and
// scoring the model's real document against that prediction would measure
// agreement with the case author. GradeBoth asks the engine instead.
func Run(ctx context.Context, m Model, c Case, opt Options) Result {
	maxTurns := opt.MaxTurns
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns
	}

	res := Result{CaseID: c.ID, Order: c.Order}

	base, err := readBase(opt.BaseDir, c.Base)
	if err != nil {
		res.Outcome = OutcomeModelError
		res.Err = err
		return res
	}

	req := PatchRequest{
		Order:       c.Order,
		Base:        base,
		BaseName:    c.Base,
		SignedBinds: scene.SignedBinds(),
		Tokens:      corpusTheme().Tokens(),
	}

	// seen fingerprints every refused document so a repeat is detected on
	// the turn it happens rather than after the budget drains. Normalised,
	// so that reformatting the same wrong answer still counts as a repeat:
	// the model has not read the address either way.
	seen := make(map[string]bool, maxTurns)

	for res.Turns < maxTurns {
		if err := ctx.Err(); err != nil {
			res.Outcome = OutcomeModelError
			res.Err = err
			return res
		}

		req.History = res.History
		res.Turns++

		body, err := m.Patch(ctx, req)
		if err != nil {
			res.History = append(res.History, Turn{ModelErr: err})
			res.Outcome = OutcomeModelError
			res.Err = fmt.Errorf("turn %d: %w", res.Turns, err)
			return res
		}

		// Parse under the base scene's name so the address the model reads
		// points at something that exists.
		doc, parseErr := scene.ParseNamed(c.Base, body)
		converged, verdict, missing := c.Converged(doc, parseErr)

		res.History = append(res.History, Turn{Document: body, Verdict: verdict})

		if converged {
			res.Outcome = OutcomeConverged
			return res
		}

		if verdict.Accepted {
			// Valid but incomplete. There is no validator message to
			// repair from, so another turn would re-put an identical
			// question; the loop stops and says which fields are
			// missing instead of burning the budget.
			res.Outcome = OutcomeIncomplete
			res.MissingBinds = missing
			return res
		}

		fp := fingerprint(body)
		if seen[fp] {
			res.Outcome = OutcomeLooped
			return res
		}
		seen[fp] = true
	}

	res.Outcome = OutcomeExhausted
	return res
}

// RunAll scores every case in order.
//
// Sequential rather than parallel, and that is a measurement decision: these
// runs are the numbers a decision about shipping /ui gets made on, so a
// reproducible order costs little and removes a class of doubt. Parallelism
// belongs behind an explicit option if a corpus ever grows large enough to
// need it.
func RunAll(ctx context.Context, m Model, cases []Case, opt Options) []Result {
	out := make([]Result, 0, len(cases))
	for _, c := range cases {
		out = append(out, Run(ctx, m, c, opt))
	}
	return out
}

// Summary aggregates results for a report.
type Summary struct {
	Total     int
	Converged int
	// TurnsHistogram maps turns-to-convergence to how many cases took that
	// many. Only converged cases appear: averaging in the failures would
	// produce a single number that moves for two opposite reasons.
	TurnsHistogram map[int]int
	Outcomes       map[Outcome]int
	// FirstShot is how many converged on turn one. Reported because
	// EVAL.md calls a case that always converges in one turn a weak case —
	// this is the number that identifies them, not a headline metric.
	FirstShot int
}

// Summarize aggregates a run.
func Summarize(results []Result) Summary {
	s := Summary{
		Total:          len(results),
		TurnsHistogram: make(map[int]int),
		Outcomes:       make(map[Outcome]int),
	}
	for _, r := range results {
		s.Outcomes[r.Outcome]++
		if r.Converged() {
			s.Converged++
			s.TurnsHistogram[r.Turns]++
			if r.Turns == 1 {
				s.FirstShot++
			}
		}
	}
	return s
}

// String renders a summary for a terminal report.
//
// It deliberately leads with the outcome breakdown rather than a pass rate. A
// single percentage invites a threshold, and PLAN.md's instruction if the
// model cannot do this reliably is not "tune the threshold" — it is that the
// document must say so.
func (s Summary) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d/%d converged", s.Converged, s.Total)
	if s.Converged > 0 {
		fmt.Fprintf(&b, " (turns:")
		for turns := 1; turns <= len(s.TurnsHistogram)+DefaultMaxTurns; turns++ {
			if n := s.TurnsHistogram[turns]; n > 0 {
				fmt.Fprintf(&b, " %dx%d", n, turns)
			}
		}
		fmt.Fprintf(&b, ")")
	}
	for _, o := range []Outcome{OutcomeExhausted, OutcomeLooped, OutcomeIncomplete, OutcomeModelError} {
		if n := s.Outcomes[o]; n > 0 {
			fmt.Fprintf(&b, ", %s=%d", o, n)
		}
	}
	return b.String()
}

// fingerprint normalises a document for repeat detection.
//
// Normalising through a JSON round-trip rather than hashing the raw bytes is
// the point: a model that returns the same wrong document with different
// indentation has still failed to read the address, and a byte hash would
// score that as progress. Unparseable bytes are hashed as-is, because a model
// repeating the same syntax error is the clearest loop there is.
func fingerprint(body []byte) string {
	var v any
	if err := json.Unmarshal(body, &v); err == nil {
		if canonical, err := json.Marshal(v); err == nil {
			body = canonical
		}
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// readBase loads the base scene a case names.
//
// The error names the case's own field, because the likeliest cause is a
// corpus typo and a bare "no such file" would send the reader to the wrong
// place.
func readBase(dir, name string) ([]byte, error) {
	if name == "" {
		return nil, errors.New("case names no base scene")
	}
	if dir == "" {
		dir = filepath.Join("..", "..", "testdata")
	}
	path := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("base scene %q: %w", name, err)
	}
	return data, nil
}
