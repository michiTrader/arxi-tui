# Design: agent flow and automations in the TUI

Status: proposal. Nothing in this document is implemented yet; each section ends with
the slices that would build it. It is written from the core's own documents
(`core/docs/design/10-execution.md`, `core/spec/events.md`, `core/spec/jobs.md`) and
from what the TUI folds today (`internal/fold`).

## 1. How the core works, in one page

**One pure function.** `Decide(State, Event, Config) -> (State', []Effect)` decides
everything. The run's state is a fold over an append-only event log. That is why live
runs, `--sim`, `replay` and `why` are the same machinery, and why the TUI can show any
of them by folding the same events it already folds for chat.

**A plain chat is a run with one agent.** `arxi -p "..."` is `run start` with the
actor and budget filled in. Nothing about agents is hidden from the chat path.

**A team is declared, not improvised.** A *blueprint* (see `core/examples/feature-team.yaml`)
lists:

- *members*: name, role (`implementer`, `reviewer`, ...), tools, and flags such as
  `advisory` (gives an opinion, never counts toward an advance rule);
- *stages*: an ordered list, each with `advance_when` (`all`, `any`, `quorum:N`), an
  optional `timeout_ms` and `on_timeout` (`escalate` by default, never a silent skip);
- *watchers*: an agent that reacts to an event pattern (for example
  `run.quiescent`), with self-exclusion and a depth limit so nothing loops.

The model does **not** decide how a task is split. The blueprint decides who may act in
which stage, and the agents decide what to do inside their turn. Splitting "build the
feature" between `backend` and `frontend` happens because both are members of the
`build` stage and each is activated with the same cause (the prompt).

**What a run looks like in events** (all already in the catalogue):

```
run.started   stage.entered(build)
agent.activated(backend)  agent.activated(frontend)         <- in parallel
tool.call / tool.call_completed / llm.response ...           <- per agent
stage.submitted(backend)  stage.submitted(frontend)          <- advance_when: all
stage.advanced(build -> review)   stage.entered(review)
agent.activated(security) ... stage.submitted(security)
run.result
```

**Delegation.** An agent may start a child run (`run start` with `parent_run_id`).
It is an `ask` tool by default, so by default a delegation is a question to the human,
not an automatic spend. The budget belongs to the **tree**: a child's spend counts in
the parent's pool (`TreeSpentUSD`).

