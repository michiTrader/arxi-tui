# Block D — Phase 3 design gate (proposals, awaiting signature)

This document drafts the four paper decisions NEXT.md's Block D lists (D1–D4).
Each unblocks a Phase 3 implementation block (E/F/G) and touches no code: it is
vocabulary, designed on paper the SCENES/BINDS/TOKENS way so the format is
frozen *before* a renderer or a command depends on it.

**These are proposals, not signatures.** The frozen docs (`docs/BINDS.md`,
`docs/TOKENS.md`) record what the owner has signed; this file records what is
proposed for signing. Nothing here is a signed row yet. Each proposal ends with
the exact text to paste into the frozen doc **on approval**, so signing is a
review of an argued decision and a copy, not a fresh authoring pass. Until then
the code guards that refuse these features (the `row_template`/`on_press`/
`scroll` refusals in `internal/scene/validate.go`, and
`TestHideAndShowAreRefusedRatherThanInventingABind` in `internal/patch`) stay as
they are: a refusal is cheaper to lift than a wrong format is to unship.

Order of dependency: D1 unblocks Block E, D2 and D3 unblock Block F, D4 unblocks
Block G. None depends on another, so they can be signed in any order or
separately.

---

## D1 — the `row.*` relative bind namespace

**Unblocks:** E1 (`row_template` render), E2 (relative-bind resolution), E3
(`team.members` projection), E6 (removing the `internal/scene/validate.go:241`
refusal). **Scenes:** 5 (CONFIG), 9 (SUBAGENTS). **Questions:** Q10 (relative
binds exist inside templates), Q20 (action args interpolate relative binds).

### The problem it solves

`row_template` is refused today for one reason, stated verbatim in
`internal/scene/validate.go:241`: "relative binds inside templates are
SCENES.md Q10 / Scene 5, and the `row.*` namespace they need is signed nowhere
in BINDS.md yet." A `list` binds to an *array of objects*; the template is
instantiated once per element, and a node inside it must be able to address
*this element's* fields. There is no signed vocabulary for that address, so the
whole feature is held. This proposal signs it.

### The decision

A `row.*` bind is a **relative** address into the current template row. It is a
fourth namespace alongside the three BINDS.md §1 already names, and the only one
that is not resolved against host state — it is resolved against one element of
the array the enclosing `list` binds to.

1. **Scope.** `row.*` is legal **only** inside a `row_template` subtree, and
   inside the `on_press` action arguments of nodes in that subtree. Anywhere
   else it is a validation error with `file:line`. A relative bind with no row
   to be relative to is a scene defect, not an empty value.
2. **Resolution.** The enclosing `list` binds to an array of objects. The
   template renders once per element; inside a given instance, `row.<field>`
   resolves to that element's `<field>`. This is the same host-computes /
   scene-names split as every other bind (BINDS.md §3): the array and its
   element shape are host state; the scene only names a field of the current
   element.
3. **Legal field names are the source array's element schema.** `row.<field>`
   is validated against the declared element shape of the *list's* `bind`, not
   against a global list. BINDS.md §4.1 already declares those shapes:
   `team.members` rows are `{id, state, role, busy, turns, spent_usd}`,
   `agent.todos` rows are `{task, blocked_on, actor}`, `slash.matches` rows are
   `{name, category, description}`. A template over `team.members` may address
   `row.state`; the same spelling over `agent.todos` is a refusal, because that
   schema has no `state`.
4. **Interpolation in action args (Q20).** In an `on_press` argument,
   `{row.<field>}` is replaced by the element's field. Scene 9's row is
   `on_press: "cmd:/agent {row.id}"`. This settles the SCENES.md prose spelling
   `{m.id}` (line 200) to its canonical form `{row.id}`: the `m` was an
   illustrative alias, and the signed vocabulary deliberately has **no per-list
   alias**. One relative namespace keeps every template in the ecosystem
   uniform — the arxi-sim lesson that a closed, shared vocabulary is what lets
   one tool read another's document.
5. **`when` inside a template.** `when: "row.<field>"` gates a per-row node on
   the current element's field — e.g. Scene 9's per-row spinner gated on
   `row.busy`. Same truthy/falsy rule as any `when`.

### Empty state and refusals (each addressed)

- A `row.<field>` where `<field>` is not in the source array's element schema:
  validation error naming the field, the node, and the source `bind` whose
  schema was checked. This is the misspelling net — the same one absolute binds
  get — so an author who writes `row.stat` is told where and what, not left with
  silence.
