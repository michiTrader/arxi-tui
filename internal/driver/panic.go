package driver

import (
	"time"
)

// PanicGesture is the immovable escape hatch: Ctrl-C twice in quick succession
// (within armTimeout) restores the raw scene no matter what a scene or plugin
// may have done. It is host-owned and never delegable, because the scene is
// untrusted content and must never capture the only way out.
//
// This is the direct port of arxi-sim's interrupt: 1.5s is long enough that a
// deliberate double tap always lands and short enough that a ctrl+c five
// seconds later does not leave.
type PanicGesture struct {
	armed   bool
	armedAt time.Time
}

const armTimeout = 1500 * time.Millisecond

// HandleCtrlC records a Ctrl-C press. Returns true if this press should trigger
// the panic gesture (i.e. the second press arrived within armTimeout).
func (g *PanicGesture) HandleCtrlC(now time.Time) bool {
	if g.armed && now.Sub(g.armedAt) <= armTimeout {
		// Second press within window — trigger escape
		g.reset()
		return true
	}
	// First press, or too late — arm for the next window
	g.armed = true
	g.armedAt = now
	return false
}

// Armed reports whether the gesture is currently armed, so a scene may display it.
// This is the host.escape.armed bind: a scene may SHOW it, no scene may CAPTURE it.
func (g *PanicGesture) Armed() bool {
	return g.armed
}

// Reset clears the arm state. Called after a timeout or a trigger.
func (g *PanicGesture) Reset() {
	g.reset()
}

func (g *PanicGesture) reset() {
	g.armed = false
	g.armedAt = time.Time{}
}
