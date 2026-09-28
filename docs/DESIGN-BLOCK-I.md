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

## I-I — the behavioral package installer (PROPOSAL, awaiting signature)

This section drafts the one thing standing between I5 and a live behavioral
mount, and the core of I6. It is paper only: signing it lifts no guard, and the
installer lands with its own counterfactuals like every I-beat before it.

### The gap this closes

`supervisor.Mount` (`internal/ext/supervisor/mount.go:62`) already runs the whole
consent-gated lifecycle — decide by identity, prompt, grant, spawn, register,
pump — and it is proven end to end by the helper-process pattern in
`mount_test.go`. It takes a `digest string`, and its own comment is emphatic:
"passing a stale or empty digest silently makes every mount look like a different
plugin." That digest is `ext.PackageDigest` (`internal/ext/identity.go:84`) over a
**package tree on disk** (executable + manifest), and `PackageDigest` too already
exists and is strict — regular files only, symlinks refused, length-framed so no
two files can be confused by concatenation.

What does not exist is everything between "a URL the user typed" and "a package
tree on disk." `httpManifestFetcher` (`cmd/arxi-tui/plugin_fetch.go`) fetches
**manifest bytes only** — one JSON document — because that is all a *declarative*
plugin (Block H) ever needed. A behavioral plugin is at least two files, and its
grant binds to their bytes. Wiring the modal loop against a fabricated digest
would break the exact grant-transfer safety the gate exists for (recorded as the
blocker in `NEXT.md` under I5-consent-loop). So the installer is a real, named
prerequisite, not a nicety: **fetch a package, lay it out, digest the laid-out
tree, then Mount is the small addition NEXT.md already anticipates.**

### Decision 1 — transport is a single gzipped tar (`.tar.gz`), one URL

A behavioral package is more than one file, so the manifest-bytes fetch does not
generalise. Three shapes were weighed:

- **(a) one archive from one URL**, extracted host-side;
- **(b) the manifest lists per-file URLs** the installer fetches individually;
- **(c) an index JSON of `{path, url, sha256}`** the installer walks.

Recommended: **(a), a `.tar.gz`.** It keeps a *single origin*, which matters
because the origin is exactly what the digest and the grant bind to — (b) and (c)
turn one attacker-controlled URL into N, each a separate egress the host must
bound and each able to serve different bytes on the install fetch than on the
preview fetch. (a) also matches the shape the rest of the system already speaks:
the registry entry carries one `manifest_url` (J2), and "one fetch to browse" is
its stated ethos. Extraction is host-side and is where the lay-out invariants are
enforced (Decision 3), so a single opaque blob is *safer* here than a list of
things the host fetches on faith.

Format is **gzip over tar**, both from the Go standard library
(`archive/tar`, `compress/gzip`). This honours the install rule verbatim — "the
user installs arxi, not arxi's dependencies" — and adds **zero** runtime
requirement onto the user's machine: no `tar` binary is shelled out to, no new
module enters `go.mod`. zip and zstd were rejected for exactly that reason (zstd
is a dependency; zip's central-directory model complicates streaming under a size
cap for no gain over tar).

### Decision 2 — the manifest lives at `<root>/plugin.json`, inside the digest

The manifest must be a **digested file in the tree**, not a sidecar fetched
separately, because the grant binds to the bytes the user consented to and the
manifest *is* those terms (its `executable`, `args`, `capabilities` are the
identity). If the manifest were fetched out of band, a later edit to it would not
move the digest and a remembered grant would silently cover new terms — the whole
failure `PackageDigest` was built to prevent, reintroduced one level up.

Fix it at a **well-known path in the bundle root: `plugin.json`.** A fixed name
means the installer needs no out-of-band pointer to find the terms, and because
the file is inside the digested tree, editing it after consent changes the digest
and re-asks. `manifest.Executable` is resolved relative to the same bundle root
(Decision 3). `plugin.json` over `manifest.json` only to avoid colliding with the
many unrelated `manifest.json` conventions; the choice is a fork below, not a
load-bearing claim.

### Decision 3 — extraction is the security boundary, and it enforces the missing in-package rule

`identity.go`'s comment already promises the executable is "relative, in-package,
no `..`/symlink," but **no code enforces that today** — `checkBehavioral`
(`manifest.go:257`) refuses *every* behavioral manifest at load (the H2 guarantee),
so the in-package check has never had a package to run against. The installer is
that missing enforcement, applied at the moment bytes hit the disk:

- **path traversal refused.** Every tar entry's cleaned path must stay inside the
  root — `..`, absolute paths, and drive-relative Windows paths are refused
  (`filepath.IsLocal` is the stdlib predicate for exactly this). This is the
  classic tar/zip-slip write-outside-root, and it is refused at *write* time, not
  digest time, because by digest time the damage is already on disk.
- **only regular files and directories.** Symlinks, hardlinks, devices, FIFOs are
  refused. This deliberately **mirrors `PackageDigest`'s own refusal** so the two
  agree: a bundle that would fail the digest walk is rejected earlier, at
  extraction, with a message that names the offending entry. A symlink is a
  pointer to bytes the digest never read (I-H); accepting one at extraction and
  refusing it at digest would be two rules where there must be one.
- **decompression bounded.** Total extracted bytes and entry count are capped, the
  same spirit as the manifest 1 MiB cap: a gzip bomb is endpoint harm the digest
  cannot prevent because it strikes before the digest runs. The cap is generous
  for a real plugin and small enough that a bomb is cut off.
- **executable resolves in-package.** After extraction, `manifest.Executable` must
  `filepath.IsLocal`-resolve to a **regular file** under the root. An executable
  outside the package is code the digest never covered (DESIGN-BLOCK-H.md); this
  is the check that sentence has been describing all along.

### Decision 4 — laid out by digest under `~/.arxi/plugins/`, atomically

Extract into a **temp dir**, digest the temp tree, then **atomic-rename** it to
`~/.arxi/plugins/<id>/<digest>/`. Three properties fall out, each paid for
elsewhere in this tree:

- **keyed by digest**, so the exact bytes a grant binds to are addressable, and a
  re-install of identical bytes is idempotent (same target path, no-op rename).
- **atomic**, so a crash mid-extract never leaves a half-tree that digests to a
  phantom — the `DiskConsentStore` atomic-write precedent (`consent_disk.go`),
  same reason: a torn write to a security record is worse than no write.
- **digest-before-rename closes the TOCTOU window.** The digest is computed over
  the temp tree and the rename target is *named by that digest*, so what you
  digested is bit-for-bit what you keep and later spawn — nothing can be swapped
  between the hash and the mount. The `~/.arxi` tree is the one the consent store
  and run log already own, so no new root is introduced.

### The flow, reusing every existing validator (no second parser)

1. **fetch** the `.tar.gz` under a size cap and timeout — the `httpManifestFetcher`
   bounds (`plugin_fetch.go`), generalised from a JSON body to an archive body;
   http/https only, as today.
2. **extract** into a temp dir under Decision 3's rules (traversal, entry-kind,
   and size refusals applied per entry as it is written).
3. **read `<root>/plugin.json`** and `ext.Parse`+`Validate` it — the *same*
   validator H2/H6 use, so a malformed behavioral manifest is refused by the parse
   that refuses a malformed declarative one, no new parser. This is also where the
   installer accepts a behavioral manifest that `ext.LoadFile` still refuses: the
   installer is the one gated door that lifts the H2 executable refusal, exactly as
   `Mount` is the one door that spawns. A *declarative* manifest reaching the
   installer is refused here — a package with no executable has nothing behavioral
   to install, and it belongs on the H6 manifest-only path.
4. **`PackageDigest(root)`** over the laid-out tree.
5. **atomic-rename** temp → `~/.arxi/plugins/<id>/<digest>/` (Decision 4).
6. **hand `(manifest, digest, execPath)` to `supervisor.Mount`**, which decides
   consent by identity — now against a *real* digest — prompts through
   `ext.ConsentScene` when unseen, grants, and spawns. Nothing in `Mount` changes;
   it was written waiting for this caller.

### Forks to resolve at signing (each to its recommended default)

1. **Transport format** — single `.tar.gz` (recommended, Decision 1) vs. per-file
   URLs vs. an index-of-files.
2. **Compression** — gzip only, stdlib (recommended) vs. also accept zip vs. zstd
   (rejected: a runtime dependency).
3. **Manifest filename in the bundle** — `plugin.json` (recommended) vs.
   `manifest.json` vs. a name the registry entry declares.
4. **Install-cache retention** — keep-by-digest until an explicit
   `/ui plugin remove`, GC nothing implicitly (recommended: a remembered grant
   references a digest, and deleting its bytes would make the silent remount fail
   at spawn) vs. keep last-N per id vs. GC on unmount.
5. **Pre-extraction integrity** — the registry entry MAY carry the bundle's
   sha256 for a cheap early reject before extraction (recommended as *advisory*
   only) vs. rely solely on `PackageDigest` post-extraction. The authority is and
   stays `PackageDigest` over the laid-out tree, because that is what the consent
   identity binds to; a registry sha256 is a fail-fast convenience, never the
   thing consent is checked against.

