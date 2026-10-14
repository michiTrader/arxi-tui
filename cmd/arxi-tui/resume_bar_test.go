package main

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// historyResumeDriver keeps the history /resume hands it and reports it back, as serveDriver does.
type historyResumeDriver struct {
	resumeDriver
	hist []driver.ChatTurn
}

func (d *historyResumeDriver) ResumeSession(h []driver.ChatTurn) {
	d.mu.Lock()
	d.hist = h
	d.mu.Unlock()
}
func (d *historyResumeDriver) ChatHistory() []driver.ChatTurn {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]driver.ChatTurn(nil), d.hist...)
}

// barsAfterResume saves the given events as a conversation, /clear, /resume it, and returns
// the usage segment of every frame drawn once the answer is back on screen.
func barsAfterResume(t *testing.T, saved []fold.Event) []string {
	t.Helper()
	t.Setenv(configDirEnv, t.TempDir())
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatal(err)
	}
	drv := &historyResumeDriver{resumeDriver: resumeDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}}
	for i, e := range saved {
		e.Seq = int64(i + 1)
		drv.evCh <- e
	}
	keys := func(s string) []scheduledEvent {
		var out []scheduledEvent
		for _, r := range s {
			out = append(out, scheduledEvent{5 * time.Millisecond, keyEvent(r)})
		}
		return out
	}
	script := []scheduledEvent{{120 * time.Millisecond, keyEvent(' ')}, {10 * time.Millisecond, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyBackspace}}}}
	script = append(script, keys("/clear")...)
	script = append(script, scheduledEvent{40 * time.Millisecond, enterEvent()})
	script = append(script, keys("/resume ")...)
	script = append(script, scheduledEvent{80 * time.Millisecond, enterEvent()})
	script = append(script, scheduledEvent{200 * time.Millisecond, ctrlCharEvent('c')}, scheduledEvent{50 * time.Millisecond, ctrlCharEvent('c')})
	tty := newFakeTTY(130, 30, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatal(err)
	}
	frames := strings.Split(tty.output(), "\x1b[?2026h")
	re := regexp.MustCompile(`ctx [^·]*·`)
	var bars []string
	cleared := -1
	for i, f := range frames {
		if strings.Contains(stripANSI(f), "/clear") {
			cleared = i
		}
	}
	for _, f := range frames[cleared+1:] {
		f = stripANSI(f)
		if strings.Contains(f, "/resume ") {
			continue // the menu hides the bar
		}
		if m := re.FindString(f); m != "" {
			bars = append(bars, m)
		}
	}
	return bars
}

func TestAfterResumeTheBarShowsTheContextOfTheLastRequest(t *testing.T) {
	bars := barsAfterResume(t, []fold.Event{
		{Type: "run.prompt", Payload: map[string]any{"text": "mira"}},
		{Type: "chat.usage", Payload: map[string]any{"context_tokens": 36000.0}},
		{Type: "chat.error", Payload: map[string]any{"text": "context deadline exceeded"}},
	})
	if len(bars) == 0 || !strings.Contains(bars[len(bars)-1], "ctx 36k/") {
		t.Fatalf("the resumed bar should read ctx 36k, got %q", bars)
	}
}

func TestAfterResumeWithNoFigureTheBarEstimatesAndSaysSo(t *testing.T) {
	bars := barsAfterResume(t, []fold.Event{
		{Type: "run.prompt", Payload: map[string]any{"text": strings.Repeat("a", 8000)}},
		{Type: "llm.response", Payload: map[string]any{"text": strings.Repeat("b", 8000), "model": "p/m"}},
	})
	if len(bars) == 0 || !strings.Contains(bars[len(bars)-1], "ctx ~4k/") {
		t.Fatalf("with no reported size the bar should estimate from the history (~4k), got %q", bars)
	}
}
