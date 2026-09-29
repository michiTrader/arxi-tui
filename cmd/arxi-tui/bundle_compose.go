package main

import (
	"encoding/json"
	"fmt"

	"github.com/michiTrader/arxi_tui/internal/ext"
	"github.com/michiTrader/arxi_tui/internal/ext/supervisor"
)

// This file is the second half of a bundle install DESIGN-BLOCK-J.md defers after
// the pure artifacts: the grant-then-compose PLAN. resolveBundle (the fetch +
// per-plugin Decide half) stops at the decision; this takes that resolution and the
// user's one answer, fans the answer out to N per-plugin grants via GrantBundle,
// and turns each grant into the supervisor.Config the loop will Start — pairing the
// grant (keyed by plugin ID) back to the Installed package it was laid out into so
// the executable resolves at spawn. It is still pure of the loop: it grants (the
// gate is in-memory and injected) and it plans, but it spawns nothing and mounts
// nothing. That split is deliberate — the security-load-bearing sequence (one
// answer → N grants → the exact set of processes that may spawn, in grant-then-
// compose order) is provable by a counterfactual here, and only the mechanical
// execution (theme.Merge, patch.Mount the scene, supervisor.Start each config +
// register + pump) is left to the loop where the live scene/theme/supervisor state
// lives.
//
// # Why plan-then-execute rather than compose inline in the loop
//
// The design's atomicity is over the workspace: a bundle that cannot be fully
// granted must change nothing (DESIGN-BLOCK-J.md, and GrantBundle's own contract —
// the N Grant calls precede any patch.Mount/theme.Merge). GrantBundle already
// enforces "grant nothing on a rejection or a failed Grant"; producing the whole
// compose plan as one value the loop either gets in full or does not get at all
// carries that same all-or-nothing shape into the loop: the loop composes only
// after this returns a complete plan, so a plan built from a rejected or partially-
// granted bundle never reaches the mount calls. Building the plan inline in the
// select — Start one plugin, then discover the next grant refused — is exactly the
// half-composed workspace the design forbids.

// bundleComposePlan is the complete, granted result a bundle install produces
// before anything is mounted: the behavioral plugins to spawn (each already carrying
// the capability subset GrantBundle produced for its own identity), and the scene
// and theme the bundle ships. The loop executes it in grant-then-compose order —
// theme first, then the scene, then Start each config — but every grant has already
// happened by the time this value exists, so a plan is a workspace change that is
// safe to apply in full or not at all.
//
// scene and theme are the bundle's embedded documents carried through verbatim as
// the raw bytes ParseBundle kept them as: parsing the scene into a *scene.Document
// and the theme into tokens is the loop's job (it owns the live document and the
// ordered token layers), and doing it here would split that ownership. They are the
// zero json.RawMessage when the bundle ships neither, which the loop reads as "this
// bundle changes no scene / no theme" — a plugin-only bundle is legal.
type bundleComposePlan struct {
	// configs is one supervisor.Config per bundle plugin, in bundle order, each with
	// its Granted set already filled from the fan-out. The loop Starts them in this
	// order so a bundle that lists plugins in a deliberate order (a producer before
	// the consumer that reads its frames) mounts them that way. Granted is the gate's
	// word, never the caller's: it comes straight from the BundleGrant, so a config
	// here can carry no power the gate did not return (invariant 7).
	configs []supervisor.Config
	// scene and theme are the bundle's embedded documents, verbatim. Empty means the
	// bundle ships that part not at all — distinct from an empty document — so the
	// loop leaves the corresponding live state untouched rather than blanking it.
	scene json.RawMessage
	theme json.RawMessage
}

// planBundleCompose grants the bundle and builds the compose plan, or returns the
// grant error with nothing planned. It is the join between the two pure artifacts:
// GrantBundle fans the single answer out to N per-plugin grants (all-or-nothing,
// each against the plugin's own identity), and this pairs each returned BundleGrant
// back to the Installed package resolveBundle laid it out into, producing the
// supervisor.Config the loop Starts.
//
// # Why pair by ID rather than by slice position
//
// GrantBundle returns one BundleGrant per decision in decision order, and
// res.installed is index-aligned with res.decisions, so a positional pair would work
// today. It is done by ID anyway because the pairing is a load-bearing identity
// claim — this config will spawn the process at this Root with this granted set —
// and an ID-keyed lookup makes a drift between the two slices a named refusal here
// rather than a plugin spawned at the wrong tree with another plugin's grant. The
// map is built over installed (the packages on disk); a grant naming an ID no
// package was laid out under is a resolveBundle/GrantBundle contract violation, and
// it is refused with the ID rather than silently dropped, because a bundle that
// grants a plugin it cannot spawn is the all-or-nothing rule broken in the caller's
// favour.
//
// On a rejection GrantBundle returns ErrBundleRejected and this returns it
// unwrapped-of-a-plan (nil): the loop reports "you rejected this bundle" and mounts
// nothing, the bundle analogue of the single-plugin ErrConsentRejected path. The
// error is returned rather than a sentinel plan so the loop cannot accidentally
// compose a "rejected" plan by forgetting to check.
func planBundleCompose(res *bundleResolution, gate *ext.Gate, answer ext.BundleAnswer) (*bundleComposePlan, error) {
	grants, err := ext.GrantBundle(gate, res.decisions, answer)
	if err != nil {
		return nil, err
	}

	// Index the laid-out packages by the identity the grant is keyed by. installed
	// holds the Root each executable resolves against; the manifest ID is what a
	// BundleGrant names, so this is the join key between "what the gate granted" and
	// "where the process lives".
	byID := make(map[string]*ext.Installed, len(res.installed))
	for _, inst := range res.installed {
		byID[inst.Manifest.ID] = inst
	}

	configs := make([]supervisor.Config, 0, len(grants))
	for _, g := range grants {
		inst, ok := byID[g.ID]
		if !ok {
			// A grant for a plugin no package was laid out under: the resolution and
			// the fan-out disagree about the bundle's contents. Refuse the whole plan
			// rather than spawn the plugins that did pair — a partially-composed bundle
			// is the dead-binds failure the all-or-nothing rule exists to prevent.
			return nil, fmt.Errorf("bundle %q: granted plugin %q has no laid-out package to spawn; the resolution and the grant fan-out disagree on the bundle's plugins", res.bundle.Name, g.ID)
		}
		configs = append(configs, supervisor.Config{
			Manifest: *inst.Manifest,
			Root:     inst.Root,
			Granted:  g.Granted,
		})
	}

	return &bundleComposePlan{
		configs: configs,
		scene:   res.bundle.Scene,
		theme:   res.bundle.Theme,
	}, nil
}
