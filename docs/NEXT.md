# NEXT — verified state and the atomic plan

This document is the ordered, dependency-aware task plan for the phases still
open. It was written after a green baseline and a three-track code audit, not
from the prose in the other docs — several claims in `README.md`,
`docs/PLAN.md` and `internal/fold/fold.go` were found **stale**, and the
corrections are recorded below so the plan targets the real tree, not the
remembered one.

Every claim here cites `file:line`. Where a number differs from what an older
doc asserts, the measured number wins and the stale source is named so it can
be fixed.

## Baseline (measured 2026-09-22)

- `go build ./cmd/...` — exit 0.
- `go vet ./...` — exit 0.
- `gofmt -l .` — no files listed.
- `go test -count=1 ./...` — all packages `ok` (engine 19s, scene 36s, the
  rest under 6s).
- `git status` clean apart from untracked `bin/`; `git log origin/master..HEAD`
  empty; `git log HEAD..origin/master` empty after `git fetch`.

The shell and workspace path are fixed: the earlier `spawn bash.exe ENOENT`
was the `arxi_tui` (underscore) vs `arxi-tui` (hyphen) directory mismatch, now
resolved. The whole "lost code" scare was that path confusion; nothing was
lost.

## Corrections to prior claims (found by the audit)

These are the doc-vs-code contradictions the audit surfaced. Fixing the docs
is cheap and belongs at the front of the relevant block; each is a task below.

1. **Fold coverage is 120/122, not 113/122.** The pinning test
   `TestTheRealLogCoverageIsMeasuredNotAssumed`
   (`internal/driver/real_run_log_folds_test.go:232`, asserting `known == 120`)
   already counts `stage.*` as handled (`internal/fold/fold.go:499-502`, switch
   cases at `fold.go:928,949,990,1012`). Only `timer.scheduled` and
   `timer.cancelled` remain unhandled (`real_run_log_folds_test.go:295-296`).
   The comment block at `internal/fold/fold.go:439-451` still says
   "113/122 (92.6%)" and lists `stage.*` as invisible — **that comment is
   stale** and must be corrected. This collapses old Block C almost entirely.

2. **`agent.mode` and `session.new_milestone` are NOT blocked.** They depend on
   `stage.*` (`docs/BINDS.md:104,110`), and `stage.*` is now folded, so the
   dependency is already satisfied.

3. **OSC 11 background detection does not exist — RESOLVED by correcting the
   claim (L4 done).** `internal/theme/theme.go:148` explicitly disclaims it
   ("adapts ... without OSC 11 queries"); `internal/term/decode.go:127` only
   generically skips OSC replies; `factory.go` already described the shipped
   mechanism (fixed with B4). The remaining stale claims —
   `README.md:44`, `cmd/arxi-tui/main.go:135`, `docs/TOKENS.md`, `docs/SCENES.md`
   and `docs/PLAN.md` — were corrected to describe the relative dim/bright SGR
   mechanism that actually ships (the terminal resolves the attributes; there is
   no query). Implementing an explicit OSC 11 query remains an *optional* future
   enhancement, not foreclosed, and is noted at each site.

4. **No CI existed — RESOLVED (L5 done).** `.github/workflows/ci.yml` now runs
   `go build`/`vet`/`gofmt`/`go test -count=1 ./...` on a `windows-latest` +
   `ubuntu-latest` matrix (`fail-fast: false`). The double-Ctrl-C escape-hatch
   tests (`cmd/arxi-tui/loop_test.go`: `TestLoopExitsOnCtrlCImmediate`,
   `TestLoopFirstCtrlCClearsInput`) run inside `go test ./...`, so AGENTS.md's
   "runs on Windows CI from Phase 0" obligation is now backed by a pipeline.

5. **`row_template` is refused in `internal/scene`, not `internal/engine`.**
   The refusal lives at `internal/scene/validate.go:241` (message names
   SCENES.md Q10 / Scene 5 and the unsigned `row.*` namespace). The field
   exists only to be refused (`internal/scene/node.go:36,49`).

6. **`scroll` is a declared-but-refused field** (`internal/scene/node.go:69`,
   refusal `validate.go:245`); `transition`, `reveal`, `enter`, `stagger` are
   **not even struct fields** (`node.go:82-86`) — they need the `[anim]` timing
   token that `docs/TOKENS.md` does not define.

7. **`bin/arxi-tui` is an untracked 4.8 MB compiled binary.** `.gitignore`
   covers `/arxi-tui` and `/arxi-tui.exe` but not `bin/arxi-tui`. It should be
   ignored, never committed.

## The plan — atomic tasks, ordered by dependency

Each task is an independently executable unit. A hard dependency is noted in
`[]`. "Sign" means the SCENES/BINDS/TOKENS method: design on paper, add the
signed row/token to the relevant doc, before any code depends on it.

### Block 0 — Housekeeping (done except one)

- **0.1 — DONE.** Workspace path fixed (`arxi-tui`, hyphen); shell works.
- **0.2 — DONE.** Green baseline captured (see above).
- **0.3** Add `bin/` (or `/bin/arxi-tui`) to `.gitignore` so the compiled
  binary is never accidentally committed (correction 7).

### Block C — Finish fold coverage (DONE 2026-09-22)

Old Block C assumed 9 missing events; the audit showed 7 already handled, and
this session closed the last 2.

- **C1 — DONE.** `stage.*` (7 events) is folded
  (`internal/fold/fold.go` handled map + switch cases).
- **C2 — DONE.** `timer.scheduled` and `timer.cancelled` are handled as a
  read-and-understood no-op (a stage deadline that arms and cancels without
  firing has no host-facing projection).
- **C3 — DONE.** The pin in `TestTheRealLogCoverageIsMeasuredNotAssumed` is
  122; the two events moved from the blind list to the PRESENT assertion, and
  the now-empty blind list has a fail-loud check for any future unaccounted
  type. Counterfactual run: reverting the handlers fails the guard in four
  places.
- **C4 — DONE.** The stale 113/122 comment block in `internal/fold/fold.go` is
  corrected to 122/122.
- **C5 — DONE.** The README coverage line reads 122 of 122.

### Block A — Close Phase 2: run the repair loop against a real model (DONE 2026-09-22, except A4)

The corpus had never run against a live model. It has now:
`deepseek-v4.1-flash` on an OpenAI-compatible endpoint, three runs, **11/12
case-runs converged**.

- **A1 — DONE.** Working endpoint obtained (vyceai.com, OpenAI-compatible). The
  earlier gateways were blocked: genspark `free_plan_block`, AgentRouter 401
  `unauthorized_client`.
- **A2 — DONE.** Direct probe returned a real completion (HTTP 200, content
  `ok`), not a plan block.
- **A3 — DONE.** `arxi-eval -model deepseek-v4.1-flash -v` run three times;
  outcomes and turns captured (see `docs/EVAL.md` "First live run").
- **A4 — IN PROGRESS.** Corpus widened from 4 to 5 cases:
  `raw-model-name-while-working` covers the when-condition arm of the bind
  validator (`validate.go` "unsigned bind … in when condition"), which no other
  case pinned. All four corpus invariants hold and it was verified live. Note:
  it converges in one turn against this model (validator coverage, not a
  repair-loop case, and its rationale says so). Further cases that reliably
  exercise the *repair* loop against a given model remain valuable but require
  per-model engineering; widening across more scenes needs those goldens frozen
  first (later phases). This is the work that moves the claim from "reliable for
  this model" to "reliable".
- **A5 — DONE.** Finding written into `docs/EVAL.md` (First live run) and
  `docs/PLAN.md` (Phase 2 measured result). The gating question is answered:
  the repair loop works against a real model.
- **A6 — DONE.** Ship decision recorded in `docs/PLAN.md`: **ship**
  agent-command-driven `/ui` self-extension behind the diff + consent gate,
  with the one-model/one-provider/four-case caveat written down.

### Block M — arxi core integration (cross-repo, high value)

`SubmitPrompt` sends `run.prompt` (`internal/driver/ndjson.go:307`), which the
core reports as not in `implemented` (`testdata/serve/session.ndjson:1`);
`run.start` is implemented by the core but never sent by the client.

**Correction (2026-09-22): M1 is a design change, not a rename.** Investigating
before implementing showed `run.prompt` and `run.start` are semantically
different verbs, so the client cannot simply swap the type string:

- `run.prompt` sends text to an **already-running** run. Its params are
  `if_seq, on_busy, run, text, to` (named verbatim by the core's `bad_params`
  refusal at `testdata/serve/session.ndjson:3`). The current client models
  exactly this: `serveDriver.runID()` derives the run id from the log
  directory (`cmd/arxi-tui/main.go:356`) and `SubmitPrompt` posts
  `{run, text}` to it (`ndjson.go:307`). The run is started **outside** the
  TUI (e.g. `arxi run start …`), and the TUI attaches to its log.
- `run.start` **creates** a run. Its CLI form is
  `arxi run start "<prompt>" --actor <blueprint> --budget <n> --sim`
  (`real_run_log_folds_test.go:20`), so it needs an actor blueprint and a
  budget, not just text — and its JSON param names appear in **no fixture** in
  this repo.

Consequences for the plan:

- **M1a — DONE (2026-09-26, schema read from the arxi source, not guessed).**
  The `run.start` request/response schema is now captured from the kernel source
  and cross-checked against the captured hello. Wire params (the surface
  normalises `-` to `_`, `arxi/internal/surface/surface.go:768-773`):
  `actor` (string, required/positional), `prompt` (string, required/positional),
  `budget` (number, **required, no default**), `max_turns` (number, optional,
  default 0), `workspace` (string, optional, default `auto`, enum
  `auto|shared|worktree|copy|none`), `model` (string, optional), `sim` (bool,
  optional). The server dispatch reads exactly `actor, prompt, budget, max_turns,
  sim, model` (`arxi/cmd/arxi/serve.go:186-196`) and maps them onto
  `hostv1.SubmitRequest` (`arxi/host/v1/types.go:88-102`) — note wire `budget` →
  `BudgetUSD`, wire `sim` → `Simulated`, and `actor` is resolved through the
  agent store / a blueprint file (`resolveActor`), **not** inline blueprint text.
  The response `result` is `hostv1.SubmitResult` (`arxi/host/v1/types.go:106-110`):
  `job_id` (the run id every later verb uses), `accepted_seq`, `status` (enum
  `queued|running|blocked|paused|succeeded|failed|cancelled|expired|unknown`).
  Pinned by `arxi/cmd/arxi/serve_lifecycle_test.go:96-106` and the budget refusal
  `serve_test.go:282`. No live `schema` query was needed; the wire schema is
  fully determined by the source.
