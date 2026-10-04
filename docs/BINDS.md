# Bind inventory — the vocabulary a scene may address

Status: **signed (2026-09-15)**. Section 2 (bootstrap set) is signed and
frozen as part of ADR-0001 — the interface boundary decision of Phase 0,
recorded in `docs/PLAN.md`. Section 4 (full inventory) is now signed by the
owner and frozen before Phase 1's goldens, because every golden scene binds
against it. The owner of the product signed this section on 2026-09-15. No
unsigned bind may appear in a golden scene.

## 1. What a bind is

A bind is a read-only address into host state that a node's `bind`/`when`
field names. Four namespaces, four authorities:

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
- **`row.*` — relative, template-scoped.** Legal only inside a `row_template`
  subtree and the `on_press` args of nodes in it. `row.<field>` resolves to
  `<field>` of the current element of the array the enclosing `list` binds to;
  the legal field names are that array's element schema (§4.7). Validated at
  load time against the source schema; `{row.<field>}` interpolates the same
  value into an action argument (Q20). This is the one namespace not resolved
  against host state — its authority is the row the template is rendering.

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
  regardless (invariant 6). The first Ctrl-C clears the input line (never the
  transcript) and arms this bit for a 4s window (`driver.ArmTimeout`); the
  sobria scene's `escape_hint` node shows `press ctrl+c again to exit` while it
  is set, and a second Ctrl-C inside the window exits. The window is long
  enough to read the hint, and the host runs an expiry timer so the bit — and
  the hint — clear themselves if no second press follows.
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
| `chat.history` | markdown text stream | fold of `run.prompt.text` + `llm.response.text` + `chat.error.text` (a failed request is a line of the conversation with role `error`, drawn with a `✗ ` marker under the `chat.error` token; `agent.failed.error` lands the same way) | on every `run.prompt`, `llm.response` or `chat.error` | empty string — the prompt line renders alone |
| `thinking.text` | text | `llm.response` payload `text` (accumulated; consecutive `llm.response` events append) | on each `llm.response` while `agent.activated` has not been followed by `agent.turn_done` | empty string — the marquee does not render (`when` is false) |
| `agent.working` | bool | `agent.activated` (→true), `agent.turn_done` (→false), `agent.failed` (→false) | on `agent.activated`/`turn_done`/`failed` | `false` — no member is active |
| `agent.mode` | text | derived: `"live"` or `"sim"` from `run.started.simulated`, plus stage state from `stage.entered`/`stage.advanced` | on `run.started`, `stage.entered`, `stage.advanced` | `"idle"` before `run.started` lands |
| `model.name` | text | `llm.response` payload `model` (format: `provider/model`, e.g. `openai/gpt-4o`) | on every `llm.response` | empty string — no response has landed yet |
| `usage.in` | uint64 | cumulative sum of `llm.response.tokens_in` across all turns in the run | on every `llm.response` | `0` before the first response |
| `usage.out` | uint64 | cumulative sum of `llm.response.tokens_out` across all turns in the run | on every `llm.response` | `0` before the first response |
| `usage.delta` | text | derived from cumulative `usage.in`/`usage.out` (e.g. `"+i25 +o35"`) | on every `llm.response` | empty string — the suffix is omitted |
| `session.tokens_used` | uint64 | derived: `run.started.budget_usd` minus running sum of `llm.response.cost_usd`, reported as used budget in USD × 1000 (microunits) for integer bind compatibility | on `run.started`, every `llm.response`, `budget.warning`, `budget.exceeded` | `0` at run start |
| `session.new_milestone` | event pulse (derived bool) | true while the most recently folded event is `stage.advanced` or `agent.turn_done`; any later event of any kind clears it | on `stage.advanced`, `agent.turn_done` | `false` — no milestone to show |
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
| `ui.max.none` | bool-ish text | `"true"` exactly when no pane is maximized (`ui.max` is empty), `"false"` otherwise; a host-derived inversion of `ui.max`, resolved from it with no fold field of its own. Scene 10 gates the 2×2 grid container on it (`when: ui.max.none`) so the grid shows only while nothing is maximized — the `status.active` idiom, because `when` has no operator to negate `ui.max` | recomputed from `ui.max` on every frame (no independent event) | `"true"` — nothing maximized at boot, so the grid is the default view |
| `ui.max.is.<id>` | bool-ish text | `"true"` exactly when `ui.max` equals `<id>`, `"false"` otherwise; a host-derived **family** (the `ui.plugin.<id>` shape), resolved by comparing the suffix to `ui.max` with no fold field of its own. Scene 10 gates each pane's maximized view on it (`when: ui.max.is.chat`) so exactly one maximized pane draws, chosen by equality against `ui.max` — the comparison `when` cannot express as an operator, signed as a resolved boolean. The suffix is open: any pane id works, so a downloaded dashboard names its own panes without a host change | recomputed from `ui.max` on every frame (no independent event) | `"false"` — no pane matches an empty `ui.max`, so no maximized pane draws at boot |
| `ui.surface` | text | the active surface/page identifier (e.g. `"chat"`, `"config"`, `"plugins"`) | on `cmd:/surface <name>` | `"chat"` — the default surface |
| `ui.hidden` | set of node ids | the ids the user has hidden via `cmd:/ui hide <id>`; `cmd:/ui show <id>` removes one, `cmd:/ui show *` clears the set | on `cmd:/ui hide`/`show` | empty set — every node's visibility is decided by its `when` alone |
| `ui.plugin.<id>` | text | the process lifecycle status of a mounted behavioral plugin, written by the host supervisor: `"starting"` (spawned, pre-handshake), `"live"` (handshake acked, streaming), `"error"` (repeated malformed frames or restart backoff), `"dead"` (process exited, frozen at last values) | on spawn, handshake ack, restart, and death (Block I supervisor) | empty string — no behavioral plugin with that id is mounted, so the bind is falsy for `when` |
| `host.run.actor` | text | the resolved actor blueprint name of the run the TUI is currently following, captured by the host from the `run.start` config it resolved (`resolveRunStartParams`, M1c), not from any arxi-core event; the driver holds it and the loop publishes it into the fold each frame | set on the first `SubmitPrompt` that begins a run (M2), and blanked while the slash menu is open so the status row stays a clean either/or with the menu hint | empty string — no run has been started (boot) or the menu is open, so the `when`-gated actor label does not render |
| `community.query` | text | the substring typed into the community installer's search `input` (Scene 7); the host recomputes `community.matches` from it via `Registry.FilterEntries` on every keystroke, the installer analogue of `slash.typed` over the command registry | on every keystroke while the installer scene is open | empty string — the browse view is open but unfiltered, so every registry entry is listed |
| `community.matches` | array of `{id, name, version, manifest_url, description, preview}` | the registry index entries filtered by `community.query` (case-insensitive substring over name/description, `Registry.FilterEntries`); the installer's `list` binds to it and its `row_template` renders one pressable card per entry, the installer analogue of `slash.matches` | on every keystroke that changes `community.query`, and once when the index is fetched | empty array — no entry matches the query; distinct from a never-fetched index, which the scene reads through a separate liveness gate rather than by collapsing the two |
| `community.selected` | int | the index into `community.matches` of the highlighted card; the list draws that row bright and the markdown preview pane renders its `preview`. The host owns it (↑/↓ while the installer is open and **wraps** at both ends), clamps it to the match list on every filter keystroke, and resets it to 0 when the installer reopens — the installer analogue of `slash.selected` | on ↑/↓ while the installer is open, and on any keystroke that changes `community.query` | `0` — the first match is highlighted |
| `community.selected.name` | text | the `name` of the entry `community.selected` points at, resolved against `community.matches`; the installer's right pane renders it as the preview heading | changes with `community.selected` or with the entry list under it | empty string — the selection is out of range (an empty browse), so the pane heading is blank |
| `community.selected.version` | text | the `version` of the selected entry, rendered under the name in the preview pane | changes with `community.selected` or the entry list | empty string — nothing selected |
| `community.selected.preview` | text | the `preview` blurb of the selected entry (the same markdown a static card inlined), rendered as the body of the preview pane | changes with `community.selected` or the entry list | empty string — nothing selected, so the pane body is blank |
| `config.categories` | array of `{name}` | the /config screen's left-rail groups (Scene 5); the category `list` binds to it and its `row_template` renders one row per group. Host view state, not an arxi-core projection — the host owns the settings model the way it owns the command registry | when the host loads or edits the settings model | empty array — no categories, so the rail draws nothing |
| `config.settings` | array of `{label, kind, enabled, value}` | the /config screen's settings (Scene 5); the settings `list` binds to it and its `row_template` mixes a `switch` and an `input` row by `kind`. `label` is the setting name, `enabled` the toggle state a `switch` reads, `value` the text an `input` reads; `kind` (`"toggle"` \| `"text"`) is the discriminator the engine turns into the §4.7 booleans. Host view state, never a run event | when the host loads or edits the settings model | empty array — no settings, so the list draws nothing |
| `hub.title` | text | the question above the choices on the provider hub (`/provider`, `/models`), composed by the host for the current level ("Choose a provider to add:", "Models of groq:", "Choose the chat model:") | on every level change of the hub | empty string — no title |
| `hub.rows` | array of `{label, status, selected}` | the hub's visible choices (Scene 12); the `list` binds to it and its `row_template` draws an arrow gutter plus label (`row.line`) and a status column (`row.status`). The host composes every row, **including a form's API-key row, which it publishes already masked** — the key itself never enters `fold.State`. The host windows a long list around the highlight, so the engine never scrolls it | on every key the hub handles and on every core answer | empty list — nothing to choose |
| `hub.hint` | text | the key legend of the current hub level (e.g. `"type to filter · ↑↓ move · enter select · esc back"`) | on every level change | empty string — no legend |
| `hub.detail` | text (markdown) | the explanation shown where the chat sits while the hub is open: what the current level does, the selected provider's URL and key state, or what a form field expects | on every key the hub handles and on every core answer | empty string — nothing to explain |

