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

	// oneShot, durMS, curve and nodeFPS are the per-node timing of the one-shot
	// props (G3 reveal, later transition/enter), populated from the activity
	// report at reconcile. A continuous scroll ticks forever at the clock's fps
	// and reads none of these; a one-shot runs a phase 0→1 once over durMS,
	// eased by curve, and stops forcing ticks once settled. The renderer reports
	// only the node's token name (it has no theme); resolveAnim turns that name
	// into a duration/curve/fps here.
	oneShot map[string]bool
	durMS   map[string]int
	curve   map[string]string
	nodeFPS map[string]int

	// rowOffset is enter's per-row stagger index (G4): row i of a staggered
	// container begins its own entrance at offset i * durMS, so its phase is
	// elapsed/durMS - i. It is 0 for every other one-shot — a lone reveal or
	// transition is row 0, which starts at zero — so the offset arithmetic is a
	// no-op for them and the shared clock stays one clock per reported id. A row
	// whose offset has not yet elapsed is absent from phases() entirely, which the
	// renderer reads as "not drawn yet": the row-count axis, spread over time here
	// where the clock owns time.
	rowOffset map[string]int

	// resolveAnim maps a timing-token name to its definition, set by the loop to
	// the active theme's lookup. It is nil in unit tests that exercise only the
	// continuous scroll path, which never resolves a token; a one-shot node with
	// no resolver (or an absent token) is treated as zero-duration, i.e. settled
	// at once, the graceful fallback that matches marqueeFPS declining to the
	// host default rather than refusing.
	resolveAnim func(string) (theme.AnimDef, bool)

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
		oneShot: map[string]bool{},
		durMS:   map[string]int{},
		curve:   map[string]string{},
		nodeFPS: map[string]int{},

		rowOffset: map[string]int{},
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

// phases is the one-shot phase fed to the renderer this frame: the curve-eased
// fraction each one-shot node has run through its token's duration (ADR-0005 /
// G-B). A continuous scroll is absent from this map — it reads ticks, not phase.
// The curve is applied here, in the loop, so the renderer receives a phase in
// [0,1] already eased and turns it straight into a frame (renderText clips to a
// phase-wide prefix). A zero-duration one-shot (an unresolved or missing token)
// is settled at 1 rather than dividing by zero: a one-shot with no run to
// measure has nothing to animate through.
//
// A staggered enter row (rowOffset > 0) subtracts its offset in duration units
// before easing: row i's fraction is elapsed/durMS - i, so it sits below zero
// until i * durMS has elapsed. A row still below zero is left out of the map
// entirely — the renderer reads an absent row in a non-nil map as "not yet
// drawn", which is the growing row count. A lone reveal or transition has
// rowOffset 0, so this subtracts nothing and their phase is unchanged.
func (c *animClock) phases() map[string]float64 {
	out := make(map[string]float64, len(c.oneShot))
	for id := range c.oneShot {
		d := c.durMS[id]
		if d <= 0 {
			out[id] = 1
			continue
		}
		t := float64(c.elapsed[id].Milliseconds())/float64(d) - float64(c.rowOffset[id])
		if t < 0 {
			continue // before this row's stagger offset: absent, drawn not-yet
		}
		out[id] = theme.EvalCurve(c.curve[id], t)
	}
	return out
}

// reconcile takes the frame's activity report and updates the tracked sets: a
// node that just appeared gets a clock started at zero, a node that left the
// frame has its clock cleared (re-entry re-animates), and the paused set is
// refreshed so the next advance freezes exactly the nodes pause_when holds.
//
// A one-shot node also has its timing resolved here, once, from the token name
// the report carries: the loop has the theme, the renderer does not, so this is
// the seam where a token becomes a duration/curve/fps. Resolving at reconcile
// rather than at phases() keeps the per-frame phase read a pure arithmetic step.
func (c *animClock) reconcile(active []engine.AnimActivity) {
	next := make(map[string]bool, len(active))
	nextPaused := make(map[string]bool, len(active))
	nextOneShot := make(map[string]bool)
	for _, a := range active {
		next[a.NodeID] = true
		nextPaused[a.NodeID] = a.Paused
		if _, ok := c.elapsed[a.NodeID]; !ok {
			c.elapsed[a.NodeID] = 0
		}
		if a.OneShot {
			nextOneShot[a.NodeID] = true
			d, curveName, fps := c.resolveOneShot(a.Token)
			c.durMS[a.NodeID] = d
			c.curve[a.NodeID] = curveName
			c.nodeFPS[a.NodeID] = fps
			c.rowOffset[a.NodeID] = a.Row
		}
	}
	for id := range c.elapsed {
		if !next[id] {
			delete(c.elapsed, id)
			delete(c.durMS, id)
			delete(c.curve, id)
			delete(c.nodeFPS, id)
			delete(c.rowOffset, id)
		}
	}
	c.active = next
	c.paused = nextPaused
	c.oneShot = nextOneShot
}

// resolveOneShot turns a one-shot node's token name into a duration, curve and
// tick rate. An empty token means anim.default (Q8). A missing resolver or a
// token the theme does not define falls back to a zero duration — settled at
// once — and the host default rate, the same graceful decline marqueeFPS makes;
// the load-time refusal (ValidateTokens against the theme) is where an undefined
// token is actually rejected, not here on the clock's hot path.
func (c *animClock) resolveOneShot(token string) (durMS int, curve string, fps int) {
	fps = hostDefaultFPS
	if token == "" {
		token = "default"
	}
	if c.resolveAnim != nil {
		if def, ok := c.resolveAnim(token); ok {
			durMS = def.DurationMS
			curve = def.Curve
			if def.FPS > 0 {
				fps = def.FPS
			}
		}
	}
	return durMS, curve, fps
}

// running reports whether any active node is unpaused and still moving, i.e.
// whether the ticker should be armed. A paused node, and a one-shot node that
// has settled (run past its duration), both ask for no ticks while still being
// present — there is nothing left for a tick to move (ADR-0005: the ticker runs
// only while a visible node animates).
//
// A staggered enter row settles at (rowOffset+1) * durMS rather than at durMS:
// row i does not begin until i * durMS, so the ticker must keep running until
// the last row has both started and finished, or a list would freeze halfway
// down with its final rows never scheduled. A row with offset 0 settles at durMS,
// unchanged for reveal and transition.
func (c *animClock) running() bool {
	for id := range c.active {
		if c.paused[id] {
			continue
		}
		if c.oneShot[id] {
			if d := int64(c.durMS[id]); d > 0 {
				settleAt := int64(c.rowOffset[id]+1) * d
				if c.elapsed[id].Milliseconds() < settleAt {
					return true
				}
			}
			continue
		}
		return true
	}
	return false
}

// interval is the ticker period at the fastest active rate: the clock's base
// fps (the marquee's) raised to any faster one-shot token's fps, so a 30fps
// reveal and a 20fps marquee share one 30fps ticker and each still advances by
// its own elapsed time (ADR-0005). One timer serves the whole frame; a token's
// fps caps its own smoothness, not the loop's.
func (c *animClock) interval() time.Duration {
	fps := c.fps
	for id := range c.active {
		if c.oneShot[id] && c.nodeFPS[id] > fps {
			fps = c.nodeFPS[id]
		}
	}
	if fps <= 0 {
		return time.Second / hostDefaultFPS
	}
	return time.Second / time.Duration(fps)
}
