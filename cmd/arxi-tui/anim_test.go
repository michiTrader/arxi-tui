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

// oneShotClock is a clock whose one-shot props resolve through a fixed timing
// token, so the phase tests below read a known duration and curve rather than
// depending on a theme. linear is chosen so phase == elapsed/duration and the
// expected values can be written by hand; the eased curves are theme.EvalCurve's
// contract, pinned in internal/theme.
func oneShotClock(fps, durationMS int) *animClock {
	c := newAnimClock(fps)
	c.resolveAnim = func(name string) (theme.AnimDef, bool) {
		return theme.AnimDef{DurationMS: durationMS, Curve: "linear", FPS: fps}, true
	}
	return c
}

// A one-shot reveal's phase runs 0→1 over its token's duration, measured by wall
// time. This is the reveal half of the clock: where a scroll reports a tick
// count, a one-shot reports the curve-eased fraction of its run, and the
// renderer turns that into a character prefix. Without it a reveal would either
// never move or jump straight to full.
func TestAnimClockOneShotPhaseRunsOverTheDuration(t *testing.T) {
	c := oneShotClock(30, 200) // a 200ms reveal
	base := time.Unix(0, 0)

	c.advance(base)
	c.reconcile([]engine.AnimActivity{{NodeID: "rv", OneShot: true, Token: "reveal.fast"}})

	// Halfway through the duration is half the (linear) phase.
	c.advance(base.Add(100 * time.Millisecond))
	if got := c.phases()["rv"]; got < 0.49 || got > 0.51 {
		t.Errorf("after 100ms of a 200ms reveal the phase is %v, want ~0.5\n"+
			"consequence: the typewriter does not track wall time against the token's duration, so\n"+
			"it reveals at the wrong rate or not at all", got)
	}
	// A continuous scroll would be in ticks; a one-shot must not be, or the
	// renderer would read a tick count where it expects a phase.
	if _, inTicks := c.phases()["rv"]; !inTicks {
		t.Errorf("a one-shot reveal is absent from phases(); the renderer reads AnimPhase for it")
	}

	// Past the duration it is settled at 1 and forces no more ticks.
	c.advance(base.Add(300 * time.Millisecond))
	if got := c.phases()["rv"]; got != 1 {
		t.Errorf("after 300ms of a 200ms reveal the phase is %v, want 1 (settled)", got)
	}
	if c.running() {
		t.Errorf("a settled one-shot reports running; the ticker would spin to repaint a frame that\n" +
			"no longer changes (ADR-0005: a settled one-shot is present but quiet, like a paused marquee)")
	}
}

// A one-shot reveal reports running only while it is mid-reveal, so the ticker
// is armed for the reveal and stopped the moment it settles — the on-demand
// property the whole clock exists to preserve.
func TestAnimClockOneShotRunsOnlyUntilSettled(t *testing.T) {
	c := oneShotClock(30, 200)
	base := time.Unix(0, 0)
	c.advance(base)
	c.reconcile([]engine.AnimActivity{{NodeID: "rv", OneShot: true, Token: "reveal.fast"}})

	c.advance(base.Add(50 * time.Millisecond))
	if !c.running() {
		t.Errorf("a reveal 50ms into a 200ms run reports not running; it is still revealing, so the\n" +
			"ticker must stay armed")
	}
}

// A one-shot that leaves the frame loses its clock and re-animates on re-entry,
// the animate-on-each-appearance fork (ADR-0005) — the same replay contract the
// continuous case has, checked on the one-shot path because it tracks extra
// per-node timing that must be cleared alongside the elapsed time.
func TestAnimClockOneShotReplaysOnReentry(t *testing.T) {
	c := oneShotClock(30, 200)
	base := time.Unix(0, 0)
	c.advance(base)
	c.reconcile([]engine.AnimActivity{{NodeID: "rv", OneShot: true, Token: "reveal.fast"}})
	c.advance(base.Add(100 * time.Millisecond))
	if c.phases()["rv"] == 0 {
		t.Fatalf("premise broken: the reveal should have advanced before it leaves")
	}

	c.reconcile(nil) // it leaves the frame
	if _, ok := c.phases()["rv"]; ok {
		t.Errorf("a departed one-shot kept its phase; it must be cleared so re-entry re-animates")
	}
	if c.running() {
		t.Errorf("no active nodes, yet the clock reports running")
	}

	c.reconcile([]engine.AnimActivity{{NodeID: "rv", OneShot: true, Token: "reveal.fast"}})
	if got := c.phases()["rv"]; got != 0 {
		t.Errorf("a re-entering reveal resumed at phase %v instead of restarting at 0", got)
	}
}

