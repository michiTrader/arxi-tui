package driver

import (
	"time"
)

// PanicGesture is the immovable escape hatch: Ctrl-C twice in quick succession
// (within armTimeout) leaves the program no matter what a scene or plugin may
// have done. It is host-owned and never delegable, because the scene is
// untrusted content and must never capture the only way out.
//
// The window is 4s because the first press is not silent: it arms a visible
// "press ctrl+c again to exit" hint (host.escape.armed), and the window has to
// be long enough for a reader to see that hint and decide, not just long enough
// for a reflexive double tap. It is still short of the ~5s where a stray Ctrl-C
// pressed to clear the line, then a second one much later, would leave against
// the reader's intent — the arm expires and the hint disappears well before
// then.
type PanicGesture struct {
	armed   bool
	armedAt time.Time
}

const armTimeout = 4 * time.Second

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
