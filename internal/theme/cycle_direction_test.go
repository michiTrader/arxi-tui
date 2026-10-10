package theme

import (
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/ui"
)

// A frame of 3 rows by 10 columns drawn as a round border in one token.
func borderFrame(style string) ui.Frame {
	return ui.Frame{Live: []ui.Line{
		{{Text: "╭────────╮", Style: style}},
		{{Text: "│", Style: style}, {Text: "  input ", Style: "dim"}, {Text: "│", Style: style}},
		{{Text: "╰────────╯", Style: style}},
	}}
}

func loadDir(t *testing.T, dir string, extra string) *Theme {
	t.Helper()
	src := `{"cycle":{"g":{"colors":["#ff0000","#0000ff"],"fps":10,"direction":"` + dir + `"` + extra + `}}}`
	th, err := LoadBytes("t.json", []byte(src))
	if err != nil {
		t.Fatalf("LoadBytes(%s): %v", dir, err)
	}
	return th
}

// colourOf is the foreground the cell at (row, col) of a cycled frame is painted with.
func colourOf(t *testing.T, th *Theme, f ui.Frame, row, col int) ui.Color {
	t.Helper()
	at := 0
	for _, s := range f.Live[row] {
		w := ui.Line{s}.Width()
		if col < at+w {
			return th.Resolve(s.Style).FG
		}
		at += w
	}
	t.Fatalf("no cell at row %d col %d", row, col)
	return ui.Color{}
}

// The whole point of a direction: top and bottom edges are not the same colours, and the
// far corners get the two ends of the palette.
func TestDiagonalGivesTopAndBottomDifferentColours(t *testing.T) {
	th := loadDir(t, "diagonal", "")
	out, fps := th.Cycled(borderFrame("g"), time.UnixMilli(0))
	if fps != 10 {
		t.Fatalf("fps = %d, want 10", fps)
	}
	red, blue := th.Resolve("g").FG, ui.MustHex("#0000ff")
	tl, tr := colourOf(t, th, out, 0, 0), colourOf(t, th, out, 0, 9)
	bl, br := colourOf(t, th, out, 2, 0), colourOf(t, th, out, 2, 9)
	if tl != red || br != blue {
		t.Errorf("corners = %v and %v, want the first and last palette colours %v and %v", tl, br, red, blue)
	}
	if tl == tr || bl == br {
		t.Errorf("an edge is one colour (%v, %v): the gradient must run across it", tl, bl)
	}
	if tl == bl && tr == br {
		t.Error("top and bottom edges are identical; a diagonal must tell them apart")
	}
	// Antidiagonal is the mirror image: it starts from the bottom-left corner.
	ad := loadDir(t, "antidiagonal", "")
	o2, _ := ad.Cycled(borderFrame("g"), time.UnixMilli(0))
	if colourOf(t, ad, o2, 2, 0) != red || colourOf(t, ad, o2, 0, 9) != blue {
		t.Error("antidiagonal should run from the bottom-left corner (first colour) to the top-right (last)")
	}
}

// Horizontal is the same on every row; vertical is the same along every row.
func TestHorizontalAndVerticalDirections(t *testing.T) {
	h := loadDir(t, "horizontal", "")
	out, _ := h.Cycled(borderFrame("g"), time.UnixMilli(0))
	for _, col := range []int{0, 4, 9} {
		if colourOf(t, h, out, 0, col) != colourOf(t, h, out, 2, col) {
			t.Errorf("horizontal: column %d differs between top and bottom", col)
		}
	}
	if colourOf(t, h, out, 0, 0) == colourOf(t, h, out, 0, 9) {
		t.Error("horizontal: the two ends of a row share a colour")
	}
	v := loadDir(t, "vertical", "")
	out, _ = v.Cycled(borderFrame("g"), time.UnixMilli(0))
	if colourOf(t, v, out, 0, 0) != colourOf(t, v, out, 0, 9) {
		t.Error("vertical: one row should be one colour")
	}
	if colourOf(t, v, out, 0, 0) == colourOf(t, v, out, 1, 0) {
		t.Error("vertical: neighbouring rows share a colour")
	}
}

// Radial is symmetric about the centre: mirrored corners match, and the edge midpoints
// differ from the corners.
func TestRadialIsSymmetricAboutTheCentre(t *testing.T) {
	th := loadDir(t, "radial", "")
	out, _ := th.Cycled(borderFrame("g"), time.UnixMilli(0))
	tl, tr, bl, br := colourOf(t, th, out, 0, 0), colourOf(t, th, out, 0, 9), colourOf(t, th, out, 2, 0), colourOf(t, th, out, 2, 9)
	if tl != tr || tl != bl || tl != br {
		t.Errorf("radial: the four corners should match, got %v %v %v %v", tl, tr, bl, br)
	}
	if mid := colourOf(t, th, out, 0, 4); mid == tl {
		t.Errorf("radial: the middle of the top edge should differ from the corner (%v)", mid)
	}
}

