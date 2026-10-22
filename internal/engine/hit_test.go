package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

const hitDoc = `{"root":{"type":"stack","children":[
 {"id":"plain","type":"text","text":"PLAIN","on_press":"cmd:/a"},
 {"type":"text","text":"NOID","on_press":"cmd:/b"},
 {"type":"text","text":"DRAWN"},
 {"id":"box","type":"box","on_press":"cmd:/outer","children":[
   {"id":"inner","type":"text","text":"INNER","on_press":"cmd:/inner"}]},
 {"id":"rows","type":"list","bind":"team.members","row_template":{"type":"row","children":[
   {"id":"pick","type":"text","bind":"row.id","on_press":"cmd:/agent {row.id}"}]}}
]}}`

func hitRows(t *testing.T) []ui.Line {
	t.Helper()
	doc, err := scene.ParseDocument([]byte(hitDoc))
	if err != nil {
		t.Fatal(err)
	}
	r := Renderer{Width: 30, Height: 20}
	st := fold.State{TeamMembers: []fold.TeamMember{{ID: "ann"}, {ID: "bob"}}}
	return r.RenderFrame(doc, st).Live
}

func hitOf(rows []ui.Line, text string) (string, bool) {
	for _, l := range rows {
		for _, s := range l {
			if len(s.Text) >= len(text) && s.Text[:len(text)] == text {
				return s.Hit, true
			}
		}
	}
	return "", false
}

// A span of a pressable node says which node a click on it presses.
// Consequence: a click has nothing to resolve and the mouse cannot press a button.
// Remedy: stamp the node's key on the spans it draws, in renderNode.
func TestPressableNodesStampTheirKeyOnTheSpansTheyDraw(t *testing.T) {
	rows := hitRows(t)
	if got, ok := hitOf(rows, "PLAIN"); !ok || got != "plain" {
		t.Errorf("PLAIN carries hit %q (found=%v), want %q", got, ok, "plain")
	}
	if got, _ := hitOf(rows, "DRAWN"); got != "" {
		t.Errorf("DRAWN carries hit %q. Consequence: a click on plain text presses something. Remedy: stamp only nodes with on_press.", got)
	}
	if got, _ := hitOf(rows, "NOID"); got != "" {
		t.Errorf("a node with no id carries hit %q. Consequence: a click names a target Tab cannot reach. Remedy: no id, no hit (the ring has the same rule).", got)
	}
}

// The node under the pointer wins over the container around it.
func TestTheInnermostPressableNodeOwnsTheClick(t *testing.T) {
	if got, _ := hitOf(hitRows(t), "INNER"); got != "inner" {
		t.Errorf("INNER carries hit %q, want %q. Consequence: a click on a button inside a pressable box presses the box. Remedy: stamp only spans nothing inside has claimed.", got, "inner")
	}
}

// A row of a template is named the way the focus ring names it.
func TestARowOfATemplateStampsTheKeyOfItsRow(t *testing.T) {
	rows := hitRows(t)
	if got, _ := hitOf(rows, "ann"); got != HitKey("pick", 0) {
		t.Errorf("row 0 carries hit %q, want %q", got, HitKey("pick", 0))
	}
	if got, _ := hitOf(rows, "bob"); got != HitKey("pick", 1) {
		t.Errorf("row 1 carries hit %q, want %q. Consequence: every row of a list presses the first one. Remedy: key a row by its index.", got, HitKey("pick", 1))
	}
}

// HitAt reads the column the pointer is on, by display width, not by byte.
func TestHitAtReadsDisplayColumns(t *testing.T) {
	l := ui.Line{{Text: "ab"}, {Text: "好好", Hit: "wide"}, {Text: "z", Hit: "z"}}
	for col, want := range map[int]string{0: "", 1: "", 2: "wide", 3: "wide", 4: "wide", 5: "wide", 6: "z", 7: "", -1: ""} {
		if got := l.HitAt(col); got != want {
			t.Errorf("HitAt(%d) = %q, want %q", col, got, want)
		}
	}
}

