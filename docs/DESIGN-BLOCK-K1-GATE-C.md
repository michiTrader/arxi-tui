# Block K1 — Gate C: behavior hooks from a behavioral plugin (signed)

This document drafts the paper decision Block K's **K1** needs: how a mounted
behavioral plugin (Block I) may participate in a running agent's turn — **gate a
tool call, propose a prompt adjustment, run a custom compaction** — under the
identity-ordered stacking and consent contract Q23 signed. It follows the
on-paper method of Blocks D, G, H, I, J, K2 and K4: argue the decision in full
here, then sign the durable seam before any code depends on it.

**Status: SIGNED (owner-accepted). Paper only — no code guard lifted.** The five
forks at the foot of this document are resolved to their recommended defaults
(F1 C-prompt out by rejection; F2 exactly one `compaction` hook; F3 fail-closed
to the hook's own ceiling — `ask` for `tool_gate`, core `Extractive{}` for
`compaction`; F4 reuse the §I-J `CallTool` timeout constant; F5 order by the §I-H
consent-identity tuple). The durable seam frozen by this signature is recorded as
**ADR-0009** in `PLAN.md`. Signing adds nothing to `go.mod` and sends no frame;
it authorizes the one buildable-now, plugin-facing quartet (declaration, consent,
awaited round-trip, composition) under "What is buildable now", each landing later
behind its own counterfactual test. The agent-facing half stays blocked on Block M
and a coordinated `arxi` surface-version bump, named below and never faked. As with
every design beat, each buildable piece lands later with its own counterfactual
test; the core-facing pieces land only once their named dependencies exist.

## The honest headline, before anything is proposed

Gate C is **not** "nearly free, it exists in the core" the way Gate D (K2,
providers) was. K2 found reusable store-owning packages and only had to expose a
protocol surface over them. Gate C is further from reachable than even I-J's tool
door (Gate B), and the honest design says so first:

1. **The agent-facing half is blocked on the same wall I-J named** — Block M (a
   live agent the TUI drives) is unbuilt, and the core surface has no verb for
   this, so adding one is a coordinated `arxi` surface-version bump, not a frame
   this host invents (DESIGN-BLOCK-I §I-J "the hard dependency, named first";
   invariant: the core "refuses unknown parameters rather than ignoring them").
2. **The in-core execution seams exist but are in-process Go interfaces, not an
   external hook.** Tool gating has `TurnToolPolicyResolver.ResolveTurnToolPolicy`
   (`arxi/internal/exec/turn.go:147`), consulted pre-dispatch and 3-valued
   (`allow`/`ask`/`deny`), with `ask` already routed to the durable inbox-decision
   mechanism (`turn.go:302-325`). Custom compaction has `compaction.Generator`
   (`arxi/internal/compaction/compaction.go:126`), carried on
   `contextprep.Request.Generator`. A live external gate would *feed* these — but
   nothing external can reach them today.
3. **Prompt tweaks have no seam at all, and the replay barrier forbids a naive
   one.** `staticMessages`/`inputMessages` (`arxi/internal/contextprep/context.go:555,522`)
   are private pure functions, and their purity is load-bearing: the durable
   `context.prepared` digest is verified on replay (`turn.go:347-399,476`). A
   prompt mutation applied *after* `Prepare` breaks `verifyPreparedContext`. So a
   prompt-tweak hook is not "a seam we have not wired" — it is a seam that cannot
   exist in the live-mutation shape without defeating replay.
