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
- **Interface with the arxi core** (`D:/projects/arxi`): the kernel boundary
  is a *subprocess boundary* — arxi-tui spawns/attaches to an `arxi` binary, the
  two are never one process, and `internal/scene`/`internal/fold` import none of
  `internal/kernel` or `host/v1`. The CLI, by contrast, embeds `internal/kernel`
  directly (`cmd/arxi/main.go`): it is a sibling driver, not a sibling shape, and
  arxi-tui deliberately does not copy that embedding. The reasons are isolation
  (Section "Interface boundary"), not convenience of one codebase.
  - See **ADR-0001: the interface boundary** (below), the deciding choice of
    Phase 0: subprocess NDJSON is the contract, not shared memory or a Go import.
  - See **ADR-0002: the protocol the boundary speaks** (below): the wire is
    *the run log* (`spec/events.md` as the authoritative event catalog, ADR-0002
    of arxi) plus request/response invocations over a future subscription
    protocol; Fase 0 implements log-follow-and-replay and a request/response
    shim, deferring the subscribe extension until multi-client justifies it.
  - See **ADR-0003: projection, not expression** (below): binds are read-only
    named projections of the fold; the scene never carries an expression
    language, and view-state binds (`slash.active`, `ui.*`, the busy line) are
    arxi-tui's own contract because the core never owned a UI.

#### ADR-0001 — the interface boundary: one process per side

arxi-tui spawns (or attaches to) the `arxi` binary; the kernel never runs in
this process. The four rejected alternatives and a word on each:

1. **Import `host/v1` directly** (the tempting half-measure). `host/v1` is the
   DTOs and backend interface, not the kernel; to embed the kernel would require
   publishing a *new* Go API surface — a permanent compatibility promise over a
   repo this project does not control. Two kernels now, divergence later.
   Rejected.
2. **Copy the kernel by value** ("as the TUI does"). Copying UI machinery is
   defensible because it is stable; copying the *live* kernel freezes its log
   format and two trees diverge. Rejected.
3. **Daemon + multi-client socket**. Sessions surviving TUI exit already
   survive via the log: `event log` replays, `run attach` follows, and the
   state is durable without a daemon. That is the win of the log-as-truth model
   (arxi ADR-0002), not of process separation. A daemon adds lifecycle/upgrade
   scope with no new capability here. Deferred, never a default.
4. **In-memory embedding** (zero IPC, one process). Rejected for all of (1):
   version skew, no fakeable protocol boundary, no crash isolation, and the TUI
   dies with the kernel — which is intolerable because the panic gesture
   (invariant 6) must survive a kernel crash to restore the raw scene.

Chose **subprocess**. The procgroup supervisor from arxi-sim
(`internal/ext/supervisor`, `LESSONS.md:55-58`) ports wholesale: the TUI owns
the child, kills it on exit, and the escape gesture can `SIGKILL`+`respawn` the
core without the interface dying.

#### ADR-0002 — the protocol the boundary speaks: the log, plus invocations

The wire is two things, and both already exist in arxi rather than being
invented here:

- **The run log** (`spec/events.md`): append-only, `seq`-numbered, one JSON
  object per line. It is the truth (arxi ADR-0002); snapshots are cache. The
  host does not embed a live kernel — it reads the log file the kernel writes,
  the same way `run attach` does without taking the writer lock. Replay is
  free: feed any captured log to the fold. Property tests, golden re-runs, and
  the eval corpus all run from fixtures over the same path.
