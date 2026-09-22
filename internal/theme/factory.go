package theme

import "github.com/michiTrader/arxi_tui/internal/ui"

// Factory returns the compiled-in sobria theme. This is the backstop when every
// user-supplied theme fails to load. It defines the tokens the three golden
// scenes (RAW, SOBRIA, MAXIMUM) reference, plus the three the change-diff view
// (PLAN.md ADR-0003) generates -- that view is a host scene like any other, and
// the backstop theme has to render it when a downloaded theme is what failed.
//
// Sobria is no-color emphasis: dim for de-emphasis, bold for headers. The diff
// tokens stay colourless for the same reason (meaning by attribute and column,
// not red/green). Light/dark auto-detection lives in the emitter
// (internal/term), not here -- the theme says what to emphasize, and the
// terminal backend decides how.
func Factory() *Theme {
	return FromMap(map[string]ui.Style{
		"dim":               {Attrs: ui.AttrDim},
		"header":            {Attrs: ui.AttrBold},
		"input.placeholder": {Attrs: ui.AttrDim},
		"banner":            {Attrs: ui.AttrBold},
		"diff.context":      {Attrs: ui.AttrDim},
		"diff.del":          {Attrs: ui.AttrStrike},
		"diff.add":          {Attrs: ui.AttrBold},
	})
}
