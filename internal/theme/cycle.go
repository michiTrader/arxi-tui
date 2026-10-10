package theme

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

// A cycle is a token that does not hold one colour but a palette the host walks through
// as time passes: a multicolour animation that any node, menu row or status word can wear
// by naming the token, exactly as it names `dim`.
//
//	"rainbow": { "colors": ["#ff0000", "#ffa500", "#ffff00", "#00c800", "#0080ff", "#a020f0"],
//	             "attrs": ["bold"], "fps": 10, "spread": 1 }
//
// Where it lives. A cycle is declared in a theme's reserved `cycle` section (beside
// `anim`, and for the same reason: it is not a plain style, so it must not be mis-read as
// one) and is also a style token of the same name, so every place that validates "does
// this token exist" accepts it with no second rule. Its resting look, the one a golden or
// a pipe sees, is the first colour: the factory scenes are byte-identical whether or not a
// theme defines cycles.
//
// Where it moves. The theme owns what a cycle looks like at step N; the host owns the
// clock. Cycled turns a rendered frame into the frame for one instant by replacing each
// cycling span with spans that name a derived token (`name\x00N`, see Resolve), so the
// emitter needs no new concept: it resolves a token name to a style, as it always did.
//
// A cycle can only change colours and attributes. It cannot move a cell, resize a row or
// write a character, so an animation chosen by a user can make the interface louder but
// never wrong.

// Cycle is one animated palette.
type Cycle struct {
	// Colors are the palette, walked in order and wrapped. At least two: one colour is
	// just a token.
	Colors []ui.Color
	// Attrs are the attributes the span wears whatever step it is on.
	Attrs ui.Attr
	// FPS is how many palette steps pass in a second. 0 means DefaultCycleFPS.
	FPS int
	// Spread is how many palette steps separate neighbouring characters. 0 paints the
	// whole span one colour at a time; 1 is a rainbow running along the text. With a
	// Direction other than DirAlong it is instead how many times the palette is laid
	// across the whole shape, first colour to last (0 and 1 both mean once).
	Spread int
	// Direction is where the colours flow. DirAlong (the default) runs them along each
	// span's text, one row at a time. The others look at where a cell is on screen, so a
	// frame drawn with the token gets one gradient across its whole shape: top and
	// bottom edges differ, and the corners line up. See Direction* below.
	Direction string
	// Reverse runs the motion the other way round (the colours keep their order across
	// the shape; they travel backwards through it).
	Reverse bool
	// Static lays the gradient across the shape and leaves it there: it never moves, so
	// it asks the host for no repaint at all.
	Static bool
}

// Directions a cycle can flow in. All but DirAlong measure each cell against the box
// that encloses every cell wearing the token in the frame, and count a row as two
// columns because a terminal cell is about twice as tall as it is wide.
const (
	// DirAlong runs the palette along the text of each span (the original behaviour).
	DirAlong = "along"
	// DirHorizontal changes colour from left to right and is the same on every row.
	DirHorizontal = "horizontal"
	// DirVertical changes colour from top to bottom.
	DirVertical = "vertical"
	// DirDiagonal flows from the top-left corner to the bottom-right one.
	DirDiagonal = "diagonal"
	// DirAntiDiagonal flows from the bottom-left corner to the top-right one.
	DirAntiDiagonal = "antidiagonal"
	// DirRadial flows outward from the centre of the shape.
	DirRadial = "radial"
	// DirConic sweeps round the centre like the hand of a clock, starting at twelve
	// o'clock; the palette closes into a loop so the seam shows no jump.
	DirConic = "conic"
)

// Directions lists every accepted direction, for messages. Besides these words a
// direction may be an angle, "<N>deg", with the CSS meaning: 0deg flows bottom to top,
// 90deg left to right, 180deg top to bottom, 270deg right to left, and anything between
// along that slope (135deg is top-left to bottom-right, the same as diagonal).
var Directions = []string{DirAlong, DirHorizontal, DirVertical, DirDiagonal, DirAntiDiagonal, DirRadial, DirConic, "<N>deg"}

