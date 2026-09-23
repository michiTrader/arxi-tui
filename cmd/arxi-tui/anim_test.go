package main

import (
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// marqueeFPS takes the anim.marquee token's fps when the theme sets one, and
// the host default otherwise (D4: AnimDef.FPS == 0 means unset). The default
// case is what a scene running under the factory theme gets, so it is the one
// that must not silently become zero — a zero-fps ticker is a divide the
// interval() guard exists to avoid.
func TestMarqueeFPSFallsBackToHostDefault(t *testing.T) {
	if got := marqueeFPS(nil); got != hostDefaultFPS {
		t.Errorf("marqueeFPS(nil) = %d, want the host default %d; a theme that names no marquee\n"+
			"token must not leave the clock at zero fps", got, hostDefaultFPS)
	}
	// SOBRIA is the factory theme; whatever it declares (or does not) for the
	// marquee token, the result must be a positive rate the ticker can use.
	if got := marqueeFPS(theme.SOBRIA()); got <= 0 {
		t.Errorf("marqueeFPS(SOBRIA) = %d; the tick rate must be positive or interval() divides by\n"+
			"zero", got)
	}
}

// The clock's phase is a function of wall time: elapsed seconds times fps,
// floored to a tick count. This pins that a node advances exactly as far as the
// time that has passed while it was active and unpaused, and no further.
func TestAnimClockAdvancesActiveNodesByWallTime(t *testing.T) {
	c := newAnimClock(10) // 10 fps: one tick per 100ms
	base := time.Unix(0, 0)

	// First advance only seeds the reference; nothing is active yet.
	c.advance(base)
	c.reconcile([]engine.AnimActivity{{NodeID: "mq"}})

	// 250ms later, at 10fps, is 2 full ticks.
	c.advance(base.Add(250 * time.Millisecond))
	if got := c.ticks()["mq"]; got != 2 {
		t.Errorf("after 250ms at 10fps the marquee is at tick %d, want 2\n"+
			"consequence: the phase is not elapsed*fps, so the marquee moves at the wrong rate or\n"+
			"not at all", got)
	}

	// Another 350ms (600ms total) is 6 ticks.
	c.advance(base.Add(600 * time.Millisecond))
	if got := c.ticks()["mq"]; got != 6 {
		t.Errorf("after 600ms total the marquee is at tick %d, want 6", got)
	}
}

// A paused node's clock holds still: advance skips it, so its tick count does
// not move while pause_when is truthy. This is the loop half of the pause
// contract — the renderer reports paused, the clock freezes the phase.
func TestAnimClockFreezesPausedNodes(t *testing.T) {
	c := newAnimClock(10)
	base := time.Unix(0, 0)
	c.advance(base)
	c.reconcile([]engine.AnimActivity{{NodeID: "mq", Paused: true}})

	c.advance(base.Add(500 * time.Millisecond))
	if got := c.ticks()["mq"]; got != 0 {
		t.Errorf("a paused marquee advanced to tick %d over 500ms; pause_when must freeze the\n"+
			"offset, so the tick count may not move while it holds", got)
	}
	if c.running() {
		t.Errorf("a clock whose only node is paused reports running; the ticker would spin to\n" +
			"repaint a frame that never changes (ADR-0005)")
	}

	// Unpausing resumes from where it froze, not from zero.
	c.reconcile([]engine.AnimActivity{{NodeID: "mq", Paused: false}})
	c.advance(base.Add(700 * time.Millisecond))
	if got := c.ticks()["mq"]; got != 2 {
		t.Errorf("after unpausing and 200ms more the marquee is at tick %d, want 2 (it resumes from\n"+
			"the frozen elapsed, not from zero)", got)
	}
	if !c.running() {
		t.Errorf("an unpaused active node must report running so the ticker arms")
	}
}

// A node that leaves the frame loses its clock, and a node that re-enters starts
// over — the animate-on-each-appearance fork ADR-0005 signed. Without the clear,
// the host would have to remember forever that a node was once seen.
func TestAnimClockClearsDepartedNodesAndReplaysOnReentry(t *testing.T) {
	c := newAnimClock(10)
	base := time.Unix(0, 0)
	c.advance(base)
	c.reconcile([]engine.AnimActivity{{NodeID: "mq"}})
	c.advance(base.Add(500 * time.Millisecond))
	if c.ticks()["mq"] == 0 {
		t.Fatalf("premise broken: the node should have advanced before it leaves")
	}

	// It leaves the frame: no longer in the activity report.
	c.reconcile(nil)
	if _, ok := c.ticks()["mq"]; ok {
		t.Errorf("a node absent from the activity report kept its clock; a departed node must be\n" +
			"cleared so re-entry re-animates (ADR-0005)")
	}
	if c.running() {
		t.Errorf("no active nodes, yet the clock reports running")
	}

	// It re-enters: the clock starts from zero, not from where it left off.
	c.reconcile([]engine.AnimActivity{{NodeID: "mq"}})
	if got := c.ticks()["mq"]; got != 0 {
		t.Errorf("a re-entering node resumed at tick %d instead of restarting at 0", got)
	}
}
