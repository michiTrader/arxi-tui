# Token system — the open style vocabulary

A **token** is a named style. Scenes reference tokens by name (`"style": "dim"`,
`"style": "warn"`), and the theme maps each name to concrete attributes. This
inversion buys three things at once:

1. **Golden files stay readable** — the frame says `"dim"`, not `\x1b[2m`.
2. **Themes swap without re-rendering** — token resolution happens at emit time,
   so switching from light to dark is purely a theme file change.
3. **Terminal capability is an emit concern** — a 24-bit teal becomes palette
   index 6 when the terminal reports ANSI-only, and that downgrade lives in one
   place rather than scattered through every renderer.

## Design principle: open definition, closed reference validation

The token namespace is **open by design** (SCENES.md:43-45). Users and plugins
may mint tokens (`"warn": "yellow"`, `"profit": "#22c55e"`). The validator
checks that every token a scene references is defined in the active theme, but
it does not enforce a closed inventory — that is the deliberate inversion of
arxi-sim, where the theme was a closed table and an unknown key was a compile
error.

A scene that references an undefined token fails validation with a `file:line`
error naming the missing token and the node that asked for it. The remedy is
either to define the token in the theme or to fix the scene.

## Format: JSON

A theme is a JSON object mapping token names to style definitions. Each style
defines foreground color (`fg`), background color (`bg`), and text attributes
(`attrs`). All three fields are optional; omitted fields inherit from the
terminal's default.

```json
{
  "dim": { "attrs": ["dim"] },
  "header": { "attrs": ["bold"] },
  "warn": { "fg": "yellow" },
  "error": { "fg": "red", "attrs": ["bold"] },
  "success": { "fg": "#22c55e" },
  "input.placeholder": { "attrs": ["dim"] }
}
```

### Token names

Token names are arbitrary strings. Namespacing with dots is conventional but
not required (`"input.placeholder"`, `"markdown.code"`). Names are
case-sensitive.

### Color values

Three spellings are legal:

- **Palette name:** `"red"`, `"bright-cyan"`, `"yellow"` — the eight base
  colors plus their bright variants (16 total). These are the colors the user
  chose in their terminal's own color scheme, which is almost always what they
  want, and they survive on terminals without truecolor.
- **Palette index:** `"0"` to `"255"` — numeric strings referencing the
  terminal's 256-color palette. Indices 0-15 are the named colors above;
  16-231 are a 6×6×6 RGB cube; 232-255 are grayscale.
- **Hex RGB:** `"#rrggbb"` or `"rrggbb"` — 24-bit color. Downgraded to the
  nearest palette index on terminals without truecolor support.

### Attributes

`attrs` is an array of attribute names. All are optional and may be combined:

- `"bold"` — heavier weight
- `"dim"` — reduced intensity (the factory scene's only emphasis)
- `"italic"` — slanted text
- `"underline"` — underscored
- `"reverse"` — swap foreground and background
- `"strike"` — strikethrough

## Factory theme: SOBRIA

The default theme shipped with arxi-tui is **sobria** — no color, emphasis by
brightening text only, light/dark adaptation handled by the terminal itself
through relative dim/bright attributes (no OSC 11 query). It defines exactly
the tokens the three golden scenes (RAW, SOBRIA, MAXIMUM) reference, and
nothing more:

```json
{
  "dim": { "attrs": ["dim"] },
  "header": { "attrs": ["bold"] },
  "input.placeholder": { "attrs": ["dim"] },
  "banner": { "attrs": ["bold"] }
}
```

`dim` and `bold` are *relative* attributes: the terminal resolves them against
whatever foreground and background it is already using, so `dim` reads as
de-emphasis and `bold` as emphasis on both light and dark backgrounds without
arxi-tui ever querying the background color. There is no OSC 11 round-trip — the
adaptation is the terminal's own, and the theme only says what to emphasize.
(An explicit OSC 11 background query, to drive a light/dark inversion in the
emit layer rather than leaning on the terminal's relative resolution, remains a
possible future enhancement — NEXT.md L4 — but is not what ships.)

## Token resolution at emit time

Frames carry token names in `ui.Span.Style` fields. The terminal emitter
resolves each name to a `ui.Style` (fg/bg/attrs) using the active theme, then
downgrades colors according to the terminal's reported capability profile
(truecolor / 256-color / 16-color / monochrome).

The resolution and downgrade steps are separated:

1. **Resolution:** token name → `ui.Style` (theme lookup)
2. **Downgrade:** `ui.Style` → ANSI escape codes (terminal capability)

Step 1 happens once per token per theme; step 2 happens once per span per
frame. Caching the resolved styles is an optimization the emitter may apply,
but it is not required for correctness.

## Validation rules

### Scene validation (at load time)

Every `"style"` field in a scene document must reference a token defined in the
active theme. A missing token is a validation error with `file:line` pointing
at the offending node.

The validator walks the scene tree, collects every `"style"` value, and checks
each against the theme's key set. The error message names the missing token and
suggests either defining it or checking for typos.

### Theme validation (at load time)

A theme file must parse as valid JSON and satisfy these constraints:

- Top-level object only (no array, no primitives).
- Every value is an object with optional `fg`, `bg`, `attrs` fields.
- `fg` and `bg` must be legal color values (palette name, index 0-255, or hex).
- `attrs` must be an array of legal attribute names.
- Unknown fields in a style definition are ignored (forward compatibility).

An invalid theme file fails at load time with a `file:line` error. The
interface falls back to the factory sobria theme compiled into the binary.

## File locations

Themes are JSON files. The search order is:

1. `--theme <path>` flag (explicit override)
2. `~/.config/arxi-tui/theme.json` (user theme)
3. Factory sobria theme (compiled-in fallback)

A theme file that fails to load logs the error and falls through to the next
location. The factory theme is the backstop: if every user-supplied theme is
broken, the interface still boots with sobria.

## Extension by plugins (Phase 3)

Plugins may define tokens in their manifest (`"tokens": {"profit":
{"fg":"green"}}`). When a plugin is enabled, its tokens are merged into the
active theme. Conflicts are resolved by precedence: user theme > plugin tokens
> factory theme.

Plugin-defined tokens are validated the same way: a scene fragment that
references a token must either define it in the fragment's own `tokens` block
or rely on the host theme providing it. A fragment that assumes a token exists
without declaring it is rejected at plugin-install time.

This is Phase 3 work. Phase 1 implements the resolver and factory theme only.

The manifest field that carries these tokens, and the declarative/behavioral
split that decides when a plugin runs at all, are signed in ADR-0006
(`docs/PLAN.md`, argued in `docs/DESIGN-BLOCK-H.md`); this section is consumed by
H4 unchanged — a `tokens` block is exactly a theme's token block, so the
existing validator checks it and H4 only adds the merge, not a new validator.

## Timing tokens — the `[anim]` vocabulary

Signed 2026-09-22 (D4, `docs/DESIGN-BLOCK-D.md`, owner-accepted). A **timing
token** is a named duration-and-curve, referenced by animation props the way a
style token is referenced by `style`. It answers Scene 4 / Q8 ("timing is a
global `[anim]` token with per-node override") and unblocks the four props
`internal/scene/node.go` parses and warns about today (`transition`, `scroll`,
`reveal`, `enter`); `focus_glow` never needed it because it has no clock.

Timing tokens live in a **separate theme section**, `anim`, so a timing name can
never collide with a style-token name:

```json
"anim": {
  "default":     { "duration_ms": 200, "curve": "ease_out", "fps": 30 },
  "marquee":     { "duration_ms": 0,   "curve": "linear",   "fps": 20 },
  "reveal.fast": { "duration_ms": 120, "curve": "ease_out", "fps": 30 }
}
```

### Shape of a timing definition

- `duration_ms` (int): how long one pass runs. `0` means **continuous** — no
  end, driven at `fps` — which is the marquee case.
- `curve` (name from the closed set below): the easing applied over the run.
- `fps` (int, optional): the tick rate; defaults to a host constant.

### The curve set is closed

`linear`, `ease_in`, `ease_out`, `ease_in_out`, `step`. This is the one D-block
vocabulary deliberately **not** open, and the reason is the plugin boundary: a
style token is pure data the emitter interprets, but a curve is an **easing
function** — code. An open curve namespace would be "bring your own render/timing
code into the mother renderer", exactly what the sparkline rule (Scene 6: the
plugin composes our primitives, it does not bring render code) and Q14 (a render
that is none of our nodes is the Phase-4 wasm ADR) forbid. Adding a curve is a
mother-binary change with its own freeze, like adding a node type.

### Global default + per-node override (Q8)

Every animated prop uses `anim.default` unless the node names a timing token. A
node overrides by naming one: `"transition": { "anim": "reveal.fast" }`,
`"reveal": { "anim": "<token>" }`, `"enter": { "row": true, "stagger":
"<token>" }`. `scroll` keeps its own `speed` (cells per tick) and `pause_when`
(a bind) from SCENES.md and takes its *clock* — the tick rate — from a timing
token (`anim.marquee` by default), so its continuous cadence is defined in one
place rather than reinvented in the renderer.

### The fold never sees this (Q9)

A timing token is consumed only by the render/emit layer that owns the host
clock; it never enters the fold. "The fold never waits on a scene, an animation,
or a plugin" — a timing token *is* the animation, and it stays on the far side
of that line.

### Validation

- A prop naming an `anim` token absent from the active theme → load-time error
  with `file:line`, naming the token and the node — the same net as an undefined
  style token.
- A timing definition with a curve outside the closed set, or a negative
  `duration_ms`/`fps` → theme-load error naming the offending key and listing the
  legal curves. The remedy differs from a style token's on purpose: "choose a
  supported curve", not "define it", because a curve is code the theme cannot
  supply.

When this is implemented, SCENES.md's Scene 4 status paragraph (which says it
"is what should be updated first") and the `Scroll`/`FocusGlow` comments in
`node.go` that cite the missing token move from "warned" to "implemented".

**The clock that consumes these tokens is ADR-0005** (`docs/PLAN.md`, signed
from `docs/DESIGN-BLOCK-G.md` G-A): a timing token is the *duration*, the clock
is the *elapsed time* measured against it. A token defines how long and how
smooth; it does not tick. The per-prop render semantics that turn a phase into
motion are SCENES.md Scene 4 (G-B).

## Signed contract (Phase 1 freeze)

The token format described here is frozen for Phase 1. The three elements —
color values, attribute names, and the theme file schema — may grow in later
phases (new attributes, new color spellings, per-token metadata), but no Phase
1 construction is ever redefined.

Signed: 2026-09-17