### Costs named, per the dependency rule

- **No new module dependency.** `archive/tar`, `compress/gzip`, `io`,
  `path/filepath`, `os` are stdlib. Zero runtime requirement lands on the user's
  machine; the `GOOS=android` artifact is unaffected.
- **New disk footprint** under `~/.arxi/plugins/`, bounded (Decision 4), visible,
  and removable via `/ui plugin remove`.
- **New egress class**: fetching an archive rather than a JSON document — same
  bounded, timed, http/https-only fetch, one wider content type.

### Guards this lifts by signing: none

The installer lands with its own counterfactuals, each built and measured rather
than argued (the I-beat method): a traversal entry (`../evil`) is refused; a
symlink entry is refused; a bundle whose `plugin.json` declares an out-of-package
`executable` is refused; a gzip bomb is cut off at the cap; and the load-bearing
one — **a one-byte edit anywhere in the bundle changes the digest and re-asks** —
proven by installing, granting-and-remembering, mutating one byte, reinstalling,
and observing the gate return `DecisionNeedsConsent` rather than the remembered
grant. That last is the grant-transfer safety the whole gate exists for, and it is
the reason a fabricated digest was never an option.

## I-J — the tool door (Gate B, agent-facing) (PROPOSAL, awaiting signature)

This section drafts the second half of I6 and the last unbuilt piece of Gate B.
The §I-I installer is the *package-delivery* half — how a plugin's bytes reach
the disk under consent. This is the *tool* half — how a mounted plugin teaches
the **agent** a tool it can call. It is paper only: signing it lifts no guard,
and each buildable beat lands with its own counterfactual like every I-beat
before it.

### What already exists, and why it is not this

Three mechanisms look adjacent and none of them is the tool door:

- **`bind` frames (I3).** A plugin publishes a value the *scene* draws. It is a
  projection the fold never sees (invariant 2); nothing consumes it but a render
  walk. The agent never reads it.
- **`action` frames (I4/I-E).** A *user* presses `ext:<id>:<action>` and the host
  routes it to the plugin. `SendAction` writes the frame and returns without
  waiting — "the plugin proposes by publishing new bind values" — because a
  button needs no return value, only a repaint. The agent is not involved and
  no result comes back.
- **the core's own tools.** The agent (the `arxi` core, driven over the
  surface-versioned NDJSON channel, `ndjson.go`) has a *fixed* tool set baked
  into its surface vocabulary (`hostSurfaceVersion = 1`). Nothing in that
  vocabulary lets the host add a tool at connection time.

The tool door is the missing thing all three imply: a plugin declares a tool,
the **agent discovers it, calls it, and gets a result back synchronously enough
to continue its turn.** That is a request/response the agent originates and the
plugin answers — the exact opposite direction from `action` (user originates,
plugin need not answer), and it crosses the two-channel boundary
(`BINDS.md` §4.4: the core channel *is* the run log; the ext channel must never
enter it) that every other I-beat kept the plugin on one side of.

### The hard dependency, named first (Block M and a core surface bump)

The half of this door that faces the **agent** cannot be built today, and the
honest design says so before proposing anything buildable:

- **There is no agent yet.** Block M (core integration — the TUI drives a real
  agent) is on the critical path and unbuilt. The `arxi` core exists and speaks
  the NDJSON surface, but the TUI has only wired `run.prompt`; a tool the agent
  calls has no consumer until M lands.
- **The core surface has no tool-injection verb.** The host advertising a
  plugin's tool to the agent is a message the core must *understand*, and the
  core refuses unknown parameters rather than ignoring them
  (`ndjson.go:203-206`). Adding "here are extra tools for this session" is a
  **surface-version bump coordinated with the `arxi` repo**, not a frame this
  host can invent unilaterally — the same reason `hostSurfaceVersion` gates the
  handshake in the first place. This is cross-repo work owned by the core's
  vocabulary, and it is *named here as a dependency, never signed here.*

So this design splits cleanly, and the split is the whole point of writing it
now: the **plugin-facing** half (declare a tool, validate it against consent,
route an agent call to the plugin and await its reply) is buildable and testable
headless today, exactly as I4 built and tested action routing while "the loop's
registry is empty until I5." The **agent-facing** half (advertise the tool over
the core channel, receive the core's call) waits on M and the surface bump, and
must not be faked against a fabricated core the way a fabricated digest was
correctly refused in §I-I.

