package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/ext"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/patch"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

// H7 freezes the Scene 6 golden: a community plugin shipped by link, composed
// into a host scene and rendered through the ordinary engine path.
//
// The frame it pins is the honest *declarative* one, and the choice is
// load-bearing. Block H implements only the declarative plugin path (SCENES.md
// Scene 6, ADR-0006): a manifest with no `executable` runs no foreign code and
// streams nothing, so its overlay can show only its own chrome and a placeholder,
// never a live `tick.price`. The mock-driven preview DESIGN-BLOCK-H.md H7
// sketches — `tick.price` rendering as its declared `mock` — cannot be produced
// here: declaring `binds` makes a manifest behavioral (H2 refuses it), and
// painting a declared mock for an unsatisfied bind is the preview renderer Q16
// signs into Block J. So this golden pins what a declarative-only load actually
// produces, which is the phrase the design itself uses for it: the ticker overlay
// composed top-right, its profit/loss tokens resolving through the merged theme,
// and no data, because a stream-less plugin has none to show.
//
// The fixtures follow the established scene-golden shape (SUBAGENTS, ANIMATION):
// testdata/TICKER.json is the composed Scene 6 document — the pinned contract the
// frame and styled goldens render — and testdata/plugins/TICKER.manifest.json is
// the plugin an author ships. TICKER.json is not hand-authored: TestTickerJSONIs
// TheMountOutput proves it is byte-for-byte what patch.Mount produces from the
// manifest and the host below, so the pinned scene is the real composition and a
// drift in the mount is a golden diff rather than a divergence nobody sees.

// tickerHost is the minimal host the ticker mounts into: a header, the gated
// notice node every shipped scene must carry (host.scene.error), a chat pane and
// an input. It is dedicated rather than SOBRIA so the composed golden isolates
// what the plugin adds instead of restating the whole default UI, and it binds
// the notice so the composed scene satisfies the shipped-scene obligation on its
// own.
const tickerHost = `{ "root": { "type": "stack", "children": [
  { "type": "text", "style": {"style": "dim"}, "text": "Δr×i · community plugin demo" },
  { "id": "notice", "type": "text", "bind": "host.scene.error",
    "when": "host.scene.error", "style": {"style": "banner"} },
  { "id": "chat", "type": "markdown", "bind": "chat.history", "grow": 1 },
  { "id": "prompt", "type": "input", "bind": "user.input", "prefix": "┃ " }
]}}`

