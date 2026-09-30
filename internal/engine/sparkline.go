package engine

import (
	"encoding/json"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

// sparkLevels are the eight block glyphs a sparkline maps a value onto, lowest
// to highest. Eight is the resolution the Unicode block-element run gives for
// free (U+2581..U+2588); a scene author reads a trend at a glance, not a value,
// so more resolution than the eye resolves in one terminal cell buys nothing.
var sparkLevels = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// parseSeries decodes the string the plugin store hands the resolver back into
// the numeric axis a sparkline draws. store.renderValue projects a `series`
// wire value to its compact JSON form — "[1,3,2]" — precisely so this node can
// parse it (store.go says so at the projection site); the engine decodes a
// plugin's JSON in exactly this one place, for the one kind whose value is not
// a display string, rather than teaching the store to pre-render a sparkline it
// cannot size to the pane.
//
// The bool is false for anything that is not a JSON array of numbers: the
// unresolved-bind placeholder "[…]", a bind that resolves to a scalar text
// value (a scene author pointed a sparkline at the wrong field), or a malformed
// frame the store somehow admitted. The caller draws that string verbatim
// rather than an empty or a zeroed chart, so "waiting" and "misconfigured" stay
// visible instead of masquerading as a flat line at zero.
func parseSeries(s string) ([]float64, bool) {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "[") {
		return nil, false
	}
	var nums []float64
	if err := json.Unmarshal([]byte(trimmed), &nums); err != nil {
		return nil, false
	}
	return nums, true
}

// sparkline renders a numeric series as a single row of block glyphs, scaled so
// the series minimum sits on the lowest glyph and its maximum on the highest.
// The scale is relative to the series itself, not an absolute axis: a sparkline
// shows a shape, and a shape the reader can see requires the data to fill the
// glyph range rather than clustering near one absolute floor.
//
// A flat series (every value equal, including a single point) has no shape, so
// there is no honest way to place it on a min..max ramp — max==min makes the
// ratio undefined. It draws on the lowest glyph: a flat line low is the reading
// the `spark` convention established, and it cannot be mistaken for the tall
// bars a varying series produces. A mid glyph would read as "half of something"
// when the truth is "no change".
//
// width caps the number of glyphs. When the series is longer than the pane the
// LAST width values are shown: a live series (a plugin's price ticks, I3) grows
// at the end, and the newest samples are the ones a watcher came for, so the
// chart scrolls the way a terminal does rather than freezing on the oldest.
func sparkline(nums []float64, width int) string {
	if width <= 0 || len(nums) == 0 {
		return ""
	}
	if len(nums) > width {
		nums = nums[len(nums)-width:]
	}

	lo, hi := nums[0], nums[0]
	for _, v := range nums[1:] {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}

	var b strings.Builder
	span := hi - lo
	for _, v := range nums {
		level := 0
		if span > 0 {
			// Scale into [0, len-1]. The +0.5 rounds to the nearest glyph rather
			// than truncating, so the top value reaches the tallest block and the
			// bottom the shortest instead of everything biasing one glyph low.
			level = int((v-lo)/span*float64(len(sparkLevels)-1) + 0.5)
			if level < 0 {
				level = 0
			}
			if level >= len(sparkLevels) {
				level = len(sparkLevels) - 1
			}
		}
		b.WriteRune(sparkLevels[level])
	}
	return b.String()
}

// renderSparkline draws a sparkline node (BINDS.md §4.4 `series` kind). It reads
// its series through the same resolveBindRow chokepoint every other bound node
// uses — so a `series` bind published by a plugin (I3), mocked in preview (J1),
// or referenced inside a row scope resolves by the one set of rules — and it
// takes its style token off n.Style exactly as renderText does, which is why a
// focused sparkline glows and a `when`-gated one hides without any code here:
// renderNode applied focusGlowed/hiddenByWhenRow before dispatching to this
// method, the chokepoint discipline the styling and when sweeps depend on.
func (r *Renderer) renderSparkline(n *scene.Node, state fold.State) ui.Frame {
	style := styleName(n.Style)
	value := resolveBindRow(n.Bind, state, r.curRow, r.PluginValues, r.PreviewMocks)

	nums, ok := parseSeries(value)
	if !ok {
		// Not a series: the unresolved placeholder "[…]" (no plugin mounted yet,
		// the honest "waiting" state) or a misconfigured bind. Draw the resolved
		// string as-is so the diagnosis is on screen, the placeholder-not-crash
		// rule a bound node owes (§I-G).
		return ui.Frame{
			Live:   []ui.Line{{ui.Span{Text: value, Style: style}}},
			Width:  r.Width,
			Height: 1,
		}
	}

	chart := sparkline(nums, r.Width)
	// A resolved-but-empty series ("[]") produces no glyphs. Clip a longer chart
	// to the pane the way renderText clips its content, so a wide series never
	// overflows a single-line node.
	if r.Width > 0 {
		if w := ansi.StringWidth(chart); w > r.Width {
			chart = ansi.Cut(chart, 0, r.Width)
		}
	}
	return ui.Frame{
		Live:   []ui.Line{{ui.Span{Text: chart, Style: style}}},
		Width:  r.Width,
		Height: 1,
	}
}
