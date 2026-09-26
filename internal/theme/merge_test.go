package theme

import (
	"testing"

	"github.com/michiTrader/arxi_tui/internal/ui"
)

// TestMergeLayersOverOnTopOfBase pins the one thing Merge knows: on a name both
// themes define, the `over` value wins; a name only `base` defines survives; a
// name only `over` defines is added. If this reverses, a plugin token could never
// override a factory default (or, worse, a user token could not override a
// plugin's), silently defeating the TOKENS.md precedence the whole plugin theme
// story rests on.
func TestMergeLayersOverOnTopOfBase(t *testing.T) {
	base := FromMap(map[string]ui.Style{
		"shared":    {Attrs: ui.AttrDim},
		"base.only": {Attrs: ui.AttrItalic},
	})
	over := FromMap(map[string]ui.Style{
		"shared":    {Attrs: ui.AttrBold},
		"over.only": {Attrs: ui.AttrUnderline},
	})

	got := Merge(base, over)

	if s := got.Resolve("shared"); s.Attrs != ui.AttrBold {
		t.Fatalf("shared token resolved to %v, want the over value (bold): on a conflict Merge must let over win, or a later layer can never override an earlier one — the precedence user > plugin > factory breaks", s.Attrs)
	}
	if s := got.Resolve("base.only"); s.Attrs != ui.AttrItalic {
		t.Fatalf("base.only resolved to %v, want italic: a name only base defines must survive the merge, or layering a plugin would erase the factory tokens it does not mention", s.Attrs)
	}
	if s := got.Resolve("over.only"); s.Attrs != ui.AttrUnderline {
		t.Fatalf("over.only resolved to %v, want underline: a name only over defines must be added, or a plugin could contribute no new token", s.Attrs)
	}
}

// TestMergeComposesTheSignedPrecedence builds the exact three-layer stack the
// host builds — Merge(Merge(factory, plugin), user) — and asserts the signed
// order user > plugin > factory (TOKENS.md L159). This is the composition, not
// the primitive: it fails if the caller layered the three in any other order, so
// it guards the one place precedence actually lives.
func TestMergeComposesTheSignedPrecedence(t *testing.T) {
	factory := FromMap(map[string]ui.Style{
		"accent":       {Attrs: ui.AttrDim},     // overridden by both
		"factory.only": {Attrs: ui.AttrReverse}, // survives untouched
	})
	plugin := FromMap(map[string]ui.Style{
		"accent":      {Attrs: ui.AttrItalic}, // beats factory, loses to user
		"plugin.only": {Attrs: ui.AttrStrike}, // survives untouched
	})
	user := FromMap(map[string]ui.Style{
		"accent": {Attrs: ui.AttrBold}, // wins outright
	})

	active := Merge(Merge(factory, plugin), user)

	if s := active.Resolve("accent"); s.Attrs != ui.AttrBold {
		t.Fatalf("accent resolved to %v, want the user value (bold): the signed precedence is user > plugin > factory, so the user layer must win a three-way conflict — recheck the Merge(Merge(factory, plugin), user) order at the composition site", s.Attrs)
	}
	if s := active.Resolve("plugin.only"); s.Attrs != ui.AttrStrike {
		t.Fatalf("plugin.only resolved to %v, want strike: a plugin token with no user or factory override must reach the active theme, or a mounted plugin's styling is dropped", s.Attrs)
	}
	if s := active.Resolve("factory.only"); s.Attrs != ui.AttrReverse {
		t.Fatalf("factory.only resolved to %v, want reverse: a factory token neither plugin nor user redefines must survive, or enabling a plugin would strip the shipped look", s.Attrs)
	}
}

// TestMergeMergesTimingTokens holds Merge to the same over-wins rule for the
// `anim` section, not only style tokens. A plugin may ship timing tokens, and if
// the merge dropped or ignored them an animation prop naming a plugin's anim
// token would fail the load-time "undefined anim token" refusal even though the
// plugin defined it.
func TestMergeMergesTimingTokens(t *testing.T) {
	base := FromMap(map[string]ui.Style{"t": {}}).withAnim(map[string]AnimDef{
		"default": {DurationMS: 200, Curve: "ease_out", FPS: 30},
		"shared":  {DurationMS: 100, Curve: "linear", FPS: 10},
	})
	over := FromMap(map[string]ui.Style{"t": {}}).withAnim(map[string]AnimDef{
		"shared":     {DurationMS: 500, Curve: "ease_in", FPS: 60},
		"plugin.pan": {DurationMS: 0, Curve: "linear", FPS: 20},
	})

	got := Merge(base, over)

	if !got.HasAnim("default") {
		t.Fatalf("merged theme lost the base-only anim token 'default': a merge that drops base timing tokens would make the factory anim.default vanish when any plugin is layered, breaking every prop that resolves the empty token to it")
	}
	if !got.HasAnim("plugin.pan") {
		t.Fatalf("merged theme is missing the over-only anim token 'plugin.pan': a plugin's own timing token must reach the active theme, or a prop naming it fails the undefined-token refusal")
	}
	if d, _ := got.Anim("shared"); d.DurationMS != 500 {
		t.Fatalf("shared anim token resolved to duration %d, want the over value 500: on a conflict the anim section must follow the same over-wins rule as style tokens", d.DurationMS)
	}
}

// TestMergeDoesNotMutateItsInputs is the property the recompose loop depends on:
// the active theme is rebuilt from the factory every time a plugin is enabled or
// removed, so if Merge wrote into `base` the factory theme would accumulate every
// plugin's tokens permanently and unmounting a plugin could never take its
// styling back.
func TestMergeDoesNotMutateItsInputs(t *testing.T) {
	base := FromMap(map[string]ui.Style{"accent": {Attrs: ui.AttrDim}})
	over := FromMap(map[string]ui.Style{"accent": {Attrs: ui.AttrBold}, "extra": {}})

	_ = Merge(base, over)

	if s := base.Resolve("accent"); s.Attrs != ui.AttrDim {
		t.Fatalf("base 'accent' changed to %v after Merge, want the original dim: Merge must not write into its inputs, or the factory theme is corrupted by the first plugin and unmount cannot restore it", s.Attrs)
	}
	if base.Has("extra") {
		t.Fatalf("base gained the over-only token 'extra' after Merge: Merge must not write into its inputs, or the factory theme accumulates every plugin's tokens permanently")
	}
}

// TestMergeToleratesNilThemes keeps Merge total, because the host composes it
// unconditionally — a session with no user theme passes nil for that layer, and a
// fragment-only plugin passes an empty theme. A nil input must be treated as
// contributing nothing, never a panic.
func TestMergeToleratesNilThemes(t *testing.T) {
	only := FromMap(map[string]ui.Style{"t": {Attrs: ui.AttrBold}})

	if s := Merge(nil, only).Resolve("t"); s.Attrs != ui.AttrBold {
		t.Fatalf("Merge(nil, over) lost over's token: a nil base must contribute nothing, not swallow the layer on top of it")
	}
	if s := Merge(only, nil).Resolve("t"); s.Attrs != ui.AttrBold {
		t.Fatalf("Merge(base, nil) lost base's token: a nil over must contribute nothing, not erase the layer under it")
	}
	if got := Merge(nil, nil); got == nil {
		t.Fatalf("Merge(nil, nil) returned nil, want an empty theme: the result must always be a usable theme the resolver can read")
	}
}
