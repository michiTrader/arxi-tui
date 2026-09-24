# Block H — Phase 3 declarative plugin mounting (proposal, awaiting signature)

This document drafts the paper decision Block H needs before H2–H7 can be
implemented: **the plugin manifest schema** (task H1). It is the same
SCENES/BINDS/TOKENS-on-paper method Blocks D and G used
(`docs/DESIGN-BLOCK-D.md`, `docs/DESIGN-BLOCK-G.md`): it touches no code, it
freezes the manifest vocabulary and the mount/validation behaviour *before* a
loader depends on it, and it is written to be signed into the frozen docs by the
owner — not merged as fact.

**Status: proposal, awaiting signature.** As with Blocks D and G, signing the
*design* lifts no code guard. The `<plugin-id>.*` bind refusal
(`internal/scene/validate.go`, `signedBinds` has no plugin entries) and the
absence of any `internal/ext` package are lifted by the *implementation* that
replaces them (H2–H6), each with its own counterfactual test. This file is the
argued record behind the signatures the owner grants.

## What Scene 6 and the sibling projects already settled, and what they did not

Scene 6 (`docs/SCENES.md:263-273`) fixed the **shape of the experience**:
`/ui plugin add https://…/tick` fetches a manifest carrying `executable`,
`capabilities`, `consent_required`, and `mounts`; the mounts declare the
plugin's UI as scene fragments (a `sparkline` + `text` row inside a top-right
overlay); a gated process streams NDJSON frames into the `tick.*` bind
namespace; and three questions were answered — Q13 (a plugin overlay joins the
focus stack), Q14 (a render that is none of our nodes is the Phase-4 wasm ADR),
Q15 (download ≠ trust ≠ grant, the consent gate checks identity with remember).

Three more decisions are already signed and this proposal only *consumes* them,
it does not reopen them:

1. **The open plugin bind namespace** — ADR-0003 (`docs/PLAN.md:303-321`) and
   BINDS.md §4.4 (`:186-199`): `<plugin-id>.*` is not inventoried; the gate is
   the boundary, not a name list; an unsatisfied plugin bind renders as a
   placeholder, never a crash; and *every bind in a plugin's fragment must
   resolve to a host field or a field the plugin declares it will stream — an
   undeclared plugin bind is a `file:line` validation error.*
2. **Token precedence** — TOKENS.md §"Extension by plugins"
   (`docs/TOKENS.md:155-167`): a plugin may define tokens in its manifest;
   merged precedence is **user theme > plugin tokens > factory theme**; a
   fragment referencing an undeclared token is rejected at install time.
3. **The consent contract** — Q15, LESSONS.md `:62-67`: identity is
   `name+version+protocol+executable+args+capability-set+digest`, exact set
   equality, session-local rejection, identity-bound persistent grants, and the
   `not_declared` vs `not_granted` split. This is the *behavioral*-plugin gate
   and it is **Block I's** work (I5); H names the manifest fields the gate will
   read, but the gate itself is not built in Block H.

What none of the above settled — and what this proposal must supply before H2
can load a single plugin — is **the manifest schema itself**: which fields
exist, which are required, what a declarative-only (zero-code) manifest looks
like versus a behavioral one, how `mounts` names *where* a fragment lands in the
host tree, and how the streamed-bind declaration feeds the H5 validator. That is
H1, below.

## The load-bearing split: declarative (Block H) vs behavioral (Block I)

The single most important boundary this schema draws is the one PLAN.md already
names (`docs/PLAN.md:94-98`): **a declarative plugin is data with zero code and
zero risk; a behavioral plugin is an external process.** The manifest schema
must make that difference *structural*, not a footnote — because the whole
safety claim of Block H is that mounting a declarative plugin runs no foreign
code, and a schema that blurs the two lets a manifest smuggle an `executable`
into a path the user believed was pure data.

