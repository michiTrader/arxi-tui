package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

func levels(current string) []fold.ModelMatch {
	var out []fold.ModelMatch
	for _, l := range []string{"low", "medium", "high", "max"} {
		out = append(out, fold.ModelMatch{Ref: l, Name: l, Provider: "about " + l, Current: l == current})
	}
	return out
}

func menuDoc(t *testing.T, node string) *scene.Document {
	t.Helper()
	d, err := scene.ParseDocument([]byte(`{"root":` + node + `}`))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	return d
}

func render(t *testing.T, d *scene.Document, st fold.State, width int) ui.Frame {
	t.Helper()
	r := Renderer{Width: width, Height: 20}
	return r.RenderFrame(d, st)
}

// layout:"horizontal" puts every level on one line with the highlighted one marked, and
// the meaning of the highlighted one on the line under it. The same rows and the same
// highlight as the vertical menu: only the geometry differs.
func TestHorizontalMenuIsOneLineWithTheHighlightMarked(t *testing.T) {
	d := menuDoc(t, `{"type":"list","bind":"model.matches","layout":"horizontal"}`)
	st := fold.State{ModelActive: true, ModelMatches: levels("high"), ModelSelected: 2}
	f := render(t, d, st, 80)
	if len(f.Live) != 2 {
		t.Fatalf("want a names line and a meaning line, got %d lines:\n%s", len(f.Live), f.Plain())
	}
	names := f.Live[0].Text()
	for _, want := range []string{"low", "medium", "‹high ✓›", "max"} {
		if !strings.Contains(names, want) {
			t.Errorf("names line lacks %q: %q", want, names)
		}
	}
	if !strings.Contains(f.Live[1].Text(), "about high") {
		t.Errorf("meaning line should describe the highlighted level: %q", f.Live[1].Text())
	}
	v := render(t, menuDoc(t, `{"type":"list","bind":"model.matches"}`), st, 80)
	if len(v.Live) != 4 {
		t.Fatalf("the default must stay vertical, one row per level: got %d lines", len(v.Live))
	}
}

// When the names do not fit, the window follows the highlight and says so at the edges,
// and no line is wider than the pane.
func TestHorizontalMenuScrollsWithTheHighlight(t *testing.T) {
	var many []fold.ModelMatch
	for _, n := range []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel"} {
		many = append(many, fold.ModelMatch{Ref: n, Name: n})
	}
	d := menuDoc(t, `{"type":"list","bind":"model.matches","layout":"horizontal"}`)
	for sel := range many {
		f := render(t, d, fold.State{ModelActive: true, ModelMatches: many, ModelSelected: sel}, 30)
		line := f.Live[0].Text()
		if !strings.Contains(line, "‹"+many[sel].Name+"›") {
			t.Errorf("sel %d: highlight not in view: %q", sel, line)
		}
		if w := ansiStringWidth(line); w > 30 {
			t.Errorf("sel %d: line is %d wide in a 30 pane: %q", sel, w, line)
		}
	}
	first := render(t, d, fold.State{ModelActive: true, ModelMatches: many, ModelSelected: 0}, 30).Live[0].Text()
	last := render(t, d, fold.State{ModelActive: true, ModelMatches: many, ModelSelected: 7}, 30).Live[0].Text()
	if !strings.HasSuffix(strings.TrimRight(first, " "), "›") || strings.Contains(first, "‹ ") {
		t.Errorf("at the start there is more to the right only: %q", first)
	}
	if !strings.Contains(last, "‹") {
		t.Errorf("at the end there is more to the left: %q", last)
	}
}

// style_by on a text node: the bound value picks the token, any other value keeps the
// node's own, and an animated token (a theme cycle) is accepted like any other.
func TestStyleByPicksTheTokenFromTheBoundValue(t *testing.T) {
	d := menuDoc(t, `{"type":"text","bind":"host.effort","style":{"style":"dim"},"style_by":{"max":"banner"}}`)
	max := render(t, d, fold.State{HostEffort: "max"}, 40)
	low := render(t, d, fold.State{HostEffort: "low"}, 40)
	if got := max.Live[0][0].Style; got != "banner" {
		t.Errorf("max should wear banner, wears %q", got)
	}
	if got := low.Live[0][0].Style; got != "dim" {
		t.Errorf("low keeps the node's own style, wears %q", got)
	}
}

// style_by is honoured on every node type, not on the ones a test happened to reach: it is
// applied where every node passes through.
func TestStyleByIsHonouredOnEveryNodeType(t *testing.T) {
	for _, typ := range []string{"text", "box", "marquee", "markdown"} {
		d := menuDoc(t, `{"type":"`+typ+`","bind":"host.effort","style_by":{"max":"banner"}}`)
		got := withStyleByFor(t, d, "max")
		if got != "banner" {
			t.Errorf("%s ignored style_by: style %q", typ, got)
		}
	}
}

func withStyleByFor(t *testing.T, d *scene.Document, effort string) string {
	t.Helper()
	r := Renderer{Width: 40, Height: 10}
	n := r.withStyleBy(d.Root, fold.State{HostEffort: effort})
	return styleName(n.Style)
}

// The animated token on the /effort row: the "max" row is drawn in the rainbow cycle,
// moves with the clock, and the highlighted dressed row keeps a marker so the highlight
// never disappears with the dressing.
func TestAnimatedDressingOnTheMaxRowMovesAndKeepsTheHighlight(t *testing.T) {
	th, err := theme.LoadBytes("t", []byte(`{"cycle":{"rainbow":{"colors":["#ff0000","#00ff00","#0000ff"],"fps":10,"spread":1}}}`))
	if err != nil {
		t.Fatal(err)
	}
	th = theme.Merge(theme.Factory(), th)
	d := menuDoc(t, `{"type":"list","bind":"model.matches","style_by":{"max":"rainbow"}}`)
	st := fold.State{ModelActive: true, ModelMatches: levels(""), ModelSelected: 3}
	f := render(t, d, st, 60)
	if got := f.Live[3].Text(); !strings.HasPrefix(got, "› max") {
		t.Errorf("the highlighted dressed row needs a marker, got %q", got)
	}
	t0 := time.UnixMilli(5_000)
	a, fps := th.Cycled(f, t0)
	b, _ := th.Cycled(f, t0.Add(100*time.Millisecond))
	if fps == 0 {
		t.Fatal("a frame with an animated row must ask the host for ticks")
	}
	if a.ANSI(th) == b.ANSI(th) {
		t.Error("the dressed row did not move with the clock")
	}
	// the other rows are untouched by the animation
	if a.Live[0].ANSI(th) != b.Live[0].ANSI(th) {
		t.Error("an undressed row changed between ticks")
	}
	// a frame where max is not shown asks for nothing
	st.ModelMatches = levels("")[:3]
	st.ModelSelected = 0
	idle, fps2 := th.Cycled(render(t, d, st, 60), t0)
	if fps2 != 0 || idle.Live == nil {
		t.Errorf("no animated row on screen but the host was asked for %d fps", fps2)
	}
}

// ansiStringWidth is shared with the renderer; this guards the test's own assumption.
func TestWidthHelperAgrees(t *testing.T) {
	if ansiStringWidth("‹max ✓›") != 7 {
		t.Fatalf("width helper disagrees: %d", ansiStringWidth("‹max ✓›"))
	}
}
