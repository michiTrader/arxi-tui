# Lessons carried in from arxi-sim and arxi

Do not re-derive what the sibling projects already paid for. Each item names the
where and the why. When in doubt, read the source; this file is the map, not the
territory.

## From arxi-sim (the TUI player — `D:/projects/arxi_cli_sim`)

### Bugs and hard rules it produced

- **A row must never end in bare air.** Width overflow, clipping and wrapping are
  enforced by the `Line`/`Span` machinery (`internal/ui/render.go`, `text.go`) —
  by construction, not politeness. Reuse that machinery; do not re-implement
  cell math anywhere a scene node draws.
- **Anchors must survive a resize, remembered rows must not.** arxi-sim pinned a
  scrolled-to line and found the pinned row became "a wrong answer" after a
  resize (`docs/PLAN.md:967`). Landmark state belongs to the fold with geometry
  re-resolved per frame; a cached row index is a bug with latency.
- **Windows ctrl+key is delivered as a raw byte, not a name.** Windows Terminal
  and conhost changed the ctrl-on-return claim **twice** before it was right
  (`docs/PLAN.md:212`); the key decoder is byte-table driven
  (`internal/term`), and its tests run on Windows CI with a documented
  limitation for the two cases a terminal cannot distinguish. Do not write a
  keymap layer that assumes named ctrl combos.
- **Selection is screen-anchored, not line-anchored — and that is a terminal
  fact, not our bug** (`docs/PLAN.md:1056`): a selection that survives a scroll
  is not fixable inside the alternate screen. Know which complaints are physics
  before scheduling them.
- **Diff color bands must assert relations, not hexes** (`docs/PLAN.md:64`):
  the test window (luma band, hue-tilt checks) lets colors be re-tuned without
  editing the test, and catches a hue-drift a plain equality test passes.
- **A green suite has already been wrong about scroll feel; the last oracle is
  the user's own scroll** (`docs/PLAN.md:603`). Anything haptic (wheel ramping,
  drag acceleration, shine cadence) ships with a named budget and needs a human
  pass before "done".
- **`file:line:` everywhere, or the error is a bug** — every refusal in
  `internal/config` carries the location; the line number is the feature.
- **Default goldens do not move; mutations get their own family**
  (`layout-*.frame`, `composition-*.frame` in `internal/app/testdata`): a
  configured change pinning its own file, the byte-identical default proven by
  `TestTheDefaultCompositionIsToday`. The arxi-tui port of this rule: the eleven
  golden scenes are the family roots.
- **Every declared key must be drawn, every drawn key declared**
  (`TestEveryDeclaredKeyIsDrawn` + the coverage audit in
  `internal/ui/theme_test.go`): the list a user reads is the list the code
  obeys, by construction. arxi-tui inverts the polarity on purpose (open
  vocabulary), but keeps the machinery: a referenced-but-undefined token is an
  error with `file:line`, and an unused token is a warning — so the audit test
  migrates, it does not disappear.
