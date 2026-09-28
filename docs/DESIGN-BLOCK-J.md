# Block J — Phase 3 the community installer as a scene (proposal, awaiting signature)

This document drafts the paper decisions Block J needs before J1–J5 can be
implemented: **preview mode** (J1, the engine contract that renders a
not-yet-installed plugin's binds as placeholders) and **the server-less registry**
(J2, a JSON index in a git repo). It follows the on-paper method of Blocks D, G
and H and the dogfooding decision of ADR-0003 (the change-diff view): the
installer is authored from node types the engine already renders, never bespoke
chrome the user cannot rewrite.

**Status: proposal, awaiting signature.** Signing the design lifts no code guard;
J1–J5 land with their own counterfactual tests. This file is the argued record
behind the signatures.

## What Scene 7 and the frozen docs already require

Scene 7 (`docs/SCENES.md`, the installer) fixes four hard requirements: the
installer **is** a scene — a `list` of registry entries, a `markdown` preview
pane, a search `input`, `i` to install; previewing a stranger's scene needs the
engine to render unsatisfied binds as **placeholders** (Q16 preview mode is part
of the engine contract); the registry is a **JSON index in a repo, no servers**
(Q17); and full bundle sharing (scene + theme + plugins, one consent screen)
rides on the same gate.

Scene 6 (the ticker) is the precedent: Block H pins its golden at the
**mounted-but-unsatisfied** state where `tick.price` renders as its declared
`mock`. J1 generalises that exact mechanism from one plugin's `tick.*` to a
previewed stranger's binds.

The load-bearing contracts J1 consumes unchanged:

- **The counter-field rule** (`BINDS.md`, `PLAN.md:319-321`): an unsatisfied bind
  renders as a placeholder, never a crash — the engine contract that makes
  community preview and forward compatibility possible at once.
- **The declared-vs-used rule** (`BINDS.md` §4.4): a `<plugin-id>.<field>` bind
  resolves iff `<field>` is a key of that manifest's `binds` map; the manifest's
  `binds` map is a plugin's schema the way a source list's `RowSchema` is a
  template's schema.
- **ADR-0003 dogfooding** (`PLAN.md:509-548`): a view drawn by bespoke engine
  code "is chrome the user cannot rewrite, which is the arxi-sim closed-vocabulary
  mistake in a new place." The installer must be a host-generated scene.

<!-- APPEND-MARKER-1 -->

## What the code provides today (the hooks J1/J2 attach to)

- **`BindDecl.Mock`** (`internal/ext/manifest.go:101-108`): raw JSON, keyed in
  `Manifest.Binds` by the **full** bind path; its doc comment names it "the
  placeholder the preview path renders before a frame arrives, read by Block J."
  `Kind` is closed at `text`/`series`. A `text` mock is a JSON string; a `series`
  mock is a JSON array. Note `binds` is a behavioral field, so a mock-bearing
  manifest fully loads only once Block I lifts the executable refusal — but for
  preview the manifest is **parsed and its `binds` map read without spawning any
  process** (`ext.Parse`/`ParseNamed`, `manifest.go:112-129`).
- **`resolveBind` and the placeholder** (`internal/engine/render.go`):
  `resolveBind(bind, state)` is a `switch` over signed host binds whose `default`
  case returns `placeholderValue = "[…]"`, named (not repeated) so `evalWhen` can
  tell "no value" from "the value is this text." Any `<plugin-id>.*` bind hits the
  default today and draws the placeholder — it never crashes. `resolveBindRow`
  is the existing precedent for "extra resolution context changes how a bind
  resolves": it special-cases the `row.` prefix and delegates the rest.
- **The Renderer's host-computed inputs** (`render.go`): `curRow`, `AnimTicks`,
  `AnimPhase`, `ChatScroll` are all deliberately separate from `fold.State`
  because "the fold projects content, never motion," and `child()` propagates
  every one into sub-renderers so "some carry it and others do not" is
  unrepresentable. This is the established pattern a new render input follows.
- **`theme.Merge`** (`internal/theme/theme.go`, H4): layers `over` on `base`,
  fresh theme, mutates neither; precedence is the caller's layering order
  `Merge(Merge(factory, plugin), user)`. **`patch.Mount`** (`internal/patch/mount.go`)
  composes fragments, prefixes ids, re-validates. **`Diff.Scene`**
  (`internal/patch/diff.go`) is the concrete template for a host-generated scene:
  build `map[string]any` nodes, marshal, re-parse through `scene.ParseNamed`.

## J1 — preview mode (BUILT PR #101)

**BUILT at the recommended defaults (forks 1 and 3 below).** The engine half is a
`Renderer.PreviewMocks map[string]string` input consulted in `resolveBindRow`
after the live plugin snapshot and before the fold, threaded through
`evalWhenRow`/`hiddenByWhenRow` and propagated by `child()`; the ext half is
`Manifest.PreviewMocks()`, the projection over the parsed manifest's `binds` mocks
that builds the table. Both landed with the counterfactuals named at the end of
this section. The overall Block J signature remains the owner's to record; J1 is
purely additive (a nil table is byte-identical to today), so it lands ahead of
that signature the way the §I-I installer did.

**PROPOSAL: preview mode is a new render input on the Renderer, mirroring
`AnimPhase`/`ChatScroll`, not a `fold.State` field.** The mock table is
host-computed view state — it comes from a parsed manifest the host holds, not
from the run log the fold folds. Putting it on `fold.State` would breach
ADR-0004's "the fold projects content" seam the same way motion would. It is the
direct analogue of `curRow`: extra resolution context that changes how one
namespace resolves, shared by a whole subtree.

Proposed shape:

```go
// PreviewMocks, when non-nil, is the preview-mode substitution table: a map
// from a fully-qualified <plugin-id>.<field> bind to the string the manifest's
// binds[<field>].mock declares. It is host-computed view state fed in per
// repaint like AnimPhase — the fold projects content, preview projects a
// not-yet-installed plugin's declared placeholders (Q16). nil (every golden not
// about preview, and every installed/live scene) means no substitution: an
// unsatisfied bind falls to placeholderValue exactly as today.
PreviewMocks map[string]string
```

**Hook point: bind resolution's placeholder path.** Today `resolveBind`'s
`default` returns `placeholderValue`. PROPOSAL: route preview through a Renderer
method that wraps `resolveBind` the way `resolveBindRow` does — if
`r.PreviewMocks != nil` and the composed bind is present in the table, return the
mock; otherwise delegate to `resolveBind`, which still returns `"[…]"` on the
default. This preserves three guarded properties:

1. a nil map is **byte-identical to today** (the `AnimPhase`-nil discipline);
2. a bind with no mock still draws `"[…]"` — never a crash (the counter-field
   rule);
3. `evalWhen` still sees a real string for a mock and `placeholderValue` for a
   true miss, so preview does not accidentally flip a `when` gate.

**Building the table.** When previewing entry E, the host parses E's manifest
with `ext.Parse` (not `LoadFile` — a behavioral manifest is refused at validate
today), then for each `field, decl` in `m.Binds` sets
`PreviewMocks["<m.ID>.<field>"] = renderMock(decl)`. `renderMock` converts
`decl.Mock` (raw JSON) to the display string: a JSON string → its text, a
`series` array → the same form `resolveBind` yields for a series. Because `Kind`
is closed at `text`/`series`, this is a two-arm switch, extended per kind as a
signed change. **Propagation:** add `PreviewMocks` to the fields `child()` copies,
so a nested preview (an overlay fragment) resolves mocks identically to the root.

**The `community.*` vs `<plugin-id>.*` question (resolved here).** SCENES.md
names the preview namespace `community.*`, but the manifest declares
`<plugin-id>.*`, and the mounted fragment binds `<plugin-id>.*`. PROPOSAL: treat
`community.*` in SCENES.md as the **conceptual** label for "the previewed
stranger's binds"; the concrete keys are `<entry-plugin-id>.<field>`, since that
is what the manifest's `binds` map and the fragment actually use. Do **not**
introduce a real second namespace — a preview substitutes the previewed plugin's
own binds.

**Golden note (feeds J5):** pin a Scene 7 preview frame where a previewed plugin's
bind draws its manifest mock, plus a second frame with `PreviewMocks == nil`
proving the byte-identical no-op — the SOBRIA-zero-row trap in AGENTS.md is the
reason both directions are pinned.

## J2 — the server-less registry (BUILT PR #102)

**BUILT as `internal/ext/registry.go`.** The loader mirrors `manifest.go`:
`ParseRegistry`/`ParseRegistryNamed` + `Registry.Validate()` with the same
addressed `*Error`, and a closed-set `version` (`legalRegistryVersions`, the index
analogue of `legalProtocols`). `Validate` never fetches — it is a pure function
over the index bytes, refusing an unknown version and, per entry, a missing
identity or a `manifest_url` that is absent or not HTTPS. `FetchRegistry` is the
one network seam, HTTPS-only at the index URL too, with the HTTP client injectable
so the fetch path is tested against `httptest.NewTLSServer` without the real
network. Forks 2 (inline `preview`) and the security note below landed at their
recommended defaults; the overall Block J signature remains the owner's, so J2
lands ahead of it the way J1 and the §I-I installer did.

**PROPOSAL: a single JSON file committed to a git repo, fetched over HTTPS (a raw
file URL), no server.** Shape:

```json
{
  "version": "reg/v1",
  "entries": [
    {
      "id": "tick",
      "name": "Ticker",
      "version": "0.2.0",
      "manifest_url": "https://raw.githubusercontent.com/…/tick/manifest.json",
      "description": "A top-right price ticker.",
      "preview": "# Ticker\n\nStreams a sparkline…"
    }
  ]
}
```

Field rationale, each tied to an existing contract:

- `version` — a closed-set index-schema version, mirroring the manifest's
  `protocol` closed set (`manifest.go:37-42`): unknown is refused, not
  negotiated. PROPOSAL token `reg/v1`.
- `id`, `name`, `version` — the entry's identity; must **equal** the fetched
  manifest's `id`/`name`/`version` (`manifest.go:57-61`), `id` matching
  `idPattern`. `name`/`version` are what the consent screen shows.
- `manifest_url` — where the full manifest is fetched on install; validated by
  the same `ext.Parse`+`Validate` install uses, no second parser.
- `description` — the one-line `list` row label.
- `preview` — markdown for the preview pane; kept **inline** in the index so the
  browse view renders with one fetch. The full manifest is fetched only on `i`
  (install) or when entering preview mode (to read `binds[...].mock`).

**Fetch/validate flow, reusing existing validators (no new ones):**

1. fetch the index JSON; refuse an unknown `version` (closed-set check);
2. on preview: fetch `manifest_url`, `ext.Parse` it, read `m.Binds` for the mock
   table (J1); cross-check index `id`/`version` against the manifest — a mismatch
   is a refusal (identity is consent identity);
3. on install (`i`): validate via `ext.LoadFile`-equivalent, then `patch.Mount`
   exactly as `/ui plugin add <url>` does (H6); the consent gate (I5) sits before
   mount. **No new install path — the registry only *discovers* URLs that flow
   into the existing H6/I5 pipeline.**

**Security note (flagged).** `manifest_url` is attacker-controlled data from a
public index. It must pass the full `Validate` and, for a behavioral entry, the
Q15 consent gate (identity: name/version/executable/args/capabilities/digest —
see DESIGN-BLOCK-I.md I-H) before any process spawns. The registry is discovery
only; it grants nothing. The escape hatch (invariant 6) is the backstop for a
hostile previewed scene.

## J3 — the installer authored from existing node types only

**BUILT (2026-09-28, PR #103) — the pure scene function; live interactivity is a
follow-up.** `Registry.InstallerScene() (*scene.Document, error)` in
`internal/ext/registry_scene.go` builds the installer as a `map[string]any`,
marshals it, and re-parses through `scene.ParseNamed`, exactly as `Diff.Scene`
does. It is the pure `index -> *scene.Document` this section names as J3's
testable core, held by three guards each proven load-bearing by a counterfactual
run by hand: it validates under both themes; every entry becomes a node carrying
both an `id` (`install:<id>`) and `on_press` `cmd:/ui plugin add <manifest_url>`
(the pair `press.go` requires to ring a node, so a card is installable, not just
drawn); and every entry's name, description and preview reach the screen. Install
is H8's `cmd:` action through the existing H6 path — the registry discovers a URL
and grants nothing new. **One deviation from the recommended default below is
deliberate and recorded in the file:** the default wants a live `list` bind + a
query-bound search `input`, which needs a new signed `community.*` array bind, a
row schema, a fold field and host-loop filtering — the interactive half this
section itself flags as H8-and-beyond. The build follows the *primary*
testability requirement instead ("a pure `index -> *scene.Document`, testable by
a golden the way `Diff.Scene` is", and `Diff.Scene` bakes), so entries are baked
as static pressable cards; the search input is laid out but not yet wired, and a
selection-driven preview pane is the follow-up. Baking forecloses neither.

**Follow-up increment (2026-09-28, PR #106) — the filter core.**
`Registry.FilterEntries(query)` (`internal/ext/registry.go`) is the pure
query-filter the live search box will call on every keystroke: empty query
returns every entry (browse open, unfiltered), a non-empty query keeps entries
whose name or description contains it as a case-insensitive substring, and a true
miss returns a nil slice so "no results" and "not yet filtered" never collapse.
It is a faithful port of `fold.FilterSlashMatches` (port, do not invent) with one
recorded domain adaptation: the slash menu matches the command name alone, while
a registry browse searches the two human-readable fields the card renders (name
and description); the `id` and `manifest_url` are deliberately not matched, since
they are not text the user reads off the card. Five counterfactuals were run by
hand (the description clause, both `ToLower`s, empty→all, and nil-not-all each
reverted and re-run to confirm the guarding test fails). This is the pure half;
signing the `community.*` view-state bind, the fold field that carries the
filtered result, and the host keystroke loop that feeds it remain the deferred
live half below.

**Follow-up increment (2026-09-28, PR #107) — the `community.*` view state
signed.** The vocabulary the live `list`/search-input pair reads is now committed
on paper before any fold field or loop depends on it, the way this block signed
J1/J2 before wiring them: `community.query` (the search substring), `community.matches`
(the filtered entries, an array-of-objects with a signed row schema
`row.id/name/version/manifest_url/description/preview`), and `community.selected`
(the highlighted card the preview pane renders), all in the `slash.*` mould
(BINDS.md §4.3 and the §4.7 row schema). They are host view state written by the
installer keystroke loop, never an arxi-core event, so a stranger's registry can
never author them; and `community.matches` is the previewed plugin's own entries,
**not** the `<plugin-id>.*` preview namespace J1 defines (that namespace carries a
previewed manifest's mocked binds, not the browse list). `validate.go` carries
them in `signedBinds`/`rowSchemas` so the `InstallerScene` builder and the
validator agree. They are signed-but-not-projected on the record
(`acceptedUnprojectedBinds`, `pulseBindsWithoutFoldFields`, and BINDS.md §4.6),
the way `ui.hidden` was before F3 gave it `State.UIHidden`; every empty-state is a
no-op, so no golden moved. **The deferred live half below is now exactly:** the
fold fields, the keystroke loop that recomputes `community.matches` from
`community.query` via `FilterEntries`, and the selection→preview pane — all gated
on the same live-loop surface the I5 modal mount uses.

**Follow-up increment (2026-09-28) — the fold fields and engine projection.**
The first live-half increment, unblocked now that the I6 modal loop
(I6-install-modal / I6-store-render) has landed the live-loop surface the signing
was gated on. `fold.State` gains `CommunityQuery`, `CommunityMatches`
(`[]CommunityMatch`, a fold-local row struct mirroring `ext.RegistryEntry`
field-for-field but declared in `fold` so the pure host-owned fold ADR-0002
requires never imports the registry's HTTP/parse surface — the host converts an
`ext.RegistryEntry` into a `CommunityMatch` at the boundary, the same seam
`SlashMatch` keeps from the command registry) and `CommunitySelected`.
`resolveBind` projects the two scalars and `rowScopesFor` instantiates the
`row_template` over `community.matches`, so the three binds graduate out of
`acceptedUnprojectedBinds`/`pulseBindsWithoutFoldFields` (and `community.matches`
into `templateProjectedBinds`) exactly as `ui.hidden` did at F3. Both directions
were measured, not argued: a constant `community.query` fails the scalar
projection-varies guard, and a `rowScopesFor` that ignores `state.CommunityMatches`
fails the composite one. No shipped scene binds `community.*` — `InstallerScene`
still bakes static cards — so the empty fields move no golden. **The deferred
live half is now just:** the keystroke loop that writes these fields via
`FilterEntries` and rebuilds `InstallerScene` to the live `list`/search pair
below, and the selection→preview pane — the parts that touch the real-tty loop.

**Follow-up increment (2026-09-28) — the live installer document.** The next
increment after the projection landed, and the pure/golden-testable half of
"rebuilds `InstallerScene` to the live `list`/search pair below": `ext.Live­In­staller­Scene()`
builds the installer as a *live* document — the search `input` binds to
`community.query`, the entry `list` is a `row_template` over `community.matches`
(`row.name`/`row.version`/`row.description` drawn, the URL and preview carried by
the scope for the loop and the preview pane) — instead of baking static cards.
It takes no index because the entries now live in the fold, written by the
keystroke loop from `FilterEntries`; the structure is therefore a pure constant
and the folded matches are the content (ADR-0002). It is pinned before the loop
mounts it, the `TICKER.json`/`COMMUNITY.json` order: `testdata/COMMUNITY-LIVE.json`
is byte-for-byte the builder output, and `.frame`/`.styled` pin the frame over a
**non-empty** folded state — the coverage this increment adds, since nothing
rendered `community.matches` into a frame before. Both directions measured: a
`rowScopesFor` that ranges an empty slice empties the list and
`TestLiveInstallerDrawsEveryMatch` fails on the first entry. Three affordances
are deliberately deferred, each named for the engine capability it needs: the
search input shows its placeholder rather than the query (`renderInput` draws a
bound value only for `user.input`), the right column stays the static help pane
(a selection-driven preview needs new signed `community.selected.*` absolute
binds), and there is no selection highlight (a `row_template` carries no row
index today). When the keystroke loop lands and mounts this document, the Scene 7
golden moves from the static build to this one in its own mutation family, and
the static `InstallerScene` retires the way `ui.hidden`'s exemption did once its
consumer existed.


**Follow-up increment (2026-09-28) — the search input shows the typed query.**
The first of the three affordances above, landed as its own engine change:
`renderInput` resolved a bound field's value only when `n.Bind == "user.input"`,
so the installer's search box — bound to `community.query` — drew its placeholder
even with a query folded. It now resolves whatever bind the input carries through
`resolveBind`. `user.input` keeps its live `UserInputCaret`; every other
view-state bind rests the caret at the end of the resolved text, because the fold
holds no caret index for it yet — that is the keystroke loop's to add. A resolved
`placeholderValue` collapses to the empty line, so an unresolved bind still shows
its hint and never draws `[…]`. The `COMMUNITY-LIVE` frame goldens move from the
placeholder to the folded query `tick`. `TestLiveInstallerSearchInputShowsThe­Query`
drives `community.query` with a value in no row and asserts both directions (the
query shows; an empty query restores the placeholder), so it fails on the
pre-change engine; the counterfactual — neutering the view-state branch to draw
no value — returns the placeholder and fails both that witness and
`TestLiveInstallerDrawsEveryMatch`'s placeholder-absence check. **Deferred after
this:** the `community.selected.*` preview pane (new signed absolute binds) and
the row-selection highlight (a `row_template` carries no row index today), plus
the keystroke loop that writes the fold fields via `FilterEntries`.


**PROPOSAL, under ADR-0003, copying the `Diff.Scene` construction pattern.** The
installer is a document the host **generates from the fetched index** — built as
`map[string]any`, marshalled, re-parsed through `scene.ParseNamed`, exactly as the
diff scene is. No new node type. Composition uses only `list`/`markdown`/`input`/
`overlay` plus `row`/`stack`/`text` (all signed):

- a `row` split into two `stack`s (the diff-view shape):
  - left `stack`: a search `input` (bound to a host view field for the query)
    above a `list` whose `bind` is the registry entries and whose `row_template`
    renders `row.name` + `row.description` per entry (the `row.*` mechanism, with
    an element schema declared like the existing `slash.matches` row);
  - right `stack`: a `markdown` node rendering the selected entry's `preview`;
- **`i` to install and search filtering are `on_press`/action routing — H8, and
  the closed action vocabulary (Q18).** J3 therefore depends on J1+J2 **and** on
  H8's action dispatch (still refused today). Flag this dependency: J3 cannot
  ship its interactivity before H8.
- a full-screen preview of a stranger's whole scene is an `overlay` (the type
  "that already exists for exactly this"), which cannot capture the escape hatch
  (invariant 6). Selecting a stranger entry sets `Renderer.PreviewMocks` (J1) from
  that entry's manifest, so any `<plugin-id>.*` bind in the previewed fragment
  draws its mock rather than `"[…]"`.

The registry-index → scene function is a pure `index -> *scene.Document`, testable
by a golden the way `Diff.Scene` is — that golden is J5.

## J4 — bundle sharing

A bundle is scene + theme + plugins under one manifest, one consent screen. The
theme half reuses `theme.Merge` at the signed precedence
`Merge(Merge(factory, plugin), user)` (H4) — a bundle's tokens are "plugin"-layer
tokens, already validated by `theme.LoadBytes` the same as a manifest's `tokens`
block. The plugin half reuses `patch.Mount`. "One consent screen" is a UX
aggregation over the existing Q15 gate (one identity grant covering all the
bundle's components), **not a new gate**. J4 is composition of J1–J3 plus I5; no
new merge or mount primitive is required.

### The bundle manifest schema (`bundle/v1`)

The one new artifact J4 adds is the bundle manifest itself: the document that
names, in one place, the curated interface a share carries. It is JSON for the
same three reasons the scene, the plugin manifest and the registry index are —
a model writes it without hallucinating, every language parses it, and it embeds
the scene, token block and plugin references that are already JSON here. It
mirrors `registry.go`/`manifest.go` deliberately (closed-set `version`, an
addressed `*Error`, a pure `Validate` that never fetches), so a bundle refusal
carries `file:line` for free and cannot drift from a manifest refusal on what an
address looks like.

Fields:

- `version` (string, required) — the closed-set schema tag, `bundle/v1`. Closed
  because the bundle shape is code on both sides, exactly as `legalProtocols`
  and `legalRegistryVersions` are: an unknown version is refused at load, never
  negotiated.
- `name` (string, required) — the human-facing label the one consent screen
  shows. Consent to an unnamed bundle is consent the user could not read, the
  same argument the manifest's required `name` makes.
- `description` (string, required) — the one-line summary of what the bundle
  installs. A bundle with no description is one the user accepts blind, the same
  argument the registry entry's required `description` makes.
- `scene` (object, optional) — an embedded scene document (a `root` node tree),
  the interface the bundle ships. Validated through `scene.ParseNamed` +
  `Validate`, the same net a hand-written document gets, with its address rebased
  onto the bundle bytes so a refusal points at the bundle line the author wrote.
- `theme` (object, optional) — an embedded token block, exactly the shape a
  plugin manifest's `tokens` block and a theme file have. Validated through
  `theme.LoadBytes` — the one token validator — and merged at the signed
  `plugin`-layer precedence by the deferred wiring.
- `plugins` (array, optional) — the plugins the bundle needs, each a
  `{ "manifest_url": "https://…" }` reference. HTTPS-only, the same security
  refusal `registry.go` makes and for the same reason: a bundle is
  attacker-controlled data, and a plaintext or `file://` URL would let it
  redirect an install to swapped code or a local path. **The references are
  discovery, not embedded code** — the plugin half of the install still flows
  through the existing H6/I5 pipeline (fetch each `manifest_url`, full manifest
  `Validate`, the Q15 consent gate) exactly as `/ui plugin add` does; the bundle
  grants nothing the install pipeline does not already gate.

`checkEmpty`, ported from the manifest: a bundle must contribute at least one of
`scene`, `theme` or `plugins`. A bundle that bundles nothing is a grant that
bought nothing — the user shared something and the workspace did not change,
indistinguishable from a broken load.

### What lands now vs. what is deferred (the J-block pattern)

J4 lands its **pure, fully-testable core first** — the bundle manifest parser and
validator (`internal/ext/bundle.go`), a pure `bytes -> validated *Bundle` unit
with counterfactual tests — exactly as J2 landed `ParseRegistry` and J3 landed
the pure `InstallerScene` before their live wiring. The **loop wiring** — fetch
each referenced manifest, compute the aggregated Q15 identity, show the *one*
consent screen (`ext.ConsentScene`'s bundle sibling), then `theme.Merge` the
tokens and `patch.Mount` the scene + plugins on a single grant — is the follow-up
increment, gated on the same live-loop surface the I5 modal mount uses. Splitting
here keeps the security-load-bearing decision (what a bundle may name, and that
every plugin reference is HTTPS) pinned by a counterfactual now, and leaves the
aggregation as a small, testable addition once the modal loop holds it.

### Open forks for the owner (each resolved to a recommended default above)

1. **`scene`/`theme` embedded vs referenced by URL** — recommend embedded: a
   bundle is a single shareable document, and a scene fetched from a second URL
   reintroduces the man-in-the-middle window the HTTPS-only plugin rule closes,
   for the one component the consent screen renders verbatim.
2. **`plugins` embedded manifests vs `manifest_url` references** — recommend
   references: the plugin half must flow through the H6/I5 install pipeline
   unchanged (fetch, `Validate`, consent), and embedding a manifest would fork
   that path into a second, drift-prone copy. The bundle discovers URLs; it
   grants nothing.
3. **One consent screen as aggregation vs a new gate** — recommend aggregation
   (as the section states): one identity grant covering the bundle's components,
   over the existing Q15 gate, never a new authority.

## J5 — freeze the Scene 7 golden

Two frames per the J1 golden note: a preview frame where a previewed plugin's
bind draws its manifest mock, and a `PreviewMocks == nil` frame proving the
byte-identical no-op. The installer scene itself is pinned as the output of the
pure `index -> *scene.Document` function, the way every host-generated scene is.

## Open forks for the owner (each resolved to a recommended default above)

1. **`community.*` as a real namespace vs a conceptual label** — recommend
   conceptual (J1): preview substitutes the previewed plugin's own
   `<plugin-id>.*` binds; no second namespace is minted.
2. **`preview` markdown inline in the index vs fetched per entry** — recommend
   inline (J2): the browse view renders with one fetch.
3. **Preview mode as a Renderer input vs a `fold.State` field** — recommend a
   Renderer input (J1): it is host-computed view state, like `AnimPhase`.

## Dependency notes carried into implementation

- J1 is the only part with no hard prerequisite beyond the engine as it stands —
  it can land ahead of Block I.
- J3's interactivity (`i`, search) depends on **H8** (`on_press` action routing),
  not only on J1/J2.
- J4 depends on **I5** (the consent gate) for its "one consent screen."

Signing this proposal lifts no code guard. J1 (preview input), J2 (registry
loader), J3 (installer scene function), J4 (bundle) and J5 (golden) each land with
their own counterfactual test.