- A `row.*` bind outside any `row_template`: validation error naming the node
  and stating that relative binds are template-only.
- A `list` whose `row_template` is present but whose own `bind` resolves to a
  scalar (not an array of objects): validation error, because a scalar has no
  rows to instantiate the template over.
- A field declared by the schema but absent or empty in a *particular* element:
  falsy, rendered as the `"[…]"` placeholder — the ordinary unresolved-bind
  rule (BINDS.md §4.6), never a crash. The distinction is deliberate:
  *misspelled* is a load-time refusal, *absent in one row* is a run-time
  placeholder.

### Text to sign (append to `docs/BINDS.md` §1, fourth bullet, and a new §4.7)

Add to the §1 namespace list:

> - **`row.*` — relative, template-scoped.** Legal only inside a `row_template`
>   subtree and the `on_press` args of nodes in it. `row.<field>` resolves to
>   `<field>` of the current element of the array the enclosing `list` binds to;
>   the legal field names are that array's element schema (§4.1). Validated at
>   load time against the source schema; `{row.<field>}` interpolates the same
>   value into an action argument (Q20).

Add §4.7 "Relative row schemas": a table stating, for each array-of-objects
bind in §4.1, the element field set its templates may address (copied from the
`source events / payload` column so the two never drift), plus the three
refusal rules above.

---

## D2 — the `where` addressing vocabulary for `/ui add` and `/ui move`

**Unblocks:** F1 (`/ui add node <where> <fragment>`), F2 (`/ui move <id>
<where>`). **Scenes:** the `/ui` self-extension surface (Q23's diff-and-attribute
model). **Home:** a new write-path doc, `docs/ADDRESSING.md` — see "Where this
is signed" below.

### The problem it solves

`/ui add` and `/ui move` both take a "where": a way to name a position in the
scene tree. BINDS.md addresses *read* state (what a node may display); it says
nothing about *where a node goes* when a patch inserts or moves it. Without a
signed vocabulary the position is invented inside the command implementation —
the exact "format designed inside a command ahead of the phase meant to choose
it" failure the hide/show refusal already names. This proposal signs the write
path.

### The decision

A `where` expression names one insertion point. Four forms, all resolving to
"a parent and an index":

1. **`above <id>`** — insert as the previous sibling of the node with that id.
2. **`below <id>`** — insert as the next sibling.
3. **`into <id>`** — append as the last child of the container `<id>`;
   **`into <id> top`** prepends as the first child. Refused if `<id>` is not a
   container (`stack`/`row`/`box`/`overlay`) — only containers hold children.
4. **Semantic anchors** — `below_input` / `above_input`, which resolve to the
   sibling position adjacent to the surface's input node **without naming an
   id**. The id of the input node is a scene-authoring detail that changes
   between themes; its *role* (the one node the user types into) is stable, so
   the anchor addresses the role. This is what lets a downloaded theme accept
   `/ui add node below_input …` without the user first reading the theme to
   learn the input node's id.

An `<id>` is resolved against the *active document*, so `/ui` addresses the
scene the user is actually looking at, not a remembered one.

### Refusals (each addressed, each with `file:line` into the active document)

1. `<id>` not found → error naming the id and that no node carries it.
2. `<id>` found more than once → error. Ids must be unique for addressing to
   mean anything; a duplicate id makes every `where` ambiguous. **This proposal
   also asks that id-uniqueness become a load-time scene invariant** if it is
   not already one, because `move`/`add` are unsafe without it — noted here so
   the dependency is not discovered inside F2.
3. `into <non-container>` → error naming the node's type and that only
   containers take children.
4. `move <id> <where>` where `<where>` resolves to a position inside `<id>`'s
   own subtree → cycle refusal: a node cannot become its own descendant. This
   is the one refusal unique to `move`; `add` cannot form a cycle because the
   fragment is new.
5. `below_input` / `above_input` with zero or more than one input node on the
   active surface → addressed refusal. Ambiguity here is a real scene defect
   (which input did you mean?), not something to resolve by guessing.

### Where this is signed