// enter's rows are staggered by their offset: row i begins at i * durMS, so at
// any wall time a prefix of the rows have started and the rest are absent from
// phases() — the growing row count, spread over time by the clock rather than
// by the renderer (G4). A row still before its offset must be absent, not phase
// 0: the renderer tells "not drawn yet" from "dim start" by whether the row has
// an entry, so delivering 0 would draw a row that should not be on screen.
func TestAnimClockStaggersEnterRowsByOffset(t *testing.T) {
	c := oneShotClock(30, 200) // each row ramps over 200ms; rows start 200ms apart
	base := time.Unix(0, 0)
	c.advance(base)

	rows := []engine.AnimActivity{
		{NodeID: "row0", OneShot: true, Token: "default", Row: 0},
		{NodeID: "row1", OneShot: true, Token: "default", Row: 1},
		{NodeID: "row2", OneShot: true, Token: "default", Row: 2},
	}
	c.reconcile(rows)

	// 100ms in: only row 0 has started (halfway); rows 1 and 2 are before their
	// offsets and must be absent.
	c.advance(base.Add(100 * time.Millisecond))
	p := c.phases()
	if got := p["row0"]; got < 0.49 || got > 0.51 {
		t.Errorf("at 100ms row 0's phase is %v, want ~0.5; row 0 has no offset and should ramp\n"+
			"straight away", got)
	}
	if _, ok := p["row1"]; ok {
		t.Errorf("at 100ms row 1 is present (phase %v); it does not start until its offset of 200ms,\n"+
			"so it must be absent — the renderer draws an absent row as not-yet-arrived, which is the\n"+
			"growing row count", p["row1"])
	}
	if _, ok := p["row2"]; ok {
		t.Errorf("at 100ms row 2 is present; it does not start until 400ms")
	}
	c.reconcile(rows)
	if !c.running() {
		t.Errorf("at 100ms the ticker reports stopped, but rows 1 and 2 have not even begun; a\n" +
			"staggered list would freeze with only its first row shown")
	}

	// 250ms in: row 0 settled, row 1 just started (50ms into its own 200ms ramp),
	// row 2 still absent.
	c.advance(base.Add(250 * time.Millisecond))
	p = c.phases()
	if got := p["row0"]; got != 1 {
		t.Errorf("at 250ms row 0's phase is %v, want 1 (settled)", got)
	}
	if got := p["row1"]; got < 0.24 || got > 0.26 {
		t.Errorf("at 250ms row 1's phase is %v, want ~0.25 (50ms into its 200ms ramp, offset 200ms)", got)
	}
	if _, ok := p["row2"]; ok {
		t.Errorf("at 250ms row 2 is present; its offset is 400ms")
	}
	c.reconcile(rows)
	if !c.running() {
		t.Errorf("at 250ms row 0 has settled but row 1 is mid-ramp and row 2 has not started, yet the\n" +
			"ticker reports stopped; running() must count a row's offset toward its settle time, or a\n" +
			"list stops filling the moment its first row finishes")
	}

	// 650ms in: every row has run past its own end (last settles at 3*200=600ms),
	// so nothing is left to tick.
	c.advance(base.Add(650 * time.Millisecond))
	p = c.phases()
	for _, id := range []string{"row0", "row1", "row2"} {
		if got := p[id]; got != 1 {
			t.Errorf("at 650ms %s's phase is %v, want 1 (all rows settled)", id, got)
		}
	}
	c.reconcile(rows)
	if c.running() {
		t.Errorf("at 650ms every staggered row has settled (the last at 600ms), yet the ticker\n" +
			"reports running; it would spin repainting a frame that no longer changes")
	}
}
