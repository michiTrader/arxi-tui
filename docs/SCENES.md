# Golden scenes — the exercise that validated the format

Before coding the engine, the reference scenes were written by hand in the
user's own scene format. If a scene did not fit, the format was wrong and
would be fixed here, on paper, for free. It did hold; what the exercise
produced instead was 23 vocabulary decisions, all now settled.

Status (2026-09-14): the owner of the product **signed all recommendations**
(questions 1–23 pass with their provisional answers), with three amendments:
Q12 — an `overlay anchor:"full"` scene may own the screen but never the escape
hatch (invariant 6 in `PLAN.md`); Q14 — a render that is none of our nodes is
the Phase-4 wasm ADR, and until that lands the ceiling is honestly our node
set; Q23 — the gate-UX rule: consent is asked when granting a power, never
again when a granted power is used (see the Q23 section below). Q4: the
filterable list is a primitive.

## Format: JSON

- The one editing live is mostly the agent → JSON is what models write
  without hallucinating, and every plugin language can parse it.
- Humans edit through `/ui` and applied presets; the raw file is the
  serialization, not the primary interface.
- The validator maps `offset → file:line`, so errors keep their address.
- Rejected for now: TOML (nested trees are awkward), a custom indentation DSL
  (a parser, validator and error model by hand, for no extra expressiveness).
  Revisit when the property set freezes.

## Primitive vocabulary (v0, settled)

Containers: `stack`, `row`, `box` (optional border), `overlay` (anchored,
floats above everything; `anchor:"full"` legal).
Content: `text`, `markdown`, `input`, `spinner`, `marquee`, `list`
(filterable, categories, count, `row_template` with relative binds),
`button`, `switch`/`slider`, `sparkline`, `rule`.
Universal: `id`, `bind` (absolute `path.state` or relative `row.field` inside
a template), `when`, `style`, `grow`/`weight`, `on_press`, `scroll`.
Actions are a closed vocabulary per surface (`cmd:/slash`, `ext:<name>:<action>`,
`answer:<kind>`, `focus:<node>`), extended only through registered names.
Binds come from a Phase-0.5 inventory: host-owned fields, computed
derivatives (`usage.in/out`, `todos.count`, `ui.focus/max/surface`), plus the
open plugin namespace (`tick.price`).

Styles are named tokens with **open definition**: users and plugins may mint
tokens (`"warn": "yellow"`); the validator checks references, not inventory.
This is the deliberate inversion of arxi-sim.

## Scene 1 — RAW (type and answer, nothing else)

```json
{ "root": { "type": "stack", "children": [
  { "id": "chat",   "type": "markdown", "bind": "chat.history", "grow": 1 },
  { "id": "prompt", "type": "input",    "bind": "user.input", "placeholder": "> " }
]}}
```

Proves: the minimal scene is legal and boots; frames/conditionals/overlays are
optional for real. Decided: `markdown` is its own node; the cursor lives in
the `input` node.

## Scene 2 — SOBRIA (the fx-inspired default)

```json
{ "root": { "type": "stack", "children": [
  { "type": "text", "style": "header",
    "text": "Δr×i v0.1.0 · Run /help for commands" },

  { "id": "chat", "type": "markdown", "bind": "chat.history", "grow": 1 },

  { "id": "thinking", "type": "marquee", "when": "agent.working",
    "bind": "thinking.text", "prefix": { "text": "• Thinking · ", "style": "dim" },
    "suffix": { "bind": "usage.delta", "style": "dim" } },

  { "id": "prompt", "type": "input", "bind": "user.input",
    "prefix": "┃ ", "placeholder": "ask anything, or / for commands" },

  { "id": "menu", "type": "overlay", "anchor": "bottom", "when": "slash.active",
    "children": [
      { "type": "rule" },
      { "id": "cmds", "type": "list", "bind": "slash.matches",
        "filter_by": "typed", "count": true,
        "categories": ["All","General","Session","Account","Model",
                       "Appearance","Security","Workspace","Media","Extensions","Product"] } ] },

  { "id": "status", "type": "row", "children": [
    { "type": "text", "bind": "slash.hint", "style": {"style": "dim"},
      "when": "slash.hint" },
    { "type": "text", "bind": "agent.mode", "style": {"style": "header"},
      "when": "status.active" },
    { "type": "text", "text": " · ", "style": {"style": "dim"}, "when": "status.active" },
    { "type": "text", "bind": "model.name", "style": {"style": "dim"},
      "when": "status.active" },
    { "type": "text", "text": " · ⚡︎", "style": {"style": "dim"},
      "when": "status.active" } ] }
]}}
```

