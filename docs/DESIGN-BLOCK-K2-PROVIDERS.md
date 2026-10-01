# Block K2 — Provider and model management from the TUI

This document drafts the paper decision Block K's **K2** needs: how a user adds
and manages model **providers** from the TUI — the `/provider add <name>
<flags>` command the owner asked for — without the CLI being the real interface.
It follows the on-paper method of Blocks D, G, H, J and K4: argue the decision in
full here, then sign the durable seam (the wire schema and the TUI surface) so
the code is built against a fixed decision rather than one invented under the
pressure of a half-written command.

**Status: signed and implemented.** The durable seam this document freezes —
the four wire verbs, the TUI command grammar, and the `api_key_env`-is-a-name
invariant — shipped exactly as argued below: arxi PR #117 (K2-a/b/c, the
protocol surface and its subprocess tests) and arxi-tui PR #148 (K2-e/f, the
slash commands and the `requireProviderVerbs` hello gate), on top of the driver
methods of arxi-tui PR #147 (K2-d). The two open questions at the foot of this
document are resolved in line, and the one place the implementation diverged
from the draft — the mutating verbs are kept off the agent by *kind*, not by a
capability — is recorded where the draft first assumed otherwise.

**Scope decided by the owner (2026-10-01).** Two repos, **not** merged — the
runtime boundary stays TUI ↔ `arxi serve` over NDJSON, exactly as every other
verb works. This increment builds the **slash command** surface (`/provider`,
`/model`); the visual provider screen over Scene 5 (CONFIG) is **deferred** to a
later increment, and the broader project plan (Scene 10 DASHBOARD, the last
unpinned golden) resumes after providers.

## The thesis constraint, and why wrapping the CLI is wrong

The product is TUI-first: the CLI is optional, the TUI is the complete
experience. A `/provider add` that shells out to `arxi provider add` and parses
its stdout keeps the CLI as the real interface — the opposite of the goal — and
re-reads a text format meant for humans. The honest path is the one Block K2 was
always written as ("Gate D providers: nearly free; exists in the core"): the
provider logic already lives in reusable, store-owning packages in arxi; what is
missing is a **protocol surface** over them, so the TUI drives providers the same
way it drives runs.

## What exists in arxi today (read from source, not guessed)

The M1a method: the schema is determined by arxi's source, cited by file.

- **Pure logic, no disk, no clock** (`arxi/internal/model/provider.go`):
  `model.New(name, baseURL, keyEnv, addedAt) (Provider, error)` validates and
  constructs a provider; `validateKeyEnv` (`provider.go:409`) **refuses a value
  that looks like a secret**, so a key passed where a variable name belongs is
  rejected at construction; `(*Provider).SetEnabled(id, on) (changed, err)`
  (`provider.go:467`) is the `model enable`/`disable` primitive and reports
  whether anything changed. A `Provider` is `{name, protocol, base_url,
  api_key_env, models:[{id, enabled}]}`.
- **The store** (`arxi/internal/modelstore/store.go`): `Open(dir)`, `Add(p)`,
  `Save(p)`, `Load(name)`, `List()`, `Owner(ref)` — one JSON file per provider,
  the credential never among the bytes.
- **The CLI** (`arxi/cmd/arxi/model.go:79` `cmdProviderAdd`) is already a thin
  wrapper: `model.New(...)` → `openProviders().Add(p)`. The protocol handler is
  the same two calls behind a dispatch descriptor.
- **The surface registry** (`arxi/internal/surface/surface.go:148-158`):
  `provider add` is `CLIOnly, Mutates` with params `name` (positional),
  `base-url`, `api-key-env`; `model list` is already
  `CLIOnly | AgentTool | Protocol, Idempotent`; `model enable`/`disable` are
  `CLIOnly, Mutates` with a positional `model`.
- **The gap, measured.** The captured hello's `implemented` list
  (`testdata/serve/session.ndjson:1`) is `blueprint.validate`, `inbox.*`,
  `run.attach/cancel/result/show/start`, `schema` — **no provider or model
  verb**. So `model list`'s `Protocol` kind in the surface is like `run.steer`
  (declared in `types`, no serve executor): the capability is marked, the
  handler is not wired. Nothing provider-related is reachable over the wire yet.

## The decision: a provider/model protocol surface, driven by the TUI

Expose the provider/model operations as serve verbs over the existing
`model` + `modelstore` packages — **zero duplication, one store owner, the
secret-refusal invariant enforced in exactly one place** — and have the TUI
drive them through driver methods and slash commands, the way it drives
`run.start`.

### The wire schema (to be implemented in arxi, read from the source above)

The surface normalises `-` to `_` on the wire (NEXT.md M1a), so flags map to
snake_case params.