- **M1b — RESOLVED, and the prior hypothesis was WRONG (2026-09-26).** The note
  below (old M1b) assumed `run.steer` was implemented "(in the hello)" and that
  the migration was `run.start` for turn one + `run.steer` for the rest. **That
  premise is false against both the current arxi source and the captured hello.**
  In this build BOTH `run.steer` AND `run.prompt` are declared-but-`not_implemented`:
  `run.steer` has no handler in `lifecycleHandlerDescriptors`, `streamingHandlers`
  or `protoHandlers` (`arxi/cmd/arxi/serve.go:175-281`), no Steer/Inject method on
  the `lifecycleHost` interface (`serve.go:127-137`), and no steer capability
  (`arxi/host/v1/capabilities.go:7-15`); the captured hello's `implemented` list
  (`testdata/serve/session.ndjson:1`) is exactly `blueprint.validate`,
  `inbox.approve`, `inbox.reject`, `inbox.reply`, `run.attach`, `run.cancel`,
  `run.result`, `run.show`, `run.start`, `schema` — steer and prompt are in
  `types` but not `implemented`. The CLI `run steer` exists (`arxi/cmd/arxi/steer.go`)
  but routes through `injectCause`/`applyInjection`, a CLI-only path never wired
  to the protocol server. **There is no protocol verb in this build that sends
  text into an already-running run.** So the achievable session→run mappings are:
  (a) one `run.start` per user turn (each turn its own run, no shared
  conversational state at the protocol layer), or (b) block M1c until the kernel
  gains a `run.steer`/`run.prompt` executor. This is still the user's product
  call, but the deciding fact is now known rather than assumed: "start + steer" is
  not an option on this build. **The gate for M2's live round-trip is to re-read
  the hello's `implemented` list at connect time and gate on it**, since a
  deployed kernel that differs from this source would change the answer, and
  `run.steer`'s absence is the whole crux.
- **M1c** [M1b] Implement `run.start` in `internal/driver/ndjson.go` and the
  `serveDriver` (`cmd/arxi-tui/main.go:364`): send `{actor, prompt, budget, sim}`
  (optionally `max_turns`, `model`), read `result.job_id`, and use it as the run
  id for `run.attach`/`run.show`/`run.result`/`run.cancel`. Do **not** send
  `workspace` unless a real enum value is chosen (the server defaults it to `auto`
  when omitted). Keep the existing `not_implemented`/refusal handling. The three
  values the source cannot supply — which `actor`/blueprint a TUI session uses,
  the per-turn `budget`, and whether `sim` defaults on — are the product decisions
  M1b flags, not wire-schema gaps.
- **M2** [M1c] Verify a real round-trip against a live `arxi serve`, re-reading
  the hello's `implemented` list at connect and gating on it.