This is a **write-path** vocabulary — how a command names the tree — and
BINDS.md is deliberately read-path only ("a read-only address into host
state", §1). Mixing a write vocabulary into it would blur the one boundary
that doc is careful about. **Proposal: a new `docs/ADDRESSING.md`** that owns
the `/ui` verbs' tree-addressing grammar (the `where` forms here now, and D3's
hide/show target grammar next), leaving BINDS.md for read state. The
alternative — a "Patch addressing" section in PLAN.md — keeps the doc count
lower but buries a frozen vocabulary inside a narrative doc; the SCENES/BINDS/
TOKENS method exists because a frozen vocabulary wants its own signable home.
Recommendation: `docs/ADDRESSING.md`. Its initial content is the four forms and
five refusals above, written as a signed table with an example per form.

---

## D3 — the per-node view-state gate for `/ui hide` and `/ui show`

**Unblocks:** F3 (`/ui hide`/`show`) and lets
`TestHideAndShowAreRefusedRatherThanInventingABind`
(`internal/patch/patch_test.go`) become a test of the *new* signed behaviour.
**Questions:** Q21 (`ui.*` is host-owned, read with `when`, written only through
registered commands).

### The problem it solves

The draft of `hide` spelled it `when: "ui.hidden"` — a scalar bool — and the
validator refused it, correctly, because that bind is signed nowhere. The
refusal's own message records why a *scalar* is the wrong fix: every `ui.*` row
BINDS.md signs is a single id (`ui.focus`, `ui.max`), so a scalar flag makes
`/ui hide a` silently unhide `b`. What hide/show need is a **per-node** gate
with **collection semantics**. This proposal signs one.

### The decision

Sign `ui.hidden` as a **set of node ids** — host-owned view state (§4.3),
written only through the registered commands `cmd:/ui hide <id>` and
`cmd:/ui show <id>` (`cmd:/ui show *` clears it). It is consumed **by the engine
walk**, not by a scene `when`:

> A node renders iff its `when` is truthy **and** its id is not in `ui.hidden`.
> When an id is in the set, that node and its subtree are dropped from the
> frame.

### Why a set consumed by the walk, and not a `when`

The obvious spelling — give the node a `when` that is false when hidden — cannot
work in this engine, and the reason is worth stating so it is not re-attempted:

- **There is no negation.** `when` shows a node when its bind is *truthy*
  (BINDS.md §4.6). To *hide* on membership you would need "show when **not** in
  the set", and no `!` operator exists — `evalWhen` resolves the whole string
  through `resolveBind`. The counterfactual eval-when defect LESSONS.md records
  was exactly an author reaching for a `!` the engine does not have.
- **The inverse ("visible-set", default-visible) breaks the falsy rule.** You
  could give every node `when: "ui.visible.<id>"` defaulting truthy, but an
  unresolved bind is *falsy* by signed contract (§4.6) — so a fresh document
  would render with everything hidden. The one rule that makes community scenes
  degrade safely would have to be inverted for this one field.

A set consumed by the walk sidesteps both: it needs no operator (membership is
computed by the host, the scene names nothing), it respects uniqueness (a set of
ids cannot confuse `a` with `b`), and it composes with `when` by simple
conjunction rather than fighting it. `hide`/`show` are the user's manual
override; `when` is the scene's own condition; a node draws only when both
agree. Order does not matter — either one removes it.

Optionally, `ui.hidden.<id>` may *also* be exposed as a derived truthy bind so a
scene can read membership in a `when` (e.g. to render a "3 hidden" badge). That
is secondary and can be signed later; the load-bearing half is the walk filter.

### Refusals / empty state

- `/ui hide <id>` / `/ui show <id>` where `<id>` is not in the active document:
  addressed refusal (the D2 "id not found" rule; hide/show share D2's
  resolution).
- Empty set is the default: every node's visibility is decided by its own `when`
  alone, exactly as today. Signing this field changes no existing golden,
  because the empty set is a no-op.

### Text to sign (append to `docs/BINDS.md` §4.3)

> | `ui.hidden` | set of node ids | the ids the user has hidden via `cmd:/ui hide`; `cmd:/ui show <id>` removes one, `cmd:/ui show *` clears the set | on `cmd:/ui hide`/`show` | empty set — every node's visibility is decided by its `when` alone |

Plus a consumption note under the table: *the engine walk drops any node whose
id is in `ui.hidden`, together with its subtree, applied in conjunction with
`when` — a node renders iff `when` is truthy and its id is not hidden.*