- **Coalesce redraws onto a deadline; the fold never waits on a slow
  renderer** — the 120 ms one-shot coalescing of `view.update` (Phase 3.3 of the
  sim's customization plan, `docs/PLAN-ui-customization.md`) is the seed of the
  animation budget in arxi-tui: a host clock, per-node `fps` requests, and
  drop-and-notify instead of blocking.
- **Process lifecycle on Windows is a solved problem with a copyable
  solution**: the procgroup kill-on-exit pattern in arxi-sim's supervisor
  (`internal/ext/supervisor/process_windows.go`) and its orphan tests. Copy it
  for the plugin host; never re-import it across repos.
- **The consent gate design already argued itself out**: identity =
  name+version+protocol+executable+args+capability-set+digest, exact set
  equality, rejection is session-local, grants persist as identity-bound
  allow-lists, and installation trust is never capability trust
  (`spec/extensions.md`, `internal/ext/identity.go`). arxi-tui's plugin gates
  inherit this whole contract; re-deriving it is how subtle regressions enter.
- **Undeclared capability vs. not-granted must be different errors**
  (`not_declared`/`not_granted` in the ext wire) so a user can tell "the
  manifest never asked" from "you said no".

### What arxi-sim deliberately failed at (the thing this project exists to fix)

Closed vocabularies with named owners — style keys, glyphs, slot names, widget
names — produced an interface that could be *filled in* but never *built on*.
`[layout]` reorders and vetoes, cannot add; extensions render one full-screen
panel, cannot float; `overlay.render` does not exist. Every time arxi-tui is
tempted to freeze a vocabulary for governability's sake, that is the failure
mode creeping back. Governable ≠ closed; the gates moved from the *vocabulary*
to the *actions*.

## From arxi (the agent core — `D:/projects/arxi`)

- **The reducer is pure, effects are described, not run** (ADR-0001/0003): the
  same reason the fold in arxi-tui is host-owned. If you need the clock, the
  answer is an event, not an import.
- **The log is the truth; snapshots are cache** (ADR-0002): a plugin effect or
  agent self-edit that is not in the log did not happen. `run why` after the
  fact is the audit — that is what arxi-tui's "diff of the gate" rides on.
- **Go instead of Rust, with tests covering what the compiler does not give**
  (ADR-0007): exhaustiveness over `Effect`/node-type switches is a test-suite
  obligation, and the arch-test import bans (`internal/arch_test.go`,
  `go list`-based) are how boundaries stay real. arxi-tui copies both idioms.
- **Quiescence is an event with a diagnosis, not a terminal state**
  (ADR-0004): when the UI stops showing progress, the fold must be able to say
  *why* — bind that event surface (`agent.quiet`, remedy fields) so a scene can
  render stuck-ness instead of a silent spinner.
- **The frozen-surface split is deliberate** (`internal/surface`: 50
  capabilities, 34 exposed as agent tools, `trigger run` withheld transitively):
  "what a human may do is not what an agent/plugin may do to itself". arxi-tui's
  plugin capability list inherits that discipline — no transitive grant.
- **Terse invocation is a declared alias, not a second code path** (ADR-0008):
  `/ui add` and the raw config write must run the same mutator once.
- **CAS on `seq`** (ADR-0006): where a future arxi-tui and the arxi core share
  writes, the concurrency resolution is already decided there. Do not invent a
  second one.

## House conventions both repos proved

- Migration status of the language policy: this repo starts English-only; any
  Spanish found is a regression to report and fix in a focused commit.
- Test failure messages name the consequence and the remedy — the pattern in
  arxi-sim's suite (`"two folds of the same log produced different states:
  replay is worthless"`) is the template.
- Commit per file-change; a local-only commit is as fragile as no commit.
  Stronger, after the cost was paid twice more: **work is pushed when it is
  green, not when it is verified.** Committing is not backing up — an
  uncommitted tree and a local-only commit are lost to the same event. The
  second loss happened mid-verification, holding a finished change back
  because the injection matrix was not finished yet; the third was survived,
  because the fix had been pushed the moment the suite went green and only
  the unpushed audit had to be redone. Verification is worth redoing. The
  thing being verified is not.
- **A guard whose documented remedy is a no-op is worse than no guard.** The
  unrendered-field audit told contributors to record a dead field in
  `scene.unrenderedFields`; doing so satisfied the audit and refused nothing,
  because the refusal read one hardcoded key. A missing guard leaves a defect
  undetected, which is recoverable; this kind converts a correct finding into
  a closed ticket and leaves the next reader evidence that the question was
  already settled. When a guard offers a remedy, the remedy is part of the
  guard and has to be tested — take the advice and assert the behaviour
  changes.
- **An audit can be blinded by the very defect it was built to find.** Two
  instances in one sweep: the field audit reports full coverage for a property
  `Node` never declared, because it enumerates struct fields and there is no
  field to enumerate; the unrendered-field map looked fully honoured because
  it had exactly one entry. Ask what a check's subject is, and whether the
  defect could remove the subject rather than fail the check.
- **An escape hatch that lives where behaviour cannot is a blindfold, not an
  exemption.** The universals audit skipped any property listed in a map of
  accepted gaps — and the map was in a `_test.go` file, so recording a gap
  there could not change what the parser did. The property stayed silently
  discarded, the gap was documented to the suite alone, and the audit passed.
  Paired with a source-level question (is this a json tag on the struct?), it
  meant reverting the fix entirely left the whole suite green. Two rules fell
  out: a guard's offered remedy is part of the guard and must be tested by
  taking it; and a guard should ask its question in the layer where the defect
  lives — the audit moved to `internal/engine` and now asks whether a frame
  changes or a refusal is raised, because the validating package cannot be the
  one that certifies the renderer.
- **Repairing an instance is not repairing the class — ask what is doing the
  dropping.** Five turns running, the same defect: a construction the format
  documents, accepted by every layer, drawn by none, reported by nothing. Four
  of those turns fixed it one field at a time, and each fix was correct. The
  fifth measured the mechanism and found the previous four had bought exactly
  the fields they named: `encoding/json` ignores *every* key outside the
  struct's tags, so declaring `on_press` and `scroll` left `focus_glow`,
  `transition`, `reveal`, `enter`, `shine` and `tab` — all named by SCENES.md —
  parsing, validating clean and drawing nothing, indistinguishable from a
  string nobody has ever typed. The cheapest tell that a repair is
  instance-shaped: an invented key behaves exactly like the documented one. If
  a nonsense input is handled identically to the thing just fixed, the fix
  addressed a symptom and the mechanism is still open. The corollary is where
  the real cost sat: the worst case was never the documented gap but the
  **typo** — a misspelled `children` deleted an entire subtree while every
  layer reported success, and a tree under a wrong top-level key validated
  clean with no root at all.
- **Two kinds of unknown deserve two answers, and the difference is whether a
  later version could be right.** An unknown *property* may be a document
  written for a future engine, so PLAN.md's signed rule applies — it is a
  warning, the scene still loads, and the author is told what was skipped
  (the engine already did this for unknown node *types* via
  `[[UNKNOWN NODE TYPE]]`; properties had the opposite behaviour, and the
  silent kind was the one the documentation called universal). A document with
  **no root** is not that: no version of the format renders a scene with no
  tree, so accepting it can only ever hide a mistake, and it is refused. The
  question that separates them is not severity but "could a later engine be
  right about this?"
- **Derive the inventory, or watch it drift.** Four inventories in this
  repository have now been maintained by hand beside the thing they describe,
  and three drifted: the signed bind map from BINDS.md (both directions), the
  unrendered-field map from the refusal it advertised, the universals audit
  from the engine it claimed to check. The vocabulary is therefore computed
  from `Node`'s json tags by reflection, and the injection that proves it
  matters is instructive: replacing the reflection with a hand-written list
  that was *correct on the day it was written* passed the entire suite. Only
  adding a field to `Node` — the drift itself — made it fail. **A restore that
  is merely a worse design is not a passing injection; it has to be the
  defect.** The list was the design, the drift was the defect, and measuring
  the first proved nothing.
- **A finding computed and never delivered is the same silence, one layer
  further out.** The warning had to reach `host.scene.error` and the screen,
  not just exist in `internal/scene`. Twice before, a remedy satisfied its
  guard and changed nothing a user could see (the `unrenderedFields` entry that
  refused nothing; the skip-list in a `_test.go` that could not reach the
  parser); a correct warning nobody ever reads would have been the third, and
  it would have passed its own unit test.
- **A fix no injection can break is not owned yet.** Three turns of injections
  paid for the habit, but the version that matters is the one aimed at the fix
  just written, not at the code it repaired: restore the original defect and
  confirm something fails. When that restore left the suite green, the fix was
  correct and unheld — a distinction invisible from the test output alone.
  Also: a restore the *compiler* rejects is not a passing injection. It proves
  the type checker works and says nothing about the guard, so the faithful
  analogue has to be built (here: keep the field, drop the json tag) before an
  exemption may be called tested.
- **A restore that fails more than the original defect is measuring the
  injection.** Reintroducing half of a two-part bug produced an incoherent
  hybrid failing twenty-odd tests across four packages — noise that looks like
  a large blast radius. A faithful restore of the original pair failed exactly
  one test. Decompose the welds and restore each alone; and where two guards
  could mask each other, verify each separately rather than assuming depth.
- **A different vocabulary is not the absence of one.** The fifth repair
  closed the node object and stopped, with a written reason: walking every
  object in the source would report `style`'s token names and `border`'s keys
  as unknown node properties, a wall of false alarms, and a guard that cries
  wolf gets deleted. The danger was real and the conclusion did not follow —
  the sub-objects each have their *own* vocabulary, and treating "different"
  as "none" left them swallowing keys exactly as `Node` had. Measured with the
  suite green: `{"border":{"shpae":"double"}}` drew the default border,
  `{"root":…,"roott":…}` was accepted in silence, and a misspelled style key
  was worse in kind than any silent drop recorded here — it **defeats the
  token validator**, because `ValidateTokens` can only check a token it can
  find, so a scene naming a token absent from the theme passes clean instead
  of failing with an address. When a guard's scope is justified by a risk,
  check whether the risk argues for a narrower scope or for a *more specific
  question* at full scope.
- **Reflecting a copy only relocates the copy.** The remedy for four drifted
  inventories was to derive them by reflection, and the first sub-object
  version reflected over two anonymous structs copied out of the accessors
  that read a border. An injection showed the hole: teaching one accessor an
  extra key left those copies untouched, so a document using it was honoured
  by the renderer and **warned about** by the guard, whole suite green. That
  is the false-alarm direction — the one that gets a guard switched off rather
  than filed as a bug — and it was the guard contradicting the very code it
  claims to describe. Reflection is only worth something when it reflects the
  thing that actually does the reading: one named type (`borderObject`),
  decoded by both accessors and reflected by the vocabulary, makes the
  disagreement unrepresentable instead of merely tested for.
- **Assert the round-trip, not the list.** The guard for the above does not
  name `shape` and `style`; that would have been a fifth hand-maintained
  inventory, correct the day it was written, which is exactly what the
  injection defeated. It asserts instead that every tag the type declares is
  accepted by the vocabulary *and* surfaced by an accessor through a real
  document, and that every vocabulary entry is a declared tag. Both halves
  were verified load-bearing: deleting the accessor half leaves the drift
  injection green.
- **`grep -- FAIL` cannot tell a caught injection from a broken build.** An
  injection was scored twice as "suite green" when the tree did not compile:
  a `git checkout` had reverted an uncommitted fix, and the filter used to
  read the result was blind to compile errors, which print no `--- FAIL`
  line. Two injections measured nothing and were briefly believed. The
  standing rule that a compile failure is not a passing injection is only
  enforceable if the instrument *reports* compile failures — so the harness
  now builds first and says so, and the lesson generalises: **an instrument
  that can only observe one kind of failure will silently report every other
  kind as success.** Commit the fix before injecting, so the restore step
  cannot delete it.
- **A guard that cannot see the case does not cover it.** The shipped-scene
  counter-assertion is the strongest false-alarm guard in the package, and it
  does not protect the border vocabulary at all: all three golden scenes write
  the *string* form (`"border": "single"`), so the object form appears nowhere
  in them. Coverage by a guard requires the guard's corpus to contain the
  construction; a counter-assertion over documents that never exercise a path
  is silent about it, however loud it is elsewhere.
- The table of "features" that once overclaimed in the arxi README is the
  standing warning against aspirational tables: **a document whose rows all say
  "works" is a document nobody can trust.**
- **"Unrepresentable" is a claim with a quantifier; name it, then ask what the
  other axes are.** One defect — a property accepted by the validator and
  ignored by the renderer — has now recurred four times, and each fix was
  correct and each was scoped to the axis that produced it. `when` honoured per
  *container*, then per *node type*, was closed by moving the work into
  `renderNode`, which `withFocusGlow` documents as making "some node types obey
  and others do not" unrepresentable. True — and quantified over node types.
  The third recurrence was a nested *position*: `renderMarquee` reads
  `prefix.Bind/.Text/.Style` out of the struct, so nothing nested is dispatched
  and no widening of a type switch ever arrives there. **A chokepoint is only a
  chokepoint for the traffic that goes through it.** The fourth was found by
  turning the rule on the third fix: gating inside `renderMarquee` is
  quantified over *one owner*, and the sweep guarding it hardcodes
  `"type":"marquee"`, varying the property and the branch with the owner held
  fixed. `prefix` is polymorphic — string or node — and each owner reads
  exactly one shape (`renderInput`/`PrefixText`, `renderMarquee`/`PrefixNode`),
  so four pairings validated clean, were walked in full by the validator, and
  drew nothing. **A sweep is only a sweep over the axes it varies**, and the
  axis a sweep holds fixed is invisible precisely because the sweep looks
  exhaustive.
- **A written inventory is allowed only if something measures it from the
  other side.** Which *shape* an owner decodes is not a Go declaration to
  reflect over — `PrefixText` and `PrefixNode` have the same signature and
  differ only in the JSON branch they read — so `nestedFormReaders` is
  hand-written, the shape this package has watched drift five times. It is
  pinned by an engine-side audit that renders both shapes under every signed
  node type against a control and fails in *both* directions: a pairing the map
  calls silent that in fact draws (the false-alarm direction), and a pairing it
  omits that in fact drops (the defect). Both were verified by counterfactual —
  over-claiming `text` and un-claiming `input` each failed exactly one subtest,
  and reverting the warning failed 42.
- **The fix that enumerates is the next defect's hiding place.** The rule above
  was written from the fourth recurrence and pointed at the fifth on the first
  probe. `nestedFormReaders` enumerated `prefix` and `suffix` — the two
  branches the defect had been found in — which is the same mistake one level
  up from the sweep that enumerated node types. `children` is a nested branch
  by exactly the same argument, and eleven of the fifteen signed owners accept
  one, validate it in full (an unsigned bind inside it is still refused with an
  address), and never draw it. It is the most expensive row in the table: the
  loss is a whole subtree, and it is indistinguishable, from the author's side,
  from the misspelled `children` this package already warns about. The remedy
  is structural rather than another row: the branch became a *key* of the
  inventory (`<branch>.<shape>`), so a nested branch added to `Node` is either
  listed or caught. **When a fix enumerates the instances of a defect, the
  enumeration itself is the next axis** — ask what makes something a member of
  that list, and key the inventory on it.
- **A guard can be correct because of its corpus rather than its check, and
  the two are indistinguishable while it passes.** The audit above found "the
  warning about this drop" with `strings.Contains(w.Msg, branch)`. The generic
  unknown-key message quotes `a misspelled "children" silently drops the whole
  subtree` in its own advice, so any document with an unrelated typo answers
  *yes* to "was the drop reported?". Measured honestly, the false positive was
  **latent**: the audit's probes were otherwise clean, so reverting the matcher
  alone failed nothing. That is not a reprieve — it locates the correctness in
  the inputs, where one added probe or one reworded sentence moves it, and the
  function that would then certify a silent drop as reported is unchanged and
  still green. Two remedies, both needed: match an *identity* (`Warning.Form`,
  set only by the code that raises it) rather than prose written for a human,
  and **put the case that can fool the guard into the guard's own corpus**.
  Only after adding an unrelated misspelling to every probe did the two
  matchers disagree — prose 0 failures against a reverted engine, identity 11 —
  which is the difference between a claim measured and a claim argued.
- **Correct the commit message the counterfactual refutes.** Two claims written
  in one turn were wrong in the flattering direction and both were caught by
  running the experiment they described: a harness trap said to pass silently
  in fact failed 15 subtests loudly, and a matcher said to be fooled was only
  fooled in principle. A commit message is the durable record of why a decision
  is right; an unverified claim in it is worse than none, because the next turn
  reads it as measurement. Re-run, then write the number.
- **An inventory keyed on an axis still enumerates that axis somewhere.** The
  fifth recurrence was closed by making the branch a *key* of
  `nestedFormReaders` (`<branch>.<shape>`), so a nested branch added to `Node`
  is "either listed or caught". That claim is true of the inventory and false
  of the repository: nothing derived the *set* of branches, and five sites
  declared it independently — `validateBinds`, `collectTokenErrors`,
  `collectWarnings`, eval's `walk`, and the inventory itself. Measured with a
  `Footer *Node` read by `renderText` and nothing else — the realistic shape,
  since a branch nobody reads at all is already caught by the unread-field
  audit — the whole suite stayed green while a `box` with a footer validated
  clean, warned nothing and drew nothing. **The sixth recurrence, through the
  axis the fifth fix held fixed.**
- **A branch no walker enters is unvalidated, not merely undrawn.** The same
  probe carrying `bind: "totally.invented"` inside the new branch also
  validated clean and was never refused. Every guarantee this package makes
  about a document — every bind resolves to a signed row in BINDS.md, every
  token exists in the theme, every unknown key is reported with an address —
  holds *only where a walker goes*. That is a containment failure rather than
  a rendering gap, and no drop-warning inventory reaches it: the inventory
  describes what the renderer composes, while the walkers decide what the
  validator can see at all. Hence the audit checks both halves, and both are
  derived — the branches off `Node`'s declaration, the walkers by shape (takes
  a `*scene.Node`, calls itself) rather than by a list, because a list of
  walkers would have been the sixth declaration of the same fact.
