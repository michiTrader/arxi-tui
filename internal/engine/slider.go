package engine

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

// sliderTrack draws a value on a horizontal track: a run of track glyphs with a
// single knob placed at `fraction` of the way across. It is the slider's whole
// picture, factored out of renderSlider so the geometry can be pinned without a
// fold or a theme in scope — the same split sparkline() makes from
// renderSparkline, and for the same reason: the placement arithmetic is where an
// off-by-one hides and a table test is the only cheap proof it does not.
//
// fraction is clamped to [0,1] rather than refused out of range. A slider is a
// value control (Q11 pairs it with switch, the boolean one), and the reading a
// clamp gives — knob pinned to an end — is the honest picture of a value that
// has walked past its range, whereas an error or an empty track would hide the
// out-of-range value behind a diagnostic. min/max are not Node fields, so [0,1]
// is the one range the format can express today without inventing vocabulary the
// scenes do not yet sign; a value already normalised by whatever wrote the bind
// is what this draws.
//
// The knob rounds to the nearest cell (+0.5) rather than truncating, so 1.0
// reaches the last cell and 0.0 the first instead of both biasing left; this is
// the same nearest-cell rounding sparkline uses to make its top value reach the
// tallest glyph. A width of one collapses to the knob alone: there is no track
// to show a position against, but drawing the knob still distinguishes "a slider
// is here" from an empty cell.
func sliderTrack(fraction float64, width int) string {
	if width <= 0 {
		return ""
	}
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	knob := int(fraction*float64(width-1) + 0.5)
	if knob < 0 {
		knob = 0
	}
	if knob >= width {
		knob = width - 1
	}
	var b strings.Builder
	for i := 0; i < width; i++ {
		if i == knob {
			b.WriteRune('●')
		} else {
			b.WriteRune('─')
		}
	}
	return b.String()
}

// renderSlider draws a scalar value as a knob on a track: the value half of the
// pair Q11 forced into v0 (SCENES.md Scene 5), where switch is the boolean half.
// renderSwitch frames a state you flip; renderSlider frames a value you move, and
// the two read differently on purpose — a `[x]` is on or off, a knob is somewhere
// along a range — so a settings pane that mixes them by row.kind (Scene 5's
// `/config` dogfood) tells a toggle from a level at a glance.
//
// It reads its value through the same resolveBindRow chokepoint every other bound
// node uses, so a slider bound to row.level inside a template resolves off its own
// row scope, a plugin-published value (I3) resolves through the store, and a
// preview mock (J1) substitutes — one set of rules, no second resolver here. The
// value is a fraction in [0,1]; parseFraction rejects anything that is not a
// number, and a rejected value draws verbatim rather than snapping the knob to
// zero, the placeholder-not-crash rule a bound node owes (§I-G) and the exact
// choice renderSparkline makes for a non-series bind: "waiting" ("[…]") and
// "misconfigured" (a scalar pointed at the wrong field) stay legible instead of
// masquerading as a real value pinned to the left.
//
// A `text` label is optional and drawn before the track, exactly as renderSwitch
// draws its label, so a lone slider node can name its own setting while a
// row_template that composes the label from a sibling text node leaves it empty
// and gets the bare track. styleName(n.Style) is applied for the reason every leaf
// in this family applies it: the declared token, and the focus glow rewritten into
// n.Style at the renderNode chokepoint, must land on the one span this node draws,
// or a styled or focused slider silently loses its token — the own-style defect
// this package has paid for four times and the own-style sweep now guards.
func (r *Renderer) renderSlider(n *scene.Node, state fold.State) ui.Frame {
	style := styleName(n.Style)
	value := resolveBindRow(n.Bind, state, r.curRow, r.PluginValues, r.PreviewMocks)

	fraction, ok := parseFraction(value)
	if !ok {
		return ui.Frame{
			Live:   []ui.Line{{ui.Span{Text: value, Style: style}}},
			Width:  r.Width,
			Height: 1,
		}
	}

	// The label eats into the width the track gets, so a labelled slider never
	// overflows the pane the way an unlabelled one filling r.Width does not: the
	// track is what is left after the label and its separating space, and a pane
	// too narrow for even one track cell falls back to the label alone rather
	// than a negative-width track.
	label := n.Text
	trackWidth := r.Width
	if label != "" {
		trackWidth = r.Width - ansi.StringWidth(label) - 1
	}
	track := sliderTrack(fraction, trackWidth)
	text := track
	if label != "" {
		if track == "" {
			text = label
		} else {
			text = label + " " + track
		}
	}
	return ui.Frame{
		Live:   []ui.Line{{ui.Span{Text: text, Style: style}}},
		Width:  r.Width,
		Height: 1,
	}
}

// parseFraction decodes the scalar a slider draws. It is the scalar analogue of
// parseSeries: the bool is false for anything strconv.ParseFloat rejects — the
// unresolved-bind placeholder "[…]", a bind resolving to non-numeric text (a
// scene author pointed a slider at the wrong field), or an empty value — so the
// caller can draw that string verbatim instead of a knob pinned to zero. A NaN or
// an infinity is rejected too: neither has a place on a [0,1] track, and letting
// one through would put the knob at an arithmetic-defined cell that reads as a
// real value.
func parseFraction(s string) (float64, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, false
	}
	if f != f { // NaN
		return 0, false
	}
	if f > 1e308 || f < -1e308 { // ±Inf and anything ParseFloat overflowed to it
		return 0, false
	}
	return f, true
}