- **Request/response invocations**: the surface declares the verbs
  (`run.prompt`, `run.pause`, …) as request types, each one request → one
  response. This maps directly to `cmd/arxi/serve.go`'s documented NDJSON
  framing (one object per line, each direction). The serve protocol today is
  request/response, *not* a publish/subscribe event stream: `serve.go` states
  that a streaming mode would require subscription IDs, event messages,
  cancellation, and writer arbitration — i.e. an *extension* to arxi's serve
  protocol, not a thing arxi-tui can build unilaterally. Phase 0 therefore
  implements request/response for commands and **log-follow** (the follow-half
  of `run attach` lifted out of arxi-sim's `ask.go`) for the live stream; the
  subscribe extension is requested of arxi when multi-client justifies it. The
  handshake for Phase 0 is a single field: a `hello` with the protocol version
  this host speaks, answered by either a `hello` (version match) or a
  terminated handshake (version mismatch).

Chose **log-follow + request/response**, not a brand-new subscription protocol,
because the cost the other analysis missed is that *no subscription layer
exists yet in arxi to extend* — inventing it would make arxi-tui's success
depend on arxi protocol work, and the eleven goldens do not need it.

##### ADR-0002 re-decided (2026-09-21): keep log-follow, for a different reason

**The stated premise was false.** "No subscription layer exists yet in arxi to
extend" is wrong, and it was wrong when it was written. Measured against the
real core (`testdata/serve/session.ndjson`, arxi 0.0.1-spec, surface v1):

- `run.attach` is in the hello's `implemented` list — one of only ten verbs
  this build serves, alongside `run.start`, `run.cancel` and `run.show`.
- `event.subscribe` is in the session's `capabilities`.
- It is a full streaming implementation, not a stub: `cmd/arxi/serve.go` has a
  separate `streamingHandlers` table, `dispatchAttach` opens a
  `hostv1.Subscription`, and `serve_stream.go` has a `subscriptionPump` per
  subscription, an ack-before-events guarantee, terminal markers gated on the
  inspect capability, and a single `connWriter` mutex as "the entire
  arbitration".

Every specific thing ADR-0002 said arxi would have to build — "subscription
IDs, event messages, cancellation, and writer arbitration" — already exists,
with a named contract (ADR-0016) and tests.

**The decision stands anyway, and the honest reason is the opposite of the
original one.** It is not that subscribing is unavailable. It is that
subscribing is a *second* implementation of the one thing this host cannot
afford to get wrong twice.

- **Log-follow is not the cheap option, it is the one already measured.** This
  turn found that `LogFollow` delivered uncommitted events: it trusted
  newline-termination, while the commit point is the removal of
  `pending.commit` (logstore step 3). Two committed events plus a two-event
  in-flight batch produced four delivered events. That defect is now fixed and
  pinned by four tests. A subscription path would have to earn the same
  confidence separately — `Subscription.Next` returns confirmed batches, so
  the core does that work server-side, but the host would then have two
  ingestion paths with two different confirmation models, and the failure mode
  of the pair disagreeing is a wrong frame, which this repo holds to be worse
  than an error.
- **Replay is the property everything else rests on.** Goldens, the eval
  corpus and the fold's determinism all run by feeding a captured log to
  `Replay`. That path IS log-follow minus the polling. A socket stream is not
  replayable from a file, so adopting it as the primary path would mean the
  thing under test and the thing in production stop being the same code — and
  the `seq`/`sequence` defect is exactly what that costs: the decoder was
  right about the file and silently wrong about the wire for as long as only
  the file was exercised.
- **What `run.attach` genuinely offers that log-follow cannot**, recorded so
  this stays a comparison and not a preference: confirmed batching without
  the host parsing `pending.commit` at all; `after_seq` resumption; server-side
  `EventFilter` by type/source/actor; terminal markers, so the host learns a
  run ended instead of inferring it; and principal-scoped authorization. The
  terminal marker is the strongest of these — `run.result` is NOT the last
  event (it lands at seq 112 of 122 in the measured run), so a host reading
  the file has no positive signal that the log is complete.

**Therefore: log-follow remains the primary path, and the subscribe path is
promoted from "requested of arxi when multi-client justifies it" to a known,
available capability this host has chosen not to use yet.** The trigger for
adopting it is no longer multi-client. It is either of:

1. the host needing a positive end-of-run signal (the terminal marker), which
   `run.result` cannot provide; or
2. the host reading a log it does not own the directory of, where
   `pending.commit` is not beside the file — at which point parsing the
   marker stops being possible and the server's confirmed batches are the only
   correct source.

The premise that was wrong is recorded here rather than deleted: the ADR was
acted on for two turns on the strength of "no subscription layer exists", and
a decision that survives the removal of its own stated reason needs its real
reason written down, or the next re-reading will assume the original one still
holds.

#### ADR-0003 — binds are view projections, never expressions

Binds are read-only addresses into host state the node names; the host computes
them. The scene never embeds an expression language. Three namespaces, three
authorities:

- `chat.*`, `agent.*`, `run.*`, `usage.*`, `session.*`, `team.*`, `model.*` —
  run state, a named projection of the fold over `spec/events.md`. The core
  defines events; the *mapping* from event to view field is arxi-tui's design
  and lives in `docs/BINDS.md`, never in the scene.
- `ui.*`, `slash.*`, `user.input`, the busy line — view state. The core never
  defined these; this is arxi-tui's own contract (Q21: scenes read `ui.*` with
  `when`, write only through registered commands).
