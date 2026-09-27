# Block I — Phase 3 behavioral plugin protocol (signed 2026-09-26)

This document drafts the paper decision Block I needs before I2–I6 can be
implemented: **the subprocess plugin protocol** (task I1) — the NDJSON wire a
behavioral plugin speaks to stream data into its `<plugin-id>.*` bind namespace
and to receive routed input. It is the same on-paper method Blocks D, G and H
used (`docs/DESIGN-BLOCK-D.md`, `docs/DESIGN-BLOCK-G.md`,
`docs/DESIGN-BLOCK-H.md`): it touches no code, it freezes the wire vocabulary
*before* a supervisor or a reader loop depends on it, and it is written to be
signed into the frozen docs by the owner — not merged as fact.

**Status: signed 2026-09-26.** The proposal below was accepted and signed into
the frozen docs — PLAN.md ADR-0007 (the wire protocol), BINDS.md §4.3 (the
`ui.plugin.<id>` liveness bind) and §4.8 (the `ext:` reservation, wire now
signed), and SCENES.md Scene 6 (the behavioral-protocol note). The four open
forks were resolved to their recommended defaults, recorded in NEXT.md's I1
entry the same way Blocks G and H recorded theirs: (1) the host ack is required
before the plugin may publish; (2) a batched multi-field `bind` frame is
allowed; (3) a live mount shows the plain placeholder, not `mock`, before the
first frame; (4) process death triggers bounded restarts with backoff, then
freezes at the last-published values.

As with Blocks D, G and H, signing the
*design* lifts no code guard. The `executable`-bearing manifest refusal
(`internal/ext/manifest.go:257-260`, `checkBehavioral`) and the absence of any
process supervisor are lifted by the *implementation* that replaces them (I2–I6),
each with its own counterfactual test. This file is the argued record behind the
signatures the owner grants.


## What Block H and the sibling projects already settled, and what they did not

Block H (`docs/DESIGN-BLOCK-H.md`, signed into ADR-0006) fixed the **manifest
schema** and the declarative/behavioral split. Every behavioral field this
protocol needs is already parsed and deliberately refused until Block I:

- `Executable string` — the discriminator (`manifest.go:78`); a manifest with an
  executable is refused at load today (`manifest.go:257-260`). Block I lifts that
  refusal *behind the consent gate* (I5); I1 designs only the wire.
- `Args []string`, `Capabilities []string`, `ConsentRequired *bool`,
  `Binds map[string]BindDecl` (`manifest.go:79-82`), with
  `BindDecl{Kind, Mock}` (`manifest.go:105-108`). The `binds` map key is the
  **full** bind path (`"tick.price"`), not the relative field
  (`DESIGN-BLOCK-H.md` schema example; `manifest.go:82`).
- The capability set is closed: `events.subscribe`, `events.emit`,
  `inbox.answer`, `actions.register` (`DESIGN-BLOCK-H.md`). The `kind` set is
  closed at `text` and `series`, widened per node as a signed change
  (`BINDS.md` §4.4).

Four already-signed decisions this protocol only *consumes*, it does not reopen:

1. **ADR-0003 — the open plugin namespace** (`PLAN.md:303-321`, `BINDS.md` §4.4):
   `<plugin-id>.*` is not inventoried; the gate is the boundary, not a name list.
   The **counter-field rule**: an unsatisfied bind renders as a placeholder,
   never a crash (`PLAN.md:319-321`, `BINDS.md`).
2. **ADR-0004 — pull by frame, not push by subscription** (`PLAN.md:323-336`):
   binds are resolved every frame the host renders; the fold is stateless across
   frames; the coalescer is the only timer this engine carries.
3. **ADR-0005 — the host clock as a loop `select` case** (`PLAN.md:338-374`):
   per-node view state held across frames like `ui.hidden`, the ticker on-demand,
   only ever *adding a reason to repaint*, never a new render path.
4. **Invariants 6 and 7** (`PLAN.md:574-585`): the scene never captures the exit;
   plugins propose, never write; every effect is an attributed event; every power
   is granted at the gate, once.

What none of them settled is the **wire between the host and a plugin process** —
the frames, the handshake, how async pushes reach a per-frame pull, and how input
routes back. That is this document.

<!-- APPEND-MARKER-1 -->

## I-A — the channel and its framing

The host↔plugin channel is a **separate NDJSON stream** over the subprocess's
stdin (host→plugin) and stdout (plugin→host), distinct from the host↔core
channel `internal/driver/ndjson.go` speaks. It reuses that file's conventions
wholesale rather than inventing a second dialect:

- one JSON object per line, each direction (`ndjson.go:21-31`);
- the same 1 MiB `maxLineBytes` cap and `line_too_long` refusal
  (`ndjson.go:187-188`);