- **The distinction that keeps a structural audit from becoming a false
  alarm is the claim a function makes, not the package it lives in.** The
  renderers also take a `*Node` and recurse, and they are *supposed* to be
  selective: `renderText` composing no children is the fact
  `nestedFormReaders` records, not a bug. Holding them to "every walker visits
  every branch" would demand every node type compose every branch — a false
  alarm on working code. The walkers audited here are the ones enforcing a
  document-wide invariant, so a branch they skip is a branch where the
  invariant does not hold.
- **A guard that resolves types can still match on position.** This audit's
  own worst bug was not a name match — selectors were resolved through
  `go/types` throughout — it was counting a branch as walked only when the
  selector appeared *in the recursive call or its range*. Every walker reaches
  `prefix` as `prefix := n.PrefixNode()` and passes the local, so the first run
  accused three walkers of skipping a branch all three visit. Resolving the
  type is not the whole of asking the right question; the dataflow between the
  branch and the call is part of it. Found by reading the source the failure
  pointed at rather than believing the failure — the same thirty seconds the
  counterfactual rule buys, spent in the other direction.
- **Exempt what is already refused, and measure that it is.** `row_template`
  is a nested branch with no inventory row, which this audit flagged on its
  first run. It is in `unrenderedFields`: a document declaring one is refused
  with an address and never reaches a frame, so there is no silent drop to
  warn about and a warning would be the false-alarm direction again. The
  exemption is written from the probe output, not from reading the map — the
  difference between a claim measured and a claim argued.