- `<plugin-id>.*` — open namespace. Anything a gated plugin streams via NDJSON
  lands here; the gate is the boundary, not a name list.

Counter-field rule (carried from `docs/BINDS.md`): an unsatisfied bind renders
as a placeholder, never a crash — this is the engine contract that makes
community preview (Q16) and forward compatibility possible at once.

#### ADR-0004 — pull by frame, not push by subscription

Binds are resolved in every frame the host renders, not via per-bind
subscriptions. Reasons: (1) it maps the scene directly onto the 120 ms coalescing
budget (`LESSONS.md:50-54`) — one clock, one resolution pass; (2) it keeps the
fold stateless across frames so geometry survives resize and scroll state stays
keyed to content (the remembered-row bug, `LESSONS.md:15-18`); (3) it removes an
entire family of recalled-record bugs — the fold does not track subscribers.
Scene node counts are tens, not hundreds of thousands, so a per-frame walk is
within the budget with headroom, and the property test `N nodes < X ms` is the
only throttle. A subscription layer would be a second tracker to maintain and a
second concurrency bug class; the coalescer is the only timer this engine
carries. Chose **pull by frame**; revisit only if the property test proves the
budget is missed by a real scene, not a synthetic one.

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
  already have. The corpus measures the **repair loop, not the first shot**:
  order → patch → validator error → retry, until convergence; first-shot
  accuracy is a vanity metric, because the real use is exactly the case where
  the engine said `file:line:` and the model had to read it. And it is data,
  not code: it can be written before the validator exists, which makes it
  Phase 0's acceptance suite from the start. If the model cannot patch scenes
  reliably, "autoextendable by command" is not a feature and the document
  must say so.

  **Measured (2026-09-22), and the ship decision.** The corpus ran against a
  real model for the first time: `deepseek-v4.1-flash` on an OpenAI-compatible
  endpoint, three runs (a single run of a non-deterministic model is weak
  evidence). **11 of 12 case-runs converged.** The two findings that decide the
  question are not the ratio:

  1. *The repair loop works against a real model.* In two of three runs a
     turn-1 JSON syntax error was returned as an addressed refusal and the
     model self-corrected on turn 2. That is the ask→grade→hand-back mechanism
     this phase exists to prove, exercised end to end by a real model rather
     than a stub.
  2. *The single miss is the one the corpus was built to detect, not a
     repair-loop fault.* `raw-add-tasks-panel` predicts the model guesses a
     bind name (`tasks.list`) for what BINDS.md signs as `agent.todos`; because
     an unknown-but-parseable bind is a warning, not a refusal (invariant
     below), there is no validator message to repair from, so it scores
     `incomplete`. It converged in the other two runs — a probabilistic
     bind-naming slip against a deliberate product tradeoff, not an inability
     to read addresses.

  **Decision: ship agent-command-driven `/ui` self-extension**, under the
  consent contract that already governs every other agent effect — the agent
  proposes a patch, the host shows the change (the change-diff view, still to
  build), it is applied on approval, and it is an attributed event in the log.
  The evidence answers "can a model patch scenes reliably" with *yes* for this
  model, and the failure mode that remains is a naming slip a diff-and-approve
  step catches by eye, not a class of silent corruption. The caveat is written
  down rather than left implicit: this is one model, one provider, and the
  four-case corpus — so the decision is *ship behind the diff/consent gate*,
  and Block A4 (widen the corpus) and re-runs on other models stay on the plan
  as the thing that would move it from "reliable for this model" to "reliable".

  ##### ADR-0003 (2026-09-22): the change-diff view is a host-generated scene, not an engine capability

  Building the change-diff view (Block B) forces a fork: is it a *scene* the
  engine renders like any other, or a *new engine capability* drawn by
  dedicated code? The fork matters because it is the project's thesis in
  miniature — "the factory-default interface is written with exactly the same
  format the user has" (AGENTS.md). A diff view drawn by bespoke engine code is
  chrome the user cannot rewrite, which is the arxi-sim closed-vocabulary
  mistake in a new place.

  **Decision: the diff view is a scene the host generates from the `Diff`, using
  only node types the engine already renders.** Measured against
  `renderNode`'s dispatch, the available types are enough: a side-by-side view
  is a `row` of two `stack`s (old / new), each `stack` a sequence of `text`
  nodes whose literal content is one diff line and whose `style` token is
  `diff.del` / `diff.add` / `diff.context`. Those three tokens are new, but the
  token namespace is open by design (TOKENS.md), so minting them in the factory
  theme is the intended path, not an engine change. The host already ships
  scenes it authored (SOBRIA.json); a diff scene it authors from the patch
  result is the same move, just built at runtime instead of read from disk, and
  it is shown transiently (an `overlay`, the type that already exists for
  exactly this) rather than merged into the user's document.

  **Why not a new node type or a host-drawn frame.** Either would render a diff
  the user cannot restyle, reposition, or replace — and the diff view is the
  one surface where the user most needs to trust what they see, so it is the
  worst place to make it un-rewritable. A `diff` node was considered and
  rejected: it would bind the diff shape into the engine, where a plugin or a
  future phase could not change how a change is shown, and it buys nothing the
  `row`/`stack`/`text`/`overlay` composition does not already give. The one
  thing the engine may still need is confirmed by measurement before B4 builds
  on it: that a `text` node renders its literal `text` under a per-node `style`
  token (it does — `renderNode`'s `text` case), so no per-item list styling and
  no new binding is required.

  **What this leaves for B4.** A pure function `Diff -> *scene.Node` (or a
  scene fragment), tested by a golden the same way every other scene is, plus
  the three theme tokens. The consent/attribution wiring (B5-B7) is unchanged
  by this decision, because it operates on the patch `Result`, not on how the
  diff is drawn.

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
   but unsatisfiable scene fails the same way a syntax error does. The boot
   path obeys the same rule and cannot inherit it from the hot path: a scene
   document that is corrupt **on disk** when the instance starts — written by
   a crashed session, or a stranger's download — falls back to the raw scene
   with the `file:line:` notice on screen. At boot there is no "last good" to
   stay on, so the fallback is explicit, invariant-listed, and tested — not
   implied by the hot-reload code.
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
  invariants (no row ends in bare air, contraction order, escape hatch,
  boot-corrupt fallback to raw). One harness, not two: the eleven golden
  scenes are the fixture set for both the hostile property tests and the
  Phase 2 eval corpus — two suites fed by one infrastructure.
- Animation budget fights render cost; the 120 ms coalescing seed generalizes,
  but a community scene can always ask for the impossible.
- **The boundary protocol must not leak future scope into Phase 0.** arxi's
  `serve` is request/response, not a subscription stream — the code documents
  that a streaming mode needs subscription IDs, event messages, cancellation,
  and writer arbitration, i.e. an extension arxi-tui cannot ship unilaterally.
  Phase 0 therefore uses log-follow (the follow half of `run attach`, lifted
  from arxi-sim's `ask.go`) for the live stream and request/response for
  commands, deferring the subscribe extension until multi-client justifies it.
  If that deferment turns out to block a Phase 0 need, the fix is scope
  re-negotiation with arxi, not a protocol invented here.