**Ways a run stops being busy:** `agent.blocked` (with a mandatory `blocked_ref`:
`approval`, `lock`, `peer`, `budget`, `timer`, `tool`, `workspace`), and
`run.quiescent`, which is the failure nobody specifies: nothing is running, nothing
can run, and the event carries a required diagnosis ("stage review advances with
quorum:3 and only two members can submit").

## 2. What the TUI already folds

`fold.State` already carries: `TeamMembers` (id, state, role, busy, turns, spent),
`StageName/StageIndex/StagePrev/StageSubmissions`, `BlockedOn/BlockedRef`, `Todos`,
`UsageIn/Out`. The SUBAGENTS scene (`testdata/SUBAGENTS.json`) draws members as rows.
What is missing is:

1. the **stage structure** (which stages exist and which one is current) - the fold
   only knows the current one;
2. **who is waiting on whom** (`blocked_on: peer`) as a relationship;
3. **the child-run tree**;
4. a **screen** for any of it in the real app, and a way to start a team run (today the
   TUI starts only the `default` actor).

## 3. Design: the Flow screen (`/flow`)

A full-screen overlay, opened with `/flow` (or a key while a run is live), closed with
Esc, in the same visual language as `/provider` and `/resume`. It has three bands, top
to bottom; each is optional and disappears when it has nothing to say.

```
 Flow · feature-team · build                      17.70 / 20.00 USD   0:42
 ───────────────────────────────────────────────────────────────────────
 ● build ──────────● review ─────○ done          stage 1 of 2 · all 1/2
 ───────────────────────────────────────────────────────────────────────
 backend    implementer  ◐ tool: bash       3 turns  $0.42
 frontend   implementer  ✓ submitted        2 turns  $0.18
 security   reviewer     ○ idle (advisory)  0 turns  $0.00
 ───────────────────────────────────────────────────────────────────────
 r1 feature-team           running   14.20
 └─ r2 researcher          done       2.90
    └─ r3 fetcher          done       0.60
 ───────────────────────────────────────────────────────────────────────
 ⚠ backend waits for approval: bash "go test ./..."     [a] approve
```

- **Stage rail** (top). One node per stage from the frozen blueprint. Filled for passed,
  highlighted for current, hollow for ahead. Beside it the advance rule and progress
  (`all 1/2`, `any 0/1`, `quorum:3 2/3`), and a timeout countdown if the stage has one.
- **Members** (middle). One row per member: name, role, state glyph, last tool, turns,
  spend. Advisory members are marked, because they never block a stage. A member that is
  `submitted` is shown as done for this stage, not as idle: the core makes the same
  distinction (§10.4) and conflating them hides the stuck case.
- **Run tree** (bottom). Only when there are child runs. Spend per node and the tree
  total against the ceiling, since the ceiling belongs to the tree.
- **Attention line** (foot). The single most important blocker: approval, budget, lock,
  peer, or `run.quiescent` with its diagnosis text verbatim. This is the line the user
  needs when "nothing is happening". Where the core offers a remedy (`blocked_ref`
  names it), the key to apply it is shown.

Design rules, taken from what already works in chat:

1. **Only what the log proves.** Every glyph is a function of folded events. No
   animation that implies activity the events do not show; a busy member shows `◐` only
   while `agent.working` is true.
2. **Replay is the same code.** Because the fold is pure, opening `/flow` on a past
   session (via `/resume`) draws the same screen. No second implementation.
3. **Narrow terminals degrade by dropping bands, not by wrapping**: tree first, then
   spend columns, then roles. The attention line is last to go.
4. **Never decorative.** If the run is a plain chat (one agent, no stages), `/flow` says
   so in one line instead of drawing an empty rail.

### Data the fold must add

| addition | source events |
|---|---|
| `Stages []{Name, Advance, TimeoutMs}` | the blueprint snapshot carried by `run.started` (`blueprint_sha` + `runs/<id>/blueprint.snapshot.yaml`), read through a new core method or an event field |
| per-stage progress | `stage.submitted`, `stage.entered` (already folded: `StageSubmissions`) |
| `WaitingOn map[member]string` | `agent.blocked.blocked_on == peer` + `blocked_ref.peer` |
| `Children []RunNode` | `run.started` with `parent_run_id`, `run.result`, `llm.response.cost_usd` |
| `Attention` | derived: latest of `agent.blocked`, `budget.exceeded`, `run.quiescent` not yet cleared |

The first row needs the core: the TUI cannot show a stage rail without knowing the stage
list. The least invasive option is a read-only `run.show` field (it already exists on
the wire) that returns the blueprint snapshot; adding it is a core change and so is
proposed as its own slice.

### Slices

1. **Fold:** `Stages`, per-stage progress, `WaitingOn`, `Attention`. Pure, table-tested
   against recorded logs from `core/testdata`. No UI.
2. **Flow screen, members + attention only** (works today for any run, no core change).
3. **Stage rail** once the core exposes the stage list.
4. **Run tree** once child runs are visible to the fold.
5. **Start a team run:** `/team` lists blueprints from the core's agent store and starts
   one (`run.start` with that actor). Until then `ARXI_ACTOR` selects it.
6. **Approve/unpause from the screen** (`inbox.reply`, `run.unpause --budget`), reusing
   the approval prompt the chat already has.

## 4. How automations work in the core

An automation is a **trigger**: a stored, named record. `arxi trigger create NAME --on
SPEC --then CMD --budget N --budget-period P`.

- `--on` is one source, written with a prefix: `cron:<expr>`, `every:<duration>`,
  `at:<instant>`, `webhook:<path>`, `file:<glob>`, `event:<pattern>`. One source per
  trigger on purpose.
- `--then` is an `arxi` command, validated against the declared surface at creation
  time, and only a command an unattended caller may use. `inbox approve` is refused:
  a trigger must never answer its own approval questions.
- Time is **UTC**, always, and printed with a trailing `Z`: a daily local 02:30 trigger
  either fires twice or never on a DST change, and nobody is watching when it does.
- `--budget` + `--budget-period` (hour, day, week, month) is a spend ceiling for the
  whole trigger per window. Admission reserves money before the job starts; unknown
  spend keeps its reservation until reconciled.
- `--overlap` when a previous run is still going: `skip`, `queue`, `parallel`,
  `cancel-previous`. `--on-missed` after downtime: `run-once` (the latest owed slot) or
  `run-all` (one per slot).
- A trigger has **no delete**, only `pause`: the reason it was stopped is usually what
  you want to read later.
- Execution is a **job**: one job owns one run and a frozen blueprint. Attempts are
  claimed with a lease and a fence; an expired attempt can never become live again;
  non-idempotent work that was started and not confirmed is recorded `unknown` and is
  never retried automatically. The scheduler runs as `arxi scheduler`.

So an automation is "a stored question, a clock, a budget, and a policy for collisions",
and every firing is an ordinary run you can open, replay and `why`.

## 5. Design: the Automations screen (`/auto`)

A list-and-detail screen, same shell as `/provider`.

```
 Automations                                  scheduler: running · next 03:00Z
 ───────────────────────────────────────────────────────────────────────
 ▸ nightly-tests    cron 0 3 * * *     active   ✓ 2h ago     $0.31 / $2.00 day
   inbox-triage     every 15m          active   ✓ 6m ago     $0.08 / $1.00 day
   deploy-watch     webhook /deploy    paused   – (paused by you, 2d ago)
 ───────────────────────────────────────────────────────────────────────
 nightly-tests
   on      cron 0 3 * * *        (03:00 UTC = 22:00 your time)
   then    run start --actor default "run the test suite and report"
   budget  $2.00 per day · overlap skip · on-missed run-once
   last    ✓ 03:00Z  r41  $0.31     ✗ 2d ago r33 failed: provider 429
```

Behaviours that follow from the core's rules rather than from taste:

- **Always show UTC and the local equivalent** side by side.
- **A skipped firing is shown, with its reason** (`overlap: skip`, budget window full).
  Silence is the failure the core is designed against; the screen must not hide it.
- **`unknown` is a first-class status**, not a failure: "started, outcome not confirmed,
  will not retry on its own".
- **No delete.** The screen offers pause/resume and "open last run" (which `/resume`s that
  run into `/flow`).
- **Creating** one is a form in the `/provider` style: the source picker (cron/every/at/
  webhook/file/event) with a human preview ("next: 03:00Z, 22:00 local"), the command
  (`then`) checked against the surface before saving, and a **mandatory** budget field,
  like `--budget` on the CLI. An automation without a ceiling cannot be saved.
- A trigger that would fire an approval-needing tool shows it **before saving**: it will
  block on `inbox` with nobody present, and the screen says so.

### Slices

1. Read-only list + detail (`trigger list/show --json`, `job` status), scheduler status.
2. Pause / resume, "open last run".
3. Create form with preview and surface validation.
4. Skipped/unknown/missed history per trigger.

## 6. Order and dependencies

`/flow` slices 1-2 and `/auto` slice 1 need no core change and are independent: do
slices 1-2 of `/flow` first, since chat and `/resume` already produce the events it
reads. Slices that need the core to expose more (blueprint stage list, child runs) are
filed as core issues before the TUI side starts, so neither side guesses the other.
Plugin-declared tools in chat remain deferred as agreed.
