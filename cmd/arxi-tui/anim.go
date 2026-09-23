package main

import (
	"time"

	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// hostDefaultFPS is the tick rate a scroll takes when its timing token names no
// fps (AnimDef.FPS == 0 means "unset", per D4). It is a host constant, not a
// theme value, because it is the fallback the theme deliberately declined to
// set — twelve frames a second is smooth enough for a marquee and cheap enough
// that an idle-but-scrolling pane is not a busy loop.
const hostDefaultFPS = 12

// marqueeFPS is the tick rate the scroll prop's clock runs at. Scroll takes its
// clock from the anim.marquee token by default (D4), so the rate is that
// token's fps when the theme sets one and the host default otherwise. It is
// read once at loop start rather than per frame, because a theme does not
// change mid-session and re-reading it every repaint would invite the tick rate
// to depend on render state.
func marqueeFPS(thm *theme.Theme) int {
	if thm != nil {
		if def, ok := thm.Anim("marquee"); ok && def.FPS > 0 {
			return def.FPS
		}
	}
	return hostDefaultFPS
}

// animClock is the host's animation clock (ADR-0005): per-node elapsed time
// held across frames, exactly as ui.hidden is host view state held across
// frames. The fold is rebuilt from the log each frame (ADR-0004, pull by
// frame) and would forget a per-node timer kept in State, so it lives here on
// the loop side and reaches the renderer only as a computed phase — never
// through fold.State, so "the fold never waits on an animation" (Q9) holds by
// construction.
//
// The phase the renderer consumes is a tick count: elapsed seconds times the
// node's fps. The renderer turns that into a horizontal offset (a scroll), so
// the clock owns time and the renderer owns pixels, and a golden pins a chosen
// tick count exactly as it pins a fold state.
type animClock struct {
	fps int

	// elapsed is accumulated, non-paused elapsed time per animating node. A
	// paused node's entry holds still; a node that leaves the frame loses its
	// entry, so re-entry re-animates from zero (the signed replay fork).
	elapsed map[string]time.Duration
	// active and paused are last frame's activity report, kept so advance can
	// move only the nodes that are still on screen and not frozen.
	active map[string]bool
	paused map[string]bool
	// last is the wall time of the previous advance, for the between-frame
	// delta. Zero until the first advance, which seeds it without advancing.
	last time.Time
}

func newAnimClock(fps int) *animClock {
	return &animClock{
		fps:     fps,
		elapsed: map[string]time.Duration{},
		active:  map[string]bool{},
		paused:  map[string]bool{},
	}
}

// advance moves each still-active, unpaused node's clock forward by the wall
// time since the last advance. It is called at the top of every repaint, so
// elapsed tracks real time whether the repaint was driven by a tick, a
// keystroke, or a driver event — the phase is a function of wall time, not of
// how many times the loop happened to wake.
func (c *animClock) advance(now time.Time) {
	if c.last.IsZero() {
		c.last = now
		return
	}
	delta := now.Sub(c.last)
	c.last = now
	for id := range c.active {
		if !c.paused[id] {
			c.elapsed[id] += delta
		}
	}
}

// ticks is the phase fed to the renderer this frame: the tick count each node
// has reached at its own fps. A node the clock has never seen is absent, which
// the renderer reads as zero — the starting frame.
func (c *animClock) ticks() map[string]int {
	out := make(map[string]int, len(c.elapsed))
	for id, e := range c.elapsed {
		out[id] = int(e.Seconds() * float64(c.fps))
	}
	return out
}

// reconcile takes the frame's activity report and updates the tracked sets: a
// node that just appeared gets a clock started at zero, a node that left the
// frame has its clock cleared (re-entry re-animates), and the paused set is
// refreshed so the next advance freezes exactly the nodes pause_when holds.
func (c *animClock) reconcile(active []engine.AnimActivity) {
	next := make(map[string]bool, len(active))
	nextPaused := make(map[string]bool, len(active))
	for _, a := range active {
		next[a.NodeID] = true
		nextPaused[a.NodeID] = a.Paused
		if _, ok := c.elapsed[a.NodeID]; !ok {
			c.elapsed[a.NodeID] = 0
		}
	}
	for id := range c.elapsed {
		if !next[id] {
			delete(c.elapsed, id)
		}
	}
	c.active = next
	c.paused = nextPaused
}

// running reports whether any active node is unpaused, i.e. whether the ticker
// should be armed. A frame whose only animations are paused arms nothing — the
// offset is frozen, so there is nothing for a tick to move (ADR-0005: the
// ticker runs only while a visible node animates).
func (c *animClock) running() bool {
	for id := range c.active {
		if !c.paused[id] {
			return true
		}
	}
	return false
}

// interval is the ticker period at the clock's fps.
func (c *animClock) interval() time.Duration {
	if c.fps <= 0 {
		return time.Second / hostDefaultFPS
	}
	return time.Second / time.Duration(c.fps)
}
