# arxi tui

The interface is a document. The running instance rewrites it live — you can
too, from inside it.

A terminal agent interface whose entire chrome (rows, panels, overlays,
animations, menus, the input bar itself) is a **scene document** addressed by
node id and edited through commands, patches, or the agent acting on your
word. The factory look is written with the same format you have; nothing the
interface can do is closed to you. The range is real: from two nodes
(type and answer, nothing else) to a full dashboard of panels, widgets and
plugins — and you can install a plugin, or a whole interface someone else
published, by handing arxi a link.

- `AGENTS.md` — how this repo is worked on (English-only, comment/test/commit
  policy, invariants).
- `docs/PLAN.md` — the plan of record: the spectrum, the decisions, the
  phases, the install rule.
- `docs/SCENES.md` — the golden scenes and the 23 settled format decisions.
- `docs/LESSONS.md` — the bugs and hard rules already paid for by arxi-sim
  and the arxi core, with sources. Do not re-derive them.
- `docs/BINDS.md` — the bind vocabulary: every field the host exposes to scenes.

## Status

**Phase 0 — Scene engine:** Complete and running.

- ✅ Scene document parser and validator (`internal/scene`)
- ✅ Fold over log events (`internal/fold`)
- ✅ Render engine with all base nodes: stack, row, box, text, markdown, input,
  overlay, list, spinner, marquee, rule (`internal/engine`)
- ✅ Terminal backend with input decoder, alternate buffer, cursor control (`internal/term`)
- ✅ Mock driver for Phase 0 dev, serve driver for Phase 0.5 (`internal/driver`)
- ✅ Event loop with double Ctrl-C escape gesture (`cmd/arxi-tui/main.go`)
- ✅ Golden scenes: RAW, SOBRIA, MAXIMUM running and tested
- ✅ Bootstrap binds: `chat.history`, `user.input`, `agent.working`, `thinking.text`,
  `usage.delta`, `slash.active`, `slash.matches`, `agent.mode`, `model.name`
- ✅ Full test suite passing (9 packages)

**Phase 1 — Tokens and themes:** Complete.

- ✅ Token format and JSON schema (`internal/theme/theme.go`)
- ✅ Token resolver with open definition (no enum)
- ✅ Factory SOBRIA theme with OSC 11 background detection
- ✅ Token wire-up in render pipeline (`engine.Cell.Style`)
- ✅ Styled golden fixtures: `RAW.styled`, `SOBRIA.styled`, `MAXIMUM.styled`
- ✅ Theme validation (scenes reference existing tokens) — repaired during
  Phase 2: `ValidateTokens` read `style["token"]`, while the shipped scenes,
  `SCENES.md`, `TOKENS.md` and the render path all write `style["style"]`, so
  the check was blind to the only spelling that occurs. With the key fixed,
  the defect it hid surfaced addressed: `SOBRIA.json:2:3` and `:25:5`
  reference the token `header`, which `TOKENS.md` signs and the theme had
  dropped. Every token test had used the validator's key rather than the
  scenes', so code and tests shared one wrong assumption and agreed.
- ✅ The shipped scenes are held to the rule the validator applies to
  downloaded ones (`TestTheShippedScenesReferenceOnlyDefinedTokens`)
- ✅ Every style reference the validator accepts now also reaches the screen.
  Fixing the key above left the renderer behind: `ValidateTokens` accepted
  both `style["token"]` and `style["style"]`, while `styleName()` still read
  `style["style"]` alone, so a scene using the other accepted spelling
  validated clean and drew **unstyled** — the one outcome that reports success
  and shows the wrong screen, since clearing validation is exactly the signal
  that says the document is fine. Found on the corpus' own gold answer: the
  converged document of `sobria-dim-the-footer`, whose order is *"grey it
  out"*, styled `model.name` as `{"token": "dim"}` and rendered it with no
  style. The render path now reads `scene.StyleTokenKeys()`, so the two
  cannot disagree again, and the guard is a property over that list rather
  than two hardcoded spellings — a test that enumerated the keys itself would
  reproduce the drift it exists to catch.
