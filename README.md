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
- ✅ Styled golden fixtures: `RAW.styled`, `SOARIA.styled`, `MAXIMUM.styled`
- ✅ Theme validation (scenes reference existing tokens) — repaired during
  Phase 2: `ValidateTokens` read `style["token"]`, while the shipped scenes,
  `SCENES.md`, `TOKENS.md` and the render path all write `style["style"]`, so
  the check was blind to the only spelling that occurs. With the key fixed,
  the defect it hid surfaced addressed: `SOARIA.json:2:3` and `:25:5`
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
  a real silent drop that cannot be refused because SOARIA ships eleven and
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
  because both SOARIA cases demanded only fields SOARIA already binds. Every
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
- ⬜ **The corpus has not yet been run against a real model.** The harness is
  verified end to end against a local stub (4/4 converged, turns 3×2 1×3,
  driven by real addressed refusals from the real validator), but the gateway
  available here refuses on plan grounds, so there is no score for any model
  yet. Phase 2's question — can a model patch scenes reliably — is therefore
  still open, and `PLAN.md` gates `/ui` on the answer.
  The plan-block detection is confirmed against the live gateway rather than a
  captured body, and re-confirmed each session it is checked: the run that
  once reported `looped=3` now reports `model_error` on every case and exits
  non-zero (latest check: `0/4 converged, model_error=4`, exit 1). That is the
  harness declining to score, which is the correct result and still not a
  score. The block arrives as **HTTP 200 carrying a normal-looking assistant
  message** — `x_genspark.code = free_plan_block` — which is why the detection
  has to exist at all: without it the refusal reads as a model that answered
  badly, and the corpus would record a plan limit as a capability measurement.
- ⬜ `/ui` commands and agent-driven patches, with the change-diff view

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

The default scene is `testdata/SOARIA.json` (the sobria look). If it fails to
load, the interface falls back to the factory RAW scene (two nodes: transcript
and input, nothing else) and states why, addressed, on screen.

## Evaluate (Phase 2)

```bash
OPENAI_API_KEY=... ./arxi-eval -model <name> -v
```

Runs the corpus in `testdata/eval/` and reports convergence and turns per case.
The exit status says whether the harness ran, not whether the model scored
well — see `docs/EVAL.md` for why no threshold is set.