No color: emphasis by brightening text, never painting backgrounds; light/dark
adaptation is the terminal's own, through relative dim/bright attributes (the fx
look, achieved without an OSC 11 query — see TOKENS.md and `theme.SOBRIA`). From
this scaffolding forward, the backend stays ours: transcript, markdown, tools,
fold.

Decided here: Q1 marquee scrolls only its bound text; Q2 the menu is an
overlay, not a stack row (the transcript must not jump); Q3 derivatives are
host-computed, named, and bound by scenes; Q4 the categorizing list is a
primitive so all menus in the ecosystem look alike.

## Scene 3 — MAXIMUM (scene 2 + side panel + banner + floating tokens)

```json
{ "root": { "type": "stack", "children": [
  { "id": "banner", "type": "box", "style": "banner",
    "children": [ { "type": "text", "text": "Δr×i v1.0 ── Trading session" } ] },

  { "type": "row", "grow": 1, "children": [
    { "type": "stack", "weight": 3, "children": [
      { "id": "chat", "type": "markdown", "bind": "chat.history", "grow": 1 },
      { "id": "thinking", "type": "marquee", "when": "agent.working",
        "bind": "thinking.text",
        "prefix": { "text": "• Thinking · ", "style": "dim" } } ] },
    { "id": "tasks", "type": "box", "weight": 1, "title": "Tasks",
      "border": "single",
      "children": [ { "type": "list", "bind": "agent.todos" } ] } ] },

  { "id": "prompt", "type": "input", "prefix": "❯ ", "grow": 0 },

  { "id": "tokens", "type": "overlay", "anchor": "top-right", "min_width": 12,
    "border": { "shape": "single", "style": "warn" },
    "children": [ { "type": "text", "bind": "session.tokens_used" } ] }
]}}
```

Decided: Q5 overlays form a focus stack, `esc` closes the top one; Q6
contraction order is fixed and documented (elastic panes first,
input/banner/footer never); Q7 overlays may declare `min-width`.

## Scene 4 — ANIMATION (focus glow, marquee cadence, staggered reveal)

`focus_glow`, `transition`, `scroll: {speed, pause_when}`, `reveal`,
`enter: {row, stagger}` — all node props on the host clock. Decided: Q8 timing
is a global `[anim]` token with per-node override; Q9 the host announces
"row new", the scene owns the animation. Form/content separation intact.

**Implementation status.** `focus_glow: { "style": "<token>" }` is implemented:
when a node's `id` equals `ui.focus`, the engine renders its content under the
named token. It needs no clock — its only input is the focused node's id, which
`ui.focus` already supplies — so it was the one property of this scene that
could land before the host clock existed.

`scroll: { "speed": <cells/tick>, "pause_when": "<bind>" }` is now implemented
too (G2), and it is what built the host animation clock (ADR-0005): a marquee
whose content overflows its budget advances a horizontal window at
`(ticks * speed) mod (width + gap)`, the tick count coming from the loop's
`time.Ticker`, and `pause_when` freezes the offset while its bind is truthy. A
`scroll` on any node type but `marquee` is refused with an address — the axis
belongs to the marquee — and a non-positive `speed` or an unsigned `pause_when`
bind is refused the same way. Every existing golden is unchanged: the phase is
an input separate from the fold, so a document with no scroll renders exactly
as before.

