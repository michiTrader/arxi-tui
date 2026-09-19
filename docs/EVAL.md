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

## Scoring (when the runner lands)

- **Converged / not converged** per case, and **turns to convergence**. One turn
  means the first patch validated; the interesting cases take two or three.
- A case that converges in one turn *every* time is a weak case: it is measuring
  the model's first shot, which PLAN.md names a vanity metric. It stays only if
  its refusal path is still worth pinning for the validator's sake.
- **Not converged** includes the case where the model loops — producing the same
  refused document twice is a failure to read the address, which is precisely
  what the `file:line:` work exists to make possible.

No threshold is set here. PLAN.md's instruction, if the model cannot do this
reliably, is not "tune the threshold": it is that the document must say so.
