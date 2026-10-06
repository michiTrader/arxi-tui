package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// TestMain keeps every test that drives loop() away from the developer's real history
// file: Enter records a line, and a test run must not leave its prompts in ~/.config.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "arxi-history-test")
	if err != nil {
		os.Exit(1)
	}
	os.Setenv(historyEnv, dir)
	os.Setenv(configDirEnv, dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func TestHistoryWalkStashesTheLineBeingTyped(t *testing.T) {
	h := loadHistory("")
	h.Add("one")
	h.Add("two")
	if got, ok := h.Older("draft"); !ok || got != "two" {
		t.Fatalf("first Older = %q,%v; want two", got, ok)
	}
	if got, ok := h.Older("two"); !ok || got != "one" {
		t.Fatalf("second Older = %q,%v; want one", got, ok)
	}
	if _, ok := h.Older("one"); ok {
		t.Fatalf("Older past the oldest line must report nothing")
	}
	if got, _ := h.Newer(); got != "two" {
		t.Fatalf("Newer = %q; want two", got)
	}
	if got, ok := h.Newer(); !ok || got != "draft" {
		t.Fatalf("Newer past the newest must give the stashed draft back; got %q,%v", got, ok)
	}
	if _, ok := h.Newer(); ok {
		t.Fatalf("Newer on a fresh line must report nothing")
	}
}

func TestHistoryAddSkipsEmptyAndRepeats(t *testing.T) {
	h := loadHistory("")
	h.Add("  ")
	h.Add("a")
	h.Add("a")
	h.Add("b")
	h.Add("a")
	if want := []string{"a", "b", "a"}; !reflect.DeepEqual(h.lines, want) {
		t.Fatalf("lines = %q; want %q", h.lines, want)
	}
}

func TestHistorySurvivesARestartIncludingMultiLineLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "history")
	h := loadHistory(path)
	h.Add("first")
	h.Add("line one\nline two")
	got := loadHistory(path)
	if want := []string{"first", "line one\nline two"}; !reflect.DeepEqual(got.lines, want) {
		t.Fatalf("reloaded = %q; want %q", got.lines, want)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("history file mode = %v (%v); want 0600", fi.Mode().Perm(), err)
		}
	}
}

func TestHistoryKeepsOnlyTheNewestLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	h := loadHistory(path)
	for i := 0; i < historyMax+5; i++ {
		h.Add(strings.Repeat("x", i+1))
	}
	if len(h.lines) != historyMax {
		t.Fatalf("kept %d lines; want %d", len(h.lines), historyMax)
	}
	got := loadHistory(path)
	if len(got.lines) != historyMax || got.lines[0] != strings.Repeat("x", 6) {
		t.Fatalf("file kept %d lines starting at %d chars; want %d starting at 6", len(got.lines), len(got.lines[0]), historyMax)
	}
}

