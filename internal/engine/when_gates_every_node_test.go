package engine

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// `when` is signed as a universal property in SCENES.md — listed with `id`,
// `bind` and `style`, not as a property of any one node type — and the
// validator treats it that way, accepting it wherever it appears. The engine
// honoured it in exactly two places: `renderHorizontal` filtered its children,
// and `renderOverlay` gated itself. Nothing else asked.
//
// That made the visibility of a node a fact about its *parent* rather than
// about the node: the same `{"type":"text","when":"ui.max"}` disappeared
// inside a `row` and drew inside a `stack`. Ten of the eleven node types
// ignored the gate under a stack — every type but `overlay`, whose own
// renderer happened to ask.
//
// This guard sweeps the matrix both axes at once: every node type, under every
// container, in both gate states. A single-parent sweep is what let the
// original two sites look like coverage.
//
// Both directions are checked deliberately. A guard that only asserted "a
// falsy gate hides it" passes on a renderer that draws nothing at all, which
// is the same mistake as a probe whose witness collides with a hardcoded
// value: it would report the gate working on a node that had simply been
// deleted.

// gateProbes are the node types, each with content that is non-empty under the
// state below. Emptiness is the confounder here: a marquee with no text and a
// list with no rows both render zero lines for reasons that have nothing to do
// with `when`, so a probe built on them would report a working gate on a
// renderer that has none.
var gateProbes = map[string]string{
	"text":     `{"type":"text","text":"WITNESS-TEXT"}`,
	"markdown": `{"type":"markdown","bind":"chat.history"}`,
	"input":    `{"type":"input","bind":"user.input"}`,
	"rule":     `{"type":"rule"}`,
	"list":     `{"type":"list","bind":"slash.matches"}`,
	"marquee":  `{"type":"marquee","bind":"thinking.text"}`,
	"spinner":  `{"type":"spinner","bind":"agent.working"}`,
	"box":      `{"type":"box","border":"single","children":[{"type":"text","text":"WITNESS-BOX"}]}`,
	"row":      `{"type":"row","children":[{"type":"text","text":"WITNESS-ROW"}]}`,
	"stack":    `{"type":"stack","children":[{"type":"text","text":"WITNESS-STACK"}]}`,
	"overlay":  `{"type":"overlay","anchor":"bottom","children":[{"type":"text","text":"WITNESS-OVL"}]}`,
}

// gateState satisfies every bind the probes read, so each node has something
// to draw, and pins the two gates the sweep uses:
//
//   - `ui.max` is unset. BINDS.md signs its empty state as "null — no pane is
//     maximized", and evalWhen reads the placeholder as false, so this is a
//     falsy gate written the way a real scene writes one.
//   - `status.active` is "true", the host's own live-status flag, so the
//     truthy direction is also a real bind rather than a literal.
func gateState() fold.State {
	return fold.State{
		History:      []fold.ChatLine{{Role: "user", Text: "WITNESS-HISTORY"}},
		UserInput:    "WITNESS-INPUT",
		ThinkingText: "WITNESS-THINKING",
		AgentWorking: true,
		SlashMatches: []fold.SlashMatch{{Name: "/alpha"}, {Name: "/beta"}},
		StatusActive: "true",
		AgentMode:    "live",
		ModelName:    "kimi-k3",
	}
}

func drawDoc(t *testing.T, src string, st fold.State) string {
	t.Helper()
	doc, err := scene.ParseDocument([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, src)
	}
	r := &Renderer{Width: 48, Height: 12}
	f := r.RenderFrame(doc, st)
	var sb strings.Builder
	for _, l := range f.Live {
		sb.WriteString(l.Text())
		sb.WriteString("\n")
	}
	return sb.String()
}

