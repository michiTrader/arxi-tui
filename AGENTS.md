# arxi-tui

A terminal interface that is data, not code. The whole project rests on one
thesis:

> **The user interface is a document, and the running instance can rewrite that
> document in place — including the agent rewriting it on the user's command.**

Everything else is a consequence of that. The chrome of arxi-sim was a closed
vocabulary with named owners; it could be chosen from but never extended. Here
the scene tree, the style tokens, and the action namespace are open by design,
and the factory-default interface is written with exactly the same format the
user has — that equality is the product.

Before touching code, read `docs/PLAN.md` (architecture, phases, invariants),
`docs/SCENES.md` (the eleven golden scenes that froze the format), and
`docs/LESSONS.md` (what arxi-sim and arxi already solved; do not re-derive it).

---

## Language policy

**EVERYTHING is written in English. No exceptions. No Spanish anywhere.**

The user of this project speaks Spanish and will often address the agent in
Spanish. **That changes nothing about what goes into files.** Conversation with
the user may be in Spanish; the repository is English-only. This is the single
most common way this rule gets broken — the language of the request leaks into
the language of the artifact.

This covers, without exception:

- **Identifiers** — variables, functions, types, constants, struct fields,
  packages, file names, directory names.
- **Comments** — including doc comments (godoc) and inline notes.
- **Test names and table-driven test case descriptions**, plus every `t.Fatalf`
  / `t.Errorf` failure message.
- **User-facing strings** — CLI output, usage text, error messages, diagnostics.
- **JSON field names, bind paths, action names, and event type names** in the
  wire format and payloads. The scene format the community will one day write
  is English-only for the same reason: every tool that ingests it must agree.
- **Commit messages**, branch names, PR titles and PR descriptions.
- **All documentation** — design docs, specs, README, and this file.

Before writing a single line, re-read this rule. If a comment, identifier,
string or commit message you are about to write would naturally come out in
Spanish, translate it **before it goes in the file**. Never write it in Spanish
"to fix later" — arxi learned that cost the hard way: the original repository
was Spanish, and the translation pass — done twice — produced two classes of
damage that compiled cleanly and kept every test green:

- **Comments replaced by a placeholder.** 540 argued comments were collapsed
  into `// Implementation note.`, deleting the entire body of reasoning about
  why each decision is right. Those comments are the primary defense against a
  later "simplification" of a load-bearing decision. Losing them is worse than
  leaving them untranslated.
- **Word-for-word substitution.** `no` became `not` regardless of context,
  yielding "with not runtime"; even flag names were corrupted (`go build -o`
  became `go build -or`).

If asked to translate anything here: read the paragraph, understand the
argument, write the English that makes the same argument with the same force.
Never map token to token, and never drop content you have not replaced with an
equivalent. Do not translate unrelated content as a side effect of some other
task.

### Why this matters beyond style

The failure messages in this project are load-bearing. Go does not give
exhaustive `match`, so a `switch` missing a variant compiles fine and the test
suite is the only net that catches it. Those test messages are the current
documentation of the decision they protect — they must name the consequence and
the remedy, and they must be readable by every contributor and every tool that
ingests them.

## Comment policy

Comments explain **why a decision is right and what breaks otherwise**. Never
what the code does — the code already says that.

```go
// BAD: increments the counter
// GOOD: The chat pane contracts first and the input/banner never do: an
// interface the user is typing into must not disappear because a scene asked
// for too much. Reversing this order lets a downloaded preset steal the only
// row that lets them fix it.
```

If a comment would still be true after the surrounding logic was rewritten
differently, it is describing behavior, not defending a decision. Rewrite it or
delete it.

## Test policy

Every test protects a decision, and its failure message names the
**consequence** and the **remedy**. `t.Fatal("mismatch")` does not satisfy this
contract.

The eleven scenes in `docs/SCENES.md` are the first goldens. The rule inherited
from arxi-sim: **default goldens never move; a configured change is pinned in
its own mutation family, named after the feature that caused it.** When a scene
changes, the golden diff is a review event, not noise.

**Nothing merges without `go test ./...` passing.** Correct the code, not the
test: when a test fails, the default assumption is that the test is right.