The rule this proposal adopts: **`executable` is the discriminator.** A manifest
with no `executable` is declarative — it contributes only `mounts` (scene
fragments) and `tokens`, both of which are data the host already knows how to
validate and render, and it streams *nothing*, so its fragments may bind only
host fields (its `<plugin-id>.*` namespace is empty and any use of it is
refused). A manifest *with* an `executable` is behavioral: it additionally
declares `capabilities`, `consent_required`, and the `binds` it will stream,
and mounting it is gated by the Block I consent contract before the process is
ever spawned. Block H implements **only the declarative path** (H2 loads a
plugin with no `executable`); the behavioral fields are *specified* here so the
schema is frozen once, but the process supervisor, the gate, and the NDJSON
stream are Block I.

This keeps H2's promise honest: "load a declarative plugin (scene fragment +
tokens, zero code)" is enforceable precisely because the schema makes "zero
code" the absence of one field, checkable at load with no gate involved.

---

## H1 — the plugin manifest schema

**Unblocks:** H2 (load declarative), H3 (mount by id), H4 (merge tokens), H5
(validate the namespace), H6 (`/ui plugin add`), H7 (Scene 6 golden). **Scene:**
6. **Questions:** Q13 (a plugin overlay joins the focus stack), Q15 (identity is
what consent checks).

### Format: JSON, for the same reason the scene is JSON

The manifest is JSON, not the TOML arxi-sim used
(`arxi_cli_sim/internal/ext/manifest.go`). SCENES.md `:19` already argued the
choice for the scene document — "JSON is what models write without
hallucinating, and every plugin language can parse it" — and the manifest embeds
scene fragments (`mounts`) and a `tokens` block whose shapes are *already* JSON
in this project. A TOML outer wrapper around JSON inner documents would force two
parsers and two error models for one file. The validator maps `offset →
file:line` for JSON already (SCENES.md `:23`); reusing it is why a manifest
refusal can carry an address for free.

### The full schema

```json
{
  "id": "tick",
  "name": "Community Ticker",
  "version": "1.2.0",
  "protocol": "ext/v1",

  "tokens": {
    "profit": { "fg": "green" },
    "loss":   { "fg": "red" }
  },

  "mounts": [
    {
      "where": "top-right",
      "fragment": {
        "id": "tick-overlay",
        "type": "overlay", "anchor": "top-right", "min_width": 12,
        "children": [
          { "type": "row", "children": [
            { "type": "sparkline", "bind": "tick.history" },
            { "type": "text", "bind": "tick.price", "style": "profit" } ] } ]
      }
    }
  ],

  "executable": "./tick",
  "args": ["--interval", "5s"],
  "capabilities": ["events.subscribe", "events.emit"],
  "consent_required": true,
  "binds": {
    "tick.price":   { "kind": "text",      "mock": "$0.00" },
    "tick.history": { "kind": "series",    "mock": [0, 0, 0] }
  }
}
```

### Field-by-field, and which block reads each

**Identity block (always required).**

- **`id`** — the plugin id, `[a-z][a-z0-9-]{0,62}` (the arxi-sim name pattern,
  `manifest.go:203`). It is the **bind namespace prefix**: every bind the plugin
  streams is `id + "." + field`, so `tick.price` is legal only under a plugin
  whose id is `tick`. It is also the prefix every mounted fragment id is scoped
  under (see H-B). One id, three roles — namespace, mount scope, and the key the
  grant is remembered against — which is why it is required even for a
  declarative manifest that streams nothing.
- **`name`** — the human-facing label shown in the consent screen and the
  installer (Scene 7). Free text; not an identifier.
- **`version`** — required; part of the consent identity (Q15). A version bump
  is a new identity and re-asks consent, which is the point: a plugin that
  changed is a plugin the grant never covered.
- **`protocol`** — the wire protocol version, a **closed set** (`ext/v1` today,
  as arxi-sim `manifest.go:210`). Closed because the protocol is code on both
  sides; an unknown protocol is refused at load, not negotiated. A declarative
  manifest still names it, so the field's absence is never ambiguous with
  "old manifest".

