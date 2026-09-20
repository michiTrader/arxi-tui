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
   destroyed and re-cloned from the remote **five times** across recent
   sessions, without warning and mid-task. Four times everything survived,
   because every commit had been pushed and the only loss was a half-applied
   edit. The fifth time two commits — a complete audit and a 44-reference
   rename, both green — existed only on the local disk and were lost entirely;
   they had to be reconstructed from scratch. The work was not lost to bad
   luck, it was lost to the gap between `commit` and `push`. Close that gap
   every time.

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

## Build

Requires Go 1.25.

```bash
go build -o arxi-tui ./cmd/arxi-tui
go vet ./... && gofmt -l .
go test -count=1 ./...
UPDATE_GOLDEN=1 go test ./internal/...   # regenerate golden fixtures
```
