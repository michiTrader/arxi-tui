package engine

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/eval"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// The limit EVAL.md wrote down, closed on the side that can actually see it.
//
// The corpus grades documents and never renders one. That asymmetry is not a
// detail of where the code sits — it is the reason the fourth instance of the
// checked-but-never-drawn class reached the measuring instrument itself:
// maximum-count-the-tasks listed `todos.count` in must_bind, its own recorded
// convergence document bound it, and the grader reported converged while the
// panel drew "[…]". Every layer agreed, and the agreement was about the
// document, never about the screen.
//
// `Converged` asks whether a bind is *present* in the tree. That is the right
// question for the grader to ask — it is judging a document a model wrote, and
// it must not need a renderer to do it. But presence is a strictly weaker
// property than projection, and the gap between them is exactly the defect
// class: a bind can be spelled correctly, signed, accepted by the validator
// and computed by the fold, and still never reach a frame.
//
// So this test asks the other half of the question, in the only package that
// can: for every must_bind field in every corpus case, render that case's
// recorded convergence document and require the value to appear. The corpus
// says the document is the right answer; here the frame has to agree.
//
// It lives in internal/engine rather than internal/eval deliberately, and the
// import direction is the whole reason. internal/eval imports scene, theme and
// ui — not engine and not fold — so it has no renderer to call and no state to
// render with. Wiring one in would point eval at the engine and make the
// harness depend on the thing it is supposed to be independent of. Asking the
// question from this side costs nothing: engine already imports scene and
// fold, and adding eval closes no loop.
//
// This is the guard that was missing when the projection regression was
// removed during verification: every test under internal/eval stayed green
// while the panel rendered a placeholder, because nothing in that package
// renders. That was recorded in EVAL.md as a real limit rather than papered
// over. This is the limit being paid off.
func TestEveryCorpusMustBindFieldReachesTheFrame(t *testing.T) {
	cases, err := eval.LoadAll(filepath.Join("..", "..", "testdata", "eval"))
	if err != nil {
		t.Fatalf("loading the corpus: %v", err)
	}
	// The floor stops a silent vacuous pass. If LoadAll ever returns an
	// empty or truncated set, every must_bind field would be "projected" by
	// never being asked about, and this audit would report success over a
	// corpus nobody can enumerate — the same failure mode LoadAll itself
	// refuses for the corpus.
	if len(cases) < 4 {
		t.Fatalf("loaded only %d corpus cases; the audit is reading the wrong directory and would pass over a set nobody can enumerate", len(cases))
	}

	state := corpusRenderState()

	checked := 0
	for _, c := range cases {
		for _, bind := range c.Convergence.MustBind {
			checked++
			t.Run(c.ID+"/"+bind, func(t *testing.T) {
				want, ok := corpusBindWitness[bind]
				if !ok {
					t.Fatalf("must_bind names %q but this audit has no witness value for it\n"+
						"consequence: a corpus case can require a bind that nothing here renders, which is\n"+
						"how the convergence document and the screen drifted apart the first time. An\n"+
						"unwitnessed bind is not checked, and a check nobody notices is missing is worse\n"+
						"than an absent one.\n"+
						"remedy: add %q to corpusBindWitness with a value the fold state below makes\n"+
						"distinguishable from both the empty state and the placeholder.", bind, bind)
				}

				doc, err := scene.ParseDocument(c.Convergence.Document)
				if err != nil {
					t.Fatalf("the recorded convergence document does not parse: %v\n"+
						"consequence: the corpus holds this document up as the correct answer, so a model\n"+
						"reproducing it exactly would be scored against a document the engine cannot load.", err)
				}
				// The document must load before its rendering means
				// anything: this audit is about a bind that is accepted and
				// still not drawn, not about smuggling past the validator.
				if verr := doc.Validate(); verr != nil {
					t.Fatalf("the recorded convergence document is refused by the validator: %v\n"+
						"consequence: the corpus would score a model converged on a document the engine\n"+
						"refuses to load.", verr)
				}

				r := Renderer{Width: 72, Height: 24}
				plain := r.RenderFrame(doc, state).Plain()

				if !strings.Contains(plain, want) {
					t.Errorf("case %q requires bind %q, and the recorded convergence document does not draw it\n"+
						"the frame does not contain %q\n"+
						"consequence: the grader answers a different question than the screen does. Converged\n"+
						"checks that the bind is *present* in the tree; this checks that it *projects*. When\n"+
						"those two disagree the corpus reports success over a placeholder — which is exactly\n"+
						"what happened with todos.count: signed in BINDS.md, accepted by the validator,\n"+
						"computed by the fold on every event, and read by resolveBind nowhere. A corpus that\n"+
						"under-credits the model gets argued with; one that over-credits it is the one nobody\n"+
						"audits, because the number looks like good news.\n"+
						"remedy: project the bind in resolveBind (or the node renderer that owns it) reading\n"+
						"the fold field the document already names; or, if it genuinely cannot be projected,\n"+
						"refuse it in the validator so the scene author gets an address instead of silence.\n"+
						"frame:\n%s", c.ID, bind, want, plain)
				}
			})
		}
	}

	// LoadAll guarantees each case lists at least one must_bind field, so a
	// zero here means the fields were read off the wrong struct field and
	// every subtest was skipped without one failing.
	if checked == 0 {
		t.Fatal("no must_bind field was checked across the whole corpus; the audit read an empty inventory and proved nothing")
	}
}

// corpusBindWitness is the value each bind must put on screen.
//
// The witness is a concrete string rather than "anything but the placeholder"
// on purpose: an audit that only asserted "not […]" would pass on a projection
// that returned the wrong field, which is a defect the behavioural test for
// the signed binds already caught once. The value has to be distinguishable
// from the empty state too, or a bind that resolves to "" would read as drawn.
//
// The map is keyed by bind rather than by case: the same field required by two
// cases is the same projection, and two spellings of one witness could drift
// apart and quietly weaken one case's check.
// Witnesses are also kept short enough to survive the narrowest panel any
// corpus scene draws. MAXIMUM's Tasks pane is sixteen columns wide, and a
// longer witness would be word-wrapped across two lines — the value would be
// on screen and the substring search would still miss it. That failure looks
// exactly like the defect this audit hunts, so a witness that cannot fit is
// not a stricter test, it is a false alarm that trains the reader to ignore
// the real one.
var corpusBindWitness = map[string]string{
	"chat.history":        "the transcript line",
	"agent.todos":         "release-notes",
	"todos.count":         "37",
	"model.name":          "openai/gpt-4o",
	"usage.in":            "1234",
	"usage.out":           "5678",
	"session.tokens_used": "9012",
}

// corpusRenderState is the one fold state every case renders against.
//
// A single shared state, rather than one per case, is what makes a wrong-field
// projection visible: every witness above is distinct, so a bind that returns
// its neighbour's value fails instead of coincidentally matching. Values are
// chosen to collide with nothing the scenes draw as chrome — a witness that
// appeared in a border or a label would be found in the frame no matter what
// the bind did.
func corpusRenderState() fold.State {
	return fold.State{
		History: []fold.ChatLine{
			{Role: "user", Text: "the transcript line"},
		},
		Todos: []fold.TodoItem{
			{Task: "release-notes", BlockedOn: "approval", Actor: "backend"},
		},
		TodosCount:        37,
		ModelName:         "openai/gpt-4o",
		UsageIn:           1234,
		UsageOut:          5678,
		SessionTokensUsed: 9012,
		StatusActive:      "true",
	}
}
