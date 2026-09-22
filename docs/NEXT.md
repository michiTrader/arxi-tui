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

3. **OSC 11 background detection does not exist.** `internal/theme/theme.go:148`
   explicitly disclaims it ("adapts ... without OSC 11 queries");
   `internal/term/decode.go:127` only generically skips OSC replies. Yet
   `README.md:44`, `internal/theme/factory.go:10` and `cmd/arxi-tui/main.go:135`
   claim OSC 11 detection is implemented. This is a claim/code contradiction:
   either the claim is corrected to describe the relative dim/bright SGR
   mechanism that actually ships, or OSC 11 is implemented. Decision required
   (task L4 / N-series below).

4. **No CI exists.** There is no `.github/` directory. The double-Ctrl-C
   escape-hatch tests exist and are platform-agnostic
   (`cmd/arxi-tui/loop_test.go`: `TestLoopExitsOnCtrlCImmediate:110`,
   `TestLoopFirstCtrlCClearsInput:135`), so they *run* on Windows when invoked,
   but AGENTS.md's "runs on Windows CI from Phase 0" is not backed by a
   pipeline. Standing up CI is a real, currently-missing task.

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

- **M1a** Obtain the `run.start` request/response schema — either from a live
  `arxi serve` via the implemented `schema` verb, or from the arxi repo.
  Guessing the param names is a `bad_params` refusal waiting to happen and is
  exactly the "verify, do not assume" rule; do not implement blind.
- **M1b** [M1a] Decide the interaction model the swap forces: if the TUI starts
  runs itself via `run.start`, the **first** user turn creates the run, but
  **subsequent** turns still need a send-to-existing-run verb — and the only
  such verb this build implements is `run.steer` (in the hello), because
  `run.prompt` is `not_implemented`. So the real migration is likely
  `run.start` for turn one + `run.steer` for the rest, not `run.start` alone.
  This needs the user's product call on how a TUI session maps to runs.
- **M1c** [M1a,M1b] Implement the chosen verbs in `internal/driver/ndjson.go`
  and the `serveDriver` (`cmd/arxi-tui/main.go:364`), keeping the
  `not_implemented`/refusal handling that already exists.
- **M2** [M1c] Verify a real round-trip against a live `arxi serve`.
- **M3** [M2] Evaluate adopting the `run.attach`/`event.subscribe` path (a
  positive end-of-run marker) — deferred per ADR-0002
  (`docs/PLAN.md:230-280`); the trigger is needing that positive signal.

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
- **B4** [B3] Implement `Diff -> *scene.Node` (a scene fragment), mint the three
  theme tokens, render side by side. Golden the fragment like any other scene.
- **B5** [B4] Wire propose→apply: agent proposes a patch → host shows the diff
  → applied on approval.
- **B6** [B5] Consent gate per Q23 (show diff + log attribution; no blocking
  per-step menu).
- **B7** [B5] Log attribution: every agent patch is an attributed event in the
  arxi log.
- **B8** [B4] Diff-view goldens + tests whose failure messages name consequence
  and remedy.
- **B9** [B5] Update the verbs advertised in the slash menu
  (`internal/fold/fold.go:1204`, held by
  `internal/patch/menu_agrees_with_the_surface_test.go:40`) and the README
  Status section.

### Block D — Phase 3 design gate (paper; unblocks E/F/G)

Each is an independent design+sign (SCENES/BINDS/TOKENS method).

- **D1** Design and sign the relative `row.*` bind namespace in `docs/BINDS.md`
  (Scene 5 / Q10) — unblocks the `internal/scene/validate.go:241` refusal.
- **D2** Design and sign the addressing vocabulary for `add`/`move` (the
  "where": `below_input`, `above <id>`, etc.).
- **D3** Design and sign the per-node view-state bind for `hide`/`show` with
  collection semantics (not the rejected scalar `ui.hidden`,
  `internal/patch/patch_test.go:165`).
- **D4** Sign the `[anim]` timing token in `docs/TOKENS.md` (Q8) — unblocks the
  four animation props.

### Block E — Phase 3: row_template + relative binds [D1]

- **E1** Implement `row_template` render in `internal/engine` (today refused).
- **E2** Implement relative-bind resolution (`row.field` inside templates).
- **E3** [E1,E2] Implement the `team.members` projection.
- **E4** [E3] Freeze the Scene 9 golden (subagents).
- **E5** [E1,E2] Freeze the Scene 5 golden (CONFIG — the `/config` dogfood).
- **E6** [E1-E3] Remove obsolete "not yet" refusals
  (`internal/scene/validate.go:241`) and `acceptedUnprojectedBinds` entries;
  update the audits.

### Block F — Phase 3: /ui add and move verbs [D2]

- **F1** Implement `/ui add node <where> <fragment>`.
- **F2** Implement `/ui move <id> <where>`.
- **F3** [D3] Implement `/ui hide` / `show`.
- **F4** [F1-F3] Update advertised verbs in the slash menu; goldens; tests.

### Block G — Phase 3: animation props [D4]

- **G1** Implement `transition`. **G2** `scroll {speed, pause_when}` (field
  exists refused at `node.go:69`). **G3** `reveal`. **G4** `enter {row,
  stagger}`. (Each atomic.)
- **G5** [G1-G4] Freeze the Scene 4 golden and update its status paragraph.

### Block H — Phase 3: declarative plugin mounting (heart of the phase)

- **H1** Design and sign the plugin manifest schema (executable, capabilities,
  consent_required, mounts, tokens, streamed binds) — Scene 6.
- **H2** [H1] Load a declarative plugin (scene fragment + tokens, zero code).
- **H3** [H2] Mount fragments by id into the scene tree.
- **H4** [H2] Merge plugin tokens with precedence (user > plugin > factory).
- **H5** [H2] Validate the `<plugin-id>.*` namespace (declared vs used, with
  `file:line`).
- **H6** [H3,H5] Implement `/ui plugin add <url>` (fetch, validate, mount) for
  the declarative path.
- **H7** [H6] Freeze the Scene 6 golden (community ticker).
- **H8** Implement `on_press` action routing (`cmd:/slash`, `focus:<node>`,
  `answer:<kind>`) — needed for interactive fragments (Scene 8 buttons).

### Block I — Phase 3: behavioral plugins (NDJSON subprocess) [H]

- **I1** Design the subprocess plugin protocol (NDJSON frames → bind
  namespace).
- **I2** [I1] Implement the subprocess lifecycle (spawn/supervise/kill),
  porting arxi-sim's procgroup supervisor.
- **I3** [I2] Stream NDJSON frames into `<plugin-id>.*` binds.
- **I4** [H8,I3] Route input via `on_press` to plugin actions.
- **I5** [I2] Consent gate by identity (name/version/executable/args/capability
  set/digest) with "remember" — Q15.
- **I6** [I5] Gate B (tools): a plugin-by-link teaches the agent a tool, running
  as its own process, under the consent contract.

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
- **L4** Resolve the OSC 11 claim (correction 3): either implement OSC 11
  light/dark detection or correct `README.md:44`, `theme/factory.go:10`,
  `main.go:135` to describe the relative dim/bright SGR mechanism that ships.
- **L5** Stand up CI (`.github/workflows`) that runs `go test ./...` including
  the double-Ctrl-C escape-hatch tests on Windows (correction 4).

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
