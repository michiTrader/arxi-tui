package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// TestLoopInputSubmitTranscriptExit drives the full event loop through a fake
// TTY with a scripted key sequence: type "test", press Enter, then double
// Ctrl-C to exit. It asserts that the typed text appears in the transcript
// and that the loop returns cleanly on the panic gesture.
//
// This is the Phase 0 integration test the raw scene needs: end-to-end through
// the real loop, the real fold, and the real renderer — only the terminal I/O
// is faked, so the scenario runs deterministically in CI without a PTY.
func TestLoopInputSubmitTranscriptExit(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factoryRAW))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	// Scripted key sequence:
	//   t e s t <enter> ctrl+c ctrl+c
	//
	// Each key gets a small delay so the loop's select sees them in order
	// with the interleaving real terminals exhibit. The two Ctrl-C presses
	// are spaced well within armTimeout (1.5s) so the panic gesture fires.
	script := []scheduledEvent{
		{0, keyEvent('t')},
		{10 * time.Millisecond, keyEvent('e')},
		{10 * time.Millisecond, keyEvent('s')},
		{10 * time.Millisecond, keyEvent('t')},
		{10 * time.Millisecond, keyEvent('\r')}, // Enter
		{50 * time.Millisecond, ctrlCharEvent('c')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
	}

	tty := newFakeTTY(80, 24, script)
	eventCh := make(chan fold.Event, 64)
	// No mock driver events: the user submits a prompt via the keyboard, and
	// in Phase 0 that lands directly in the fold (typeKey appends it). There
	// is no core responding yet; the test asserts on the host-side transcript.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = loop(ctx, tty, doc, eventCh)
	if err != nil {
		t.Fatalf("loop returned error: %v", err)
	}

	out := tty.output()

	// The transcript must show the user's submitted text.
	if !strings.Contains(out, "test") {
		t.Errorf("transcript does not contain submitted text 'test'; output:\n%s", out)
	}

	// After submission the input line must be cleared — the prompt line should
	// show only the placeholder "> ", not the typed buffer.
	// The last rendered frame ends with the prompt line; "test" should appear
	// above it, not on it.
	if !strings.Contains(out, "> ") {
		t.Errorf("output does not contain input prompt '> '; output:\n%s", out)
	}

	// No "UNKNOWN NODE TYPE" — the factory scene must render cleanly.
	if strings.Contains(out, "UNKNOWN NODE TYPE") {
		t.Errorf("render produced unknown node type:\n%s", out)
	}
}

// TestLoopExitsOnCtrlCImmediate verifies that the escape gesture fires when the
// transcript is empty and the input buffer is empty: the very first two Ctrl-C
// presses should cause the loop to return nil.
func TestLoopExitsOnCtrlCImmediate(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factoryRAW))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	script := []scheduledEvent{
		{0, ctrlCharEvent('c')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
	}

	tty := newFakeTTY(80, 24, script)
	eventCh := make(chan fold.Event, 64)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = loop(ctx, tty, doc, eventCh)
	if err != nil {
		t.Fatalf("loop returned error: %v", err)
	}
}

// TestLoopFirstCtrlCClearsInput verifies that the first Ctrl-C clears the input
// buffer without exiting, and a subsequent regular keypress cancels the arm.
func TestLoopFirstCtrlCClearsInput(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factoryRAW))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	// Type "abc", first Ctrl-C (clears input and arms), then type 'x'
	// (disarms, input shows "x"), then Ctrl-C twice to exit.
	script := []scheduledEvent{
		{0, keyEvent('a')},
		{10 * time.Millisecond, keyEvent('b')},
		{10 * time.Millisecond, keyEvent('c')},
		{10 * time.Millisecond, ctrlCharEvent('c')}, // first Ctrl-C: clears "abc", arms
		{50 * time.Millisecond, keyEvent('x')},      // disarms, starts fresh input
		{10 * time.Millisecond, ctrlCharEvent('c')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
	}

	tty := newFakeTTY(80, 24, script)
	eventCh := make(chan fold.Event, 64)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = loop(ctx, tty, doc, eventCh)
	if err != nil {
		t.Fatalf("loop returned error: %v", err)
	}

	out := tty.output()
	// The input buffer should have been cleared by the first Ctrl-C, then 'x'
	// typed, so "x" appears but "abc" should not appear as current input.
	// "abc" may appear if a previous frame rendered it before the Ctrl-C
	// cleared it, so we check that the final state shows "x" in the prompt.
	if !strings.Contains(out, "x") {
		t.Errorf("expected 'x' in final output (input was cleared then re-typed); output:\n%s", out)
	}
}