// Time moves the gradient: a later instant paints other colours, the same instant paints
// the same ones, and the text never changes.
func TestPositionalCycleMovesWithTheClockAndKeepsTheText(t *testing.T) {
	th := loadDir(t, "diagonal", "")
	f := borderFrame("g")
	a, _ := th.Cycled(f, time.UnixMilli(1_000_000))
	b, _ := th.Cycled(f, time.UnixMilli(1_000_000))
	c, _ := th.Cycled(f, time.UnixMilli(1_000_300))
	if a.ANSI(th) != b.ANSI(th) {
		t.Fatal("the same instant painted two different frames")
	}
	if a.ANSI(th) == c.ANSI(th) {
		t.Fatal("the gradient does not move")
	}
	if a.Plain() != f.Plain() || c.Plain() != f.Plain() {
		t.Fatal("a gradient changed the text")
	}
}

// Only cells wearing the token are painted by it: the content inside the frame keeps its
// own style, so an animated border can never animate the input letters.
func TestAGradientBorderLeavesTheContentAlone(t *testing.T) {
	th := loadDir(t, "diagonal", "")
	out, _ := th.Cycled(borderFrame("g"), time.UnixMilli(123456))
	for _, s := range out.Live[1] {
		if strings.Contains(s.Text, "input") && s.Style != "dim" {
			t.Fatalf("the content was restyled to %q", s.Style)
		}
	}
}

// Shades between palette colours are blended for #rrggbb palettes, so the flow is smooth;
// indexed colours stay stepped (their look belongs to the terminal).
func TestRampBlendsRGBAndKeepsIndexedStepped(t *testing.T) {
	rgb := Cycle{Colors: []ui.Color{ui.MustHex("#000000"), ui.MustHex("#ffffff")}, Direction: DirHorizontal}
	if rgb.rampLen() != 2*rampSteps {
		t.Fatalf("rampLen = %d, want %d", rgb.rampLen(), 2*rampSteps)
	}
	if mid := rgb.colorAt(rampSteps / 2); mid.R < 100 || mid.R > 160 {
		t.Errorf("midway between black and white = %v, want a grey", mid)
	}
	if rgb.colorAt(0) != rgb.Colors[0] || rgb.colorAt(rampSteps) != rgb.Colors[1] {
		t.Error("the ramp must hit the palette colours exactly")
	}
	idx := Cycle{Colors: []ui.Color{ui.Idx(1), ui.Idx(4)}, Direction: DirDiagonal}
	if idx.rampLen() != 2 {
		t.Errorf("indexed ramp = %d entries, want 2 (no blending)", idx.rampLen())
	}
}

