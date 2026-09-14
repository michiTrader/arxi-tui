# Bind inventory — the vocabulary a scene may address

Status: **draft**. Section 2 (bootstrap set) is **signed** and frozen as part
of ADR-0001 — the interface boundary decision of Phase 0, recorded in
`docs/PLAN.md`. Section 4 (full inventory) is Phase 0.5's real work and is
deliberately *not* guessed here. The owner signs each section; an unsigned
bind cannot appear in a golden scene.

## 1. What a bind is

A bind is a read-only address into host state that a node's `bind`/`when`
field names. Three namespaces, three authorities:

- **`chat.*`, `agent.*`, `run.*`, `usage.*`, `session.*`, `team.*`, `model.*`
  — run state.** Its truth is the arxi core's event log (`spec/events.md`,
  `host/v1`); a bind here is a *named projection of the fold*, computed by
  the host (Q3). The core defines events, not views — the mapping from event
  to view field is arxi-tui's design, done here and nowhere else.
- **`ui.*`, `slash.*`, `user.input`, the busy line — view state.** The core
  never defined these because it never had a UI. This is arxi-tui's own
  contract (Q21: scenes read `ui.*` with `when`, write only through
  registered commands). Transcribing `events.md` would produce an inventory
  the interface cannot boot from; this half is genuine design work.
- **`<plugin-id>.*` — open namespace.** Anything a gated plugin streams via
  NDJSON lands here. Not inventoried, by construction: the gate is the
  boundary, not the name list.

Rules that apply to every bind, frozen by the scenes exercise:

- An unsatisfied bind renders as a **placeholder, never a crash** — the
  engine contract that makes community preview (Q16) and forward
  compatibility possible at the same time.
- A bind name is stable once signed. Retiring one is a document edit with its
  own golden mutation, never a silent removal.
- Every derived field names its source events, so `file:line:` errors can
  point at *why* a bind is empty ("no `llm.response` yet in this run").

## 2. Bootstrap set (for signature — everything Phase 0 needs)

Scene 1 (RAW) is two nodes, and these are the only fields it and the host's
own survival gestures touch:

| bind | type | source | notes |
|---|---|---|---|
| `chat.history` | markdown text stream | fold of `run.prompt` + `llm.response` text over the run's log | the transcript pane; append-only view, scroll keyed to content, not rows |
| `user.input` | text | the TUI input buffer | *view* state: exists only because there is a keyboard; never written to the core's log until submitted as `run.prompt` |
| `user.input.submitted` | event-ish pulse | enter key | consumed by the host's submit path; listed so the name is reserved |
| `host.escape.armed` | bool | first Ctrl-C | a scene may *show* it, no scene may capture it (invariant 6) |
| `host.scene.error` | text \| null | validator | the last-good-scene notice (invariant 3); null means the active scene validated |

Nothing else. If Phase 0 finds it needs a sixth field, that is a finding about
the bootstrap set's completeness, and this section gets amended and
re-signed. These five binds are the full Phase 0 surface, mapped as follows:

- `chat.history` — the host's only run-state projection in Phase 0. It is a
  view over `llm.response`+`run.prompt` events in the run log, i.e. the
  log-follow path of ADR-0002. The raw scene binds the transcript node to it;
  no other bind is referenced.
- `user.input` — view state, owned by `internal/driver` (the input ring on the
  TUI side of ADR-0001). It is emitted onto the log only when submitted as
  `run.prompt`; until then it never touches the kernel.
- `user.input.submitted` — the enter-key pulse consumed by the host's submit
  path before it builds the `run.prompt` event. Reserved name only.
- `host.escape.armed` — derived from the in-process Ctrl-C count in
  `internal/driver`, not from the core. It is a scene *display* field; the
  panic gesture itself is handled in `cmd/arxi-tui` and is immovable
  regardless (invariant 6).
- `host.scene.error` — set by `internal/engine`'s validator on failure, read
  by the host's boot renderer. It survives a corrupt-on-disk scene via the
  raw-scene fallback (invariant 3), and is the one bind the scene may render
  but the core never provides.

## 3. Derived-field conventions (settled by Q3/Q21, restated as rules)

- Computations live in the host, are *named* here, and are bound by scenes;
  a scene never contains an expression language.
- Counterfields like `usage.in` / `usage.out` / `todos.count` aggregate over
  the fold (`llm.response.tokens_in?`, `agent.todos`) and state their window
  (run? tree? session?) in this document — an unlabeled counter is a lie with
  latency, as `budget.*`'s tree-vs-run footnote in `events.md` already warns.
- `ui.*` is the only writable half (through `cmd:` actions); run-state binds
  are read-only everywhere, scenes included.

## 4. Full inventory — to be designed (Phase 0.5, before Phase 1 goldens)

The known workload, from the binds the eleven golden scenes already use —
each line below still needs: type, exact source events or view mechanism,
update timing, empty-state behavior.

- **From run state** (map onto `events.md` + fold): `agent.working`,
  `agent.mode`, `agent.todos`, `thinking.text`, `model.name`,
  `session.tokens_used`, `session.new_milestone`, `team.members` (per-row
  `state`/`id`), `usage.delta`, `usage.in`, `usage.out`, `todos.count`,
  blocked/remedy surface (`run.quiescent.diagnosis`, `agent.blocked.
  blocked_ref` → a scene should be able to render *why* it is stuck, per
  ADR-0004).
- **From view state** (arxi-tui's own design): `slash.active`,
  `slash.matches` (rows: name, category, description — the `row_template`
  binds of Scene 5), menu selection/focus, `ui.focus`, `ui.max`, `ui.surface`.
- **From the plugin namespace contract**: shape of a `tick.*` stream frame
  (NDJSON record → bind path → type), so Scene 6's manifest validation has
  something to validate against.

Exit criterion: every `bind`/`when` string in the eleven scenes resolves to a
signed row here, with none invented by the engine implementation.
