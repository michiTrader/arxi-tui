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

### The frame test's reach, measured

`TestEveryCorpusMustBindFieldReachesTheFrame` is the strongest of the three
projection guards — it is the only one that looks at a drawn value — and it is
also the narrowest. It can only check the binds some case names in `must_bind`,
and that set was never chosen to be a coverage list. Measured rather than
assumed: **7 of the 30 signed binds** appear in a `must_bind` anywhere in the
corpus. Twenty-three have no check that inspects a projected value at all.

The seven are worth reading closely, because the way `session.tokens_used` got
in is the uncomfortable part. It is not there because anyone judged the budget
display worth pinning to a frame. It is there because
`TestDoingNothingDoesNotPass` found two cases a do-nothing model could pass and
needed some bind SOARIA did not already have; `session.tokens_used` was the
field that fit. Had that widening reached for a different bind, the frame test
would have rendered right past the fifth instance — the defect it was written
in response to — and reported green.

So the frame test's coverage of the actual defect was a coincidence of an
unrelated repair. That is not a reason to distrust it; it is a reason not to
let the corpus decide which binds get checked at all.

### Asking from an axis the corpus cannot move

`TestEverySignedBindProjectionVariesWithItsFoldField` closes that. For every
signed bind `fold.State` carries a field for, it perturbs **only that field**
and requires `resolveBind` to return something different. The bind list comes
from `scene.SignedBinds()` and the field mapping from `fold.State`'s own json
tags, so neither the corpus nor a case author can widen or narrow what gets
measured.

It is weak in exactly the way the body audit is weak, and deliberately so: it
cannot distinguish a correct projection from one returning a plausible wrong
field, and it does not try — the goldens and the behavioural tests own that
question. What it proves is the single property a constant cannot fake, which
is that the output depends on the input. Twenty-three scalar binds are checked
where the corpus reached seven.

Two design points are load-bearing, both of them about what the guard refuses
to excuse quietly:

- The two signed binds with no fold field — `user.input.submitted` and
  `session.new_milestone`, both documented pulses in BINDS.md — are exempted
  **by name, with their reason recorded**. Skipping unmappable binds silently
  would mean a bind that later disappears from `fold.State` gets excused by the
  same gap that legitimately covers a pulse.
- A run that checks zero binds is a hard failure. If a refactor renamed the json
  tags or emptied `SignedBinds()`, every bind would fall into a skip bucket and
  the file would report success having measured nothing — the same false-pass
  shape the do-nothing model exposed in the grader, one layer down.

Verified the way this document requires: with the fifth instance's `return "0"`
reinjected into `render.go`, the guard reports `session.tokens_used` inert; with
the fold-reading projection restored, it passes and `render.go` is byte-identical
to where it started. A guard that would not have caught the defect it was
written after is not worth its maintenance, and that is checkable rather than
arguable.

No sixth instance surfaced. Every other scalar bind already varies with its
field — which is a result, not an absence of one: it is the first time this
class has been swept across the whole signed surface instead of the part some
other test happened to reach.

### The composite axis, and a correction to the paragraph above

"Swept across the whole signed surface" was too strong, and the overstatement
is worth keeping visible rather than editing away. That guard perturbs a fold
field and asks `resolveBind` whether its answer changed. `resolveBind` returns
a string, so any field that is not a scalar falls out of it here:

```go
if !perturbScalar(perturbed.Field(idx)) {
        // Composite field; not this guard's axis.
        continue
}
```

The comment is true about the axis. The mechanism is a silent skip bucket —
the precise shape that guard's own second design point forbids. Its reason for
listing the two pulses **by name** was that excusing the unmappable in silence
would let a bind that later vanishes from `fold.State` slip through the same
crack. Five binds were already going through a crack one kind-switch wide, and
nothing named them. Measured:

```
signed                          30
scalars checked by that guard   23
skipped silently                 5   agent.todos, chat.history,
                                     slash.matches, team.members,
                                     agent.blocked.blocked_ref
unmapped, exempted by name       2   user.input.submitted,
                                     session.new_milestone
```

23 + 5 + 2 = 30, so the five were the entire remainder, and "the whole signed
surface" was in fact 23/30 of it.

`TestEverySignedCompositeBindIsDrawnOrRecordedUnprojected` closes that. For
every signed bind whose `fold.State` field is a slice or map, it builds a
one-element witness by reflection, renders the bind, and requires some frame
to move. Binds in `acceptedUnprojectedBinds` must move *nothing* — so the prose
claim and the frame are now checked against each other in both directions,
which is new: the label audit only ever checked that a listed bind had not
quietly gained a case, never that it still drew nothing.

#### The first draft passed, and was wrong in the way that matters

That draft carried a hand-written map from bind to the one node type that draws
it — `list` for `agent.todos`, `markdown` for `chat.history` — rendered each
composite through its recorded type, passed, and caught an injected defect that
blanked `agent.todos`.

Then the same injection was tried on `chat.history`: its `resolveBind` case was
changed to compute `ChatHistoryMarkdown()` and discard it. **The entire suite
stayed green, that draft included.** `chat.history` reaches the frame through
two independent paths — `renderMarkdown` reads `state.History` directly, while
`resolveBind` feeds `text` and `spinner` nodes and every `when` gate through
`evalWhen` — and rendering through one recorded node type exercised the first
and never touched the second.

The map was not a convenience. It was a second inventory, written by exactly
the kind of judgement the scalar guard refused to let the corpus make, and it
silently decided which half of a bind's surface got measured. Swept across
every node type instead — the list parsed out of `renderNode`'s own switch, so
a new node type is swept the moment it exists — the real shape appears:

| Bind | Frame moves in |
| --- | --- |
| `chat.history` | markdown, text, spinner |
| `thinking.text` | markdown, text, marquee, spinner |
| `user.input` | input, text, spinner |
| `agent.todos` | list |
| `slash.matches` | list |
| `team.members` | — (placeholder, on the record) |
| `agent.blocked.blocked_ref` | — (placeholder, on the record) |

Two binds are multi-path. A guard that picks one path per bind is guessing, and
the guess was wrong before the file was ever committed.

#### Verified by injection, twice, against the tests that already existed

The question a new guard has to answer is not "does it pass" but "what does it
catch that the tree did not already catch". Both answers were measured:

- **R10b** — `chat.history`'s `resolveBind` case blanked. Before: the whole
  suite green, including the first draft of this guard. After the redesign:
  this test is the only thing in the tree that fails, and it names the split
  precisely — *draws in node type(s) markdown, but its resolveBind case returns
  the same value for both states*.
- **R10d** — `team.members` drawn through `renderList`'s `default` branch, so
  it gains no case label at all. Invisible to the label audit by construction,
  which reads case labels; caught here, because the frame moved while
  `acceptedUnprojectedBinds` still claimed Scene 9 was blocking.

A third injection (`R10c`, `team.members` given a real case label) is caught by
*both* this guard and the label audit. That one is reported as overlap rather
than as a win: it is the reason R10d was written, since a guard justified only
by defects another test already catches has not earned its maintenance.

`render.go` is byte-identical to its committed state after every restore — 0
changes in porcelain — so nothing here moved production.

#### What it still does not do

The same weakness as its two siblings, and deliberately: it cannot tell a
correct rendering from a plausible wrong one. A list drawing its todos in the
wrong order, or the actor where the task belongs, passes here. The goldens and
the behavioural scene tests own that question. What it proves is the property a
placeholder cannot fake — that the frame depends on the state — across the five
binds that previously had no check of any kind on that axis.

With this, all 30 signed binds are measured for projection: 23 scalars on
`resolveBind`, 5 composites across all 11 node types, 2 pulses exempted by name
with their reasons on the record.

### The other half of a node: the token it is drawn under