Two test decisions are made now so they are never re-litigated with code on
top: **the escape-hatch test (immovable double Ctrl-C, with its timing) runs
on Windows CI from Phase 0** — LESSONS.md records that Windows delivered
ctrl+key as a raw byte and the claim changed twice, and an "immovable"
gesture proven only on Linux is a slogan — and the **SCENES ↔ BINDS audit
test** (every `bind`/`when` string in the eleven scenes resolves to a signed
row in `docs/BINDS.md`, and a signed row no scene uses is a warning) is the
direct port of arxi-sim's `TestEveryDeclaredKeyIsDrawn` machinery: port it,
do not invent it.

## Architectural boundaries

- The **fold stays pure and host-owned**. The scene says form; the folded state
  says content. The fold never waits on a scene, an animation, or a plugin.
- **The panic gesture is immovable.** No scene, preset, or plugin can capture
  the escape hatch (`Ctrl-C` twice, or `-scene ""` at start) that restores the
  raw scene. This invariant is what makes community content safe to run at all.
- **Plugins propose, they never write.** The same rule as arxi-sim and the arxi
  kernel: a plugin effect enters through a gate, is attributed in the log, and
  cannot forge sequence or authorship.
- Imports of `internal/ext`, `internal/scene` (and later the wasm runtime) must
  not pull in the UI packages; keep the arch-test seams from arxi-sim
  (`go list`-based import checks).

## Commit policy — MANDATORY, not exceptions

**Commit after every file you create or modify.**

This is not process hygiene, it is data-loss prevention with a track record on
the sibling projects. Concretely:

1. After every `Write`/`Edit` call — or a very small, tightly coupled group of
   files forming one atomic change (a source file and the fixture it needs) —
   immediately `git add` + `git commit` with a descriptive conventional-commit
   message **in English**.
2. **Push in the same breath as the commit — `git commit && git push`.** Not
   "before ending the turn", not "once the feature works": the push belongs to
   the commit, and a commit that has not been pushed is not saved. The design
   work is signed and the working copy lives on one disposable disk: the
   private remote must exist **before `go.mod`**, because the Go module path is
   derived from the repo URL and guessing it costs a later migration.

   **This rule has a measured price.** The development sandbox has been
   destroyed and re-cloned from the remote **nine times** across recent
   sessions, without warning and mid-task. Six times everything survived,
   because every commit had been pushed and the only loss was a half-applied
   edit. The fifth time two commits — a complete audit and a 44-reference
   rename, both green — existed only on the local disk and were lost entirely;
   they had to be reconstructed from scratch. The work was not lost to bad
   luck, it was lost to the gap between `commit` and `push`. Close that gap
   every time.

   **The seventh destruction, and the failure mode this rule actually has.**
   It happened again, and this time nothing had been committed at all: a
   measured defect, a working fix, and a guard verified to fail without it —
   all of it green on disk, none of it in a commit, because the work had not
   reached a point that felt like a milestone. Every word of the loss was
   avoidable and the rule above already said so.

   The lesson is not "push more". It is that **this rule is obeyed at the
   moment code compiles, not at the moment a task feels finished** — and
   "feels finished" is the judgement that fails, because it is made by the
   same agent that is absorbed in the work. There is no such thing as a
   change too small or too provisional to commit; a commit that is later
   rewritten costs a rebase, and an uncommitted one costs the whole turn.
   The instant `go build` passes, `git commit && git push`, even mid-defect,
   even with the fix half-argued.

3. **Open the pull request early and keep pushing to it.** A PR is not the
   ceremony at the end of a finished feature; it is the durable record of work
   in progress. Open it after the first pushed commit and let later commits
   land on the same branch. A branch with an open PR is reviewable, linkable,
   and survives the loss of every local file.

4. Do not batch a whole feature into one commit at the end. This is in tension
   with the squash convention some workflows impose, and the tension resolves
   in favour of the separate commits: each one here carries the argument for a
   single decision, and collapsing five of them destroys the record of which
   defect motivated which change. Squash only when the commits are genuinely
   one change split by accident.

5. Before ending a turn, verify `git status` is clean **and** that
   `git log origin/<branch>..HEAD` is empty. A clean working tree with
   unpushed commits is the exact state that lost work above, and it looks
   identical to a finished turn.

