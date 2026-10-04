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
	"github.com/michiTrader/arxi_tui/internal/theme"
)

func TestClearCommandMatching(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"/clear", true},
		{"  /clear  ", true},
		{"/clear now", false},
		{"/help", false},
		{"clear", false},
		{"", false},
	} {
		if got := clearCommand(tc.in, 0); got != tc.want {
			t.Errorf("clearCommand(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
	// A menu pick: "/cle" filters to one row, "clear".
	if !clearCommand("/cle", 0) {
		t.Error("the highlighted menu row 'clear' should count as /clear")
	}
	if clearCommand("/hel", 0) {
		t.Error("the 'help' row must not clear")
	}
}

// blockingChat answers only when released, so a turn can be in flight at /clear.
type blockingChat struct {
	release chan struct{}
	mu      sync.Mutex
	hists   [][]driver.ChatTurn
}

func (b *blockingChat) Hello() *driver.Hello {
	return &driver.Hello{Implemented: []string{"chat.send"}}
}
func (b *blockingChat) SubmitChatSend(ctx context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error) {
	b.mu.Lock()
	b.hists = append(b.hists, p.History)
	b.mu.Unlock()
	<-b.release
	return &driver.ChatSendResult{Text: "answer to " + p.Prompt, Model: "m", Provider: "p"}, nil
}

func drain(out chan fold.Event, until string, d time.Duration) []fold.Event {
	var got []fold.Event
	deadline := time.After(d)
	for {
		select {
		case ev := <-out:
			got = append(got, ev)
			if ev.Type == until {
				return got
			}
		case <-deadline:
			return got
		}
	}
}

func TestChatResetForgetsHistory(t *testing.T) {
	core := &blockingChat{release: make(chan struct{}, 4)}
	out := make(chan fold.Event, 32)
	c := newChatSession(core, out)

	core.release <- struct{}{}
	if err := c.send(context.Background(), "one"); err != nil {
		t.Fatal(err)
	}
	drain(out, "agent.turn_done", 2*time.Second)

	c.reset()

	core.release <- struct{}{}
	if err := c.send(context.Background(), "two"); err != nil {
		t.Fatal(err)
	}
	drain(out, "agent.turn_done", 2*time.Second)

	core.mu.Lock()
	defer core.mu.Unlock()
	if len(core.hists) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(core.hists))
	}
	if len(core.hists[1]) != 0 {
		t.Errorf("history leaked into the new session: %+v", core.hists[1])
	}
}

func TestChatResetOrphansAnInFlightTurn(t *testing.T) {
	core := &blockingChat{release: make(chan struct{}, 4)}
	out := make(chan fold.Event, 32)
	c := newChatSession(core, out)

	if err := c.send(context.Background(), "slow"); err != nil {
		t.Fatal(err)
	}
	// Wait for the request to be in flight, then clear and let it finish.
	for i := 0; i < 200; i++ {
		core.mu.Lock()
		n := len(core.hists)
		core.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	drain(out, "agent.activated", time.Second) // the pre-clear events are not the point
	c.reset()
	// The new session accepts a line at once, even though the old turn is pending.
	core.release <- struct{}{} // finishes the orphan
	time.Sleep(50 * time.Millisecond)
	for {
		select {
		case ev := <-out:
			if ev.Type == "llm.response" || ev.Type == "agent.turn_done" {
				t.Fatalf("the orphaned turn reported %s into the new session", ev.Type)
			}
			continue
		default:
		}
		break
	}
	c.mu.Lock()
	hl := len(c.history)
	c.mu.Unlock()
	if hl != 0 {
		t.Errorf("the orphaned turn wrote %d history entries", hl)
	}
	core.release <- struct{}{}
	if err := c.send(context.Background(), "fresh"); err != nil {
		t.Fatalf("a cleared session must accept a line: %v", err)
	}
}

// clearDriver is a testDriver that also supports /clear.
type clearDriver struct {
	testDriver
	cleared int
}

func (d *clearDriver) ClearSession() { d.cleared++ }

func TestLoopClearWipesTheTranscript(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	script := []scheduledEvent{
		{0, keyEvent('h')},
		{10 * time.Millisecond, keyEvent('i')},
		{10 * time.Millisecond, enterEvent()},
		{100 * time.Millisecond, keyEvent('/')},
		{10 * time.Millisecond, keyEvent('c')},
		{10 * time.Millisecond, keyEvent('l')},
		{10 * time.Millisecond, keyEvent('e')},
		{10 * time.Millisecond, keyEvent('a')},
		{10 * time.Millisecond, keyEvent('r')},
		{10 * time.Millisecond, enterEvent()},
		{100 * time.Millisecond, keyEvent('o')},
		{10 * time.Millisecond, keyEvent('k')},
		{10 * time.Millisecond, enterEvent()},
		{100 * time.Millisecond, ctrlCharEvent('c')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
	}
	tty := newFakeTTY(80, 24, script)
	drv := &clearDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	if drv.cleared != 1 {
		t.Errorf("ClearSession called %d times, want 1", drv.cleared)
	}
	// "/clear" itself is never sent to the model.
	for _, s := range drv.submitted {
		if strings.Contains(s, "clear") {
			t.Errorf("/clear was submitted as a prompt: %q", s)
		}
	}
	if len(drv.submitted) != 2 || drv.submitted[0] != "hi" || drv.submitted[1] != "ok" {
		t.Fatalf("submitted = %q", drv.submitted)
	}
	// The last frame holds the new session only: "ok" is there, "hi" is gone.
	tty.mu.Lock()
	frames := append([]string(nil), tty.frames...)
	tty.mu.Unlock()
	// Find the first frame painted after /clear: from there on "hi" must not
	// be drawn again while "ok" must appear.
	cleared := -1
	for i, f := range frames {
		if frameHasUserLine(f, "ok") {
			cleared = i
			break
		}
	}
	if cleared < 0 {
		t.Fatalf("the new session's message was never drawn")
	}
	for _, f := range frames[cleared:] {
		if frameHasUserLine(f, "hi") {
			t.Errorf("the old session survived /clear:\n%s", stripANSI(f))
			break
		}
	}
}

// frameHasUserLine reports whether a row of the frame is the user's message text,
// drawn behind the "┃ " marker.
func frameHasUserLine(frame, text string) bool {
	for _, l := range strings.Split(stripANSI(frame), "\n") {
		if strings.TrimRight(strings.TrimSpace(l), " ") == "┃ "+text {
			return true
		}
	}
	return false
}
