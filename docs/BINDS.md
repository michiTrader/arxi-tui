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
| `agent.todos` | array of `{task, blocked_on, actor}` | projected from pending `agent.blocked` events: `task` is the human-readable description, `blocked_on` the reason (`approval`/`lock`/`peer`/`budget`/`timer`/`tool`/`workspace`), `actor` the owning agent | on every `agent.blocked` / `agent.unblocked` | empty array — the Tasks pane renders empty |
| `todos.count` | uint | derived: `len(agent.todos)` — the count of pending todos, for a header badge that must not re-walk the list | on every `agent.blocked` / `agent.unblocked` / `tool.call` | `0` |
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
| `slash.selected` | int | the index into `slash.matches` of the highlighted row; the list renders this row bright and every other row dim. The host owns it (↑/↓ while the menu is open and **wraps** at both ends), clamps it to the match list on every filter keystroke, and resets it to 0 when the menu reopens | on ↑/↓ while `slash.active`, and on any keystroke that changes `slash.typed` | `0` — the first match is highlighted |
| `slash.hint` | text | the footer line shown only while the slash menu is open (e.g. `"↑↓ navigate · enter use · esc close"`); the empty string when the menu is closed, which the scene uses as `when` to gate the row | on any keystroke that changes `slash.active`/`slash.typed` | empty string — no hint |
| `status.active` | bool-ish text | `"true"` while the slash menu is closed so the live status row renders (via `when: status.active`); `"false"` while it is open, which hides the status row so the navigation hint is the single line of info the bottom bar carries | on any keystroke that changes `slash.active` | `"true"` — the status row is visible by default |
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

### 4.6 Signed is not the same as projected

A signed row means the name is committed to and the validator accepts it. It
does **not** by itself mean the engine draws it, and conflating the two cost
this project a real defect: nine binds in §4.1–4.3 were signed, accepted, and
computed by `internal/fold` on every event, while `resolveBind` read none of
them. Each fell through to the `"[…]"` placeholder. A scene author who spelled
one correctly got silence; one who misspelled it got an addressed refusal — so
the refusal was positive evidence the bind was wired, and the correct spelling
was the one with no diagnostic.

Two guards now hold the two halves apart, both in `internal/engine`:

- `TestEverySignedBindIsHandledOrJustified` enumerates this document's
  vocabulary through `scene.SignedBinds()` and requires each name to be handled
  by the engine or justified in writing. A newly signed row that nobody wires
  fails the suite on its own.
- `TestEverySignedScalarBindTheFoldComputesReachesTheFrame` pins each projected
  bind against a distinguishable value in the drawn frame, so a case returning
  the *wrong* fold field is caught too — the structural audit cannot see that,
  because the case label is present.

Four signed binds are deliberately not projected, and the reason is recorded in
`acceptedUnprojectedBinds` rather than left to be rediscovered:
`team.members` (needs per-row templates over the still-unsigned `row.*`
namespace, the same blocker as `row_template`), `agent.blocked.blocked_ref`
(its projection is the command-resolution rule described above, not a value to
print), `session.new_milestone` (a pulse with no fold field and an undecided
lifetime — Scene 11), and `user.input.submitted` (signed only to reserve the
name, as §4.3 states).

**An unresolved bind is falsy.** It still *displays* as `"[…]"` so a scene from
a newer build draws rather than crashes (ADR-0003), but display and visibility
are different questions. Until this was fixed, `when` treated the placeholder
as an ordinary non-empty string, so every unresolved gate rendered its node
**on** — inverting `ui.max`'s signed empty-state exactly, and Scene 10 gates
each pane on it. A dropped value degrades toward the empty state; a gate that
fails open degrades toward chrome the user cannot dismiss.
