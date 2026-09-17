package theme

import "github.com/michiTrader/arxi_tui/internal/ui"

// Factory returns the compiled-in sobria theme. This is the backstop when every
// user-supplied theme fails to load. It defines exactly the tokens the three
// golden scenes (RAW, SOBRIA, MAXIMUM) reference, and nothing more.
//
// Sobria is no-color emphasis: dim for de-emphasis, bold for headers. Light/dark
// auto-detection via OSC 11 lives in the emitter (internal/term), not here — the
// theme says what to emphasize, and the terminal backend decides how.
func Factory() *Theme {
	return FromMap(map[string]ui.Style{
		"dim":               {Attrs: ui.AttrDim},
		"header":            {Attrs: ui.AttrBold},
		"input.placeholder": {Attrs: ui.AttrDim},
		"banner":            {Attrs: ui.AttrBold},
	})
}
