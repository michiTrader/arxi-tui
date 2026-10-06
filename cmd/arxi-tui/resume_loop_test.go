package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// resumeDriver is a testDriver that also keeps what /clear and /resume tell it.
type resumeDriver struct {
	testDriver
	mu      sync.Mutex
	cleared int
	resumed []driver.ChatTurn
}

func (d *resumeDriver) ClearSession() { d.mu.Lock(); d.cleared++; d.mu.Unlock() }
func (d *resumeDriver) ResumeSession(h []driver.ChatTurn) {
	d.mu.Lock()
	d.resumed = h
	d.mu.Unlock()
}

// TestLoopResumeBringsBackAConversation drives the real loop: a conversation is saved
// while it happens, /clear empties the screen, and /resume puts it back on the screen and
// hands the model its history.
func TestLoopResumeBringsBackAConversation(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatal(err)
	}
	drv := &resumeDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}
	// The driver answers every question, so the conversation has both halves.
	drv.evCh <- fold.Event{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "remember zebra"}}
	drv.evCh <- fold.Event{Type: "llm.response", Seq: 2, Payload: map[string]any{"text": "noted: zebra", "model": "p/m"}}

	keys := func(s string) []scheduledEvent {
		var out []scheduledEvent
		for _, r := range s {
			out = append(out, scheduledEvent{5 * time.Millisecond, keyEvent(r)})
		}
		return out
	}
	enter := scheduledEvent{40 * time.Millisecond, enterEvent()}
	script := []scheduledEvent{{120 * time.Millisecond, keyEvent(' ')}, {10 * time.Millisecond, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyBackspace}}}}
	script = append(script, keys("/clear")...)
	script = append(script, enter)
	script = append(script, keys("/resume ")...)
	script = append(script, scheduledEvent{80 * time.Millisecond, enterEvent()}) // the first row
	script = append(script, scheduledEvent{200 * time.Millisecond, ctrlCharEvent('c')}, scheduledEvent{50 * time.Millisecond, ctrlCharEvent('c')})

	tty := newFakeTTY(100, 30, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	drv.mu.Lock()
	defer drv.mu.Unlock()
	if drv.cleared < 2 {
		t.Errorf("ClearSession ran %d times; /clear and then /resume must both reset", drv.cleared)
	}
	if len(drv.resumed) != 2 || drv.resumed[0].Text != "remember zebra" || drv.resumed[1].Text != "noted: zebra" {
		t.Errorf("the model must get the old history, got %+v", drv.resumed)
	}
	// The answer is on screen before /clear, gone after it, and back only through
	// /resume: look for it in the frames drawn after the menu was last open.
	frames := strings.Split(tty.output(), "\x1b[?2026h")
	for i := range frames {
		frames[i] = stripANSI(frames[i])
	}
	menu, cleared := -1, -1
	for i, f := range frames {
		if strings.Contains(f, "/resume ") {
			menu = i
		}
		if strings.Contains(f, "/clear") {
			cleared = i
		}
	}
	if menu < 0 || cleared < 0 || menu <= cleared {
		t.Fatalf("the script did not reach the menu after /clear (clear %d, menu %d of %d frames)", cleared, menu, len(frames))
	}
	for _, f := range frames[cleared+1 : menu] {
		if strings.Contains(f, "noted: zebra") {
			t.Fatalf("/clear left the old answer on screen:\n%s", f)
		}
	}
	var back bool
	for _, f := range frames[menu+1:] {
		back = back || strings.Contains(f, "noted: zebra")
	}
	if !back {
		t.Error("the resumed answer never came back to the screen")
	}
}