// wrapGated puts one child, carrying the given `when`, inside one container.
func wrapGated(t *testing.T, parent, childJSON, when string) string {
	t.Helper()
	var child map[string]any
	if err := json.Unmarshal([]byte(childJSON), &child); err != nil {
		t.Fatalf("probe json: %v", err)
	}
	if when != "" {
		child["when"] = when
	}
	doc := map[string]any{"root": map[string]any{
		"type":     parent,
		"children": []any{child},
	}}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// TestWhenGatesEveryNodeUnderEveryContainer is the sweep: for each node type
// and each container, a falsy `when` must draw nothing and a truthy one must
// draw what the ungated node draws.
func TestWhenGatesEveryNodeUnderEveryContainer(t *testing.T) {
	st := gateState()
	containers := []string{"stack", "row", "box", "overlay"}

	for name, probe := range gateProbes {
		for _, parent := range containers {
			t.Run(name+"/under-"+parent, func(t *testing.T) {
				ungated := drawDoc(t, wrapGated(t, parent, probe, ""), st)
				if strings.TrimSpace(ungated) == "" {
					// The probe itself is broken: a node that draws nothing
					// ungated cannot measure a gate, and would report every
					// gate as working.
					t.Fatalf("probe draws nothing without a gate; the witness is wrong, not the engine")
				}

				hidden := drawDoc(t, wrapGated(t, parent, probe, "ui.max"), st)
				if strings.TrimSpace(hidden) != "" {
					t.Errorf("a falsy `when` did not hide a %s under a %s:\n%s",
						name, parent, hidden)
				}

				shown := drawDoc(t, wrapGated(t, parent, probe, "status.active"), st)
				if strings.TrimSpace(shown) == "" {
					t.Errorf("a truthy `when` hid a %s under a %s", name, parent)
				}
			})
		}
	}
}

// TestHiddenGrowChildReservesNoRows is the reservation half, and it is a
// separate property from drawing.
//
// renderStack measures its children before dividing the remaining rows among
// the growers. A hidden child that is merely skipped at draw time still counts
// in that measuring pass: it takes its proportional share and renders the
// share as blank rows. The content disappears and the hole it sat in does not,
// which on screen is a gap the scene never asked for — and it is invisible to
// the sweep above, because that only asks whether the node's own text is
// absent.
func TestHiddenGrowChildReservesNoRows(t *testing.T) {
	st := gateState()

	// Two growers of equal weight. With the gate falsy the survivor should own
	// the whole elastic band, not half of it.
	const doc = `{"root":{"type":"stack","children":[
      {"type":"markdown","bind":"chat.history","grow":1,"when":"%s"},
      {"type":"markdown","bind":"chat.history","grow":1}
    ]}}`

	hidden := drawDoc(t, strings.Replace(doc, "%s", "ui.max", 1), st)
	both := drawDoc(t, strings.Replace(doc, "%s", "status.active", 1), st)

	countWitness := func(s string) int {
		return strings.Count(s, "WITNESS-HISTORY")
	}
	if got := countWitness(hidden); got != 1 {
		t.Errorf("a hidden grow child still drew: want 1 witness, got %d:\n%s", got, hidden)
	}
	if got := countWitness(both); got != 2 {
		t.Errorf("control: both growers should draw, got %d:\n%s", got, both)
	}

	// The survivor must own the rows the hidden child would have taken, and
	// the witness for that is *where* it sits, not how tall the frame is.
	//
	// Frame height cannot see this defect at all: RenderFrame pads its output
	// to the terminal height, so the scene is the same number of rows whether
	// the hidden child's share was reclaimed or left blank. An earlier version
	// of this assertion compared heights, passed under the injection that
	// removes the reservation skip, and was measuring the padding rather than
	// the layout.
	//
	// The position is the thing that moves: with the share reclaimed the
	// survivor starts at the top of the band, and with it reserved the
	// survivor is pushed down by exactly the rows the hidden child kept.
	const alone = `{"root":{"type":"stack","children":[
      {"type":"markdown","bind":"chat.history","grow":1}
    ]}}`
	row := func(frame string) int {
		for i, line := range strings.Split(frame, "\n") {
			if strings.Contains(line, "WITNESS-HISTORY") {
				return i
			}
		}
		return -1
	}
	if got, want := row(hidden), row(drawDoc(t, alone, st)); got != want {
		t.Errorf("a hidden grow child kept its share of the budget: the survivor starts at row %d, and owns row %d when the hidden child is absent from the scene entirely",
			got, want)
	}
}

// TestHiddenWeightedColumnReservesNoWidth is the horizontal twin of the
// reservation property, and it is the one the sweep above cannot see.
//
// renderHorizontal divides the row's width among its children before drawing
// them. A hidden child that renderNode merely declines to draw is still in
// that division: it takes its weighted share of the columns and leaves the
// share blank, so the surviving column is pushed right by the width of
// content that is not on screen.
//
// The sweep misses it because an *unweighted* row packs its columns left, so
// a hidden sibling costs nothing visible and the frame looks correct. Only
// weights make the reserved gap observable — which is why removing the row's
// filter failed nothing until this case existed, and why the guard measures
// the surviving column's offset rather than its presence.
func TestHiddenWeightedColumnReservesNoWidth(t *testing.T) {
	st := gateState()

	gated := drawDoc(t, `{"root":{"type":"row","children":[
      {"type":"text","text":"AAAA","weight":1,"when":"ui.max"},
      {"type":"text","text":"BBBB","weight":1}
    ]}}`, st)
	alone := drawDoc(t, `{"root":{"type":"row","children":[
      {"type":"text","text":"BBBB","weight":1}
    ]}}`, st)

	if strings.Contains(gated, "AAAA") {
		t.Fatalf("the gated column drew at all:\n%s", gated)
	}
	if got, want := strings.Index(gated, "BBBB"), strings.Index(alone, "BBBB"); got != want {
		t.Errorf("a hidden weighted column kept its share of the width: the surviving column starts at col %d, and starts at col %d when the hidden one is absent from the scene entirely",
			got, want)
	}
}

// TestGatedOverlayIsNotPreRenderedByTheStack guards the path renderNode does
// not cover.
//
// renderStack does not route overlays through renderNode: it calls
// renderOverlay directly in its measuring pass, because it has to know the
// overlay's height to reserve rows for it before the growers divide the rest.
// So the gate for an overlay that is a direct stack child is enforced by the
// stack's own skip, not by renderNode — and the slash menu is exactly that
// shape, with `when: slash.active` as what closes it.
//
// renderOverlay used to re-check the gate itself, which read as defence in
// depth and was mutual masking: with both copies present, deleting either one
// left the other hiding the overlay, so neither deletion failed a test and
// each looked like dead code to whoever removed it. One gate, measured here.
func TestGatedOverlayIsNotPreRenderedByTheStack(t *testing.T) {
	st := gateState()

	closed := drawDoc(t, `{"root":{"type":"stack","children":[
      {"type":"markdown","bind":"chat.history","grow":1},
      {"type":"input","bind":"user.input"},
      {"type":"overlay","anchor":"bottom","when":"ui.max","children":[
        {"type":"text","text":"WITNESS-MENU"}]}
    ]}}`, st)
	open := drawDoc(t, `{"root":{"type":"stack","children":[
      {"type":"markdown","bind":"chat.history","grow":1},
      {"type":"input","bind":"user.input"},
      {"type":"overlay","anchor":"bottom","when":"status.active","children":[
        {"type":"text","text":"WITNESS-MENU"}]}
    ]}}`, st)

	if strings.Contains(closed, "WITNESS-MENU") {
		t.Errorf("a gated overlay drew through the stack's pre-render:\n%s", closed)
	}
	if !strings.Contains(open, "WITNESS-MENU") {
		t.Errorf("control: an ungated overlay should draw:\n%s", open)
	}

	// The rows an open menu occupies come out of the growers' budget, so a
	// closed one must give them back — and the direction is worth stating,
	// because the obvious guess is backwards. The transcript is the grower and
	// it reserves its full share whether or not it has content to fill it, so
	// the rows the menu releases are taken by the transcript and the input is
	// pushed *down*, not up. A closed-but-still-reserved menu leaves the input
	// where the open one put it, with blank rows beneath.
	rowOf := func(frame, needle string) int {
		for i, line := range strings.Split(frame, "\n") {
			if strings.Contains(line, needle) {
				return i
			}
		}
		return -1
	}
	if rowOf(closed, "WITNESS-INPUT") <= rowOf(open, "WITNESS-INPUT") {
		t.Errorf("a closed overlay still reserved its rows: input at %d closed, %d open — closing the menu should hand its rows back to the transcript and move the input down",
			rowOf(closed, "WITNESS-INPUT"), rowOf(open, "WITNESS-INPUT"))
	}
}

// TestSoariaThinkingLineObeysItsGate is the reachable case, on a shipped scene
// rather than a synthetic one.
//
// SOBRIA's thinking marquee is `{"id":"thinking","type":"marquee","when":
// "agent.working"}` — a direct child of the root stack, which is precisely the
// position that had no gate. BINDS.md §4.1 signs the mechanism by name in
// thinking.text's empty state: "the marquee does not render (`when` is
// false)".
//
// It looked correct in every golden because those pin a state where
// thinking.text is empty, and the marquee collapses on empty text through a
// path that has nothing to do with `when`. The gate was untested because a
// different rule was covering for it. Hold the text non-empty and flip only
// `agent.working`, and the two frames were byte-identical: the line drew while
// the agent was idle.
func TestSoariaThinkingLineObeysItsGate(t *testing.T) {
	data, err := os.ReadFile("../../testdata/SOARIA.json")
	if err != nil {
		t.Fatalf("read SOARIA: %v", err)
	}

	base := fold.State{
		History:      []fold.ChatLine{{Role: "assistant", Text: "answered"}},
		StatusActive: "true",
		AgentMode:    "live",
		ModelName:    "kimi-k3",
		// Non-empty on purpose, and held constant across both frames: the only
		// thing that varies below is the gate.
		ThinkingText: "reticulating splines",
	}

	idle := base
	idle.AgentWorking = false
	working := base
	working.AgentWorking = true

	idleFrame := drawDoc(t, string(data), idle)
	workingFrame := drawDoc(t, string(data), working)

	if strings.Contains(idleFrame, "reticulating splines") {
		t.Errorf("the thinking line drew with agent.working false:\n%s", idleFrame)
	}
	if !strings.Contains(workingFrame, "reticulating splines") {
		t.Errorf("the thinking line did not draw with agent.working true:\n%s", workingFrame)
	}
}

// TestUngatedNodesAreNotHidden is the load-bearing negative.
//
// The gate is applied in renderNode, which every node passes through, so a
// predicate that answered "hidden" for a node with no `when` would blank the
// entire tree — including all three shipped goldens, and invariant 1 with
// them. That the goldens still pass is the real proof, but they would fail
// with a diff of the whole screen and no statement of the rule; this fails
// with the rule.
func TestUngatedNodesAreNotHidden(t *testing.T) {
	st := gateState()
	for name, probe := range gateProbes {
		for _, parent := range []string{"stack", "row", "box", "overlay"} {
			if got := drawDoc(t, wrapGated(t, parent, probe, ""), st); strings.TrimSpace(got) == "" {
				t.Errorf("a %s with no `when` drew nothing under a %s: absence of a gate is not a closed gate",
					name, parent)
			}
		}
	}
}