// "along" (and no direction at all) is the original behaviour, byte for byte.
func TestAlongIsTheOriginalBehaviour(t *testing.T) {
	plain := loadRainbow(t)
	along, err := LoadBytes("t", []byte(strings.Replace(rainbowTheme, `"fps":10`, `"direction":"along","fps":10`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	f := ui.Frame{Live: []ui.Line{{{Text: "max", Style: "rainbow"}}}}
	now := time.UnixMilli(777_000)
	a, _ := plain.Cycled(f, now)
	b, _ := along.Cycled(f, now)
	if a.ANSI(plain) != b.ANSI(along) {
		t.Fatal(`direction "along" must paint exactly what no direction paints`)
	}
}

func TestADirectionThatDoesNotExistIsRefused(t *testing.T) {
	_, err := LoadBytes("t", []byte(`{"cycle":{"x":{"colors":["red","blue"],"direction":"sideways"}}}`))
	if err == nil || !strings.Contains(err.Error(), "radial") {
		t.Fatalf("err = %v, want a refusal that lists the directions", err)
	}
}

func TestDirectionSurvivesTheJSONRoundTrip(t *testing.T) {
	c, err := ParseCycle("x", CycleDef{Colors: []string{"red", "blue"}, Direction: "Diagonal", Spread: 2})
	if err != nil {
		t.Fatal(err)
	}
	if d := c.Def(); d.Direction != "diagonal" || d.Spread != 2 {
		t.Fatalf("Def() = %+v", d)
	}
}

// A frame with two separate frames (two shapes) of the same token is measured as one box;
// a frame with no cell of the token must not crash or change.
func TestPositionalCycleWithNoCellsIsHarmless(t *testing.T) {
	th := loadDir(t, "radial", "")
	f := ui.Frame{Live: []ui.Line{{{Text: "hello", Style: "dim"}}}}
	out, fps := th.Cycled(f, time.Now())
	if fps != 0 || out.Plain() != f.Plain() {
		t.Fatalf("fps=%d", fps)
	}
}

// An angle is the CSS one: 90deg flows left to right, 180deg top to bottom, and 135deg is
// the diagonal. Any angle works, so the model is never told "the system cannot do that".
func TestAnAngleIsADirection(t *testing.T) {
	red, blue := ui.MustHex("#ff0000"), ui.MustHex("#0000ff")
	at := func(dir string) (*Theme, ui.Frame) {
		th := loadDir(t, dir, `,"static":true`)
		out, _ := th.Cycled(borderFrame("g"), time.UnixMilli(0))
		return th, out
	}
	th, out := at("90deg")
	if colourOf(t, th, out, 0, 0) != red || colourOf(t, th, out, 0, 9) != blue || colourOf(t, th, out, 0, 4) != colourOf(t, th, out, 2, 4) {
		t.Error("90deg must run left to right and be the same on every row")
	}
	th, out = at("180deg")
	if colourOf(t, th, out, 0, 3) != red || colourOf(t, th, out, 2, 3) != blue {
		t.Error("180deg must run from the top row (first colour) to the bottom row (last)")
	}
	th, out = at("0deg")
	if colourOf(t, th, out, 2, 3) != red || colourOf(t, th, out, 0, 3) != blue {
		t.Error("0deg must run from the bottom row to the top row")
	}
	th, out = at("135deg")
	if colourOf(t, th, out, 0, 0) != red || colourOf(t, th, out, 2, 9) != blue {
		t.Error("135deg must run from the top-left corner to the bottom-right one")
	}
	th, out = at("30deg")
	if colourOf(t, th, out, 0, 0) == colourOf(t, th, out, 2, 9) {
		t.Error("30deg is a slope of its own: the corners must differ")
	}
	for _, bad := range []string{"deg", "abcdeg", "99999deg", "NaNdeg"} {
		if _, err := LoadBytes("t.json", []byte(`{"cycle":{"g":{"colors":["red","blue"],"direction":"`+bad+`"}}}`)); err == nil {
			t.Errorf("direction %q must be refused", bad)
		}
	}
}

// Conic sweeps round the centre: the top edge is not the bottom edge, and twelve o'clock
// and just before it meet without a jump (the palette is closed into a loop).
func TestConicSweepsRoundTheCentre(t *testing.T) {
	th := loadDir(t, "conic", `,"static":true`)
	out, _ := th.Cycled(borderFrame("g"), time.UnixMilli(0))
	if colourOf(t, th, out, 0, 2) == colourOf(t, th, out, 2, 2) {
		t.Error("a conic gradient must tell the top edge from the bottom one")
	}
	if colourOf(t, th, out, 0, 9) == colourOf(t, th, out, 2, 0) {
		t.Error("opposite corners are half a turn apart and must differ")
	}
}

// Static draws the gradient and asks for no repaint; reverse runs it the other way.
func TestStaticAndReverse(t *testing.T) {
	th := loadDir(t, "diagonal", `,"static":true`)
	a, fps := th.Cycled(borderFrame("g"), time.UnixMilli(0))
	b, _ := th.Cycled(borderFrame("g"), time.UnixMilli(777))
	if fps != 0 {
		t.Errorf("a static gradient asked for %d repaints a second", fps)
	}
	if colourOf(t, th, a, 0, 3) != colourOf(t, th, b, 0, 3) {
		t.Error("a static gradient moved")
	}
	if colourOf(t, th, a, 0, 0) == colourOf(t, th, a, 2, 9) {
		t.Error("a static gradient was not drawn across the shape")
	}

	// Three colours: with two, the loop is its own mirror image and forward and backward
	// land on the same shade.
	three := func(extra string) *Theme {
		th, err := LoadBytes("t.json", []byte(`{"cycle":{"g":{"colors":["#ff0000","#00ff00","#0000ff"],"fps":10,"direction":"diagonal"`+extra+`}}}`))
		if err != nil {
			t.Fatal(err)
		}
		return th
	}
	fw, rv := three(""), three(`,"reverse":true`)
	t0, t1 := time.UnixMilli(0), time.UnixMilli(50)
	f0, _ := fw.Cycled(borderFrame("g"), t0)
	f1, _ := fw.Cycled(borderFrame("g"), t1)
	r0, _ := rv.Cycled(borderFrame("g"), t0)
	r1, _ := rv.Cycled(borderFrame("g"), t1)
	if colourOf(t, fw, f0, 0, 0) == colourOf(t, fw, f1, 0, 0) {
		t.Fatal("the forward gradient did not move")
	}
	if colourOf(t, fw, f1, 0, 0) == colourOf(t, rv, r1, 0, 0) || colourOf(t, fw, f0, 0, 0) != colourOf(t, rv, r0, 0, 0) {
		t.Error("reverse must start where forward does and then travel the other way")
	}
}

// The five-word directions and the angles round-trip through the JSON form.
func TestAngleAndFlagsSurviveTheJSONRoundTrip(t *testing.T) {
	c, err := ParseCycle("g", CycleDef{Colors: []string{"red", "blue"}, Direction: "45deg", Reverse: true, Static: true})
	if err != nil {
		t.Fatal(err)
	}
	d := c.Def()
	if d.Direction != "45deg" || !d.Reverse || !d.Static {
		t.Errorf("Def() = %+v", d)
	}
}
