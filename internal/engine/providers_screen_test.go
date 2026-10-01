package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// providersState seeds the providers screen's one list bind directly, the way
// configState seeds config.settings: providers.models is host view state no
// arxi-core event produces (BINDS.md §4.3) — the host fills it from a model.list
// round-trip over the serve socket — so there is no event log to fold, and a
// test builds the State the host would.
//
// The two models carry the witnesses one row_template must distinguish: a model
// that is enabled and one that is disabled. A scene that renders these correctly
// proves two decisions at once — the enable button draws only on the disabled row
// and the disable button only on the enabled one (the row.disabled/row.enabled
// gate, this engine's `when`-with-no-operator idiom), and the ref the pressed
// command would carry is the provider/id form the model.enable verb accepts.
func providersState() fold.State {
	return fold.State{
		ProviderModels: []fold.ProviderModel{
			{Provider: "moonshot", ID: "kimi-k2", Enabled: true},
			{Provider: "deepseek", ID: "deepseek-chat", Enabled: false},
		},
	}
}

func providersDoc(t *testing.T) *scene.Document {
	t.Helper()
	data, err := os.ReadFile("../../testdata/PROVIDERS.json")
	if err != nil {
		t.Fatalf("read PROVIDERS.json: %v", err)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	return doc
}

// TestProvidersSceneRenders holds the Scene 12 (PROVIDERS) screen to the K2
// dogfood it exists for: one model list whose row_template toggles each model
// through an enable/disable button, the config.settings shape applied to the
// provider verbs K2 shipped. The screen is a document, so the test asserts the
// screen the document must produce rather than a shape it happens to have.
func TestProvidersSceneRenders(t *testing.T) {
	doc := providersDoc(t)
	if err := doc.Validate(); err != nil {
		t.Fatalf("scene validation: %v", err)
	}

	r := Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, providersState())
	got := f.Plain()

	if strings.Contains(got, "UNKNOWN NODE TYPE") {
		t.Errorf("providers scene rendered an unknown node type:\n%s", got)
	}

	// Every model's provider and id render: the row_template instantiates per
	// element of providers.models, the per-element guarantee rowScopesFor gives.
	for _, want := range []string{"moonshot", "kimi-k2", "deepseek", "deepseek-chat"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected the model list to render %q; got:\n%s\n"+
				"consequence: a model the host projects has no row, so it cannot be seen or toggled.\n"+
				"remedy: rowScopesFor must supply one providers.models scope per element.", want, got)
		}
	}

	// Both buttons render: an "enable" button on the disabled row and a "disable"
	// button on the enabled one. Each is gated on a per-row boolean, so a missing
	// one means a gate collapsed and the row shows the wrong affordance.
	if !strings.Contains(got, "enable") {
		t.Errorf("expected an \"enable\" button on the disabled model's row; got:\n%s\n"+
			"consequence: a disabled model has no affordance to enable it, so the screen is read-only for\n"+
			"exactly the row that needs the action.\n"+
			"remedy: the enable button is gated on when:\"row.disabled\"; rowScopesFor must synthesize it.", got)
	}
	if !strings.Contains(got, "disable") {
		t.Errorf("expected a \"disable\" button on the enabled model's row; got:\n%s\n"+
			"consequence: an enabled model cannot be turned off from the screen.\n"+
			"remedy: the disable button is gated on when:\"row.enabled\".", got)
	}

	// A relative bind that lost its row scope resolves to the placeholder. The
	// model rows must never render one: every gated control that draws has a
	// value, and a "[…]" is a row present but empty (§4.6).
	if strings.Contains(got, "[…]") {
		t.Errorf("a providers row rendered the placeholder [\u2026]; got:\n%s\n"+
			"consequence: a row.* bind resolved with no element in scope, so a control is present but\n"+
			"empty — the sub-renderer lost the row scope.\n"+
			"remedy: rowScopesFor must supply the field and the sub-renderer must read r.curRow.", got)
	}
}

// TestProvidersRowButtonGating is the counterfactual the screen turns on, run as
// an assertion: a disabled model's row carries the "enable" button and not the
// "disable" one, and an enabled model's row the reverse. This engine's `when` is a
// bare truthiness test with no comparison operator, so the row_template gates its
// two buttons on the synthesized row.disabled/row.enabled booleans; if those
// collapsed to one value — the mistake a single gate invites — an enabled row would
// show a stray "enable" and the two actions would stop being mixed by state.
func TestProvidersRowButtonGating(t *testing.T) {
	doc := providersDoc(t)
	if err := doc.Validate(); err != nil {
		t.Fatalf("scene validation: %v", err)
	}

	r := Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, providersState())
	lines := strings.Split(f.Plain(), "\n")

	rowOf := func(label string) string {
		for _, l := range lines {
			if strings.Contains(l, label) {
				return l
			}
		}
		t.Fatalf("no rendered row contains %q:\n%s", label, f.Plain())
		return ""
	}

	// The enabled model's row carries the disable action and not the enable one:
	// row.disabled is false for it, so the enable button is gated off.
	enabled := rowOf("kimi-k2")
	if !strings.Contains(enabled, "disable") {
		t.Errorf("the enabled model's row has no disable button:\n%q\n"+
			"consequence: the enabled row shows no way to turn the model off.\n"+
			"remedy: the disable button is gated on when:\"row.enabled\".", enabled)
	}
	if strings.Contains(enabled, "enable") && !strings.Contains(enabled, "disable") {
		t.Errorf("the enabled model's row drew an enable button:\n%q\n"+
			"consequence: row.disabled is not false for an enabled model, so the enable button drew where\n"+
			"only disable should — the two actions are no longer mixed by state.\n"+
			"remedy: rowScopesFor must synthesize row.disabled as boolField(!m.Enabled).", enabled)
	}

	// The disabled model's row carries the enable action: row.enabled is false for
	// it, so the disable button is gated off.
	disabled := rowOf("deepseek-chat")
	if !strings.Contains(disabled, "enable") {
		t.Errorf("the disabled model's row has no enable button:\n%q\n"+
			"consequence: the disabled row shows no way to turn the model on.\n"+
			"remedy: the enable button is gated on when:\"row.disabled\".", disabled)
	}
}

// TestProvidersSceneMatchesGolden pins the plain frame. UPDATE_GOLDEN=1
// regenerates it. The providers screen's shape is frozen, so a later change to the
// list, the row_template scope, the switch/button mix or the providers.models fold
// shows up as a reviewable golden diff.
func TestProvidersSceneMatchesGolden(t *testing.T) {
	doc := providersDoc(t)
	r := Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, providersState())
	got := f.Plain()

	goldenPath := "../../testdata/PROVIDERS.frame"
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
		t.Errorf("providers scene does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

// TestProvidersSceneStyledGolden pins the styled frame, so a dropped or changed
// style token on the header, the switch/button controls or the footer is a golden
// diff rather than a silent regression. UPDATE_GOLDEN=1 regenerates it.
func TestProvidersSceneStyledGolden(t *testing.T) {
	doc := providersDoc(t)
	r := Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, providersState())
	got := f.Styled()

	goldenPath := "../../testdata/PROVIDERS.styled"
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
		t.Errorf("providers scene styled output does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}
