# The eval corpus — Phase 2's gate

PLAN.md gates the `/ui` work on proof rather than hope:

> before the feature ships, an **eval corpus** — natural-language order →
> correct scene patch — runs against the model on the eleven scenes we already
> have. The corpus measures the **repair loop, not the first shot**: order →
> patch → validator error → retry, until convergence; first-shot accuracy is a
> vanity metric, because the real use is exactly the case where the engine said
> `file:line:` and the model had to read it. And it is data, not code: it can be
> written before the validator exists, which makes it Phase 0's acceptance suite
> from the start. If the model cannot patch scenes reliably, "autoextendable by
> command" is not a feature and the document must say so.

This document is the corpus contract. The cases live in `testdata/eval/` as
JSON, one file per case, and `internal/eval` loads them.

## Why it is data, and what that buys before any model runs

A corpus of Go test functions would need the patch surface to exist before the
first case could be written, which is backwards: the corpus is the thing that
decides whether the patch surface ships at all. As data it can be written now,
and it is immediately useful without a model in the loop, because **the half of
each case that describes the engine's behaviour is checkable against the engine
today**.

That is the load-bearing property. Every case records the refusals the model is
expected to hit on the way to convergence — each with the reason and the address
the engine actually produces. `internal/eval` replays those against the real
validator on every `go test` run. So the corpus is not a passive fixture waiting
for Phase 2: it is a live test of the validator's diagnostics, and it cannot
drift from them silently. If someone changes a refusal's wording or moves its
address, the corpus fails, and the change becomes a review event — the same rule
the goldens follow.

It has already earned that. Writing the first three cases produced four
corrections, all to the corpus rather than the engine, and one of them
substantive: a case asserted that a `grid` node is refused, and the engine
accepted it — because PLAN.md signs "unknown-but-parseable is a warning" so a v0
document keeps booting under v1. The invented refusal would have taught the
model to expect an error this product has decided never to raise. The other
three were addresses off by one, five and one line, which is precisely the drift
the `file:line:` work exists to make visible.

It earned it again on the fourth case: both of `maximum-count-the-tasks`'
attempts were written against line 32 and the engine addresses both at line 33.
Two out of two hand-written addresses wrong is the argument for replaying
expectations against the validator rather than reviewing them by eye — an
off-by-one here is not cosmetic, because the address is the entire input the
model repairs from, and a corpus pinning line 32 would be scoring the model on
a handicap the corpus invented.

The model-facing half (does the model converge, and in how many turns) runs when
the runner lands. The corpus shape is designed so that adding the runner adds no
new data.

## Case shape

```json
{
  "id": "sobria-add-model-row",
  "order": "add a row under the input showing which model is answering",
  "base": "SOARIA.json",
  "rationale": "…the decision this case protects…",
  "attempts": [
    {
      "note": "the plausible first miss this case exists to exercise",
      "document": { "root": { "…": "a whole scene document" } },
      "expect_refused": {
        "reason": "unsigned bind \"model.current\"",
        "line": 7,
        "kind": ""
      }
    }
  ],
  "convergence": {
    "document": { "root": { "…": "…" } },
    "must_bind": ["model.name"]
  }
}
```

- **`order`** is what the user says, in the user's own register. Orders are
  deliberately underspecified — "put the tasks panel on the left" does not say
  what `weight` to use — because that is how orders arrive.
- **`base`** names a pinned scene in `testdata/`. The eleven golden scenes are
  the fixture set for the corpus *and* for the hostile property tests: PLAN.md
  asks for one harness, not two.
- **`attempts`** is the repair loop, in order. Each entry is a patch attempt
  that must be **refused**, with the reason and address the engine is expected
  to produce. This is the part that runs today.
- **`expect_refused.kind`** selects the validator that must refuse it: empty for
  the bind/parse path, `"token"` for `ValidateTokens`. The two report through
  different types (`*scene.Error` vs `TokenError`), so a corpus that only ever
  ran the bind path would mark a scene "refused" that the engine loads happily.
- **`convergence`** is the accepted end state: it must validate, and it must
  bind the fields `must_bind` names — so a document that validates by deleting
  the feature the order asked for does not count as converged.

### Why a whole document instead of a patch operation

`attempts[].document` and `convergence.document` are complete scene documents,
not `/ui add node …` operations. The patch-operation vocabulary is Phase 2's own
design work, and a corpus written in its terms would freeze that vocabulary
*before* the exercise meant to inform it — the same mistake as pinning a golden
before the phase that needs it. A full document is format-agnostic: it says what
the interface must end up being, and any patch surface that gets there passes.
When the operation format settles, cases may carry the operations alongside the
documents as an extra field; nothing here has to change.

### What `line` is counted against

