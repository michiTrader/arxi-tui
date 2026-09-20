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