`reveal: { "anim": "<token>" }` is implemented too (G3), the first one-shot prop
on the clock scroll built. A text node with `reveal` draws a growing prefix of
its content: the host clock eases `elapsed / duration_ms` through the token's
curve into a phase `∈ [0,1]`, and the renderer clips the content to
`round(phase * width)` graphemes — the character-count axis, one the renderer
already draws whenever it truncates text, so it invents no new rendering. The
phase reaches the engine as `Renderer.AnimPhase`, an input separate from the
fold (Q9), and a nil phase draws the whole text, so no existing golden moves. A
`reveal` on any node type but `text` is refused with an address — the axis is
the text node's — and the `anim` token is checked against the theme's `anim`
section at load (an empty token resolves `anim.default`, Q8), the same net a
style token gets. It is a one-shot on appearance: the clock starts when the node
appears, runs `0 → 1` once, and the node draws settled thereafter (ADR-0005).

`transition: { "anim": "<token>" }` is implemented too (G1), the second one-shot
prop on that clock. A node with `transition` wears the theme's dim intensity
while its entrance runs and its own settled style once the phase reaches `1` —
the SGR dim→bright intensity axis, one the renderer already draws whenever it
resolves a style token, so it invents no new rendering. The phase reaches the
engine as the same `Renderer.AnimPhase` (an input separate from the fold, Q9),
and a nil phase draws the settled style, so no existing golden moves. Unlike
`scroll` (marquee) and `reveal` (text), transition carries **no node-type
refusal**: the intensity axis is universal, so it is honoured at the `renderNode`
chokepoint on every node type — the same reach `focus_glow` has, and the reason
`enter`'s `row:false` container entrance is "identical to putting transition on
the container itself". A container has no own content to dim, so it is
unaffected the way it is for a glow; dimming a subtree is `enter`'s (G4)
scheduler question. Its only load-time refusal is the timing token, checked
against the theme's `anim` section exactly as reveal's is. The intensity axis is
discrete (dim / normal / bold), so a transition draws two states — dim while
running, settled when done — with no intermediate frame; a finer ramp would need
the opacity or colour axis this section refuses as the fifth axis.

`enter: { "row": true, "stagger": "<token>" }` is implemented too (G4), the
scheduler that closes Scene 4's animation vocabulary. With `row:true` a container
staggers its rows — the children of a stack/row/box or the instantiations of a
list's `row_template` — so row *i* begins its own entrance at offset
`i * stagger.duration_ms`: a row past its offset+duration is settled, a row
mid-entrance draws dim, and a row before its offset is not drawn at all. The
visible **row count grows** top-to-bottom as the stagger advances, which is the
axis that distinguishes `enter` from putting `transition` on every row (which
draws all rows dim at once, count fixed). One token drives both the inter-row
delay and each row's own dim→settled ramp, so the shape names exactly one
timing token. `row:false` (or an omitted `row`) is the degenerate whole-container
entrance: the subtree dims as one unit until settled — the subtree dimming
`transition` (G1) deliberately left to `enter`, because a container has no own
content to dim. `enter` composes the two earlier one-shot props rather than
adding an axis (G-B): each row's own entrance is a `transition`, and a row that
also carries `reveal` composes it on top. Its load-time refusals are the two a
schedule needs — a `row:true` with no `stagger` token, or a `row:true` on a node
with no rows (neither children nor a `row_template`) — plus the stagger token
checked against the theme's `anim` section, exactly as reveal's and transition's
are. The per-row phases reach the engine as the same host-computed
`Renderer.AnimPhase` (an input separate from the fold, Q9), each row keyed by the
container's id and its index, and a nil phase draws every row settled, so no
existing golden moves. The clock those props measure elapsed time against is
ADR-0005 (`docs/PLAN.md`); the timing vocabulary they consume is D4's `anim`
section (`docs/TOKENS.md`).

With `enter` implemented, all five of Scene 4's animation properties are drawn
by the engine and none remains parsed-and-warned.