- ✅ A border's declared style token reaches the frame. `SCENES.md` Scene 3
  signs the object form (`{"shape": "single", "style": "warn"}`) so a frame
  can carry a token, `BorderStyleName` exists to read it and `ValidateTokens`
  refuses an undefined one — but both drawing paths stamped the literal
  `"border"` onto all eight frame spans and never asked. The same class as
  above, and more deceptive: naming a bad token *does* get a refusal, so the
  field looks wired; a correct value simply did nothing. The bare string form
  keeps `"border"`, held by its own test because MAXIMUM's styled golden pins
  six spans under that name and the unconditional fix moves it (verified: the
  naive version fails both that guard and `TestMaximumSceneStyledGolden`).
- ✅ A construction the validator accepts is one the engine draws — and where
  that is not true yet, the refusal is explicit. `row_template` was the third
  instance of the class above and the first to reach the *measuring
  instrument* rather than a scene: the validator walked it for binds and for
  tokens, `loc.go` addressed it, the binds audit collected through it and
  `eval`'s `CollectBinds` walked it by name citing Q10 — five places saying
  the field was live — while `internal/engine` read it in zero. Because the
  grader counts a bind found inside a template, an answer satisfying
  `must_bind` only there scored **converged** while the list drew `[…]`.
  Measured, not reasoned: reachable from a case the corpus already ships
  (`raw-add-tasks-panel`, *"put a tasks panel on the right"*), which returned
  `converged=true, missing=[]` on a panel with no tasks in it — the corpus
  lying in the model's favour, the one direction nobody audits.
  The fix is a refusal rather than an implementation: the field's semantics
  are relative binds (`row.kind`) and `row.*` is signed nowhere in BINDS.md —
  it is Scene 5, i.e. Phase 3 — so drawing it now would invent format ahead of
  the phase meant to design it. The message says *"not yet rendered"* rather
  than *"invalid"*, because the author spelled the field correctly and a wrong
  diagnosis costs the repair loop a turn it charges to the model.
- ✅ The **fourth** instance of the class fails the suite by itself
  (`internal/scene/unrendered_audit_test.go`). Three were found by hand, one
  per session; the audit enumerates `Node`'s fields from the source and holds
  each to one of three states — rendered, refused via `unrenderedFields`, or
  justified in writing in `acceptedUnreadFields`. Types are resolved with
  `go/types` rather than matched as text, because the text draft reported
  `Node.ID` as read (`eval.Case` also has an `ID`) and a guard that cries wolf
  is a guard that gets deleted. The scene package is excluded from the read
  set on purpose: *being validated* is the shared signature of all three
  instances, so counting this package's own reads would make the audit agree
  with the bug. Two fields stay unread and say why: `filter_by` is decorative
  (the host filters regardless — verified byte-identical frames with the
  field, with a nonsense value, and with no field at all), and `categories` is
  a real silent drop that cannot be refused because SOBRIA ships eleven and
  invariant 1 outranks this audit. Both entries fail the day the engine reads
  them, which is the signal to delete them.

- ✅ A container does not restyle its children's content
  (`internal/engine/container_preserves_child_style_test.go`). The bind guards
  ask whether a value reaches the frame; this asks about the other half of what
  a node declares — the token it is drawn under. A value arriving under the
  wrong token is on screen and wrong, and every projection guard passes it.
  Swept as a matrix (every node type × bordered/borderless × the container
  declaring a token or not), one shape of sixteen discarded the child's token:
  a **bordered box**, whose content loop flattened each row with `l.Text()` and
  re-emitted it under the box's style. The rule was already written down in
  `padLine` — *"chrome must not restyle the content it fills around"* — and
  already honoured by `wrapWithBorder`, the bordered *overlay* path. Three of
  four drawing paths obeyed it and nothing compared them, which is why the
  guard enumerates rather than testing the box. Reachable from a shipped scene:
  MAXIMUM's Tasks panel is a bordered box around a list whose empty state is
  minted `dim`, and `MAXIMUM.styled` had been pinning it bare as correct output
  since the day it was generated. The fix moves two golden lines and they were
  measured apart: `no tasks` gains `«dim:…»` (the repair), and the banner's one
  span becomes two adjacent spans of the *same* token (a boundary, not a
  change) — with SGR codes stripped the emitted text is byte-identical, so no
  cell moved and invariant 1 holds. Verified by injection: welding the sibling
  `wrapWithBorder` path leaves every golden green and fails only this guard.

