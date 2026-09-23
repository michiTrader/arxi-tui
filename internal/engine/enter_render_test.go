package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// The render half of G4: enter is the scheduler that composes the row-count axis
// (a container's rows) with the intensity axis (transition's dim→settled), and
// the frame is a pure function of the per-row phases the host clock feeds in
// (ADR-0005 / SCENES.md Scene 4, G-B). These pin what the design signs a row
// draws at each stage of its personal clock — not drawn before its offset, dim
// mid-entrance, settled after — and the whole-container (row:false) degenerate
// case, which is transition lifted to the subtree.
//
// Counterfactuals, run rather than argued (see the comment on each test): the
// three states a row can be in are the three the phase map distinguishes, and
// reverting the mapping in any one direction fails a case here.

// enterStackDoc is a stack of three bright text rows with a row:true enter. The
// settled token is "bright" so the dim override is visibly different from it —
// a renderer that hardcoded the settled style would pass a "text" probe by
// accident, the trap transitionDoc set for the same reason.
func enterStackDoc(t *testing.T) *scene.Document {
	t.Helper()
	body := `{ "root": { "id": "list", "type": "stack",
	  "enter": { "row": true, "stagger": "default" }, "children": [
	    { "type": "text", "text": "alpha", "style": { "style": "bright" } },
	    { "type": "text", "text": "beta",  "style": { "style": "bright" } },
	    { "type": "text", "text": "gamma", "style": { "style": "bright" } } ] } }`
	doc, err := scene.ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: enter stack doc must parse; got %v", err)
	}
	if verr := doc.Validate(); verr != nil {
		t.Fatalf("premise broken: enter stack doc must validate; got %v", verr)
	}
	return doc
}

// A staggered enter draws its rows in whatever state each row's own clock has
// reached: a settled row in its own token, a mid-entrance row dim, and a row the
// clock has not started not at all — so the visible row count grows top-to-bottom
// as the stagger advances. This is the row-count axis the design names, and it is
// the difference from putting transition on every row (which would draw all three
// dim at once, count fixed).
//
// Counterfactual, run: making enterRowState always return (drawn=true, dim=false)
// draws gamma and settles beta, failing both the row-count and the dim
// assertions; making it return (false, false) draws nothing, failing alpha.
func TestEnterDrawsRowsInTheirScheduledState(t *testing.T) {
	doc := enterStackDoc(t)
	state := fold.Fold(nil)

	// row 0 settled, row 1 mid-entrance, row 2 not yet reached (absent key).
	phase := map[string]float64{
		enterRowKey("list", 0): 1.0,
		enterRowKey("list", 1): 0.4,
	}
	r := Renderer{Width: 40, Height: 10, AnimPhase: phase}
	frame := r.RenderFrame(doc, state)

	if got := styleOfText(frame, "alpha"); got != "bright" {
		t.Errorf("the settled row (phase 1) renders under token %q, want its own %q.\n"+
			"consequence: a row past its entrance is not being drawn settled, so the list never\n"+
			"finishes arriving — it stays dim or vanishes.\n"+
			"remedy: enterRowState must report a row with phase >= 1 drawn and not dim.", got, "bright")
	}
	if got := styleOfText(frame, "beta"); got != transitionDimToken {
		t.Errorf("the mid-entrance row (phase 0.4) renders under token %q, want %q.\n"+
			"consequence: a row that has started but not settled is not dimmed, so its arrival is\n"+
			"invisible — the intensity half of the entrance is dropped.\n"+
			"remedy: enterRowState must report a row with 0 <= phase < 1 drawn and dim, and dimFrame\n"+
			"must rewrite its spans to the dim token.", got, transitionDimToken)
	}
	if strings.Contains(frame.Plain(), "gamma") {
		t.Errorf("the not-yet-reached row (no clock entry) is drawn; frame:\n%s\n"+
			"consequence: the visible row count does not grow with the stagger — every row is present\n"+
			"from the first frame, which is transition-on-every-row, not enter. The row-count axis is\n"+
			"the whole point of enter.\n"+
			"remedy: enterRowState must report a row with no entry in a non-nil phase map as not drawn.",
			frame.Plain())
	}
	if len(frame.Live) != 2 {
		t.Errorf("the frame has %d rows, want 2 (alpha settled, beta dim, gamma not yet drawn).\n"+
			"consequence: the row count is wrong, so the list is not filling one row at a time.",
			len(frame.Live))
	}
}

