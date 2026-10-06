package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// TestLoopStylePickChangesTheScreenAndIsSaved drives the real loop: /style, choose
// "plain" (the third row) and the user's own message loses its marker on screen while
// the choice lands on disk for the next start.
func TestLoopStylePickChangesTheScreenAndIsSaved(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(configDirEnv, dir)
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatal(err)
	}
	drv := &testDriver{evCh: make(chan fold.Event, 64)}
	drv.evCh <- fold.Event{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "zebra question"}}
	drv.evCh <- fold.Event{Type: "llm.response", Seq: 2, Payload: map[string]any{"text": "zebra answer", "model": "p/m"}}

	keys := func(s string) []scheduledEvent {
		var out []scheduledEvent
		for _, r := range s {
			out = append(out, scheduledEvent{5 * time.Millisecond, keyEvent(r)})
		}
		return out
	}
	script := []scheduledEvent{{120 * time.Millisecond, keyEvent('/')}}
	script = append(script, keys("style ")...)
	script = append(script,
		scheduledEvent{40 * time.Millisecond, arrowEvent(term.KeyDown)},
		scheduledEvent{20 * time.Millisecond, arrowEvent(term.KeyDown)},
		scheduledEvent{40 * time.Millisecond, enterEvent()},
		scheduledEvent{250 * time.Millisecond, ctrlCharEvent('c')},
		scheduledEvent{50 * time.Millisecond, ctrlCharEvent('c')})

	tty := newFakeTTY(100, 30, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}

	b, err := os.ReadFile(dir + "/style")
	if err != nil || strings.TrimSpace(string(b)) != "plain" {
		t.Fatalf("saved style = %q (err %v), want plain", b, err)
	}
	frames := strings.Split(tty.output(), "\x1b[?2026h")
	last := stripANSI(frames[len(frames)-1])
	if strings.Contains(last, "┃ zebra question") {
		t.Errorf("the marker is still on the message after choosing plain:\n%s", last)
	}
	if !strings.Contains(last, "zebra question") {
		t.Errorf("the message vanished:\n%s", last)
	}
}