- **Deriving two axes out of three is still an enumeration.** The sixth fix
  derived the *branches* from `Node` and found the *walkers* by shape rather
  than by a list, and was argued in its own commit message as the form "the
  seventh recurrence cannot walk around". It then searched two hand-written
  directories. Measured: a recursive `*scene.Node` walker added to
  `cmd/arxi-tui`, skipping `suffix`, left the audit green — **the seventh
  appearance, inside the fix written to end the sixth.** The tell is available
  without the injection: a fix that derives some of its inputs and hardcodes
  the rest reads as structural because of the derived part. Count the inputs,
  not the impression.
- **The machinery was already in the package.** `goPackageDirs` — walk the
  module, skip this package — had existed in `unrendered_audit_test.go` since
  the unread-field audit, three files away from the code that hand-listed two
  directories. Before writing an enumeration, grep for the derivation: this
  repository has now twice written a list beside a function that computes it.
- **An exclusion argued by one criterion and implemented by another is a
  comment that is false.** The same audit excluded `internal/engine` by
  package name, under a comment stating in as many words that the distinction
  "is not which package but which claim the function makes". Both halves were
  written in the same sitting and they disagree: a renderer moved out of
  `engine` would have been held to the rule, and a whole-document walker added
  *inside* `engine` would have escaped it. The remedy is to implement the
  criterion the comment names — a walker is exempt when it dispatches on
  `n.Type`, because choosing behaviour per node type is what a renderer does.
  **When a comment names the real criterion and the code tests a proxy for it,
  the comment is the specification and the code is the bug.**
