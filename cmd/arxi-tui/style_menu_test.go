package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/engine"
)

func TestNormalizePromptStyle(t *testing.T) {
	cases := map[string]string{
		"": engine.PromptBar, "bar": engine.PromptBar, "BAND": engine.PromptBand,
		" plain\n": engine.PromptPlain, "neon": engine.PromptBar,
	}
	for in, want := range cases {
		if got := normalizePromptStyle(in); got != want {
			t.Errorf("normalizePromptStyle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPromptStyleIsRemembered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "style")
	if got := loadPromptStyle(path); got != engine.PromptBar {
		t.Errorf("nothing saved yet: got %q, want the default", got)
	}
	if err := savePromptStyle(path, engine.PromptBand); err != nil {
		t.Fatal(err)
	}
	if got := loadPromptStyle(path); got != engine.PromptBand {
		t.Errorf("after saving band: got %q", got)
	}
	// Choosing the default again leaves nothing on disk.
	if err := savePromptStyle(path, engine.PromptBar); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the default should remove the file, stat err = %v", err)
	}
	// Removing a file that was never there is not an error.
	if err := savePromptStyle(path, engine.PromptBar); err != nil {
		t.Errorf("saving the default twice: %v", err)
	}
}

func TestPromptStyleSurvivesAHandEditedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "style")
	os.WriteFile(path, []byte("not-a-style\n"), 0o600)
	if got := loadPromptStyle(path); got != engine.PromptBar {
		t.Errorf("garbage in the file: got %q, want the default", got)
	}
	if err := savePromptStyle("", engine.PromptBand); err == nil {
		t.Error("saving with nowhere to keep it must say so")
	}
}

func TestStyleMenuMarksTheStyleInUse(t *testing.T) {
	rows := styleMenuData(engine.PromptBand)
	if len(rows) != 3 {
		t.Fatalf("want 3 styles, got %d", len(rows))
	}
	for _, r := range rows {
		if r.Current != (r.Ref == engine.PromptBand) {
			t.Errorf("%s: Current = %v", r.Ref, r.Current)
		}
	}
}
