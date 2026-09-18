# Bind inventory — the vocabulary a scene may address

Status: **signed (2026-09-15)**. Section 2 (bootstrap set) is signed and
frozen as part of ADR-0001 — the interface boundary decision of Phase 0,
recorded in `docs/PLAN.md`. Section 4 (full inventory) is now signed by the
owner and frozen before Phase 1's goldens, because every golden scene binds
against it. The owner of the product signed this section on 2026-09-15. No
unsigned bind may appear in a golden scene.

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

## 4. Full inventory

Each row records exactly what the engine implementation needs to produce — no
more, no less. The owner of the product signed this section on 2026-09-15.

### 4.1 Run state (mapped from the arxi core event catalog)

These binds are read-only projections of the fold over `spec/events.md`.
Source events come from `internal/kernel/event.go` (the exhaustive `EventType`
const block) and the payload field names are confirmed against the actual
emission sites in `internal/provider/executor.go` and
`internal/app/acceptance.go`. The fold never imports the core; it reads these
events from the log file the core writes.

| bind | type | source events / payload | update timing | empty-state |
|---|---|---|---|---|
| `chat.history` | markdown text stream | fold of `run.prompt.text` + `llm.response.text` | on every `run.prompt` or `llm.response` | empty string — the prompt line renders alone |
| `thinking.text` | text | `llm.response` payload `text` (accumulated; consecutive `llm.response` events append) | on each `llm.response` while `agent.activated` has not been followed by `agent.turn_done` | empty string — the marquee does not render (`when` is false) |
| `agent.working` | bool | `agent.activated` (→true), `agent.turn_done` (→false), `agent.failed` (→false) | on `agent.activated`/`turn_done`/`failed` | `false` — no member is active |
| `agent.mode` | text | derived: `"live"` or `"sim"` from `run.started.simulated`, plus stage state from `stage.entered`/`stage.advanced` | on `run.started`, `stage.entered`, `stage.advanced` | `"idle"` before `run.started` lands |
| `model.name` | text | `llm.response` payload `model` (format: `provider/model`, e.g. `openai/gpt-4o`) | on every `llm.response` | empty string — no response has landed yet |
| `usage.in` | uint64 | cumulative sum of `llm.response.tokens_in` across all turns in the run | on every `llm.response` | `0` before the first response |
| `usage.out` | uint64 | cumulative sum of `llm.response.tokens_out` across all turns in the run | on every `llm.response` | `0` before the first response |
| `usage.delta` | text | derived from cumulative `usage.in`/`usage.out` (e.g. `"+i25 +o35"`) | on every `llm.response` | empty string — the suffix is omitted |
| `session.tokens_used` | uint64 | derived: `run.started.budget_usd` minus running sum of `llm.response.cost_usd`, reported as used budget in USD × 1000 (microunits) for integer bind compatibility | on `run.started`, every `llm.response`, `budget.warning`, `budget.exceeded` | `0` at run start |
| `session.new_milestone` | event pulse | derived: fires on `stage.advanced` (stage transition) or `agent.turn_done` (turn boundary) | on `stage.advanced`, `agent.turn_done` | null/pulse inactive — no milestone to show |
| `team.members` | array of objects | projected from the run's members: each row has `id` (the agent name), `state` (`idle`/`thinking`/`tool`/`submitted`/`waiting`/`inactive`/`failed`), `role` (`backend`/`frontend`/`...` from `agent.activated`), `busy` (bool), `turns` (uint), `spent_usd` (float64) | on `agent.activated`, `agent.turn_done`, `agent.blocked`, `agent.unblocked`, `agent.failed` | empty array — the subagent list does not render |
| `todos.count` | uint | derived: count of pending `agent.blocked` events with `blocked_ref` present | on every `agent.blocked` / `agent.unblocked` / `tool.call` | `0` |
| `run.quiescent.diagnosis` | text | `run.quiescent` payload `diagnosis` (the concrete reason: "stage X advances with …", "agent waits for …", etc.) | on `run.quiescent` | null — no diagnosis until a quiescent event lands |

### 4.2 Agent blocked / remedy surface

