package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// configState seeds the /config screen's two list binds directly, the way the
// community tests seed community.matches: config.categories and config.settings
// are host view state no arxi-core event produces (BINDS.md §4.3), so there is no
// event log to fold — the host writes them, and a test builds the State the host
// would.
//
// The settings deliberately carry all three witnesses one row_template must
// distinguish: a toggle that is off, a toggle that is on, and a text field. A
// scene that renders these correctly proves three separate decisions at once —
// the switch reads its per-row boolean (one [ ] and one [x], not two of either),
// the input reads its per-row string through the row scope ("nvim", not the
// placeholder), and the row.is_toggle/row.is_text discrimination draws exactly
// one control per row (no row shows both a checkbox and a text value).
func configState() fold.State {
	return fold.State{
		ConfigCategories: []fold.ConfigCategory{
			{Name: "General"},
			{Name: "Privacy"},
		},
		ConfigSettings: []fold.ConfigSetting{
			{Label: "Telemetry", Kind: "toggle", Enabled: false},
			{Label: "Auto-update", Kind: "toggle", Enabled: true},
			{Label: "Editor", Kind: "text", Value: "nvim"},
		},
	}
}

func configDoc(t *testing.T) *scene.Document {
	t.Helper()
	data, err := os.ReadFile("../../testdata/CONFIG.json")
	if err != nil {
		t.Fatalf("read CONFIG.json: %v", err)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	return doc
}

// TestConfigSceneRenders holds the Scene 5 (CONFIG) golden scene to the dogfood
// it exists for: one settings list whose row_template mixes a `switch` row and an
// `input` row by the setting's kind. arxi-sim's /config was 730 lines of Go; the
// claim of this scene is that the same screen is a document, so the test asserts
// the screen the document must produce rather than a shape it happens to have.
func TestConfigSceneRenders(t *testing.T) {
	doc := configDoc(t)
	if err := doc.Validate(); err != nil {
		t.Fatalf("scene validation: %v", err)
	}

	r := Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, configState())
	got := f.Plain()

	if strings.Contains(got, "UNKNOWN NODE TYPE") {
		t.Errorf("config scene rendered an unknown node type:\n%s", got)
	}

	// Both categories render, one row each: the category list instantiates its
	// row_template per element of config.categories.
	for _, want := range []string{"General", "Privacy"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected the category rail to render %q; got:\n%s\n"+
				"consequence: the row_template over config.categories is not instantiating a row per\n"+
				"element, so a group the host projects is invisible.\n"+
				"remedy: rowScopesFor must supply row.name per element and the template must read it.", want, got)
		}
	}

	// Every setting's label renders: the settings row_template instantiates per
	// element of config.settings, the same per-element guarantee.
	for _, want := range []string{"Telemetry", "Auto-update", "Editor"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected the settings list to render the label %q; got:\n%s\n"+
				"consequence: a setting the host projects has no row, so it cannot be seen or changed.\n"+
				"remedy: rowScopesFor must supply one config.settings scope per element.", want, got)
		}
	}

	// The two toggles render as checkboxes, one off and one on: the switch reads
	// its state from row.enabled through the same bindTruthy rule a `when` uses. A
	// missing [ ] or [x] means the switch is ignoring its per-row bind.
	if !strings.Contains(got, "[ ]") {
		t.Errorf("expected an off toggle to render [ ]; got:\n%s\n"+
			"consequence: the switch is not reading row.enabled, so an off setting looks the same as\n"+
			"an on one and the toggle is decorative.\n"+
			"remedy: renderSwitch must read its bind through evalWhenRow against the row scope.", got)
	}
	if !strings.Contains(got, "[x]") {
		t.Errorf("expected an on toggle to render [x]; got:\n%s\n"+
			"consequence: the switch draws every row unchecked regardless of row.enabled.\n"+
			"remedy: renderSwitch must read row.enabled per row.", got)
	}

	// The text setting's value renders. This is the direct witness of the
	// renderInput row-scope fix: an input inside a row_template binds to the
	// relative row.value, and before the fix renderInput resolved it through
	// resolveBind — which knows nothing of the row scope — so it collapsed to the
	// placeholder and every text row drew its hint instead of its value.
	if !strings.Contains(got, "nvim") {
		t.Errorf("expected the text setting to render its value \"nvim\"; got:\n%s\n"+
			"consequence: an input inside a row_template shows the placeholder instead of the row's\n"+
			"value — the row-position axis renderInput never resolved a relative bind on.\n"+
			"remedy: renderInput must resolve n.Bind through resolveBindRow with r.curRow.", got)
	}

	// A relative bind that lost its row scope resolves to the placeholder. The
	// settings rows must never render one: every gated control that draws has a
	// value, and a "[…]" is a row present but empty — the wrong-value-vs-honest-gap
	// distinction §4.6 signs.
	if strings.Contains(got, "[…]") {
		t.Errorf("a settings row rendered the placeholder [\u2026]; got:\n%s\n"+
			"consequence: a row.* bind resolved with no element in scope, so a control is present but\n"+
			"empty — the sub-renderer lost the row scope.\n"+
			"remedy: rowScopesFor must supply the field and the sub-renderer must read r.curRow.", got)
	}
}