- ✅ A node draws its own content under its own token
  (`internal/engine/node_honours_own_style_test.go`). The guard above holds
  the *child* fixed at `text` and sweeps the containers, which can only see a
  parent discarding a declaration — never a leaf that never applied one. That
  is the **fifth** instance of the accepted-but-not-drawn class: `style` is a
  universal property in SCENES.md's vocabulary and `ValidateTokens` is
  type-agnostic, yet `input`, `list`, `markdown` and `rule` emitted the token
  they minted and ignored the one the scene declared. Reachable and silent:
  SOBRIA has one node of each, and styling all four is accepted by **both**
  validators while leaving the frame **byte-identical** — so nothing refuses,
  the repair loop gets no `file:line`, and `converged` scores the unchanged
  screen as a win. The fix makes a declaration replace the minted default and
  keeps the default when the scene declares nothing; that fallback is
  load-bearing, since all three goldens declare nothing here and invariant 1
  says the factory frames do not move (none did). Two tokens stay fixed on
  purpose: the list's `[…]` placeholder is the engine reporting it has no
  projection, not content a scene may dress up, and the **selected** slash row
  stays bright because BINDS.md §4.3 signs it as the only indication of what
  `Enter` will submit. That second boundary was argued and unmeasured —
  erasing the highlight passed the entire suite — so it now has its own guard,
  failing on that injection alone. Verified one weld at a time (`rule`,
  `markdown`, `input`, `list`): each caught only by these guards, with
  `render.go` byte-identical after every restore.

**Phase 1.5 — The SCENES ↔ BINDS audit:** Complete.

- ✅ `internal/scene/binds_audit_test.go` parses `docs/BINDS.md` and holds the
  validator's runtime inventory to the signed document, in both directions
- ✅ All three pinned scenes audited (MAXIMUM was previously unchecked)
- ✅ An unexercised signed bind is a logged warning, per `AGENTS.md`
- ✅ Inventory drift repaired: 4 unsigned binds removed, 18 signed-but-rejected
  binds restored, `agent.todos` signed in §4.1

**Phase 1.6 — The address on every refusal:** Complete.

- ✅ `internal/scene/loc.go` maps a byte offset to `file:line:col`, with the
  column counted in runes (the shipped scenes are full of `Δ`, `┃` and
  box-drawing glyphs, so a byte column points mid-character)
- ✅ Every refusal in `internal/scene` is a `*scene.Error` carrying its
  address — unsigned binds and `when` conditions point at the offending node
  (BINDS.md §4.5), and JSON syntax/type errors keep the offset the standard
  library had already computed