func TestHistoryDamagedFileIsAnEmptyHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	if err := os.WriteFile(path, []byte("not json\n\"ok\"\n\x00\x01\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := loadHistory(path)
	if want := []string{"ok"}; !reflect.DeepEqual(h.lines, want) {
		t.Fatalf("lines = %q; want only the readable one %q", h.lines, want)
	}
}

func TestHistoryStepKeys(t *testing.T) {
	cases := []struct {
		k    term.Key
		want int
	}{
		{term.Key{Type: term.KeyUp}, -1},
		{term.Key{Type: term.KeyDown}, 1},
		{term.Key{Type: term.KeyUp, Mod: term.ModShift}, 0},
		{term.Key{Type: term.KeyRunes, Runes: []rune{'p'}, Mod: term.ModCtrl}, -1},
		{term.Key{Type: term.KeyRunes, Runes: []rune{'n'}, Mod: term.ModCtrl}, 1},
		{term.Key{Type: term.KeyRunes, Runes: []rune{'p'}}, 0},
		{term.Key{Type: term.KeyEnter}, 0},
	}
	for _, c := range cases {
		if got := historyStep(c.k); got != c.want {
			t.Errorf("historyStep(%+v) = %d; want %d", c.k, got, c.want)
		}
	}
}

// runLoopKeys drives the real loop with a script and returns what it submitted.
func runLoopKeys(t *testing.T, script []scheduledEvent) []string {
	t.Helper()
	doc, err := scene.ParseDocument([]byte(factoryRAW))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	tty := newFakeTTY(80, 24, script)
	drv := &testDriver{evCh: make(chan fold.Event, 64), seq: 1000}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	return drv.submitted
}

func typed(s string) []scheduledEvent {
	var out []scheduledEvent
	for _, r := range s {
		out = append(out, scheduledEvent{5 * time.Millisecond, keyEvent(r)})
	}
	return out
}

func exitKeys() []scheduledEvent {
	return []scheduledEvent{
		{50 * time.Millisecond, ctrlCharEvent('c')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
	}
}

func TestLoopUpArrowRecallsTheLastSentLine(t *testing.T) {
	t.Setenv(historyEnv, t.TempDir())
	script := append(typed("hello"), scheduledEvent{10 * time.Millisecond, enterEvent()})
	script = append(script,
		scheduledEvent{20 * time.Millisecond, arrowEvent(term.KeyUp)},
		scheduledEvent{10 * time.Millisecond, enterEvent()},
	)
	script = append(script, exitKeys()...)
	got := runLoopKeys(t, script)
	if want := []string{"hello", "hello"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("submitted = %q; want %q (Up then Enter resends the last line)", got, want)
	}
}

func TestLoopDownArrowReturnsTheDraft(t *testing.T) {
	t.Setenv(historyEnv, t.TempDir())
	script := append(typed("one"), scheduledEvent{10 * time.Millisecond, enterEvent()})
	script = append(script, typed("draft")...)
	script = append(script,
		scheduledEvent{20 * time.Millisecond, arrowEvent(term.KeyUp)},
		scheduledEvent{10 * time.Millisecond, arrowEvent(term.KeyDown)},
		scheduledEvent{10 * time.Millisecond, enterEvent()},
	)
	script = append(script, exitKeys()...)
	got := runLoopKeys(t, script)
	if want := []string{"one", "draft"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("submitted = %q; want %q (the half-typed line must come back on Down)", got, want)
	}
}

func TestLoopHistoryIsRememberedAcrossRuns(t *testing.T) {
	t.Setenv(historyEnv, t.TempDir())
	first := append(typed("remember me"), scheduledEvent{10 * time.Millisecond, enterEvent()})
	first = append(first, exitKeys()...)
	runLoopKeys(t, first)

	second := []scheduledEvent{
		{20 * time.Millisecond, arrowEvent(term.KeyUp)},
		{10 * time.Millisecond, enterEvent()},
	}
	second = append(second, exitKeys()...)
	got := runLoopKeys(t, second)
	if want := []string{"remember me"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("submitted = %q; want %q from the previous run", got, want)
	}
}

func TestLoopCtrlPRecallsToo(t *testing.T) {
	t.Setenv(historyEnv, t.TempDir())
	script := append(typed("abc"), scheduledEvent{10 * time.Millisecond, enterEvent()})
	script = append(script,
		scheduledEvent{20 * time.Millisecond, ctrlCharEvent('p')},
		scheduledEvent{10 * time.Millisecond, enterEvent()},
	)
	script = append(script, exitKeys()...)
	got := runLoopKeys(t, script)
	if want := []string{"abc", "abc"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("submitted = %q; want %q", got, want)
	}
}

func TestLoopUpInMultiLineInputMovesTheCaretFirst(t *testing.T) {
	t.Setenv(historyEnv, t.TempDir())
	script := append(typed("old"), scheduledEvent{10 * time.Millisecond, enterEvent()})
	script = append(script,
		scheduledEvent{10 * time.Millisecond, pasteEvent("a\nb")},
		scheduledEvent{10 * time.Millisecond, arrowEvent(term.KeyUp)}, // row 2 -> row 1: no history
		scheduledEvent{10 * time.Millisecond, keyEvent('X')},
		scheduledEvent{10 * time.Millisecond, enterEvent()},
	)
	script = append(script, exitKeys()...)
	got := runLoopKeys(t, script)
	if want := []string{"old", "aX\nb"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("submitted = %q; want %q (Up inside a multi-line input moves the caret, it does not recall)", got, want)
	}
}

func TestLoopSlashMenuKeepsItsArrows(t *testing.T) {
	t.Setenv(historyEnv, t.TempDir())
	script := append(typed("old"), scheduledEvent{10 * time.Millisecond, enterEvent()})
	script = append(script, typed("/")...)
	script = append(script,
		scheduledEvent{10 * time.Millisecond, arrowEvent(term.KeyDown)},
		scheduledEvent{10 * time.Millisecond, arrowEvent(term.KeyUp)},
		scheduledEvent{10 * time.Millisecond, arrowEvent(term.KeyUp)},
		scheduledEvent{10 * time.Millisecond, keyEvent('x')},
		scheduledEvent{10 * time.Millisecond, enterEvent()},
	)
	script = append(script, exitKeys()...)
	got := runLoopKeys(t, script)
	for _, s := range got[1:] {
		if s == "old" {
			t.Fatalf("an arrow in the open slash menu recalled history: %q", got)
		}
	}
}
