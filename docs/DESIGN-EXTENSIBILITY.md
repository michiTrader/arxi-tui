# DESIGN — extensibility without Go changes

Status: Phase 1 (contract, pure core) in progress; Phase 2 (wiring) next.
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

Later artifact kinds (`tools`, `events`, `panels`) are *reserved keys*: the
parser refuses them today with a message naming the phase that adds them, so a
package written for the future fails loudly instead of being half-loaded.

### Operations

One operation is the unit of "do something". Its `kind` is a closed set:

| kind | meaning | Phase |
|---|---|---|
| `builtin` | a Go function the host registered by name (the critical operations stay in Go) | 1 |
| `http` | one request to a declared host | 1 |
| `exec` | one process, argv only, never a shell | 1 |
| `service` | a long-lived process/stream (voice, watchers) | reserved; refused with a clear message |

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

Added to the scene action grammar (Phase 2) **only with validation**:

- `say:<text>` — a local message, sanitised and bounded; never sent to the model.
- `open:<url>` — `https` only, no credentials in the URL, host in the package's
  `permissions.open`; only from a keypress or a typed command, never from a hook.
- `set:<setting>=<value>` — only the closed settings a hook can watch
  (`effort`, `mode`, `style`) with a value from that setting's own value set.

## Factory commands as packages

Over time the factory commands become packages on this same contract. The
*critical* operations stay Go `builtin`s (anything that touches sessions, the
agent run, credentials, or the filesystem outside the package). The package
only carries the name, description, usage and which builtin to call — so a user
or the AI can replace a factory command's *surface* without being able to
replace its *authority*.

## Persistence, consent, undo

Packages live in `<config>/packages/<id>.json` (user) and `.arxi/packages/` (project),
validated whole on every load and every patch, saved and undone like the other
settings. Anything that can run code or reach the network (`http`, `exec`,
`send`, `open`) is put to the user for consent when added or changed, even in
full-access mode, because the user approved "full access" for the agent's work
and not for what their own keyboard will do later (the rule behaviour already
applies to keys, commands and hooks). Consent is bound to the package digest, so
editing a package re-asks.

## Phases

- **Phase 1** — `internal/ext/v2`: contract, strict validation, templates,
  http/exec runners with their guards, outcome parsing, the `input.pre`
  pipeline. Pure and fully tested; no UI wiring.
- **Phase 2** — wiring: load packages, register commands in the slash menu and
  classifier, run `input.pre` in the loop, `say:/open:/set:` actions, `/ui`
  and `ui_edit` verbs to create / modify / disable / remove a package, consent.
- **Later** — factory commands as packages, tools and events artifacts, panels,
  `service` operations (voice).