- **Two probes differing in one property are worth more than four differing in
  many.** The exemption above was proved by injecting one function twice into
  the same package: with `switch n.Type` it is exempt, without it the audit
  reports two findings. Same name, same file, same body otherwise. A probe
  that varies one thing answers "is this the property the guard keys on?",
  which is the question an exemption always raises and which a probe varying
  location *and* shape cannot answer.
- **The last enumeration in a derived guard is the form its subject may take.**
  The seventh fix derived the branches from `Node`, found the walkers by shape,
  and discovered the packages from the module — three axes derived, and it read
  as fully structural. What stayed fixed was the *form* recursion may take:
  `callsSelf` asked whether a body contained a call spelled like its own name.
  Measured: a genuine whole-document walker written as a mutually recursive
  pair, skipping `suffix`, left the audit green — neither function calls
  itself, so neither was a walker at all. **The eighth appearance.** The
  progression is now four fixes long and the shape never changes: each one
  derives the axis the last defect used and hardcodes the next one down.
- **Mutual recursion is not an exotic spelling, it is one refactor away.** A
  self-recursive walker becomes a mutually recursive pair the moment someone
  splits a long function in two, which is the most ordinary edit in a
  codebase. A guard that recognises only direct recursion is not covering an
  unusual case badly, it is covering the *normal evolution* of the thing it
  audits not at all. When a guard keys on a code shape, ask what that shape
  becomes under the refactors people actually perform.
