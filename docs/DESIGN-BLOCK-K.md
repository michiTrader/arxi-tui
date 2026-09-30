# Block K — Phase 4 behavior and the embedded-wasm escape hatch (proposal)

This document drafts the paper decision Block K's **K4** needs: the *costed* ADR
on an embedded WebAssembly runtime for "a render that is none of our nodes"
(Q14, `docs/SCENES.md`; the escape-hatch row of the when-data-when-code table,
`PLAN.md:561`). It follows the on-paper method of Blocks D, G, H and J: argue the
decision in full here, then sign the durable seam as an ADR in `PLAN.md`.

**Scope.** This file signs **K4 only**. K1–K3 (Gate C behavior hooks, Gate D
providers, self-extension over the opened gates) are the *behavior* half of
Phase 4 and are deliberately left unstarted: each needs a live agent and the
coordinated arxi-core surface this host cannot invent, and none of them is what
Q14 asks. K4 is separable because it decides a *render* seam, not a behavior
gate — it touches the engine's frame contract (ADR-0004), not the tool/hook/
provider doors — so it can be costed and frozen now without waiting on the live
loop the rest of K needs.

**Status: proposal, awaiting signature.** Signing this lifts no code guard and
adds no dependency to `go.mod`; it freezes *how* a wasm render will land and
*why it does not land yet*, so that when the first non-expressible render
appears it is built against a fixed decision rather than one invented under the
pressure of a waiting plugin author — the same discipline ADR-0007 applied to
the behavioral wire (freeze it before a consumer depends on it).

## What Q14 actually asks, and what the honest ceiling is today

Q14 is the expressiveness escape hatch: a plugin author wants a render the
mother system has no node for — a Braille canvas, a candlestick chart, a
sixel image, a layout no composition of `text`/`row`/`overlay`/`sparkline`
can express. Every scene the project has frozen so far (`docs/SCENES.md`, all
eleven) is expressible in nodes, and the two "graph" renders that arrived —
the marquee and the `sparkline` — landed as *mother-system nodes* precisely
because they were expressible: the plugin composes our primitives, it does not
bring render code (SCENES.md Scene 6). That is the rule Q14 lives at the edge
of: **if it can be said as a node, it is said as a node** (`PLAN.md:563`), and
a new node type is cheaper, safer and more rewritable than foreign render code
every time it is possible.

So the honest ceiling today is our node vocabulary, and SCENES.md states it
outright: "until that lands the ceiling is honestly our node [set]"
(`SCENES.md:12`). K4 does not raise that ceiling — it decides what happens the
first time a render genuinely cannot be said as a node, and it makes that
decision now, while no such render exists, so the answer is a signed seam
rather than an improvisation.

## The decision: freeze the seam, defer the runtime

**Adopted: wazero is the runtime, the render contract is pull-by-frame, and the
runtime is not embedded until the first non-expressible render exists.** The
three parts are separable and each is argued below.

### Why wazero, and why the cost is affordable

`wazero` is the Bytecode Alliance's WebAssembly runtime for Go, and its single
decisive property for this project is that it is **pure Go with zero CGO** —
which is not a preference here, it is the install rule (`PLAN.md:116`): the
product ships static `CGO_ENABLED=0` binaries per platform, Termux included,
and any runtime that needed a C toolchain on the build host or a shared library
on the user's machine would violate "the user installs arxi, not arxi's
dependencies" (`PLAN.md:112`). A wasm interpreter that itself broke the static
build would be self-defeating: the escape hatch would cost the install
guarantee it exists to protect.

Measured on this host (go 1.26.5, `CGO_ENABLED=0`), against wazero v1.12.0:

| build | size | note |
|---|---|---|
| empty `main` (windows/amd64) | 1,787,904 B | baseline |
| empty `main` + wazero compile | 6,049,792 B | **+4.06 MiB** marginal |
| same, `GOOS=android GOARCH=arm64` | 6,328,401 B | Termux target, CGO-free |
| same, `GOOS=linux GOARCH=arm64` | 5,742,022 B | CGO-free |

