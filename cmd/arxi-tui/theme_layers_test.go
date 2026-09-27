package main

import (
	"testing"

	"github.com/michiTrader/arxi_tui/internal/patch"
	"github.com/michiTrader/arxi_tui/internal/theme"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

// layerTheme builds a one-token plugin layer whose single token carries a
// distinguishing attribute, so a test can tell which layer won a conflict by the
// attribute the active theme resolves.
func layerTheme(token string, attr ui.Attr) *theme.Theme {
	return theme.FromMap(map[string]ui.Style{token: {Attrs: attr}})
}

// TestComposeThemeLayersPluginOverFactory is H4's positive control on the loop
// side: composeTheme must layer a mounted plugin's tokens over the factory base
// so a plugin token wins a conflict and a factory-only token survives untouched.
// Without it a mounted plugin's colours never reach the screen — the silent
// half-mount the merge exists to close.
func TestComposeThemeLayersPluginOverFactory(t *testing.T) {
	base := theme.FromMap(map[string]ui.Style{
		"accent":       {Attrs: ui.AttrDim},     // the plugin overrides this
		"factory.only": {Attrs: ui.AttrReverse}, // survives untouched
	})
	layers := []pluginThemeLayer{{id: "tick", thm: layerTheme("accent", ui.AttrBold)}}

	active := composeTheme(base, layers)

	if s := active.Resolve("accent"); s.Attrs != ui.AttrBold {
		t.Errorf("accent resolved to %v, want the plugin value (bold).\nConsequence: a plugin token loses to the factory default, so a mounted plugin's styling never ships.\nRemedy: composeTheme merges each plugin layer over the base (plugin > factory).", s.Attrs)
	}
	if s := active.Resolve("factory.only"); s.Attrs != ui.AttrReverse {
		t.Errorf("factory.only resolved to %v, want reverse.\nConsequence: layering a plugin strips a factory token the plugin never touched — enabling a plugin would erase the shipped look.\nRemedy: composeTheme starts from the factory base and only overrides what a layer redefines.", s.Attrs)
	}
}

// TestComposeThemeUserWinsOverPlugin pins the top of the precedence: a user layer
// merged last must beat a plugin token, the `user` in user > plugin > factory.
// The boot path has no user theme layer yet, so this composes the user layer the
// way the loop will when one exists — merged after every plugin layer — proving
// composeTheme's order is the signed one, not merely plugin-over-factory.
func TestComposeThemeUserWinsOverPlugin(t *testing.T) {
	base := theme.FromMap(map[string]ui.Style{"accent": {Attrs: ui.AttrDim}})
	plugin := pluginThemeLayer{id: "tick", thm: layerTheme("accent", ui.AttrItalic)}
	user := pluginThemeLayer{id: "", thm: layerTheme("accent", ui.AttrBold)}

	// The user layer is the last layer merged, exactly as the loop will place it.
	active := composeTheme(base, []pluginThemeLayer{plugin, user})

	if s := active.Resolve("accent"); s.Attrs != ui.AttrBold {
		t.Errorf("accent resolved to %v, want the user value (bold).\nConsequence: a plugin token overrides the user's own choice — a downloaded plugin restyles the interface out from under the user.\nRemedy: the user layer merges last so it wins; composeTheme folds layers in order, so the caller must place user after every plugin.", s.Attrs)
	}
}

// TestComposeThemeLaterPluginWinsOverEarlier pins the between-plugins rule: when
// two plugins declare the same token, the one mounted later wins, the same
// "over wins" rule Merge applies within a pair. This is what makes N strangers'
// token blocks compose deterministically rather than by map iteration order.
func TestComposeThemeLaterPluginWinsOverEarlier(t *testing.T) {
	base := theme.FromMap(map[string]ui.Style{"accent": {Attrs: ui.AttrDim}})
	first := pluginThemeLayer{id: "a", thm: layerTheme("accent", ui.AttrItalic)}
	second := pluginThemeLayer{id: "b", thm: layerTheme("accent", ui.AttrBold)}

	active := composeTheme(base, []pluginThemeLayer{first, second})

	if s := active.Resolve("accent"); s.Attrs != ui.AttrBold {
		t.Errorf("accent resolved to %v, want the later plugin's value (bold).\nConsequence: two plugins declaring one token would resolve by an undefined order, so the same two plugins could paint differently between runs.\nRemedy: composeTheme folds layers in mount order, so a later plugin's token overrides an earlier one's.", s.Attrs)
	}
}

// TestApplyTokenLayerAddThenRemoveIsExact is the mount/unmount round-trip on the
// layer set: adding a plugin's layer then removing it by id must return the set
// to empty, so recomposing lands back on the factory base with no residue. This
// is the property that makes a plugin's styling reversible — the whole point of
// keying the layer by id and recomposing from the base rather than un-merging.
func TestApplyTokenLayerAddThenRemoveIsExact(t *testing.T) {
	base := theme.FromMap(map[string]ui.Style{"accent": {Attrs: ui.AttrDim}})

	var layers []pluginThemeLayer
	layers = applyTokenLayer(layers, &patch.PluginTokens{ID: "tick", Theme: layerTheme("accent", ui.AttrBold)})
	if s := composeTheme(base, layers).Resolve("accent"); s.Attrs != ui.AttrBold {
		t.Fatalf("after add, accent = %v, want bold: the added layer must reach the active theme", s.Attrs)
	}

	layers = applyTokenLayer(layers, &patch.PluginTokens{ID: "tick", Remove: true})
	if len(layers) != 0 {
		t.Errorf("after remove, %d layer(s) remain, want 0.\nConsequence: an unmounted plugin's tokens linger in the active theme forever — an unmount that does not take the styling back.\nRemedy: applyTokenLayer drops the layer whose id matches the remove op.", len(layers))
	}
	if s := composeTheme(base, layers).Resolve("accent"); s.Attrs != ui.AttrDim {
		t.Errorf("after remove, accent = %v, want the factory dim.\nConsequence: the plugin's override survives its own removal.\nRemedy: recompose from the factory base over the emptied layer set.", s.Attrs)
	}
}

// TestApplyTokenLayerReAddReplacesInPlace pins that re-adding the same plugin id
// updates its layer rather than stacking a second one, so a plugin re-mounted
// after an edit does not accumulate stale layers that a single remove cannot
// fully clear.
func TestApplyTokenLayerReAddReplacesInPlace(t *testing.T) {
	base := theme.FromMap(map[string]ui.Style{"accent": {Attrs: ui.AttrDim}})

	var layers []pluginThemeLayer
	layers = applyTokenLayer(layers, &patch.PluginTokens{ID: "tick", Theme: layerTheme("accent", ui.AttrItalic)})
	layers = applyTokenLayer(layers, &patch.PluginTokens{ID: "tick", Theme: layerTheme("accent", ui.AttrBold)})

	if len(layers) != 1 {
		t.Errorf("re-adding plugin \"tick\" produced %d layers, want 1.\nConsequence: a re-mount stacks layers, so one remove leaves a stale layer behind and the styling never fully clears.\nRemedy: applyTokenLayer replaces the layer for an id already present.", len(layers))
	}
	if s := composeTheme(base, layers).Resolve("accent"); s.Attrs != ui.AttrBold {
		t.Errorf("after re-add, accent = %v, want the newer value (bold).\nConsequence: a re-mount keeps the stale token instead of the freshly declared one.\nRemedy: replacing the layer must store the new theme.", s.Attrs)
	}
}