- **A property of a cycle must be judged on the cycle.** Once mutual recursion
  counts, "does this walker reach every branch?" stops being a question about
  a function: `probeVisitKids` descends the branches and `probeVisit` does the
  visiting, and each half alone reaches only some of them. Judging the halves
  separately fails a walker that is complete — the false-alarm direction,
  measured and confirmed absent only after the branches of a whole recursive
  cycle were unioned and reported under one name. The same applies to the
  renderer exemption: one half dispatching on `n.Type` makes the pair a
  renderer, and exempting only that half would hold the other to a rule it
  was never making a claim about.
- **Reachability subsumes the check it replaces, which is why it is the right
  shape.** Direct recursion is the length-one cycle. A fix that *adds* a
  mutual-recursion case beside the self-call case would have been a second
  enumeration — two forms listed instead of one — and the three-function cycle
  in the counterfactuals would have been the ninth recurrence. Prefer the
  generalisation that makes the old case an instance to the one that makes it
  a sibling.
- **A derived guard has one enumeration left, and it is a type spelling.** The
  eighth fix derived the branches, the walkers, the packages and the recursion
  form — four axes — and decided whether a field carries a node by comparing
  source text: `typ == "*Node" || typ == "[]*Node"`. Measured with the whole
  suite green: `Slots map[string]*Node`, read by `renderText` only, produced
  **both** halves of the defect at once — a silent drop and an unsigned bind
  never refused. **The ninth appearance.** `[][]*Node` for a grid and a named
  slice type were each caught only after the fix, and each would otherwise
  have been its own recurrence.
- **The progression is now five turns long and has never changed shape.** Each
  fix derived the axis the last defect used and hardcoded the next one in:
  branch set -> package set -> recursion form -> type shape. Written out, the
  pattern is obvious and it was invisible in every individual turn, because
  the derived parts are what a reader sees. **When a guard derives most of its
  inputs, the remaining literal is not an oversight, it is the next defect** —
  and it is findable by listing what the guard hardcodes rather than by
  waiting for an injection to find it.