**Declarative block (the zero-code contribution; both plugin kinds may carry
it).**

- **`tokens`** — a theme fragment merged at precedence *user > plugin > factory*
  (TOKENS.md `:157-160`, already signed). Optional. Its shape is exactly a
  theme's token block, so the existing token validator checks it; H4 only adds
  the merge, not a new validator.
- **`mounts`** — an array of `{where, fragment}` objects. `fragment` is an
  ordinary scene subtree (validated by the *scene* validator, the same one
  `/ui add` re-runs), and `where` is the addressing that places it (H-B below).
  A declarative plugin is entirely this field plus `tokens`. Optional only in
  the degenerate sense that a plugin with neither `mounts` nor `tokens` does
  nothing and is refused as empty.

**Behavioral block (present iff `executable` is; specified here, built in Block
I).**

- **`executable`** — a **relative** path inside the package (arxi-sim
  `spec/extensions.md:26`, "explicit relative executable path, regular files
  only"). Its presence is the declarative/behavioral discriminator (above). An
  absolute path, a `..` escape, or a symlink is refused — the package is the
  trust boundary and the executable must live inside it.
- **`args`** — the argv passed to the executable; part of the consent identity
  (Q15), so changing an arg re-asks consent. Optional, default `[]`.
- **`capabilities`** — a **closed set** (arxi-sim `capability.go:12-17`:
  `events.subscribe`, `events.emit`, `inbox.answer`, `actions.register`). Closed
  because each capability is a door the host opens in its own code; a manifest
  cannot mint one. Declaring a capability is *not* being granted it (the
  `not_declared` vs `not_granted` split, LESSONS.md `:65`): absence from the
  manifest is `not_declared`, presence-without-consent is `not_granted`, and the
  two are different refusals so a user can tell "it never asked" from "you said
  no".
- **`consent_required`** — the manifest's own declaration that mounting it
  crosses the consent gate. For a behavioral plugin this is `true` and the
  Block I gate enforces it; the field exists so the *declarative* loader can
  refuse early — a manifest with an `executable` and `consent_required: false`
  is a manifest trying to run code without a gate, and is refused at load before
  Block I is even consulted.
- **`binds`** — the declaration of the `<plugin-id>.*` fields the process will
  stream: a map from full bind path (`tick.price`) to `{kind, mock}`. `kind`
  names which node the value is legal under (a `text` value, a `series` for a
  sparkline) so the validator can reject `sparkline bind:"tick.price"` when
  `tick.price` is declared `kind:"text"`. `mock` is the placeholder value the
  **preview** path (Scene 7 / Q16, Block J) renders before any frame arrives —
  the concrete form of "an unsatisfied plugin bind renders as a placeholder"
  (BINDS.md §4.4). This map is the single source H5 validates the fragment's
  `tick.*` binds against; see H-C.

---

## H-B — where a mount lands, and how fragment ids stay unique

**Unblocks:** H3 (mount fragments by id). **Reuses:** ADDRESSING.md (D2, signed
2026-09-22).

A mount must say *where* in the host scene its fragment goes, and the project
already signed a `where` vocabulary for exactly this — D2's addressing for
`/ui add`/`move` (`docs/ADDRESSING.md`). H-B's decision is to **reuse it
unchanged**, so a plugin author and a `/ui add` user name a location the same
way and the two paths cannot drift on what `below_input` means. A mount's
`where` accepts the D2 forms: `above <id>` / `below <id>`, `into <id> [top]`,
and the semantic anchors `below_input` / `above_input`. It adds the overlay
anchors Scene 6 needs (`top-right`, `bottom`, …) as a `where` that means "a new
`overlay` child of root at that anchor" — the mount does not need to name an
existing id to float, which is why the ticker example mounts `top-right`
without referencing any host node.

This has three consequences, each already paid for by D2/F1–F3 and reused rather
than reinvented:

1. **Mounting is `/ui add node`, from the host side instead of the user side.**
   H3 does not need a new placement engine: `internal/patch/add.go` already
   inserts a fragment at a D2 `where`, source-to-source over the generic map
   tree so a fragment's undeclared keys survive (F1). Mounting a plugin fragment
   is the same insert, attributed to the plugin instead of to a `/ui` command.
2. **The id-uniqueness invariant is the collision defense.** ADDRESSING.md
   `:49-63` makes two nodes with the same non-empty id a load-time failure, per
   document. A mounted fragment whose id already exists in the host tree is that
   failure, refused with `file:line` — so a plugin cannot silently shadow a host
   node or another plugin's node.
3. **Fragment ids are namespaced under the plugin id, and this is the rule that
   makes (2) livable.** Requiring every plugin author to pick globally-unique
   raw ids is a collision waiting to happen (two tickers both using `overlay`).
   Instead, the loader **prefixes every id in a mounted fragment with
   `<plugin-id>/`** — `tick-overlay` mounted by plugin `tick` becomes
   `tick/tick-overlay` in the composed tree. A plugin's fragment refers to its
   own nodes by their unprefixed ids (the prefixing is mechanical, applied by
   the loader after validation), and the `<plugin-id>/` prefix cannot collide
   across plugins because the id namespace is closed on the same pattern the
   bind namespace is. This is the one addressing rule Block H adds that D2 did
   not need, because D2 never composed two authors' trees.

Unmounting (a plugin removed, or `/ui plugin remove <id>`) is the inverse: drop
every node whose id begins `<plugin-id>/` and every token the plugin defined.
Because both are namespaced, removal names no host node and cannot orphan one —
the same property that lets `show *` clear `ui.hidden` without naming an id
(F3).

---

## H-C — validating the `<plugin-id>.*` namespace (declared vs used)

**Unblocks:** H5. **Reuses:** the `rowSchemas` scope mechanism
(`internal/scene/validate.go:140-154`).

Today a `tick.price` bind is refused: `validateOneBind`
(`validate.go:250-276`) checks `signedBinds[bind]` and there is no plugin entry,
so every `<plugin-id>.*` bind is "unsigned bind … every bind must appear in
BINDS.md §4.5". H5 lifts that refusal *only for binds a manifest declares*, and
the mechanism to reuse is the one already in the file for `row.*`: a **scope**
threaded through validation that is non-nil only inside a declared context.

The rule, mirroring `row.*` exactly:

- A bind matching `<plugin-id>.<field>` is legal **iff** it appears inside a
  fragment mounted by a plugin whose `id` is `<plugin-id>` **and** `<field>` is
  a key the manifest's `binds` map declares. This is the direct analogue of
  "`row.<field>` is legal iff inside a `row_template` and `<field>` is in the
  source list's `RowSchema`" (E2). The manifest's `binds` map *is* the plugin's
  schema, the same shape `rowSchemas` is.
- A `<plugin-id>.*` bind **outside** any mount of that plugin is refused — the
  namespace does not leak into the host scene, exactly as a `row.*` bind outside
  a template is refused.
- A declared bind used under the **wrong node type** for its `kind` is refused:
  `binds["tick.price"].kind == "text"` used as `sparkline bind:"tick.price"` is
  a `file:line` refusal naming the field, its declared kind, and the node. The
  `kind` is the plugin-bind analogue of the axis a `reveal`/`scroll` prop rides
  — declaring it is what lets the validator reject the wrong pairing instead of
  rendering a silent mismatch.
- A **declarative** manifest (no `executable`, so no `binds`) that mounts a
  fragment using any `<plugin-id>.*` bind is refused: it streams nothing, so the
  namespace is empty, so the use is undeclared. This is the load-time face of
  the H-A split — a zero-code plugin binds only host fields.

`SignedBinds()` (`validate.go:102-130`) is exported so Phase 2's repair loop can
tell a model which binds exist; the comment there warns against a fourth
hand-copied inventory. A plugin's declared binds must feed the **same** single
source — the manifest's `binds` map is consulted directly, never copied into a
second table — so H5 adds a code path, not a parallel inventory.