// angleOf reads a direction written as an angle ("135deg", "-30deg", "12.5deg").
func angleOf(d string) (float64, bool) {
	num, ok := strings.CutSuffix(d, "deg")
	if !ok || num == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(num, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < -3600 || v > 3600 {
		return 0, false
	}
	return v, true
}

// rampSteps is how many shades are blended between two neighbouring palette colours when
// a cycle flows across a shape and all its colours are #rrggbb. Indexed colours cannot be
// blended (their look is the terminal's), so they keep one step.
const rampSteps = 16

// Positional reports whether the cycle looks at where a cell is on screen.
func (c Cycle) Positional() bool { return c.positional() }

// positional reports whether the cycle looks at where a cell is on screen.
func (c Cycle) positional() bool {
	switch c.Direction {
	case DirHorizontal, DirVertical, DirDiagonal, DirAntiDiagonal, DirRadial, DirConic:
		return true
	}
	_, ok := angleOf(c.Direction)
	return ok
}

// steps is how many ramp entries lie between two neighbouring palette colours.
func (c Cycle) steps() int {
	if !c.positional() {
		return 1
	}
	for _, col := range c.Colors {
		if col.Kind != ui.ColorRGB {
			return 1
		}
	}
	return rampSteps
}

// rampLen is the number of entries in the whole (closed) ramp.
func (c Cycle) rampLen() int { return len(c.Colors) * c.steps() }

// colorAt is ramp entry n: a palette colour, or a blend of two neighbouring ones.
func (c Cycle) colorAt(n int) ui.Color {
	k, st := len(c.Colors), c.steps()
	n = ((n % (k * st)) + k*st) % (k * st)
	a, f := n/st, n%st
	if f == 0 {
		return c.Colors[a]
	}
	b := c.Colors[(a+1)%k]
	from := c.Colors[a]
	t := float64(f) / float64(st)
	mix := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t)) }
	return ui.Color{Kind: ui.ColorRGB, R: mix(from.R, b.R), G: mix(from.G, b.G), B: mix(from.B, b.B)}
}

const (
	// DefaultCycleFPS is the step rate a cycle that names none takes.
	DefaultCycleFPS = 8
	// MaxCycleFPS caps the rate: a terminal cannot show more, and a repaint loop asked
	// for more would only burn the CPU the user is typing on.
	MaxCycleFPS = 30
	// MaxCycleColors and MaxCycleSpread bound what one token may ask for.
	MaxCycleColors = 64
	MaxCycleSpread = 16
)

// CycleDef is the JSON form of a Cycle: what a theme's `cycle` section and a user's
// animations are written in.
type CycleDef struct {
	Colors []string `json:"colors"`
	Attrs  []string `json:"attrs,omitempty"`
	FPS    int      `json:"fps,omitempty"`
	Spread int      `json:"spread,omitempty"`
	// Direction is where the colours flow: along (the default), horizontal, vertical,
	// diagonal, antidiagonal or radial. See the Dir* constants.
	Direction string `json:"direction,omitempty"`
	// Reverse runs the motion backwards; Static stops it (a fixed gradient).
	Reverse bool `json:"reverse,omitempty"`
	Static  bool `json:"static,omitempty"`
}

// rate is the cycle's step rate with the default filled in.
func (c Cycle) rate() int {
	if c.FPS <= 0 {
		return DefaultCycleFPS
	}
	return c.FPS
}

// base is the style the token has at rest: the first colour and the attributes. It is
// what a frame that is not being animated shows, and what Has/Resolve answer for the
// bare name.
func (c Cycle) base() ui.Style {
	s := ui.Style{Attrs: c.Attrs}
	if len(c.Colors) > 0 {
		s.FG = c.Colors[0]
	}
	return s
}

// Validate refuses a cycle that would not draw what it says. name is the token's, so a
// theme with several says which one is wrong.
func (c Cycle) Validate(name string) error {
	switch {
	case len(c.Colors) < 2:
		return fmt.Errorf("cycle %q has %d colour(s); an animated token needs at least two colours to move between (a single colour is an ordinary token)", name, len(c.Colors))
	case len(c.Colors) > MaxCycleColors:
		return fmt.Errorf("cycle %q has %d colours; the most one token may walk through is %d", name, len(c.Colors), MaxCycleColors)
	case c.FPS < 0 || c.FPS > MaxCycleFPS:
		return fmt.Errorf("cycle %q asks for fps %d; use 1-%d (omit it for %d)", name, c.FPS, MaxCycleFPS, DefaultCycleFPS)
	case c.Spread < 0 || c.Spread > MaxCycleSpread:
		return fmt.Errorf("cycle %q asks for spread %d; use 0-%d (0 paints the whole span one colour, 1 runs a rainbow along it)", name, c.Spread, MaxCycleSpread)
	case !validDirection(c.Direction):
		return fmt.Errorf("cycle %q asks for direction %q; use one of %s", name, c.Direction, strings.Join(Directions, ", "))
	}
	return nil
}

