# arxi tui — plan

This is the plan of record. The Spanish originals of the design conversation
(`docs/PLAN-arxi-tui.es.md`, `docs/SCENES.es.md`) are not carried here on
purpose: this repository is English-only, and this file replaces them.

## The idea in one line

A terminal interface where **the UI is a data document** that the running
instance can rewrite live, directed by the user. Not "a theme", not "a
full-screen extension": the whole interface, alive and addressable.

## The goal is a spectrum, not a look

The reference point *fx* (Vercel Labs' Zig coding agent) is about restraint and
whitespace, not the target itself. What matters is that **the same engine** can
wear any point of this axis, and the user moves along it without touching the
binary:

| Extreme | Scene | What shows |
|---|---|---|
| **Raw** | one `input` and one `text` | type, get a plain-text or minimal-markdown reply; nothing else |
| **Base (arxi-sim today)** | arxi-sim's chrome expressed as a scene | transcript, input bar, status rows, panels |
| **Sobria (the fx-inspired default)** | a preset over the base | one-line header, `┃` prompt, single rules, segmented footer, filterable menus, no color |
| **Maximum** | panels, widgets, overlays, banners, indicators | rich chrome assembled from parts, plugins everywhere |

Three properties make the spectrum possible, and arxi-sim had none of them:

1. **Everything is the same kind of data.** The factory scene *is* the same
   format the user edits: there is no privileged path.
2. **Removing is as easy as adding.** Going raw is not "disabling features",
   it is not having those nodes: the minimal scene is two nodes.
3. **Presets are scenes, not code.** "Sobria", "dense", "chat only" are scene
   documents that apply, compose, and edit like any other.

## What arxi-sim taught, and what is corrected here

Carried over as-is: the **pure fold** (nothing writes state; everything
proposes), **goldens** (default output pinned byte-for-byte; a pixel change is
a review event), the **cell/Frame model** and terminal backend, `file:line:`
error discipline, and the Windows procgroup pattern.

Not repeated: closed vocabularies with named owners (style key, glyph, widget
and slot lists you cannot extend), fixed composition (`[layout]` only reorders
and vetoes), extensions capped to one full-screen panel with no
`overlay.render`, and a non-composable `full` slot. In one phrase: arxi-sim
froze the *how* to govern the *what*; arxi tui opens the *how* and trusts the
user. See `docs/LESSONS.md` for the itemized ledger.

## Central decision: the interface is data, not compiled code

### 1. The scene tree

The interface is one document: a tree of typed nodes with props and styles.

- Base nodes: `stack`, `row`, `box`, `text`, `markdown`, `input`, `overlay`,
  `list`, `spinner`, `marquee`, `switch`/`slider`, `button`, `sparkline`,
  `rule`. The set is **open by design**: adding a node type is a new capability
  of the host, not a vocabulary crime.
- **Every node has a stable `id` and an absolute address**
  (`root/squad/{2}`), which is what makes "add a row above the input" a *patch*
  instead of a fork.
- The default chrome is simply the default scene: same format, no special
  path.

The format itself — the v0 vocabulary, the bind model, the 23 settled
decisions — lives in `docs/SCENES.md`. That document is the contract; this
plan does not restate it.

### 2. Hot reload

- The instance watches its scene document. A change validates and re-renders.
- An invalid patch **never kills the session**: `file:line` error, last good
  scene stays on screen. A *semantically* impossible scene (layout loop,
  dead bind, zero-period animation) must fail the same way — validation is
  not just schema.
- Rendering is a scene diff → cell update. State (transcript, focus, scroll)
  lives outside the scene: the scene says *how* things look, never *what* is
  there. Scroll/focus survive a geometry change because they are keyed to
  fold content, not row numbers (the arxi-sim remembered-row bug).

### 3. A control surface for the live instance

- **Deterministic (commands):** `/ui add node below_input …`, `/ui move …`,
  `/ui style …`, `/ui plugin add <url>`. Validated, logged, testable.
- **User/agent-directed (documents):** the user asks for a change; the agent
  patches the scene; it appears instantly. The agent never paints pixels — it
  **proposes scenes**, the same way it proposes events.

## Extensions: the model we want

An extension is a **provider of scene fragments**, not "one panel".

- Declarative-only plugins (scene + tokens) have zero code and zero risk; they
  cover the whole raw↔maximum range.
- Plugins with behavior are external processes (NDJSON) that mount fragments
  via their manifest (`mounts`), stream bind namespaces
  (`tick.price`), and route input through the scene's `on_press`.
- Overlays, banners, rows beside the input are nodes — not forbidden
  capabilities.
- The pipeline doors (Q23, settled): **B tools → C hooks → D providers**, each
  behind one consent gate *per grant*, and **self-extension** uses exactly the
  doors the user opened, on user command only.
- The escape hatch (the wasm interpreter, `wazero` or similar) stays a
  costed ADR in Phase 4 for renders that are none of our nodes.

## Install rule: one command, Termux included

**The user installs arxi, not arxi's dependencies.** Static Go binaries,
`CGO_ENABLED=0`, per-platform artifacts (linux/macos/windows/**android-arm64**
for Termux) published to GitHub Releases, with `install.sh` living **in this
repo** and fetched from the release host. `arxi.sh` is a later vanity domain
that will point at that same script once the project outgrows its owner —
until then no document prints a URL we do not control, because a public repo
advertising an installer site that does not exist is worse than no site.
Plugins never add a
user-side dependency: declarative is data, subprocess plugins ship as their
own static binary, wasm plugins are interpreted inside ours. Inherited from
fx: auto light/dark detection via the OSC 11 query.

## The default scene: the sobria look, written in the user's own format

The first visible product is the default scene, copied from fx's visual
*rules* — written entirely with the user's format, because if the factory
scene cannot be expressed with the user's tools, the tools are incomplete.

- **No color by default.** Everything in the terminal's own grays; emphasis is
  **brightening text**, never painting backgrounds. Color is a later layer.
- One-line header: `Δr×i v0.1.0 · Run /help for commands`.
- The prompt is the `┃` **prefix of the input node**, not a box, not a node.
- Panels and menus sit between single `────` rules, not inside frames.
- Minimal status footer: `auto · kimi-k3 · ⚡︎`.
- Menus: flat filter-as-you-type list, `tab` to walk categories
  (`All · General · Session · …`), a count in the header, a key row in the
  footer.
- From the fx scaffolding forward, keep **our** backend: transcript, markdown,
  tools, fold — from arxi-sim and the arxi core.
- The thinking line is the marquee that arxi-sim already built
  (`internal/ui/block.go` `thinkingMarquee`): `• Thinking (3s) (↑4 ↓11)` with
  the reasoning scrolling left behind the gray counters.

## Stack

- **Go**, single static binary. Dependencies allowed and named; install rule
  is the real constraint (see above).
- **New project** (`arxi_tui`, this repo), own module: mixing the two design
  philosophies in one repo would rot the old one.
- **Reuse from arxi-sim**: terminal backend, cell/Frame model, fold, golden
  discipline, procgroup supervision, consent/identity machinery — **by copy,
  never import**; two contracts, two repos. `docs/LESSONS.md` is the map of
  what was already paid for.
- **Interface with the arxi core** (`D:/projects/arxi`): arxi-tui is a
  frontend to the same kernel the CLI drives — events, log, `host/v1`
  capabilities. The bind inventory (Phase 0.5) has two sources with two
  authorities: run-state binds map onto the core's event catalog and fold,
  while view-state binds (`slash.active`, `ui.*`, the busy line) are a
  vocabulary the core never defined because it never had a UI — that half is
  genuine arxi-tui design, and treating it as transcription is how Phase 0.5
  becomes a dead end.

## When data, when code

| I want… | Tool |
|---|---|
| colors, glyphs, type, opacities | style tokens (data) |
| input borders, shine, spinner | node props (data) |
| reorder / veto / duplicate rows | scene (data) |
| a new row above the input | new node in the scene (data) |
| a floating popup, a banner | `overlay` node (data) |
| buttons, lists, switches, dashboards | scene nodes (data) |
| logic: compute something, react, register tools, hooks | plugin process (code, gated) |
| a render that is none of our nodes | wasm runtime — **ADR, Phase 4** |

Rule: if it can be said as data, it is said as data. Code is the last option,
not the first.

## Phases

- **Phase 0 — Scene engine.** Document, validation (syntax *and* semantics),
  diff to cells, hot reload, reproducible default scene. Ends with the **raw
  scene running and usable daily** — not with a "complete engine". The golden
  budget is paced by the phases: Phase 0 pins the raw scene and the engine's
  property tests only; each golden scene gets its family when the phase that
  needs it lands, never all eleven up front.
- **Phase 0.5 — The bind vocabulary.** The closed inventory of fields the host
  exposes to scenes (`chat.history`, `agent.working`, `usage.in/out`,
  `ui.focus`, `ui.max`, …) plus computed derivatives, and the open plugin
  namespace. The inventory is *designed*, not transcribed: half its fields map
  to core events, the other half (view state: `slash.active`, `ui.*`, the busy
  line) exists only because this project has a UI, and it must be written
  down as arxi-tui's own contract. It lands in two beats so it cannot become
  the project's bottleneck: a **bootstrap set** — the handful of binds the
  raw scene needs (`chat.history`, `user.input`, plus the host's own focus
  and escape state) — frozen with Phase 0, enough to boot and use the
  interface; the **full vocabulary** (`docs/BINDS.md`) frozen before Phase 1
  pins its goldens, because every golden scene binds against it.
- **Phase 1 — The spectrum.** Tokens, base nodes, and the golden scenes:
  raw, sobria default (the fx rules above), and the maximum dashboard/panel
  set. Range visible from day one.
- **Phase 2 — Mutation from inside.** `/ui …` commands + agent-driven patches,
  with validation, `file:line`, goldens, and the **change-diff view** (the
  agent shows what it altered before it is trusted). Gated on proof, not on
  hope: before the feature ships, an **eval corpus** — natural-language order
  → correct scene patch — runs against the model on the eleven scenes we
  already have. If the model cannot patch scenes reliably, "autoextendable by
  command" is not a feature and the document must say so.
- **Phase 3 — Third-party mounting.** Plugins as scene fragments mounted by
  id; overlays, banners, input-adjacent rows, per-node focus and input;
  `/ui plugin add <url>`; the community installer itself as a scene (Q16/17).
  Tools gate (door B) lands here or in Phase 4, with the consent contract.
- **Phase 4 — Behavior.** Doors C and D, self-extension over all doors, and
  the embedded-wasm decision as a costed ADR.

## Invariants this plan must not break

1. With no customization, the factory scene draws byte-identical frames.
2. The fold is pure and host-owned; the scene says form, the fold says
   content; the fold never waits on the scene.
3. An invalid patch never kills the session: last good scene stays; a valid
   but unsatisfiable scene fails the same way a syntax error does.
4. Every error carries `file:line:`.
5. If it can be expressed as data, code is not required.
6. **The scene never captures the exit.** An immovable panic gesture
   (`Ctrl-C` twice / `-scene ""`) restores the raw scene no matter what the
   active scene or plugin does — this is what makes community content safe to
   run.
7. Plugins propose, never write; every plugin effect is an attributed event in
   the arxi log, and every power is granted at the gate, once, never
   re-asked on ordinary use and never self-extended without a user order.

## Open risks (honest list)

- The promise "usable as time passes" is front-loaded: it depends on
  validation, the last-good-scene rule, and the immovable escape hatch
  working **from Phase 0**, not from Phase 2. Until the agent-patch story is
  proven (Phase 2 eval corpus), a broken user scene must be survivable by a
  human editing the file — the raw fallback is the guarantee, `/ui` is the
  convenience.
- "Usable as time passes" also means *old documents keep booting*: node types,
  style tokens and binds grow, so a scene written for v0 must render under v1.
  The mechanism is the same one that makes community content safe —
  unknown-but-parseable is a warning, missing state is a placeholder (Q16
  preview mode), and no v0 construction is ever redefined, only added to.
- The scene format may still hit a ceiling we have not seen; Scene 18 (the
  user's strangest idea) is the pre-test.
- Derivatives/binds are a real design surface — the Q3 debt, now Phase 0.5.
- Testing scales differently here: user scenes are unbounded, so goldens
  cover the *engine and the eleven scenes*, and property tests cover
  invariants (no row ends in bare air, contraction order, escape hatch).
- Animation budget fights render cost; the 120 ms coalescing seed generalizes,
  but a community scene can always ask for the impossible.