- **M3** [M2] Evaluate adopting the `run.attach`/`event.subscribe` path (a
  positive end-of-run marker) — deferred per ADR-0002
  (`docs/PLAN.md:230-280`); the trigger is needing that positive signal.

  Superseded note (kept for the record, per the "record the wrong premise rather
  than delete it" rule): the original M1b read *"the only such verb this build
  implements is `run.steer` (in the hello), because `run.prompt` is
  `not_implemented`. So the real migration is likely `run.start` for turn one +
  `run.steer` for the rest."* Both clauses were wrong — steer is not implemented
  and not in the hello — and the error ran in the flattering direction (it
  described a clean migration that the wire does not support). It was written from
  the prose of the hello's `types` list without checking the `implemented` list
  beside it; the correction above cites the handler tables that decide the
  question.

### Block B — Agent patches + side-by-side diff (A6 said ship behind this gate)

- **B1 — DONE.** Diff model defined in `internal/patch/diff.go`: `Op`,
  `DiffLine` (both 1-based line numbers), `Diff`. Line-level over the canonical
  serialisation, argued in the file comment; node-addressing is a presentation
  the view can layer on, not a different truth.
- **B2 — DONE.** `DiffSource` computes an exact LCS line diff against the
  canonical form of both sides, so reformatting is never reported as a change.
  `Result.Diff` is populated by `apply`. Tests pin both properties;
  counterfactual (canonicalization disabled) fails the reformatting test on
  minified and tab-indented input.
- **B3 — DONE.** ADR-0003 in `docs/PLAN.md`: the diff view is a **host-generated
  scene** (a `row` of two `stack`s of styled `text` nodes in an `overlay`),
  not a new engine capability — the dogfooding choice, needing only three new
  theme tokens (`diff.del`/`diff.add`/`diff.context`) the open token system
  already permits.
- **B4 — DONE.** `Diff.Scene(title)` in `internal/patch/diff.go` authors the
  view as a titled box over a two-column row (old | new) of styled `text`
  nodes. Tokens `diff.context`/`diff.del`/`diff.add` minted in `SOBRIA()` and
  `Factory()`, colourless. Rendered through the normal engine path and pinned
  by a golden (`testdata/DIFF.styled`); a second test holds the scene to both
  validators against both themes and asserts the change lands on the correct
  side. (This subsumes B8's diff-view goldens.)
- **B5** [B4] Wire propose→apply: agent proposes a patch → host shows the diff
  → applied on approval. **Note:** this depends on the agent-proposes-a-patch
  channel, which is the same surface Block M feeds (the agent's turn produces a
  proposed document). B5 is where the diff view meets the live agent, so it is
  gated on M being far enough that an agent turn can carry a patch.
- **B6** [B5] Consent gate per Q23 (show diff + log attribution; no blocking
  per-step menu).
- **B7** [B5] Log attribution: every agent patch is an attributed event in the
  arxi log.
- **B8 — DONE (folded into B4).** The diff-view golden and the failure-message
  tests landed with B4.
- **B9** [B5] Update the verbs advertised in the slash menu
  (`internal/fold/fold.go:1204`, held by
  `internal/patch/menu_agrees_with_the_surface_test.go:40`) and the README
  Status section.

### Block D — Phase 3 design gate (paper; unblocks E/F/G)

Each is an independent design+sign (SCENES/BINDS/TOKENS method). All four were
drafted as reviewable proposals in `docs/DESIGN-BLOCK-D.md` (PR #44) and
**owner-accepted 2026-09-22**; the signed text now lives in the frozen docs. The
code guards that refuse these features stay until each is *implemented* (signing
the design does not lift a refusal — the implementation does, with its own
counterfactual test).

- **D1 — SIGNED** (BINDS.md §1 + §4.7). `row.*` relative bind namespace:
  template-scoped, resolved against the enclosing list's element schema,
  `{row.field}` interpolation (Q10/Q20). Unblocks Block E.
- **D2 — SIGNED** (new `docs/ADDRESSING.md`). The `where` addressing vocabulary
  for `/ui add`/`move` (`above`/`below <id>`, `into <id> [top]`,
  `below_input`/`above_input`), with the id-uniqueness invariant and cycle
  refusal. Write-path, kept out of read-path BINDS.md. Unblocks Block F.
- **D3 — SIGNED** (BINDS.md §4.3). `ui.hidden` as a **set of node ids** consumed
  by the engine walk (a node renders iff `when` truthy AND id ∉ `ui.hidden`);
  the scalar and default-visible spellings are rejected with reasons. Unblocks
  F3.
- **D4 — SIGNED** (TOKENS.md "Timing tokens"). The `[anim]` timing token: an
  `anim` theme section of `{duration_ms, curve, fps}`, a **closed** curve set (a
  curve is code), global default + per-node override (Q8), fold boundary kept
  (Q9). Unblocks Block G.

### Block E — Phase 3: row_template + relative binds [D1]

- **E1 — DONE.** `renderRowTemplate` in `internal/engine` instantiates a list's
  `row_template` once per element of the array its bind names. The row scope
  lives on the Renderer and is propagated into every sub-renderer (row/stack/
  box/overlay build fresh ones), so a `row.*` bind under a container inside a
  template resolves — the gap the per-row `when` counterfactual caught.
- **E2 — DONE.** `resolveBindRow`/`evalWhenRow`/`hiddenByWhenRow` resolve a
  `row.<field>` against the current element; a relative bind with no row in
  scope is the falsy placeholder. `validateBindsScoped` accepts `row.<field>`
  against the source list's schema (`scene.RowSchema`), refuses it elsewhere,
  refuses an unknown field, and refuses a `row_template` over a schema-less
  bind — all with `file:line`.
- **E3 — DONE (rendering).** `team.members` renders through a `row_template`
  (`rowScopesFor`); the fold already projected `State.TeamMembers`. Removed from
  `acceptedUnprojectedBinds`; the composite audit now witnesses it through a
  template (`templateProjectedBinds`).
- **E4 — DONE (2026-09-22).** `testdata/SUBAGENTS.json` freezes Scene 9: a
  `list` over `team.members` whose `row_template` renders one row per member
  (`row.role` per element). Pinned as `SUBAGENTS.frame`/`.styled` with a render
  test folding a busy+idle two-member team. The template is a single `text`
  node — a container reached through `row_template` has no own-style rendering,
  so `TestEveryNestedNodeHonoursItsOwnToken` would read a container template as
  a silent style drop; the spinner-or-glyph + label composition and row-click
  (`on_press`, still refused) wait on that decision / H8. The Scene 9 heading
  was reformatted to `SUBAGENTS (below the input)` so the progress audit's
  fixture↔scene name match resolves.
- **E5 — TODO.** Freeze the Scene 5 golden (CONFIG). Blocked on `switch`/`input`
  rendering inside a template — `switch` is not yet in `renderNode`'s dispatch,
  so the `/config` dogfood needs those primitives first.
- **E6 — DONE.** The `row_template` refusal left `unrenderedFields`; the
  `team.members` entry left `acceptedUnprojectedBinds`; the nested-branch,
  nested-owner, zero-value-key and composite-projection audits were reconciled.

### Block F — Phase 3: /ui add and move verbs [D2]

- **F1 — DONE (2026-09-23).** `/ui add node <where> <fragment>` in
  `internal/patch` (`add.go`): the write-path `where` vocabulary D2 signed
  (`above`/`below <id>`, `into <id> [top]`, the `below_input`/`above_input`
  semantic anchors). Source-to-source over the generic map tree like `set`/
  `style`, so a fragment's undeclared keys survive; the result is re-parsed and
  re-validated (invariant 3). Refusals carry `file:line` for every
  ADDRESSING.md §2 case (unknown id, ambiguous id, `into` a non-container,
  zero/many input anchors, resolved-but-no-sibling-slot) and §3 (a fragment
  reusing an id — the uniqueness clash, attributed to the fragment). `Verbs()`
  and the slash-menu `ui` description now advertise `add`; the menu-agreement
  and verb-round-trip sweeps both cover it. Counterfactuals run for the
  above/below offset, the into-top prepend, and the dup-id refusal.
  Note: the §3 id-uniqueness invariant is enforced *at the add boundary*, not
  yet as a load-time check on every document — that broader guard remains
  available to add when `move` (F2) needs an anchor guaranteed unique before it
  resolves.
- **F2 — DONE (2026-09-23).** `/ui move <id> <where>` in `internal/patch`
  (`move.go`). Reuses F1's write-path `where` resolver — `parseWhere` was
  factored out of `parseAdd` as the single reader of the grammar, so `add` and
  `move` cannot drift on what `top`/`below_input` mean — and F1's
  `insert`/`insertSibling` placement, keeping the edit source-to-source over the
  generic map tree so a moved node's undeclared keys survive. Adds the one
  refusal `add` skips: the **cycle refusal** (ADDRESSING.md §2.4, a node cannot
  become its own descendant), checked before the subject is detached, over the
  subject's whole subtree (prefix/suffix/row_template included), covering the id
  anchors (set membership, incl. `move x into x`) and the semantic anchors (the
  input node living inside the subject). Refusals carry `file:line` for every §2
  case `move` reaches: unknown/ambiguous subject, unknown/ambiguous anchor,
  `into` a non-container, resolved-but-no-sibling-slot, a subject in no children
  list. `Verbs()` and the `ui` slash-menu now advertise `move`; the
  menu-agreement and verb-round-trip sweeps cover it. Counterfactuals run for
  the cycle refusal (disabling it fails exactly the three cycle tests) and the
  detach (an off-by-one that fails to drop the moved node fails the offset
  tests). The §3 id-uniqueness invariant is still enforced at the verb boundary,
  not yet load-time — the broader guard remains F2's natural companion.
- **F3 — DONE (2026-09-23).** `/ui hide <id>` / `/ui show <id>` / `/ui show *`,
  gated on D3's signed `ui.hidden` set (BINDS.md §4.3). Unlike `add`/`move`/
  `set`/`style`, hide/show are *not* source edits: they write host-owned view
  state, so the document bytes are untouched. `fold.State.UIHidden`
  (`map[string]bool`, json `ui.hidden`) is the set, held by the loop across
  frames like the input buffer and re-attached each repaint (no core event
  produces it). The engine walk consumes it in `hiddenByWhenRow` — a node draws
  iff its `when` is truthy AND its id is not a member, composing by conjunction —
  and the membership test lives in that shared predicate rather than at the
  `renderNode` chokepoint, so a node reached through `prefix`/`suffix` (which
  bypass `renderNode`) is covered too. The patch surface returns the set
  mutation in `Result.ViewState` (`hide`/`show`/`show *`), and `uiCommandKey`
  applies it to the loop's set. An unknown id is refused with `file:line` (D3
  gives hide/show D2's id resolution); `show *` clears the set without naming an
  id. `Verbs()` and the `ui` slash-menu advertise the two verbs; the
  menu-agreement and verb-round-trip sweeps cover them. The bind audits move
  `ui.hidden` off the "signed-not-projected" and pulse lists into a new
  `walkConsumedBinds` category — handled by the walk, proven by a behavioural
  drop test (`hiddenFilterVaries`, and `TestUIHidden*`) rather than a switch
  label. Counterfactual: disabling the membership test fails exactly the three
  `TestUIHidden*` tests and the composite audit's `ui.hidden` case, nothing
  else. The §3 id-uniqueness invariant is still enforced at the verb boundary,
  not yet load-time.
- **F4 — DONE (2026-09-23).** The slash-menu advertisement and the sweeps
  landed incrementally with F1–F3 (each verb was added to `Verbs()`, the `ui`
  description, the menu-agreement test and the verb-round-trip sweep in the same
  PR that implemented it), so the surface and its chrome never drifted. This
  task closed the one remaining gap: the README **Status** section still
  described "two verbs" with `add`/`move`/`hide`/`show` all refused as "not
  yet", which was stale after F1–F3 — the accepted-but-not-drawn class pointed
  at the docs. It now describes the six implemented verbs (`add`, `move`, `set`,
  `style`, `hide`, `show`), the D2 addressing and D3 view-state notes as
  *implemented*, and the menu-drift bullet as a resolved past defect. No golden
  moves: the empty `ui.hidden` set is a no-op, so nothing new was frozen. Block
  F is complete.

### Block G — Phase 3: animation props [D4]

- **G0 — DONE (2026-09-23).** The `[anim]` timing-token vocabulary D4 signed is
  parsed and validated in `internal/theme` (`anim.go`, `theme.go` `Load`): a
  theme's `anim` section is lifted out before style tokens, each definition is
  validated (closed curve set `linear`/`ease_in`/`ease_out`/`ease_in_out`/`step`,
  non-negative `duration_ms`/`fps`, curve required), and `Theme.Anim`/`HasAnim`/
  `AnimNames` expose the tokens to the render/emit layer (never the fold — the
  D4/Q9 boundary). Theme-load refusals name the offending token and list the
  legal curves; counterfactual (validation disabled) fails exactly the three
  refusal tests. SCENES.md Scene 4 and the `node.go` comments were updated per
  D4's instruction, moving the blocker from "the token does not exist" to "the
  token exists; the props wait on a clock and their render semantics." **This is
  the foundation G1–G4 share; it is not any one prop.**
- **G-design — SIGNED (2026-09-23).** The design beat that stood between the
  timing token and a moving prop is drafted in `docs/DESIGN-BLOCK-G.md` (PR #54)
  and signed into the frozen docs. **G-A** — the host animation clock — is
  ADR-0005 in `docs/PLAN.md`: a `time.Ticker` fourth `select` case in the loop,
  per-node elapsed time held across frames like `ui.hidden`, the phase reaching
  the renderer as an input separate from `fold.State` so `RenderFrame` stays
  pure and every golden is phase-pinned, the ticker on-demand at the max active
  `fps`. **G-B** — the per-prop render semantics — is signed into SCENES.md
  Scene 4 as a table (`transition`→intensity, `scroll`→horizontal offset,
  `reveal`→character count, `enter`→row-count scheduler), on the principle that
  an animation only chooses which already-expressible frame to draw and never
  adds a fifth axis. The three open forks were resolved to their recommended
  defaults (on-demand ticker; `transition` triggers on appearance; re-entry
  re-animates), recorded in ADR-0005. Signing lifts no code guard — G1–G4 do,
  each with its own counterfactual.
- **G-A + G2 — DONE (2026-09-23).** The host animation clock and the `scroll`
  marquee landed together (PR #56), because the clock earns its place only once
  a prop consumes it. `scroll` graduated from a refused `json.RawMessage` to a
  read `*Scroll{Speed, PauseWhen}` struct: `validateScroll` honours it on a
  `marquee` and refuses it elsewhere (wrong node type, non-positive speed, or an
  unsigned `pause_when` bind) with an address, and it left `unrenderedFields`.
  The two foundational guards that encoded the refused-raw design
  (`unrendered_test.go`, `zero_value_key_test.go`) and the sibling audits
  (`rawBranchAccessors`, the remedy fixtures) were rewritten to the graduated
  behaviour, each with a counterfactual run — the review event the note below
  predicted, not a silent edit. The sub-forks the design left open were resolved
  as proposed: honoured on `marquee` only, and absent/`nil` is the accepted
  no-op. `renderMarquee` now windows the overflowing text at
  `(ticks * speed) mod (width + gap)`; the phase reaches it as `Renderer.AnimTicks`,
  an input separate from `fold.State`, so `RenderFrame` stays pure and every
  golden is unchanged. The clock is a `time.Ticker` fourth `select` case
  (`cmd/arxi-tui/anim.go`): per-node elapsed time held across frames like
  `ui.hidden`, advanced by wall time, armed only while a visible marquee is
  unpaused-active, and `pause_when` freezes the offset. The tick case only
  repaints, so the escape hatch stays uncapturable (invariant 6).
- **G3 — DONE (2026-09-23).** `reveal` is the first one-shot prop, landed on the
  clock G-A/G2 built (PR #57). It graduated from parsed-and-warned to a read
  `*Reveal{Anim}` struct on `scene.Node` (the `anim:"1"` tag puts it on the
  progress audit's animation axis): `validateReveal` honours it on a `text` node
  and refuses it elsewhere with an address (the character-count axis is the text
  node's), and `ValidateTokens` refuses a reveal naming an `anim` token the
  active theme does not define (empty resolves `anim.default`, Q8). The one-shot
  clock machinery this shares with G1/G4 landed here: `theme.EvalCurve` maps a
  curve name to its easing, the host clock grew a per-node `phases()` alongside
  scroll's `ticks()` (curve-eased `elapsed / duration_ms`, settling at 1 and
  arming no ticker once settled), and the renderer gained `AnimPhase` — a `[0,1]`
  one-shot input separate from `fold.State`. `renderText` clips the content to
  `round(phase * width)` graphemes via `ansi.Cut`; a nil `AnimPhase` draws the
  whole text, so no golden moves. The factory themes gained an `anim` section
  (`default`/`marquee`/`reveal.fast`) so a reveal resolves under the shipped
  look. Counterfactuals run in three places: reverting the phase mapping fails
  the render's start/mid/absent cases, disabling the axis check fails the
  off-text refusals, disabling the token check fails the undefined-token case.
- **G1 — DONE (2026-09-23).** `transition` is the second one-shot prop, landed on
  the clock G-A/G2 built and the one-shot pattern G3 set (PR #58). It graduated
  from parsed-and-warned to a read `*Transition{Anim}` struct on `scene.Node` (the
  `anim:"1"` tag puts it on the progress audit's animation axis): the engine draws
  it at the `renderNode` chokepoint — the node wears the theme's dim intensity
  while `AnimPhase[id] < 1` and its settled style once the phase reaches `1`, the
  SGR dim→bright intensity axis SCENES.md Scene 4 / G-B signs. Unlike scroll
  (marquee) and reveal (text) there is **no node-type refusal**: the intensity
  axis is universal, so transition is honoured everywhere the way `focus_glow` is
  (which is also what makes `enter`'s `row:false` container entrance well-defined).
  It reuses the host clock unchanged — it reports the same one-shot activity
  (`OneShot`, `Token`) reveal does, so the loop cannot tell the two apart and
  needs no new code — and `ValidateTokens` refuses a transition naming an `anim`
  token the theme lacks (empty resolves `anim.default`, Q8), its only load-time
  refusal. A nil `AnimPhase` draws the settled style, so no golden moves.
  Counterfactuals run: always-settled fails the running/dim cases, never-settle
  fails the settled and nil cases, and disabling the token check fails the
  undefined-token case.
- **G4 — DONE (2026-09-23).** `enter` is the scheduler that closes Scene 4's
  animation vocabulary, landed on the clock G-A/G2 built and the one-shot pattern
  G3/G1 set (PR #59). It graduated from parsed-and-warned to a read struct
  `{row, stagger}` with `anim:"1"`; `validateEnter` refuses a `row:true` with no
  `stagger` token and a `row:true` on a node with no rows (children or a
  `row_template`), and `ValidateTokens` refuses a stagger naming an absent `anim`
  token — while `row:false` is universal (the whole-container entrance, transition
  on the container). The engine draws it by splitting `renderNode`'s type switch
  into `renderByType`: `renderEnterRows` stands each row up as its own frame and
  places it not-drawn / dim / settled by its personal clock (the row-count axis),
  `enterWhole` dims the whole subtree for `row:false` (the subtree dimming
  transition left to enter), and `dimFrame` rewrites a frame's spans to the dim
  token. The clock learned the per-row offset (`AnimActivity.Row` →
  `rowOffset`): row *i*'s phase is `elapsed/durMS − i`, absent until its offset,
  and `running()` settles it at `(rowOffset+1)·durMS` so the ticker stays armed
  until the last row arrives. The five node-type dispatch audits that read
  `renderNode` were retargeted to `renderByType`. Render, clock and scene tests
  each carry counterfactuals run in both directions.
- **G5 — DONE (2026-09-23).** The remaining Scene 4 golden is frozen and Block G
  is closed. Two golden families cover Scene 4's motion. First, each prop landed
  with its own render/clock test pinned at chosen phases (t=0, mid, settled), the
  discipline DESIGN-BLOCK-G.md signs — scroll's, reveal's, transition's and
  enter's status paragraphs are all updated in SCENES.md, and all five animation
  properties are drawn by the engine with none left parsed-and-warned. Second,
  the durable composed pin no per-prop test carried: `testdata/ANIMATION.json` is
  a shippable Scene 4 document (a `scroll` marquee, a `reveal` text, a
  `transition` heading, and a staggered `enter` list over `agent.todos`), frozen
  as `ANIMATION.frame`/`.styled` at a **chosen non-nil phase** so the fixture
  witnesses motion — a windowed marquee, a phase-clipped reveal, a dim
  mid-entrance transition, and the enter row-count axis mid-flight (one settled
  row, one dim, one not yet drawn). It is pinned at a chosen phase, not the nil
  phase that would draw it settled and cover none of it — the SOBRIA.styled
  zero-row-marquee trap AGENTS.md records. A witness test asserts each prop is
  moving before the byte-for-byte golden, the document validates against the
  factory theme's `anim` section (so it is genuinely shippable, not merely
  parseable), and the counterfactual was run: forcing `enterRowState`
  always-settled draws every enter row settled and fails the styled golden and
  the witness on the dim and absent rows.

### Block H — Phase 3: declarative plugin mounting (heart of the phase)

- **H1 — SIGNED (2026-09-23).** The plugin manifest schema (`id`, `name`,
  `version`, `protocol`, `tokens`, `mounts`, `executable`, `args`,
  `capabilities`, `consent_required`, `binds`), drafted as a reviewable proposal
  in `docs/DESIGN-BLOCK-H.md` (PR #61, owner-accepted and merged to master), is
  now signed into the frozen docs — SCENES.md Scene 6, BINDS.md §4.4, PLAN.md
  ADR-0006, ADDRESSING.md §4, and a TOKENS.md forward pointer. It settles four
  things H2–H7 depend on: the JSON format and field list; the load-bearing
  **declarative (Block H) vs behavioral (Block I)** split with `executable` as
  the single discriminator (a manifest with no `executable` runs zero code and
  streams nothing); mount addressing (H-B, reusing D2's `where` grammar plus
  overlay anchors, with `<plugin-id>/` id prefixing so N strangers' trees compose
  without id collisions); and the `<plugin-id>.*` declared-vs-used validation
  (H-C, mirroring the `row.*` `rowSchemas` scope mechanism against the manifest's
  `binds` map). It *consumes* three already-signed decisions unchanged —
  ADR-0003's open plugin namespace, TOKENS.md's `user > plugin > factory`
  precedence, and the Q15 consent identity contract (named here, built in Block
  I). The four open forks were resolved to their recommended defaults when the
  design was accepted (the same way Block G's three were): (1) `executable` is
  the discriminator, a single source rather than an explicit `kind` field that
  could disagree; (2) the loader mechanically prefixes every mounted id with
  `<plugin-id>/` after validation; (3) the `binds` `kind` set starts closed at
  `text` and `series`, widened per node as a signed change; (4) `/ui plugin
  remove <id>` ships with H3, since unmount is the mechanical inverse of mount
  and the symmetry is what makes the id/token namespacing testable. Signing lifts
  no code guard; H2–H6 do, each with its own counterfactual.
- **H2 — DONE (2026-09-26).** `internal/ext` loads a declarative plugin manifest
  (`manifest.go`): `ParseNamed`/`Parse` build the manifest with an address book
  (top-level key offsets, `loc.go`), and `Validate` refuses, each with
  `file:line`: a malformed identity block (id grammar `[a-z][a-z0-9-]{0,62}`,
  required `name`/`version`, closed `protocol` set `ext/v1`); a **behavioral**
  manifest (one with an `executable`) with "behavioral plugins are Block I" — the
  discriminator that keeps "zero code" checkable at load as the absence of one
  field; a declarative manifest declaring a behavioral field
  (`capabilities`/`args`/`binds`/`consent_required`) — the discriminator failing
  the other way; an empty plugin (no `mounts` and no `tokens`); a malformed token
  block, through the new exported `theme.LoadBytes` (one validator, two callers —
  `theme.Load` now delegates to it); and each mounted fragment through the scene
  validator, with the fragment rebased onto the manifest bytes so a fragment
  refusal reports the manifest-absolute `file:line`. The package stays on the
  pure-data side of the arch seam (imports `scene` and `theme`, no UI package).
  Counterfactuals run in both directions: disabling the executable check loads a
  behavioral manifest (fails the counterfactual test), and disabling the rebase
  drops the manifest-absolute address (fails the fragment-address test).

- **H3 — DONE (2026-09-26).** `patch.Mount` composes a loaded declarative
  manifest's fragments into the host document (`internal/patch/mount.go`),
  reusing `add.go`'s write-path `insert` at each mount's `where` — the D2 grammar
  plus the overlay-anchor form (`top-right`/`bottom`/… → a top-level child of
  root) ADDRESSING.md §4 / H-B signs — so a plugin author and a `/ui add` user
  cannot drift on what a position means. Every mounted node id is rewritten to
  `<plugin-id>/<id>` (H-B.3) before the id-uniqueness invariant (ADDRESSING.md §3)
  runs over the composed tree, so N strangers' trees compose without collision and
  a surviving duplicate is the plugin declaring the same raw id twice — refused
  with the id named. `Mount` re-validates the composed document (invariant 3) and
  runs `m.Validate()` itself, so a behavioral manifest is refused even when handed
  straight to the mounter. `patch.Unmount` is the fork-4 inverse: drop every node
  whose id begins `<plugin-id>/` (tokens are H4's half), which names no host node.
  `patch` imports `ext` (never the reverse), keeping `ext`'s pure-loader arch
  seam. Counterfactuals run in both directions: the prefix collision test composes
  two plugins sharing a raw id (prefixed → distinct; raw → collides), and the
  behavioral and self-collision guards were reverted by hand and observed to fail.
  H6 wires this behind `/ui plugin add <url>`/`remove <id>`.

- **H4 — DONE (2026-09-26).** Plugin tokens are merged into the live host at the
  signed precedence `user > plugin > factory` (TOKENS.md). The `theme.Merge`
  primitive and `m.Theme()` parser already existed (H7's golden witnessed
  `Merge(factory, plugin)`); H4 wires them into the running loop and closes the
  silent half-mount where a plugin's fragments landed but its own tokens never
  reached the active theme, rendering it unstyled. `patch.Result` gains
  `Tokens *PluginTokens` (`internal/patch/patch.go`): a `plugin add` carries the
  plugin's parsed token block keyed by id (empty-not-nil when it declares none,
  so the host merges a no-op rather than nil-checking), a `plugin remove` signals
  dropping that layer by id — the token half of unmount the H3 note deferred. The
  precedence is *not* encoded in the op or in `Merge` (which only knows "over
  wins"); it is the loop's layering order. The loop keeps the factory `baseTheme`
  and one `pluginThemeLayer` per mounted plugin, and `applyTokenLayer` (a free
  function beside `applyViewState`) folds each op into that ordered set:
  add-replaces-by-id so a re-mount is not a second layer, remove-drops by id so it
  names no factory/user token. `composeTheme` recomposes the active theme from the
  base over every layer in mount order — a later plugin wins a token conflict with
  an earlier one, and the user layer merges last when the boot path grows one — so
  a remove is exact (recompose from the base leaves no residue). It also re-points
  the clock's anim lookup so a plugin's one-shot token (reveal/transition/enter)
  resolves; the continuous-marquee tick rate stays at the boot value by design
  (re-arming the ticker mid-session is out of scope). `uiCommandKey` threads the
  callback and applies `Result.Tokens` alongside the document replacement (a mount
  changes both). Counterfactuals run in both directions: on the patch side,
  Mount not setting `Tokens` fails `TestMountCarriesThePluginTokenLayer`; on the
  loop side, `composeTheme` ignoring its layers fails the plugin-over-factory,
  user-over-plugin and later-plugin precedence tests, and a remove no-op fails the
  add-then-remove round-trip. Note: the boot path still has no user theme layer
  (main.go loads only the compiled-in factory `SOBRIA`), so the `user >` half is
  proven through `composeTheme`'s ordering rather than a live user file — the
  layer merges last the moment such a file exists.

- **H5 — DONE (2026-09-26).** `scene.ValidateWithPlugin(*PluginScope)` lifts the
  unsigned-bind refusal for a bind in a plugin's own `<id>.` namespace, but only
  when the plugin declares it — the plugin-namespace analogue of §4.7's `row.*`
  scope, threaded through `validateBindsScoped` beside the row scope and branched
  in `validateOneBind` (`internal/scene/validate.go`). `Validate()` is unchanged
  (the same walk with no plugin scope). A signed host bind resolves *before* the
  plugin scope, so a plugin whose id spells a host namespace (`agent`) cannot
  shadow `agent.working`. Three refusals, each `file:line`: a `<id>.*` bind in a
  **declarative** plugin (no executable, hence no binds — empty namespace, streams
  nothing); a namespace bind for a **field the plugin does not declare**; and a
  declared bind used under a node type its `kind` cannot draw (`kind` set closed
  at `text`→{text,marquee} and `series`→{sparkline}, mirroring the render axes the
  way `overlayAnchors` mirrors the engine's anchor set). `ext.validateFragment`
  projects `m.ID`+`m.Binds` into the scope and validates each mounted fragment
  with it (`internal/ext/manifest.go`, `pluginScope()`), reading the manifest's
  `binds` map directly rather than copying it into a second inventory. Because
  `checkBehavioral` refuses any manifest with `binds`, a manifest reaching the
  fragment check is declarative, so its scope's binds are empty and using its own
  namespace is the refused empty-namespace case — the load-time face of the
  declarative/behavioral split. Counterfactual, run by hand: neutering the
  `validatePluginBind` branch (accept everything in-namespace) fails the wrong-kind,
  undeclared-field and declarative-empty scene tests, while the declared-and-correct
  and out-of-namespace tests still pass — so the branch is exactly what enforces
  the rule. The `arxi_tui/internal/scene` package still imports no UI package and,
  crucially, no `ext`: the scope is a scene-owned type the loader projects into.
- **H6 — DONE (2026-09-26).** `/ui plugin add <url>` and `/ui plugin remove <id>`
  are the declarative plugin path's command surface. `patch.parsePlugin` reads the
  subcommand into `Command{Verb:"plugin", Key:add|remove, Value:url|id}` — one verb
  and one string, the closed shape `Command` keeps — and `Verbs()` advertises
  `plugin`, so the slash-menu agreement sweep and the verb round-trip sweep both
  cover it (the menu `ui` description names it). `applyPlugin` routes `remove` to
  `Unmount` (a pure source edit, no fetch) and `add` through an injected
  `patch.Fetcher` to `ext.ParseNamed` + `Mount`, so the H3 composer's guarantees
  (id prefixing, uniqueness, the behavioral refusal, invariant-3 re-validation)
  are reused rather than reimplemented — a fetched behavioral manifest is refused
  by the same gate a local one is, which is H6's load-bearing counterfactual
  (fetching does not buy a manifest past H2). The network lives behind the
  `Fetcher` seam exactly as `internal/eval` keeps the only real HTTP client at the
  edge: `patch` stays a pure offline transform (a fake fetcher returns fixture
  bytes in tests), `Apply` is the fetch-free spelling that delegates to
  `ApplyWithFetch(nil)`, and the one real implementation —
  `cmd/arxi-tui.httpManifestFetcher` (http/https only, a 15s timeout, a 1 MiB body
  cap) — is threaded into `uiCommandKey`. Counterfactuals run: removing the
  nil-fetcher guard panics the no-fetcher test, the behavioral manifest is refused
  by Mount, mount+remove round-trips back to the original document, and the fetch
  size cap is exercised at exactly the cap (accepted) and one past it (refused).

- **H7 — DONE (2026-09-26).** The Scene 6 golden is frozen as the composed
  declarative-ticker scene. `testdata/plugins/TICKER.manifest.json` is the
  declarative manifest an author ships (id `tick`, `profit`/`loss` tokens, one
  top-right overlay fragment, no `executable`); `testdata/TICKER.json` is the
  composed Scene 6 document `patch.Mount` produces from it and a dedicated host,
  pinned as `TICKER.frame`/`.styled` and rendered through the ordinary engine
  path. `TestTickerJSONIsTheMountOutput` proves the fixture is byte-for-byte the
  mount output (not hand-authored), the witness test asserts the overlay reached
  the frame with no `UNKNOWN NODE TYPE`, and the styled golden witnesses both
  contributed tokens surviving the `Merge(factory, plugin)` composition.
  **The golden pins the honest declarative frame, not the mock-driven preview**
  DESIGN-BLOCK-H.md H7 sketched: declaring `binds` makes a manifest behavioral
  (H2 refuses it) and painting a declared `mock` for an unsatisfied bind is the
  Q16 preview renderer — that is Block J. A declarative plugin streams nothing,
  so the overlay shows its chrome and a no-data placeholder, which is the frame
  the design itself calls "the honest frame a declarative-only load produces".
  The Scene 6 heading was reformatted to `TICKER (a community plugin shipped by
  link)` so the progress audit's fixture↔scene name match resolves (exactly as
  Scene 9 was reformatted for SUBAGENTS, E4); the audit now counts 6 of 11
  scenes pinned. Note: this pins Scene 6 against a dedicated minimal host, not
  the behavioral stream (Block I) or the installer preview (Block J).
- **H8 — DONE (2026-09-26).** `on_press` action routing. The closed action
  grammar is signed in BINDS.md §4.8 — `cmd:<command>`, `focus:<node>`,
  `answer:<kind>`, with `ext:` reserved and refused (behavioral plugins, Block I
  / I4) — and read by one function, `scene.ParseAction`, so the validator and the
  host dispatcher cannot drift on what a legal action is. `on_press` graduated
  from the wholesale `unrenderedFields` refusal (the last standing entry, so that
  map is empty now) to `validateOnPress`, which refuses a prefix outside the set,
  an empty argument, an out-of-vocabulary `answer` kind, the reserved `ext:` arm,
  and a `{row.<field>}` interpolation naming a field the enclosing template's row
  schema does not declare (Q20) — each with `file:line`. The host loop
  (`cmd/arxi-tui/press.go`) owns the `ui.focus` cursor as view state like
  `ui.hidden`: Tab/Shift-Tab move it over the pressable nodes in document order
  with the input as home (Q19, so the typing flow is always one Tab away), and
  Enter on a focused node dispatches its action — `focus:` sets the cursor,
  `cmd:` runs the command through the same `uiCommandKey`/`SubmitPrompt` surface a
  typed line takes (so a button and a keystroke cannot diverge), and `answer:` is
  recognised but reports its deferral to the Block I driver channel rather than
  dropping silently. The two universal-property audits were reconciled: `on_press`
  joins `id` as an addresses-not-draws property, held to round-trip **plus** a
  malformed-action refusal so the exemption stays load-bearing. Counterfactuals
  run in both directions: neutering `validateOnPress` fails the scene refusal
  tests and the engine's malformed-action check; breaking `advanceFocus` or the
  `cmd:` dispatch arm fails the host press tests. Two parts are signed-but-unbuilt
  by design and recorded at their sites: the `tab: false` input opt-out (the
  input-as-home ring already protects typing) and the `answer:` inbox call (needs
  Block I), plus dispatch of a *template* row (Scene 9's `cmd:/agent {row.id}`),
  which needs the per-element `{row.field}` substitution at press time. This
  unblocks I4 (the `ext:` arm) and Scene 8's interactive buttons.

### Block I — Phase 3: behavioral plugins (NDJSON subprocess) [H]

- **I1 — SIGNED (2026-09-26).** The subprocess plugin protocol, drafted as a
  reviewable proposal in `docs/DESIGN-BLOCK-I.md` (PR #75, owner-accepted and
  merged to master), is now signed into the frozen docs — PLAN.md ADR-0007 (the
  wire), BINDS.md §4.3 (the `ui.plugin.<id>` liveness bind) and §4.8 (the `ext:`
  reservation, wire now signed), and SCENES.md Scene 6 (the behavioral-protocol
  note). It settles what I2–I6 depend on: a **second** NDJSON channel over the
  subprocess's stdin/stdout, reusing `internal/driver/ndjson.go`'s framing, cap,
  error-code set and cancellable `readLine` wholesale but kept separate from the
  host↔core channel because that channel *is* the run log and a plugin's
  `tick.price` must never enter the fold (two channels, two authorities, matching
  ADR-0003's namespace split); a `hello`/ack handshake where the host tells the
  plugin its `plugin_id` and `granted` capability subset, so the plugin publishes
  only **relative** `bind` frames the host prefixes into `<plugin-id>.*` and can
  never forge another namespace; async push composed with pull-by-frame (ADR-0004)
  through a host-owned latest-value store held like `ui.hidden`, with a fifth
  loop `select` case that only adds a reason to repaint (ADR-0005) and
  last-value-wins backpressure; `ext:<plugin-id>:<action>` input routed as an
  `id`-correlated `action` frame with host-resolved `{row.field}` args (H8's
  reserved fourth prefix, dispatched by I4); a consent-gate-first lifecycle over
  the ported arxi-sim procgroup supervisor with the escape hatch untouched
  (invariant 6); and the I5 identity tuple
  `name+version+protocol+executable+args+capability-set+digest`, the `digest`
  computed by the loader over the fetched package, not declared. The four open
  forks were resolved to their recommended defaults, the same way Blocks G and H
  resolved theirs: (1) the host ack is **required before the plugin may publish**,
  since the ack is the one point that communicates `granted` and publishing
  before it is acting on ungranted power; (2) a **batched multi-field `bind`
  frame** is allowed so a coherent snapshot lands atomically for one repaint (a
  torn frame showing a new price beside an old sparkline is the failure this
  prevents under pull-by-frame); (3) a **live mount shows the plain placeholder,
  not `mock`**, before the first frame, keeping "waiting" and "preview" (Block J's
  `BindDecl.Mock`) visually distinct and the Scene 6 golden pinned on the mock
  frame; (4) process death triggers **bounded restarts with backoff, then freezes
  at the last-published values** — never crash (the fold is untouched, invariant
  2), never busy-loop respawn, with the death reported through the new
  `ui.plugin.<id>` liveness bind so the diagnosis the placeholder cannot carry is
  visible. New vocabulary signed before I2: the `ui.plugin.<id>` host view-state
  bind (§4.3) and the `digest` computation (computed, not a manifest field).
  Signing lifts no code guard; I2 (supervisor), I3 (frame ingestion), I4 (input
  routing), I5 (consent gate) and I6 (tools door) each land with their own
  counterfactual, exactly as H2–H6 did.
- **I2 — DONE (2026-09-26).** `internal/ext/supervisor` runs a behavioral plugin
  as a subprocess and speaks the ext/v1 wire (ADR-0007) to it. It is a copy of
  arxi-sim's procgroup supervisor (ADR-0001), **never an import**: the ported part
  is the cross-platform kill-the-whole-tree mechanics — `process_unix.go`
  (`Setpgid` + a negative-pid `SIGTERM`/`SIGKILL` so a plugin's grandchildren die
  too) and `process_windows.go` (a `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` Job Object
  + `CREATE_NEW_PROCESS_GROUP`) — so an unmount leaves no orphan (invariant 6). The
  package imports `internal/ext` for the `Manifest` and no UI package. `Start` is
  non-blocking (the handshake, reader and any restart run on the supervisor's own
  goroutine, so mounting never stalls the loop); `runOnce` spawns → handshakes →
  reads until death, always leaving the child dead and reaped before it returns.
  The handshake reads the plugin's `hello` (bounded by `HandshakeTimeout`), gates
  on the manifest `protocol` token, and answers with the ack carrying `plugin_id`
  and `granted` — **the ack precedes any forwarded frame**, so a plugin cannot act
  on ungranted power (fork 1). `stderr` is reserved for human logs (pointed at the
  host's stderr, never parsed as frames). The reader forwards every non-handshake
  frame verbatim on `Frames()` as `{Type, Raw}` — mapping a `bind` frame into the
  `<plugin-id>.*` store is I3, so the supervisor keeps the raw bytes rather than
  committing to a shape. Death policy is fork 4: a restartable death
  (`ErrUnexpectedExit` — EOF, a clean exit the host did not request, a spawn
  failure) costs one attempt of the bounded budget and a doubling backoff, then
  freezes with the error recorded; a fatal protocol error (`ErrFatalProtocol` — a
  wrong wire version, a frame before the hello) stops at once and does not consume
  the budget; a handshake timeout (`ErrHandshakeTimeout`) is restartable (a slow
  start is not proof of a broken plugin). `Close` cancels, kills the group and
  reaps, idempotently — the runtime companion to `/ui plugin remove <id>`.
  Counterfactuals run (all three by hand): a wrong `plugin_id` in the ack makes the
  helper exit and the happy-path test time out; classifying the protocol mismatch
  as `ErrUnexpectedExit` restarts three times instead of stopping; removing the
  `attempt >= MaxRestarts` guard never settles (14 launches in 10s under backoff,
  which also witnesses the backoff working). Note: the whole-group kill (an
  orphaned grandchild) is guaranteed by the ported mechanics but not portably
  asserted in a test across the unix/Windows split; the reap of the direct child
  is pinned by `TestSupervisorCloseReapsTheProcess`. The `ui.plugin.<id>` liveness
  bind (§4.3) is written by the loop from this supervisor's terminal state in I3.
- **I3 — DONE (2026-09-26).** Behavioral plugin frames become resolvable binds.
  Three pieces, each with a counterfactual run by hand. (1) `internal/ext`
  gains a host-owned `PluginStore` (`store.go`): `Ingest` maps a `bind` frame
  into `<plugin-id>.<field>` — the host composes the prefix, so a plugin only
  ever utters a *relative* field and can never write outside its namespace
  (§I-C) — validating each field against the manifest `binds` map (the runtime
  face of H5's load-time rule, the map consulted directly, never copied) and
  the value's shape against the declared `kind` (`text`→scalar, `series`→numeric
  array; the wire-value axis of scene's node-type `bindKindNodeTypes`). A
  batched frame (fork 2) is validated whole and committed whole, so one bad
  field lands none of them — the torn-frame guarantee, counterfactual: a
  commit-as-you-go variant leaves a partial write and fails the atomic test.
  `Snapshot` returns a fresh `map[string]string` (JSON→string projection owned
  by the store so the engine never decodes plugin JSON), merged with the
  `ui.plugin.<id>` liveness bind; `DropPlugin` erases exactly one prefix (the
  dotted test kills a `HasPrefix` that would take a sibling `ticker`). (2) The
  renderer resolves both families: `resolveBindRow` consults a new
  `Renderer.PluginValues` snapshot — an input separate from `fold.State` for the
  invariant-2 reason (a plugin's value entering the fold would make a stranger's
  process an authority over the run log, ADR-0003), threaded through `child()`
  and the `when`/row chokepoints like `curRow`, with `bindTruthy` single-sourcing
  the placeholder-is-falsy rule so a `when: ui.plugin.<id>` gate reads it too. A
  nil snapshot (every non-plugin golden, the pure path) resolves every plugin
  bind to absent → placeholder, so no golden moves; counterfactual: neutering the
  lookup fails exactly the resolve, no-leak and liveness-shown tests. (3)
  `Supervisor.DrainInto` (`supervisor/pump.go`) is the "stream frames into binds"
  verb over a real subprocess: it drains `Frames()`, ingests `bind` frames
  (dropping a single malformed/undeclared frame, never killing the process — §I-G),
  and writes liveness from the terminal state — `dead` with values frozen on an
  error exit, `DropPlugin` on a clean Close (`/ui plugin remove`); counterfactual:
  always-drop erases the frozen values and fails the death test. Two parts are
  scoped out and recorded at their sites: the loop wiring that launches the pump
  goroutine and feeds the snapshot to the renderer each repaint waits on the live
  mount (I5, which spawns behind consent), and a pre-publish `live` / backoff
  `error` liveness needs a supervisor lifecycle signal I2 does not expose (the
  bridge uses first-frame as the `live` proxy). The `series` kind is stored and
  resolvable but drawn by nothing yet — the `sparkline` node is signed and
  unimplemented, a later per-node change.
- **I4 — DONE (2026-09-27).** Route input via `on_press` to plugin actions. Three
  pieces, each with a counterfactual run by hand. (1) `internal/scene` lifts H8's
  wholesale `ext:` refusal: `ActionExt` graduates `ext:<plugin-id>:<action>` from
  refused-with-a-Block-I-address to a parsed action (`action.go`), splitting the
  two segments on the **first** colon only (the manifest id grammar has no colon,
  so a colon inside the action name stays there) into `PluginID` and `Arg`, each
  refused when empty with `file:line`. Whether a plugin with that id is mounted
  and granted is a runtime concern, so a well-formed `ext:` validates clean at
  load — the same load-vs-runtime split `focus:` lives under (a `focus:` naming no
  node reports at press time, not load). Counterfactual: the scene tests move
  `ext:tick:refresh` from the refused set to the accepted set and add the
  two-segment refusals; reverting the parse fails them. (2)
  `internal/ext/supervisor` gains the host→plugin half: `Supervisor.SendAction`
  (`send.go`) writes the `{type:action,id,action,args}` frame (§I-E, ADR-0007) to
  the live child's stdin, gated on the granted `actions.register` capability — the
  wire face of "power granted at the gate, once" (invariant 7). The sender is
  registered after the ack (a plugin cannot receive an action before it knows its
  grants) and cleared when the child dies, so a press racing a restart reports
  `ErrPluginNotLive` rather than writing to a dead pipe. `Registry` (`registry.go`)
  maps plugin id → supervisor and owns the host-side correlation id; a missing id
  is `ErrPluginNotMounted`, distinct from not-granted and not-live so the three
  reports differ. The `echoaction` helper mode makes the round-trip observable on
  `Frames()`; counterfactuals run: disabling the capability gate routes an
  ungranted action (fails the ungranted test), and not registering the sender
  makes every `SendAction` `ErrPluginNotLive` (fails the round-trip and registry
  tests). (3) The host loop dispatches the arm: `dispatchPress`
  (`cmd/arxi-tui/press.go`) gains the `ActionExt` case, routing the split id and
  action through a `pluginActionRouter` seam (satisfied by `supervisor.Registry`,
  held by the loop beside `ui.hidden`/`ui.focus`). A router error or a nil router
  is reported to the user, never a silent drop or a nil-interface panic — the §I-G
  placeholder-not-crash rule on the input side; counterfactual: dropping the
  `SendAction` call fails the routes-to-router press test. Two parts are scoped out
  and recorded at their sites: the loop's registry is **empty until I5** mounts a
  plugin behind the consent gate (so an `ext:` press reports "no plugin with that
  id is mounted" today — the routing mechanism is complete, the live spawn is
  I5's), and the per-element `{row.field}` argument substitution an `ext:` press in
  a template will carry rides on the same template-row dispatch H8 parked, so a
  plain `ext:<id>:<action>` press sends empty args and the resolved-args map
  arrives at `SendAction` once that lands. The `id`-correlated `ok`/error ack
  (§I-E, a PROPOSAL "so a button can show it was accepted") is a noted refinement:
  the plugin proposes by publishing new bind values (I3), so `SendAction` returns
  once the frame is written and does not wait on a reply.
- **I5 — DONE (2026-09-27).** Consent gate by identity, with "remember." Three
  pieces, each with a counterfactual run by hand. (1) `internal/ext/identity.go`
  computes the grant identity tuple — `name + version + protocol + executable +
  args + capability-set + digest` (§I-H, Q15) — ported from arxi-sim's
  `identity.go`, never imported: `Identity` hashes a named struct so a field
  insert can never silently reorder the pre-image, sorts the capability set so
  membership (not declaration order) is the authority, and takes the digest as
  an argument because it is `PackageDigest`'s job, not the manifest's. `PackageDigest`
  hashes a fetched package tree canonically (slash-normalised path + exec-bit +
  byte-length-framed content), refusing symlinks and non-regular entries — a
  symlink points at bytes the digest never read, so a grant bound to the tree
  would cover content the consent screen never showed. Counterfactuals: dropping
  the digest from the pre-image, or the capability sort, each fails
  `TestIdentityBindsEveryTupleField`; the symlink refusal is Windows-conditional
  (privilege) so the content/mode half stands alone. (2) `internal/ext/consent.go`
  is the gate: a closed capability vocabulary (`KnownCapability`), a three-way
  `Classify` (Granted / NotGranted / NotDeclared) so "it never asked" and "you
  said no" are different refusals (§I-H), a `ConsentStore` seam with a
  session-scoped `MemoryConsentStore` (the disk-backed store lands with the
  config layer), and a `Gate` whose `Decide` answers `DecisionRemembered` /
  `DecisionNeedsConsent` by identity and whose `Grant` refuses any capability the
  manifest did not declare or the closed set does not know — the gate is the one
  place authority is widened past what was asked, and it does not. `remember`
  decides persistence and nothing else; a rejection is the absence of a Grant, so
  it is session-local by construction. Counterfactuals: `Grant` persisting
  regardless of `remember` fails the session-local test; `Classify` skipping the
  declared check collapses the two denials and fails the three-way test; a
  version or digest bump re-asks (`TestAVersionBumpReAsks`). (3)
  `internal/ext/supervisor/mount.go` is the one verb I3 and I4 each pointed at
  with "the live mount is I5": `Mount` runs the load-time order (§I-F) —
  `gate.Decide` → prompt only if unseen → **only on a grant** spawn the
  supervisor, `Registry.Add` it, and launch the `DrainInto` pump goroutine — with
  `cfg.Granted` **overwritten** by the gate's decision so a caller cannot
  pre-fill it to smuggle a capability past the gate (invariant 7). The consent
  `Prompt` is a callback so the orchestration carries no UI (a test supplies a
  fixed answer; the real loop supplies the consent scene); a rejection returns
  `ErrConsentRejected` and spawns nothing. Counterfactuals: keeping the caller's
  `cfg.Granted` (skipping the overwrite) makes the helper reject its ack and no
  bind lands (fails `TestMountIgnoresCallerSuppliedGranted`); ignoring the
  rejection spawns a refused plugin (fails `TestMountDoesNotSpawnOnReject`). Two
  parts are scoped out and recorded here: the **consent scene** (rendering the
  identity tuple and reading a Y/N/remember answer) is Scene-7-adjacent UI and
  rides on the scene layer, so `Prompt` is the seam it will satisfy. The loop
  wiring that calls `Mount` from `/ui plugin add` for a *behavioral* manifest
  (holding the gate, store and registry beside `ui.hidden`) waits on that consent
  scene — the routing mechanism (I4) and the live-mount orchestration (this) are
  both complete; what remains is the screen that asks the user.
  - **I5-disk — DONE (2026-09-27).** `ext.DiskConsentStore`
    (`internal/ext/consent_disk.go`) is the persisted `ConsentStore` the "remember"
    promise (Q15) needs across a host restart, not just a re-mount in one session.
    It is kept **path-agnostic** — `OpenDiskConsentStore(path)` takes the file
    location, because the only thing the config layer decides here is *where* the
    file lives; folding an OS path into the store would put that decision in the
    wrong layer. Keyed by the identity string, so persistence inherits the whole
    identity contract (a version/digest/arg change is a different key and re-asks).
    The write is atomic (temp file in the same dir, then rename) so a crash leaves
    either the old or the new complete allow-list, never a truncated one; a missing
    file is the first run (empty, no error) while a file that exists but does not
    parse is **refused, not silently emptied** — starting empty on a corrupt file
    would forget every remembered grant and look identical to a fresh install. A
    write failure is retained on `Err()` rather than surfaced from `Remember` (which
    cannot return an error without changing the seam for `MemoryConsentStore`), and
    because the *session* grant is valid regardless of disk, a full disk must not
    block a mount the user just approved. Counterfactuals run by hand: disabling
    the flush fails the persist-across-reopen test; swallowing a malformed file into
    an empty store fails the refusal test. What remains for the loop is only the
    config layer choosing the path and pointing a gate at it.
  - **I5-consent-scene (view) — DONE (2026-09-27).** `ext.ConsentScene(m, digest)`
    (`internal/ext/consent_scene.go`) is the visual half of the `Prompt` seam
    `supervisor.Mount` takes — the screen shown when the gate returns
    `DecisionNeedsConsent`. It is a **host-generated scene** built exactly the way
    the change-diff view is (ADR-0003, `patch/diff.go`): a box over a stack of
    styled `text` rows, authored through the normal `scene.ParseNamed` path, no new
    engine capability. It renders the whole identity tuple the grant binds to (I-H):
    name+version, id/protocol, the executable+args `runs:` line, the package digest
    (carried full; a narrow terminal clips the tail, and no human eyeballs a
    sha256 — the gate computes `Identity()` over the full value), and every
    requested capability one per row (a capability-less manifest states "no host
    powers" rather than rendering blank). Plain rows carry **no** style token, not
    `"text"`: the Factory backstop signs `dim`/`header`/`banner` (and the diff
    tokens) but not `"text"`, so a plain row must reference no token to render under
    both themes — proven by `TestConsentSceneValidates` against SOBRIA and Factory.
    `TestConsentSceneShowsEveryIdentityField` is the property no golden guarantees
    alone (a byte-match to a fixture that itself dropped a field proves only that
    the omission is stable); counterfactual run by hand: dropping a capability from
    the render fails exactly it. `testdata/CONSENT.styled` freezes the rendered
    frame through the engine (`internal/engine/consent_view_test.go`), the diff
    view's golden twin. **Scoped out and remaining:** the interactive half — the
    loop layer that reads a Y/N/remember keypress against this screen and calls
    `supervisor.Mount` from `/ui plugin add` for a *behavioral* manifest (holding
    the gate/store/registry beside `ui.hidden`) — is the next increment; this lands
    the view first, exactly as B4 pinned the diff view before B5 wired it live.
  - **I5-consent-loop (wiring) — PARTIAL (2026-09-27).** Two install-independent
    halves of the loop wiring landed, each pure and counterfactual-tested rather
    than buried in the 1.7k-line loop select. (1) The **config layer** the gate
    reserved: `consentStorePath()` (`cmd/arxi-tui/consent.go`) resolves the
    allow-list location (`ARXI_CONSENT_FILE` override, else `~/.arxi/consent.json`,
    the same `~/.arxi` tree the run log uses) and `openConsentGate()` builds the
    `ext.Gate` over the `DiskConsentStore`. A malformed/unreadable file falls back
    to a **session** store and leaves the file untouched — re-asking one session
    preserves the grants for recovery, where a reset would destroy them and a boot
    refusal would let an allow-list problem take down the whole TUI. Proven by a
    grant-remembered-across-reopen test (the counterfactual — a memory fallback —
    fails it) and a malformed-file test that asserts the bytes are still on disk
    after fallback. (2) The **keypress→answer** mapping: `consentAnswerForKey`
    (`cmd/arxi-tui/consent_prompt.go`) turns a key pressed against the screen into
    a `supervisor.ConsentAnswer` — `y` grants the declared set session-only, `r`
    grants and remembers, `n`/Esc reject, and **every other key leaves the prompt
    standing** ("no answer is not a yes", Q15; the counterfactual — a granting
    default branch — fails the standing-prompt test). Kept a pure function of the
    key for the same reason `focusKey`'s grammar is: the keypress→grant mapping is
    the security-load-bearing decision, and a pure function is what a counterfactual
    pins exactly.
    **Blocked and remaining:** the third half — the modal loop state that shows
    `ext.ConsentScene`, feeds keys through `consentAnswerForKey`, and calls
    `supervisor.Mount` from a *behavioral* `/ui plugin add` — cannot land honestly
    yet, because `Mount` needs the `digest` and `identity.go` is emphatic that a
    stale/empty digest "silently makes every mount look like a different plugin".
    The digest is `PackageDigest` over the **fetched package (executable + manifest)**
    (§I-H), but the loop's `patch.Fetcher` (`plugin_fetch.go`) fetches **manifest
    bytes only** — there is no package-tree download or executable installer, so
    there is nothing to digest. Wiring the spawn against a fabricated digest would
    break the exact grant-transfer safety the gate exists for. The remaining wiring
    therefore waits on a behavioral package installer (Block J installer territory
    / part of I6): fetch a package bundle, lay it out, `PackageDigest` it, then the
    modal mount is a small, testable addition (the helper-process pattern
    `supervisor/mount_test.go` uses proves the spawn side already works end to end).
  - **I5-installer-design — DRAFTED (2026-09-27), awaiting signature.** The
    behavioral package installer the blocker above named is now argued on paper:
    DESIGN-BLOCK-I.md §I-I. A behavioral package ships as a single `.tar.gz` from
    one URL (stdlib `archive/tar`+`compress/gzip`, no runtime dependency); its
    manifest lives at `<root>/plugin.json` **inside** the digested tree so editing
    the terms moves the digest; extraction is the security boundary (traversal,
    symlink and decompression-bomb refusals, and the in-package `executable` check
    `identity.go` promises but no code enforces yet); and the tree is laid out by
    digest under `~/.arxi/plugins/<id>/<digest>/` with a digest-before-rename step
    that closes the TOCTOU window. Signing lifts no guard; the installer lands with
    its own counterfactuals (the load-bearing one: a one-byte bundle edit re-asks).
    Once signed, the I5 modal mount is the small addition the blocker above
    describes.
  - **I6-installer-core — LANDED (2026-09-27), stacked on the design.** The two
    pieces §I-I puts at the center — extraction as the security boundary and the
    digest-keyed atomic lay-out — are implemented in `internal/ext/installer.go`
    as pure units a signed §I-I needs unchanged. `Installer.Extract` reads a
    `.tar.gz` (stdlib `archive/tar`+`compress/gzip`, no new module) and enforces
    the Decision 3 invariants at *write* time: `filepath.IsLocal` traversal
    refusal, regular-files-and-directories-only (mirroring `PackageDigest`'s
    symlink/non-regular refusal so a bundle that would fail the digest walk is
    rejected earlier), and a shared decompression byte budget plus an
    entry-count cap. `Installer.LayoutByDigest` is Decision 4: digest the temp
    tree, then atomic-rename to `root/<id>/<digest>/` — digest-before-rename
    closes the TOCTOU window, keyed-by-digest makes identical bytes idempotent.
    Counterfactual-tested (traversal vs benign, symlink vs regular, byte and
    entry caps exact at the boundary, one-byte-change re-keys the digest; the
    traversal guard proven load-bearing by reverting it). **Remaining** before
    the modal mount: a behavioral-aware manifest validation path (`Validate()`
    refuses every behavioral manifest today, H2 — a decision to settle at
    signing), the in-package `executable` resolution check, the HTTP archive
    fetch at the `cmd/` edge, and the loop wiring into `supervisor.Mount`.
- **I6** [I5] Gate B (tools): a plugin-by-link teaches the agent a tool, running
  as its own process, under the consent contract. Its package-delivery half is
  the §I-I installer above (drafted; extraction+lay-out core landed); the
  tool-door half remains.
  - **I6-behavioral-validate — LANDED (2026-09-27).** §I-I step 3, now that the
    design is signed (§I-I merged): `Manifest.ValidateBehavioral` (`behavioral.go`)
    is the installer's gated door — the one validation path that accepts a manifest
    with an `executable`, the exact manifest `Validate` refuses under H2. It is a
    second entry point rather than a flag on `Validate` because lifting the H2
    refusal is a capability owned by a named caller (the installer, which owns the
    consent gate and supervisor), not a boolean anyone can pass. It reuses the
    identity, token-block and mount validators unchanged; the two deliberate
    differences are that the executable is required (a package with nothing to run
    belongs on the H6 `/ui plugin add` path) and `checkEmpty` is skipped (a
    behavioral plugin whose whole contribution is its process mounts nothing).
    `validateExecutablePath` finally enforces the in-package rule `identity.go`
    promised — an `executable` at `../x` or `/usr/bin/x` names bytes `PackageDigest`
    never read — reusing the same `filepath.IsLocal` predicate `Extract` applies to
    every tar entry, so the two cannot drift on what "inside the package" means.
    Counterfactuals run: the door lifts exactly the executable refusal (both halves
    over one manifest), the in-package and require-executable guards each proven
    load-bearing by reverting them. Remaining for the I5 modal mount: the HTTP
    archive fetch at the `cmd/` edge (§I-I step 1), then wiring `supervisor.Mount`
    with a real digest.
  - **I6-install-orchestration — LANDED (2026-09-28).** The rest of §I-I's flow,
    threaded and testable without a terminal, on an integration branch that stacks
    the still-unmerged installer-core, behavioral-validate and archive-fetch PRs so
    the whole path builds and tests together while each dependency PR stays
    independently reviewable. Four pieces:
    (1) `Installer.InstallFromBundle` (`installer.go`) is the offline half — Extract
    (write-time refusals) → read `plugin.json` from inside the tree → ValidateBehavioral
    → confirm the executable actually ships as a regular file (the gap validation
    cannot close: it proves the path is in-package, not that the file exists) →
    LayoutByDigest — with one deferred staging cleanup covering the refusal,
    idempotent-skip and moved-away exits.
    (2) `supervisor.Config.Root` resolves a manifest's *relative* executable against
    the installed tree at spawn, as an absolute path rather than chdir-plus-relative
    because Windows `CreateProcess` resolves a relative program name against the
    parent's directory, not the child's; the manifest keeps the relative path so the
    consent identity stays machine-independent (empty Root leaves every existing
    caller untouched).
    (3) `installBehavioralPlugin` (`plugin_install.go`) is the cmd-edge thread —
    archive fetch → InstallFromBundle → consent-gated `supervisor.Mount` with the
    real `PackageDigest` — with the consent `Prompt` injected so it is testable
    headless; `pluginsRootPath` resolves `~/.arxi/plugins` (ARXI_PLUGINS_DIR
    override) so the staging rename stays on one filesystem.
    (4) Counterfactuals run: the executable-exists guard reverted fails its test, the
    Root join reverted fails the relative-exec spawn, and the cmd thread's rejection
    path is proven to lay the package out yet spawn and register nothing. **Remaining
    for I6:** the interactive `/ui plugin install <url>` command in the modal loop —
    parse the verb, drive the consent scene through `consentAnswerForKey` to build
    the `Prompt`, and hold the returned supervisor for `/ui plugin remove` — plus the
    tool-door half (the agent-facing side of Gate B).
  - **I6-install-modal — LANDED (2026-09-28).** The interactive
    `/ui plugin install <url>` command, wired into the loop. Five pieces, each pure
    part pinned before the glue:
    (1) `parsePluginInstall` (`plugin_install_cmd.go`) is the host-level grammar.
    Install is not a source-to-source patch — it fetches a bundle, spawns a held
    process — so it is intercepted in the host before `patch.ApplyWithFetch`, not
    added to the patch surface's closed verb set (which owns the manifest-only
    add/remove). It claims exactly the install lines and refuses a missing/extra URL
    itself so the user sees the real problem, not patch's "unknown subcommand".
    `parsePluginRemoveID` is its sibling: a peek, so the host can `Close` a
    behavioral plugin's process (which the pure patch surface cannot) while a
    declarative remove still flows to `patch.Unmount`.
    (2) `installBehavioralPlugin` now takes a prompt factory `promptFor(digest)`
    rather than a bare `Prompt`: `ext.ConsentScene` renders the digest as half the
    identity a grant binds to, but the digest is known only after `InstallFromBundle`
    — inside the thread — so the factory is the one place it can reach the screen
    without duplicating the fetch→install→mount order at the call site.
    (3) `installModal` (`install_modal.go`) is the loop-side consent state: `busy`
    (an install is in flight) and `consent` (a screen is up capturing keys).
    `handleKey` routes every non-panic key through `consentAnswerForKey` and ALWAYS
    consumes it while a screen is up, so a `y` meant for the prompt cannot leak into
    chat; it answers the blocked worker on y/r/n/Esc and leaves the prompt standing
    otherwise.
    (4) `startInstall` runs the whole thread on a worker goroutine (a hung fetch must
    never freeze the loop or the panic gesture, invariant 6) and bridges consent back
    over channels: the `Prompt` sends its digest-built screen on `consentReqCh` and
    blocks on a per-request reply the loop answers through the modal; the final result
    lands on `installDoneCh`.
    (5) The loop wiring: two select cases (show the screen; clear `busy` and hold the
    supervisor by id / name a rejection distinctly from a failure), the modal key
    branch placed right after the Ctrl-C check (escape hatch uncapturable) and before
    every other handler (no answer leaks to chat), `repaint` swapping to the consent
    doc, and shutdown closing every held supervisor. Counterfactuals run: making an
    undecided key fall through fails the standing-prompt test; the bridge test proves
    a real `ConsentScene` reaches the loop and a rejection is reported with the
    package laid out and nothing spawned. (`-race` could not run in the sandbox: no
    C toolchain; the bridge has no shared mutable state, the consent doc is built on
    the worker then only read by the loop after the channel handoff.)
- **I6-store-render — LANDED (2026-09-28).** The store→render half: a mounted
    plugin's frames, drained into the host-owned `PluginStore` by the pump goroutine
    (I3 `DrainInto`), now reach the render walk and trigger a repaint. Two pieces,
    the pure part pinned first. (1) `PluginStore.Changed()` (`internal/ext/store.go`)
    is the store's analogue of the animation tick: the pump writes the store from its
    own goroutine, so without a wake-up a pushed value sits unseen until an unrelated
    event repaints. It is a coalesced, buffered-at-1, non-blocking wake-up posted
    behind every committed write (Ingest / SetLiveness / DropPlugin) — a hung loop
    cannot back-pressure the pump, and under last-value-wins a dropped signal names a
    repaint already scheduled. It sits after the validation gate, so a refused frame
    (which changed nothing) posts nothing. It carries no value and dispatches no
    gesture, so selecting on it cannot capture the escape hatch (invariant 6), and it
    is a reason to repaint, never a fold event (invariant 2). Counterfactuals run:
    removing the Ingest signal fails the value-wake test; widening the buffer past 1
    fails the coalescing test; signalling before the validation gate fails the
    refused-frame test. (2) The loop wiring (`cmd/arxi-tui/main.go`): the repaint
    closure sets `r.PluginValues = pluginStore.Snapshot()` so the renderer resolves
    `<plugin-id>.*` and `ui.plugin.<id>` binds from the live store (the resolver
    support landed in I3), fed as a SEPARATE input from `fold.State` (ADR-0003, so a
    stranger's process is never an authority over the run log); and a new select case
    `case <-pluginStore.Changed(): repaint()` wakes the loop when a frame arrives. A
    nil snapshot (no plugin mounted) resolves every plugin bind to the placeholder,
    so no non-plugin scene moves — the full suite including goldens stays green. The
    loop-wiring lines rest on tested constituents (store `Changed()`/`Snapshot()`,
    the I3 renderer `PluginValues` resolution); `run` owns a real tty with no
    fake-tty harness, so the glue is verified by build + the pieces it composes, as
    with the earlier I6 loop-wiring commit. **Remaining for I6:** the tool-door half
    (the agent-facing side of Gate B).
  - **I6-tooldoor-design — DRAFTED (2026-09-28), awaiting signature.** DESIGN-BLOCK-I.md
    §I-J drafts the last unbuilt piece of Gate B: how a mounted plugin teaches the
    **agent** a tool it can call, as distinct from an I3 `bind` (a scene projection
    the agent never reads) and an I4 `action` (user-originated, plugin need not reply).
    A tool is an agent-originated request/response that crosses the two-channel
    boundary, so the host is the broker. The draft names the hard dependency first —
    the agent-facing half is blocked on Block M (no real agent wired yet) and a
    coordinated `arxi` core surface-version bump for a tool-injection verb, neither of
    which this host can invent — and splits out a **plugin-facing half buildable and
    testable headless today**, exactly as I4 built action routing before I5 mounted a
    live plugin: a manifest `tools` array + `ToolDecl`, a new `tools.register`
    capability gated at I5, and `Supervisor.CallTool` (the await-a-reply sibling of
    I4's fire-and-forget `SendAction`, reusing the id-correlated `action`/`ok` channel).
    Five decisions with forks resolved to recommended defaults; new vocabulary
    (manifest `tools`, `ToolDecl`, `tools.register`) named for signing; the core
    surface verb named as a dependency, not signed here (the fabricated-core mistake
    §I-I refused with a fabricated digest). **Remaining for I6:** owner signature on
    §I-J, then the buildable plugin-facing half with its counterfactuals; the
    agent-facing half stays blocked on Block M + the surface bump.

### Block J — Phase 3: the community installer as a scene (Scene 7) [H]

- **J1** Preview mode: render unsatisfied binds as placeholders from
  `community.*` manifest mocks (engine contract, Q16).
- **J2** Registry as a JSON index in a repo (no servers) — Q17.
- **J3** [J1,J2] Installer scene: entry list + markdown preview panel + search
  input + `i` to install.
- **J4** [J3,I5] Share complete bundles (scene+theme+plugins, one consent
  screen).
- **J5** [J3] Freeze the Scene 7 golden.

### Block K — Phase 4: behavior + wasm [I]

- **K1** Gate C (behavior hooks): gate tool calls, prompt tweaks, custom
  compaction, with identity-ordered stacking.
- **K2** Gate D (providers): wire provider plugins (nearly free; exists in the
  core).
- **K3** [K1,K2] Self-extension over the gates the user opened, only on user
  order (diff + attribution).
- **K4** Costed ADR on embedded wasm (wazero) for renders that are none of our
  nodes (Scene 14 / Q14).

### Block L — Distribution and install

Nothing here exists yet (audit correction: no `install.sh`, no CI, no
per-platform build tooling). All aspirational in `docs/PLAN.md:107`.

- **L1** `install.sh` in the repo, served from the release host.
- **L2** Per-platform static builds (`CGO_ENABLED=0`): linux, macos, windows,
  android-arm64 (Termux).
- **L3** [L2] GitHub Releases publication + artifact wiring.
- **L4 — DONE (correct-the-claim).** The OSC 11 claim (correction 3) is
  resolved by correcting the docs/comments to the relative dim/bright SGR
  mechanism that ships: `README.md`, `cmd/arxi-tui/main.go`, `docs/TOKENS.md`,
  `docs/SCENES.md`, `docs/PLAN.md`. `factory.go`/`theme.go` were already correct.
  Implementing an explicit OSC 11 query is left as an optional future
  enhancement (noted at each site), not a blocker.
- **L5 — DONE.** `.github/workflows/ci.yml` runs `go build`/`vet`/`gofmt`/
  `go test -count=1 ./...` on a `windows-latest` + `ubuntu-latest` matrix,
  `fail-fast: false`. The double-Ctrl-C escape-hatch tests run inside
  `go test ./...` on Windows (correction 4). Go pinned to 1.25.0.

## Recommended critical path

1. **Block 0.3** (gitignore the binary) — trivial, do first.
2. In parallel: **C2-C5** (2 events + doc fixes; nearly free), **A**
   (run the eval — gates Phase 2), **M** (core integration — so the TUI drives
   a real agent), and **D** (paper design — touches no code).
3. **B** once A6 gives the ship verdict.
4. With D signed: **E → F → G** (the Phase 3 work that was blocked on paper).
5. **H → I → J** (declarative plugins, then behavioral, then the installer).
6. **K** (Phase 4) and **L** (distribution) last, though L can start as soon as
   there is something shippable.

Architect's note: **A and M unblock the product** — without A the self-extend
ship decision cannot be made, and without M the TUI never draws a real agent;
put them first after Block 0. **D is the bottleneck for all of Phase 3**: four
paper decisions (`row.*`, add/move addressing, per-node view-state, `[anim]`)
unblock six implementation blocks.
