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
func barsAfterResume(t *testing.T, saved []fold.Event, want string) []string {
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
	script := []scheduledEvent{{10 * time.Millisecond, keyEvent(' ')}, {10 * time.Millisecond, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyBackspace}}}}
	script = append(script, keys("/clear")...)
	script = append(script, scheduledEvent{40 * time.Millisecond, enterEvent()})
	script = append(script, keys("/resume ")...)
	script = append(script, scheduledEvent{80 * time.Millisecond, enterEvent()})
	// The terminal stays open and the keys wait for the saved conversation to be taken in.
	// The loop reads the driver's events and the keyboard from two channels; a key sent
	// while the conversation is still queued can be handled first, so /clear ran before
	// anything was saved and /resume had nothing to resume (seen on a slow Windows runner).
	// The loop handles one thing at a time, so an empty queue means every saved event has
	// been folded and written before the next key is read.
	tty := newOpenFakeTTY(130, 30, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, "") }()
	queueDeadline := time.Now().Add(30 * time.Second)
	for len(drv.evCh) > 0 && time.Now().Before(queueDeadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond) // the last event taken may still be being handled
	go func() {
		for _, ev := range script {
			time.Sleep(ev.delay)
			tty.events <- ev.event
		}
	}()
	// Wait for the bar the test expects; if it never comes the caller's assertion reports
	// what was drawn instead.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if bars := resumedBars(tty.output()); len(bars) > 0 && strings.Contains(bars[len(bars)-1], want) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, ev := range exitKeys() {
		time.Sleep(ev.delay)
		tty.events <- ev.event
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the loop did not quit")
	}
	return resumedBars(tty.output())
}

// resumedBars is the usage segment of every frame drawn after the /resume menu was shown,
// skipping the frames where the menu hides the bar. Before that point the bar still shows the
// figures of the events the test injected, which is not what /resume restored; counting them
// made the wait end early and the exit keys land in the middle of the command.
func resumedBars(output string) []string {
	frames := strings.Split(output, "\x1b[?2026h")
	re := regexp.MustCompile(`ctx [^·]*·`)
	menu := -1
	for i, f := range frames {
		if strings.Contains(stripANSI(f), "/resume ") {
			menu = i
		}
	}
	if menu < 0 {
		return nil
	}
	var bars []string
	for _, f := range frames[menu+1:] {
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
	}, "ctx 36k/")
	if len(bars) == 0 || !strings.Contains(bars[len(bars)-1], "ctx 36k/") {
		t.Fatalf("the resumed bar should read ctx 36k, got %q", bars)
	}
}

func TestAfterResumeWithNoFigureTheBarEstimatesAndSaysSo(t *testing.T) {
	bars := barsAfterResume(t, []fold.Event{
		{Type: "run.prompt", Payload: map[string]any{"text": strings.Repeat("a", 8000)}},
		{Type: "llm.response", Payload: map[string]any{"text": strings.Repeat("b", 8000), "model": "p/m"}},
	}, "ctx ~4k/")
	if len(bars) == 0 || !strings.Contains(bars[len(bars)-1], "ctx ~4k/") {
		t.Fatalf("with no reported size the bar should estimate from the history (~4k), got %q", bars)
	}
}