func validDirection(d string) bool {
	if d == "" {
		return true
	}
	for _, k := range Directions {
		if d == k {
			return true
		}
	}
	_, ok := angleOf(d)
	return ok
}

// ParseCycle reads one cycle from its JSON form and validates it.
func ParseCycle(name string, def CycleDef) (Cycle, error) {
	c := Cycle{FPS: def.FPS, Spread: def.Spread, Direction: strings.ToLower(strings.TrimSpace(def.Direction)),
		Reverse: def.Reverse, Static: def.Static}
	for i, s := range def.Colors {
		col, err := ui.ParseColor(s)
		if err != nil {
			return Cycle{}, fmt.Errorf("cycle %q colour %d: %w", name, i+1, err)
		}
		c.Colors = append(c.Colors, col)
	}
	for _, a := range def.Attrs {
		at, err := parseAttr(a)
		if err != nil {
			return Cycle{}, fmt.Errorf("cycle %q: %w", name, err)
		}
		c.Attrs |= at
	}
	return c, c.Validate(name)
}

// Def is the JSON form of c, so a cycle can be written back to a file and shown to the
// user in the words they would type.
func (c Cycle) Def() CycleDef {
	d := CycleDef{FPS: c.FPS, Spread: c.Spread, Direction: c.Direction, Reverse: c.Reverse, Static: c.Static}
	for _, col := range c.Colors {
		d.Colors = append(d.Colors, col.String())
	}
	for _, a := range strings.Fields(ui.Style{Attrs: c.Attrs}.String()) {
		d.Attrs = append(d.Attrs, a)
	}
	return d
}

// cycleSep joins a cycle token and a palette index in a derived token. NUL cannot appear
// in a token a person types or a scene names, so a derived name can never collide with
// one.
const cycleSep = "\x00"

// Cycle looks up an animated token by name.
func (t *Theme) Cycle(name string) (Cycle, bool) {
	if t == nil || t.cycles == nil {
		return Cycle{}, false
	}
	c, ok := t.cycles[name]
	return c, ok
}