- **`provider.add`** — params `name` (string, required), `base_url` (string,
  optional), `api_key_env` (string, optional). Maps to
  `model.New(name, base_url, api_key_env, now)` → `store.Add`. Result: the
  registered provider snapshot `{name, protocol, base_url, api_key_env,
  models:[{id, enabled}]}`. A base-url-less provider not in the known table is
  refused by `model.New` (the CLI's exact refusal); a key-shaped `api_key_env`
  is refused by `validateKeyEnv`. Mutating → `CLIOnly | Protocol`, **not**
  `AgentTool` (see "How the agent is kept out" below).
- **`model.list`** — no params. Maps to `store.List()` flattened to rows
  `{provider, id, enabled}` (optionally `base_url`). Idempotent, no capability
  beyond connect. This is the single read for both "what providers" and "what
  models", since a provider with models answers both; a dedicated
  `provider.list` is added only if a zero-model provider must be shown.
- **`model.enable`** / **`model.disable`** — params `model` (string ref,
  required; `provider/id` or bare `id`). Maps to `store.Owner(ref)` →
  `SetEnabled(id, on)` → `store.Save`. Result `{provider, model, enabled,
  changed}` — `changed:false` is "already in that state", not a failure.
  Mutating → `CLIOnly | Protocol`, **not** `AgentTool`.

### How the agent is kept out (the divergence from the draft)

The draft above assumed the mutating verbs would "need a capability" — a
`ToolPolicy` gate like the agent-tool verbs carry. The implementation did not
use one, and the reason is that a capability gates the wrong door. `ToolPolicy`
(`PolicyAllow`/`PolicyAsk`) governs the **`AgentTool`** door only: it decides
whether an agent, mid-run, may call a verb offered to its tool loop. The threat
it defends against — an agent registering a provider that names a credential env
var, or flipping a model that changes the bill — is closed more simply by never
offering the verb to the agent at all. So the three mutating verbs are
`CLIOnly | Protocol` with **no** `AgentTool` bit and **no** `ToolPolicy`, the
exact shape `run.attach` already uses: reachable by the host (the TUI, on the
user's behalf) over the wire, invisible to the agent loop. The TUI's
`requireProviderVerbs` gate confirms the connected core *implements* them;
nothing in the agent's surface can reach them to widen its own reach. `model.list`
stays `CLIOnly | AgentTool | Protocol, PolicyAllow` — a read an agent may freely
make.

### The `api_key_env` invariant, end to end

The wire carries only the **name** of the environment variable, never a key —
the §20.1 decision arxi already enforces. The single enforcement site stays
`model.New`/`validateKeyEnv` on the arxi side, so a TUI client that mistakenly
sends a key is refused by the core rather than by a second validator the TUI
would have to keep in sync. The TUI's `/provider add` grammar takes
`--api-key-env VAR`; the TUI never stores, logs or transmits a key, and the
success message names the variable, not a secret.

## The TUI surface

### Slash commands (this increment)

Host-intercepted before the patch surface, the `parsePluginInstall` pattern
(`cmd/arxi-tui/plugin_install_cmd.go`): a **pure line-parser** returns
`matched` + extracted args or a located refusal, and an async step calls the
driver verb and reports the result into the chat/notice. A malformed but matched
line is refused *here* with `file:line`, never fallen through to a grammar that
would name the wrong thing.

- `/provider add <name> [--base-url URL] [--api-key-env VAR]`
- `/provider list`  (reads `model.list`, grouped by provider)
- `/model list`
- `/model enable <ref>` / `/model disable <ref>`

### Deferred (next increment, with the owner's "config later")

The visual provider screen over Scene 5 (CONFIG): fold view-state binds
`providers.*` / `models.*` (the `config.*` pattern — host-owned, no core event),
a provider `list` with a `row_template`, and `on_press` add/enable. Deferred by
the owner's decision; the slash commands above are the complete path to
"forget the CLI" on their own, and the scene layers on top without changing the
wire.

## The cross-repo plan (two repos, wire boundary, M-block discipline)

Pure core first, each piece network-free and counterfactual-tested, the wiring
last — the pattern every M increment followed.

**In arxi (its own repo, its own PRs):**

- **K2-a** Add `Protocol` to the `Kind` of `provider.add`, `model.enable`,
  `model.disable` in `surface.go`; keep `model.list` as is.
- **K2-b** Add serve dispatch handlers (the `lifecycleHandlerDescriptors` /
  `protoHandlers` shape) mapping each verb to a host method over
  `modelstore` + `model`. The mutating verbs are reached through `protoHandlers`
  (self-contained request/response) and carry no `ToolPolicy`: the agent is kept
  out by the absent `AgentTool` bit, not by a capability gate.
- **K2-c** Add the four verbs to the hello `implemented` list; pin with arxi's
  subprocess-test style, including the `validateKeyEnv` refusal over the wire.

**In arxi-tui (this repo):**

- **K2-d** Pure driver methods `SubmitProviderAdd`, `SubmitModelList`,
  `SubmitModelEnable`/`Disable` (siblings of `SubmitRunStart` in
  `internal/driver/ndjson.go`): build params, read the result, turn `ok:false`
  into `*Refusal`, fail loud on a success with no provider/model named.
- **K2-e** The slash-command parsers + async orchestration in `cmd/arxi-tui`
  (the `plugin_install_cmd.go` + `install_modal.go` split).
- **K2-f** A hello gate `requireProviderVerbs` (the `requireRunStart` analogue,
  `run_start_gate.go`): a build whose hello does not implement these verbs
  refuses the command with a located message instead of hanging on a reply that
  never comes — the M1b lesson that `types` is not `implemented`.

## Open questions for signature — resolved

1. **`provider.list` vs reuse `model.list`.** Resolved: reuse `model.list`. It
   carries the provider per row, so the TUI's `/provider list` reads the same
   verb and collapses the rows to distinct provider names (the `ProviderView`
   flag on the parsed command). No `provider.list` verb was added; it waits for
   a concrete need to show a zero-model provider, which no current screen has.
2. **Capability name for the mutating verbs.** Resolved by removing the
   question: the verbs take no capability. As "How the agent is kept out" argues
   above, a `ToolPolicy` gates the `AgentTool` door, and these verbs do not open
   that door — they are `CLIOnly | Protocol`, host-reachable and agent-invisible,
   the `run.attach` shape. There was nothing to name.