- the same closed error-code set and `protoError{Code, Message, Fix, Operation}`
  shape (`ndjson.go:62-104`), with the permanent/transient branch (`Permanent()`,
  `ndjson.go:134-143`);
- the context-cancellable line read (`readLine`, `ndjson.go:364-389`) is the
  pattern the plugin reader loop copies.

**stderr is reserved for human logs, never frames** (PROPOSAL). Rationale: it
keeps a plugin's stray `println` from corrupting the frame stream, the mirror of
why the core keeps its run log separate from its response stream.

Why a second channel rather than routing plugin frames through the core: the core
channel *is* the run log (ADR-0002), and a plugin's `tick.price` is not a run
event and must never enter the fold (`BINDS.md` §4.4 — a separate authority). Two
channels, two authorities, matching the namespace split in ADR-0003.

**Protocol token.** The wire version is the manifest's `protocol` field, already
a closed set `{"ext/v1"}` (`manifest.go:42`, `legalProtocols`). The plugin
handshake gates on this string the way `Handshake` gates on the surface version
(`ndjson.go:241-248`). PROPOSAL: reuse `ext/v1` as the single protocol token so
the manifest declaration and the wire handshake cannot drift — the protocol is
"code on both sides, refused at load, not negotiated" (`manifest.go:37-42`).

## I-B — the handshake

Mirroring the core convention that the spawned process sends `hello` first
(`ndjson.go:26-27`):

**Plugin → host, first line:**

```json
{ "type": "hello", "protocol": "ext/v1" }
```

The host validates `protocol` against `legalProtocols` exactly as `Handshake`
validates the surface version (`ndjson.go:241-248`); a mismatch terminates the
handshake and the supervisor kills the child.

**Host → plugin, ack line** — where invariant 7's "granted at the gate, once" is
expressed on the wire:

```json
{ "type": "hello", "protocol": "ext/v1",
  "plugin_id": "tick",
  "granted": ["events.subscribe", "events.emit"] }
```

- `plugin_id` tells the plugin its namespace prefix so it never hard-codes it —
  and, critically, the plugin thereafter names only **relative** fields while the
  host owns the prefix (I-C), so a plugin cannot spoof another plugin's
  namespace. This is the wire analogue of the `<plugin-id>/` id-prefix mechanic
  H-B already uses for mounted node ids (`patch/mount.go:73-82`).
- `granted` is the subset of the manifest's `capabilities` the user actually
  consented to at the I5 gate — the wire face of "power granted at the gate,
  once" (invariant 7). The plugin learns its powers from the host, once, at
  handshake, not by asking per use.

**Fork (recommended default): the host ack is required before the plugin may
publish.** It is the single point where `granted` is communicated, and a plugin
publishing before it knows its grants is a plugin acting on ungranted power.

## I-C — publishing a bind value into `<plugin-id>.*`

**Plugin → host:**

```json
{ "type": "bind", "field": "price",   "value": "$1.23" }
{ "type": "bind", "field": "history", "value": [1, 3, 2, 5, 4] }
```

- `field` is **relative** (`"price"`, never `"tick.price"`). The host composes
  the full path as `plugin_id + "." + field` and checks it against the manifest's
  `binds` map keys (full paths, `manifest.go:82`). The plugin can only ever write
  inside its own namespace by construction — it never utters the prefix — which
  is the wire enforcement of ADR-0003's "the gate is the boundary."
- A `field` whose composed path is **not** a key in the manifest's `binds` map is
  refused with a `bad_params`-class frame back to the plugin (PROPOSAL). This is
  the runtime companion to the load-time H5 rule (`BINDS.md` §4.4): the manifest
  `binds` map is the single source, consulted directly, never copied.
- **`kind` is not repeated on the wire.** It is already declared in the manifest
  `binds` map (`manifest.go:105-108`); repeating it would let the two disagree,
  the same reasoning that made `executable` a single discriminator
  (`DESIGN-BLOCK-H.md` / ADR-0006). The host looks up the declared `kind` and
  validates the frame's `value` shape against it:
  - `kind:"text"` → `value` is a JSON scalar, rendered by a `text` node;
  - `kind:"series"` → `value` is a JSON array of numbers, rendered by a
    `sparkline` node;
  - a shape mismatch is refused, mirroring the load-time wrong-node-type refusal
    (`BINDS.md` §4.4).

**Fork (recommended default): allow a batched multi-field frame**
`{"type":"bind","binds":{"price":"$1.23","history":[…]}}` so a coherent snapshot
lands atomically for one repaint. Rationale: pull-by-frame (ADR-0004) reads all
binds at once, so a coherent multi-field update should arrive as one frame to
avoid a torn frame showing a new price beside an old sparkline.

## I-D — composing async push with pull-by-frame (the core tension)