The three guards above all ask the same kind of question — does the bind's
*value* reach the frame. A scene node declares two things, and nothing asked
about the second. A value that arrives under the wrong token is on screen and
wrong, and every projection guard passes it, because the text is there; only
the styling belongs to somebody else.

Swept as a matrix (every node type in `renderNode`'s switch × bordered and
borderless × the container declaring a token of its own or not), rendering a
child that declares `{"style": "…"}` and asking whether that token survives to
the frame. Sixteen combinations draw the child. One shape did not keep it:

	box       bordered=false  childTokenKept=true
	box       bordered=true   childTokenKept=false   <-- here
	overlay   bordered=true   childTokenKept=true
	row/stack (both)          childTokenKept=true

`renderBox`'s content loop flattened each child row with `l.Text()` and
re-emitted it as one span under the box's token — or under none. Everything
the children said about themselves was discarded at the border.

#### It is a repeat, and that is what makes it a finding

`padLine` already carries the rule in writing, and it was written from a bug
that had shipped: *"Flattening the line into its first span is how a dim menu
row came out undimmed: the padding is chrome, and chrome must not restyle the
content it fills around."* `wrapWithBorder` — the **bordered overlay** path —
was fixed to honour it and says so in its own comment.

So three of the four bordered/borderless drawing paths obeyed the rule, one did
not, and nothing in the tree compared them. A rule honoured in three places out
of four is not a rule; it is a coincidence, and the fourth place is found by
enumeration or not at all. That is why the guard sweeps the matrix instead of
testing the box.

#### Reachable from a shipped scene, and already visible in a golden

MAXIMUM's Tasks panel is a bordered box around a list. `renderList` mints its
empty state as `{Text: "no tasks", Style: "dim"}`, and `MAXIMUM.styled`
recorded what actually reached the screen:

	«border:│»no tasks          «border:│»

Bare. The `dim` token was created by the renderer and deleted by the box one
call later. The golden had been pinning the defect as correct output since the
day it was generated — the same shape as the `row_template` case, where five
places agreed a field was live and the engine read it in zero.

#### Verified by injection, and by separating the two golden changes

The fix moves exactly two lines of `MAXIMUM.styled`, and they are not the same
kind of change. Measured by emitting real ANSI through the factory theme before
and after:

- **Line 5** — `no tasks` gains `«dim:…»`. In ANSI: `\e[2m` appears where there
  was nothing. This is the defect, repaired.
- **Line 2** — the banner's single `«banner:…»` span becomes two adjacent
  `«banner:…»` spans. In ANSI: the same bold, closed and reopened at the
  content/padding boundary. Same attribute, same cells.

Stripping SGR codes from both emissions, **the plain text is byte-identical** —
no cell moved, so invariant 1 holds. Reporting "the golden changed" without
that split would have hidden a real repair behind a cosmetic span boundary, or
the reverse.

The injections:

- **R11b** (`wrapWithBorder` welded again — the *sibling* path, reintroducing
  the identical defect where it had already been fixed once): every golden
  stays green, because no shipped scene uses a bordered overlay. This guard is
  the only thing in the tree that fails. That is the one that earns its keep.
- **R11a** (the weld restored in `renderBox`): caught by this guard *and* by
  `TestMaximumSceneStyledGolden` — but only because the golden was just
  updated. Reported as overlap, not as a win.
- **R11c** (the box computes its own token and discards it, so the padding it
  adds goes unstyled): caught by the **golden**, not by this guard — correctly.
  The guard does not require a container to style its own chrome, and must not:
  MAXIMUM's banner depends on exactly that, and it is the distinction the fix
  draws. Chrome may style chrome; it may not restyle the content it wraps.

#### What it does not check

Whether the token is the *right* one. A child asking for `warn` and getting
`warn` passes here even if the theme maps `warn` to something illegible. The
goldens own appearance; this owns the property that a declaration survives the
trip to the frame at all.

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
