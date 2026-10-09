package theme

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

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
	// whole span one colour at a time; 1 is a rainbow running along the text.
	Spread int
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
	}
	return nil
}

// ParseCycle reads one cycle from its JSON form and validates it.
func ParseCycle(name string, def CycleDef) (Cycle, error) {
	c := Cycle{FPS: def.FPS, Spread: def.Spread}
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
	d := CycleDef{FPS: c.FPS, Spread: c.Spread}
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
	s.FG = c.Colors[n%len(c.Colors)]
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
	out = f
	out.Committed = t.cycleLines(f.Committed, now, &fps)
	out.Live = t.cycleLines(f.Live, now, &fps)
	if fps == 0 {
		return f, 0
	}
	return out, fps
}

func (t *Theme) cycleLines(lines []ui.Line, now time.Time, fps *int) []ui.Line {
	var out []ui.Line
	for i, l := range lines {
		nl, changed := t.cycleLine(l, now, fps)
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

func (t *Theme) cycleLine(l ui.Line, now time.Time, fps *int) (ui.Line, bool) {
	var out ui.Line
	changed := false
	for i, s := range l {
		c, ok := t.cycles[s.Style]
		if !ok || s.Text == "" {
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
		step := int(now.UnixMilli() * int64(c.rate()) / 1000)
		// Text already carrying escape sequences is left whole: cutting inside one would
		// print its tail as text.
		if c.Spread == 0 || strings.ContainsRune(s.Text, '\x1b') {
			s.Style += cycleSep + strconv.Itoa(step%len(c.Colors))
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