// tickerManifest loads and validates the declarative ticker manifest. It fails
// the test if the manifest does not load, because H7 must start from a manifest
// H2 accepts — otherwise the golden would be measuring the loader, not the mount.
func tickerManifest(t *testing.T) *ext.Manifest {
	t.Helper()
	src, err := os.ReadFile("../../testdata/plugins/TICKER.manifest.json")
	if err != nil {
		t.Fatalf("read TICKER.manifest.json: %v", err)
	}
	m, err := ext.ParseNamed("TICKER.manifest.json", src)
	if err != nil {
		t.Fatalf("ParseNamed(ticker manifest): %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("the ticker manifest does not load: %v; H7 must start from a manifest H2 accepts", err)
	}
	return m
}

// tickerDoc reads the composed Scene 6 document, the pinned contract the frame
// and styled goldens render. It validates structurally and — the shippability
// premise — against the merged theme Merge(factory, plugin): the plugin's
// profit/loss tokens layered over the factory look at plugin precedence
// (TOKENS.md user > plugin > factory). A `style:"profit"` that resolved to no
// token in any active theme would be a scene naming a token nothing defines, and
// the render would emit an unstyled span while the golden pretended coverage.
func tickerDoc(t *testing.T) *scene.Document {
	t.Helper()
	doc, err := scene.ParseFile("../../testdata/TICKER.json")
	if err != nil {
		t.Fatalf("parse TICKER.json: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("the composed Scene 6 golden must validate structurally; got %v", err)
	}
	pluginTheme, err := tickerManifest(t).Theme()
	if err != nil {
		t.Fatalf("manifest token block did not parse: %v", err)
	}
	merged := theme.Merge(theme.Factory(), pluginTheme)
	if errs := scene.ValidateTokens(doc, merged); len(errs) > 0 {
		t.Fatalf("premise broken: the composed ticker names a token the merged theme does not ship,\n"+
			"so it is not a shippable scene; got %v", errs)
	}
	return doc
}

// tickerFrame renders the composed scene at a fixed size, the single source of
// truth the witness and both golden tests share so they cannot drift on which
// frame is being asserted. The fold state is empty: the host binds resolve to
// their empty projections and the ticker overlay is static, so the frame is
// deterministic without inventing host activity the scene is not about.
func tickerFrame(t *testing.T) ui.Frame {
	t.Helper()
	r := Renderer{Width: 72, Height: 12}
	return r.RenderFrame(tickerDoc(t), fold.Fold(nil))
}

// TestTickerJSONIsTheMountOutput proves testdata/TICKER.json is exactly what
// patch.Mount composes from the manifest and tickerHost — so the pinned Scene 6
// contract is the real declarative-plugin composition, not a hand-authored
// lookalike. UPDATE_GOLDEN=1 regenerates TICKER.json from the mount, which is the
// only way it is ever written: the fixture cannot drift from the composer,
// because the composer is what produces it.
func TestTickerJSONIsTheMountOutput(t *testing.T) {
	m := tickerManifest(t)
	res, err := patch.Mount("TICKER.json", []byte(tickerHost), m)
	if err != nil {
		t.Fatalf("Mount refused the declarative ticker: %v; H7's premise is that a declarative plugin composes into the host", err)
	}

	goldenPath := "../../testdata/TICKER.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, res.Source, 0644); err != nil {
			t.Fatalf("write TICKER.json: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read %s: %v", goldenPath, err)
	}
	if string(res.Source) != string(want) {
		t.Errorf("TICKER.json is not the mount output; the pinned Scene 6 scene has drifted from what\n"+
			"patch.Mount composes from the manifest and host.\n--- mount produced ---\n%s\n--- TICKER.json ---\n%s",
			res.Source, string(want))
	}
}

// TestTickerSceneWitnessesTheMountedPlugin asserts, before the byte-for-byte
// golden, that the mounted plugin actually reached the frame with its declarative
// content — so a regression that drops the mount, the overlay render, or the
// no-data placeholder fails here with a named consequence, not only as an opaque
// golden diff.
func TestTickerSceneWitnessesTheMountedPlugin(t *testing.T) {
	got := tickerFrame(t).Plain()

	if strings.Contains(got, "UNKNOWN NODE TYPE") {
		t.Fatalf("the Scene 6 golden rendered an unknown node type — the ticker fragment names a node the\n"+
			"engine cannot draw:\n%s", got)
	}
	// The overlay's own content must be on screen: the label and the honest
	// no-stream placeholder. A missing label means the mounted overlay did not
	// compose or did not render; a missing placeholder means the declarative
	// no-data state — the whole point of a stream-less plugin — is not shown.
	for _, want := range []string{"TICK", "no data yet"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected the mounted ticker overlay to render %q; got:\n%s\n"+
				"consequence: a community plugin shipped by link did not reach the frame, so Scene 6's\n"+
				"declarative path draws nothing.\n"+
				"remedy: patch.Mount must compose the fragment and the engine must render the overlay.", want, got)
		}
	}
	// The host under it is still present, not replaced by the mount.
	if !strings.Contains(got, "community plugin demo") {
		t.Errorf("the host header is gone after mounting; a plugin adds to the host tree, it does not\n"+
			"replace it. frame:\n%s", got)
	}
}

// TestTickerSceneStyledFrameCarriesPluginTokens is the token half of the witness:
// the styled frame annotates each span with its token name, so a contributed
// plugin token that dropped out of the merge or the render is a missing substring
// here. Both are checked so the profit/loss pair cannot degrade to one token
// pretending to be a set.
func TestTickerSceneStyledFrameCarriesPluginTokens(t *testing.T) {
	styled := tickerFrame(t).Styled()
	for _, tok := range []string{"profit", "loss"} {
		if !strings.Contains(styled, tok) {
			t.Errorf("the styled frame does not carry the %q token; a contributed plugin token dropped out\n"+
				"of the composed scene, so the merge or the render lost it. styled:\n%s", tok, styled)
		}
	}
}

// TestTickerSceneMatchesGolden freezes the plain frame. UPDATE_GOLDEN=1
// regenerates it. This is the durable pin: a change to the overlay render, the
// fragment, or the host it composes into shows up here as a reviewable golden
// diff.
func TestTickerSceneMatchesGolden(t *testing.T) {
	got := tickerFrame(t).Plain()

	goldenPath := "../../testdata/TICKER.frame"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	if got != string(want) {
		t.Errorf("Scene 6 ticker frame does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

// TestTickerSceneStyledGolden freezes the styled frame, so a dropped or changed
// style token on the mounted overlay — a plugin token or a host one under it — is
// a golden diff rather than a silent regression. UPDATE_GOLDEN=1 regenerates it.
func TestTickerSceneStyledGolden(t *testing.T) {
	got := tickerFrame(t).Styled()

	goldenPath := "../../testdata/TICKER.styled"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	if got != string(want) {
		t.Errorf("Scene 6 ticker styled output does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}
