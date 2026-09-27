package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// renderWithPlugins is renderToText with a plugin snapshot attached, the input
// the loop hands the renderer each repaint (I3 / ADR-0007 §I-D). It renders a
// hand-built tree rather than a golden for the reason ui_hidden_test states: an
// empty snapshot is the default and moves no golden, so the property only appears
// once a plugin has published.
//
// The snapshot is a plain map[string]string on purpose. That IS the renderer's
// contract with the host: the loop reads ext.PluginStore.Snapshot() (which the
// store's own tests pin) and hands the renderer a map. Building the map here
// rather than importing ext keeps the engine's test off the data-side package and
// tests exactly the boundary the renderer owns — resolving a path to a value.
func renderWithPlugins(t *testing.T, n *scene.Node, state fold.State, plugins map[string]string) string {
	t.Helper()
	r := &Renderer{Width: 80, Height: 24, PluginValues: plugins}
	var b strings.Builder
	for _, l := range r.renderNode(n, state, 24).Live {
		b.WriteString(l.Text())
		b.WriteString("\n")
	}
	return b.String()
}

// TestPluginBindResolvesFromTheSnapshot is the positive half of I3: a text node
// bound to a plugin's `<plugin-id>.*` field draws the value the snapshot carries,
// proving the store→renderer bridge that makes a behavioral plugin's stream
// visible at all.
func TestPluginBindResolvesFromTheSnapshot(t *testing.T) {
	node := &scene.Node{Type: "text", Bind: "tick.price"}
	got := renderWithPlugins(t, node, fold.State{}, map[string]string{"tick.price": "$4.20"})
	if !strings.Contains(got, "$4.20") {
		t.Errorf("a text node bound to tick.price did not render the published value; got:\n%s\n"+
			"consequence: the plugin snapshot never reaches bind resolution, so a mounted behavioral plugin renders forever as the empty placeholder — the stream is invisible.\n"+
			"remedy: resolveBindRow must consult the renderer's PluginValues before falling through to fold.State.", got)
	}
}

// TestPluginBindDoesNotLeakIntoFoldState is the invariant-2 guard: a plugin value
// resolves ONLY from the snapshot, never from fold.State, and a plugin path is
// never a fold field. With a value in the snapshot the node draws it; with the
// snapshot dropped the same fold.State draws the placeholder — so no path exists
// by which a plugin's value could have entered the fold.
func TestPluginBindDoesNotLeakIntoFoldState(t *testing.T) {
	node := &scene.Node{Type: "text", Bind: "tick.price"}
	state := fold.State{}
	if got := renderWithPlugins(t, node, state, map[string]string{"tick.price": "$4.20"}); !strings.Contains(got, "$4.20") {
		t.Fatalf("precondition: the snapshot value must draw; got:\n%s", got)
	}
	if got := renderWithPlugins(t, node, state, nil); strings.Contains(got, "$4.20") {
		t.Errorf("the plugin value survived when the snapshot was dropped; got:\n%s\n"+
			"consequence: a plugin's value reached the frame through fold.State, making a stranger's process an authority over the run log — the exact leak invariant 2 / ADR-0003 forbid.\n"+
			"remedy: PluginValues is the only source for a plugin path; nothing writes it into fold.State.", got)
	}
}

// TestUnsatisfiedPluginBindIsThePlaceholder is the §I-G rule: before the first
// frame arrives (or with no plugin mounted at all), a `<plugin-id>.*` bind is
// unsatisfied and renders as the plain placeholder — never mock, never a crash.
// A nil snapshot is the pure/golden path, so this is also the no-op guarantee
// that keeps every non-plugin golden unchanged.
func TestUnsatisfiedPluginBindIsThePlaceholder(t *testing.T) {
	node := &scene.Node{Type: "text", Bind: "tick.price"}
	got := renderWithPlugins(t, node, fold.State{}, nil)
	if !strings.Contains(got, placeholderValue) {
		t.Errorf("an unsatisfied plugin bind rendered %q, not the placeholder %q\n"+
			"consequence: a live mount before its first frame would draw something other than the honest \"no value yet\" placeholder, blurring the line §I-G draws between waiting and preview (mock).\n"+
			"remedy: an absent plugin path must fall through to resolveBind's default placeholder.", strings.TrimSpace(got), placeholderValue)
	}
}

// TestLivenessBindGatesANode proves ui.plugin.<id> (§4.3) drives a `when`: a
// footer gated on the liveness bind renders when the supervisor has marked the
// plugin degraded and stays hidden when no such plugin is mounted (the bind is
// absent, hence falsy). This is the diagnosis path the placeholder cannot carry —
// a scene can show "plugin degraded" precisely because the liveness bind is host
// view state a dying plugin cannot suppress.
func TestLivenessBindGatesANode(t *testing.T) {
	footer := &scene.Node{Type: "text", When: "ui.plugin.tick", Text: "PLUGIN DEGRADED"}

	// No plugin mounted: the liveness bind is absent → falsy → the footer hides.
	// This is the empty-state §4.3 signs, and it moves no golden.
	hidden := renderWithPlugins(t, footer, fold.State{}, nil)
	if strings.Contains(hidden, "PLUGIN DEGRADED") {
		t.Errorf("the liveness-gated footer rendered with no plugin mounted; got:\n%s\n"+
			"consequence: ui.plugin.<id>'s empty state is not falsy, so every scene gating on a plugin's liveness would show its degraded chrome before any plugin exists.", hidden)
	}

	// Supervisor marks the plugin dead: the liveness bind is present and truthy,
	// so the diagnosis footer draws.
	shown := renderWithPlugins(t, footer, fold.State{}, map[string]string{"ui.plugin.tick": "dead"})
	if !strings.Contains(shown, "PLUGIN DEGRADED") {
		t.Errorf("the footer gated on ui.plugin.tick did not render when the plugin was marked dead; got:\n%s\n"+
			"consequence: a degraded plugin cannot surface its own diagnosis, so a dead plugin looks identical to one that is merely quiet (§I-G).\n"+
			"remedy: the liveness snapshot must resolve ui.plugin.<id> and evalWhen must read it as truthy.", shown)
	}
}