The subprocess **pushes** asynchronously; binds are **pulled per frame**
(ADR-0004). The bridge is a **host-owned latest-value store**, held across frames
exactly like `ui.hidden` and the animation clock (`BINDS.md`; ADR-0005
`PLAN.md:352-354`):

- one reader goroutine per live plugin drains the plugin's stdout with the
  cancellable `readLine` pattern (`ndjson.go:364-389`); on each `bind` frame it
  writes into `map[bindPath]json.RawMessage` under a mutex, the discipline
  `NDJSONDriver` already uses (`ndjson.go:36`, `250-253`);
- per frame, bind resolution reads a **snapshot** of that store. The fold never
  waits on the plugin (invariant 2); the store is view state, not fold state, so
  geometry and scroll survive a value arriving (ADR-0004 reason 2);
- the store lives **in the host loop, beside `ui.hidden` and the anim clock**,
  not in `fold.State` (rebuilt from the log each frame — it would forget a pushed
  value). PROPOSAL: a `pluginStore` owned by the loop, re-attached to the renderer
  each repaint the way `ui.hidden` is (`NEXT.md` Block F, F3).

**Repaint trigger and backpressure.** A `bind` frame requests a repaint through
the existing 120 ms coalescing budget (`LESSONS.md`), the same way the animation
ticker is "a fourth `select` case" that "adds a reason to repaint, not a new
render path" (ADR-0005 `PLAN.md:341-346`). PROPOSAL: a fifth `select` case fed by
a `chan struct{}` the reader goroutine signals, coalesced. **Backpressure is
last-value-wins**: a bind is a projection, so only the newest value matters for a
frame; the store overwrites, and a plugin flooding faster than 120 ms costs one
map write per frame and one coalesced repaint (drop-and-notify, not block). This
is why `kind:"series"` sends the **whole window** each frame rather than deltas —
last-value-wins requires each frame to be self-contained.

## I-E — routing input to the plugin via `on_press` (I4 / H8)

When the user activates a node whose `on_press` is the closed-vocabulary form
`ext:<plugin-id>:<action>` (`SCENES.md` Q18), the host sends:

**Host → plugin:**

```json
{ "type": "action", "id": "a1", "action": "refresh", "args": { "symbol": "AAPL" } }
```

- `action` is the `<action>` segment of `ext:tick:refresh`. `args` carries
  `{row.field}` values **already resolved by the host** (the `row.*`
  interpolation contract, `BINDS.md` §4.7) — the plugin receives concrete values,
  never scene syntax.
- H8 owns parsing `on_press` and dispatching by prefix (`cmd:`/`focus:`/`answer:`/
  `ext:`); I4 is the `ext:` arm that writes this frame. It requires the plugin to
  have been granted `actions.register` and/or `inbox.answer`, checked against the
  `granted` set from the handshake.
- **The plugin still only proposes.** An `action` never mutates host state
  directly; the plugin responds by publishing new bind values (I-C) or, for
  effects that touch the run, by emitting through the core under its granted
  capability — every effect an attributed event (invariant 7). PROPOSAL: the host
  answers an `action` with an `id`-correlated `ok`/refusal frame (reusing the
  `protoResponse` shape, `ndjson.go:49-55`) so a button can show it was accepted.

## I-F — lifecycle: spawn / supervise / kill (I2)

- **Consent gate first (I5), then spawn.** The order is load-time:
  `Manifest.Validate()` (`manifest.go:200-220`) → I5 consent gate (I-H) → spawn.
  A process is never spawned before consent — "download ≠ trust ≠ grant" (Q15).
- **Port the arxi-sim procgroup supervisor by copy** (`LESSONS.md`; ADR-0001
  `PLAN.md:196-199`): the host owns the child and kills the whole process group on
  exit so no orphan survives. New package `internal/ext/supervisor` (or
  `internal/pluginhost`), never an import of arxi-sim.
- **Then the handshake (I-B), then the reader goroutine (I-D).**
- **The escape hatch is untouched (invariant 6).** Ctrl-C twice / `-scene ""`
  exits and can `SIGKILL`+respawn the plugin without the interface dying
  (ADR-0001). The plugin reader is on its own goroutine/`select` case and cannot
  capture the exit, the same guarantee the animation tick case lives under
  (ADR-0005 `PLAN.md:366-367`). A mounted plugin overlay joins the focus stack
  (Q13) but cannot capture the exit.
- **Unmount** (`/ui plugin remove <id>`) kills the process group, drops the
  reader, and clears the plugin's slice of the latest-value store — the runtime
  companion to H3's node/token unmount (`patch/mount.go` `Unmount`).

## I-G — error and placeholder semantics

- **Before the first frame arrives (live mount):** the bind is unsatisfied, so it
  renders as the standard placeholder and is falsy for `when` (the counter-field
  rule, `BINDS.md`; ADR-0003 `PLAN.md:319-321`). The scene draws, never crashes.