When signed, update `TestHideAndShowAreRefusedRatherThanInventingABind` to pin
the new behaviour (a hidden id drops its node; `show` restores it; the set
distinguishes `a` from `b`), keeping a case that a *scalar* spelling is still
refused so the rejected design cannot creep back.

---

## D4 — the `[anim]` timing token

**Unblocks:** G1 (`transition`), G2 (`scroll`), G3 (`reveal`), G4 (`enter`) —
the four Scene 4 props that `internal/scene/node.go` parses and *warns* about
today, each blocked on this token. **Scene:** 4 (ANIMATION). **Questions:** Q8
(timing is a global `[anim]` token with per-node override), Q9 (the host
announces "row new", the scene owns the animation).

### The problem it solves

Four animation props are parsed, addressed and refused with the identical
reason, stated in `node.go`'s `Scroll`/`FocusGlow` comments: they "need elapsed
time, and the timing format Q8 assigns to a global `[anim]` token does not
exist — docs/TOKENS.md does not mention `anim`." `focus_glow` shipped precisely
because it needs *no* clock. This proposal defines the timing format so the
other four can move from warned to implemented — and node.go's comments say
this paragraph "is what should be updated first" when it is signed.

### The decision

A **timing token** is a named duration-and-curve, referenced by animation props
the way a style token is referenced by `style`. It lives in a **separate theme
section** so it cannot collide with the style-token namespace:

```json
"anim": {
  "default":     { "duration_ms": 200, "curve": "ease_out", "fps": 30 },
  "marquee":     { "duration_ms": 0,   "curve": "linear",   "fps": 20 },
  "reveal.fast": { "duration_ms": 120, "curve": "ease_out", "fps": 30 }
}
```

1. **Shape of a timing definition:** `duration_ms` (int; `0` means *continuous*,
   no end — the marquee case), `curve` (a name from the closed set below), and
   an optional `fps` (int; the tick rate, defaulting to a host constant).
2. **The curve set is closed:** `linear`, `ease_in`, `ease_out`, `ease_in_out`,
   `step`. This is the one place D-block vocabulary is deliberately *not* open,
   and the reason is the plugin boundary: a style token is pure data the emitter
   interprets, but a curve is an **easing function** — code. An open curve
   namespace would be "bring your own render/timing code into the mother
   renderer", which is exactly what the sparkline rule (Scene 6: "the plugin
   composes our primitives, it does not bring render code") and Q14 (a render
   that is none of our nodes is the Phase-4 wasm ADR) forbid. Adding a curve is
   a mother-binary change with its own freeze, like adding a node type.
3. **Global default + per-node override (Q8).** Every animated prop uses the
   `anim.default` timing unless the node names a timing token. A node overrides
   by naming one: `"transition": { "anim": "reveal.fast" }`,
   `"reveal": { "anim": "<token>" }`, `"enter": { "row": true, "stagger":
   "<token>" }`. `scroll` keeps its own `speed` (cells per tick) and
   `pause_when` (a bind) from SCENES.md and takes its *clock* — the tick rate —
   from a timing token (`anim.marquee` by default), so its continuous cadence is
   defined in one place rather than reinvented in the renderer.
4. **The fold never sees this (Q9, architectural boundary).** A timing token is
   consumed only by the render/emit layer that owns the host clock; it never
   enters the fold. "The fold never waits on a scene, an animation, or a plugin"
   — a timing token is the animation, and it stays on the far side of that line.

### Validation

- A prop naming an `anim` token absent from the active theme → load-time error
  with `file:line`, naming the token and the node — the same net as an undefined
  style token.
- A timing definition with a curve outside the closed set, or a negative
  `duration_ms`/`fps` → theme-load error naming the offending key and listing
  the legal curves. The remedy differs from a style token's on purpose: it is
  "choose a supported curve", not "define it", because a curve is code the
  theme cannot supply.

### Text to sign (new section in `docs/TOKENS.md`)

"Timing tokens (the `[anim]` vocabulary)": the `anim` theme section, the
definition shape, the closed curve set with its rationale, the default +
per-node override rule, the fold boundary, and the two validation rules above.
When signed, update SCENES.md's Scene 4 status paragraph (lines 149–155) —
which explicitly says it "is what should be updated first" — to move
`transition`/`scroll`/`reveal`/`enter` from "warned" to "implementable", and
update the `Scroll`/`FocusGlow` comments in `node.go` that cite the missing
token.
