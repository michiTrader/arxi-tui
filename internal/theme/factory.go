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
		// The product mark "Δr×i" is the one coloured thing on the first row: a
		// deep-orange -> amber -> yellow gradient spread over its four letters, one token per
		// letter so no node needs gradient support and the rest of the row stays dim.
		// The command menu is drawn in explicit greys rather than the terminal's faint
		// attribute, which several terminals render too dark to read on black. The
		// resting name is a light grey, the description a step darker; the highlighted
		// row is pure white for the name and a light grey for the description. Tabs
		// and the count are mid grey, the active tab white, and the rules that frame
		// the menu a very dark grey so they delimit without competing.
		"menu.name":          {FG: ui.MustHex("#b4b4b4")},
		"menu.name.selected": {FG: ui.MustHex("#ffffff"), Attrs: ui.AttrBold},
		"menu.desc":          {FG: ui.MustHex("#8a8a8a")},
		"menu.desc.selected": {FG: ui.MustHex("#cfcfcf")},
		"menu.tab":           {FG: ui.MustHex("#8a8a8a")},
		"menu.tab.selected":  {FG: ui.MustHex("#ffffff"), Attrs: ui.AttrBold},
		"menu.rule":          {FG: ui.MustHex("#303030")},
		"menu.hint":          {FG: ui.MustHex("#8a8a8a")},
		"brand.1":            {FG: ui.MustHex("#ff6b1a"), Attrs: ui.AttrBold},
		"brand.2":            {FG: ui.MustHex("#ff9a1f"), Attrs: ui.AttrBold},
		"brand.3":            {FG: ui.MustHex("#ffbf26"), Attrs: ui.AttrBold},
		"brand.4":            {FG: ui.MustHex("#ffe14d"), Attrs: ui.AttrBold},
		"diff.context":       {Attrs: ui.AttrDim},
		"diff.del":           {Attrs: ui.AttrStrike},
		"diff.add":           {Attrs: ui.AttrBold},
	}).withAnim(factoryAnim())
}
