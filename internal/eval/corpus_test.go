package eval

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// corpusDir is the corpus, relative to this package.
func corpusDir() string { return filepath.Join("..", "..", "testdata", "eval") }

// TestCorpusLoads is the shape check. It runs first because every other test
// here is vacuous if the corpus does not parse.
func TestCorpusLoads(t *testing.T) {
	cases, err := LoadAll(corpusDir())
	if err != nil {
		t.Fatalf("LoadAll: %v\n"+
			"consequence: the corpus is Phase 2's gate; if it cannot load, the gate is not measuring anything.\n"+
			"remedy: fix the case file named in the error.", err)
	}
	t.Logf("%d corpus case(s) loaded", len(cases))
}

// TestEveryExpectedRefusalIsTheRefusalTheEngineGives is the reason this corpus
// is worth having before the model runner exists.
//
// Each attempt claims the engine refuses a document for a stated reason at a
// stated line. That claim is checkable today, against the real validator. Two
// things follow, and both are the point:
//
//  1. The corpus cannot be written from imagination. An attempt whose refusal
//     the engine does not actually produce fails here, so a case cannot encode
//     a repair loop the model would never be walked through. This test caught
//     exactly that on the first cases written: an invented "unknown node type"
//     refusal for a grid node, which the engine accepts by design (PLAN.md:
//     "unknown-but-parseable is a warning", so a v0 document keeps booting
//     under v1).
//  2. The corpus becomes a live test of the diagnostics themselves. If someone
//     changes a refusal's wording or moves its address, this fails — which
//     makes the change a review event rather than a silent regression in the
//     one signal Phase 2 depends on (PLAN.md: the corpus measures the repair
//     loop, and the refusal is the only input the model gets on the retry).
func TestEveryExpectedRefusalIsTheRefusalTheEngineGives(t *testing.T) {
	cases, err := LoadAll(corpusDir())
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	for _, c := range cases {
		t.Run(c.ID, func(t *testing.T) {
			for i, a := range c.Attempts {
				// The attempt is a document the engine must refuse. Parse
				// under the case's own name so the address in the failure
				// message points at something a reader can open.
				name := filepath.Base(c.Path)
				doc, parseErr := scene.ParseNamed(name, a.Document)

				got, line, addressed := refuse(doc, parseErr, a.Refused.Kind)

				if got == "" {
					t.Errorf("attempt %d (%s) was accepted, but the case says it must be refused with %q\n"+
						"consequence: the case claims a repair turn that does not exist, so it would score the model against a loop it is never put through — and a corpus of imaginary refusals measures nothing.\n"+
						"remedy: either correct the attempt document so it really is refused, or delete the attempt if the engine is right to accept it.",
						i, a.Note, a.Refused.Reason)
					continue
				}

				if !strings.Contains(got, a.Refused.Reason) {
					t.Errorf("attempt %d (%s) refusal does not match the case\n  engine: %s\n  case wants reason containing: %q\n"+
						"consequence: the corpus and the validator disagree about why this document is wrong. Either the case is fiction, or a diagnostic changed and the corpus is the thing that noticed.\n"+
						"remedy: if the engine's reason is the better one, update the case; if the case is right, the diagnostic regressed.",
						i, a.Note, got, a.Refused.Reason)
				}

				// The address is checked separately from the reason, because
				// the two rot independently: a message can stay perfect while
				// the line silently drifts to the document's first line.
				if a.Refused.Line > 0 {
					if !addressed {
						t.Errorf("attempt %d (%s) refused without an address: %s\n"+
							"consequence: invariant 4 requires file:line: on every refusal, and the repair loop is exactly the case where the model has to read it.\n"+
							"remedy: return an addressed refusal (*scene.Error, or a TokenError carrying its Loc).", i, a.Note, got)
						continue
					}
					if line != a.Refused.Line {
						t.Errorf("attempt %d (%s) refused at line %d, case says line %d\n  engine: %s\n"+
							"consequence: a misaddressed refusal sends the repair attempt to edit a line that was already correct — the corpus would then be scoring the model on a handicap the engine created.\n"+
							"remedy: if the engine's line is right, correct the case; if not, the address machinery drifted.",
							i, a.Note, line, a.Refused.Line, got)
					}
				}
			}
		})
	}
}

// TestEveryConvergenceDocumentActuallyConverges pins the other end. A case
// whose "correct" answer does not validate would make the case unpassable —
// the model could produce the right document and still be scored as a failure,
// which is the most expensive kind of corpus bug because it looks like a model
// deficiency in the results.
func TestEveryConvergenceDocumentActuallyConverges(t *testing.T) {
	cases, err := LoadAll(corpusDir())
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	for _, c := range cases {
		t.Run(c.ID, func(t *testing.T) {
			doc, err := scene.ParseNamed(filepath.Base(c.Path), c.Convergence.Document)
			if err != nil {
				t.Fatalf("convergence document does not parse: %v\n"+
					"consequence: the case's own answer is invalid, so the case can never be passed and its score is meaningless.\n"+
					"remedy: correct the convergence document.", err)
			}
			if err := doc.Validate(); err != nil {
				t.Fatalf("convergence document does not validate: %v\n"+
					"consequence: the case is unpassable — a model producing exactly this document would be scored as a failure, and the result would read as a model deficiency rather than a corpus bug.\n"+
					"remedy: correct the convergence document, or sign the bind it needs in docs/BINDS.md §4.", err)
			}
			// A converged document must also survive the token validator:
			// a case whose answer references a token the factory theme does
			// not define would be unpassable in the same way, just through
			// the other validator.
			if errs := scene.ValidateTokens(doc, theme.SOBRIA()); len(errs) > 0 {
				t.Fatalf("convergence document references an undefined token: %v\n"+
					"consequence: the case's own answer is refused by the token validator, so the case is unpassable.\n"+
					"remedy: use a token the factory theme defines, or mint it in the theme.", errs[0])
			}

			// Validating is not enough: the converged document must still do
			// what the order asked for. Deleting the offending node also
			// makes a scene validate.
			bound := make(map[string]bool)
			for _, b := range collectBinds(doc) {
				bound[b] = true
			}
			for _, want := range c.Convergence.MustBind {
				if !bound[want] {
					t.Errorf("convergence document does not bind %q\n  order: %s\n"+
						"consequence: the case would accept a document that satisfies the validator by removing the feature the order asked for, which is the cheapest way to pass an eval and learn nothing.\n"+
						"remedy: bind the field in the convergence document, or correct must_bind if the order does not actually require it.",
						want, c.Order)
				}
			}
		})
	}
}

