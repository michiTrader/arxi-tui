package main

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// compactDriver is a hub driver that can say what its history holds and records the
// compactions it was asked for.
type compactDriver struct {
	hubTestDriver
	mu       sync.Mutex
	history  []driver.ChatTurn
	compacts []string
	busy     int // Compact answers "busy" this many times first
}

func (d *compactDriver) ChatHistory() []driver.ChatTurn { return d.history }

func (d *compactDriver) Compact(_ context.Context, focus string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.busy > 0 {
		d.busy--
		return errChatBusy
	}
	d.compacts = append(d.compacts, focus)
	return nil
}

func (d *compactDriver) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.compacts)
}

func settingsLoop(t *testing.T, drv *compactDriver, script []scheduledEvent) string {
	t.Helper()
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatal(err)
	}
	tty := newFakeTTY(110, 30, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	return stripANSI(tty.output())
}

func newCompactDriver(window int, turns int) *compactDriver {
	// A model no table knows, so a window of 0 really is "unknown" here.
	core := &modelCore{def: "openai/mimo-pro", models: []driver.ModelRow{
		{Provider: "openai", ID: "mimo-pro", Enabled: true, ContextWindow: window},
	}}
	return &compactDriver{
		hubTestDriver: hubTestDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}, hub: core},
		history:       longHistory(turns, 1000),
	}
}

// answered feeds the loop one finished turn that left `context` tokens in the window.
func answered(drv *compactDriver, context float64) {
	for i, e := range []fold.Event{
		{Type: "run.prompt", Actor: "assistant", Payload: map[string]any{"text": "hi"}},
		{Type: "llm.response", Actor: "assistant", Payload: map[string]any{"text": "ok", "model": "openai/gpt-4o",
			"tokens_in": context, "tokens_out": float64(5), "context_tokens": context}},
		{Type: "agent.turn_done", Actor: "assistant"},
	} {
		e.Seq = int64(2000 + i)
		drv.evCh <- e
	}
}

func TestLoopSettingsPickIsSavedShownAndDrivesAutocompact(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(configDirEnv, dir)
	drv := newCompactDriver(1_000_000, 10)

	script := append([]scheduledEvent{}, typed("/settings 80")...)
	script = append(script,
		scheduledEvent{400 * time.Millisecond, enterEvent()},
		scheduledEvent{1500 * time.Millisecond, ctrlCharEvent('c')},
		scheduledEvent{50 * time.Millisecond, ctrlCharEvent('c')})
	go func() {
		time.Sleep(900 * time.Millisecond)
		answered(drv, 900_000) // 90% of the window, at an 80% setting
	}()
	out := settingsLoop(t, drv, script)

	saved := loadAutoCompact(settingsPath())
	if !saved.On || saved.Percent != 80 {
		t.Fatalf("the pick was not saved: %+v (dir %s)", saved, dir)
	}
	if n := drv.count(); n != 1 {
		t.Fatalf("a turn at 90%% of the window with autocompact at 80%% asked for %d compactions, want 1", n)
	}
	if !strings.Contains(out, "autocompact 80%") {
		t.Errorf("the status bar never showed autocompact:\n%s", out)
	}
	if !strings.Contains(out, "90%") {
		t.Errorf("the status bar never showed the share of the window:\n%s", out)
	}
	if len(drv.submitted) != 0 {
		t.Errorf("the settings line reached the chat as %q", drv.submitted)
	}
}

func TestLoopAutocompactOffNeverCompacts(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	drv := newCompactDriver(1_000_000, 10)
	go func() {
		time.Sleep(300 * time.Millisecond)
		answered(drv, 990_000)
	}()
	settingsLoop(t, drv, []scheduledEvent{{900 * time.Millisecond, ctrlCharEvent('c')}, {50 * time.Millisecond, ctrlCharEvent('c')}})
	if n := drv.count(); n != 0 {
		t.Fatalf("autocompact was never switched on, yet %d compactions were asked for", n)
	}
}

func TestLoopAutocompactWaitsOutABusySession(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(configDirEnv, dir)
	if err := saveAutoCompact(dir+string(os.PathSeparator)+"settings.json", autoCompactSetting{On: true, Percent: 80}); err != nil {
		t.Fatal(err)
	}
	drv := newCompactDriver(1_000_000, 10)
	drv.busy = 2 // the session is still freeing itself when turn_done arrives
	go func() {
		time.Sleep(300 * time.Millisecond)
		answered(drv, 900_000)
	}()
	settingsLoop(t, drv, []scheduledEvent{{1200 * time.Millisecond, ctrlCharEvent('c')}, {50 * time.Millisecond, ctrlCharEvent('c')}})
	if n := drv.count(); n != 1 {
		t.Fatalf("a busy session should be retried until it is free: %d compactions", n)
	}
}

func TestLoopAutocompactNeedsAKnownWindow(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(configDirEnv, dir)
	saveAutoCompact(dir+string(os.PathSeparator)+"settings.json", autoCompactSetting{On: true, Percent: 80})
	drv := newCompactDriver(0, 10) // the core did not say the window
	go func() {
		time.Sleep(300 * time.Millisecond)
		answered(drv, 900_000)
	}()
	settingsLoop(t, drv, []scheduledEvent{{900 * time.Millisecond, ctrlCharEvent('c')}, {50 * time.Millisecond, ctrlCharEvent('c')}})
	if n := drv.count(); n != 0 {
		t.Fatalf("with no known window %d compactions were asked for", n)
	}
}

// `/settings` alone, then Enter, is how the command is meant to be used: it opens the
// menu, and the next words filter it. (The test above types the space itself.)
func TestLoopSettingsEnterOnTheBareCommandOpensTheMenu(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	drv := newCompactDriver(1_000_000, 10)
	script := append([]scheduledEvent{}, typed("/settings")...)
	script = append(script, scheduledEvent{100 * time.Millisecond, enterEvent()})
	script = append(script, typed("70")...)
	script = append(script,
		scheduledEvent{200 * time.Millisecond, enterEvent()},
		scheduledEvent{300 * time.Millisecond, ctrlCharEvent('c')},
		scheduledEvent{50 * time.Millisecond, ctrlCharEvent('c')})
	settingsLoop(t, drv, script)
	if got := loadAutoCompact(settingsPath()); !got.On || got.Percent != 70 {
		t.Fatalf("Enter on /settings, then 70, then Enter saved %+v, want on at 70", got)
	}
	if len(drv.submitted) != 0 {
		t.Errorf("the command word reached the chat as %q", drv.submitted)
	}
}
