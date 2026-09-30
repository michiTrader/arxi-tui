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
- **0.3 — DONE (2026-09-29).** `.gitignore` now ignores the whole `/bin/` tree as
  a directory rather than enumerating each artifact path. The enumerated list had
  let `bin/arxi-tui.exe~` — a 4.9 MB editor/build backup whose trailing-tilde name
  matched none of the exact patterns — get committed as a tracked binary; it is
  untracked (kept on disk) and the directory rule closes the recurrence (correction
  7).

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
  - **M1c pure core — BUILT (2026-09-29, PR #123).** The network-free half landed
    first per the block pattern (M1a/M1b captured the schema; every J increment
    landed its pure core ahead of the loop wiring). `NDJSONDriver.SubmitRunStart`
    (`internal/driver/ndjson.go`) is the `run.start` wire method, the sibling of
    `SubmitPrompt`: `RunStartParams` always sends the required `actor`/`prompt`/
    `budget` and omits each optional (`max_turns`/`model`/`sim`) when unset so the
    core applies its own default rather than the client sending a zero it never
    chose; `workspace` is deliberately absent (its empty string is an illegal
    fourth enum member — the core defaults it to `auto` on omission). It returns
    `RunStartResult` (the core's `SubmitResult`: `job_id`/`accepted_seq`/`status`,
    the shape M1a captured from arxi's `serve_lifecycle_test.go`), turns `ok:false`
    into the same `*Refusal` error `SubmitPrompt` does rather than a zero-valued
    result that reads as a started run, and fails loud on `ok:true` with an empty
    `job_id` — an unreachable run is worse than a refusal, which at least says so.
    Pinned by `run_start_test.go` (5 tests) with counterfactuals run by hand:
    neutering the empty-`job_id` guard, returning nil on `ok:false`, and always
    sending `sim` each fail exactly their test; a set-optionals test is the
    counterfactual of the omission one (an optional the caller sets must reach the
    wire). **Deferred (the impure half):** the `serveDriver`/loop wiring that calls
    it needs the three product decisions M1b flagged (actor/blueprint, per-turn
    budget, sim default), which are the user's call; that is M2's live round-trip
    territory.
- **M2** [M1c] Verify a real round-trip against a live `arxi serve`, re-reading
  the hello's `implemented` list at connect and gating on it.
  - **M1c config resolver — BUILT (2026-09-29, PR #124).** The three product
    decisions M1b flagged are now answered by the user and encoded in
    `resolveRunStartParams` (`cmd/arxi-tui/run_config.go`), the second
    network-free half after the pure `SubmitRunStart` (#123): **actor** is
    plug-and-play — a baked `defaultActor` so a session starts with no config,
    overridable by `ARXI_ACTOR`, and returned as a status-bar label so the
    connected agent is never invisible; **budget** defaults to a conservative
    `1.0`, overridable by `ARXI_BUDGET`; **sim** is off by default (a live
    model), opt-in via `ARXI_SIM`. Malformed `ARXI_BUDGET` (unparseable or
    non-positive) and unrecognised `ARXI_SIM` are refused **by name** rather
    than silently defaulted — with a live model as the default, guessing past a
    bad value spends against a number the user never wrote. Pinned by nine tests
    (`run_config_test.go`) with `getenv` injected (no `os.Setenv`, parallel-safe)
    and all three refusals' counterfactuals run by hand; the malformed-budget
    test was strengthened after measuring that the non-positive guard downstream
    silently covered it. **Still deferred (the live half of M2):** calling the
    resolver from `serveDriver`, re-pointing `LogFollow` at the new run's log dir
    from the returned `job_id` (the run.start→job_id→log-path convention is a
    wire fact only a live `arxi serve` can confirm), surfacing the actor label in
    the status-bar scene, and confirming the `defaultActor` name against a kernel
    that actually ships it — a wrong default is rejected at the core's agent
    store, so plug-and-play's default *name* is provisional until this check.
  - **M2 log-path piece — BUILT (2026-09-29, PR #125).** The third pure piece
    after #123's wire method and #124's config resolver: `runLogPathForJob`
    (`cmd/arxi-tui/run_log_path.go`) builds the event-log path a `run.start`
    `job_id`'s log follows — `<runsRoot>/<job_id>/events.ndjson` — as the exact
    inverse of `serveDriver.runID`, so `runID(runLogPathForJob(root, id)) == id`
    and the run the TUI creates is *followed* and *addressed* under one id, not
    two. The `<runsRoot>/<id>/events.ndjson` layout is the wire fact only a live
    `arxi serve` can confirm; it lives in one function so that confirmation
    touches a single site, the way `runID` keeps the forward derivation to one.
    An empty `job_id` is refused rather than yielding `<runsRoot>/events.ndjson`
    (the runs root itself, no run's log) — the second line behind
    `SubmitRunStart`'s empty-`job_id` guard. `defaultRunsRoot` is `~/.arxi/runs`,
    the parent of `openServeDriver`'s existing `~/.arxi/runs/last` default, so the
    two derivations agree on where runs live. Pinned by `run_log_path_test.go`
    (round-trip against `runID`, the per-job-dir layout, the empty-`job_id`
    refusal) with the derivation counterfactual run by hand: dropping the
    `job_id` directory fails both the round-trip and the layout tests. **Still
    deferred (the impure live half):** restructuring `openServeDriver` to spawn
    serve → handshake → `resolveRunStartParams` → `SubmitRunStart` → follow *this*
    run's log via `runLogPathForJob`, surfacing the actor label in the status
    bar, gating on the hello's `implemented` list, and confirming the
    `defaultActor` name and the log-path layout against a live kernel.
  - **M2 hello gate — BUILT (2026-09-29, PR #126).** The fourth pure piece after
    #123's wire method, #124's config resolver and #125's log-path derivation:
    `requireRunStart` (`cmd/arxi-tui/run_start_gate.go`) reads the hello the core
    sent at connect and decides whether this build can begin a run *at all*,
    before `openServeDriver` commits to resolving params and following a log that
    no run would ever write. The check is on the hello's `implemented` list, not
    its `types` list — the distinction M1b paid for: `run.prompt` and `run.steer`
    are both *declared* in surface v1 and both answer `not_implemented` on this
    build, so a host that trusted `types` would send `run.start` into a kernel
    that never executes it and hang forever on a log never created. Three refused
    states kept distinct because their remedies differ — a **nil hello** (the
    handshake has not run, a caller-ordering bug), an **undeclared** `run.start`
    (the connected surface is not the v1 run vocabulary this host speaks, the
    wrong kernel), and a **declared-but-unimplemented** `run.start` (the right
    surface on a build that has not wired the executor, named as
    `not_implemented` so it reads permanent not transient). Pinned by
    `run_start_gate_test.go` (the accepting state + the three refusals) with the
    counterfactual run by hand: gating on `types` instead of `implemented` (the
    M1b bug) fails exactly the declared-but-unimplemented test and nothing else.
    **Still deferred (the impure live half):** calling this gate from
    `openServeDriver` between the handshake and `SubmitRunStart`, the
    `resolveRunStartParams` → `SubmitRunStart` → `runLogPathForJob` follow
    sequence, surfacing the actor label in the status bar, and confirming the
    `defaultActor` name and the log-path layout against a live kernel.
  - **M2 join — BUILT (2026-09-29, PR #127).** The fifth network-free piece
    joins the four that landed alone: `startRun` (`cmd/arxi-tui/run_start.go`)
    sequences `requireRunStart` → `resolveRunStartParams` → `SubmitRunStart` →
    `runLogPathForJob` in the one order that is both correct and testable without
    a subprocess — gate on the hello *first* (a declared-but-unimplemented
    `run.start` is refused before a prompt is ever spent), resolve session config
    (a malformed `ARXI_BUDGET`/`ARXI_SIM` refuses by name), create the run, then
    derive the follow path from the returned `job_id` so the TUI follows the run
    it just created. It takes the session's first prompt as the run's first
    prompt (the one-`run.start`-per-turn mapping M1c established, since
    `run.prompt`/`run.steer` have no executor on this build) and returns the log
    path plus the actor label for the status bar. This is the join analogous to
    J4's `planBundleCompose` (#121): the pieces were proven alone, this proves
    their composition. Pinned by `run_start_test.go` (5 tests over a fake
    `runStarter`) with the ordering counterfactual run by hand: submitting before
    the gate/config fails exactly the gating-before-submit, config-before-submit
    and param-forwarding tests, and nothing else. **Still deferred (the impure
    live half):** calling `startRun` from a restructured `openServeDriver`/
    `serveDriver.SubmitPrompt` (the run.start round-trip moves off boot and onto
    the first user line, since it needs a prompt), re-pointing `LogFollow` at the
    returned path, surfacing the actor label in the status-bar scene, and
    confirming the `defaultActor` name and the `<runsRoot>/<job_id>/events.ndjson`
    layout against a live `arxi serve` — the one thing only a real kernel confirms.
  - **M2 serve-driver live half — BUILT (2026-09-29, PR #128).** The impure
    wiring the join left to the loop, landed the way the I6/J loop-wiring commits
    were (verified by build + the pieces it composes; the sandbox has no live
    `arxi serve`). `serveDriver` (`cmd/arxi-tui/serve_driver.go`) now calls
    `startRun` against a real subprocess on the first user line instead of posting
    `run.prompt` to a run identified by a fixed log directory. The structural fact
    that forced the restructure: `run.start` **creates** a run, and a run's event
    log does not exist until the run does, so log-follow cannot be armed at boot
    the way the old fixed-path follow was (`ARXI_RUN_DIR`/`~/.arxi/runs/last`,
    which only worked when a run had already been started *outside* the TUI). On
    this build `run.start` is the only verb that can create a run
    (`run.prompt`/`run.steer` are declared-but-unimplemented, M1b) and it needs the
    prompt — so the round-trip and its log-follow move off boot and onto
    `SubmitPrompt`. `openServeDriver` now only spawns, handshakes and hands the loop
    a relay channel; each `SubmitPrompt` runs the M2 sequence via `startRun` (gate
    on hello → resolve config → `run.start` → derive the follow path from the
    returned `job_id`), arms `LogFollow` on that path, and relays the created run's
    events onto the relay channel the loop reads. Each prompt starts a fresh run
    (M1c option a: one `run.start` per turn, since this build cannot steer),
    cancelling the previous run's follow first so two logs never relay at once; the
    relay is never closed here because a closed `eventCh` tells the loop the session
    ended and a session outlives any one run. `follow` and `rs` are seams
    (`followFunc`, `runStarter`) so the sequencing is pinned without a subprocess:
    `serve_driver_test.go` (5 tests) proves the run-it-created follow end to end,
    that a refused `run.start` arms no follow, that a follow failure surfaces named
    by the run, the per-turn follow swap, and Close's cancel+teardown — with two
    counterfactuals run by hand (neutering the previous-run cancel fails the swap
    test; neutering the relay copy fails the end-to-end relay test). `runID`/`logPath`
    are kept (the round-trip with `runLogPathForJob` still holds, now via
    `runIDFromLogPath`) so a later attach/show/cancel verb (M3) can address the run
    the TUI is following. **Still deferred:** surfacing the actor label in the
    status-bar scene (needs a signed host view-state bind and a scene row — its own
    increment; the label is captured on the driver at the moment it is known), and
    confirming the `defaultActor` name and the `<runsRoot>/<job_id>/events.ndjson`
    layout appearing on disk against a live `arxi serve`.
  - **M2 actor-label — BUILT (2026-09-29, PR #129).** The first of the two
    deferred pieces above: the run's actor now shows in the SOBRIA status bar.
    `host.run.actor` is signed in `docs/BINDS.md` §4.3 as host view state (not a
    run-state projection) and accepted by `signedBinds` — the host resolves the
    actor from the `run.start` config it sends (`resolveRunStartParams`), so it
    knows the label one round-trip before any `run.started` could echo it, and
    binding to that resolved value shows the actor the instant the run is
    requested. `fold.State.RunActor` carries it (default empty, pinned by
    `TestViewStateBindsDefaultCorrectly`); `serveDriver.ActorLabel()` exposes the
    label captured on `SubmitPrompt` (the mock driver does not implement the
    optional `actorLabeler`, so a run-less session publishes nothing); the loop
    re-attaches it each frame like `user.input` and blanks it while the slash
    menu is open, so the status row stays the `status.active`/`slash.hint`
    either/or. `resolveBind` projects it (the checked-but-never-drawn class: the
    field existed one function short of the frame until this case was added).
    SOBRIA.json gains a `when: host.run.actor`-gated label + separator, and
    because the golden folds start no run the label is empty and the node does
    not draw — **no default golden moved** (`TestSobriaScene*` pass unchanged).
    `TestSobriaSceneShowsRunActor` renders with an actor and asserts the empty
    counterfactual; `TestServeDriverActorLabelIsEmptyUntilARunStarts` pins the
    getter both directions. **Still deferred:** confirming the `defaultActor`
    name and the `<runsRoot>/<job_id>/events.ndjson` layout on disk against a
    live `arxi serve` (the one thing only a real kernel confirms; the sandbox has
    none), and the same label on the richer MAXIMUM status bar (its own golden
    mutation when a scene there wants it).
- **M3** [M2] The run-addressing verbs after `run.start`
  (`run.attach`/`run.show`/`run.result`/`run.cancel`), each addressing the run
  by the `job_id` `run.start` returned. The serveDriver already keeps that id
  through its log path (`runIDFromLogPath`), so these verbs have a run to reach.
  - **M3 run.cancel pure core — BUILT (2026-09-29, PR #130).** The first
    run-addressing verb, landed network-free ahead of any loop wiring per the
    block pattern. `NDJSONDriver.SubmitRunCancel` (`internal/driver/ndjson.go`)
    is the `run.cancel` wire method, the sibling of `SubmitRunStart`. Its schema
    was read from the arxi source, not guessed: the server dispatch reads
    `stringParam(params, "run")` and `stringParam(params, "reason")`
    (`arxi/cmd/arxi/serve.go` run.cancel entry) onto `hostv1.CancelRequest`
    (`arxi/host/v1/types.go`), and the `result` is a `hostv1.Job` snapshot. So
    `RunCancelParams{RunID, Reason}` sends the run id as the wire param `run`
    (the same name `run.prompt` addresses a run by) and omits `reason` when
    empty; `RunCancelResult` carries the Job's `id`/`status`/`terminal`/
    `cancellation_requested`, because cancel is a **request** (a running job
    answers non-terminal with `cancellation_requested=true`; a finished one
    answers `terminal=true` without it), so the host reads state rather than
    assuming the run stopped. It keeps the same `ok:false`→`*Refusal` contract
    (a cancel of a missing run answers `not_found`), refuses an empty run id
    locally so the failure names the real cause instead of borrowing the core's
    `not_found`, and fails loud on `ok:true` with no job id (an unverifiable
    cancel is worse than a refusal). Pinned by `run_cancel_test.go` (6 tests)
    with counterfactuals run by hand: neutering each of the three guards fails
    its test, and the reason-omission test is the counterfactual of the
    set-reason one. **Deferred (the impure half) — and it is a product decision,
    not just wiring:** `serveDriver.Close` currently cancels only the *local*
    follow goroutine, leaving the kernel run alive, so wiring `SubmitRunCancel`
    into `Close` would make quitting the TUI terminate the run. Whether quit
    means **cancel** the run or **detach** from it (leaving it running, the
    `arxi run attach` + Ctrl-C semantics) is the user's call, the same class of
    decision M1b flagged for actor/budget/sim — so the capability lands now and
    the exit semantics wait for that decision.
  - **M3 run.show pure core — BUILT (2026-09-29, PR #131).** The read verb of
    the run-addressing set, landed network-free ahead of any loop wiring per the
    block pattern. `NDJSONDriver.SubmitRunShow` (`internal/driver/ndjson.go`) is
    the `run.show` wire method, the read complement to `SubmitRunCancel`. Its
    schema was read from the arxi source, not guessed: the server dispatch reads
    `stringParam(params, "run")` onto `hostv1.InspectRequest`
    (`arxi/cmd/arxi/serve.go` run.show entry → `host.Inspect`), and the `result`
    is a `hostv1.Job` snapshot (`arxi/host/v1/types.go`) — the same struct
    run.cancel returns. So `RunShowParams{RunID}` sends the run id as the wire
    param `run` and carries no optionals (an inspect mutates nothing and takes no
    reason). `RunShowResult` projects a **wider** slice of that Job than
    `RunCancelResult` does, and the difference is the point: they read the same
    wire object at different widths because they answer different questions. A
    cancel-acknowledgement needs only to confirm which run was addressed and
    whether the request took hold; an inspect exists to surface run state, so it
    carries `id`/`status`/`terminal`/`turns`/`max_turns`/`spent_usd`/
    `budget_usd`/`cancellation_requested`/`result` — each a field a run status
    line actually shows, not a speculative copy of every Job field. Under-
    projecting here would make run.show a worse run.cancel rather than the status
    query it is. It keeps the same `ok:false`→`*Refusal` contract (a show of a
    missing run answers `not_found`), refuses an empty run id locally so the
    failure names the real cause instead of borrowing the core's `not_found`, and
    fails loud on `ok:true` with no job id (an unverifiable snapshot is worse than
    a refusal). Pinned by `run_show_test.go` (6 tests) with counterfactuals run by
    hand: neutering the wider field tags collapses turns/spend to zero (fails the
    snapshot test), dropping the result tag empties it (fails the finished-run
    test), and removing each of the two guards fails its test. Because run.show
    is a **pure read**, it has no impure half of its own to defer — it is safe to
    call repeatedly — but a *consumer* is deferred: nothing in the loop polls it
    yet, since the status bar is fed by the event log (the fold), not by a
    request-response snapshot. The trigger for wiring it is a surface that needs
    state the event stream does not carry (an explicit `/show`-style command, or a
    reconcile after a dropped follow).
  - **M3 run.result / run.attach — DEFERRED per ADR-0002.** The other two
    run-addressing verbs are deliberately not built, and the deferral is
    architectural rather than "no consumer yet". `run.result` is the core's
    `host.Wait` (a terminal projection: `arxi/cmd/arxi/serve.go` run.result →
    `host.Wait`), and `run.attach`/`event.subscribe` is the streaming path
    ADR-0002 (`docs/PLAN.md:230-280`) weighs against log-follow and keeps as a
    "known, available capability this host has chosen not to use yet". Adopting
    the subscribe path would give the host a second event-ingestion model beside
    log-follow, and the failure mode of the two disagreeing is a wrong frame,
    which this repo holds to be worse than an error; replay (the goldens, the
    eval corpus, the fold's determinism) runs on log-follow and a socket stream
    is not replayable from a file. The trigger for adopting either is the host
    needing a positive end-of-run marker — which `run.result` cannot itself
    provide, since it lands at seq 112 of 122 in the measured run and so is not
    the last event — or reading a log whose directory the host does not own
    (where `pending.commit` is not beside the file, and the server's confirmed
    batches become the only correct source). Neither trigger has fired, so both
    stay unbuilt.


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
- **M4** [M2] The inbox decision verbs (`inbox.approve`/`inbox.reject`/
  `inbox.reply`), the operator's answer to an agent that has blocked on an
  approval or a question. Unlike the run-addressing verbs these are not about a
  run's lifecycle but about a pending *item* on it, so they address a run **and**
  an item. The scene side is already signed: `docs/BINDS.md` freezes the closed
  answer-kind vocabulary `answer:approve`/`answer:reject`/`answer:reply` for
  Scene 8's decision buttons, mirroring these three core verbs, so a button and
  the verb it fires name the same act — what was missing is the driver method
  that sends the verb when such a button is pressed.
  - **M4 approve/reject pure core — BUILT (2026-09-29, PR #132).** The first two
    decision verbs, landed network-free ahead of any `on_press` wiring per the
    block pattern. Both are in this build's hello `implemented` list (verified,
    not guessed), and the schema was read from arxi source: the serve dispatch
    reads `stringParam(params, "run")` and `itemParam(params)` (`item`, falling
    back to `id`) through the `decisionIdentity` guard, `inbox.reject`
    additionally reads `reason`, and `host.Approve`/`host.Reject` each return a
    `hostv1.Job` snapshot (`arxi/host/v1/host.go`). So
    `SubmitInboxApprove{RunID, ItemID}` and `SubmitInboxReject{RunID, ItemID,
    Reason}` send the run and item as the wire params `run`/`item` and (reject)
    omit `reason` when empty, exactly as run.cancel omits its unset reason.
    `DecisionResult` projects `id`/`status`/`terminal` only — the **narrow** ack
    like `RunCancelResult`, not the wide `RunShowResult`: answering a decision is
    a mutation whose ack confirms which run was addressed and where it went (an
    approved item unblocks the run, a rejection can end it), and a caller wanting
    the full budget/turns projection asks run.show. Both refuse an empty run id
    **and** an empty item id locally (the core's `decisionIdentity` requires
    both) so the failure names the missing identity at the call site instead of
    borrowing the core's unaddressed refusal; both keep the `ok:false`→`*Refusal`
    contract and fail loud on `ok:true` with no job id. A shared `submitDecision`
    helper holds the identical transport contract so the per-verb guards stay in
    the callers. Pinned by `inbox_decision_test.go` (12 tests) with
    counterfactuals run by hand: neutering approve's run-id guard fails its
    empty-run test, neutering its item-id guard fails its empty-item test,
    passing the wrong verb string to `submitDecision` fails the reject-refusal
    type assertion (proving the shared helper echoes the caller's verb), and
    dropping the reason-omit branch fails the set-reason test. **Deferred (the
    consumer):** routing an `on_press` `answer:approve`/`answer:reject` to these
    calls — that is the H8 action-routing seam meeting the driver, its own
    increment. `inbox.reply` (the question-answer verb, which takes free `text`
    and targets a question item rather than an approval) is the remaining
    decision verb, deferred as its own pure-core increment.
  - **M4 reply pure core — BUILT (2026-09-29, PR #133).** The third and last
    decision verb, landed the same way its siblings were. Same schema source:
    the serve dispatch reads `run`/`item` through `decisionIdentity` and the
    answer as `stringParam(params, "text")`, calling `host.Answer` which returns
    the same `hostv1.Job` snapshot (`arxi/cmd/arxi/serve.go`,
    `arxi/host/v1/types.go` `AnswerRequest{JobID, ItemID, Text}`). So
    `SubmitInboxReply{RunID, ItemID, Text}` sends `run`/`item`/`text` and reuses
    `DecisionResult` and the shared `submitDecision` transport. **The one
    distinction from reject:** `text` is sent **unconditionally**, where reject
    omits an unset `reason`. `reason` is metadata about the act of rejecting, so
    its absence records nothing; `text` is the *substance* of the answer, so "the
    operator submitted an empty reply" is a fact the wire must carry rather than
    an omission the core reads as no text field. For the same reason `text` is
    **not** guarded locally — `decisionIdentity` checks only run and item, so the
    core accepts an empty text, and refusing it here would reject a request the
    core would honour (the opposite direction from the run/item guards, which
    refuse only what the core refuses unaddressed). Pinned by six more tests in
    `inbox_decision_test.go` with counterfactuals run by hand: reusing reject's
    omit-when-empty branch for `text` fails `TestInboxReplySendsEmptyTextUnconditionally`
    (the load-bearing guard), neutering reply's item-id guard fails its
    empty-item test, and passing the wrong verb string to `submitDecision` fails
    the reply-refusal type assertion. With this the full closed answer-kind
    vocabulary has its driver half; the deferred consumer (routing `on_press`
    `answer:*` to these three calls, the H8 seam) is unchanged.

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
  - **H8 row-interpolation expand — BUILT (2026-09-29, PR #134).** The
    press-time half of the `{row.<field>}` interpolation `validateOnPress`
    checks at load (Q20), the substitution H8/E4 parked. Extraction
    (`interpolationTokens`) and the schema check already ran at load, and a
    template's `on_press` is stored verbatim (`Action.Arg` keeps the braces)
    because the element it was instantiated for is only known when a specific
    row is pressed; `scene.ExpandRowInterpolation(arg, row)` resolves those
    fields against the pressed row's scope. Pure and network-free, landed ahead
    of the impure focus-ring wiring per the block pattern — it is the keystone
    the deferred consumers wait on (Scene 9's `cmd:/agent {row.id}` dispatch,
    per-element `ext:` args, and eventually `answer:`). Contract: `row` is keyed
    by the full token (`"row.field"`→value), the same keying `resolveBindRow`
    uses, so display and press read one scope; an absent `row.*` field is an
    **error**, not an empty substitution (the validator already refused any
    field the element schema does not declare, so a miss is a scope/schema drift
    and expanding to `""` would address the wrong run — the wrong frame this repo
    holds worse than a refusal); a non-`row.*` `{...}` token is left verbatim,
    exactly as `interpolationTokens` ignores it (it belongs to a later host
    resolver). Pinned by six tests in `action_expand_test.go` with
    counterfactuals run by hand: silently expanding a missing field to `""`
    fails the missing-field test, dropping non-row tokens fails the verbatim
    test, and stopping after the first token fails the every-field test.
    **Still deferred (the impure half):** enumerating a template's instantiated
    rows in the focus ring and recovering the pressed row's scope, so
    `dispatchPress` can call this expander — that is the focus-ring wiring the
    row-click consumers ride on, its own increment.
  - **H8 row-press enumeration — BUILT (2026-09-29, PR #135).** The second pure
    piece toward the row-click consumer: `engine.RowPresses(n, state)` composes
    the two signed halves — `rowScopesFor` (the per-element scopes) and
    `scene.ExpandRowInterpolation` (PR #134) — into the flat, row-major list of
    `RowPress{RowIndex, NodeID, OnPress}` the deferred focus-ring wiring will Tab
    over and the dispatcher will look a pressed `(row, node)` up in. Landed ahead
    of that wiring per the block pattern, so the resolution is network-free and
    testable before a focus cursor exists. Two decisions recorded at the site:
    (1) a pressable row target is `(RowIndex, NodeID)`, never node id alone — an
    authored id repeats across every instantiated row, so `RowPress` carries the
    pair and leaves the `ui.focus` id scheme to the loop that owns it, rather
    than pinning a synthetic-id format the consumer has not yet needed; (2) the
    whole on_press string is expanded, not the parsed arg, because a validated
    on_press carries braces only in its argument region (a brace before the first
    colon is an unknown prefix the validator refused), so whole-string expansion
    equals arg expansion **and** keeps `ParseAction` the single grammar reader —
    reconstructing `"cmd:"+arg` would be a second writer of the prefix
    vocabulary. A `{row.<field>}` the scope lacks is propagated as an error, not a
    dropped press (a silently dropped press is a button that looks present and
    does nothing). Pinned by five tests in `row_press_test.go` with
    counterfactuals run by hand: swallowing the missing-field error fails the
    error test, stopping after the first row fails the per-row test, and skipping
    expansion fails the same test on the on_press value. **Still deferred (the
    impure half, unchanged):** the focus ring enumerating these targets,
    recovering a pressed `(row, node)`, and dispatching its `OnPress` — plus the
    focus-glow render path matching an instantiated row. That is the row-click
    wiring, its own increment, now with both pure halves (`ExpandRowInterpolation`
    and `RowPresses`) in place beneath it.
  - **H8 row-focus key — BUILT (2026-09-29, PR #136).** The third pure piece,
    and the keystone the impure focus ring needs before it can Tab onto a row:
    `rowFocusKey(nodeID, rowIndex)` / `parseRowFocusKey(key)`
    (`cmd/arxi-tui/rowfocus.go`), the encode/decode that lets the single
    `ui.focus` string name one pressable node of one instantiated `row_template`
    row and recover that `(NodeID, RowIndex)` at Enter. It lives in the host
    package because focus is host-owned view state (BINDS.md §4.3), not a signed
    wire/scene format — the same standing as `enterRowKey`, an engine-internal
    clock key. Two decisions recorded at the site: (1) `ui.focus` stays **one
    string**, a row target folded into a synthetic key and unfolded at dispatch,
    so `advanceFocus`, `findPressable` and the `focus_glow` bind are untouched
    rather than rippling a pair through every focus getter; (2) the separator is
    the **NUL marker** `\x00row\x00`, one no author id can contain (the guarantee
    `enterRowKey` documents), so a row key can never collide with an ordinary
    node in the mixed ring **and** `parseRowFocusKey`'s marker test doubles as
    the discriminator the dispatcher branches on (no marker → resolve a plain
    node via `findPressable`; marker → look the `(row, node)` up in
    `RowPresses`). Pinned by four tests in `rowfocus_test.go` with
    counterfactuals run by hand: dropping the row index fails the round-trip and
    per-row-distinctness tests, decoding a marker-less string as a row target
    fails the plain-id rejection, and skipping the integer guard fails the
    bad-index rejection. **Still deferred (the impure half, now the only piece
    left):** the focus ring enumerating template-row targets (interleaving these
    keys with `pressableIDs`), recovering a pressed `(row, node)` and dispatching
    its expanded `OnPress` through `dispatchPress`, plus the focus-glow render
    path matching an instantiated row. All three pure halves
    (`ExpandRowInterpolation`, `RowPresses`, `rowFocusKey`) now sit beneath it.

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
  - **I6-tooldoor-design — SIGNED (PR #99, merged) and plugin-facing half BUILT
    (PR #100).** DESIGN-BLOCK-I.md §I-J signed the last piece of Gate B: how a
    mounted plugin teaches the **agent** a tool it can call, as distinct from an
    I3 `bind` (a scene projection the agent never reads) and an I4 `action`
    (user-originated, plugin need not reply). A tool is an agent-originated
    request/response that crosses the two-channel boundary, so the host is the
    broker. The design named the hard dependency first — the agent-facing half is
    blocked on Block M (no real agent wired yet) and a coordinated `arxi` core
    surface-version bump for a tool-injection verb, neither of which this host can
    invent — and split out the **plugin-facing half, now built and tested headless**
    exactly as I4 built action routing before I5 mounted a live plugin:
      - `ToolDecl` + the manifest `tools` array (`internal/ext`), validated by
        `ValidateBehavioral`: a tool with no name, no parameters, or a duplicate
        relative name is refused, and `tools` without the `tools.register`
        capability is a contradiction refusal (the `consent_required`-with-
        `executable` precedent); `checkBehavioral` refuses `tools` on a declarative
        manifest. `Parameters` stays an opaque `json.RawMessage` the host forwards
        verbatim.
      - `tools.register` in the closed capability set (`consent.go`), a distinct
        power from `actions.register`; it joins the I5 identity tuple automatically
        via the existing exact-set-equality, so a plugin gaining it re-asks.
      - `Supervisor.CallTool(tool, args) (result, error)` — the await-a-reply
        sibling of the fire-and-forget `SendAction`: it writes the id-correlated
        `action` frame under the `tools.register` grant, and the reader routes the
        `{type:ok,id,result}` / `error` reply to the awaiting caller (never
        forwarded on Frames()). The wait is bounded by the reply, `CallTimeout`
        (`ErrToolTimeout`), and Close (`ErrPluginNotLive`), never on the loop
        (invariant 6). A timeout and an `error` reply map to distinct Go errors so
        the agent-facing layer can tell them apart. This is the §I-E `ok`/error ack
        graduating to load-bearing.
    Each beat landed with counterfactuals built and run (AGENTS.md): neutering
    `validateTools` fails all four refusals; removing `tools.register` from the
    closed set fails the grant while the identity test stays green; breaking the
    reader's reply diversion makes the round-trip time out; removing the timer arm
    hangs past the harness ceiling; mapping an `error` reply to `ok` reports a
    failure as success. **Remaining for I6:** the agent-facing half stays blocked
    on Block M (a real agent) + a coordinated `arxi` core surface-version bump —
    the host→core advertisement of granted tools, the core→host tool-call frame,
    and the fold events for a tool call/result — none of which is built against a
    fabricated core (the fabricated-core mistake §I-I refused with a fabricated
    digest).

### Block J — Phase 3: the community installer as a scene (Scene 7) [H]

- **J1** Preview mode: render unsatisfied binds as placeholders from
  `community.*` manifest mocks (engine contract, Q16).
- **J1 — BUILT (2026-09-28, PR #101).** Preview mode: the engine renders a
  not-yet-installed plugin's `<plugin-id>.*` binds as the manifest's declared
  mocks instead of the placeholder (Q16). Two halves, each with counterfactuals.
  (1) Engine: `Renderer.PreviewMocks map[string]string`, a resolver input fed per
  repaint like `PluginValues`/`AnimPhase` (never a `fold.State` field — the mocks
  come from a parsed manifest the host holds while browsing, not the run log, so
  putting them on the fold would make a not-yet-installed stranger an authority
  over it, invariant 2 / fork 3's recommended default). Consulted in
  `resolveBindRow` after the live snapshot and before the fold, so a mounted
  plugin's real frame wins over a mock (§I-G) and an un-mocked bind still degrades
  to the placeholder (the counter-field rule). Because `resolveBindRow` is the one
  resolver both display and `evalWhenRow` flow through, a mock is a truthy string
  for a `when` while a true miss stays the falsy placeholder — one chokepoint, no
  per-node-type/position gap. `child()` propagates it. A nil table reads as empty:
  byte-identical to before J1. (2) ext: `Manifest.PreviewMocks()`, the sibling of
  `pluginScope()` — a projection over the manifest's own `binds` map keyed by the
  same fully-qualified `<id>.<field>` paths the fragment and store use (fork 1's
  recommended default: `community.*` is the conceptual label, the concrete keys
  are the previewed plugin's own binds). A field with no mock is omitted so it
  falls through to the placeholder; `renderValue` is reused so a mock renders
  byte-for-byte as a live frame of the same shape would. Counterfactuals run:
  neutering the resolver's preview lookup fails the substitution and the `when`
  halves; reversing the plugins/preview order fails the live-wins ordering guard;
  removing the empty-mock skip fails the omit guard; the nil/unrelated-table guard
  pins the no-op on a non-previewed bind (the SOBRIA-zero-row trap). **Remaining
  for J:** J2 (registry loader), J3 (installer scene, its interactivity on H8 —
  now DONE — plus J1/J2), J4 (bundle), J5 (Scene 7 golden). J1's Scene 7 preview
  golden is pinned as part of J5.
- **J2** Registry as a JSON index in a repo (no servers) — Q17.
- **J2 — BUILT (2026-09-28, PR #102).** The server-less registry as
  `internal/ext/registry.go`, mirroring `manifest.go`: `ParseRegistry`/
  `ParseRegistryNamed` + `Registry.Validate()` with the same addressed `*Error`
  and offset addressing, and a closed-set `version` (`legalRegistryVersions`, the
  index analogue of `legalProtocols`) — an unknown version is refused, not
  negotiated. `Validate` **never fetches**: it is a pure function over the index
  bytes, refusing an unknown version and, per entry, a missing identity
  (`id`/`name`/`version`/`description`, `id` matching `idPattern`) or a
  `manifest_url` that is absent or **not HTTPS**. HTTPS-only is a security refusal
  (a plaintext or `file://` URL from a public index would let it redirect the
  install to swapped code or a local path — the design's security note).
  `FetchRegistry` is the one network seam, HTTPS-only at the index URL too, with
  the HTTP client injected (`fetchRegistry`) so the fetch path is tested against
  `httptest.NewTLSServer` without the real network. **Counterfactuals run** (each
  disabled and re-run per AGENTS.md): the closed-set version check, the HTTPS
  check on `manifest_url` (both `http://` and `file://`), and the `Validate` call
  inside the fetch path all go silent when reverted; the positive controls and the
  intentional preview-optional/description-required split are pinned too. **What
  J2 does NOT do:** it discovers URLs only — install still flows through the
  existing H6/I5 pipeline (fetch `manifest_url`, full manifest `Validate`, Q15
  consent gate, `patch.Mount`); the registry grants nothing. **Remaining for J:**
  J3 (installer scene — needs J1+J2 **and** H8 action routing for its `i`/search
  interactivity, now DONE — built as PR #103), J4 (bundle, needs I5), J5 (Scene 7
  golden, including J1's preview frame and the nil no-op frame).
- **J3** [J1,J2] Installer scene: entry list + markdown preview panel + search
  input + `i` to install.
- **J3 — BUILT (2026-09-28, PR #103), pure scene function; live interactivity a
  follow-up.** `Registry.InstallerScene() (*scene.Document, error)` in
  `internal/ext/registry_scene.go` is the pure `index -> *scene.Document` J3's
  design names as its testable core, mirroring `patch.Diff.Scene`: a
  `map[string]any` assembled, marshalled and re-parsed through
  `scene.ParseNamed`, so the host authors the installer through the same
  parse+validate path a user's scene takes (ADR-0003). Layout is the two-column
  diff-view shape — left stack: a search `input` then one pressable card per
  entry; right stack: a `markdown` help pane. **Install is H8's `cmd:` action,
  not a new mechanism** (H8 is DONE): each card carries `on_press`
  `cmd:/ui plugin add <manifest_url>` with the `id` `press.go` requires to ring
  it, so Tab-focus + Enter installs through the existing H6 pipeline (fetch,
  Validate, Q15 consent gate); the registry discovers a URL and grants nothing.
  **Three guards, each proven load-bearing by a counterfactual run by hand:** the
  scene validates under SOBRIA and Factory; every entry becomes a node with both
  `id` and the exact `cmd:/ui plugin add <manifest_url>` (dropping either key, or
  the URL, fails it — a card drawn but unreachable, or one installing the wrong
  plugin); every entry's name, description and preview reach the screen (dropping
  any child fails it — the preview markdown especially, which a text-only walker
  would silently miss). The empty index still produces a valid, browsable scene
  (search input, zero cards), pinned so "no plugins yet" and "the build failed"
  cannot look the same. **One deliberate deviation, recorded in the file:** the
  design's recommended default is a live `list` bind + a query-bound search
  input, which needs a new signed `community.*` array bind, a row schema, a fold
  field and host-loop filtering — the interactive half the design defers. This
  follows the design's *primary* testability requirement (pure function + golden
  like `Diff.Scene`, which bakes), so entries are baked as static cards; live
  search filtering and a selection-driven preview pane are the follow-up
  increment that adds the view-state bind. **Remaining for J:** the J3 follow-up
  live loop (the keystroke loop that writes the now-built `community.*` fold
  fields via `FilterEntries` and rebuilds `InstallerScene` to a live `list`/search
  pair, plus the selection→preview pane — the signed bind and its engine
  projection are done), J4 (bundle, needs I5), J5 (freeze the Scene 7 golden,
  including this installer scene, J1's preview frame and the nil no-op frame).
- **J3 follow-up — filter core BUILT (2026-09-28, PR #106).**
  `Registry.FilterEntries(query)` is the pure query-filter the live search box
  will call per keystroke: empty→all entries, otherwise a case-insensitive
  substring match on name or description, nil slice on a true miss (so "no
  results" and "not yet filtered" stay distinct). A faithful port of
  `fold.FilterSlashMatches` with one recorded adaptation — the slash menu matches
  the command name alone, a registry browse searches the two card-rendered fields
  (name + description); the `id`/`manifest_url` are deliberately excluded as text
  the user never reads off the card. Five counterfactuals run by hand (description
  clause, both `ToLower`s, empty→all, nil-not-all).
- **J3 follow-up — `community.*` view state SIGNED (2026-09-28, PR #107).** The
  vocabulary the live search box and entry list will read is now committed on
  paper the way Blocks D/G/H/I signed theirs, before any fold field or loop
  depends on it: `community.query` (search substring), `community.matches` (the
  filtered entries, an array-of-objects row schema
  `row.id/name/version/manifest_url/description/preview`), and
  `community.selected` (the highlighted card), all in the `slash.*` mould
  (BINDS.md §4.3, §4.7). They are host view state written by the installer
  keystroke loop, never an arxi-core event; `community.matches` is the previewed
  plugin's own entries, **not** the `<plugin-id>.*` preview namespace (J1).
  `validate.go` carries them in `signedBinds`/`rowSchemas` so the `InstallerScene`
  builder and the validator agree. **Still deferred (the live half):** the fold
  fields that carry the query/matches/selection, the installer keystroke loop
  that writes them via `FilterEntries`, and the selection→preview pane — all
  gated on the same live-loop surface the I5 modal mount uses. Recorded as
  signed-but-not-projected in `acceptedUnprojectedBinds` (no engine case) and
  `pulseBindsWithoutFoldFields` (no fold field to perturb), the way `ui.hidden`
  was before F3 gave it `State.UIHidden`; every empty-state is a no-op, so no
  golden moved. `TestEverySignedBindIsHandledOrJustified`, the projection-varies
  guards and `TestSignedInventoryMatchesDocument` all stay green on the signing.
- **J3 follow-up — fold fields + engine projection BUILT (2026-09-28).** The
  first live-half increment, now that the I6 modal loop (I6-install-modal /
  I6-store-render) has landed the live-loop surface the signing was gated on.
  `fold.State` gains `CommunityQuery` (string), `CommunityMatches`
  (`[]CommunityMatch` — a new fold-local row struct mirroring
  `ext.RegistryEntry` field-for-field, declared in `fold` rather than imported so
  the fold stays the pure host-owned state ADR-0002 requires) and
  `CommunitySelected` (int), all host view state in the `slash.*` mould the host
  loop will own. `resolveBind` projects `community.query`/`community.selected`
  and `rowScopesFor` instantiates a `row_template` over `community.matches`
  (`row.id/name/version/manifest_url/description/preview`), exactly as
  `slash.*`/`team.members` are drawn. The three binds are therefore removed from
  `acceptedUnprojectedBinds` and `pulseBindsWithoutFoldFields` and
  `community.matches` joins `templateProjectedBinds` — the stale-exemption move
  each guard's own remedy demands, the way `ui.hidden` graduated at F3. Two
  counterfactuals run: `community.query` returning a constant fails
  `TestEverySignedBindProjectionVariesWithItsFoldField`; a `rowScopesFor` that
  ignores `state.CommunityMatches` fails
  `TestEverySignedCompositeBindIsDrawnOrRecordedUnprojected`. Every field
  defaults empty and no shipped scene binds `community.*` (the `InstallerScene`
  still bakes static cards), so the full suite including goldens stays green.
  **Still deferred:** the installer keystroke loop that writes these fields via
  `Registry.FilterEntries` and rebuilds `InstallerScene` to a live `list`/search
  pair, and the selection→preview pane — the parts that touch the real-tty loop.
- **J3 follow-up — live installer document BUILT (2026-09-28).** The next
  increment after the projection landed: `ext.LiveInstallerScene()`, the
  community installer as a *live* document that binds the search `input` to
  `community.query` and the entry `list` to a `row_template` over
  `community.matches`, rather than baking static cards the way the frozen
  `InstallerScene` (Scene 7) does. It takes no index — the entries now live in
  the fold, written by the keystroke loop from `Registry.FilterEntries` — so the
  structure is a pure constant and the folded matches are the content (ADR-0002:
  scene says form, fold says content). It is pinned before the loop mounts it,
  the same order `TICKER.json`/`COMMUNITY.json` pin a build output ahead of its
  consumer: `testdata/COMMUNITY-LIVE.json` is byte-for-byte the builder output
  (`TestLiveInstallerJSONIsTheBuilderOutput`), and `.frame`/`.styled` pin the
  frame rendered over a **non-empty** folded `community.*` state — the coverage
  the increment exists for, since no test rendered `community.matches` into a
  frame before, leaving the PR #108 projection proven only at the unit resolver.
  `TestLiveInstallerDrawsEveryMatch` witnesses each folded entry reaching the
  frame through the template; counterfactual run: neutering `rowScopesFor`'s
  `community.matches` case empties the list and it fails on the first entry.
  **Three affordances are deliberately deferred, each the engine capability it
  needs:** (1) the search input shows its placeholder, not the typed query —
  `renderInput` draws a bound value only for `user.input` today, so displaying
  `community.query` is its own engine change; (2) the right column stays the
  static help pane — a selection-driven preview needs the selected entry's fields
  as new signed absolute binds (`community.selected.preview` and friends); (3) no
  selection highlight — the row's index inside its own scope is not carried by
  any `row_template` today. **Still deferred after this:** those three, plus the
  keystroke loop that writes the fold fields via `FilterEntries` and swaps this
  document onto the display (at which point the Scene 7 golden moves to this
  builder in its own mutation family and the static `InstallerScene` retires).

- **J3 follow-up — search input shows the typed query BUILT (2026-09-28).** The
  first of the three affordances the live installer deferred: `renderInput` drew
  a bound field's value only when `n.Bind == "user.input"`, so the installer's
  search box — bound to `community.query` — showed its placeholder even after a
  query was folded. It now resolves whatever bind the input carries through
  `resolveBind`; `user.input` keeps its live `UserInputCaret`, and every other
  view-state bind rests the caret at the end of the resolved text, since the fold
  carries no caret index for it yet (the keystroke loop is where a per-bind caret
  would land). A resolved `placeholderValue` collapses to the empty line, so an
  unresolved bind still shows its hint and never draws `[…]`. The `COMMUNITY-LIVE`
  `.frame`/`.styled` goldens move from `search community plugins` to the folded
  query `tick` — the frame the increment produces. `TestLiveInstallerSearchInput-
  ShowsTheQuery` drives `community.query` with a value present in no row and
  asserts both directions (the query shows; an empty query restores the
  placeholder), so it fails on the pre-change engine; counterfactual run:
  neutering the view-state branch to draw no value returns the placeholder and
  fails both the query witness and `TestLiveInstallerDrawsEveryMatch`'s
  placeholder-absence check. **Still deferred:** the two remaining live affordances
  (the `community.selected.preview` pane, which needs new signed absolute binds,
  and the row-selection highlight, which needs the row index inside its scope),
  plus the keystroke loop that writes the fold fields via `FilterEntries` and
  swaps this document onto the display.

- **J3 follow-up — row-selection highlight BUILT (2026-09-28).** The second of
  the three deferred affordances: the live installer now draws the
  `community.selected` row bright. The blocker recorded above was "the row index
  inside its scope," and the resolution is to answer the comparison instead of
  exposing the index: this engine's `when` is a bare truthiness test with no
  operator, so a scene cannot write `row.index == community.selected` itself.
  `rowScopesFor` synthesizes a per-row `row.selected` boolean
  (`boolField(i == state.CommunitySelected)`) — the one `row.*` field on
  `community.matches` with no `CommunityMatch` column behind it, signed in
  BINDS.md §4.7 and `rowSchemas` with the argument for why it is a boolean and
  not an integer — and `liveInstallerList` wraps the name in a `row` of a
  `when: "row.selected"` caret glyph and the name text, exactly the shape Scene 9
  uses for `when: "row.busy"`. A marker rather than a brightened token because
  `when` shows or hides a node, it does not switch one node's token between two
  values. The `COMMUNITY-LIVE` goldens move (Scene 7 variant, its own mutation
  family): the default `community.selected=0` marks the first entry.
  `TestLiveInstallerHighlightsTheSelectedRow` drives the cursor across both rows
  so the marker must *move*, and asserts it on the selected row's own line and
  absent from the other — failing both when no row is marked and when every row
  is. Counterfactual run: `boolField(...&&false)` drops the marker and fails
  "not marked"; `boolField(...||true)` marks every row and fails "also marked";
  render.go restored, probe never committed. **Still deferred:** the
  `community.selected.preview` pane (new signed absolute binds) and the keystroke
  loop that writes the fold fields via `FilterEntries` and swaps this document
  onto the display.

- **J3 follow-up — selected-entry preview pane BUILT (2026-09-28).** The third
  and last deferred affordance: the live installer's right column now previews
  the entry `community.selected` points at, replacing the static help paragraph
  (`installerHelp`) that stood in for it. The blocker recorded above was "new
  signed absolute binds," and this signs them: `community.selected.{name,version,
  preview}` (BINDS.md §4.3, `signedBinds`). `community.selected` is an *index*, so
  `resolveBind` resolves it against `community.matches` through a
  `selectedCommunityMatch` helper that returns the zero match when the selection
  is out of range — the empty browse or a frame before the host's first clamp —
  which maps every field to `""` and collapses the pane to blank, the same no-op
  an empty match list gives the list. They are the absolute-bind analogue of the
  `row.*` schema (§4.7): the same entry fields, addressed by the selection rather
  than per row, because a pane outside the `list` has no row scope. Signed as the
  pane consumes them, not the whole namespace ahead of a consumer (`id`,
  `manifest_url`, `description` are resolvable the same way but unrendered, so
  unsigned — the §4.6 rule in the small). `renderMarkdown`'s default case now
  resolves `n.Bind` through `resolveBindRow` instead of drawing `n.Text` alone, so
  the multi-line preview blurb wraps in a markdown pane; a bound markdown node
  previously dropped its bind silently — the checked-but-never-drawn class one
  node type over — and an unbound pane still draws `n.Text`, so no literal-text
  markdown moves. `liveInstallerPreview` stacks the name (header), version (dim)
  and preview (markdown); the `COMMUNITY-LIVE` goldens move (Scene 7 variant, its
  own mutation family): the right column becomes `Community Ticker` / `0.1.0` /
  its blurb (`community.selected=0`). `TestLiveInstallerPreviewPaneShowsTheSelected-
  Entry` drives the cursor across two entries and asserts the selected entry's
  preview blurb is on screen and the other's is not — the preview field is the one
  entry field the list does not draw, so a sentinel there witnesses the *pane*,
  not the list. Counterfactuals run: reverting `renderMarkdown` to `n.Text` drops
  the blurb and fails "did not show the selected preview"; pinning
  `selectedCommunityMatch` to index 0 fails both directions at `selected=1`. The
  highlight test was hardened in the same commit (marker+name frame-wide, since
  the name now appears twice), and the three new scalar binds were added to
  `TestEverySignedScalarBindTheFoldComputesReachesTheFrame`. **Still deferred:**
  only the host keystroke loop remains — it writes the `community.*` fold fields
  via `Registry.FilterEntries` on every keystroke, moves `community.selected` on
  ↑/↓, and swaps this document onto the display; when it lands the Scene 7 golden
  moves from the static build to `LiveInstallerScene` in its own mutation family
  and the static `InstallerScene` retires.

- **J3 follow-up — keystroke-loop core BUILT (2026-09-28, PR #113), impure wiring
  BUILT (2026-09-29).** The pure core landed first — `cmd/arxi-tui/installerBrowse`
  — the same order `FilterEntries` landed the filter core ahead of the loop that
  calls it. `installerBrowse`'s transitions are pure so they are proven without the
  tty, exactly as `consentAnswerForKey` is the pure pinned core of the consent
  modal: `matches()` runs `Registry.FilterEntries` over the query and converts each
  `ext.RegistryEntry` to `fold.CommunityMatch` — the ext→fold boundary `fold.go`
  names as the host's job — and the signed BINDS.md §4.3 behaviour (empty query
  lists everything, clamp on filter, wrap on ↑/↓, reset to 0 on open) is pinned by
  eight tests over a three-entry fixture, each with a named consequence, with
  counterfactuals run by hand.

  The impure wiring that drives it has now landed too: `/ui plugin browse <url>`
  (`parsePluginBrowse`, `cmd/arxi-tui/plugin_install_cmd.go`) is intercepted in the
  host before the patch surface, like install and remove, because opening the
  installer fetches a registry index, swaps `LiveInstallerScene` onto the display
  and drives a keystroke loop — none of which the pure patch transform can do. The
  fetch runs on a worker (`startBrowseFetch`, `installer_browse_open.go`, using the
  HTTPS-only bounded `FetchRegistryWithClient`) so a hung registry never freezes the
  loop or the panic gesture (invariant 6); the loop publishes
  `installerBrowse.publish` while `browse != nil` and routes every non-panic key
  through `routeBrowseKey`, which maps query runes→`typeRune`, ↑/↓→`moveUp`/`moveDown`
  (wrap), Backspace→`backspace`, Esc→close, and Enter→the selected entry's
  `manifest_url` handed to `startInstall` — the same install path a typed
  `/ui plugin install` takes, so a pressed card and a typed line cannot install
  different bytes.

  With the consumer live, the Scene 7 golden moved from the static
  `Registry.InstallerScene` build to `ext.LiveInstallerScene` in its own mutation
  family (`testdata/COMMUNITY.json`/`.frame`/`.styled` regenerated), and the static
  builder retired — its `installerCard`/`installerHelp` deleted, `installerNotice`
  kept as shared chrome in `registry_scene.go`, and `community_live_test.go` renamed
  to the `TestCommunityScene*` family. The nested-style sweep absorbed the live
  list's `row_template` (it now folds `community.matches`); the stack/row exemption
  records the one surface that owns no style token, with a hand-run
  counterfactual.

- **J4 — pure core BUILT (2026-09-28, committed f438423 / fe3ec73); live wiring
  TODO [J3,I5].** Share complete bundles (scene+theme+plugins, one consent
  screen). The J-block pattern held: the pure, network-free core landed first, the
  way J2's `ParseRegistry` and J3's `LiveInstallerScene` did. `internal/ext/bundle.go`
  is the `bundle/v1` parser and validator — `ParseBundle`/`ParseBundleNamed` +
  `Bundle.Validate`, mirroring `registry.go`/`manifest.go` (closed-set
  `legalBundleVersions`, an addressed `*Error`, a `Validate` that **never fetches**).
  It refuses, each with `file:line`: a missing/unknown `version`, a missing `name`
  or `description` (the identity the one consent screen shows), an empty bundle
  (`checkEmpty`, ported from the manifest — a share that changes nothing bought
  nothing), an embedded `scene` that does not parse or declares no `root` (rebased
  onto the bundle bytes for a bundle-absolute address, and deliberately **not** run
  through the full bind validator — a bundle scene may wire into its bundled
  plugins' `<id>.*` namespaces, unknown until each manifest is fetched, so that is a
  mount-time check), a malformed embedded `theme` block (through the one token
  validator `theme.LoadBytes`), and a plugin reference whose `manifest_url` is
  absent or not HTTPS (the same security refusal `registry.go` makes). Pinned by
  `bundle_test.go` (13 tests) with counterfactuals run by hand.
  **The pure core has no caller yet** — it is dead until the live wiring lands.
  **Remaining (the deferred impure half, DESIGN-BLOCK-J.md J4 "what lands now vs
  deferred"):** the loop wiring that fetches each referenced manifest, computes the
  aggregated Q15 identity, shows the *one* consent screen (`ext.ConsentScene`'s
  bundle sibling), then `theme.Merge` + `patch.Mount`s the scene and plugins on a
  single grant — gated on the same live-loop surface the I5 modal mount uses
  (`cmd/arxi-tui/install_modal.go`, `startInstall`). **The design point that beat
  needed is now pinned (2026-09-29, PR #116):** DESIGN-BLOCK-J.md's new section
  "How 'one consent screen' aggregates N grants" resolves it — the screen is
  **one**, the grants are **N per-plugin** against each plugin's own unchanged Q15
  identity (`internal/ext/identity.go` stays per-manifest), and one `y`/`r`/`n`
  answer fans out to N `Gate.Grant` calls. The aggregate-identity reading is
  rejected there as a new authority that breaks grant transfer in both directions.
  The wiring's two recorded consequences: the embedded scene/theme request no
  powers (H2 declarative split — zero code, no capability block), and mount order
  is grant-then-compose (atomicity from `checkEmpty` carried to install time). So
  the live wiring is now unblocked: `BundleConsentScene` (presentation) and the
  loop's one-answer→N-`Grant` fan-out (sequencing) are the only new artifacts,
  both over the unchanged gate.
  **Presentation half BUILT (2026-09-29, PR #118).** `BundleConsentScene`
  (`internal/ext/bundle_consent_scene.go`) is the first of those two artifacts,
  landed the way `ConsentScene`'s view landed before its loop: a pure
  `(name, description, []BundlePluginConsent) -> *scene.Document` authored through
  the same map→`ParseNamed` path and the same tokens both themes sign. It stacks
  the bundle identity above one full identity+capability block per plugin needing
  a fresh grant (the same tuple `ConsentScene` shows), lists already-remembered
  plugins by name only as trusted-no-new-power (the whole cost of the install, not
  just its new part), names a plugin-less scene/theme bundle as a confirm that
  "runs no plugin code", and always draws the `y`/`n`/`r` keys the loop will bind.
  Pinned by `bundle_consent_scene_test.go` (5 tests over two distinct manifests),
  with both counterfactuals run by hand: rendering only `needsConsent[0]` fails on
  the second plugin's missing fields, and re-detailing the remembered half
  surfaces its capability and fails the trusted-no-new-power assertion.
  **Fan-out sequencing BUILT (2026-09-29, PR #119).** `GrantBundle`
  (`internal/ext/bundle_install.go`) is the second of the two artifacts the design
  names, landed pure the way `ParseBundle` and `BundleConsentScene` were: a
  `(*Gate, []BundlePluginDecision, BundleAnswer) -> ([]BundleGrant, error)` fan-out
  of one bundle answer to N per-plugin `Gate.Grant` calls, each against the plugin's
  own unchanged per-manifest `Identity(m, digest)`. It enforces the three properties
  the design pins — all-or-nothing (`Rejected` grants and composes nothing, returns
  `ErrBundleRejected`), one `Grant(m, digest, m.Capabilities, remember)` per
  not-yet-remembered plugin with already-remembered plugins carried through without
  a re-grant, and grant-then-compose atomicity (a `Grant` refusal aborts before the
  caller composes; atomicity is over the workspace, deliberately not the remember
  store, documented at the site). `ConsentsFor` projects the loop's per-plugin
  decisions into the `[]BundlePluginConsent` `BundleConsentScene` renders so the
  screen and the fan-out read one list. Pinned by `bundle_install_test.go` (6 tests)
  with the counterfactuals run by hand: granting before the `Rejected` check, a
  `break` after the first grant, and swallowing the `Grant` refusal each fail
  exactly their test; a remembered-narrow-grant probe proves a remembered plugin is
  carried without the widening a spurious re-grant would cause; and a
  remember-then-standalone-`Decide` probe proves the N rows are keyed per-manifest,
  the property the rejected aggregate-identity reading would have destroyed.
  **Remaining (the impure loop half):** the cmd-edge orchestration that fetches the
  bundle, runs the unchanged H6/I5 pipeline per `manifest_url` (fetch →
  `InstallFromBundle` → `LayoutByDigest` → `Gate.Decide`) to build the
  `[]BundlePluginDecision`, drives `BundleConsentScene` on the live modal surface,
  calls `GrantBundle` on the single `y`/`r`/`n` answer, then grant-then-composes
  (`theme.Merge` + `patch.Mount` the scene and plugins, `supervisor.Start` the
  behavioral ones) — gated on `cmd/arxi-tui/install_modal.go`'s `startInstall`,
  exactly the surface a single behavioral install already uses. Both pure artifacts
  the design named are now built; what is left is the loop that calls them.
  **Resolve half BUILT (2026-09-29, PR #120).** `resolveBundle`
  (`cmd/arxi-tui/bundle_install.go`) is the first increment of that loop half — the
  bundle analogue of `installBehavioralPlugin` — landed with the same injected-fetcher
  seam so it is testable without a network. It fetches the bundle JSON, `Validate`s
  it, and runs the unchanged H6/I5 pipeline (fetch archive → `InstallFromBundle` →
  `Gate.Decide`) once per referenced plugin against that plugin's OWN per-manifest
  identity, returning the parsed bundle, one `BundlePluginDecision` per plugin
  (index-aligned with the `Installed` tree each was decided over so a later
  `BundleGrant` pairs back to its tree), and grants/spawns nothing — "download ≠
  trust ≠ grant" (Q15) holds across a bundle. It stops at the decision on purpose:
  the two fetchers are injected because they carry different bodies under different
  caps (bundle JSON vs plugin `.tar.gz`), and a plugin that fails to fetch/lay out
  fails the whole bundle (all-or-nothing), named by its URL. Pinned by
  `bundle_install_test.go` (5 tests) with the counterfactuals run by hand: replacing
  the all-or-nothing return with `continue`, skipping `Validate`, and deciding
  against a flipped digest each fail exactly their test. **Remaining (the compose
  half):** drive `BundleConsentScene` on the live modal surface, call `GrantBundle`
  on the single `y`/`r`/`n` answer, then grant-then-compose (`theme.Merge` +
  `patch.Mount` the scene and plugins, `supervisor.Start` the behavioral ones) —
  the modal-loop half that owns the keyboard and the live scene/theme/supervisor
  state, gated on `startInstall`'s surface.

  **Plan half BUILT (2026-09-29, PR #121).** `planBundleCompose`
  (`cmd/arxi-tui/bundle_compose.go`) is the second increment: the join between the
  two pure artifacts. It calls `GrantBundle` to fan the single answer out to N
  per-plugin grants (all-or-nothing, each against the plugin's own identity), then
  pairs each returned `BundleGrant` back — by plugin ID, not slice position, so a
  drift between the resolution and the fan-out is a named refusal rather than a
  process spawned at the wrong tree with another plugin's grant — to the `Installed`
  package `resolveBundle` laid it out into, producing the `supervisor.Config` the
  loop will `Start` (each carrying only the gate's granted set, invariant 7). The
  bundle's embedded scene and theme are carried through verbatim as the raw bytes
  the loop parses and mounts. It grants (in-memory gate) and plans but spawns and
  mounts nothing: the whole compose plan is one value the loop gets in full or not
  at all, carrying `GrantBundle`'s workspace atomicity into the loop (a rejected or
  partially-granted bundle never reaches the mount calls). A rejection returns
  `ErrBundleRejected` and a nil plan, so the loop cannot compose a "rejected" plan by
  forgetting to check. Pinned by `bundle_compose_test.go` (4 tests) with all four
  counterfactuals run by hand: nulling `Granted`, swallowing the rejection into an
  empty plan, dropping the scene, and skipping carried grants each fail exactly their
  test. The loop-execution half is the increment below.

  **Loop-execution half BUILT (2026-09-29, PR #122).** The impure wiring the plan
  half left to the loop, landed pure pieces first per the J-block pattern.
  (1) `bundleAnswerForKey` (`cmd/arxi-tui/bundle_consent_prompt.go`) is the
  keypress→`ext.BundleAnswer` mapping for the one bundle screen, the sibling of
  `consentAnswerForKey`: y/r grant (r remembers), n/Esc reject, every other key
  leaves the prompt standing. It carries NO capability subset — a bundle is
  all-or-nothing, so the per-plugin declared sets live in `GrantBundle`'s decisions,
  not the answer — which is why it is a separate function, not a remap of the
  single-plugin answer. (2) `parsePluginBundle` (`plugin_install_cmd.go`) is the
  `/ui plugin bundle <url>` grammar, a distinct verb from `install` because a bundle
  URL is a `bundle/v1` JSON document naming N plugins plus a scene/theme, while
  `install` fetches one behavioral `.tar.gz`; a shared verb would have to sniff the
  payload. (3) `bundleModal` + `startBundleInstall` (`bundle_modal.go`) are the
  loop-visible state and the resolve→consent→plan worker: unlike a single plugin the
  bundle ALWAYS shows the screen (a named confirm even when every plugin is
  remembered), so the worker always reaches the consent bridge and `planBundleCompose`
  turns the one answer into a plan or `ErrBundleRejected`. (4)
  `executeBundleComposePlan` (`bundle_execute.go`) is the grant-then-compose executor
  the loop runs on a plan: it parses the embedded theme and scene FIRST (so a
  malformed document aborts with nothing composed — workspace atomicity), then applies
  the theme layer (keyed `bundle:<name>` so it cannot collide with a plugin id), then
  replaces the live document, then `supervisor.Start`s each granted config + registers
  + pumps — the same Start→Add→pump sequence `supervisor.Mount` runs, minus the
  decide/grant the plan already did. (5) The loop wiring (`main.go`): `bundleMod`
  beside `modal`, the two bridge channels, the `/ui plugin bundle` branch (refusing
  while either install is busy — one consent screen at a time over the shared gate),
  the `bundleMod.capturing()` cases in the key branch and the activeDoc switch, and
  the two select cases that show the screen and execute the plan. Counterfactuals run
  by hand: a granting default branch in `bundleAnswerForKey` fails the standing-prompt
  test; over-claiming in `parsePluginBundle` fails the dispatch-boundary test;
  applying the theme before parsing the scene fails the malformed-scene atomicity
  test. **Block J is now complete** — J1–J3 (preview, registry, installer scene + live
  loop), J4 (bundle, both pure artifacts and the loop wiring) and J5 (Scene 7 golden)
  are all built. Note: the live spawn path (`supervisor.Start` over a real subprocess)
  is verified by build + the pieces it composes, as with the I6 loop-wiring commits;
  the sandbox has no fake-tty harness for the real loop.
- **J5** [J3] Freeze the Scene 7 golden.
- **J5 — DONE (2026-09-28, PR #104).** The Scene 7 golden is frozen as the
  host-generated community installer, pinned the way Scene 6 (TICKER) and Scene 9
  (SUBAGENTS) are, plus the two J1 preview frames the J1/J3 notes defer here.
  `testdata/registries/COMMUNITY.registry.json` is a fixed, well-formed `reg/v1`
  index (two entries, HTTPS manifest URLs, inline preview markdown), kept in a
  subdirectory so it is not counted on the progress audit's scene axis.
  `testdata/COMMUNITY.json`/`.frame`/`.styled` are the installer scene and its
  plain/styled goldens; `TestCommunityJSONIsTheInstallerSceneOutput` proves
  `COMMUNITY.json` is byte-for-byte `Registry.InstallerScene().Source()` — the
  same not-hand-authored guarantee `TestTickerJSONIsTheMountOutput` carries — so a
  drift in the builder is a golden diff, not a silent divergence. This is the
  fixture the audit matches to `## Scene 7 — COMMUNITY`, and it now reads **7 of
  11 scenes pinned** (CONFIG, BUTTONS, DASHBOARD, ANIMATED remain). J1's preview
  mode is pinned at the composed-frame level:
  `testdata/plugins/COMMUNITY-PREVIEW.manifest.json` declares a `tick.price` text
  mock and a mock-less `tick.status`, and `testdata/COMMUNITY-PREVIEW.frame` /
  `COMMUNITY-PREVIEW-NIL.frame` are the previewed-with-mocks frame (the mocked
  bind draws `$1.23 ▲`, the un-mocked one stays the honest `[…]`) and the nil
  no-op frame (every bind `[…]`, byte-identical to a pre-J1 render). The two
  differ on exactly the mocked field, which is the built-in counterfactual: a
  regression in the preview resolver collapses them and the witness fails on the
  missing `$1.23`. **One J3 gap surfaced and fixed here:** the installer is a
  shipped scene, so `InstallerScene` now emits the gated `host.scene.error` notice
  node every shipped scene must carry (`TestEveryShippedSceneBindsTheNotice`); the
  `when` gate keeps it costless, so only `COMMUNITY.json` moved and the frame
  goldens did not. **Remaining for J:** the J3 follow-up live loop (keystroke
  loop writing the `community.*` fold fields via `FilterEntries` + a live
  `list`/search `InstallerScene` + selection→preview; the signed bind and its
  engine projection are done) and J4 (bundle, needs I5).

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