Golden status (G5, 2026-09-23): `testdata/ANIMATION.json` freezes this scene as
`ANIMATION.frame`/`.styled`, pinned at a **chosen non-nil phase** — a windowed
`scroll` marquee, a phase-clipped `reveal`, a dim mid-entrance `transition`, and
a staggered `enter` list over `agent.todos` mid-flight (one settled row, one dim,
one not yet drawn). Motion is the subject here, so the golden pins a running
frame rather than the nil phase, which would draw every prop settled and cover
none of it. Each prop also carries its own render/clock test pinned at chosen
phases (the discipline DESIGN-BLOCK-G.md signs); this fixture is the composed pin
those isolated tests do not carry, and Block G is closed.

The render semantics are signed on one principle: **an animation never draws
anything the renderer cannot already draw at a fixed phase; the clock only
chooses which already-expressible frame to emit at time t.** The frame already
varies along four axes a static document produces, and each prop is a rule
mapping the curve-eased phase `∈ [0,1]` onto one of them:

| Prop | Shape | Frame axis | Trigger |
|---|---|---|---|
| `transition` | `{ "anim": "<token>" }` | SGR dim→bright intensity | node appearance |
| `scroll` | `{ "speed": <cells/tick>, "pause_when": "<bind>" }` | horizontal offset (marquee) | continuous while visible |
| `reveal` | `{ "anim": "<token>" }` | character count (typewriter) | node appearance |
| `enter` | `{ "row": true, "stagger": "<token>" }` | row count, scheduling per-row `transition`/`reveal` | list appearance |

`enter` is a **scheduler** over the other two props (row *i* starts at
`i * stagger.duration_ms`), not a fifth axis. Any behaviour needing a fifth axis
— true opacity, sub-cell motion, colour interpolation beyond dim/bright — is
**out of scope and refused by omission**: adding it is a mother-binary freeze,
the same bar as adding a node type or a curve. That boundary is what keeps
G1–G4 from growing into "an animation engine".


## Scene 5 — CONFIG (the /config screen as a scene, not as Go)

Category `list` + a settings `list` with `row_template` mixing `switch` and
`input` rows by `row.kind`, provenance line, key footer. This is the
dogfooding test: arxi-sim's `/config` is 730 lines of Go; here it is a
document. Decided: Q10 relative binds exist inside templates; Q11
`switch`/`slider` are first-class primitives (the only forced v0 addition);
Q12 full-screen overlays are legal but can never capture the escape hatch.

## Scene 6 — A COMMUNITY PLUGIN SHIPPED BY LINK (the ticker)

`/ui plugin add https://…/tick` → manifest with `executable`,
`capabilities`, `consent_required`, and `mounts` declaring its UI as
fragments (a `sparkline` + `text` row inside a top-right overlay). The
process streams NDJSON frames into the `tick.*` bind namespace. The sparkline
is a *mother-system* node: the plugin composes our primitives, it does not
bring render code. Decided: Q13 a plugin overlay joins the focus stack; Q14 a
non-expressible render wants the Phase-4 wasm ADR; Q15 download ≠ trust ≠
grant — the consent gate checks identity (name, version, executable, args,
capability set, digest) exactly as arxi-sim's contract, with remember.

## Scene 7 — COMMUNITY (browse, preview, install, from inside the TUI)

The installer *is* a scene: a `list` of registry entries, a `markdown`
preview pane, a search `input`, `i` to install. Previewing a stranger's scene
requires the engine to render unsatisfied binds as placeholders
(`community.*` mocks from the manifest) instead of crashing. Decided: Q16
preview mode is part of the engine contract; Q17 the registry is a JSON index
in a repo — no servers. Full bundle sharing (scene + theme + plugins in one
manifest, one consent screen) rides on the same gate.

## Scene 8 — BUTTONS

`button` with `on_press: "answer:approve"` and friends. Decided: Q18 the
action vocabulary is closed per surface and extended only through registered
names; Q19 tab order follows scene order, and the input may opt out
(`tab: false`) to protect the typing flow.

