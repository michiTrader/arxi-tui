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

<!-- APPEND-MARKER-1 -->