// A nil phase map is the pure/golden path with no clock, and a staggered enter
// there draws every row settled — the no-op guarantee that keeps a document with
// an enter rendering fully when nothing drives time, so no non-motion golden
// moves. This is the inversion the row-count axis forces made explicit: a nil map
// is settled (all rows), while a non-nil map with a row absent is not-drawn.
func TestEnterNilPhaseDrawsEveryRowSettled(t *testing.T) {
	doc := enterStackDoc(t)
	state := fold.Fold(nil)

	settled := Renderer{Width: 40, Height: 10}
	frame := settled.RenderFrame(doc, state)

	for _, want := range []string{"alpha", "beta", "gamma"} {
		if got := styleOfText(frame, want); got != "bright" {
			t.Errorf("with no clock (nil AnimPhase) row %q drew token %q, want its settled %q.\n"+
				"consequence: a document with an enter must render every row fully when nothing is\n"+
				"animating it, or every non-motion golden with a staggered list would move.",
				want, got, "bright")
		}
	}
	if len(frame.Live) != 3 {
		t.Errorf("the settled frame has %d rows, want all 3 drawn.", len(frame.Live))
	}
}

// row:false is the whole-container entrance: the subtree draws dim while the
// entrance runs and in its own tokens once the phase reaches 1 — transition's
// dim→settled rule lifted from one node's style to every span of the frame. It
// reads the phase the way transition does, so a nil map is settled and a non-nil
// map with no entry is the dim first frame.
//
// Counterfactual, run: making enterWhole always return the frame unchanged fails
// the running and first-frame cases (they draw settled); making it always dim
// fails the settled and nil cases.
func TestEnterRowFalseDimsTheWholeSubtreeThenSettles(t *testing.T) {
	body := `{ "root": { "id": "unit", "type": "stack", "enter": { "row": false }, "children": [
	    { "type": "text", "text": "alpha", "style": { "style": "bright" } },
	    { "type": "text", "text": "beta",  "style": { "style": "bright" } } ] } }`
	doc, err := scene.ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	if verr := doc.Validate(); verr != nil {
		t.Fatalf("premise broken: %v", verr)
	}
	state := fold.Fold(nil)

	cases := []struct {
		name  string
		phase map[string]float64
		want  string
	}{
		{name: "running (phase 0.5)", phase: map[string]float64{"unit": 0.5}, want: transitionDimToken},
		{name: "first frame (non-nil, no entry)", phase: map[string]float64{}, want: transitionDimToken},
		{name: "settled (phase 1)", phase: map[string]float64{"unit": 1.0}, want: "bright"},
		{name: "no clock (nil map)", phase: nil, want: "bright"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Renderer{Width: 40, Height: 10, AnimPhase: tc.phase}
			frame := r.RenderFrame(doc, state)
			for _, row := range []string{"alpha", "beta"} {
				if got := styleOfText(frame, row); got != tc.want {
					t.Errorf("row %q renders under token %q, want %q.\n"+
						"consequence: the whole-container entrance is not taking its intensity from the\n"+
						"phase — either it never dims (no entrance) or it never settles (dim forever).\n"+
						"remedy: enterWhole must dim the whole frame while AnimPhase[id] < 1 and return it\n"+
						"untouched once the phase reaches 1; a nil map is settled.", row, got, tc.want)
				}
			}
		})
	}
}