## Scene 9 — SUBAGENTS (below the input)

A `list` bound to `team.members`, per-row spinner-or-glyph by state,
`ticking`/`waiting`/`idle` from the fold's team events, each row clickable
into that agent (`cmd:/agent {m.id}`). What arxi-sim hand-coded
(`internal/app/team.go`) becomes a document. Decided: Q20 action args
interpolate relative binds, same machinery as Q10.

Golden status (E4, 2026-09-22): `testdata/SUBAGENTS.json` freezes the
`row_template`-per-element core — one row per member, `row.role` resolved
against its own element — which is the Block E machinery this pins durably. The
row template is a single content node (`text`) because a container reached
through `row_template` has no own-style rendering to honour, so the styled-node
audit (`internal/engine/nested_node_style_test.go`) would read a container
template as a silent style drop; combining the spinner-or-glyph with the role
label on one row waits on that decision. The per-row `when` gate, the empty
state, and the falsy-out-of-template rule are pinned by
`internal/engine/row_template_test.go`. Clicking a row into its agent
(`on_press: "cmd:/agent {row.id}"`) is still refused (`on_press`,
`internal/scene/validate.go`) and lands with the action-dispatch work (H8).

## Scene 10 — DASHBOARD (2×2 with click-to-maximize)

No `grid` primitive was needed: nested `row`/`stack` with `weight`, per-pane
`when: ui.max==''`, `on_press: "cmd:/max <pane>"`. Decided: Q21 `ui.*` is the
host-owned interface state (focus, maximized pane, current surface); scenes
read it with `when` and write it only through registered commands.

## Scene 11 — ANIMATED, INTERACTIVE BANNER

`box` + `shine` + `marquee` + `when: session.new_milestone` + `on_press` in
one node, with no new primitives — the composition of scenes 2, 4 and 8.
Decided: Q22 hit-testing runs on the final frame's cells; the engine resolves
marquee/cursor collisions, not the scene.

## Q23 (signed) — how much of the pipeline plugins may touch

The arxi core already has every door inside it: `internal/tool` + `toolrun`,
`internal/provider`, `internal/blueprint` (system prompt, model, tool
grants), and `host/v1` capabilities. What was undecided was which open toward
the TUI. Settled order: **B tools first** (a plugin by link teaches the agent
a new tool; it runs as its own process), **C behavior hooks second** (gating
tool calls, prompt adjustments, custom compaction — with a stack order by
identity), **D providers third** (nearly free, already exists), and
**self-extension on top of whichever doors the user opened — and only on
user command.**

**UX rule (the owner's own words):** the gate is the exception, never the
rhythm. Consent is asked when **granting a power** (a persistent change: this
tool is registered, this hook is mounted), never again when a granted power is
used. When the user orders the agent to extend itself, the agent makes the
change and shows the **diff of what changed** with log attribution — no
blocking menu per step. Governable does not mean interrogated: it means the
truth is always available afterward.

## Verdict of the exercise

Eleven scenes fit in ~13 primitives; the only forced additions were
`switch`/`slider` and `button`. `grid` proved unnecessary. Twenty-three
format decisions fell out of the writing, none of which changed the
architecture. No scene required recompiling the mother binary. The honest
ceiling (a render that is none of our nodes, a pipeline hook behind door C) is
visible on paper, has its phase, and has its consent contract. The bet
survived the cheap test; the expensive test is now code.

## Next

1. Phase 0.5, in two beats (see `PLAN.md`): the **bootstrap binds** the raw
   scene needs, frozen with Phase 0; then the full inventory in
   `docs/BINDS.md` before Phase 1's goldens. The run-state half maps onto the
   arxi core's event catalog (`D:/projects/arxi/spec/events.md`) and `host/v1`;
   the view-state half (`slash.active`, `ui.*`, the busy line) is arxi-tui's
   own design — the core never defined it because it never had a UI.
2. Scene 18, the user's strangest interface, on paper.
3. Phase 0 with the raw scene as its exit criterion.
