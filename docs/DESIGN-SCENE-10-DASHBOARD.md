# Scene 10 — DASHBOARD: how click-to-maximize reads `ui.max` without a `when` operator

This document signs the one open decision Scene 10 (DASHBOARD) needs before its
golden can be pinned, the last of the eleven. It follows the on-paper method of
Blocks D, G, H, J, K2 and K4: argue the decision in full, then freeze the durable
seam — here, the two host binds that let a pane gate on *which* pane is
maximized — so the fixture is built against a fixed vocabulary rather than one
invented under the pressure of a half-written scene.

**Status: signed.** The two binds below are added to `docs/BINDS.md` §4.3 and
`internal/scene/validate.go`'s inventory, resolved in `internal/engine/render.go`,
and the fixture + goldens are pinned in the same change.

## What the scene is

SCENES.md Scene 10: a 2×2 dashboard of four panes, each clickable to maximize.
Nested `row`/`stack` with `weight` builds the grid — no `grid` primitive — and a
pane's header carries `on_press: "cmd:/max <pane>"`, which writes the host-owned
`ui.max` interface state (Q21: `ui.*` is read by scenes with `when` and written
only through registered commands). Two visual states:

- **nothing maximized** (`ui.max` is null): the four panes tile the frame 2×2.
- **one maximized** (`ui.max` is a pane id): that pane fills the frame, the other
  three are gone.

## The problem: the shorthand SCENES.md wrote is not a predicate this engine has

The Scene 10 sketch reads `per-pane when: ui.max==''`. That `==` is the trap. This
engine's `when` is a **bare truthiness test with no comparison operator** — a
decision made and defended repeatedly after the scenes were sketched:
`internal/scene/validate.go:277` ("This engine's `when` has no comparison
operator"), `internal/engine/render.go:2250` (evalWhen: non-empty is truthy; "",
"0", "false", and the placeholder are falsy). The codebase has met this exact wall
three times and resolved it the same way each time rather than growing the
operator:

- `config.settings` cannot gate a node on `row.kind == "toggle"`, so the engine
  synthesizes `row.is_toggle` / `row.is_text` and the template gates on those
  (validate.go:273-280).
- `community.matches` cannot gate a highlight on `row.index == selected`, so the
  engine synthesizes `row.selected` per row from `community.selected`
  (validate.go:263-267).
- The slash menu cannot gate the status bar on `slash.active == false`, so the
  host computes `status.active` as the inversion (BINDS.md §4.3).

Three precedents, one shape: **where `when` would need to compare, the host
computes the boolean and signs it.** Scene 10 is the fourth instance, not a reason
to reopen the operator decision. Adding `==` to `when` would touch the single most
load-bearing primitive in the format and invalidate the comments and tests that
assert its shape across the engine — a wide blast radius to buy what a signed
boolean buys locally.

## The decision: two host binds, derived from `ui.max`

Both are pure functions of the existing `ui.max` host state (fold field `UIMax`,
BINDS.md §4.3). Neither adds a fold field or a projection step — they resolve in
`render.go` the same way `ui.max` itself does, so there is no new lifetime to get
wrong (the arxi-sim remembered-row class). They are host view state, so they are
signed in §4.3 beside `ui.max`.

- **`ui.max.none`** — truthy exactly when no pane is maximized (`UIMax == ""`),
  falsy otherwise. This is the `status.active` precedent: a host-computed
  inversion of a state `when` cannot negate. It gates the 2×2 grid container, so
  the grid shows only while nothing is maximized.

- **`ui.max.is.<id>`** — a bind *family* (the `ui.plugin.<id>` precedent,
  render.go:2043), truthy exactly when `UIMax == <id>`. The suffix is the pane id
  the gate asks about. Each pane's maximized view is gated `when:
  "ui.max.is.chat"`, so exactly one maximized pane draws, chosen by equality
  against `ui.max` — the comparison the sketch wanted, expressed as a resolved
  boolean rather than an operator in the predicate language.

### Why a family and not four signed booleans

Signing `ui.max.is.chat`, `ui.max.is.team`, … as four exact binds would bake the
factory dashboard's pane ids into the host inventory, which contradicts the
thesis (the scene tree is open; a user's dashboard may name its panes anything).
The family resolves any suffix against `UIMax`, so a downloaded dashboard with
panes `left`/`right` gates `when: "ui.max.is.left"` with no change to the host —
the same open-vocabulary property `ui.plugin.<id>` has for plugin ids.

### The default direction is safe

An unresolved or empty `UIMax` makes `ui.max.none` truthy (grid shown) and every
`ui.max.is.<id>` falsy (no maximized pane) — exactly the signed default "null — no
pane is maximized" (BINDS.md). The dangerous direction evalWhen's comment names (a
gate failing *open* and painting chrome nobody asked for) does not occur: the
grid is the boot state, and no maximized pane appears until `ui.max` names one.

## The wire / interface seam being frozen

- `ui.max.none` : host bind, read-only to scenes, resolved from `UIMax == ""`.
- `ui.max.is.<id>` : host bind family, read-only to scenes, resolved from
  `UIMax == <id>`.
- `on_press: "cmd:/max <pane>"` : already signed (BINDS.md §4.3/§4.8; the `max`
  command is in the slash registry). Writes `ui.max`; the two binds above read it.

No arxi-core verb, no fold-projection change, no new dependency. The pin is a
host-view-state scene like Scene 5 (CONFIG): a test seeds the `fold.State` the
host would write (`UIMax`) and renders at a fixed size.

## What this does not build

Live click dispatch — a key press on a focused pane header actually writing
`UIMax` through the `/max` command — is the loop wiring, not the format decision,
and is deferred the way Scene 5's live config writes were. The golden pins the
*format*: the scene validates, and renders both states deterministically from a
seeded `UIMax`. The `cmd:/max` dispatcher in `cmd/arxi-tui` is a later increment.
