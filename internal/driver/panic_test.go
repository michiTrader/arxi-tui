package driver

import (
	"testing"
	"time"
)

// TestPanicGestureDoubleCtrlC verifies the escape hatch triggers on two
// presses within armTimeout. This is the immovable invariant — the raw scene
// must be recoverable even from a hostile scene.
func TestPanicGestureDoubleCtrlC(t *testing.T) {
	g := &PanicGesture{}
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// First Ctrl-C arms the gesture; should not trigger escape.
	if g.HandleCtrlC(now) {
		t.Error("first Ctrl-C should not trigger escape")
	}
	if !g.Armed() {
		t.Error("first Ctrl-C should arm the gesture")
	}

	// Second Ctrl-C within 1.5s triggers escape.
	if !g.HandleCtrlC(now.Add(500 * time.Millisecond)) {
		t.Error("second Ctrl-C within armTimeout should trigger escape")
	}
	if g.Armed() {
		t.Error("gesture should be disarmed after triggering")
	}
}

// TestPanicGestureTimeout verifies a delayed second Ctrl-C does not trigger.
func TestPanicGestureTimeout(t *testing.T) {
	g := &PanicGesture{}
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	g.HandleCtrlC(now)

	// More than 1.5s later — should arm, not trigger.
	if g.HandleCtrlC(now.Add(2 * time.Second)) {
		t.Error("Ctrl-C after armTimeout should not trigger escape")
	}
}

// TestPanicGestureAnyKeyClears verifies that any keypress between the two
// Ctrl-C presses cancels the arm. This follows arxi-sim's behavior: a reader
// who pressed Ctrl-C to clear the line and then kept typing is writing, not
// asking to leave.
func TestPanicGestureAnyKeyClears(t *testing.T) {
	g := &PanicGesture{}
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	g.HandleCtrlC(now)
	if !g.Armed() {
		t.Fatal("should be armed")
	}

	// Simulate a regular keypress cancelling the arm
	g.Reset()
	if g.Armed() {
		t.Error("keypress should cancel the arm")
	}
}