### Decision 1 — a tool is declared in the manifest, never registered at runtime

A tool must be a **field in the manifest**, inside the digested tree, for the
same reason the `executable`, `args` and `capabilities` are (§I-H): the grant
binds to the bytes the user consented to, and a tool is *power the agent can
invoke on its own*. A tool that appeared over the wire after mount would be
authority the consent screen never showed and the digest never covered — the
identical failure `PackageDigest` was built to prevent, one level up. So a new
manifest field:

```json
"tools": [
  { "name": "quote",
    "description": "Fetch the latest quote for a ticker symbol.",
    "parameters": { "type": "object",
      "properties": { "symbol": { "type": "string" } },
      "required": ["symbol"] } }
]
```

`ToolDecl{Name, Description, Parameters}` mirrors `BindDecl` (`manifest.go:105`):
`Parameters` is an opaque `json.RawMessage` JSON-Schema object the host forwards
to the agent verbatim and never interprets — the host is a broker, not a
validator of the agent's argument semantics. The manifest `tools` array is the
single source of what a plugin may expose, consulted directly, never copied — the
same rule H5 set for `binds`.

### Decision 2 — the agent-visible name is host-composed `<plugin-id>.<tool>`

The name the agent sees is composed by the host from the plugin id and the
declared tool name, exactly as I3 composes `<plugin-id>.<field>` for binds
(§I-C). A plugin utters only its *relative* tool name and can never shadow a core
tool or another plugin's tool — the wire-security property that made the bind
namespace safe makes the tool namespace safe for free. A bare tool name the
plugin chose would let a malicious package register `read_file` and intercept the
agent's calls to the core's own `read_file`; host composition makes that
unrepresentable rather than merely refused.

### Decision 3 — a distinct capability `tools.register`, gated at the I5 gate

The closed capability set today is `events.subscribe`, `events.emit`,
`inbox.answer`, `actions.register` (`consent.go:26-33`). A tool the agent invokes
autonomously is strictly more power than a user-pressed `action`: the user is not
in the loop at call time, so the grant must be *legible as that*. Recommend a new
capability **`tools.register`** — "this plugin may add tools your agent can call
on its own" — rather than overloading `actions.register`, which the consent
screen presents as "may receive button presses." Two grants, two sentences the
user can weigh separately.

This extends the closed set, so it is **new vocabulary to sign** (like
`ui.plugin.<id>` was signed before I2). `Grant` already refuses any capability
the closed set does not know (`consent.go`, I5), so the token is inert until the
set is widened and the gate is the one place the widening happens.

### Decision 4 — the agent's call is synchronous; the plugin answers with a result

This is the crux and the one place the plugin does **not** merely propose into a
bind. The agent calls a tool to *get an answer it will reason over in the same
turn*; a value dribbling into a scene bind three frames later (the I4 model) is
useless to it. So the invocation is a request/response the host brokers:

1. **core → host** (waits on the surface bump, M): the agent calls
   `<plugin-id>.<tool>` with JSON args.
2. **host → plugin**: the existing `action` frame (§I-E), `id`-correlated —
   `{ "type": "action", "id": "t7", "action": "quote", "args": {…} }`. The host
   already owns this frame and this correlation id (`registry.go`, I4). No new
   plugin-facing frame is invented; the tool door *reuses the action channel and
   adds the wait.*
3. **plugin → host**: the `id`-correlated reply already drafted as the §I-E
   PROPOSAL and listed in the frame vocabulary —
   `{ "type": "ok", "id": "t7", "result": {…} }` or an `error` frame. This is
   where that PROPOSAL graduates from "so a button can show it was accepted" to
   load-bearing: a button ignored the reply, a tool cannot.
4. **host → core**: the result, correlated to the agent's call.

The host **awaits** the plugin reply on a worker goroutine with a timeout, never
on the loop — a hung plugin must never freeze the interface or the panic gesture
(invariant 6), the same rule `startInstall` (§I5-modal) already follows. A
timeout or an `error` reply becomes a **tool-error returned to the agent**, which
decides what to do next; the host neither crashes nor blocks, and last-value
bind state is untouched.

### Decision 5 — the fold stays host-owned; a tool result is an attributed event, not a fold authority

A tool call belongs in the run log — the fold already carries `ToolCalls`
(`fold/fold.go:73`). But the *result* comes from a stranger's process, and
invariant 2 (ADR-0003) is absolute: a plugin value must never enter the fold as
an authority. The resolution is the one "plugins propose, they never write" gives
for free: the tool call and its result are events **authored by the host on the
plugin's behalf**, attributed to the plugin id, entering through the same gate
every plugin effect does (invariant 7). The fold records "the agent called
`<plugin-id>.<tool>`, which returned R, from plugin P" — the plugin proposed R,
the host attributed it, and no forged sequence or authorship is possible. The
agent's reasoning over R is the agent's own; the fold's authority over *what
happened* stays host-owned.

