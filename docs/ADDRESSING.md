# Addressing — how a `/ui` verb names a place in the scene tree

Status: **signed 2026-09-22** (D2, `docs/DESIGN-BLOCK-D.md`, owner-accepted).

This is the **write-path** vocabulary. `docs/BINDS.md` addresses *read* state —
what a node may display — and is deliberately read-only. This document owns the
other half: how a command that inserts, moves, hides or shows a node names the
position it acts on. Keeping the two apart is the point — a write vocabulary
mixed into BINDS.md would blur the one boundary that doc is careful about.

## 1. The `where` expression (for `/ui add` and `/ui move`)

`/ui add node <where> <fragment>` and `/ui move <id> <where>` both take a
`where`: one insertion point, resolved against the **active document** (the
scene the user is looking at, never a remembered one). Every form resolves to a
parent node and an index within its children.

| form | resolves to | example |
|---|---|---|
| `above <id>` | previous sibling of `<id>` | `/ui move status above chat` |
| `below <id>` | next sibling of `<id>` | `/ui add node below chat {…}` |
| `into <id>` | last child of container `<id>` | `/ui add node into tasks {…}` |
| `into <id> top` | first child of container `<id>` | `/ui move prompt into main top` |
| `below_input` / `above_input` | sibling adjacent to the surface's input node | `/ui add node below_input {…}` |

The **semantic anchors** `below_input` / `above_input` resolve *without naming
an id*. The id of the input node is a scene-authoring detail that changes
between themes; its *role* — the one node the user types into — is stable, so
the anchor addresses the role. This is what lets a downloaded theme accept
`/ui add node below_input …` without the user first reading the theme to learn
the input node's id.

## 2. Refusals — each addressed with `file:line` into the active document

1. `<id>` not found → refusal naming the id and that no node carries it.
2. `<id>` found more than once → refusal. Ids are addresses; a duplicate makes
   every `where` ambiguous. See §3.
3. `into <non-container>` → refusal naming the node's type and that only
   containers (`stack`/`row`/`box`/`overlay`) take children.
4. `move <id> <where>` where `<where>` resolves to a position inside `<id>`'s
   own subtree → **cycle refusal**: a node cannot become its own descendant.
   This is the one refusal unique to `move`; `add` cannot form a cycle because
   the fragment is new.
5. `below_input` / `above_input` with zero or more than one input node on the
   active surface → addressed refusal. Ambiguity here is a real scene defect
   (which input did you mean?), not something to resolve by guessing.

## 3. Id uniqueness is a load-time invariant

`/ui move` and every `<id>` form above are unsafe without it: a duplicate id
makes the target ambiguous, and it is *already* latent independent of the write
path — `focus_glow` keys on `id == ui.focus`
(`internal/engine/render.go`), and `ui.max` / `cmd:/max <pane>` address a pane
by id, so two nodes sharing an id already double-glow or fight over the maximised
slot. Therefore: **a scene document with two nodes carrying the same non-empty
`id` fails validation at load time**, with a `file:line` error naming the id and
both offending nodes.

Scope is **per document**. The eval corpus fixtures (`testdata/eval/*.json`)
embed several scene documents in one file (a `base` plus attempt variants), so a
per-*file* uniqueness check would wrongly flag them; the invariant is checked on
each parsed `Document`, not on file text. The empty id is exempt — an unnamed
node is not addressable and many nodes legitimately have none.
