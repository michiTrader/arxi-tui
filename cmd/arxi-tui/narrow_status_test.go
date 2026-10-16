package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/michiTrader/arxi_tui/internal/defaultscene"
	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// The owner saw "arxi-tui" cut to "arxi-ti" while narrowing a PowerShell window: the info row
// (folder · branch · ...) was wider than the terminal and the terminal cut it, silently. The
// row now wraps its words onto the next row (default) or drops whole segments
// (/ui text status.narrow hide). These tests drive the real loop with the built-in scene and
// look at what is emitted to the terminal, row by row.

// narrowRun starts the loop on a terminal `width` wide, inside a folder called arxi-tui that
// is a git checkout on a long branch, with the given texts.json, and returns every row the
// loop emitted (stripped of colours), one string per painted row.
func narrowRun(t *testing.T, width int, texts string) (rows []string, cwdShown string) {
	t.Helper()
	home := t.TempDir()
	proj := filepath.Join(home, "arxi-tui")
	if err := os.MkdirAll(filepath.Join(proj, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".git", "HEAD"), []byte("ref: refs/heads/some-long-branch-name\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Chdir(proj)
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	t.Setenv(historyEnv, t.TempDir())
	if texts != "" {
		if err := os.WriteFile(filepath.Join(cfg, "texts.json"), []byte(texts), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cwd, _ := os.Getwd()
	cwdShown = shortCwd(cwd)

	doc, err := scene.ParseNamed(defaultscene.Name, defaultscene.JSON)
	if err != nil {
		t.Fatal(err)
	}
	tty := newFakeTTY(width, 24, append(typed("x"), exitKeys()...))
	drv := &testDriver{evCh: make(chan fold.Event, 64), seq: 1000}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	cup := regexp.MustCompile("\x1b\\[\\d+;1H\x1b\\[K")
	tty.mu.Lock()
	defer tty.mu.Unlock()
	for _, f := range tty.frames {
		for _, piece := range cup.Split(f, -1)[1:] {
			rows = append(rows, strings.TrimRight(stripANSI(piece), " \r\n"))
		}
	}
	return rows, cwdShown
}

func noRowWiderThan(t *testing.T, rows []string, width int) {
	t.Helper()
	for _, r := range rows {
		if w := ansi.StringWidth(r); w > width {
			t.Errorf("the loop painted a %d-column row on a %d-column terminal (the terminal would cut it): %q", w, width, r)
		}
	}
}

func TestLoopTheInfoRowWrapsByDefaultAndNothingIsCut(t *testing.T) {
	const width = 24
	rows, cwd := narrowRun(t, width, "")
	noRowWiderThan(t, rows, width)
	all := strings.Join(rows, "\n")
	if !strings.Contains(all, cwd) {
		t.Errorf("the folder %q was not drawn whole:\n%s", cwd, all)
	}
	if !strings.Contains(all, "some-long-branch-name") {
		t.Errorf("the branch was not drawn whole (it should have moved to the next row):\n%s", all)
	}
}

func TestLoopTheInfoRowHidesWholeSegmentsWhenTheUserAsksForIt(t *testing.T) {
	const width = 24
	rows, cwd := narrowRun(t, width, `{"status.narrow":"hide"}`)
	noRowWiderThan(t, rows, width)
	all := strings.Join(rows, "\n")
	if !strings.Contains(all, cwd) {
		t.Errorf("the folder %q is the first segment and fits, yet was not drawn:\n%s", cwd, all)
	}
	if strings.Contains(all, "some-long") || strings.Contains(all, "· some") {
		t.Errorf("hide left part of the branch on screen (a whole segment or nothing):\n%s", all)
	}
}

func TestLoopTheInfoRowStaysOneRowWhenItFits(t *testing.T) {
	for _, texts := range []string{"", `{"status.narrow":"hide"}`} {
		rows, cwd := narrowRun(t, 100, texts)
		noRowWiderThan(t, rows, 100)
		found := false
		for _, r := range rows {
			if strings.Contains(r, cwd) && strings.Contains(r, "some-long-branch-name") {
				found = true
			}
		}
		if !found {
			t.Errorf("texts %q: folder and branch should share one row at 100 columns:\n%s", texts, strings.Join(rows, "\n"))
		}
	}
}

func TestTheNarrowSettingAcceptsOnlyWrapOrHide(t *testing.T) {
	for _, ok := range []string{"wrap", "hide", "Hide", " wrap "} {
		if err := validChatLook("status.narrow", ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "cut", "yes", "truncate"} {
		if validChatLook("status.narrow", bad) == nil {
			t.Errorf("%q must be refused: a value that does nothing would report success", bad)
		}
	}
	if _, ok := textIndex["status.narrow"]; !ok {
		t.Fatal("status.narrow is not in the registry, so /ui text and ui_guide cannot reach it")
	}
	if textIndex["status.narrow"].def != "wrap" {
		t.Errorf("the factory value is %q, want wrap (nothing is lost)", textIndex["status.narrow"].def)
	}
}

func TestTheNarrowModeFollowsTheUsersText(t *testing.T) {
	setActiveTexts(userTexts{}) // other tests that start the loop leave their texts active
	defer setActiveTexts(userTexts{})
	if got := narrowMode(); got != engine.NarrowWrap {
		t.Errorf("factory mode = %q", got)
	}
	next, err := applyTexts(userTexts{}, map[string]string{"status.narrow": "hide"})
	if err != nil {
		t.Fatal(err)
	}
	setActiveTexts(next)
	defer setActiveTexts(userTexts{})
	if got := narrowMode(); got != engine.NarrowHide {
		t.Errorf("after /ui text status.narrow hide the mode is %q", got)
	}
	if _, err := applyTexts(userTexts{}, map[string]string{"status.narrow": "cut"}); err == nil {
		t.Error("/ui text status.narrow cut was accepted")
	}
}