Per `docs/LESSONS.md:91-94`: quiescence is an event with a diagnosis, not a
terminal state. The fold must be able to render *why* it is stuck. The
`agent.blocked` event carries `blocked_ref` (a structured object), and the
remedy is derived from it.

| bind | type | source events / payload | update timing | empty-state |
|---|---|---|---|---|
| `agent.blocked.blocked_ref` | object \| null | `agent.blocked` payload `blocked_ref` | on `agent.blocked` | null — no member is blocked |
| `agent.blocked.blocked_on` | text | `agent.blocked` payload `blocked_on` (one of: `approval`, `lock`, `peer`, `budget`, `timer`, `tool`, `workspace`) | on `agent.blocked` | empty string |
| `agent.blocked.actor` | text | `agent.blocked` payload `actor` (derived from `agent.activated` or the event's own `actor` field) | on `agent.blocked` | empty string |

The remedy is not a separate bind: the engine reads `blocked_on` +
`blocked_ref` and resolves it to a command string per the `blocked_ref` rule in
`spec/events.md` (`approval` → `arxi inbox approve <inbox_id>`, `budget` →
`arxi run unpause --budget <higher>`, etc.). The scene renders
`agent.blocked.blocked_ref`'s fields through relative binds in a template row.

### 4.3 View state (arxi-tui's own contract, Q21)

These binds have no corresponding event in the arxi core — they exist only
because this project has a UI. They are owned by the host, read with `when` and
written only through registered commands (`cmd:/slash`, `cmd:/max`,
`cmd:/focus`, `cmd:/plugin`). The event that sets each is named.

| bind | type | view mechanism | update timing | empty-state |
|---|---|---|---|---|
| `slash.active` | bool | set true when the user types `/` in the input buffer; set false on Enter/Esc or when the buffer clears | on `user.input` change crossing the `/` threshold | `false` |
| `slash.typed` | text | the substring typed after `/` in the input buffer | on every keystroke while `slash.active` | empty string |
| `slash.matches` | array of `{name, category, description}` | derived from the host's command registry (`fold.Commands`), filtered by `slash.typed` via case-insensitive substring match | on every keystroke while `slash.active` | empty array — no commands match |
| `slash.selected` | int | the index into `slash.matches` of the highlighted row; the list renders this row bright and every other row dim. The host owns it (↑/↓ while the menu is open), clamps it to the match list on every filter keystroke, and resets it to 0 when the menu reopens | on ↑/↓ while `slash.active`, and on any keystroke that changes `slash.typed` | `0` — the first match is highlighted |
| `ui.focus` | text \| null | the `id` of the currently focused node; set by `cmd:/focus <node>` or Tab navigation | on `focus:<node>` action, on Tab/Shift-Tab | null — focus defaults to the input node at boot |
| `ui.max` | text \| null | the `id` of the maximized pane; set by `cmd:/max <pane>` (Scene 10) | on `cmd:/max` action | null — no pane is maximized |
| `ui.surface` | text | the active surface/page identifier (e.g. `"chat"`, `"config"`, `"plugins"`) | on `cmd:/surface <name>` | `"chat"` — the default surface |

### 4.4 Plugin namespace contract

The `<plugin-id>.*` namespace is open by design (ADR-0003). Any field a gated
plugin streams via NDJSON lands here. This row documents the *shape* the engine
expects so Scene 6's manifest validation has a contract to validate against.

| bind | type | source | update timing | empty-state |
|---|---|---|---|---|
| `<plugin-id>.*` | open | NDJSON frame from a gated plugin process | on each frame | placeholder — an unsatisfied plugin bind renders as a placeholder, never a crash |

A plugin manifest declares `mounts` (scene fragments) and the bind fields it
streams. The engine validates that every bind in a plugin's fragment resolves
either to a host-owned field (Section 4.1–4.3) or to a field the plugin declares
it will stream. An undeclared plugin bind is a validation error with `file:line`.

### 4.5 Exit criterion

Every `bind`/`when` string in the eleven scenes resolves to a signed row above.
No bind is invented by the engine implementation. A scene that references an
unsigned bind fails validation at load time with a `file:line` error pointing at
the offending node.
