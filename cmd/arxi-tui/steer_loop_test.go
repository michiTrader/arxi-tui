package main

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// fakeLiveDriver is a driver that may or may not take a line while an answer runs, and records
// what it was asked to do about one.
type fakeLiveDriver struct {
	testDriver
	mu       sync.Mutex
	canSteer bool
	steerErr error
	steered  []string
	stopped  int
}

func (d *fakeLiveDriver) CanSteer() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.canSteer
}

func (d *fakeLiveDriver) Steer(text string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.steerErr != nil {
		return d.steerErr
	}
	d.steered = append(d.steered, text)
	return nil
}

func (d *fakeLiveDriver) StopForSteer() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopped++
	return true
}

func (d *fakeLiveDriver) counts() (steered []string, stopped int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.steered...), d.stopped
}

func steerLine(s string) []scheduledEvent {
	return append([]scheduledEvent{{300 * time.Millisecond, keyEvent(rune(s[0]))}}, append(typed(s[1:]), altEnterAfter(20*time.Millisecond))...)
}

// With a core that takes a line while the answer runs, Alt+Enter writes it to the answer and
// does not stop anything.
// Consequence: stopping the turn throws away the reading and the tool results the model had
// gathered. Remedy: queue.steer uses the live path whenever the driver says it can.
func TestSteerKeyWritesToTheRunningAnswerWithoutStoppingIt(t *testing.T) {
	drv := &fakeLiveDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64), seq: 1000}, canSteer: true}
	queueLoop(t, drv, drv.evCh, append(steerLine("use tabs"), leave(1000*time.Millisecond)...), func(send func(fold.Event)) {
		time.Sleep(100 * time.Millisecond)
		send(working())
	})
	steered, stopped := drv.counts()
	if len(steered) != 1 || steered[0] != "use tabs" {
		t.Fatalf("steered = %q, want [use tabs].\nConsequence: the line never reaches the answer in flight.\nRemedy: queue.steer calls Steer when CanSteer is true.", steered)
	}
	if stopped != 0 {
		t.Fatalf("the answer was stopped %d time(s) although the line was written to it.\nConsequence: what the model had done is thrown away.\nRemedy: only stop when Steer cannot take the line.", stopped)
	}
	if len(drv.submitted) != 0 {
		t.Fatalf("the line was also sent as a new message: %q.\nConsequence: the model gets it twice.", drv.submitted)
	}
}

// With no live steering (an older core, or a turn without tools), the same key stops the
// answer and sends the line in its place.
// Consequence: the key would do nothing, and the line would sit in the input.
func TestSteerKeyFallsBackToStopAndResendWhenTheCoreCannotTakeALine(t *testing.T) {
	drv := &fakeLiveDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64), seq: 1000}}
	queueLoop(t, drv, drv.evCh, append(steerLine("use tabs"), leave(1000*time.Millisecond)...), func(send func(fold.Event)) {
		time.Sleep(100 * time.Millisecond)
		send(working())
	})
	steered, stopped := drv.counts()
	if len(steered) != 0 {
		t.Fatalf("a line was written to a turn that cannot take one: %q", steered)
	}
	if stopped != 1 {
		t.Fatalf("stopped %d times, want 1.\nConsequence: the correction waits for an answer nobody stops.\nRemedy: fall back to StopForSteer.", stopped)
	}
	if len(drv.submitted) != 1 || drv.submitted[0] != "use tabs" {
		t.Fatalf("submitted = %q, want [use tabs] sent in the stopped answer's place", drv.submitted)
	}
}

// A write that fails (the turn ended between the check and the write) is not lost: the line
// goes the stop-and-resend way.
// Consequence: the line would be dropped from the input and sent nowhere.
func TestSteerKeyWriteFailureFallsBack(t *testing.T) {
	drv := &fakeLiveDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64), seq: 1000}, canSteer: true, steerErr: errors.New("turn over")}
	queueLoop(t, drv, drv.evCh, append(steerLine("use tabs"), leave(1000*time.Millisecond)...), func(send func(fold.Event)) {
		time.Sleep(100 * time.Millisecond)
		send(working())
	})
	_, stopped := drv.counts()
	if stopped != 1 || len(drv.submitted) != 1 || drv.submitted[0] != "use tabs" {
		t.Fatalf("stopped=%d submitted=%q, want one stop and the line sent.\nConsequence: a line accepted by the input vanishes.", stopped, drv.submitted)
	}
}

// A line the core never read comes back as chat.steer_unsent and goes out after the answer.
// Consequence: the user typed something, saw it accepted, and nobody answered it.
func TestSteerUnsentLineIsSentWhenTheAnswerEnds(t *testing.T) {
	drv := newQueueDriver()
	queueLoop(t, drv, drv.evCh, leave(1500*time.Millisecond), func(send func(fold.Event)) {
		time.Sleep(100 * time.Millisecond)
		send(working())
		send(fold.Event{Type: "chat.steer_unsent", Seq: 11, Payload: map[string]any{"text": "too late"}})
		time.Sleep(300 * time.Millisecond)
		send(turnDone(20))
	})
	if len(drv.submitted) != 1 || drv.submitted[0] != "too late" {
		t.Fatalf("submitted = %q, want [too late].\nConsequence: a line written to an answer that ended first is answered by nobody.\nRemedy: chat.steer_unsent joins the queue.", drv.submitted)
	}
}