- **`mock` versus the live placeholder (fork, recommended default).**
  `BindDecl.Mock` is the value the **preview** path renders before any frame
  (`manifest.go:104-107`, Block J). Recommend: a *live* mount uses the plain
  placeholder before the first frame (honest: "no value yet"), and `mock` is
  reserved for Block J preview (honest: "here is what it will look like"). This
  keeps the Scene 6 golden (H7) — pinned at the mounted-but-unsatisfied state —
  on the `mock` frame, while a live run shows the placeholder until the socket
  speaks. Recommending against a live `mock` fallback keeps "preview" and
  "waiting" visually distinct.
- **If the process dies:** last-published values remain in the store (stale but
  non-crashing — the fold is untouched, invariant 2). PROPOSAL: the host owns a
  liveness/error status bind reflecting death, and the supervisor applies bounded
  restarts with backoff, then surfaces the error and freezes at last values —
  never crash, never busy-loop respawn (the "quiescence is an event with a
  diagnosis" lesson, `LESSONS.md`). **New vocabulary to sign before I2:** the
  status-bind name belongs in `BINDS.md` §4.3 (view state).
- **`line_too_long` / malformed frame:** refused with the existing closed code
  (`ndjson.go:187-188`), the offending frame dropped; a repeated malformed stream
  is a supervisor kill decision, not a fold event.

## I-H — consent identity for I5

Identity is the tuple already argued out and inherited by copy (`LESSONS.md`;
`DESIGN-BLOCK-H.md`; Q15):

**`name + version + protocol + executable + args + capability-set + digest`**,
mapped to the built manifest fields: `name` (`manifest.go:59`), `version`
(`manifest.go:60`; a bump re-asks), `protocol` (`manifest.go:61`), `executable`
(`manifest.go:78`; relative, in-package, no `..`/symlink), `args`
(`manifest.go:79`; changing an arg re-asks), `capability-set`
(`manifest.go:80`; **exact set equality**), and `digest`.

**`digest` is the one identity component with no manifest field yet (PROPOSAL):**
a content hash over the package bytes (executable + manifest), *computed* by the
loader over the fetched package, not declared — "an executable outside the
package is code the digest never covered" (`DESIGN-BLOCK-H.md`). The digest binds
the grant to the exact bytes.

Gate behaviour (inherited, `LESSONS.md`): rejection is session-local; grants
persist as identity-bound allow-lists ("remember"); and `not_declared` (absent
from the manifest) versus `not_granted` (present, consent refused) are different
refusals, so a user can tell "it never asked" from "you said no."
`consent_required:false` with an `executable` is already a load refusal before
the gate is consulted (`DESIGN-BLOCK-H.md`).

## Frame vocabulary summary (proposed `ext/v1`)

| Direction   | `type`     | Fields                              | Purpose                                                        |
|-------------|------------|-------------------------------------|----------------------------------------------------------------|
| plugin→host | `hello`    | `protocol`                          | announce wire version (sent first)                             |
| host→plugin | `hello`    | `protocol`, `plugin_id`, `granted`  | ack; tell the plugin its namespace and granted powers          |
| plugin→host | `bind`     | `field`, `value` (or `binds` map)   | publish into `<plugin-id>.<field>`; shape checked vs `kind`    |
| host→plugin | `action`   | `id`, `action`, `args`              | route `on_press: ext:<id>:<action>` (I4/H8)                    |
| host→plugin | `ok`/error | `id`, `ok`, `error{code,…}`         | correlate / acknowledge an action                              |
| either      | error      | `code`, `message`, `fix`, `operation` | closed-set refusal (`line_too_long`, `bad_params`, …)        |

## Forks resolved at signing (each to its recommended default above)

1. **Host ack required before publish** — resolved **yes** (I-B): the ack is the
   one point that communicates `granted`.
2. **Batched multi-field `bind` frame** — resolved **allow** (I-C): a coherent
   snapshot should land in one repaint.
3. **`mock` on a live mount before the first frame** — resolved **no** (I-G):
   keep preview and waiting visually distinct.
4. **Restart policy on process death** — resolved **bounded restarts with
   backoff then freeze-at-last-values** (I-G).

## New vocabulary this design signed before I2

- A host-owned plugin liveness/error bind, `ui.plugin.<id>`, in `BINDS.md` §4.3
  (I-G) — signed 2026-09-26.
- The `digest` computation for the I5 identity tuple (I-H) — computed, not a
  manifest field.

Signing this proposal lifts no code guard. I2 (supervisor), I3 (frame ingestion),
I4 (input routing), I5 (consent gate) and I6 (tools door) each land with their own
counterfactual test, exactly as H2–H6 did.

