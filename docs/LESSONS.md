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
- **A restore that fails more than the original defect is measuring the
  injection.** Reintroducing half of a two-part bug produced an incoherent
  hybrid failing twenty-odd tests across four packages — noise that looks like
  a large blast radius. A faithful restore of the original pair failed exactly
  one test. Decompose the welds and restore each alone; and where two guards
  could mask each other, verify each separately rather than assuming depth.
- The table of "features" that once overclaimed in the arxi README is the
  standing warning against aspirational tables: **a document whose rows all say
  "works" is a document nobody can trust.**