// CycleNames lists the animated tokens, sorted.
func (t *Theme) CycleNames() []string {
	if t == nil {
		return nil
	}
	out := make([]string, 0, len(t.cycles))
	for n := range t.cycles {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// WithCycles returns a copy of t that also defines the given animated tokens. Each is
// registered as a style token of the same name (its resting look) so every reference
// check that asks "is this token defined" accepts it. t is not changed: the active theme
// is recomposed from its layers whenever one of them changes. A cycle replaces a plain
// token of the same name, the way any later layer does.
func (t *Theme) WithCycles(cycles map[string]Cycle) *Theme {
	out := Merge(t, nil)
	for n, c := range cycles {
		out.cycles[n] = c
		out.tokens[n] = c.base()
	}
	return out
}

// derived resolves `name\x00N`: the token's resting style with the foreground of palette
// entry N. ok is false when the name is not a derived one.
func (t *Theme) derived(name string) (ui.Style, bool) {
	i := strings.Index(name, cycleSep)
	if i <= 0 || t == nil {
		return ui.Style{}, false
	}
	c, ok := t.cycles[name[:i]]
	if !ok || len(c.Colors) == 0 {
		return ui.Style{}, false
	}
	n, err := strconv.Atoi(name[i+1:])
	if err != nil || n < 0 {
		return ui.Style{}, false
	}
	s := c.base()
	s.FG = c.colorAt(n)
	return s, true
}

// Cycled returns the frame as it looks at now: every span whose style token is a cycle
// is replaced by spans naming the derived token for the palette step that instant lands
// on. fps is the fastest rate among the cycles the frame actually uses, 0 when it uses
// none; the host keeps its repaint ticker running at that rate and stops it when it is 0,
// so an idle interface costs nothing.
//
// The input frame is not changed. Only the style of a span changes, never its text or
// its width, so a layout that fits still fits.
func (t *Theme) Cycled(f ui.Frame, now time.Time) (out ui.Frame, fps int) {
	if t == nil || len(t.cycles) == 0 {
		return f, 0
	}
	geo := t.cycleShapes(f)
	out = f
	out.Committed = t.cycleLines(f.Committed, 0, now, &fps, geo)
	out.Live = t.cycleLines(f.Live, len(f.Committed), now, &fps, geo)
	if fps == 0 {
		return f, 0
	}
	return out, max(fps, 0)
}

// shape is the box enclosing every cell that wears one positional cycle in a frame, in
// cells (columns) and rows. It is what a direction measures against, so the same border
// flows the same way wherever it sits on screen.
type shape struct {
	c0, c1, r0, r1 int
	set            bool
}

func (b *shape) add(c0, c1, r int) {
	if !b.set {
		*b = shape{c0, c1, r, r, true}
		return
	}
	b.c0, b.c1 = min(b.c0, c0), max(b.c1, c1)
	b.r0, b.r1 = min(b.r0, r), max(b.r1, r)
}

// rowCells is how many columns one row counts for: a terminal cell is about twice as
// tall as it is wide, so a step down looks twice as far as a step across.
const rowCells = 2

// flow is a direction made ready to measure with: the words are fixed, an angle is turned
// into the unit step it points along once, not once per cell.
type flow struct {
	dir    string
	sx, sy float64 // linear: the step along the slope, in columns (x) and half-rows (y)
	linear bool
}

func flowOf(dir string) flow {
	f := flow{dir: dir}
	if deg, ok := angleOf(dir); ok {
		rad := deg * math.Pi / 180
		// CSS: 0deg points up, 90deg right. y grows downward on a screen.
		f.sx, f.sy, f.linear = math.Sin(rad), -math.Cos(rad), true
	}
	return f
}

// span is the lowest and highest value of the dot product over the box's four corners:
// a linear flow starts at the corner it points away from.
func (f flow) span(b shape) (lo, hi float64) {
	w, h := float64(b.c1-b.c0), float64((b.r1-b.r0)*rowCells)
	for i, v := range []float64{0, w * f.sx, h * f.sy, w*f.sx + h*f.sy} {
		if i == 0 || v < lo {
			lo = v
		}
		if i == 0 || v > hi {
			hi = v
		}
	}
	return lo, hi
}

// extent is the largest position the cycle's direction measures inside the shape; the
// ramp runs once (or Spread times) over it.
func (f flow) extent(b shape) float64 {
	w, h := float64(b.c1-b.c0), float64((b.r1-b.r0)*rowCells)
	var e float64
	switch {
	case f.linear:
		lo, hi := f.span(b)
		e = hi - lo
	case f.dir == DirHorizontal:
		e = w
	case f.dir == DirVertical:
		e = h
	case f.dir == DirDiagonal, f.dir == DirAntiDiagonal:
		e = w + h
	case f.dir == DirRadial:
		e = math.Hypot(w/2, h/2)
	case f.dir == DirConic:
		e = 2 * math.Pi
	}
	if e < 1e-9 || (e < 1 && !(f.dir == DirConic)) {
		return 1
	}
	return e
}

// pos is where the cell at column c, row r sits along the direction, from 0 to extent.
func (f flow) pos(b shape, c, r int) float64 {
	dc, dr := float64(c-b.c0), float64((r-b.r0)*rowCells)
	if f.linear {
		lo, _ := f.span(b)
		return dc*f.sx + dr*f.sy - lo
	}
	switch f.dir {
	case DirHorizontal:
		return dc
	case DirVertical:
		return dr
	case DirDiagonal:
		return dc + dr
	case DirAntiDiagonal:
		return dc + (float64((b.r1-b.r0)*rowCells) - dr)
	case DirRadial:
		w, h := float64(b.c1-b.c0)/2, float64((b.r1-b.r0)*rowCells)/2
		return math.Hypot(dc-w, dr-h)
	case DirConic:
		w, h := float64(b.c1-b.c0)/2, float64((b.r1-b.r0)*rowCells)/2
		a := math.Atan2(dc-w, -(dr - h)) // from twelve o'clock, clockwise
		if a < 0 {
			a += 2 * math.Pi
		}
		return a
	}
	return 0
}

// cycleShapes measures, for each positional cycle the frame uses, the box around its
// cells. It is empty (and costs one pass over the spans) when no cycle is positional.
func (t *Theme) cycleShapes(f ui.Frame) map[string]*shape {
	var geo map[string]*shape
	scan := func(lines []ui.Line, rowBase int) {
		for i, l := range lines {
			col := 0
			for _, s := range l {
				w := ansi.StringWidth(s.Text)
				if c, ok := t.cycles[s.Style]; ok && c.positional() && w > 0 {
					if geo == nil {
						geo = map[string]*shape{}
					}
					b := geo[s.Style]
					if b == nil {
						b = &shape{}
						geo[s.Style] = b
					}
					b.add(col, col+w-1, rowBase+i)
				}
				col += w
			}
		}
	}
	for _, c := range t.cycles {
		if c.positional() {
			scan(f.Committed, 0)
			scan(f.Live, len(f.Committed))
			break
		}
	}
	return geo
}

func (t *Theme) cycleLines(lines []ui.Line, rowBase int, now time.Time, fps *int, geo map[string]*shape) []ui.Line {
	var out []ui.Line
	for i, l := range lines {
		nl, changed := t.cycleLine(l, rowBase+i, now, fps, geo)
		if changed && out == nil {
			out = make([]ui.Line, len(lines))
			copy(out, lines[:i])
		}
		if out != nil {
			out[i] = nl
		}
	}
	if out == nil {
		return lines
	}
	return out
}

func (t *Theme) cycleLine(l ui.Line, row int, now time.Time, fps *int, geo map[string]*shape) (ui.Line, bool) {
	var out ui.Line
	changed := false
	col := 0
	for i, s := range l {
		width := ansi.StringWidth(s.Text)
		c, ok := t.cycles[s.Style]
		if !ok || s.Text == "" {
			col += width
			if changed {
				out = append(out, s)
			}
			continue
		}
		if !changed {
			changed = true
			out = append(out, l[:i]...)
		}
		// A static gradient is drawn but asks for no repaint: fps stays at -1 (used, not
		// moving) unless something that does move is on screen too.
		switch r := c.rate(); {
		case c.Static:
			if *fps == 0 {
				*fps = -1
			}
		case r > *fps:
			*fps = r
		}
		// Text already carrying escape sequences is left whole: cutting inside one would
		// print its tail as text.
		whole := strings.ContainsRune(s.Text, '\x1b')
		if b := geo[s.Style]; c.positional() && b != nil && !whole {
			// One shade per cell, from where the cell is in the shape.
			steps, total := c.steps(), c.rampLen()
			// The shape shows the palette once, first colour to last (Spread times when
			// asked), so its two ends differ; time slides it round the closed loop, which
			// is what keeps the motion free of any jump.
			window := float64(max(c.Spread, 1) * (len(c.Colors) - 1) * steps)
			if c.Direction == DirConic {
				window = float64(max(c.Spread, 1) * len(c.Colors) * steps) // a closed loop
			}
			tick := int(now.UnixMilli() * int64(c.rate()) * int64(steps) / 1000)
			switch {
			case c.Static:
				tick = 0
			case c.Reverse:
				tick = -tick
			}
			fl := flowOf(c.Direction)
			ext, at := fl.extent(*b), col
			for _, r := range s.Text {
				cell := string(r)
				space := int(math.Round(fl.pos(*b, at, row) / ext * window))
				out = append(out, ui.Span{Text: cell, Style: s.Style + cycleSep + strconv.Itoa(((tick-space)%total+total)%total), Fill: s.Fill})
				at += ansi.StringWidth(cell)
			}
			col += width
			continue
		}
		col += width
		step := int(now.UnixMilli() * int64(c.rate()) / 1000)
		switch {
		case c.Static:
			step = 0
		case c.Reverse:
			step = -step
		}
		step = ((step % len(c.Colors)) + len(c.Colors)) % len(c.Colors)
		if c.positional() || c.Spread == 0 || whole {
			s.Style += cycleSep + strconv.Itoa(step%len(c.Colors)*c.steps())
			out = append(out, s)
			continue
		}
		for j, r := range []rune(s.Text) {
			out = append(out, ui.Span{
				Text:  string(r),
				Style: s.Style + cycleSep + strconv.Itoa((step+j*c.Spread)%len(c.Colors)),
				Fill:  s.Fill,
			})
		}
	}
	if !changed {
		return l, false
	}
	return out, true
}