// TestConfigRowKindDiscrimination is the counterfactual the whole scene turns on,
// run as an assertion rather than argued: a toggle row draws a checkbox and no
// text value, and a text row draws its value and no checkbox. This engine's `when`
// is a bare truthiness test with no comparison operator, so the row_template gates
// its two sibling nodes on the synthesized row.is_toggle/row.is_text booleans; if
// those two collapsed to one value — the mistake a single discriminator invites —
// a text row would show a stray checkbox and a toggle row a stray input, and the
// two node types would stop being mixed by kind.
func TestConfigRowKindDiscrimination(t *testing.T) {
	doc := configDoc(t)
	if err := doc.Validate(); err != nil {
		t.Fatalf("scene validation: %v", err)
	}

	r := Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, configState())
	lines := strings.Split(f.Plain(), "\n")

	rowOf := func(label string) string {
		for _, l := range lines {
			if strings.Contains(l, label) {
				return l
			}
		}
		t.Fatalf("no rendered row contains the label %q:\n%s", label, f.Plain())
		return ""
	}

	// The text row shows its value and carries no checkbox: row.is_toggle is false
	// for it, so the switch is gated off and only the input draws.
	editor := rowOf("Editor")
	if !strings.Contains(editor, "nvim") {
		t.Errorf("the text setting's row does not show its value:\n%q\n"+
			"consequence: the input in the row_template is not reading row.value for this row.\n"+
			"remedy: the input must resolve row.value through the row scope.", editor)
	}
	if strings.Contains(editor, "[x]") || strings.Contains(editor, "[ ]") {
		t.Errorf("the text setting's row drew a checkbox:\n%q\n"+
			"consequence: row.is_toggle is not false for a kind:\"text\" row, so the switch drew where\n"+
			"only the input should — the two node types are no longer mixed by kind.\n"+
			"remedy: rowScopesFor must synthesize row.is_toggle as (Kind == \"toggle\"), not a value\n"+
			"that is also true for a text row.", editor)
	}

	// A toggle row shows a checkbox and no text value: row.is_text is false for it,
	// so the input is gated off and only the switch draws.
	telemetry := rowOf("Telemetry")
	if !strings.Contains(telemetry, "[ ]") {
		t.Errorf("the toggle setting's row does not show its checkbox:\n%q\n"+
			"consequence: row.is_toggle is not true for a kind:\"toggle\" row, so the switch is gated off.\n"+
			"remedy: rowScopesFor must synthesize row.is_toggle as (Kind == \"toggle\").", telemetry)
	}
}

// TestConfigSceneMatchesGolden pins the plain frame. UPDATE_GOLDEN=1 regenerates
// it. This is the durable E5 pin: the /config screen's shape is frozen, so a later
// change to either list, the row_template scope, the switch/input mix or the
// config fold shows up as a reviewable golden diff.
func TestConfigSceneMatchesGolden(t *testing.T) {
	doc := configDoc(t)
	r := Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, configState())
	got := f.Plain()

	goldenPath := "../../testdata/CONFIG.frame"
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
		t.Errorf("config scene does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

// TestConfigSceneStyledGolden pins the styled frame, so a dropped or changed style
// token on the header, category rows, the switch/input controls or the footer is a
// golden diff rather than a silent regression. UPDATE_GOLDEN=1 regenerates it.
func TestConfigSceneStyledGolden(t *testing.T) {
	doc := configDoc(t)
	r := Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, configState())
	got := f.Styled()

	goldenPath := "../../testdata/CONFIG.styled"
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
		t.Errorf("config scene styled output does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}
