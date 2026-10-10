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
)

// Directions lists every accepted direction, for messages.
var Directions = []string{DirAlong, DirHorizontal, DirVertical, DirDiagonal, DirAntiDiagonal, DirRadial}

// rampSteps is how many shades are blended between two neighbouring palette colours when
// a cycle flows across a shape and all its colours are #rrggbb. Indexed colours cannot be
// blended (their look is the terminal's), so they keep one step.
const rampSteps = 16

// positional reports whether the cycle looks at where a cell is on screen.
func (c Cycle) positional() bool {
	switch c.Direction {
	case DirHorizontal, DirVertical, DirDiagonal, DirAntiDiagonal, DirRadial:
		return true
	}
	return false
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
	return false
}

// ParseCycle reads one cycle from its JSON form and validates it.
func ParseCycle(name string, def CycleDef) (Cycle, error) {
	c := Cycle{FPS: def.FPS, Spread: def.Spread, Direction: strings.ToLower(strings.TrimSpace(def.Direction))}
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
	d := CycleDef{FPS: c.FPS, Spread: c.Spread, Direction: c.Direction}
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
	return out, fps
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

// extent is the largest position the cycle's direction measures inside the shape; the
// ramp runs once (or Spread times) over it.
func (b shape) extent(dir string) float64 {
	w, h := float64(b.c1-b.c0), float64((b.r1-b.r0)*rowCells)
	var e float64
	switch dir {
	case DirHorizontal:
		e = w
	case DirVertical:
		e = h
	case DirDiagonal, DirAntiDiagonal:
		e = w + h
	case DirRadial:
		e = math.Hypot(w/2, h/2)
	}
	if e < 1 {
		return 1
	}
	return e
}

// pos is where the cell at column c, row r sits along the direction, from 0 to extent.
func (b shape) pos(dir string, c, r int) float64 {
	dc, dr := float64(c-b.c0), float64((r-b.r0)*rowCells)
	switch dir {
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
		if r := c.rate(); r > *fps {
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
			tick := int(now.UnixMilli() * int64(c.rate()) * int64(steps) / 1000)
			ext, at := b.extent(c.Direction), col
			for _, r := range s.Text {
				cell := string(r)
				space := int(math.Round(b.pos(c.Direction, at, row) / ext * window))
				out = append(out, ui.Span{Text: cell, Style: s.Style + cycleSep + strconv.Itoa(((tick-space)%total+total)%total), Fill: s.Fill})
				at += ansi.StringWidth(cell)
			}
			col += width
			continue
		}
		col += width
		step := int(now.UnixMilli() * int64(c.rate()) / 1000)
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