**The `community.*` installer view state (signed 2026-09-28, J3 follow-up; argued in `docs/DESIGN-BLOCK-J.md` J3).** These three rows are the live half of the community installer (Scene 7): `Registry.InstallerScene` (J3, PR #103) currently bakes the entries as static cards because the live `list`/search-input pair needs exactly this signed vocabulary. They are host view state in the `slash.*` mould — a `query`, its filtered `matches`, and a `selected` cursor — written by the installer's own keystroke loop, never by an arxi-core event, so a stranger's registry can never author them. `community.matches` is the previewed plugin's own entries; it is not the `<plugin-id>.*` preview namespace (J1, §4.4), which carries a *previewed manifest's* mocked binds, not the browse list. The empty-state of each is a no-op — an empty query lists everything, an empty match array draws no cards, a zero cursor highlights the first — so signing these rows moves no golden until the installer loop populates them (the deferred live half, §4.6).

**The `community.selected.*` scalar projection (signed 2026-09-28, J3 follow-up).** `community.selected` is an *index*; the preview pane needs the selected entry's *fields*, and a pane outside the `list` has no `row.*` scope to read them through. So the three `community.selected.<field>` rows resolve the index against `community.matches` to the selected entry and expose its `name`, `version` and `preview` as absolute binds — the same entry fields the `row.*` schema (§4.7) exposes per row, addressed by the selection instead. They are signed as the pane consumes them, not as a whole namespace ahead of a consumer: `id`, `manifest_url` and `description` are carried by the match and resolvable the same way, but nothing renders them yet, so signing them now would be vocabulary with no witness (the §4.6 rule, applied in the small). Their empty-state is the selection being out of range — an empty browse, or a frame before the host's first clamp — which yields `""` for every field and collapses the pane to blank, the same no-op an empty `community.matches` gives the list.

**Consumption of `ui.hidden` (signed 2026-09-22, D3).** Unlike every other row
in this table, `ui.hidden` is consumed by the **engine walk**, not by a scene
`when`: the walk drops any node whose id is in the set, together with its
subtree. A node renders iff its `when` is truthy **and** its id is not in
`ui.hidden`. The two compose by conjunction and order does not matter — either
one removes the node.

It is a *set of ids*, not a scalar bool, and not consumed through `when`, for
reasons that were paid for and must not be re-litigated (`docs/DESIGN-BLOCK-D.md`
D3, and `TestHideAndShowAreRefusedRatherThanInventingABind`):

- A **scalar** `ui.hidden` would make `/ui hide a` unhide `b`, because every
  other `ui.*` row is a single id.
- A `when`-based hide cannot be written: `when` shows a node when its bind is
  *truthy* and this engine has **no negation** (`evalWhen` resolves the whole
  string through `resolveBind`), so "show when not hidden" is unspellable.
- The inverse "visible-set" (default-visible) would invert §4.6's signed rule
  that an unresolved bind is *falsy* — a fresh document would render with
  everything hidden.

The empty set is the default and a no-op, so signing this row moves no existing
golden. `ui.hidden.<id>` MAY later be exposed as a derived truthy membership
bind (for a "N hidden" badge); that is secondary and not signed here.

**The `ui.plugin.<id>` liveness bind (I1, signed 2026-09-26; argued in
`docs/DESIGN-BLOCK-I.md` §I-G, ADR-0007).** It is host view state, not a plugin
bind, and the distinction is load-bearing: a behavioral plugin writes only
**relative** fields into its own `<plugin-id>.*` namespace over the wire (it
never utters a prefix, §I-C), so it can neither write nor forge a `ui.*` field.
The supervisor owns this bind — the wire's counter-field rule keeps a scene
alive when a plugin is silent (an unsatisfied `<plugin-id>.*` bind is a
placeholder, §4.4), but that rule cannot tell "no value yet" apart from "the
process died and these values are stale." The liveness bind is the diagnosis
the placeholder cannot carry: a footer or overlay reads `when: ui.plugin.tick`
to show a plugin is degraded, exactly the "quiescence is an event with a
diagnosis" lesson the supervisor is ported to honour. It lives in `ui.*` rather
than the plugin namespace precisely so a dying plugin cannot suppress the report
of its own death. The empty-string default is falsy, so signing this row moves
no golden until a behavioral plugin is actually mounted (Block I), and no
declarative plugin (Block H) ever sets it — a stream-less plugin has no process
to be live.

**The `host.run.actor` label (signed 2026-09-29, M2 follow-up).** It is host
view state, not a run-state projection, and the distinction is exactly the one
that decides where the value comes from. The actor blueprint of a run is the
one field the host chooses rather than reads: `run.start` takes it as a
parameter (`resolveRunStartParams` resolves it from `ARXI_ACTOR` or the
`defaultActor`, M1c), so the host already knows the actor at the moment it
begins a run, one wire round-trip before any `run.started` event could echo it
back. Binding the status label to that resolved value rather than to an event
means the actor shows the instant the run is requested, and it does not depend
on the core emitting an `actor` field the fold would otherwise have to project.
The driver captures it on the `SubmitPrompt` that starts the run (it is held on
`serveDriver.actorLabel`) and the loop publishes it into the fold each frame,
the same host-owned re-attachment `user.input`, `ui.focus` and `host.scene.error`
get. It is blanked while the slash menu is open so the bottom bar stays the
either/or the `status.active`/`slash.hint` pair already enforces — the actor is
one more `when: status.active`-class field, but gated on its own presence so it
also stays absent at boot. The empty-string default is falsy, so signing this
row and adding its `when`-gated node to a shipped scene moves no default golden:
the golden folds start no run, so the label is empty and its node does not draw.


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

**Declared-vs-used rule (H1, signed 2026-09-23; argued in
`docs/DESIGN-BLOCK-H.md`). Implemented H5 (2026-09-26,
`internal/scene/validate.go` `ValidateWithPlugin`/`validatePluginBind`, wired by
`internal/ext/manifest.go` `pluginScope`).** This is the concrete scope rule H5
implements, and it is the plugin-namespace analogue of §4.7's `row.*` rule — the
manifest's `binds` map is a plugin's schema the same way a source list's
`RowSchema` is a template's schema:

- A `<plugin-id>.<field>` bind resolves **iff** it appears inside a fragment
  mounted by a plugin whose `id` is `<plugin-id>` **and** `<field>` is a key of
  that manifest's `binds` map. The map is consulted directly as the single
  source — never copied into a second inventory (the warning on
  `SignedBinds()`, `internal/scene/validate.go`).
- A `<plugin-id>.*` bind **outside** any mount of that plugin is refused with
  `file:line` — the namespace does not leak into the host scene, exactly as a
  `row.*` bind outside a `row_template` is refused.
- A declared bind used under the **wrong node type** for its `binds[...].kind`
  is refused with `file:line` naming the field, its declared kind, and the node:
  a `kind:"text"` bind used as `sparkline bind:"<plugin-id>.field"` is rejected,
  the `kind` being the plugin-bind analogue of the axis a `scroll`/`reveal` prop
  rides. The initial `kind` set is closed at `text` and `series` (Scene 6's two
  uses), widened per node as a signed change.
- A **declarative** manifest (no `executable`, hence no `binds`) that mounts a
  fragment using any `<plugin-id>.*` bind is refused: it streams nothing, so its
  namespace is empty and the use is undeclared. This is the load-time face of
  the declarative/behavioral split (ADR-0006) — a zero-code plugin binds only
  host fields.

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

Three signed binds are deliberately not projected, and the reason is recorded in
`acceptedUnprojectedBinds` rather than left to be rediscovered:
`team.members` (needs per-row templates over the still-unsigned `row.*`
namespace, the same blocker as `row_template`), `agent.blocked.blocked_ref`
(its projection is the command-resolution rule described above, not a value to
print), and `user.input.submitted` (signed only to reserve the name, as §4.3
states).

`session.new_milestone` was a fourth for the same reason — a pulse whose
lifetime was left undecided until a scene needed it — and it has now retired:
Scene 11 decided the lifetime as the narrowest honest one. The pulse reads true
only while a milestone event (`stage.advanced` or `agent.turn_done`) is the LAST
event folded, and any later event of any kind clears it, so it flashes at the
transition and is gone the moment the run does anything else. That keeps the
fold pure — a duration would need a clock the fold must not hold (invariant 1),
and "until dismissed" would need host view-state the fold does not own. The fold
field `State.NewMilestone` (derived by `deriveNewMilestone`) now exists and
`resolveBind` returns its truthiness, so a `when: session.new_milestone` gate
draws its node exactly at the milestone.

The three `community.*` installer binds (§4.3) were signed-but-not-projected for
a fifth reason, recorded the same way and now retired: the vocabulary was
committed on paper first so the `InstallerScene` builder and the validator agreed
on it before any projection depended on it. The fold fields now exist —
`State.CommunityQuery`, `State.CommunityMatches` (a `[]CommunityMatch` whose
row schema §4.7 signs) and `State.CommunitySelected` — so `resolveBind` projects
`community.query`/`community.selected` and `rowScopesFor` instantiates the
`row_template` over `community.matches`, exactly as `slash.*` and `team.members`
are drawn. They are therefore removed from `acceptedUnprojectedBinds` and
`pulseBindsWithoutFoldFields`, the way `ui.hidden`'s entries were once F3 gave it
`State.UIHidden`; keeping them would be the stale exemption those maps' own
guards refuse. The empty-state of each is still a no-op — an empty query lists
everything, an empty match array draws no cards, a zero cursor highlights the
first — so the projection moves no golden until the installer keystroke loop
populates the fields (the remaining live half: the loop that writes them via
`Registry.FilterEntries`, and the selection→preview pane, `DESIGN-BLOCK-J.md`
J3 follow-up).

**An unresolved bind is falsy.** It still *displays* as `"[…]"` so a scene from
a newer build draws rather than crashes (ADR-0003), but display and visibility
are different questions. Until this was fixed, `when` treated the placeholder
as an ordinary non-empty string, so every unresolved gate rendered its node
**on** — inverting `ui.max`'s signed empty-state exactly, and Scene 10 gates
each pane on it. A dropped value degrades toward the empty state; a gate that
fails open degrades toward chrome the user cannot dismiss.

## 4.7 Relative row schemas (the `row.*` namespace)

Signed 2026-09-22 (D1, `docs/DESIGN-BLOCK-D.md`). A `row.*` bind is relative to
the current element of the array its enclosing `list` binds to. It is resolved
against that element, not against host state — the one namespace §1 marks as
having the row for its authority. The `row_template` is instantiated once per
element; inside a given instance, `row.<field>` reads that element's `<field>`.

The **legal field names** for a template are the element schema of the list's
own `bind`, copied here from §4.1 so the two never drift. A `row.<field>` naming
a field the source schema does not declare is a load-time refusal, exactly as a
misspelled absolute bind is.

| list `bind` | element schema — the `row.<field>` names it may address |
|---|---|
| `team.members` | `row.id`, `row.state`, `row.role`, `row.busy`, `row.turns`, `row.spent_usd` |
| `agent.todos` | `row.task`, `row.blocked_on`, `row.actor` |
| `slash.matches` | `row.name`, `row.category`, `row.description` |
| `community.matches` | `row.id`, `row.name`, `row.version`, `row.manifest_url`, `row.description`, `row.preview`, `row.selected` |
| `config.categories` | `row.name` |
| `config.settings` | `row.label`, `row.enabled`, `row.value`, `row.is_toggle`, `row.is_text` |
| `hub.rows` | `row.line`, `row.status` |

`community.matches` carries one row field, **`row.selected`**, that is not an
element column: it is a boolean the engine synthesizes per row from
`community.selected`, true on the row whose index equals the cursor. It is signed
here rather than in the `{id, name, …}` element schema (§4.1) because it is not
data the host fetched — it is the highlight answer. This engine's `when` is a
bare truthiness test with no comparison operator, so a scene cannot write
`row.index == community.selected` itself; the projection answers that comparison
once per row and the `row_template` gates its highlight on `when: "row.selected"`,
the same shape `team.members` uses for `when: "row.busy"`. A `row.index` integer
would be dead without an operator to compare it, so the boolean is the honest
projection of "is this the selected row", not a convenience over one.

Interpolation (Q20): in an `on_press` argument, `{row.<field>}` is replaced by
the element's field. Scene 9's row is `on_press: "cmd:/agent {row.id}"`. The
SCENES.md prose spelling `{m.id}` (line 200) is illustrative; the signed
vocabulary has **no per-list alias** — one relative namespace keeps every
template in the ecosystem uniform, the arxi-sim lesson that a shared closed
vocabulary is what lets one tool read another's document.

Refusals, each addressed with `file:line`:

- `row.<field>` where `<field>` is not in the source array's element schema:
  refusal naming the field, the node, and the source `bind` whose schema was
  checked (the misspelling net absolute binds already get).
- `row.*` outside any `row_template`: refusal naming the node and stating that
  relative binds are template-only.
- A `list` with a `row_template` whose own `bind` is a scalar, not an
  array-of-objects: refusal, because a scalar has no rows to instantiate over.

`config.settings` carries **two** such synthesized fields, `row.is_toggle` and
`row.is_text`, and they are the reason Scene 5 needs this idiom at all. The
settings list mixes a `switch` row and an `input` row in one `row_template`, and
which one a given row draws depends on the setting's `kind`. This engine's `when`
is a bare truthiness test with no comparison operator, so the template cannot
write `when: "row.kind == toggle"`; the projection answers that comparison once
per row and exposes it as the two mutually exclusive booleans, and the template
gates its `switch` on `when: "row.is_toggle"` and its `input` on
`when: "row.is_text"` — the same shape `community.matches` uses for
`when: "row.selected"`. `kind` is the source column and is deliberately **not**
signed as a `row.*` field: a bare string would be dead here without an operator
to compare it, exactly the argument that keeps `row.index` out of
`community.matches`. `row.enabled` (the `switch`'s state) and `row.value` (the
`input`'s text) are ordinary element columns.

A field declared by the schema but absent or empty in a *particular* element is
falsy and renders as the `"[…]"` placeholder (§4.6) — never a crash. Misspelled
is a load-time refusal; absent-in-one-row is a run-time placeholder.

`hub.rows` carries one synthesized field, **`row.line`**: the selection gutter
plus the row's label. The gutter is `"→ "` on the highlighted row and two spaces
on every other row. It is a value rather than a `when`-gated glyph (the
`community.matches` idiom) because a gated glyph shifts the row's columns by two
cells as the highlight moves, and this list is a table. `row.status` is read
verbatim: the host has already decided what each row says. That includes the one
kind of row whose status is a secret: on a form, the API key field's `status` is
a run of `•` the length of the key, and the key itself is held in the host's own
struct. Putting the real value in `fold.State` and masking it in the engine was
rejected: a masked render of a real value is one `%+v` away from a log line,
whereas a `State` that never held the key cannot leak it.

## 4.8 The `on_press` action namespace (Q18, signed 2026-09-26, H8)

`on_press` is universal (SCENES.md §Scene 8, Q18) — any node may carry it — and
its value is a single string naming the action a press dispatches. Q18's
decision is that **the action vocabulary is closed per surface and extended only
through registered names**, so `on_press` is not free text: it is a `prefix:arg`
pair whose prefix is drawn from a closed set, and a prefix outside that set is a
load-time refusal, not a silent no-op. This is the same net every other field in
this document gets, and it is signed here because H8 is the first beat that reads
`on_press` rather than refusing it wholesale.

The closed prefix set, and what each one does when pressed:

| prefix | argument | effect |
|---|---|---|
| `cmd:` | a command line, e.g. `cmd:/agent 5`, `cmd:/max chat` | run the command exactly as if the user had typed it into the input and pressed Enter. The `cmd:/slash`, `cmd:/max`, `cmd:/focus`, `cmd:/ui …` (incl. `plugin`) forms §4.3 names are the registered commands; a `cmd:` naming an unknown command is handled by the same command surface a typed one is (it is not this field's job to enumerate the command registry — that would be a second copy of it). |
| `focus:` | a node `id`, e.g. `focus:reject` | set `ui.focus` (§4.3) to that id. It is the declarative twin of `cmd:/focus <node>`: a button can hand focus to a sibling without the user knowing the command. The id is resolved at press time against the live scene; a `focus:` naming no node in the scene leaves focus unchanged and reports it, never crashes. |
| `answer:` | a kind, e.g. `answer:approve`, `answer:reject` | answer the agent's pending prompt/inbox item with that kind (Scene 8's approve/reject buttons). The **kind vocabulary is itself closed** — `approve`, `reject`, `reply` — mirroring the arxi core's `inbox.approve`/`inbox.reject`/`inbox.reply` verbs, so a scene author and the core agree on what a button means. |

`ext:<plugin-id>:<action>` is a **fourth** prefix routing a press to a
behavioral plugin (a press routed to a subprocess over NDJSON). Its wire is
signed — DESIGN-BLOCK-I §I-E / ADR-0007 fix the `action` frame it produces
(`{"type":"action","id":…,"action":…,"args":…}`, with `{row.field}` values
resolved by the host before they cross the channel) — and it is **dispatched by
I4**: `scene.ParseAction` splits the `<plugin-id>` and `<action>` segments (only
the first colon splits, so a colon inside the action name stays with it) and
checks both are non-empty; the host loop routes the press through the plugin
registry (`internal/ext/supervisor.Registry`), which resolves the live plugin by
id, applies its granted `actions.register` capability, and writes the frame. As
with `focus:`, the mounted/granted/live check is a **runtime** concern: a
well-formed `ext:` validates clean at load (a scene author may write
`ext:tick:refresh` before the plugin is mounted), and a press against an
unmounted, ungranted, or dead plugin is **reported, never crashed** (§I-G). What
remains signed-but-unbuilt is the per-element `{row.field}` argument
substitution — a plain `ext:<id>:<action>` press carries no args today, and the
resolved-args map rides on the same template-row dispatch H8 parked (Scene 9's
`cmd:/agent {row.id}`).


Argument interpolation reuses §4.7 unchanged: inside a `row_template`, an
`on_press` argument may contain `{row.<field>}`, replaced by the current
element's field at press time (Q20). Scene 9's row is `cmd:/agent {row.id}`.
There is no alias vocabulary — one relative namespace, for §4.7's reason.

**Tab order (Q19, signed with H1's scenes).** A press needs a pressable node in
focus. Focus is the single `ui.focus` cursor (§4.3): Tab and Shift-Tab move it
in **scene order** over the nodes that carry an `on_press`, and the input node is
the home of the cursor (focus defaults to it at boot, and Tab returns to it after
the last pressable node), so the typing flow is always one Tab away — Q19's
"protect the typing flow" concern. Enter while a pressable node holds focus
dispatches its `on_press`; Enter while the input holds focus submits the buffer
as it does today. Q19 also signs a `tab: false` opt-out on the input for a
buttons-only surface; that explicit opt-out is **not** implemented by H8 (the
input-as-home ring already protects typing), and is left as a signed-but-unbuilt
refinement so no golden depends on a field the engine does not yet read.

Refusals, each addressed with `file:line`:

- An `on_press` whose prefix is outside the closed set
  (`cmd:`/`focus:`/`answer:`/`ext:`): refusal naming the node and listing the
  legal prefixes.
- An `on_press` with a prefix but an empty argument (`cmd:`, `focus:`, `answer:`
  with nothing after the colon): refusal, because a command/id/kind is the whole
  content of the action.
- An `answer:` whose kind is outside the closed kind set: refusal naming the kind
  and the legal set, the same net a mistyped bind gets.
- An `ext:` missing either segment (`ext:` with no id, `ext::action` with an
  empty id, or `ext:id` / `ext:id:` with no action): refusal, because the
  `<plugin-id>` and `<action>` are both required to build the `action` frame.
  Whether the named plugin is mounted and granted is a runtime report, not a
  load-time refusal.