6. **Also verify `git log origin/main..origin/<branch>` is empty, or that
   an open PR covers it.** Rule 5 checks that the local disk is safe; it
   says nothing about whether the work is *reviewable*, and the two come
   apart in a way that has now happened.

   **Measured, on the turn that added this rule.** The sandbox was
   destroyed for the **sixth** time, mid-commit. Rules 2–5 worked exactly
   as written: every pushed commit came back, and the one unpushed half
   was the only casualty. But the PR had been opened early per rule 3,
   with the first commit — and it was merged while the remaining four
   commits were still being pushed to the same branch. Those four landed
   on a branch whose PR had already closed. Nothing was lost and nothing
   was unpushed, so rule 5 reported a clean finish; 657 lines sat on the
   remote with no open PR pointing at them, which is invisible in exactly
   the way an unpushed commit is.

   The remedy is not to open the PR later — that reintroduces the risk
   rule 3 exists to remove. It is to re-check the PR at the end of the
   turn as well as at the start: an early PR is a live object, and a merge
   can happen between the first push and the last.

   **The eighth destruction, and rule 6 predicting its own event.** It
   happened again, and this time the rules held completely: all five
   commits of the turn came back intact, because each had been pushed the
   moment it compiled, and there was nothing to reconstruct. What is
   worth recording is the second half. While the sandbox was down, the
   open PR was merged and `master` advanced — exactly the scenario the
   paragraph above describes. It had been written one turn earlier, from
   a single occurrence, and it described the next occurrence before it
   happened.

   That is the argument for writing these down while they are fresh, and
   the reason the count in this section is maintained. A rule derived
   from one event reads like superstition until the second event arrives;
   this one arrived one turn later. **On restore, always re-read
   `origin/master` before assuming the branch state from memory** — the
   check costs one `git fetch` and it is the check that told this turn
   there was nothing to rebuild.

   **The ninth destruction.** Clean restore, nothing lost, nothing to
   rebuild — the sixth time the rules produced that outcome. The only
   cost was environmental: the sandbox ships without a Go toolchain, so
   every restore begins by installing Go 1.25 before anything can be
   built or measured. Budget that, and do not report a test result until
   it has actually run in the new sandbox.

   **The tenth destruction.** Clean again, and the count is now more
   useful as a ratio than a tally: ten destructions, seven of them
   costing nothing at all. The two remaining lessons are both
   environmental rather than procedural — Go is absent on every restore,
   and `git fetch` before assuming any branch state — and both were
   already written here before this turn needed them.

   **The eleventh destruction.** Clean: eleven destructions, eight of
   them costing nothing. Both standing lessons applied again unchanged,
   which is now the whole content of this entry — Go absent, `git fetch`
   first, nothing to rebuild. When a restore stops teaching anything
   new, stop lengthening the list and keep the ratio: the procedural
   rules are settled, and the only open cost is that the toolchain has
   to be reinstalled before any number can honestly be reported.

   **The twelfth destruction.** Twelve destructions, nine costing
   nothing. Keeping to the rule above, this entry records only the
   ratio; the two standing lessons applied unchanged again.

   **The thirteenth destruction.** Thirteen destructions, ten costing
   nothing. Ratio only, per the rule above.

   **The fourteenth and fifteenth destructions.** Fifteen destructions,
   twelve costing nothing. Ratio only, per the rule above.

   The fifteenth is worth two sentences because it landed differently: it
   arrived *mid-command*, while a single `cat >> docs/LESSONS.md &&
   git commit && git push` chain was in flight. Three commits of the turn
   were already on the remote and all three came back; the only casualty
   was the documentation append, which had not reached a commit because
   it was chained behind one. **A chain that ends in `git push` is not a
   saved edit until it runs** — the write and the commit are one command,
   so an interruption between them loses the write with no trace in
   `git status`. Write the file, commit it, then continue; do not batch
   the edit and its commit behind a `&&` that also has to survive a test
   run.

   **The sixteenth destruction.** Sixteen destructions, thirteen costing
   nothing. Ratio only, per the rule above — it arrived mid-counterfactual,
   with an injected field on disk and the audit commit already pushed, so
   the injection died with the disk and nothing had to be undone. That is
   the intended behaviour of an uncommitted probe, not a lucky outcome:
   **an injection is the one edit that must never be committed**, so the
   disposable disk is exactly where it belongs.

   **The seventeenth destruction.** Seventeen destructions, fourteen
   costing nothing. Ratio only, per the rule above. The open PR was
   merged while the sandbox was down — the scenario rule 6 describes,
   now for the third time — and `git fetch` before assuming any branch
   state is again what turned it into a non-event.

   **The eighteenth destruction.** Eighteen destructions, fifteen
   costing nothing. Ratio only, per the rule above; the open PR was
   again merged while the sandbox was down, for the fourth time.

   **The nineteenth destruction.** Nineteen destructions, sixteen
   costing nothing. Ratio only, per the rule above.

