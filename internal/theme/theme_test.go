package theme

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/ui"
)

func TestLoadValidTheme(t *testing.T) {
	tmpDir := t.TempDir()
	themePath := filepath.Join(tmpDir, "theme.json")

	themeJSON := `{
		"dim": { "attrs": ["dim"] },
		"header": { "fg": "bright-white", "attrs": ["bold"] },
		"warn": { "fg": "yellow" },
		"error": { "fg": "red", "attrs": ["bold"] },
		"success": { "fg": "#22c55e" }
	}`

	if err := os.WriteFile(themePath, []byte(themeJSON), 0644); err != nil {
		t.Fatalf("failed to write test theme: %v", err)
	}

	theme, err := Load(themePath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Check that dim token resolves to AttrDim
	dimStyle := theme.Resolve("dim")
	if !dimStyle.Has(ui.AttrDim) {
		t.Errorf("dim token should have AttrDim; got %+v", dimStyle)
	}
	if dimStyle.FG.Kind != ui.ColorNone {
		t.Errorf("dim token should have no foreground; got %+v", dimStyle.FG)
	}

	// Check that header token has bold + bright-white
	headerStyle := theme.Resolve("header")
	if !headerStyle.Has(ui.AttrBold) {
		t.Errorf("header token should have AttrBold; got %+v", headerStyle)
	}
	if headerStyle.FG.Kind != ui.ColorIndex || headerStyle.FG.R != ui.White+ui.Bright {
		t.Errorf("header token should have bright-white (index %d); got %+v", ui.White+ui.Bright, headerStyle.FG)
	}

	// Check that success token has hex RGB
	successStyle := theme.Resolve("success")
	if successStyle.FG.Kind != ui.ColorRGB {
		t.Errorf("success token should have RGB color; got kind %d", successStyle.FG.Kind)
	}
	if successStyle.FG.R != 0x22 || successStyle.FG.G != 0xc5 || successStyle.FG.B != 0x5e {
		t.Errorf("success token should be #22c55e; got #%02x%02x%02x", successStyle.FG.R, successStyle.FG.G, successStyle.FG.B)
	}
}

func TestResolveUndefinedToken(t *testing.T) {
	theme := FromMap(map[string]ui.Style{
		"dim": {Attrs: ui.AttrDim},
	})

	// Resolving an undefined token returns the zero style, never an error.
	// The validator is responsible for checking references before resolution.
	style := theme.Resolve("undefined")
	if !style.IsZero() {
		t.Errorf("undefined token should resolve to zero style; got %+v", style)
	}
}

func TestHasToken(t *testing.T) {
	theme := FromMap(map[string]ui.Style{
		"dim":    {Attrs: ui.AttrDim},
		"header": {Attrs: ui.AttrBold},
	})

	if !theme.Has("dim") {
		t.Errorf("theme should have 'dim' token")
	}
	if !theme.Has("header") {
		t.Errorf("theme should have 'header' token")
	}
	if theme.Has("undefined") {
		t.Errorf("theme should not have 'undefined' token")
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	themePath := filepath.Join(tmpDir, "broken.json")

	if err := os.WriteFile(themePath, []byte("{not valid json"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := Load(themePath)
	if err == nil {
		t.Fatal("Load should fail on invalid JSON")
	}
}

func TestLoadInvalidColor(t *testing.T) {
	tmpDir := t.TempDir()
	themePath := filepath.Join(tmpDir, "badcolor.json")

	themeJSON := `{ "bad": { "fg": "not-a-color" } }`
	if err := os.WriteFile(themePath, []byte(themeJSON), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := Load(themePath)
	if err == nil {
		t.Fatal("Load should fail on invalid color")
	}
}

func TestLoadInvalidAttribute(t *testing.T) {
	tmpDir := t.TempDir()
	themePath := filepath.Join(tmpDir, "badattr.json")

	themeJSON := `{ "bad": { "attrs": ["blink"] } }`
	if err := os.WriteFile(themePath, []byte(themeJSON), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := Load(themePath)
	if err == nil {
		t.Fatal("Load should fail on invalid attribute")
	}
}

func TestTokens(t *testing.T) {
	theme := FromMap(map[string]ui.Style{
		"dim":    {Attrs: ui.AttrDim},
		"header": {Attrs: ui.AttrBold},
		"warn":   {FG: ui.Idx(ui.Yellow)},
	})

	tokens := theme.Tokens()
	if len(tokens) != 3 {
		t.Fatalf("expected 3 tokens; got %d", len(tokens))
	}

	// Check that all expected tokens are present (order unspecified)
	found := make(map[string]bool)
	for _, name := range tokens {
		found[name] = true
	}
	for _, expected := range []string{"dim", "header", "warn"} {
		if !found[expected] {
			t.Errorf("expected token %q not found in %v", expected, tokens)
		}
	}
}

func TestZeroTheme(t *testing.T) {
	var theme *Theme

	// Nil theme should not panic
	if theme.Has("anything") {
		t.Error("nil theme should report no tokens")
	}
	style := theme.Resolve("anything")
	if !style.IsZero() {
		t.Errorf("nil theme should resolve to zero style; got %+v", style)
	}
	if tokens := theme.Tokens(); len(tokens) != 0 {
		t.Errorf("nil theme should return empty token list; got %v", tokens)
	}
}