### What is buildable now (plugin-facing half), with counterfactuals

Independent of M and the surface bump, and testable headless exactly as I4 was:

1. **`ToolDecl` + manifest `tools`** in `internal/ext` — parsed by the same
   `ext.Parse` the installer already uses, validated by `ValidateBehavioral`
   (`behavioral.go`): a tool naming a duplicate relative name is refused; a tool
   with an empty name or absent `parameters` is refused; `tools` present without
   the `tools.register` capability declared is a contradiction refusal (the
   `consent_required:false`-with-`executable` precedent, `manifest.go`).
   Counterfactual: reverting each refusal accepts a malformed `tools` block.
2. **`tools.register`** added to the closed capability set (`consent.go`) and to
   the I5 identity tuple's capability-set component automatically (it is exact-set
   equality already, §I-H) — so granting tool power re-asks a plugin that never
   had it. Counterfactual: a plugin gaining a `tools.register` grant it did not
   have before must return `DecisionNeedsConsent`, proven by the existing
   capability-set-equality test extended with the new token.
3. **`Supervisor.CallTool(name, args) (result, error)`** in
   `internal/ext/supervisor` — the request/response sibling of I4's fire-and-
   forget `SendAction`: it writes the `action` frame under the granted
   `tools.register` capability, then **awaits** the `id`-correlated reply with a
   timeout, mapping a timeout and an `error` reply to distinct Go errors so the
   agent-facing layer can report them apart. The `echoaction` helper mode already
   makes the round-trip observable (`send_test.go` precedent); a new
   `replytool` helper answers with a result. Counterfactuals: dropping the
   capability gate routes an ungranted tool call (fails the ungranted test); a
   helper that never replies must surface the timeout error, not hang (fails a
   deadline-bounded test).

### What waits on Block M and the surface bump (agent-facing half)

Named, not built, so no code is written against a fabricated core:

- the host→core advertisement of granted plugin tools at connection time (a new
  surface verb, a coordinated `arxi` surface-version bump);
- the core→host tool-call frame and the host→core result frame on the surface
  channel;
- the fold events for a plugin tool call/result (Decision 5), which need the
  event vocabulary M settles.

### Forks to resolve at signing (each to its recommended default)

1. **Capability** — a new `tools.register` (recommended, Decision 3) vs. reuse
   `actions.register`. Recommend new: the grant sentence must distinguish
   "receives button presses" from "your agent may call this on its own."
2. **Tool declaration site** — manifest `tools` array (recommended, Decision 1)
   vs. runtime registration over the wire (rejected: power the digest never
   covered and the consent screen never showed).
3. **Invocation shape** — synchronous `id`-correlated reply the host awaits with
   a timeout (recommended, Decision 4) vs. async-via-binds like I4 (rejected: the
   agent needs a result inside its turn; a bind is a scene projection, not a
   return value).
4. **Agent-visible name** — host-composed `<plugin-id>.<tool>` (recommended,
   Decision 2) vs. a plugin-chosen bare name (rejected: shadows a core tool).
5. **Result-to-fold attribution** — host authors the call/result events on the
   plugin's behalf, attributed to the plugin id (recommended, Decision 5) vs. the
   plugin writing the fold (rejected outright: invariant 2).

### New vocabulary this design would sign before the buildable half

- The manifest `tools` array and `ToolDecl{Name, Description, Parameters}` in
  `internal/ext` — a declared, digested field like `binds`.
- The `tools.register` capability, extending the closed set in `consent.go` and
  joining the I5 identity tuple's capability-set component.

### Named dependency this design does NOT sign (owned by the `arxi` core)

- The core surface verb that advertises host-supplied tools to the agent, the
  core→host tool-call frame, and the result frame — a coordinated
  surface-version bump in the `arxi` repo, blocked on Block M wiring a real
  agent. This is the core's vocabulary, not this host's, and inventing it here
  would be the fabricated-core mistake §I-I refused to make with a fabricated
  digest.

### Guards this lifts by signing: none

Signing lifts no code guard. The buildable half lands with the counterfactuals
listed above, each constructed and measured rather than argued (the I-beat
method); the agent-facing half lands only once M and the surface bump exist, with
its own counterfactuals then.