## Working rules

**One step at a time, in order.** The bind vocabulary is the only hard
prerequisite Phase 0 cannot code around, so it lands in two beats (see
`docs/PLAN.md`): its **bootstrap subset** — the few binds the raw scene uses
— freezes *before* Phase 0 code, and the **full inventory** in
`docs/BINDS.md` freezes before Phase 1 pins its goldens. Phase 0 ends with the
raw scene (two nodes) running, not with a "complete engine".

**Dependencies are allowed, but named.** arxi-sim's "stdlib only" claim was
already false (`charmbracelet/x/ansi` ships in go.mod); the honest rule the
product needs is the install rule, not the dependency rule: **the user installs
arxi, not arxi's dependencies.** Go compiles them in; Termux gets a dedicated
`GOOS=android` artifact; any dependency that brings a runtime requirement onto
the user's machine is rejected, and any new dependency names its cost in the
commit that adds it.

**`file:line:` on every error, always.** A refusal without a location is a bug.

**Verify, do not assume.** Before reporting a number, measure it. Before
saying tests pass, run them. Counts, file contents and git state have all been
wrong from memory on the sibling projects.

**The instrument is not exempt.** The progress audit exists because a
completion figure was asserted for thirteen turns and never measured; three
turns were then spent guarding its *denominators*, and each guard was
justified by a measurement. Its *numerators* were never checked at all, and
both of them were wrong — one counted `strings.Repeat` as an implemented
animation property, the other counted a node type as rendered because of an
unrelated `switch` in the same function. Both errors ran in the flattering
direction, and the animation one disarmed the single state that subtest can
fail on.

The general rule: **when a measurement is a ratio, the numerator needs the
same scrutiny as the denominator, and it usually gets less** — a denominator
that moves is visible in the reported total, while a numerator that
over-counts just looks like progress. The specific rule, paid for five times
in `internal/engine` now: **when a guard can ask the type checker, matching on
an identifier name is not a shortcut, it is a different question.** A bare
`sel.Sel.Name == "X"` matches `pkg.X`, `otherType.X` and `n.X` alike, and a
bare `id.Name == param` matches a local that shadows the parameter.

**A rule derived in one file is not finished until it has been pointed
somewhere else.** Both rules above were written from defects in the progress
audit, and the audit is where they stopped being applied. Grepping the tree
for name-matching guards took one command and found three: two already
resolved types and were cleared as negative findings, and the third —
`TestEveryBindCaseBodyReadsTheFoldState`, in a different file, written at a
different time, for a different defect class — had the identical bug. Its
`readsState` numerator is satisfied by any local spelled like the `fold.State`
parameter, so a bind case answering `state := "sobria"; return state` scored
as reading the fold. That is the exact shape the test exists to reject,
certifying itself.

The direction repeats too, and it is the part worth internalising: that axis
can only fail on a case that does *not* read the state, so the false positive
did not inflate a count, it **disarmed the single condition the subtest could
fail on**. Three over-counting numerators in this package now, all three
flattering, two of them pre-empting their own failure mode. When a numerator
counts the passing state, an error in it is not a measurement error — it is
the guard switching itself off.

**Prove the fix in both directions, with a counterfactual you actually run.**
Every one of these was settled by constructing the defect and measuring the
old and new matcher over the same probe, never by argument. A guard that
passes on a clean tree has demonstrated nothing; the claim is that it fails on
the defect, and that claim is cheap to test and routinely false.