func hitRowsOf(t *testing.T, docJSON string, w, h int, st fold.State) []ui.Line {
	t.Helper()
	doc, err := scene.ParseDocument([]byte(docJSON))
	if err != nil {
		t.Fatal(err)
	}
	r := Renderer{Width: w, Height: h}
	return r.RenderFrame(doc, st).Live
}

// A pressable text that wraps is pressable on every row it takes, not just the first.
// Consequence: a long label or list entry can be clicked on its first line only.
// Remedy: keep Hit on the spans wrapping splits (ui.WrapSpans).
func TestAWrappedPressableNodeIsPressableOnEveryRow(t *testing.T) {
	rows := hitRowsOf(t, `{"root":{"type":"stack","children":[
 {"id":"long","type":"markdown","text":"one two three four five six seven eight nine ten","on_press":"cmd:/a"}]}}`, 12, 10, fold.State{})
	n := 0
	for _, l := range rows {
		if l.Text() != "" && strings.TrimSpace(l.Text()) != "" {
			n++
			if got := l.HitAt(0); got != "long" {
				t.Errorf("row %q carries hit %q, want %q", l.Text(), got, "long")
			}
		}
	}
	if n < 2 {
		t.Fatalf("premise: the text must wrap onto several rows, got %d", n)
	}
}

// An overlay is composited over the rows below it by cutting them; both the overlay's own
// press targets and the ones left uncovered must survive the cut.
// Consequence: a click on a pressable overlay, or beside it, does nothing or presses the wrong node.
// Remedy: cutLine and compositeLine keep Hit.
func TestACompositedOverlayKeepsItsPressTargetsAndTheOnesBesideIt(t *testing.T) {
	rows := hitRowsOf(t, `{"root":{"type":"stack","children":[
 {"id":"base","type":"text","text":"BASEBASEBASEBASEBASE","on_press":"cmd:/base"},
 {"id":"ov","type":"overlay","anchor":"top-right","min_width":6,"children":[
   {"id":"over","type":"text","text":"OVER","on_press":"cmd:/over"}]}]}}`, 20, 5, fold.State{})
	if len(rows) == 0 {
		t.Fatal("no rows")
	}
	if got := rows[0].HitAt(15); got != "over" {
		t.Errorf("the overlay's column carries hit %q, want %q. Consequence: a click on a pressable overlay does nothing. Remedy: keep Hit in compositeLine.", got, "over")
	}
	if got := rows[0].HitAt(0); got != "base" {
		t.Errorf("the column left of the overlay carries hit %q, want %q. Consequence: the cut drops the node under it. Remedy: keep Hit in cutLine.", got, "base")
	}
}

// A pressable node inside a row_template that has no id is not a target (the ring skips it
// too), so a click on it presses nothing instead of a key like "\x00row\x000".
func TestAnIdlessPressableNodeInARowIsNotATarget(t *testing.T) {
	rows := hitRowsOf(t, `{"root":{"type":"stack","children":[
 {"id":"rows","type":"list","bind":"team.members","row_template":{"type":"row","children":[
   {"type":"text","bind":"row.id","on_press":"cmd:/agent {row.id}"}]}}]}}`, 20, 5, fold.State{TeamMembers: []fold.TeamMember{{ID: "ann"}}})
	for _, l := range rows {
		for _, s := range l {
			if s.Hit != "" {
				t.Errorf("span %q carries hit %q. Consequence: a click names a target the ring never offers. Remedy: no id, no hit.", s.Text, s.Hit)
			}
		}
	}
}

// The find bar washes the hits of a word by splitting spans; the pieces keep their target.
// Consequence: while a search is open, the highlighted words of a pressable row cannot be clicked.
// Remedy: copy Hit in washRow.
func TestFindHighlightKeepsTheHitOfTheSpansItSplits(t *testing.T) {
	l := ui.Line{{Text: "say hello there", Hit: "k"}}
	out := washRow(l, []rune("hello"), "find")
	if len(out) < 2 {
		t.Fatalf("premise: the row must split at the hit, got %d spans", len(out))
	}
	for _, s := range out {
		if s.Hit != "k" {
			t.Errorf("span %q lost its hit (%q)", s.Text, s.Hit)
		}
	}
}
