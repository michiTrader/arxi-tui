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
- **G1** Implement `transition`. **G4** `enter {row, stagger}`. Each atomic, each
  now implementable against the signed contract, the host clock G-A built, *and*
  the one-shot pattern G3 set as the template — `transition` rides the same
  `AnimPhase` phase (curve-eased `elapsed / duration_ms`) onto the SGR-intensity
  axis, and `enter` schedules per-row `transition`/`reveal`. The pattern: graduate
  the field on `scene.Node` if it is not already there, sign its refusals in
  `validateScroll`/`validateReveal`'s shape, read the phase in the renderer, and
  land a golden pinned at chosen phases with a counterfactual that reverts the
  phase mapping.
- **G5** [G1,G4] Freeze the remaining Scene 4 golden(s) and update the status
  paragraph as each prop lands (scroll's and reveal's paragraphs are already
  updated).

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