`expect_refused.line` is a line of the **`document` fragment**, counting its
opening `{` as line 1 — not a line of the case file that contains it. That is
the frame of reference the measurement needs: what Phase 2 grades is a document
the model produced, and the address the model reads on its retry is an address
into that document. Numbering against the case file would pin an artefact of
where the fragment happens to sit in the JSON, and every reindentation of a case
would move an expectation that describes nothing about the engine.

The distinction is easy to miss because both numbers are plausible: in
`sobria-dim-the-footer` the refusal is at fragment line 11, while line 11 of the
case file is an unrelated `"type": "stack"`. It was measured, not assumed.

### Why `expect_refused` names a substring and a line, not a whole message

Pinning the exact message byte-for-byte would make every wording improvement a
corpus-wide rewrite, and the corpus would start voting against clearer errors.
Pinning nothing would let the diagnostic rot into "invalid scene". The reason
fragment plus the line number is the smallest pair that holds the two properties
the repair loop actually needs: the error says *what* is wrong and *where*.

## What a case must earn

A case is only worth its maintenance if it can fail for a reason someone cares
about. Two rules, both inherited from the golden discipline:

1. **Every refusal a case records must be one the engine actually produces**,
   with the address it actually produces. A hand-written expectation that no
   longer matches is drift, and the corpus test reports it as such.
2. **A case names the decision it protects** in `rationale`. "More coverage" is
   not a rationale; "the order is underspecified and the obvious reading binds a
   field that sounds signed but is not" is.

### The cases, and what each one is for

| Case | Base | Refusal path | What it is the only cover for |
| --- | --- | --- | --- |
| `raw-add-tasks-panel` | RAW | unsigned bind | An order that needs a *new node*, not an edited one |
| `sobria-add-model-row` | SOARIA | unsigned bind | A bind that sounds signed (`model.current`) and is not |
| `sobria-dim-the-footer` | SOARIA | undefined token | The token validator, which reports through another type |
| `maximum-count-the-tasks` | MAXIMUM | unsigned bind → undefined token | A repair path longer than one turn, and MAXIMUM |

The last row is the one the corpus was missing in both of its dimensions, and
both gaps were measured rather than noticed by eye. Every case before it
carried exactly one attempt, so a corpus whose stated metric is the repair loop
never pinned more than a single refusal — and one refusal cannot distinguish a
model that reads addresses from one that happens to fix the first thing it is
told. MAXIMUM was also the only pinned scene the corpus never patched, despite
PLAN.md asking for one fixture set feeding both the hostile property tests and
this corpus.

Its two refusals arrive through *different types* on consecutive turns — a
`*scene.Error` for the bind, then a `TokenError` for the token — which is the
combination `GradeBoth` exists for. A model that repairs the bind and stops
reading is scored `exhausted` here, not `converged`.

### A case must not be passable by doing nothing

A case is scored converged when its final document validates and binds every
field `must_bind` names. Nothing in that rule asks whether the model *changed*
anything — so if every field a case demands is already bound by its own base
scene, handing the base scene straight back passes the case.

That was not hypothetical. A scripted model that ignored the order and echoed
`req.Base` scored **2/4 converged**: both SOARIA cases demanded only
`model.name`, which SOARIA already binds. The finish line sat behind the
starting line, and every other test in the package stayed green throughout,
because they all ask whether a *refusal* is real — and every refusal was real.
What was wrong was the definition of done.

`TestDoingNothingDoesNotPass` enforces the rule: every case must demand at
least one bind its base scene does not already have, proven by running the real
judge against the base document rather than inferred from the bind lists. The
two weak cases were widened (`session.tokens_used`; `usage.in`/`usage.out`)
until the do-nothing model scores **0/4**, each case reporting `incomplete`
with the field it is missing. The first attempted widening used `agent.mode`
and the guard rejected it — SOARIA binds that too — which is the test doing its
job on its own author.

This is the same failure `GradeBoth` guards from the other side, and the reason
both are worth their weight: a false pass is indistinguishable from a real one
in the output, and it moves the number PLAN.md gates `/ui` on in the direction
that argues for shipping.

## Scoring

The runner has landed: `internal/eval/run.go`, driven by `cmd/arxi-eval`.

- **Converged / not converged** per case, and **turns to convergence**. One turn
  means the first patch validated; the interesting cases take two or three.
- A case that converges in one turn *every* time is a weak case: it is measuring
  the model's first shot, which PLAN.md names a vanity metric. It stays only if
  its refusal path is still worth pinning for the validator's sake.

### "Not converged" is not one fact

The runner reports five outcomes, because collapsing them points a reader at
the wrong fix:

| Outcome | What happened | Why it is its own outcome |
| --- | --- | --- |
| `converged` | Validates, and binds every `must_bind` field | — |
| `exhausted` | Still refused when the budget ran out | The model was at least making *different* mistakes |
| `looped` | Returned a document it had already been refused for | The rule above: a failure to read the address. A larger budget cannot help, and reporting it as exhaustion suggests exactly that |
| `incomplete` | Validates, but does not bind what the order asked for | There is no validator message to repair from, so another turn re-puts an identical question. The engine was satisfied and the user was not |
| `model_error` | Never produced a gradeable answer | A transport result, never a score: a flaky network must not be able to argue that `/ui` is unshippable |

Loop detection fingerprints the **canonicalised** document, not the raw bytes.
A model that returns the same wrong answer with different indentation has still
failed to read the address, and a byte hash would score that as progress.

### What `converged` does not check, and the false pass it allowed

`converged` means the document validates and binds every `must_bind` field. It
does **not** mean the screen is right, and once that gap is stated plainly the
failure it permits is obvious: a bind the validator accepts and the renderer
ignores satisfies `must_bind` while drawing nothing.

That was not hypothetical. Nine binds signed in BINDS.md §4 — `todos.count`,
`ui.focus/max/surface`, `slash.typed`, `slash.selected`,
`run.quiescent.diagnosis`, `agent.blocked.actor/blocked_on` — were accepted by
`validate.go`, computed by `internal/fold` on every event, and read by
`resolveBind` nowhere, so each fell through to the `"[…]"` placeholder.

`maximum-count-the-tasks` lists `todos.count` in `must_bind`, and its own
recorded convergence document binds it. Measured before the fix:

```
GRADER: converged=true  missing=[]
SCREEN: │[…]          │
```

This is the same class as `row_template`, and one step worse in placement.
`row_template` was *reachable* from a shipped case; this one was already
sitting in the corpus's ground truth — the document the corpus holds up as the
correct answer. A model reproducing it exactly would have been scored right for
an empty panel, and Phase 2's number would have carried that.

The direction matters. A corpus that under-credits the model gets argued with;
a corpus that over-credits it is the one nobody audits, because the number
looks like good news.

The fix was an implementation rather than a refusal, which is the opposite of
the `row_template` call and for a stated reason: `row_template`'s semantics
(relative `row.*` binds) are unsigned and belong to Phase 3, so drawing it
would have invented format ahead of the phase meant to design it. These nine
had nothing left to design — the value already existed and was correct in
`fold.State`, discarded one function short of the frame.

**The guard lives in `internal/engine`, not here.** `internal/eval` cannot
catch this: it grades documents and never renders one, so removing the
projection again leaves the whole `eval` package green while `engine` fails.
That is a real limit of what the corpus can self-check, and it is written down
rather than papered over — the corpus verifies its refusals against the
validator, but it cannot verify its convergence documents against the screen.

### The limit, paid off

That paragraph left one question open: whether `must_bind` should eventually
assert on a rendered frame. It should, and it now does —
`TestEveryCorpusMustBindFieldReachesTheFrame` renders every case's recorded
convergence document and requires each `must_bind` field to put a value on
screen.

`Converged` asks whether a bind is *present* in the tree. That is the right
question for the grader to ask: it judges documents a model wrote, and needing
a renderer to do it would point the harness at the engine it must stay
independent of. But presence is strictly weaker than projection, and the whole
defect class lives in the gap between them — a bind can be signed, accepted,
folded on every event, and still never reach a frame.

So the question is asked from the other side. The test lives in
`internal/engine` because the import direction decides it: `internal/eval`
imports `scene`, `theme` and `ui` — no renderer, no fold state — while
`engine` already imports both and adding `eval` closes no loop. The corpus
stays a document grader; the engine checks the corpus's answers against the
screen.

Two details are load-bearing. Each bind's witness value is distinct, so a
projection returning its neighbour's field fails instead of coincidentally
matching. And each is short enough to survive MAXIMUM's sixteen-column Tasks
pane: a longer witness gets word-wrapped, the value is on screen, and the
substring search misses it anyway — a false alarm shaped exactly like the real
defect, which is the kind that teaches a reader to ignore the guard.

### Fifth instance: a case that was present and still wrong

Building that test found one immediately, and it was a new shape.
`session.tokens_used` **had** its case in `resolveBind`. The case returned the
literal `"0"`, under a comment stating the budget was not yet wired from
`run.started`. It had been wired since: `fold.go` captures `budget_usd` into
`BudgetMicrounits`, accumulates `cost_usd` into `CostMicrounits`, and
`deriveSessionTokensUsed()` subtracts them on every fold. The comment outlived
the condition it described.

A stale justification is worse than a missing case, because both signals that
normally survive this class are inverted:

- A missing bind draws `[…]`, which looks wrong on screen. This drew `0` —
  the bind's own signed empty state in BINDS.md §4.1 — so no frame at any
  budget ever looked like an error.
- The structural audit decides a bind is handled by reading case labels out
  of the source. The label was there, so the guard read green.

`TestMaximumSceneRenders` asserted `│0`. That assertion was a transcription of
the constant rather than a reading of the fold, which made the one test
positioned to catch the defect into the thing protecting it. It now asserts
`9999`, derived from the events the test itself declares — `budget_usd` 10.0 is
10000 microunits, the single response costs 0.001 — so any constant fails it.
Two goldens moved one cell each; the diff is the review event the rule intends.

The class has now moved one level in, twice: **field → name → body**. Each
guard was written at the level the last defect lived at, and the next instance
arrived one level deeper. `TestEveryBindCaseBodyReadsTheFoldState` closes the
third: every bind case in a switch must mention the fold state parameter. It is
deliberately weak — it cannot tell a correct projection from a wrong field, and
the behavioural tests already do that — but it is the one property a constant
cannot satisfy.

Measured with the constant reinjected rather than argued: the label audit
passes, and every test under `internal/eval` passes. The two new audits are the
only things in the tree that fail, and they fail on different axes — one reads
the source, one reads the frame. That is the reason to keep both.

### One judge, two callers

The corpus test and the runner grade through the same code (`Grade`,
`GradeBoth`, `Converged` in `internal/eval/grade.go`). The test asks about
documents a human wrote into a case file; the runner asks about documents a
model produced. Two copies of that logic would let the corpus test stay green
while the runner scored the identical document differently, and the promise
that every recorded refusal is one the engine really produces would hold only
for the half nobody grades.

One difference is deliberate. A case declares which validator its attempt
targets, because the author knows what they wrote; a model does not. Model
output is therefore graded with `GradeBoth` — bind path first, matching the
engine's load order — so a document that satisfies the named validator and is
still refused at load time cannot be scored as converged. A false pass is the
worst result this harness can produce, because in the output it is
indistinguishable from a real one.

### What the model is told

The prompt carries the order, the base scene, and both vocabularies: the signed
binds from `scene.SignedBinds()` and the theme's tokens. The bind list is read
from the validator's own inventory rather than written into the prompt, because
a hand-copied list would be a fourth copy of something that has already drifted
once — and its failure mode is the expensive one: the model is told a signed
bind does not exist, avoids it, and the corpus records that as the model's
failure rather than the prompt's.

The retry carries **every** prior attempt and its refusal, not just the last.
Looping is one of the outcomes reported here, and a model shown only its most
recent attempt cannot tell that it is repeating itself — the harness would be
scoring a handicap it created.

A markdown code fence around the answer is stripped, and nothing else is. The
measurement is whether a model can patch a scene and repair from a `file:line`,
not whether it can suppress a formatting habit every chat-tuned model has;
scoring a correct document as a syntax error over three backticks would report
a presentation problem as a capability one. Salvaging JSON out of prose would
be the opposite error — the harness answering for the model — so prose is left
to fail.

### A refusal that is not the model's

A gateway or proxy that answers *instead of* the model is reported as
`model_error`, never as a score. This rule is written from an incident, not as
a precaution.

The first real run reported `0/3 converged, looped=3`. Every case had in fact
been answered by the API gateway with a plan-block message — "Free-plan credits
can't be used with the Genspark API" — delivered with HTTP 200 and
`finish_reason: "stop"`, shaped exactly like a successful completion. The
runner graded that prose as the model's document, refused it as invalid JSON,
saw the identical prose on the retry, and correctly concluded the model was
looping.

Every layer behaved as designed and the conclusion was false. The runner
already separates `model_error` from a score precisely so transport trouble
cannot depress the number a shipping decision rests on, and that separation was
defeated by a failure arriving as a 200. The lesson kept in the code: **a
status-code check is not a transport check.**

Detection is narrow on purpose. Over-reaching is the mirror-image error:
excusing genuine malformed output as transport noise would delete real failures
from the scores and inflate the measured capability. A model that returns bad
JSON is still scored as a model that returned bad JSON.

### Running it

```bash
go build -o arxi-eval ./cmd/arxi-eval
OPENAI_API_KEY=... arxi-eval -model <name> -v
```

`arxi-eval` is a separate binary from `arxi-tui`. The shipped interface must not
carry an eval harness, an HTTP client for a model API, or a reason to read
`OPENAI_API_KEY`.

Its exit status reports whether the *harness* ran, never whether the model
scored well. No threshold is set here, and that is deliberate: PLAN.md's
instruction if the model cannot do this reliably is not "tune the threshold" —
it is that the document must say so.
