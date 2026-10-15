# DESIGN — extensibility without Go changes

Status: Phases 0-6 implemented end to end: the contract and runner in `internal/ext/v2`, and the host
side in `cmd/arxi-tui` (`package_auto.go`). Factory commands stay Go code (decided: not now). See
"Phases 4-6: state" at the end for what was and was not verified.
Branch: `feat/extensibility-phase0`.

## Goal

The in-app AI (and the user) can create, modify, replace, disable and rebuild
commands, rules, tools, events, panels and `/ui` itself, with **no Go change and
no rebuild per customisation**. The contract must not block what comes later
(voice, long-running services, richer panels), and it must not assume that
"HTTP plus scripts" is enough for them: those arrive as new *operation kinds*
or new *artifact kinds*, never as a rewrite of this contract.

## What exists today (and is kept)

| Layer | Where | Role |
|---|---|---|
| Behaviour | `cmd/arxi-tui/behaviour.go` | user commands / keys / hooks as lists of actions (`cmd:`, `focus:`, `ext:`). Closed, validated, undoable, never runs code. |
| ext v1 | `internal/ext` | plugins: manifest + consent + supervised subprocess speaking NDJSON. Tool-gate verdicts are *narrow-only* (`ComposeVerdict`). |
| Scene | `internal/scene` | closed action grammar (`ParseAction`). |
| Phase 0 | `cmd/arxi-tui/slash_dispatch.go` | a `/…` line is never sent to the model by accident. |

ext v1 is heavy (a process per plugin, a wire protocol, a consent screen) and
only the AI-facing *tools* and *gate* hooks reach the agent. It cannot add a
command or intercept an input line. ext v2 is the light, declarative layer that
sits beside it.

## The ext/v2 contract

A **package** is one JSON document (`protocol: "ext/v2"`). It is data: parsing
and validating it runs nothing. It declares:

```
{
  "protocol": "ext/v2", "id": "weather", "name": "Weather", "version": "1",
  "permissions": { "net": ["api.example.com"], "exec": [], "send": false },
  "commands": [ { "name": "weather", "description": "...", "op": {…} } ],
  "rules":    [ { "id": "no-secrets", "stage": "input.pre", "when": {…}, "op": {…} } ]
}
```

`tools`, `events`, `panels` and `services` are further artifact kinds. Any other
top-level key is refused as malformed (the parser rejects unknown fields), so a package
written for the future fails loudly instead of being half-loaded.

### Operations

One operation is the unit of "do something". Its `kind` is a closed set:

| kind | meaning | Phase |
|---|---|---|
| `builtin` | a Go function the host registered by name (the critical operations stay in Go) | 1 |
| `http` | one request to a declared host | 1 |
| `exec` | one process, argv only, never a shell | 1 |
| `service` | a long-lived process/stream (voice, watchers) | not an op kind: declared in `services` with an `exec` op (Phase 6) |

A new capability = a new kind (or a new field on an existing kind) plus a new
permission. Nothing in the package format changes shape. This is the answer to
"voice and complex extensions": they are `service` operations plus panels, and
the permission model already has a slot for them.

### Outcomes and verdicts

An operation yields an **outcome**: `{verdict, say, send, text}`.
Verdicts for the `input.pre` stage, ordered by how much they restrict:

`continue` (line goes on unchanged) < `transform` (the text is replaced) <
`handled` (the line is consumed, nothing is sent) < `block` (consumed, with a
reason shown to the user).

### `input.pre`

Runs on a **plain-text line about to be sent to the model**. It never sees a
`/…` line: commands are resolved by the registry, so a rule cannot shadow,
intercept or re-route a factory command (`/help`, `/ui`, …) and cannot
intercept the user's escape routes.

Rules run in a fixed order (package id, then rule id), each seeing the text the
previous one left. `handled` and `block` end the pipeline. At most 8 rules and
2 s of total budget per line.