4. **The core already decided the governing direction.** `agent.tool.policy` is a
   CLI command deliberately held **off the wire** (`arxi/internal/surface/surface.go:341`;
   pinned by `surface_test.go:359-375`, rationale: "an agent that can widen its
   own tool policy does not belong over a socket"). Gate C must not reopen that:
   it is the **user governing the agent**, never the agent widening itself.

So this document's real job is twofold: **freeze the seam** so the day M and the
core bump arrive the wiring is predetermined (the value K2's paper gave before
its code), and **identify the one buildable-now, plugin-facing half** the way I-J
did — declare-and-consent machinery testable headless against a helper process,
with the core-facing half named as a dependency and never faked.

## What exists in the core today (read from source, not guessed)

The M1a method: the shape is determined by arxi's source, cited by file.

- **The tool-gate seam.** `runDurableTurn` (`arxi/internal/exec/turn.go:262`)
  resolves a policy string for every tool call *before* any dispatch and before
  the durable `exec.work_started` is written: `TurnToolPolicyResolver`
  (`turn.go:147-151`) returns `allow`/`ask`/`deny`, consulted at three sites
  (`turn.go:302,1074,1123`). `deny` runs `runStoppedToolChild`; `ask` suspends
  through `suspendAuthorization` (`turn.go:1154`) and resolves via the **durable
  inbox-decision** path. The sole implementation,
  `provider.Executor.ResolveTurnToolPolicy` (`arxi/internal/provider/executor.go:408`),
  delegates to the pure `tool.Resolve` (`arxi/internal/tool/policy.go:105`):
  deny-if-ungranted → deny-if-unknown → overrides → mutating⇒ask → read⇒allow.
  **This is the exact "gate this tool call" point, and it is already 3-valued and
  pre-dispatch.** The policy source is the static `Executor.ToolPolicy` map
  (`executor.go:83-90`) plus the blueprint grant — in-process, no external input.
- **The compaction seam.** `compaction.Generator` (`compaction.go:126-128`),
  passed via `contextprep.Request.Generator` (`context.go:259`), invoked in
  `compact` (`context.go:434`). The generator is swappable, but the only one is
  `Extractive{}` (`compaction.go:133`) and the overflow mode is hardcoded to
  `"summarize"` (`context.go:425`).
- **No prompt seam.** Context assembly (`contextprep.Prepare`, `context.go:293`)
  is a pure function; its layers are private and fixed, and purity is enforced by
  the `context.prepared` digest barrier verified on replay (`turn.go:347-399`).
- **The serve dispatch pattern.** `cmd/arxi/serve.go` has three tables checked in
  order: `lifecycleHandlerDescriptors` (`serve.go:178`, capability- and
  principal-gated), `streamingHandlers` (`serve.go:282`), and `protoHandlers`
  (`serve.go:367`, pure `func(params)(any,error)`). K2's `provider.add`/`model.*`
  are `protoHandlers` (`serve.go:367-374`, `handleProviderAdd` at `serve.go:796`);
  a behavior verb that is capability- and principal-scoped is the
  `lifecycleHandlerDescriptors` shape, needing a **net-new `hostv1.Capability`**
  (`arxi/host/v1/capabilities.go:6-16` has only job/decision/event caps) and a
  net-new `host/v1` method (`arxi/host/v1/host.go`). Params are validated strictly
  (`validateParams`, `serve.go:676`); unknown params are refused.
- **No behavior anywhere on the wire.** No hook/behavior/tool-gate/prompt/
  compaction verb in any dispatch table, no matching capability, and no plugin or
  external-process influence inside the core at all. The only external boundary is
  the serve socket, which refuses network exposure (`serve.go:1201-1216`). All
  agent behavior is frozen into the blueprint/run config at wiring time and
  verified pure for durable replay — there is no live per-call override seam.

## The triage: three sub-features, three different ceilings

Q23 lists Gate C as one door ("behavior hooks: gating tool calls, prompt
adjustments, custom compaction"), but the source shows they are **not one
difficulty**. Naming the ceiling of each is the first honest act:

- **C-tool-gate — the tractable one.** The core already has the 3-valued
  pre-dispatch seam *and* `ask` already routes to the durable inbox decision the
  TUI already drives (`inbox.approve`/`reject`/`reply`, shipped as M4). A plugin
  that proposes `deny`/`ask` on a tool call is the most reachable sub-feature:
  the core-side change is to let an external verdict *narrow* what
  `TurnToolPolicyResolver` returns, and the user-facing `ask` is a path that
  already exists end to end. This is where Gate C first becomes real.
- **C-compaction — reachable but constrained.** `compaction.Generator` is a clean
  interface, but a stranger's generator must stay **pure for replay**: the chosen
  artifact is what the digest barrier verifies. The resolution (below) is that the
  plugin proposes an artifact once, the host records it as an attributed event,
  and replay reads the record — never re-invokes the plugin.
- **C-prompt — the one with no honest live shape.** There is no post-`Prepare`
  seam and there cannot be one without defeating `verifyPreparedContext`. The only
  honest form of "prompt influence" is **frozen submit-time input** — which
  already exists as `SubmitRequest.Prompt`/`Blueprint`/`Model` (`host/v1/types.go:88`)
  and is not a hook at all. **Recommendation: C-prompt is deferred out of Gate C**
  as a live hook; a plugin wanting to shape the prompt does it by contributing to
  the blueprint at submit time, under the existing config, not by mutating a
  running turn. This is recorded as a fork, not smuggled in.

So Gate C's buildable arc is **C-tool-gate first, C-compaction second, C-prompt
rejected as a live hook** — and all three agent-facing halves wait on M + the
core bump regardless.

## The stance: a plugin proposes a verdict; the core decides; the user governs

Three invariants fix the architecture before any wire detail (PLAN.md
"Invariants", items 2, 6, 7):

- **Plugins propose, never write (inv. 2 & 7).** A hook returns a *proposed*
  verdict (`deny`/`ask`, or a proposed compaction artifact). The core's
  `TurnToolPolicyResolver` remains the sole authority that acts on it; the fold is
  never written by the plugin.
- **A hook may only narrow, never widen (the `agent.tool.policy`-off-wire rule).**
  The core refuses to let the agent loosen its own tool policy over a socket. A
  Gate C hook composes with the core verdict by **most-restrictive-wins**
  (`deny` > `ask` > `allow`): a plugin can tighten a call to `ask`/`deny`, never
  turn a core `deny` into `allow`. This makes "the user governs the agent" the
  only representable direction and keeps the core's existing decision intact.
- **Identity-ordered stacking (Q23).** When several hooks are mounted, they are
  consulted in a deterministic order keyed by consent identity (the §I-H tuple),
  and the composed verdict is the most restrictive across all of them — order
  cannot change the outcome under most-restrictive-wins, but it fixes which
  plugin's `ask` reason is surfaced, so it must be deterministic for replay.
- **The escape hatch is untouched (inv. 6).** A hook invocation runs on a worker
  with a timeout, never on the loop; a hung or crashed hook cannot freeze the
  interface or capture double-Ctrl-C. A timeout **fails safe** (see fork F3).

## Determinism and replay: the hook runs once, its verdict is logged

This is the crux that `agent.tool.policy`-off-wire and the `context.prepared`
digest both point at. A hook consults a stranger's live process, which is
non-deterministic — but the run log must replay deterministically (invariant 2;
the eval corpus, the goldens, and `verifyPreparedContext` all depend on it). The
resolution is the one invariant 7 gives for free, exactly as I-J resolved the
tool result (DESIGN-BLOCK-I §I-J Decision 5):

- The hook is invoked **once**, live, during the turn. Its proposed verdict (and,
  for compaction, its artifact) is written into the run log as an **attributed
  event authored by the host on the plugin's behalf** — "plugin P proposed `deny`
  on call C, with reason R." The plugin never writes the fold; the host attributes
  what the plugin proposed.
- **Replay reads the recorded verdict and never re-invokes the plugin.** This is
  the same contract `context.prepared` already enforces for the prepared context:
  the live computation happens once and is frozen into the durable record, so a
  replay is faithful to what actually happened even though the plugin process is
  long gone. It is why a hung plugin on replay is impossible — replay touches no
  plugin.
- Consequence for the core bump: the net-new fold event vocabulary (a
  hook-proposed verdict, attributed) is part of what the `arxi` surface bump must
  settle, named here as a dependency (below), not invented here.

## The invocation shape: reuse the I-J action/reply round-trip

Gate C does not invent a new plugin-facing frame. The host→plugin `action` frame
and the `id`-correlated `ok`/`error` reply the ext/v1 protocol already carries
(DESIGN-BLOCK-I §I-E, frame vocabulary; graduated to load-bearing by §I-J
Decision 4) are exactly a request/response the host awaits:

1. **core → host** (waits on the surface bump + M): the agent is about to run tool
   call C; the core asks the host for any external verdict, carrying the call name
   and args.
2. **host → plugin**: an `id`-correlated `action` frame naming the hook
   (`{"type":"action","id":"h9","action":"gate.tool","args":{"tool":"bash","input":{…}}}`),
   sent only to a plugin that was **granted the matching capability** (below).
3. **plugin → host**: the `id`-correlated reply — a proposed verdict
   (`{"type":"ok","id":"h9","result":{"verdict":"ask","reason":"…"}}`) or an
   `error`. The host **awaits** on a worker goroutine with a timeout (invariant 6),
   never on the loop — the rule `startInstall` and §I-J `CallHook` already follow.
4. **host → core**: the composed, most-restrictive verdict, which the core feeds
   into `TurnToolPolicyResolver`'s result and acts on (`ask` → the existing
   durable inbox decision; `deny` → `runStoppedToolChild`).

A timeout or an `error` reply does not crash and does not block; it resolves to
the fail-safe default (F3) and is logged as such. The whole round-trip is the
sibling of `Supervisor.CallTool` (§I-J Decision 4 / PR #100): the same
`echoaction`/`replytool` helper-process pattern makes it observable headless.

## Decision 1 — hooks are declared in the manifest, never registered at runtime

A hook is **power the agent's turn is subjected to**, so it binds to the consent
identity exactly as `executable`, `args`, `capabilities`, `binds` and `tools` do
(DESIGN-BLOCK-I §I-H, §I-J Decision 1): it must be a **field in the digested
manifest tree**, inside `plugin.json`, not registered over the wire after mount.
A hook that appeared at runtime would be authority the consent screen never showed
and the digest never covered — the exact failure `PackageDigest` was built to
prevent. A new manifest field, mirroring `tools`:

```json
"hooks": [
  { "kind": "tool_gate", "tools": ["bash", "write"] },
  { "kind": "compaction" }
]
```

`HookDecl{Kind, Tools}` mirrors `ToolDecl`/`BindDecl`: `Kind` is a **closed set**
(`tool_gate`, `compaction` — `prompt` deliberately absent, see triage), widened
only by a signed change; `Tools` is the optional subset of tool names a
`tool_gate` hook is consulted for (absent ⇒ all tools), so a plugin that only
needs to see `bash` is not woken for every `read`. The manifest `hooks` array is
the single source of what a plugin may hook, consulted directly, never copied —
the H5 rule for `binds`, the §I-J rule for `tools`. `ValidateBehavioral`
(`internal/ext/behavioral.go`) refuses: an unknown `kind`; a `tool_gate` naming an
empty tool list element; a `compaction` hook declared more than once (the core
runs one generator); and — the §I-J contradiction precedent — a `hooks` array
present without the matching capability declared.

## Decision 2 — distinct closed capabilities, gated at the I5 gate

The closed capability set today is `events.subscribe`, `events.emit`,
`inbox.answer`, `actions.register`, `tools.register` (`internal/ext/consent.go`,
after §I-J). A behavior hook is a **different** power from any of these — it
observes and can veto the agent's own calls — so it must be legible as that on the
consent screen, not folded into `tools.register` ("adds tools your agent can
call"). Two new capabilities, each a sentence the user weighs separately:

- **`hooks.tool_gate`** — "this plugin may **see and veto** your agent's tool
  calls (it can tighten them to ask-first or deny, never loosen them)." The
  narrow-only promise is stated in the grant itself, because it is the whole
  reason the power is safe to grant.
- **`hooks.compaction`** — "this plugin may **rewrite how your agent's history is
  compacted** when context overflows."

Both extend the closed set, so they are **new vocabulary to sign** (as
`ui.plugin.<id>` and `tools.register` were). `Grant` already refuses any
capability the closed set does not know, so each token is inert until the set is
widened, and the gate is the one place widening happens. Both join the §I-H
identity tuple's capability-set component automatically (exact-set equality), so a
plugin that gains a hook capability it never had **re-asks** — the grant-transfer
safety the whole gate exists for.

## What is buildable now (plugin-facing half), with counterfactuals

Independent of M and the surface bump, and testable headless exactly as I4 and
§I-J's plugin-facing half were — against a helper process, no live agent. The
counterfactuals are constructed and run, not argued (the I-beat method):

1. **`HookDecl` + manifest `hooks`** in `internal/ext`, parsed by the same
   `ext.Parse` and validated by `ValidateBehavioral` (Decision 1). Counterfactual:
   reverting each refusal (unknown kind, empty tool element, duplicate compaction,
   `hooks`-without-capability) accepts a malformed block.
2. **`hooks.tool_gate` / `hooks.compaction`** added to the closed capability set
   (`consent.go`) and thereby to the I5 identity tuple's capability-set component
   (exact-set equality, §I-H). Counterfactual: a plugin gaining a hook grant it
   lacked before must return `DecisionNeedsConsent`, proven by extending the
   existing capability-set-equality test with the new tokens.
3. **`Supervisor.CallHook(kind, args) (verdict, error)`** in
   `internal/ext/supervisor` — the request/response sibling of §I-J's `CallTool`:
   it writes the `id`-correlated `action` frame only under the matching granted
   capability, awaits the reply with a timeout, and maps a timeout and an `error`
   reply to distinct Go errors so the core-facing layer can report them apart and
   apply the fail-safe default. A `replyhook` helper mode answers with a verdict.
   Counterfactuals: dropping the capability gate routes an ungranted hook call
   (fails the ungranted test); a helper that never replies surfaces the timeout,
   not a hang (a deadline-bounded test).
4. **The verdict-composition function** — pure: `(coreVerdict, []pluginVerdict) ->
   verdict` by most-restrictive-wins with deterministic identity order (the stance
   above). This needs no process at all. Counterfactual: a plugin `allow` must
   never override a core `deny` (fails the narrow-only test); reordering the inputs
   must not change the composed verdict, only the surfaced reason.

These four are the honest "buildable now" — the declaration, the consent, the
awaited round-trip, and the composition — the same quartet §I-J shipped for tools.

## What waits on Block M and the surface bump (agent-facing half)

Named, not built, so no code is written against a fabricated core (the mistake
§I-I refused with a fabricated digest and §I-J refused with a fabricated core):

- the **host→core advertisement** that this session has external gate(s)/compaction
  for the agent's turn — a new surface verb, a coordinated `arxi` surface-version
  bump;
- the **core→host gate request** and **host→core verdict** frames on the surface
  channel, feeding `TurnToolPolicyResolver` (`turn.go:147`) and, for compaction,
  `compaction.Generator` (`compaction.go:126`);
- the **net-new `hostv1.Capability`** (`host/v1/capabilities.go`) and the host
  method that carries a verdict, since today's host surface is only
  job/decision/event (§5 of the grounding);
- the **fold event vocabulary** for a hook-proposed, host-attributed verdict
  (determinism section), which the core bump settles alongside M's event schema;
- a **live agent** (Block M) for any of it to have a consumer at all.

C-prompt is **not** on this list: it is rejected as a live hook (triage), so it
has no agent-facing half to wait on — prompt influence stays submit-time blueprint
config, outside Gate C.

## Forks to resolve at signing

The design is settled enough to build the plugin-facing quartet; these are the
points the signature pins, each with a recommendation so signing is a yes/no, not
a fresh argument (the K2 method — resolve forks in line, record where the owner's
call differs):

- **F1 — is C-prompt truly out of Gate C?** Recommendation: **yes, deferred.**
  There is no post-`Prepare` seam and one cannot exist without defeating
  `verifyPreparedContext` (the honest headline, point 3). "Prompt influence" that
  survives replay is submit-time blueprint config, which already exists
  (`SubmitRequest.Prompt`/`Blueprint`/`Model`) and is not a hook. Signing records
  C-prompt as a fork **closed by rejection**, not smuggled in as a weaker hook.
- **F2 — one `compaction` hook, or a stack?** Recommendation: **exactly one.** The
  core runs a single `compaction.Generator` (`compaction.go:133`); a stack of
  compaction hooks has no composition rule that is both deterministic and
  meaningful (unlike tool-gate's most-restrictive-wins). `ValidateBehavioral`
  refuses a second `compaction` hook (Decision 1). If a future need for layered
  compaction appears, it is its own signed change, not an unstated allowance here.
- **F3 — the fail-safe default on timeout or error.** Recommendation:
  **fail-closed, and the closed direction is the hook's own ceiling, not a blanket
  `deny`.** A `tool_gate` hook that times out or errors resolves to the **most
  restrictive verdict that hook could have returned had it answered** — i.e. the
  call is treated as `ask` (routed to the durable inbox the user already drives),
  never silently `allow`ed. A hook exists to *veto*; a vetoing hook that fell over
  must not become a rubber stamp. It is **not** auto-`deny`, because a crashed
  third-party hook should not be able to hard-block the user's own agent with no
  recourse — `ask` puts the human in the loop, which is the safe-and-recoverable
  floor. For `compaction`, a timeout/error falls back to the core's built-in
  `Extractive{}` generator (the status quo), logged as an attributed fallback. Every
  fail-safe resolution is written to the run log as such, so replay sees the
  fallback, not a re-invocation.
- **F4 — the hook invocation timeout value.** Recommendation: **reuse the §I-J
  `CallTool` worker-timeout constant**, not a new one — the round-trip is the same
  shape and a second knob is a second thing to tune wrong. The value is pinned
  where `CallTool`'s is; Gate C does not introduce a parallel deadline.
- **F5 — stacking order determinism.** Under most-restrictive-wins the composed
  *verdict* is order-independent, but the *surfaced reason* (which plugin's `ask`
  the user sees) is not. Recommendation: **order by the §I-H consent-identity
  tuple**, deterministic and already the key everything else sorts by, so replay
  surfaces the same reason every time. Recorded, not left to map-iteration order.

## New vocabulary this design would sign

- Two closed capabilities — **`hooks.tool_gate`**, **`hooks.compaction`** — added
  to the §I-J closed set and thereby to the §I-H identity tuple (Decision 2).
- A manifest field — **`hooks: [HookDecl{Kind, Tools}]`** — with a **closed `Kind`
  set `{tool_gate, compaction}`** (`prompt` deliberately absent, F1), digested with
  the rest of the manifest tree (Decision 1).
- One supervisor method — **`Supervisor.CallHook`** — and its `replyhook` helper
  mode, the request/response sibling of §I-J's `CallTool`.
- A pure **verdict-composition** function (most-restrictive-wins, identity-ordered).

Everything else named here (the surface verb, the host↔core frames, the
`hostv1.Capability`, the fold event vocabulary) is a **dependency to be settled by
the `arxi` surface bump alongside Block M**, not vocabulary this document signs.

## Guards this lifts by signing: none

Signing this document lifts no code guard, adds nothing to `go.mod`, and sends no
frame. It freezes the seam so the day Block M and the surface bump arrive the
wiring is predetermined, and it authorizes the one buildable-now, plugin-facing
quartet above — each landing later behind its own counterfactual test, the
core-facing half never faked. As with every design beat before it, the paper is
signed first; the code that depends on it comes after, and only once its named
dependencies exist.
