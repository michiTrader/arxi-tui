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
