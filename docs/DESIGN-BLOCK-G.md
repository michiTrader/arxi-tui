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

---

## G-B — the per-prop render semantics

**Unblocks:** G1 (`transition`), G2 (`scroll`), G3 (`reveal`), G4 (`enter`).
**Scene:** 4. **Questions:** Q8 (per-node timing override), Q9 (host announces
"row new", the scene owns the animation).

### The problem it solves, and the principle that keeps it invention-free

The objection that keeps these props refused is that implementing them invents
renderer behaviour. G-B answers it with one constraint that makes the whole
block invention-free rather than merely argued:

> **An animation never draws anything the renderer cannot already draw at a
> fixed phase. The clock only chooses *which* already-expressible frame to emit
> at time t.**

The terminal frame the renderer produces today already varies along exactly
four axes an animation can ride, and every axis is something a static document
can already produce:

- **character count** — a `text` node clips to a width and shows a prefix of its
  content (`truncateText`, `render.go:86-93`). A phase that shortens the shown
  prefix is a typewriter reveal; the renderer already draws truncated text.
- **row count / height** — a container lays children within a row budget and
  draws a prefix of them (`renderNode`'s budget contract). A phase that grows
  the number of rows drawn is rows entering one at a time; the renderer already
  draws a subset of children.
- **SGR intensity** — the shipped relative dim/bright mechanism (no OSC 11; the
  terminal resolves the attributes, `theme.go:148`, `render.go` focus glow). A
  phase that ramps a node from dim to full is a fade-in expressed in the one
  intensity vocabulary the project already ships.
- **horizontal offset** — a `text` node can show any window of its content, not
  only the head. A phase that advances the window start is a marquee; the
  renderer already draws a clipped substring.

So the four props below are **not** four new rendering capabilities. Each is a
rule mapping `phase ∈ [0,1]` (after the token's curve is applied) to one of
these four existing axes. The curve set is closed for exactly this reason (D4):
the easing is code, but the axis it drives is already in the binary. Adding a
prop that needs a *fifth* axis — true opacity, sub-cell motion — is a
mother-binary change with its own freeze, the same bar as adding a node type or
a curve. This proposal deliberately signs only props expressible on the four
existing axes; anything else is out of scope and named so below.

### G1 — `transition`

**Shape:** `"transition": { "anim": "<token>" }` (token optional; `anim.default`
otherwise, Q8). **Axis:** SGR intensity. **Trigger:** node appearance.

When a node with `transition` first appears (its `when` truthy and id not in
`ui.hidden`), its content ramps from the theme's dim intensity to its settled
style over the token's `duration_ms`, eased by the curve. `curve(elapsed /
duration_ms)` yields `phase ∈ [0,1]`; `0` draws the node dim, `1` draws it in
its ordinary style, and the intensity is the dim/bright SGR the project already
ships — no new colour, no opacity. Past `duration_ms` the node is simply in its
ordinary style forever and forces no more ticks.

**The trigger is appearance, not content-change, and this is a signed scope
limit, not an oversight.** A cross-fade *between an old and a new bound value*
would require the host to remember each node's previous rendered content across
frames — a second per-node memory beyond the start-time map, and one that
interacts with the fold's content in a way Q9 wants kept simple. Appearance is
the trigger every other prop here already uses, needs only the start-time map
G-A signs, and covers the Scene 4 intent ("a row transitions in"). A
value-change transition is a later, separately-signed prop if it is ever wanted;
naming that boundary now stops it being invented inside G1.

### G2 — `scroll` (the marquee)

**Shape:** `"scroll": { "speed": <cells-per-tick>, "pause_when": "<bind>" }`,
clock from `anim.marquee` by default (D4 fixed this). **Axis:** horizontal
offset. **Trigger:** continuous while visible (`duration_ms == 0`).

When a `text` node's content is wider than its budget, `scroll` advances the
shown window instead of statically truncating. At tick *n* the window starts at
`offset = (n * speed) mod (content_width + gap)`, wrapping so the tail is
followed by the head after a fixed gap — the classic marquee, and exactly the
`ansi`-width-aware windowing `truncateText` already does, only with a moving
start rather than a fixed one. `speed` is cells per tick (an int; the *rate* is
the token's `fps`, so cadence lives in one place per D4). `pause_when` names a
bind (BINDS.md §4.6 truthiness): while truthy the offset holds, so a scene can
freeze the marquee when the pane is unfocused or the agent is idle without the
renderer inventing a pause policy.

`scroll` is already a declared-but-refused field (`node.go:72`,
`validate.go`); G2 is the task that lifts that refusal, and it stays
`json.RawMessage` no longer — G-B is the signature that lets its
`{speed, pause_when}` shape be parsed into fields, because the shape is now read
rather than merely refused.

### G3 — `reveal`

**Shape:** `"reveal": { "anim": "<token>" }`. **Axis:** character count.
**Trigger:** node appearance.

A `text` node with `reveal` shows a growing prefix of its content: at phase *p*
(curve-eased `elapsed / duration_ms`), the visible length is `round(p *
len(content))` graphemes, cut at grapheme boundaries the way `truncateText`
already cuts wide characters so a reveal never splits one. `p = 0` shows
nothing, `p = 1` shows the whole string, and past the end the full string draws
forever. This is a typewriter over the *display-width* count the renderer
already computes; it introduces no new measurement.

### G4 — `enter`

**Shape:** `"enter": { "row": true, "stagger": "<token>" }`. **Axis:** row count
(the container) composed with a per-row one-shot. **Trigger:** appearance of the
list.

`enter` animates the rows of a `list` (or the children of a container) arriving
in sequence. With `row: true`, row *i* begins its own entrance at offset `i *
stagger.duration_ms`, so the list fills top-to-bottom rather than all at once.
Each row's *own* entrance is a `transition` (intensity ramp) unless the row also
carries `reveal`, keeping the alphabet closed: `enter` is a **scheduler** over
the other two props, not a fifth axis. The container draws each row in whatever
state its personal clock has reached — a row past its offset+duration is
settled, a row before its offset is not yet drawn — so the visible row count
grows as the stagger advances, which is the row-count axis above. `stagger`
names a timing token whose `duration_ms` is the inter-row delay.

**`row: false` (or omitted) is the whole-container entrance:** the container
transitions in as one unit, identical to putting `transition` on the container
itself. `row: true` is the only form that needs the scheduler; naming both
keeps the degenerate case from being a special path.

### Refusals / empty state (each addressed)

- A prop naming an `anim` token absent from the theme → the load-time refusal
  D4 already signed and G0 implements (`internal/theme`), naming the token and
  the node. G-B adds no new token-resolution rule; it consumes the one that
  ships.
- `scroll` with a non-positive `speed`, or `enter` with `row: true` and no
  `stagger` token → load-time refusal naming the node and the missing/invalid
  field, the same net every other shape gets. A zero `speed` marquee would tick
  forever without moving — a defect worth naming, not clamping.
- `reveal`/`transition`/`enter` on a node type whose axis does not apply (e.g.
  `reveal` on a node with no text content) → load-time refusal naming the node
  type and the axis, rather than a silent no-op. The axis a prop rides is part
  of its signature, so a prop on the wrong node type is a scene defect the same
  way a `row.*` bind outside a template is.
- Any behaviour needing a fifth axis (opacity, sub-cell motion, colour
  interpolation beyond dim/bright) → **out of scope, refused by omission**: it
  is not signed here, so a document requesting it is refused, and adding it is a
  future mother-binary freeze. This is the explicit boundary that keeps G1–G4
  from growing into "an animation engine".

### Text to sign (append to `docs/SCENES.md` Scene 4)

Scene 4's status paragraph (`SCENES.md:150-165`) moves `transition`/`scroll`/
`reveal`/`enter` from "warned about, waiting on a clock and their semantics" to
a signed per-prop table: for each prop, its shape, the single frame axis it
rides (intensity / horizontal offset / character count / row count), its
trigger (appearance vs continuous), and the invention-free principle above —
that the clock chooses which already-expressible frame to draw and adds no
fifth axis. The `Scroll`/`FocusGlow` comments in `internal/scene/node.go` that
still cite "render semantics signed nowhere" are updated to cite this section.

---

## Open decisions for the owner

Three genuine forks are flagged rather than silently resolved, because they
change behaviour the ecosystem will depend on:

1. **Ticker lifecycle (G-A.4).** Proposed: the ticker runs only while a visible
   node animates. The alternative — a always-on low-rate heartbeat — is simpler
   to reason about but repaints when nothing moves, which the "repaints only on
   events" property exists to avoid. Recommend the on-demand ticker.
2. **`transition` trigger (G1).** Proposed: appearance only; value-change
   transitions are a separately-signed future prop. Accepting this now stops a
   value-change cross-fade being invented inside G1.
3. **One-shot replay (G-A.5).** Proposed: a node that leaves and re-enters the
   frame animates again. The alternative — animate once per session — needs the
   host to remember forever that a node was seen, which no other host state
   does. Recommend animate-on-each-appearance.

Once these are settled and the text above is signed, G1–G4 implement one prop
each against this contract, each landing with a golden pinned at chosen phases
(t=0, mid, settled) and a counterfactual that reverts the phase mapping and
shows exactly that prop's tests failing — the same discipline every Block E/F
task closed with.
