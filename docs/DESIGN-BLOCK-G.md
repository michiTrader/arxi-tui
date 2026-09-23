# Block G — Phase 3 animation semantics (proposal, awaiting signature)

This document drafts the two paper decisions Block G still needs before any of
G1–G4 (`transition`, `scroll`, `reveal`, `enter`) can be implemented. It is the
same SCENES/TOKENS-on-paper method Block D used (`docs/DESIGN-BLOCK-D.md`): it
touches no code, it freezes vocabulary and behaviour *before* a renderer
depends on it, and it is written to be signed into the frozen docs by the owner
— not merged as fact.

**Status: awaiting signature.** Nothing here is signed until the owner accepts
it; on acceptance, G-A is signed into `docs/PLAN.md` (a new ADR for the host
clock) and `docs/TOKENS.md` (the animation-phase render contract), and G-B is
signed into `docs/SCENES.md` Scene 4 (the per-prop behaviour table). As with
Block D, signing the *design* lifts no code guard: the `scroll` refusal
(`internal/scene/node.go:72`, `validate.go`) and the warned-but-unimplemented
state of `transition`/`reveal`/`enter` are lifted by the *implementation* that
replaces them, each with its own counterfactual test (G1–G4).

## What D4/G0 already settled, and what it did not

D4 signed the **timing vocabulary** — how long, which curve, what tick rate —
and G0 implemented it (`internal/theme/anim.go`: `AnimDef{DurationMS, Curve,
FPS}`, the closed curve set, load-time validation). That is the *input* an
animation reads. It says nothing about two things this proposal supplies:

1. **The clock that turns a duration into motion.** The renderer is clockless
   by construction — `RenderFrame` is "no I/O, no clock" (`render.go:95-97`)
   and the loop repaints only on terminal events and driver events
   (`main.go:474-542`). An elapsed-time prop needs a time-driven repaint and
   per-node elapsed-time state, and neither exists. **G-A** designs it.
2. **What each prop does to the frame over its run.** D4 was explicit that it
   signed the timing, not the behaviour (`DESIGN-BLOCK-D.md:315`). The concrete
   render semantics of `transition`/`reveal`/`enter`/`scroll` are signed
   nowhere. Building them without signing them is inventing renderer behaviour,
   the objection that keeps `row_template` and `on_press` refused. **G-B**
   designs it.

Order of dependency: G-B's semantics are expressed in terms of G-A's phase
input, so G-A is the foundation. Both must be signed before G1. G1–G4 then
implement one prop each against the signed contract.

---

## G-A — the host animation clock

**Unblocks:** every one of G1–G4 (all four need elapsed time). **Scene:** 4.
**Questions:** Q8 (timing is a token; this is the clock that consumes it), Q9
(the fold never waits on an animation).

### The problem it solves

A timing token is a duration; a duration is meaningless without a clock to
measure it against. Today nothing in the running instance advances time: the
loop is a `select` over three channels (`ctx.Done()`, terminal events, driver
events, `main.go:474-542`) and each repaint is a pure fold-and-render. To make
a prop move, the host must (a) repaint on a timer, not only on input, and (b)
hold, per animated node, how long that node has been animating — state that
outlives a single frame. Both are host concerns that must stay off the far side
of the fold (Q9).

### The decision

Add a fourth `select` case to the loop: a **tick channel** driven by a
`time.Ticker`, alongside `ctx.Done()`, terminal events, and driver events. A
tick calls the same `repaint()` closure every other case calls (`main.go:409`).
The clock adds no new render path; it adds a new *reason to repaint*.

1. **The clock lives in the loop, not the renderer.** `RenderFrame` stays pure:
   "no I/O, no clock" is preserved verbatim. The loop owns wall time, reads it
   once per tick, and hands the renderer a computed **animation phase** — the
   render layer is still a pure function of `(document, fold state, phase)`.
   This is the single most load-bearing choice: it keeps every golden
   deterministic (a golden pins a chosen phase, exactly as it pins a fold
   state today), and it keeps the fold-and-render pipeline testable without a
   real clock.

