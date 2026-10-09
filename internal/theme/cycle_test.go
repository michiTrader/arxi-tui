package theme

import (
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/ui"
)

const rainbowTheme = `{"cycle":{"rainbow":{"colors":["#ff0000","#00ff00","#0000ff"],"attrs":["bold"],"fps":10,"spread":1}}}`

func loadRainbow(t *testing.T) *Theme {
	t.Helper()
	th, err := LoadBytes("t.json", []byte(rainbowTheme))
	if err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	return th
}

// A cycle is a token like any other for every check that asks whether a token exists, and
// at rest it is its first colour: a pipe or a golden sees a steady style.
func TestACycleIsATokenAndRestsOnItsFirstColour(t *testing.T) {
	th := loadRainbow(t)
	if !th.Has("rainbow") {
		t.Fatal("a cycle must also be a defined token, or every reference check would refuse it")
	}
	got := th.Resolve("rainbow")
	want := ui.Style{FG: ui.MustHex("#ff0000"), Attrs: ui.AttrBold}
	if got != want {
		t.Fatalf("resting style = %v, want %v", got, want)
	}
}

// The same instant always paints the same frame, and a later instant paints a different
// colour: the cycle moves with the clock and only with it.
func TestACycleMovesWithTheClock(t *testing.T) {
	th := loadRainbow(t)
	f := ui.Frame{Live: []ui.Line{{{Text: "max", Style: "rainbow"}}}}
	t0 := time.UnixMilli(1_000_000)
	a, fps := th.Cycled(f, t0)
	if fps != 10 {
		t.Fatalf("fps = %d, want 10 (the cycle's own rate, so the host can arm its ticker)", fps)
	}
	b, _ := th.Cycled(f, t0)
	if a.ANSI(th) != b.ANSI(th) {
		t.Fatal("the same instant painted two different frames")
	}
	c, _ := th.Cycled(f, t0.Add(100*time.Millisecond))
	if a.ANSI(th) == c.ANSI(th) {
		t.Fatal("a later instant painted the same colours: the cycle does not move")
	}
	if a.Plain() != f.Plain() || c.Plain() != f.Plain() {
		t.Fatal("an animation changed the text; it may only change colours")
	}
}

// spread 1 puts a different palette step on neighbouring characters (a rainbow along the
// word); spread 0 paints the whole span one colour.
func TestSpreadRunsTheRainbowAlongTheText(t *testing.T) {
	th := loadRainbow(t)
	f := ui.Frame{Live: []ui.Line{{{Text: "max", Style: "rainbow"}}}}
	out, _ := th.Cycled(f, time.UnixMilli(0))
	if got := len(out.Live[0]); got != 3 {
		t.Fatalf("spread 1 should split \"max\" into 3 spans, got %d", got)
	}
	ansi := out.ANSI(th)
	for _, hex := range []string{"38;2;255;0;0", "38;2;0;255;0", "38;2;0;0;255"} {
		if !strings.Contains(ansi, hex) {
			t.Errorf("frame lacks colour %s: %q", hex, ansi)
		}
	}
	flat, err := LoadBytes("t", []byte(`{"cycle":{"p":{"colors":["#ff0000","#00ff00"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	one, _ := flat.Cycled(ui.Frame{Live: []ui.Line{{{Text: "max", Style: "p"}}}}, time.UnixMilli(0))
	if len(one.Live[0]) != 1 {
		t.Fatalf("spread 0 should keep one span, got %d", len(one.Live[0]))
	}
}

// A frame that wears no cycle costs nothing: the same frame comes back and no ticker is
// asked for. An idle interface must stay idle.
func TestAFrameWithoutACycleAsksForNoTicks(t *testing.T) {
	th := loadRainbow(t)
	f := ui.Frame{Live: []ui.Line{{{Text: "hello", Style: "dim"}}}}
	out, fps := th.Cycled(f, time.Now())
	if fps != 0 {
		t.Fatalf("fps = %d, want 0", fps)
	}
	if &out.Live[0] != &f.Live[0] {
		t.Fatal("an unaffected frame should be returned as is")
	}
}

func TestACycleThatCannotMoveIsRefused(t *testing.T) {
	for name, src := range map[string]string{
		"one colour":  `{"cycle":{"x":{"colors":["#ff0000"]}}}`,
		"bad colour":  `{"cycle":{"x":{"colors":["#ff0000","nope"]}}}`,
		"fps too big": `{"cycle":{"x":{"colors":["red","blue"],"fps":500}}}`,
		"bad spread":  `{"cycle":{"x":{"colors":["red","blue"],"spread":99}}}`,
		"bad attr":    `{"cycle":{"x":{"colors":["red","blue"],"attrs":["glow"]}}}`,
	} {
		if _, err := LoadBytes("t.json", []byte(src)); err == nil {
			t.Errorf("%s: accepted; an animation that cannot draw what it says must be refused with the reason", name)
		}
	}
}

// Layers compose by name: a later plain token replaces an earlier animated one (the user
// said "this is one colour"), and a later cycle replaces a plain one. Neither input is
// changed.
func TestCyclesComposeLikeTokens(t *testing.T) {
	th := loadRainbow(t)
	plain := FromMap(map[string]ui.Style{"rainbow": {FG: ui.Idx(2)}})
	m := Merge(th, plain)
	if _, still := m.Cycle("rainbow"); still {
		t.Fatal("a plain token laid over a cycle must replace it")
	}
	if _, ok := th.Cycle("rainbow"); !ok {
		t.Fatal("Merge changed its base")
	}
	back := Merge(plain, th)
	if _, ok := back.Cycle("rainbow"); !ok {
		t.Fatal("a cycle laid over a plain token must win")
	}
	w := Factory().WithCycles(map[string]Cycle{"r": th.cycles["rainbow"]})
	if !w.Has("r") || !w.Has("dim") {
		t.Fatal("WithCycles must keep the base and add the cycle")
	}
	if len(Factory().CycleNames()) != 0 {
		t.Fatal("WithCycles changed the theme it was called on")
	}
}

// Counterfactual for the one place a derived token could leak: a name that merely
// contains the separator, and names of cycles that do not exist, resolve to nothing.
func TestDerivedNamesOnlyResolveForRealCycles(t *testing.T) {
	th := loadRainbow(t)
	for _, n := range []string{"nope\x001", "rainbow\x00x", "rainbow\x00-1", "\x001"} {
		if !th.Resolve(n).IsZero() {
			t.Errorf("%q resolved to a style", n)
		}
	}
	if th.Resolve("rainbow\x001").FG != ui.MustHex("#00ff00") {
		t.Error("step 1 of the rainbow should be green")
	}
	if th.Resolve("rainbow\x004").FG != ui.MustHex("#00ff00") {
		t.Error("steps wrap around the palette")
	}
}
