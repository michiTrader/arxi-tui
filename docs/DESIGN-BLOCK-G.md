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