// A staggered enter reports one one-shot activity per row, carrying the stagger
// token and the row's index, so the loop can start each row's own clock at its
// offset (row i at i * duration). Every row is reported even when it is not yet
// drawn: the clock starts a row's offset countdown from the frame it first
// appears in the report, so an undrawn row that went unreported would never
// arrive.
func TestEnterReportsOneStaggeredActivityPerRow(t *testing.T) {
	doc := enterStackDoc(t)
	state := fold.Fold(nil)

	// Only row 0 has started; rows 1 and 2 are not drawn yet but must still be
	// reported so their arrivals get scheduled.
	phase := map[string]float64{enterRowKey("list", 0): 0.2}
	r := Renderer{Width: 40, Height: 10, AnimPhase: phase}
	_, active := r.RenderFrameActive(doc, state)

	if len(active) != 3 {
		t.Fatalf("a 3-row staggered enter reported %d activities, want 3 (one per row, drawn or not).\n"+
			"consequence: a row not reported is a row the clock never starts, so it never enters and\n"+
			"the list stops filling partway down.\ngot: %+v", len(active), active)
	}
	for i, a := range active {
		if a.NodeID != enterRowKey("list", i) {
			t.Errorf("activity %d has NodeID %q, want the row key %q; the loop keys each row's clock\n"+
				"by this id.", i, a.NodeID, enterRowKey("list", i))
		}
		if !a.OneShot {
			t.Errorf("row %d does not report OneShot; the loop would drive it as a continuous scroll\n"+
				"(a tick count) instead of a phase that settles.", i)
		}
		if a.Token != "default" {
			t.Errorf("row %d reports token %q, want the stagger token %q; the loop resolves the\n"+
				"inter-row delay and each row's duration by this name.", i, a.Token, "default")
		}
		if a.Row != i {
			t.Errorf("row %d reports Row index %d, want %d; the loop starts row i at offset\n"+
				"i * duration, so a wrong index puts the row in the wrong slot of the stagger.", i, a.Row, i)
		}
	}
}

// The row-count axis is honoured for a row_template list too, not only a
// children container: enter staggers the instantiated rows the same way. This is
// the canonical Scene 4 case — a list of team members or todos arriving in
// sequence — so an enter that worked on children and not on a template would
// miss the shape the design was written for.
func TestEnterStaggersRowTemplateRows(t *testing.T) {
	body := `{ "root": { "id": "todos", "type": "list", "bind": "agent.todos",
	  "enter": { "row": true, "stagger": "default" },
	  "row_template": { "type": "text", "bind": "row.task", "style": { "style": "bright" } } } }`
	doc, err := scene.ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	if verr := doc.Validate(); verr != nil {
		t.Fatalf("premise broken: %v", verr)
	}

	state := fold.State{
		Todos: []fold.TodoItem{
			{Task: "first"},
			{Task: "second"},
			{Task: "third"},
		},
	}
	if len(state.Todos) != 3 {
		t.Fatalf("premise broken: the fold must carry 3 todos to stagger; got %d", len(state.Todos))
	}

	// row 0 settled, row 1 dim, row 2 not yet reached.
	phase := map[string]float64{
		enterRowKey("todos", 0): 1.0,
		enterRowKey("todos", 1): 0.3,
	}
	r := Renderer{Width: 40, Height: 10, AnimPhase: phase}
	frame := r.RenderFrame(doc, state)

	if got := styleOfText(frame, "first"); got != "bright" {
		t.Errorf("the settled template row renders under %q, want %q; a staggered list must draw a\n"+
			"finished row in its own token.", got, "bright")
	}
	if got := styleOfText(frame, "second"); got != transitionDimToken {
		t.Errorf("the mid-entrance template row renders under %q, want %q; a row still arriving is dim.",
			got, transitionDimToken)
	}
	if strings.Contains(frame.Plain(), "third") {
		t.Errorf("the not-yet-reached template row is drawn; the row count must grow as the stagger\n"+
			"advances, for a row_template exactly as for children. frame:\n%s", frame.Plain())
	}
}
