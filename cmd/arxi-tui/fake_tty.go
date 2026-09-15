package main

import (
	"strings"
	"sync"
	"time"

	"github.com/michiTrader/arxi_tui/internal/term"
)

// fakeTTY is a test double for Terminal. It plays back a scripted sequence of
// terminal events on its Events channel and records whatever the loop writes to
// it, so a test can assert on the rendered frames.
type fakeTTY struct {
	mu       sync.Mutex
	width    int
	height   int
	frames   []string
	events   chan term.Event
}

// newFakeTTY creates a fake terminal with the given dimensions and a planned
// event stream. The stream is drained by a goroutine that emits each event
// with the delay that follows it, mirroring how a real terminal interleaves
// bytes across time.
func newFakeTTY(width, height int, script []scheduledEvent) *fakeTTY {
	ft := &fakeTTY{
		width:  width,
		height: height,
		events: make(chan term.Event, 64),
	}
	go func() {
		for _, ev := range script {
			if ev.delay > 0 {
				time.Sleep(ev.delay)
			}
			ft.events <- ev.event
		}
		close(ft.events)
	}()
	return ft
}

// scheduledEvent pairs a terminal event with the delay that should precede it.
// A delay of zero means "emit immediately after the previous event."
type scheduledEvent struct {
	delay time.Duration
	event term.Event
}

// Write captures rendered output. The loop writes clear-screen + frame bytes;
// the test inspects the accumulated string.
func (f *fakeTTY) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.frames = append(f.frames, string(p))
	return len(p), nil
}

// Size returns the configured dimensions.
func (f *fakeTTY) Size() (int, int) { return f.width, f.height }

// Events returns the playback channel.
func (f *fakeTTY) Events() <-chan term.Event { return f.events }

// output returns everything the loop wrote, concatenated.
func (f *fakeTTY) output() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.frames, "")
}

// keyEvent builds a term.EventKey for a single rune with no modifiers.
func keyEvent(r rune) term.Event {
	return term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyRunes, Runes: []rune{r}}}
}

// ctrlCharEvent builds a term.EventKey for a control character byte.
// The decoder reports Ctrl-C as 'c' with ModCtrl, matching how a config file
// would spell it — see internal/term decode_test.go.
func ctrlCharEvent(r rune) term.Event {
	return term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyRunes, Runes: []rune{r}, Mod: term.ModCtrl}}
}