Two facts fall out of the measurement and are the reason the ADR can be signed
rather than deferred for more study. First, wazero **cross-compiles to every
install target including android/arm64 with `CGO_ENABLED=0`** — the property
the install rule requires, confirmed by building it, not asserted. Second, its
only non-stdlib module dependency is `golang.org/x/sys`, which arxi already
carries as an indirect dependency — so embedding wazero adds exactly one new
third-party module (wazero itself), a cost this project's dependency rule
requires be *named* but does not forbid (`PLAN.md`, "dependencies are allowed,
but named").

The ~4 MiB is the whole reason the runtime is **deferred, not embedded now**:
it is a real cost (arxi's own binary is ~10.8 MiB, so wazero is a ~35–40 %
increase) paid against *zero* current consumers, because no non-expressible
render exists. Embedding it today would enlarge every platform artifact and
widen the threat surface (a sandbox to audit, a frame contract to test) for a
capability nothing uses. The decision that is cheap and load-bearing now is the
*seam*; the decision that should wait for a real consumer is the *dependency*.

### The render contract: pull-by-frame, propose-never-write

A wasm render plugin is bound by the same two invariants every other plugin is,
and the contract is chosen so the wasm case cannot be the exception that breaks
them.

**It is pulled once per frame, never a pushing renderer (ADR-0004).** The host
calls the module's single exported render function per repaint with an
immutable snapshot — the same `PluginValues` view-state snapshot the behavioral
wire already feeds the renderer (ADR-0007), plus the frame's width/height — and
receives back a **description of cells**: spans with text and style-token names,
in the same shape `ui.Frame` already carries, never an imperative sequence of
terminal writes. The module returns a frame fragment; it does not drive the
terminal. This is the exact mechanism the `sparkline` node already embodies at
the Go level (`internal/engine/sparkline.go`: read one bind snapshot, return one
`ui.Frame` line), lifted across the wasm boundary — which is why the seam is
expressible today even though the runtime is not present: the host side is the
render walk that already exists.

**It proposes a frame, it never writes host state (invariant: plugins
propose).** The module's output is data the host validates and composes, exactly
like a mounted scene fragment or a streamed bind value: style-token names it
does not own resolve through the same `user > plugin > factory` cascade, an
unknown token is the same warning an unknown property is (`vocabulary.go`), and
a returned span cannot forge a run event because the render path never touches
the fold (invariant 2). A wasm render is a *view*, and a view has no authority
over content — the same wall ADR-0003 builds between `PluginValues` and
`fold.State`.

**It cannot capture the escape hatch (invariant 6).** The module is called from
inside the render walk, which is downstream of the loop `select`; it never sees
a key event and has no path to the input decoder, so double-Ctrl-C and
`-scene ""` are as untouchable for a wasm render as they are for the animation
clock or the plugin store — all three are reasons to repaint or things to draw,
never gestures. wazero's sandbox reinforces this by default: a module gets no
filesystem, no network and no clock unless the host grants it through WASI, so
the *capability* grant (the I5 consent gate) is the only door in, and a render
module declares no capabilities at all.

### Why the runtime waits, stated as the rule it will be judged by

The runtime lands the first time a scene needs a render that **cannot be
expressed as a node type**, and the discipline that keeps this honest is the
one that produced the marquee and the sparkline as nodes: before a render is
declared non-expressible, the node route must have been tried and shown
insufficient, because a node is rewritable by the user and foreign render code
is not (the arxi-sim closed-vocabulary mistake, ADR-0003 dogfooding). The ADR's
test, when the runtime arrives, is a real render that fails that bar — not a
render an author preferred to write in wasm. Until then the ceiling stays the
node set, said out loud, and the escape hatch stays a signed cost with no
process behind it.