### The permission-narrowing rule

**A rule, hook or package can only narrow what the user's own action would have
done; it can never widen it.** Concretely:

1. `transform` output may not start with `/` (a rule cannot mint a command), may
   not be empty, and is sanitised and bounded.
2. `input.pre` never sees slash lines (above).
3. An outcome's `send` text is honoured only if the package declared
   `permissions.send`; otherwise it is refused and reported.
4. An error, timeout or malformed output of a rule is **fail-open to the
   status quo** (`continue`, plus a local warning) unless the rule asks
   `on_error: "block"`. Failing never produces something the user did not ask
   for.
5. A command cannot take the name of a factory command (already enforced for
   user commands) and a package command cannot shadow another package's.
6. Actions a package produces go through the same validation as the behaviour
   layer: never `answer:`, never a chain into another command or rule.

### Security of operations

- **http**: `https` only; the host must be in `permissions.net`; redirects are
  refused (a 3xx is an error), so an allowed host cannot bounce to an
  unlisted one; the connection is refused if the host resolves to a loopback,
  private or link-local address unless the permission is spelled
  `local:<host:port>` (visible on the consent screen); response size, request
  size and time are bounded; header values cannot carry control characters.
- **exec**: `argv` only, no shell; `argv[0]` is either a path *inside the
  package* or a bare name listed in `permissions.exec`; the environment is
  scrubbed (nothing inherited but a minimal `PATH`), so API keys in the host's
  environment never reach a script; stdout+stderr bounded, hard timeout, the
  whole process group is killed on timeout; a substituted value that starts
  with `-` is refused for a whole-element placeholder (option injection) unless
  the operation sets `allow_dash`.
- **templates**: `{args}`, `{1}`…`{9}`, `{input}`; escaping depends on the
  place (URL query-escaped, JSON-escaped in a JSON body, literal in argv).
  Unknown placeholders are a validation error, not an empty string.
- All text coming back from a network or process is untrusted: it is
  sanitised (ANSI/control/bidi stripped, bounded) before it is shown, and it is
  never executed as a command.

### The new actions: `say:`, `open:`, `set:`

Scene actions (`scene.ParseAction`), accepted **only with validation**, usable from
`on_press` buttons, keys, menu rows, user commands and hooks (docs/BINDS.md §4.8):

- `say:<text>` — a local note (`chat.note`), sanitised, at most 400 runes. Never sent
  to the model.
- `open:<url>` — `scene.ParseOpenURL`: `https` only, no credentials, no port, no IP
  address, no bare host name, no space, control or non-ASCII character (an
  international name is written as punycode, a non-ASCII path byte percent-encoded),
  at most 2000 bytes. At run time the host must also be listed in `permissions.open`
  of a package the user has **approved**, and it is refused when the action comes from
  a hook (a hook fires without a keypress). The browser is started by argv, never
  through a shell.
- `set:<setting>=<value>` — only the closed settings a hook can watch (`effort`,
  `mode`, `style`), with a value from that setting's own value set, checked when the
  document is validated. A `set:` made by a hook starts no further hook, so two hooks
  cannot loop.

A button's raw action line cannot be forged from a `cmd:` line: the loop queues it under
a private mark (`rawActionMark`) that `press.go` refuses at the front.

## Factory commands as packages

Over time the factory commands become packages on this same contract. The
*critical* operations stay Go `builtin`s (anything that touches sessions, the
agent run, credentials, or the filesystem outside the package). The package
only carries the name, description, usage and which builtin to call — so a user
or the AI can replace a factory command's *surface* without being able to
replace its *authority*.

## Persistence, consent, undo

### Where packages live

`<config>/packages/<id>.json` (a single document) or `<config>/packages/<id>/package.json`
(a folder that may also hold scripts for `exec` ops), and the same under the project's
`.arxi/packages/`. Every document is parsed strictly and validated whole on every load
and every edit; the id must equal the file or folder name. A package that fails to load
is listed with the reason (`/package`), never half-run. At most 64 packages; links are
not followed. The user's packages win over a project's on a clashing id or command name.