**A test that can only fail while another test is already failing is that
test, not a second one.** `TestStylingAShippedSceneChangesItsFrame` was written
as the reachability half of the styling sweep: does any of this happen to a
real document? It exercises only the node types the sweep has *already*
flagged, so the moment the sweep goes green it has nothing to stamp and
`t.Skip`s. Measured: it has skipped since the commit that introduced it — the
commit that fixed the defect and silenced both checks together. It is armed
only when it is redundant and switched off exactly when it would be the sole
evidence. This is the numerator rule arriving at a whole test instead of a
count, and the tell is the same: the passing state and the can-fail state are
the same condition. **`t.Skip` is a pass**; a skip that is permanent is a
deleted test that still prints.

**A sweep over one axis silently exempts every other axis.** The styling sweep
iterates node *types* out of `renderNode`'s dispatch, and a node reached
through `prefix`, `suffix` or `row_template` is a node *position* — no type
name identifies it, so no widening of that loop ever arrives there. Blanking
the declared token on both of `renderMarquee`'s nested spans left the **entire
suite green, goldens included**. When a guard enumerates a dimension, ask what
the other dimensions of the same object are; the enumeration reads as coverage
and the reader cannot see the axis that was not chosen.

**A guard that fixes a defect "everywhere" has fixed it everywhere along one
axis.** `when` was once honoured by a row and an overlay and dropped by every
other node type; a style token was read by four node types and dropped by the
rest. Both were closed by moving the work into `renderNode`, and
`withFocusGlow`'s comment states the reason well: that function is the one
every node passes through, so "some node types obey and others do not" becomes
unrepresentable rather than merely tested for. That claim is true, and it is
about node **types**. Measured this turn: a node under `prefix` or `suffix`
never reaches `renderNode` at all — `renderMarquee` reads `prefix.Bind`,
`prefix.Text`, `prefix.Style` straight out of the struct — so the same `when`
was accepted by the validator and ignored by the renderer for the third time,
in a position the chokepoint cannot see. **A chokepoint is only a chokepoint
for the traffic that goes through it**; when a fix is described as making a
defect unrepresentable, name the quantifier out loud and then ask what the
*other* quantifiers over the same object are.

**The counterfactual is not a formality, and it catches the test as often as
the code.** The guard written this turn passed on the clean tree and was
wrong. Its `when` probe first used a *satisfied* gate — which correctly renders
identically to no gate at all, so it accused the engine of a silent drop
exactly when the property worked, and failed against the renderer that had just
been fixed. The replacement used `!agent.working`, which reads like the obvious
negation and is not: no `!` operator exists in this engine, `evalWhen` resolves
the whole string through `resolveBind` and gets the falsey placeholder, and
BINDS.md signs no such row — so `Validate` refused the document, the case took
the refusal branch, and **it never rendered anything**. That version reported
green with the engine fix reverted. Neither bug was findable by reading; both
took thirty seconds to find by reverting the fix and re-running. **Run the
counterfactual even when the guard is green and the fix is obviously right —
it is the only check that can fail the checker.**

**A test's own probes are code, and untested code.** Two of the defects this
turn were in probe construction, not in the engine: `"grow":true` failed to
decode because `grow` is an int, and a fragment whose key `scene.Node` does not
declare is discarded by `encoding/json` in silence — producing a node identical
to the control, which the sweep would report as an engine silent drop. A false
accusation in the flattering direction: it looks like coverage and it measures
the harness. Where a guard builds documents from fragments, assert that each
fragment actually changed the parsed node.

**A golden that discards what you are asserting about is not covering it.**
The one test that renders the marquee with its gate open is named for its
prefix and suffix and asserts on `f.Plain()` — styling stripped. The *styled*
golden folds to `agent.working=false`, so the marquee draws zero rows there;
`"Thinking"` appears 0 times in `SOBRIA.styled`. Two fixtures whose names
promise the coverage, neither delivering it, and the gap invisible because
each looks like the other's backstop. Check what a golden actually contains,
not what its name says.

## Build

Requires Go 1.25.

```bash
go build -o arxi-tui ./cmd/arxi-tui
go vet ./... && gofmt -l .
go test -count=1 ./...
UPDATE_GOLDEN=1 go test ./internal/...   # regenerate golden fixtures
```