// TestCorpusExercisesTheRepairLoopNotTheFirstShot is the corpus' self-audit.
// PLAN.md is explicit that first-shot accuracy is a vanity metric and the
// measurement of record is the repair loop. A corpus of cases with no refusal
// path would satisfy every other test in this file and measure precisely the
// metric the plan rejects, so the corpus has to be held to its own purpose.
func TestCorpusExercisesTheRepairLoopNotTheFirstShot(t *testing.T) {
	cases, err := LoadAll(corpusDir())
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	withRepair := 0
	kinds := make(map[string]int)
	for _, c := range cases {
		if len(c.Attempts) > 0 {
			withRepair++
		} else {
			t.Logf("case %q has no refused attempt: it measures the first shot only", c.ID)
		}
		for _, a := range c.Attempts {
			kinds[a.Refused.Kind]++
		}
	}
	if withRepair == 0 {
		t.Errorf("no corpus case exercises a refusal\n" +
			"consequence: the corpus would measure first-shot accuracy, which PLAN.md names a vanity metric — \"the real use is exactly the case where the engine said file:line: and the model had to read it\".\n" +
			"remedy: every case should carry the refusals its order plausibly provokes.")
	}
	// Both validators should be represented. They fail independently, and a
	// corpus blind to one of them would let that one's diagnostics rot.
	if kinds[KindToken] == 0 {
		t.Errorf("no corpus case exercises a token refusal\n" +
			"consequence: the token validator reports through a different type (TokenError) and would have no corpus coverage, while being the most common scene error there is because the token vocabulary is open by design.\n" +
			"remedy: add a case whose order provokes an invented token name.")
	}
	t.Logf("%d of %d cases exercise at least one repair turn; refusals by kind: bind=%d token=%d",
		withRepair, len(cases), kinds[KindBind], kinds[KindToken])
}

// refuse runs the validator the attempt names and reports what it said: the
// message, the line it addressed, and whether it carried an address at all.
// An empty message means the engine accepted the document.
//
// The two validators are dispatched here rather than at the call site because
// they differ in shape as well as type — Validate returns one error and stops,
// ValidateTokens collects every offender — and the corpus cares about the same
// three facts either way.
func refuse(doc *scene.Document, parseErr error, kind string) (msg string, line int, addressed bool) {
	if parseErr != nil {
		// A document that does not parse never reaches either validator, so
		// the parse refusal is the answer regardless of the declared kind.
		var sceneErr *scene.Error
		if asSceneError(parseErr, &sceneErr) {
			return sceneErr.Error(), sceneErr.Loc.Line, sceneErr.Loc.Line > 0
		}
		return parseErr.Error(), 0, false
	}

	switch kind {
	case KindToken:
		errs := scene.ValidateTokens(doc, theme.SOBRIA())
		if len(errs) == 0 {
			return "", 0, false
		}
		return errs[0].Error(), errs[0].Loc.Line, errs[0].Loc.Line > 0
	default:
		err := doc.Validate()
		if err == nil {
			return "", 0, false
		}
		var sceneErr *scene.Error
		if asSceneError(err, &sceneErr) {
			return sceneErr.Error(), sceneErr.Loc.Line, sceneErr.Loc.Line > 0
		}
		return err.Error(), 0, false
	}
}

// collectBinds returns every bind path a document addresses. It walks the same
// arms the validator does — children, prefix, suffix, row_template — because a
// must_bind field satisfied only inside a row template is still satisfied, and
// SCENES.md Q10 makes templates the place relative binds live.
//
// It is a local walker rather than an export from internal/scene on purpose:
// the corpus is a consumer of the scene package, and widening that package's
// API for a test's convenience is how an internal detail becomes a contract
// nobody meant to sign.
func collectBinds(doc *scene.Document) []string {
	var out []string
	var walk func(n *scene.Node)
	walk = func(n *scene.Node) {
		if n == nil {
			return
		}
		if n.Bind != "" {
			out = append(out, n.Bind)
		}
		if n.When != "" {
			if parts := strings.Fields(n.When); len(parts) > 0 && parts[0] != "" {
				out = append(out, parts[0])
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
		walk(n.PrefixNode())
		walk(n.Suffix)
		walk(n.RowTemplate)
	}
	walk(doc.Root)
	return out
}

// asSceneError is errors.As specialised to *scene.Error, kept as a helper so
// the intent reads at the call site.
func asSceneError(err error, target **scene.Error) bool {
	for err != nil {
		if se, ok := err.(*scene.Error); ok {
			*target = se
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