- ✅ Undefined token references carry `file:line` too (LESSONS.md's rule, with
  arxi-sim's polarity inverted: the reference is checked, not the inventory)
- ✅ Invariant 3's boot notice reaches the screen: `loadScene` returns the
  addressed reason and the host publishes it on `host.scene.error`, in both
  the tty and the piped render path. It used to fall back silently.
- ✅ A structural guard (`TestEveryRefusalInThisPackageIsAddressed`) covers
  both parse entry points, so a later rule returning a bare `fmt.Errorf`
  fails the suite instead of quietly reopening the gap

**Phase 2 (in progress) — Mutation from inside.** The gate lands before the
feature, as `PLAN.md` requires:

- ✅ `docs/EVAL.md` — the corpus contract: case shape, scoring, and why the
  corpus is data rather than code
- ✅ `internal/eval` + `testdata/eval/` — the corpus loader and its first four
  cases, covering all three pinned scenes. Every expected refusal is replayed
  against the real validator on each `go test`, so a case cannot claim a
  refusal the engine does not produce — which already caught an invented one
  and three misaddressed ones (see EVAL.md).
- ✅ A repair path longer than one turn (`maximum-count-the-tasks`): an unsigned
  bind, then an invented token, refused through two different types on
  consecutive turns. Until it landed, every case held a single refusal, so the
  corpus measured the first shot while documenting that it measured the loop.
- ✅ The false-pass guard (`TestDoingNothingDoesNotPass`): a scripted model that
  ignored the order and echoed the base scene back scored **2/4 converged**,
  because both SOBRIA cases demanded only fields SOBRIA already binds. Every
  other test stayed green — they ask whether a refusal is real, and every
  refusal was. Each case must now demand a bind its base scene lacks; the
  do-nothing model scores 0/4.
- ✅ `scene.SignedBinds()` — the validator's own inventory, exported so the
  runner can tell the model which binds exist. The alternative was a fourth
  hand-written copy of a list that has already drifted once, and the drift
  would have been recorded as the model's failure rather than the prompt's.
- ✅ One judge for both callers (`internal/eval/grade.go`): the corpus test and
  the runner grade through the same code, so the recorded attempts are a
  prediction of the runner's behaviour rather than a parallel story about it
- ✅ The model runner (`internal/eval/run.go`) — the repair loop, converged /
  turns-to-convergence, and five outcomes rather than two: `converged`,
  `exhausted`, `looped`, `incomplete`, `model_error`. Proven by scripted
  models, so the loop is testable without a network; ten injected regressions,
  all caught.
- ✅ `cmd/arxi-eval` — runs the corpus against an OpenAI-compatible endpoint.
  A separate binary: the shipped interface carries no eval harness and no
  reason to read `OPENAI_API_KEY`.
- ✅ **The corpus has been run against a real model.** First live run on
  2026-09-22 against `deepseek-v4.1-flash` on an OpenAI-compatible endpoint
  (not the plan-blocked gateway below). Three runs, because a single run of a
  non-deterministic model is weak evidence: **11 of 12 case-runs converged**.
  - Run 1: 3/4 converged (turns 1,1,2); `raw-add-tasks-panel` scored
    `incomplete` (`unbound: agent.todos`).
  - Run 2: 4/4 converged (turns 1,1,2,1).
  - Run 3: 4/4 converged (all 1 turn).

  Two facts matter more than the ratio. **The repair loop demonstrably works
  against a real model**: in two of the three runs a turn-1 JSON syntax error
  (`invalid character ']' after object key:value pair`) was handed back as an
  addressed refusal and the model self-corrected on turn 2 — the exact
  ask→grade→hand-back mechanism Phase 2 exists to prove. And **the one
  non-convergence is the failure the corpus was built to detect, not a
  repair-loop fault**: `raw-add-tasks-panel` predicts the model will guess
  `tasks.list` for what BINDS.md signs as `agent.todos`, and because PLAN.md
  decided an unknown-but-parseable bind is a *warning*, not a load-time
  refusal, there is no validator message to repair from — so it scores
  `incomplete` rather than `exhausted`. It converged in the other two runs, so
  the miss is a probabilistic bind-naming slip against a deliberate product
  tradeoff, not evidence the model cannot read addresses.

  Phase 2's question — can a model patch scenes reliably — is answered *yes*
  for this model: it reads the format, converges mostly on the first turn, and
  uses addressed refusals to recover. The ship/no-ship call for
  agent-command-driven `/ui` self-extension is recorded in `docs/PLAN.md`.

  The plan-block detection below stays because it guards a real hazard: the
  gateway once available here refused on plan grounds with **HTTP 200 carrying
  a normal-looking assistant message** — `x_genspark.code = free_plan_block` —
  so without the detection a plan limit would record as a capability
  measurement. It was re-confirmed live each session it was checked (latest,
  against `gpt-5-mini`: `0/4 converged, model_error=4`, exit 1, block confirmed
  by a direct probe rather than inferred from the harness).
- ✅ The `/ui` control surface (`internal/patch`), PLAN.md's deterministic
  half. A patch is a **source-to-source transformation** — bytes in, bytes
  out, re-parsed through `scene.ParseNamed` — rather than a mutation of
  `*scene.Node`, and the choice was measured rather than argued. A
  `Document`'s address book is built by the parser and by nothing else, so a
  Document assembled in memory has no offsets; the same refusal, on the same
  document, both ways:

  ```
  parsed from bytes : probe.json:2:4: node type "text" declares "row_template" …
  rebuilt by hand   : <scene>: node type "text" declares "row_template" …
  ```

  The address is gone. That is the worst place in the project to lose it,
  because Phase 2's thesis is the repair loop and the validator error is the
  only input the retry gets: a tree-mutating `/ui` works for every command
  that succeeds and degrades the diagnostics of exactly the commands that
  fail. Round-trip fidelity was measured before it was built on (both shipped
  scenes re-marshal and re-parse tree-stable and warning-stable). The edit
  walks a generic map rather than the typed tree, because `scene.Node` drops
  undeclared keys and editing through it would silently delete every unknown
  property on the way past — including every field a later phase adds and
  every field a plugin fragment carries.
- ✅ Two verbs, and the omissions are the substance rather than the unfinished
  edge. `add`/`move` must answer *where*, and that addressing vocabulary is
  Scene 5 / Phase 3; `hide`/`show` need a per-node view-state bind, and the
  draft that invented one (`ui.hidden`) was refused by the validator against
  the shipped scene — correctly, since every `ui.*` row BINDS.md signs is a
  single id, so a scalar flag makes `/ui hide a` silently unhide `b`. All four
  are refused the way `row_template` is: *"not yet"* rather than *"invalid"*,
  because a wrong diagnosis costs the repair loop a turn.
- ✅ Measured while wiring it: **about half of every shipped scene is
  unaddressable** — SOBRIA declares an id on 7 of 15 nodes, MAXIMUM 7 of 13,
  RAW 3 of 4. So "no node with that id" is the expected answer to much of what
  a user will try, and the reason is invisible from the screen (the status
  row's model name is a node they can see and point at, and `model.name` is
  its *bind*). The refusal lists the ids that exist and counts the anonymous
  ones; synthesising addresses was rejected as a second way to name a node,
  designed here rather than in SCENES.md.
- ✅ The dispatch runs **ahead of the slash menu**, which is a fix rather than
  an ordering preference. The menu filters on the whole typed string, so it
  matches nothing once an argument is present (`ui` → 1, `ui style` → 0,
  `ui style status dim` → 0) and its Enter branch returns the buffer
  untouched: a complete, correct command did nothing at all — no patch, no
  refusal, no prompt — with the menu showing an empty list. The guard for it
  had to be rewritten after an injection: the first version called the handler
  directly, and welding the dispatch back behind the menu left it **green**,
  because the defect is in the order of the branches and that order does not
  exist inside the function under test. It now types the command into `loop()`
  through the scripted TTY.
- ✅ The slash menu advertised `add, move, style, plugin` while the surface
  implemented two verbs — the accepted-but-not-drawn class one layer out, and
  worse there than in a document, since the menu is read at the moment of use
  and so invites the user into a refusal. Held in both directions by a test in
  `patch_test`, which is where it can import both packages without `fold`
  importing the mutation layer (ADR-0002).
- ⬜ Agent-driven patches, with the full change-diff view. The summary line is
  in place (`/ui: styled "status" as "dim"`); the side-by-side view belongs
  with the agent half, where a proposal arrives *before* it is applied.

## Build

Requires Go 1.25.

```bash
go build -o arxi-tui ./cmd/arxi-tui
go build -o arxi-eval ./cmd/arxi-eval    # the Phase 2 eval harness
go vet ./... && gofmt -l .
go test -count=1 ./...
UPDATE_GOLDEN=1 go test ./internal/...   # regenerate golden fixtures
```

## Run

```bash
# With mock driver (Phase 0 dev, no arxi binary needed)
./arxi-tui

# With arxi serve subprocess (Phase 0.5, requires arxi binary)
ARXI_BIN=/path/to/arxi ./arxi-tui
```

### What the NDJSON bridge actually does today

Measured on 2026-09-21 against a real `arxi serve` (arxi 0.0.1-spec, surface
v1) built from `michiTrader/arxi@main`, not against the mock. The transcripts
are committed as `testdata/serve/session.ndjson` (a live protocol session) and
`testdata/serve/real_run.ndjson` (121 events the core wrote for a simulated
run). Both are regenerable; the commands are in the test headers.

The bridge had never been run against the core before this, and it did not
work:

- **The handshake could never succeed.** It compared the hello's `version`
  against a `"0.1.0"` invented in this repo; the core sends its *binary*
  version, `"0.0.1-spec"`. Fixed to gate on `surface_version`, which is the
  vocabulary number and the thing the bind inventory is written against.
- **`run.prompt` is declared and not implemented.** It is the only request
  arxi-tui sends, and this build of the core answers it `not_implemented`. The
  host now reads the hello's `implemented` list, so a permanent gap is
  distinguishable from a transient failure instead of being rediscovered one
  refused request at a time.
- **`run.attach` *is* implemented**, which contradicted ADR-0002's premise that
  no subscription layer exists in arxi to extend. **Re-decided** (docs/PLAN.md,
  "ADR-0002 re-decided"): log-follow stays, but for the opposite reason to the
  one recorded. Not because subscribing is unavailable — it is implemented,
  `event.subscribe` is in the session capabilities, and `serve_stream.go`
  already has subscription IDs, pumps, cancellation and writer arbitration —
  but because log-follow is the path `Replay`, the goldens and the eval corpus
  all run through, and a second ingestion path with a different confirmation
  model is two chances at a wrong frame. The adoption trigger is now specific:
  a positive end-of-run signal (`run.result` is *not* the last event — seq 112
  of 122), or reading a log whose directory the host does not own.
- **Log-follow said "confirmed" and meant "newline-terminated".** The commit
  point is the *removal* of `pending.commit` (logstore step 3), so between the
  batch append and the commit the log holds complete, newline-terminated
  records that the core's own `Open()` truncates away. Measured: two committed
  events plus a two-event in-flight batch delivered **four**. The follower now
  stops at the smaller of the last newline and the marker's rollback point,
  and tracks its own delivered offset because the confirmed boundary can move
  *backwards*.
- **A refusal read as success.** `ok:false` came back with a nil error, and the
  host's only call site was `_ = drv.SubmitPrompt(...)`. Refusals are now a
  `driver.Refusal` carrying the core's code, sentence, `fix` and `operation`,
  with `Permanent()` for the retry branch.
- **The event decoder was right about the file and wrong about the socket.**
  `kernel.Event` on disk spells the sequence `seq`; `host/v1.Event` on the wire
  spells it `sequence`. Reading only `seq` left every subscription event at
  Seq 0 — silently. Unified into one `decodeEvent` that accepts either and
  refuses a record with neither.
- **Every simulated run was labelled live.** `agent.activated` overwrote
  `AgentMode`, conflating "who is working" with "whose money is at stake". The
  mock could not catch it; its `run.started` carries `simulated:false`.

Fold coverage against a real run is **122 of 122 events** (100%), re-measured
from a starting point of 13/122. The `exec.*` family (91 events) and
`run.result` came first; then the `tool.*` family (8 events), which is not the
largest remaining count — `stage.*` is 7 — but is the only family in the log
that says *what the agent did*; then `stage.*` (blueprint position, 120/122);
and finally the `timer.*` pair (2 events) that closed it. The last two are a
stage deadline that armed and cancelled without firing, handled as a
read-and-understood no-op: a scheduled deadline has no host-facing projection,
because the user-visible half of *a timer exists* is an agent blocked on one
(`blocked_on=timer`), which folds into a todo already.

The figure is pinned by a test that also asserts the accounting closes against
the log's length, so every event is either handled or named in the blind list
(now empty, with a fail-loud check for any future unaccounted type). Every
re-measurement was *forced* by that pin rather than reported alongside it.

The default scene is `testdata/SOBRIA.json` (the sobria look). If it fails to
load, the interface falls back to the factory RAW scene (two nodes: transcript
and input, nothing else) and states why, addressed, on screen.

## Evaluate (Phase 2)

```bash
OPENAI_API_KEY=... ./arxi-eval -model <name> -v
```

Runs the corpus in `testdata/eval/` and reports convergence and turns per case.
The exit status says whether the harness ran, not whether the model scored
well — see `docs/EVAL.md` for why no threshold is set.
