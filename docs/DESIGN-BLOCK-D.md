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