### Consent

Nothing runs until the user approves it. The grant lives in `packages-consent.json`,
keyed by scope and id (a project package also by folder) and **bound to the package
digest**, so any edit, by anyone, makes it inert until approved again. The user
approves with `/package approve <id>`, which shows what the package can do in plain
words and a code (the first 10 hex characters of the digest), then
`/package approve <id> <code>`. The two-step form exists so that approval is a
deliberate act on a version the user just saw. It cannot come from a menu row, a
button, a key, a user command or a hook: those travel the loop as synthesised lines
(`synth`) and are refused. `/package list|show|approve|revoke` are the only verbs.

### What the interface agent may do (`ui_edit` `packages`)

A list of `{put}`, `{remove}`, `{disable}`, `{enable}` entries (at most 8 per call),
reaching only what a person could do with a text editor in the user's own packages
folder:

- only the user's own **single-document** packages; a folder package has files of its
  own and a project package belongs to the repository, so both are refused;
- every document is validated exactly as at load before the user is asked anything,
  and a refusal names the entry and the reason and changes nothing;
- the user is shown a diff plus, for each package written or switched on, what it can
  do, and is **asked even in full access** (`put` and `enable` widen what runs;
  `disable` and `remove` only narrow and are not asked in full access);
- writing never approves: the tool result tells the agent the package does nothing
  until the user types `/package approve`, and the loop tells the user the same;
- `remove` withdraws the grant too, so the same id used later is a new question;
- the loop refuses a change whose packages moved on while the question was open (the
  user edited a file by hand), and refuses it for a scene started with `-scene`;
- plan mode refuses it.

### Undo

`/ui undo` restores what the last change touched. A package change records the
document each touched package held before (`interfaceState.Packages`, a nil for none),
so undo restores those packages and leaves every other one, the ones the user wrote by
hand included, as they are. Undo swaps the two states, so a second undo is a redo.
Restoring a package does not restore its approval: the digest decides, so a restored
version the user approved before is running again and any other is inert.

### How a package reaches the screen

- **Commands.** A runnable package's commands are published to the `/` menu and to the
  line classifier (so `/name` is a command, not an unknown word). They run on a worker,
  never on the render loop. The answer is a `chat.note`.
- **`chat.note`.** The host's answer to a command of the user's. A role of its own: it
  is drawn like the other local lines, never sent to the model, never saved to the
  session, and styled by the `chat.note.*` tokens.
- **Rules.** `input.pre` runs on a worker for a plain-text line headed to the model,
  before the line is submitted, within the budget in `input.pre` above. A line
  that starts with `/` never reaches a rule. The bar is cleared when the rules start; a
  second line sent while they still run is not sent and gets a local warning asking
  the user to press Enter again.
- **Unknown `/word`.** A `/…` line that is no registered command is a local warning and
  is never sent to the model; `/send <text>` is the explicit send (Phase 0).

## Phases

