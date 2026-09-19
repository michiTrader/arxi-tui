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
  The plan-block detection is now confirmed against the live gateway rather
  than a captured body: the same run that once reported `looped=3` now reports
  `model_error=3` and exits non-zero. That is the harness declining to score,
  which is the correct result and still not a score.
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