- **A composite type is a spelling problem, not a shape problem.** `*Node`,
  `[]*Node`, `map[string]*Node`, `[][]*Node`, a named slice type and an alias
  are six spellings of "this field can hold a node", and only one question
  distinguishes them from `map[string][]string`: walk the type down to what it
  is built from and ask what is at the bottom. `reflect` answers it in six
  lines; the AST cannot answer it at all, because a named type and its
  definition are different source text. **When a guard classifies a type,
  reflection is not an implementation detail — it is the only thing that
  resolves the alias.**
- **A walk over a recursive type needs a `seen` set for correctness, not
  safety.** `typeContainsNode` walks pointers, slices, arrays and map keys and
  values, and `Node` reaches `Node` through `Children`, so the first draft did
  not terminate. The guard against revisiting is load-bearing rather than
  defensive decoration — worth saying out loud, because a reviewer removing it
  as noise would hang the suite rather than fail it.
- **The rule that replaced the injection worked, and it also cleared a
  suspect.** The ninth recurrence produced "list what a derived guard still
  hardcodes, and the next defect is on the list". Applied, the list had two
  entries and they resolved differently: `"json.RawMessage"`, compared as
  source text, let a raw branch declared through an alias escape both halves
  of the audit — the tenth appearance, and the first found by reading rather
  than by guessing where to inject. `"Type"` was a **negative finding**:
  renaming `Node.Type` breaks the build at every use, so the compiler pins
  it. A list of suspects is only worth keeping if entries can be cleared off
  it, and clearing one costs a single injection that fails to compile.
- **An enumeration can hide in the *shape* a guard looks for, not just in its
  values.** With both literals resolved, the exemption still matched only a
  switch tag and an `if` condition — the two places the existing renderers
  happen to put the test. A renderer dispatching through a tagless `switch {
  case n.Type == "text": }` — ordinary Go, and what a dispatch becomes the
  moment one arm needs a compound condition — was reported as a walker
  skipping two branches. **The eleventh appearance, in the false-alarm
  direction**, which is the one that gets a guard switched off rather than
  merely weakened.
- **Two failures bracket a predicate; one only moves it.** The first widening
  asked whether the body *reads* `n.Type`, and the floor failed the run
  immediately: all four real walkers read the type to name it in diagnostics
  (`node type %q declares …`), so the audit dropped to one walker out of
  four. Too narrow slanders a renderer; too broad deletes the audit. The
  line between them — **branching on the type, not reading it** — is visible
  only from both sides, and the reasoning that produced the over-broad
  version ("this direction is strictly more conservative") was confident and
  wrong. When widening a predicate, run the opposite counterfactual in the
  same breath; the argument for the widening is exactly what the argument
  for the original narrowing looked like.
- **The recurrences have changed direction, and that is the signal.** Nine of
  the first eleven were silent drops — the guard missing a defect. The last
  two were both **false alarms**: a renderer dispatching through a tagless
  switch, and a walker reaching a branch through an accessor, each accused of
  a fault it does not have. That is what a maturing guard looks like. The
  remaining ways to be wrong are increasingly "correct code the guard does
  not recognise" rather than "broken code it fails to see", and the cost
  flips with the direction: a missed defect weakens the audit, a false alarm
  gets the whole audit deleted.
- **Prefer the false-alarm probe on a shape the codebase already contains.**
  The accessor case was not hypothetical: `prefix` is *already* reached
  through `PrefixNode()`, and the audit only survived because `Suffix` and
  `Children` happen to be selected directly. The probe that found it —
  rewrite one walker to use an accessor — is the cheapest possible test, and
  the shape was visible by reading the four walkers side by side. **When
  checking a guard for false alarms, do not invent an unusual input; take a
  form the codebase uses in one place and apply it in another.**
- **A resolution that widens acceptance needs the "mentioned but not done"
  probe.** Teaching the audit that `n.SuffixNode()` means the `Suffix` branch
  removes a false alarm and, written carelessly, would also let an accessor
  *launder* a skipped branch: mention it, never walk it, pass. Both halves
  had to be measured, and the condition that separates them — every return is
  that field or nil — is exactly the line that would have been left out. An
  accessor that *decodes* (`PrefixNode`, which returns nil for a string
  prefix and a parsed node otherwise) hands back no field and must not be an
  alias for one.