- **Phase 0** — done (#255): a `/…` line is never sent to the model by accident.
- **Phase 1** — done: `internal/ext/v2`: contract, strict validation, templates,
  http/exec runners with their guards, outcome parsing, the `input.pre` pipeline.
- **Phase 2** — done: package store and hash-bound grants; `chat.note`; package commands
  in the menu, classifier and workers; `input.pre` in the loop; `/package`;
  `say:`/`open:`/`set:`; `ui_edit` `packages` verbs with consent and undo.
- **Phase 3** — done: model-callable tools.
- **Phases 4-6** — done, host wired (see the last section).
- **Not now (decided)** — factory commands as packages.

## Phase 3: package tools the model can call

A package may declare `tools`: operations the model calls by itself as
`pkg_<id>_<name>` with `{"args": "<text>"}`. A tool is a command the model types
instead of the user: same op kinds, same permissions, same guards. Differences:

- `description` is required; only `result: say` is allowed, and the text goes back
  to the model only, framed as data and not as instructions.
- Policy follows the mode: exec tools use the run policy, http tools the web policy,
  builtins are always allowed. A tool the mode would deny is not offered at all.
- The consent text (`/package show`) lists the tools and says the model may call
  them by itself. Revoking a package stops its tools at once, even mid-turn (the
  name is re-resolved at every call).
- The core takes at most 8 client tools per turn; the `/ui` tools come first and
  package tools fill the rest, in name order. What does not fit is reported.
- Tools need a project folder and a core with client-tool support; without it the
  turn still answers and says so.

## Phases 4-6: state

Implemented and tested in `internal/ext/v2` (strict validation, limits, consent text,
digest coverage, runner):

- `events`: `on` is `turn.done`, `turn.failed`, `turn.started`, `turn.cancelled`, `shell.ended`, `tool.used`, `file.changed`,
  `approval.asked`, `provider.error`, `context.high` (80% of the window, once until it drops), `model.changed`, `session.resumed`,
  `effort`, `mode` or `style`. `is` narrows the events that carry a value (a setting, the tool of `tool.used` / `approval.asked`,
  the model of `model.changed`); it is checked against the host's values, except a package's own `pkg_` tools. `Runner.RunEvent` returns text only.
- `panels`: one line (title up to 24 characters, up to 120 runes), refreshed on `turn.done`
  or every 30..3600 s, or fed by a service. `Runner.RunPanel`, `PanelLine`.
- `services`: a long-lived `exec` program the user starts with a command. `Runner.StartService`
  keeps the scrubbed environment, kills the whole process group on stop, cleans and cuts
  output lines and rations them (burst 10, 5 per second, drops counted).

Everything that runs by itself is narrowed to showing text: no placeholders (nothing was
typed), no send, no transform, no actions; the runner re-checks at run time.

Host side (`cmd/arxi-tui/package_auto.go`), wired and tested:

- **Events** fire from `settingChanged` (effort, mode, style; also when the user's own hook
  made the change, since an event only shows a note) and from the end of a turn
  (`agent.turn_done`, `agent.failed`). A run that is still going is never started again on
  top of itself, and a failing event warns once, not on every firing.
- **Panels** are joined as `Title: text` into the `host.panels` bind (its own row on the bar,
  blank while the slash menu is open). A panel runs when the package loads, after each turn
  if `refresh` is `turn.done`, and every `every_s` seconds (a 5 s clock exists only while some
  panel has a timer). A failure shows `(failed)` and warns once. An answer that was still on
  its way when the package changed is discarded (the package's hash is part of the job key).
- **Services** are started with `/service start <name>`, listed with `/service` or
  `/service list`, and stopped with `/service stop <name>`. Only a line the user typed starts
  one: a menu row, button, key or command/hook of the behaviour layer is refused (`synth`).
  A service whose package is revoked, switched off or edited is stopped at the next reload and
  the user is told. Everything stops on exit.
- `/package show` says what runs by itself and that only the user starts a service;
  `/package list` counts events, panels and services; the `ui_edit` guide teaches the three
  artifacts and lists them per package.

Verified: unit tests of `packageAuto` (race detector, mutation checks), and through the real
`loop` (`package_auto_loop_test.go`): `/service` list and start, refusal from a behaviour
command, a panel on the bar, the end of a turn refreshing a panel and firing `turn.failed`, a
setting change firing an event. Only on Linux with a real `sh`.

Not verified: Windows runtime (`cmd.exe`, `taskkill`), a real model writing such a package,
a real terminal showing the row.

Decided, not now: factory commands as packages. They stay Go code (the Commands registry in
`internal/fold`); turning them into packages would make them removable by the model, which
would need its own design (what may never be removed, e.g. `/package`, `/ui reset`).