2. **Per-node elapsed time is host-owned view state, held across frames like
   `uiHidden`.** The loop keeps an `animClock` local — a monotonic reference
   plus a `map[nodeID]startTime` — beside `input`, `slashSel`, and `uiHidden`
   (`main.go:396-407`). A node's elapsed time at repaint is `now - start[id]`.
   This map is view state for the same reason `uiHidden` is: the fold is
   rebuilt from the log each frame (ADR-0004, pull-by-frame) and would forget a
   per-node timer kept only in `State`, so it must survive the next repaint on
   the host side.

3. **The phase never enters `fold.State` (Q9, architectural boundary).** The
   fold projects *content*, not motion. The animation phase reaches the
   renderer through a **separate input**, not through `fold.State` — a
   `Renderer.phase` field set per repaint, the same shape `curRow` already
   uses for row scope (`render.go:102-111`), or an explicit `RenderFrame`
   parameter. Either way the fold's type gains no timing field, so "the fold
   never waits on an animation" stays true by construction rather than by
   discipline. This is the direct analogue of how `ui.hidden` is host view
   state consumed by the walk without the fold owning the policy.

4. **The ticker runs only while something is animating.** When no visible node
   declares an animation prop, there is no ticker and the loop is exactly as
   quiet as today (repaints on input only) — the "repaints only on events"
   property is preserved for the common case. The host arms the ticker when a
   repaint produces a frame containing an active animation and stops it when
   the last one ends. The tick rate is the **maximum `fps`** among the active
   timing tokens (a 30 fps reveal and a 20 fps marquee share one 30 fps ticker;
   each node still advances by its own elapsed time), so one timer serves the
   whole frame and a token's `fps` caps its own smoothness, not the loop's.

5. **A one-shot animation's clock starts when its node first appears; a
   continuous one runs while its node is visible.** `reveal`/`enter`/`transition`
   over a finite `duration_ms` are one-shot: the node's `start[id]` is set the
   first repaint the node is present (its `when` truthy and its id not in
   `ui.hidden`), the phase runs `0 → 1` over `duration_ms`, and past the end the
   node draws in its final, settled form with no further ticks it forces. A node
   that disappears and reappears animates again (its start is cleared when it
   leaves the frame) — a re-entering row is a new entrance, which is the only
   reading that does not require the host to remember forever that a node was
   once seen. `scroll` with `duration_ms == 0` (the marquee) has no end and
   ticks continuously while visible, halting when `pause_when` is truthy.

### Refusals / empty state

- **Empty is the default and changes no golden.** A document with no animation
  prop arms no ticker, feeds phase to nothing, and renders exactly as today.
  Signing G-A moves no existing fixture, the same no-op guarantee D3's empty
  `ui.hidden` set gave.
- **The escape hatch is untouched.** The ticker case sits beside the terminal
  case in the same `select`; Ctrl-C is handled on the terminal channel before
  any tick can matter (`main.go:485-494`), and a tick only repaints — it reads
  no input and cannot capture a gesture (invariant 6). A runaway animation
  cannot wedge the door.
- **An animation never contracts the input or banner.** The phase parameterises
  what a node draws *within the budget it already has*; it does not change the
  row-budget arithmetic (`renderNode`'s contract, `render.go:123-133`). The
  chat-pane-contracts-first invariant is a layout rule and animation is not a
  layout input — a revealing node occupies its settled height's budget from
  frame one, revealing *into* it, so a half-revealed node cannot steal a row
  from the input the way a growing one could.

### Text to sign (a new ADR in `docs/PLAN.md`, plus a note in `docs/TOKENS.md`)

`docs/PLAN.md` gains **ADR-0005 — the host animation clock**: the loop owns a
`time.Ticker`-driven fourth `select` case; per-node elapsed time is host view
state held across frames like `ui.hidden`; the animation phase reaches the
renderer as an input separate from `fold.State`, keeping `RenderFrame` pure and
every golden phase-pinned; the ticker runs only while a visible node animates,
at the max active `fps`; one-shot clocks start on appearance, continuous ones
run while visible. `docs/TOKENS.md`'s timing-token section gains a forward
pointer: *the clock that consumes these tokens is ADR-0005; a token is the
duration, the clock is the elapsed time measured against it.*
