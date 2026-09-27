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

// H7 freezes the Scene 6 golden: a community plugin shipped by link, mounted into
// the default host scene and rendered through the ordinary engine path.
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
// produces, which is exactly the phrase the design uses for it: the ticker
// overlay composed top-right, its profit/loss tokens resolving through the merged
// theme, and no data, because a stream-less plugin has none to show.
//
// What it witnesses end-to-end, and no earlier Block H test did in one fixture:
//   - a manifest loaded by ext, composed into a real host by patch.Mount, and
//     rendered by the engine — the whole declarative path at once;
//   - both contributed tokens (profit, loss) surviving the merge and reaching the
//     styled frame, so a dropped plugin token is a reviewable golden diff;
//   - the host UI still present under the mount, so a plugin adds rather than
//     replaces.

// tickerHostEvents folds the host into the same state SOBRIA's own golden uses,
// so the frame under the ticker is the recognisable default UI and a diff here is
// about the mount, not about a host state this test invented. agent.turn_done
// leaves agent.working false, so the thinking marquee is absent and the frame is
// deterministic.
func tickerHostEvents() []fold.Event {
	return []fold.Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{
			"run_id": "r1", "actor": "user", "budget_usd": 10.0,
			"max_turns": 10, "simulated": false,
		}},
		{Type: "agent.activated", Seq: 2, Payload: map[string]any{"agent": "backend"}},
		{Type: "llm.response", Seq: 3, Payload: map[string]any{
			"agent": "backend", "model": "openai/gpt-4o",
			"text":      "Hola! How can I help you today?",
			"tokens_in": 25, "tokens_out": 35, "cost_usd": 0.001,
		}},
		{Type: "agent.turn_done", Seq: 4, Payload: map[string]any{"agent": "backend"}},
	}
}

// tickerComposed loads the declarative ticker manifest, mounts it into the
// SOBRIA host, and returns the composed document together with the theme the host
// renders it under. The theme is Merge(factory, plugin): the plugin's
// profit/loss tokens layered over the factory look at plugin precedence
// (TOKENS.md user > plugin > factory). Validating the composed scene against that
// merged theme is what makes the fixture's claim "shippable" rather than "merely
// parseable" — a `style:"profit"` that resolved to no token in any active theme
// would be a scene naming a token nothing defines, and the render would emit an
// unstyled span while the golden pretended coverage.
func tickerComposed(t *testing.T) *scene.Document {
	t.Helper()

	host, err := os.ReadFile("../../testdata/SOBRIA.json")
	if err != nil {
		t.Fatalf("read SOBRIA.json: %v", err)
	}
	manifestSrc, err := os.ReadFile("../../testdata/TICKER.manifest.json")
	if err != nil {
		t.Fatalf("read TICKER.manifest.json: %v", err)
	}

	m, err := ext.ParseNamed("TICKER.manifest.json", manifestSrc)
	if err != nil {
		t.Fatalf("ParseNamed(ticker manifest): %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("the ticker manifest does not load: %v; H7 must start from a manifest H2 accepts", err)
	}

	res, err := patch.Mount("SOBRIA.json", host, m)
	if err != nil {
		t.Fatalf("Mount refused the declarative ticker: %v; H7's premise is that a declarative plugin composes into the host", err)
	}

	pluginTheme, err := m.Theme()
	if err != nil {
		t.Fatalf("manifest token block did not parse: %v", err)
	}
	merged := theme.Merge(theme.Factory(), pluginTheme)
	if errs := scene.ValidateTokens(res.Doc, merged); len(errs) > 0 {
		t.Fatalf("premise broken: the mounted ticker names a token the merged theme does not ship,\n"+
			"so it is not a shippable scene; got %v", errs)
	}
	return res.Doc
}

// tickerFrame renders the composed scene at a fixed size against the host state,
// the single source of truth the witness and both golden tests share so they
// cannot drift on which frame is being asserted.
func tickerFrame(t *testing.T) ui.Frame {
	t.Helper()
	r := Renderer{Width: 80, Height: 24}
	return r.RenderFrame(tickerComposed(t), fold.Fold(tickerHostEvents()))
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
	// The host under it is still the default UI, not replaced by the mount.
	if !strings.Contains(got, "Hola! How can I help you today?") {
		t.Errorf("the host transcript is gone after mounting; a plugin adds to the host tree, it does not\n"+
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
// regenerates it. This is the durable pin: a change to the mount placement, the
// overlay render, the fragment, or the host it composes into shows up here as a
// reviewable golden diff.
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