- **A derivation can be right about every field it names and still answer the
  wrong question.** The previous turn collapsed two hand-written lists — the
  fields the shallow clone clears, and the ones it restores — into one
  derivation over Node's type, and recorded that the available bug class
  shrank from "the two lists disagree" to "the one list is wrong". It also
  measured *widening* that derivation and found it changed nothing. Both are
  true, and both pointed away from the defect that was actually live:

      {"root":{"type":"text","text":"x","on_press":""}}   -> validated clean
      {"root":{"type":"text","text":"x","scroll":null}}   -> refused

  Both keys are in `unrenderedFields`; both were written by the author. The
  list named the right fields. What was wrong was the *question*:
  `declaredUnrenderedFields` reconstructs its answer by re-serialising the
  node, and `omitempty` drops a key written with its type's zero value — so it
  answers "which fields does this node hold a value for", while the validator
  needs "which keys did the author write". They coincide for every non-zero
  value, which is every fixture anyone writes by hand, including all 25 in the
  380-line audit written the turn before to check exactly this remedy. The
  gap showed only between two keys sitting side by side in the same map,
  behaving differently because `json.RawMessage` keeps `null` and a string
  does not. **When a derivation replaces a written list, the next thing to
  check is not whether it names the right things but whether the property it
  derives from is the property the caller needs** — and the cheapest probe is
  the value an author writes while *removing* a property, not while adding it.
  The fix is a union with the parser's `declaredKeys`, not a replacement: a
  hand-built Document has no source text, and reading only the parser's record
  switches the guard off for every caller that does not come from a file —
  which is the Phase 2 patch path, not a hypothetical.
- **Widening a derivation is caught; narrowing it is invisible. Measure both,
  in the same turn.** The widening counterfactual ("accept strings and bools")
  was run and correctly reported as inert. The opposite was not run, and it is
  the dangerous one:

      case reflect.Pointer, reflect.Slice, reflect.Array:  ->  case reflect.Pointer:
        `children` is no longer cleared — the exact drift just fixed
        a cyclic subtree now makes json.Marshal fail, so the function
          returns nil and refuses *nothing* on that node — fail-open
        the entire suite stays green, the new 380-line audit included

  The asymmetry has a reason worth keeping: with clear and restore reading one
  list, a field wrongly *included* is blanked and restored by the same list, so
  the widening cancels out — while a field wrongly *excluded* is never cleared,
  so nothing can report it missing. A guard built on "the two halves agree"
  only sees errors that make them disagree. The second walk here was a comment
  claiming the two derivations "only agree permanently when they ask the same
  question", with nothing enforcing it; it is now a test that fails on either
  edit. **A derived guard's blind spot is the direction in which its two halves
  stay consistent, and that direction is never the one the last fix was about.**
- **A `return nil` on an error path is a fail-open switch, and "unreachable
  today" is a property of other code.** The marshal error in
  `declaredUnrenderedFields` is genuinely unreachable — but only because the
  clearing removes the cycles that would trigger it. That makes its deadness a
  downstream consequence of the derivation above, not a fact about the
  function, and the narrowing counterfactual brought it alive: nil means no
  field is refused at all, so the guard does not weaken, it switches off, and
  the caller cannot tell. Pinned by testing the property that keeps it dead —
  every Node-bearing field is cleared before the marshal — rather than the dead
  line itself.
- **A probe named for a property nobody checks it has is the audit's own blind
  spot.** The zero-value audit added last turn keyed its cases off
  `unrenderedFields`, so a new entry cannot silently skip it — but the *json
  spelling* of each zero value stayed hand-written, and nothing asserted the
  spelling was a zero value. Measured, on `empties["on_press"]` changed from
  `""` to `"cmd:/help"`, one copy-paste's worth of edit:

      the probe parses, the field is set, the validator refuses it
      the subtest passes — through `declaredUnrenderedFields`, the value half
      the union's `declaredKeys` half is never consulted for that key
      whole suite green, this file included

  The audit goes on reporting "on_press is refused when written empty" while
  measuring the non-empty case. That is strictly worse than the hole it
  replaced: the passing subtest is what tells the next reader the direction is
  covered, so the union could be deleted and three guards would still be
  green. **Deriving the *set* of cases from the map does not make the cases
  honest — a hand-written case body is still a hand-written list, and the
  question to ask it is whether it has the property its name promises.** Fixed
  by asking the same marshaller `declaredUnrenderedFields` asks whether each
  spelling survives a round trip, rather than by writing better spellings.
- **The exemption inside a guard is where the guard gets switched off, and
  widening an exemption never fails a green test.** The zero-value check must
  let `scroll: null` pass, because for a `json.RawMessage` `null` is four real
  bytes and genuinely is how an author empties one. That branch is also the
  only way to hold a non-zero spelling and still pass. Replacing it with
  `true` and drifting a spelling in the same edit left the suite green,
  because the only test that could object was the one being mutated — the
  decision was reachable solely through a `t.Errorf`, so nothing could call
  it. Split into a plain function returning `(bool, error)` and pinned from
  both sides: a non-zero string must be rejected, `null` on a raw field must
  be accepted. **A decision that only exists inside an assertion cannot be
  measured; give it a name and a return value before trusting its
  counterfactual.**
