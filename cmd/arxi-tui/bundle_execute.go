package main

import (
	"context"
	"fmt"

	"github.com/michiTrader/arxi_tui/internal/ext"
	"github.com/michiTrader/arxi_tui/internal/ext/supervisor"
	"github.com/michiTrader/arxi_tui/internal/patch"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// executeBundleComposePlan is the loop-execution half of a bundle install: the
// mechanical compose planBundleCompose deliberately left to the loop, "where the live
// scene/theme/supervisor state lives" (bundle_compose.go). Every grant has already
// happened by the time a plan exists (GrantBundle ran inside planBundleCompose), so
// this is pure composition — it decides nothing and grants nothing, it only applies
// the workspace change the granted plan describes.
//
// # Order and atomicity
//
// The design's atomicity is over the workspace: a bundle either composes whole or
// not at all. GrantBundle already guaranteed the grant half is all-or-nothing before
// this is reached; this keeps the compose half from tearing by doing every fallible
// step — parsing the embedded theme and scene — BEFORE any process is spawned or any
// live state is mutated. A malformed embedded document (validated at resolve time, so
// this is a defensive check, not the common path) therefore aborts with nothing
// composed rather than after half the plugins are already running. The visible order
// is then the one the plan comment names: theme, then scene, then Start each config —
// so a scene that binds a plugin's tokens sees them, and the plugins come up last
// against the interface that will read their frames.
//
// # The theme layer's key
//
// A bundle is not a plugin, so its embedded theme has no plugin id to key its token
// layer by. It is keyed by a "bundle:"-prefixed name so it cannot collide with a
// plugin id (the manifest id grammar has no colon), which is what a `/ui plugin
// remove <id>` looks its layer up by. There is no `/ui bundle remove` verb yet, so
// this layer is not individually removable today; keying it distinctly is what makes
// that verb a mechanical addition rather than a search for which layer a bundle
// contributed. A re-install of the same bundle replaces the layer by this key rather
// than stacking a second copy, the same add-replaces-by-id property a plugin re-mount
// has.
func executeBundleComposePlan(
	ctx context.Context,
	bundleName string,
	plan *bundleComposePlan,
	doc **scene.Document,
	applyTokens func(*patch.PluginTokens),
	store *ext.PluginStore,
	reg *supervisor.Registry,
	mounted map[string]*supervisor.Supervisor,
) error {
	// Parse both embedded documents first, before touching any live state: a parse
	// failure must abort with nothing composed (workspace atomicity), and the only
	// fallible steps are these two parses — supervisor.Start does not fail
	// synchronously (a spawn failure surfaces on the supervisor's own goroutine as a
	// death, handled by the pump/restart policy).
	var themeLayer *theme.Theme
	if len(plan.theme) > 0 {
		t, err := theme.LoadBytes(bundleName+" (bundle theme)", plan.theme)
		if err != nil {
			return fmt.Errorf("bundle %q: embedded theme is invalid: %w", bundleName, err)
		}
		themeLayer = t
	}
	var newDoc *scene.Document
	if len(plan.scene) > 0 {
		d, err := scene.ParseNamed(bundleName+" (bundle scene)", plan.scene)
		if err != nil {
			return fmt.Errorf("bundle %q: embedded scene is invalid: %w", bundleName, err)
		}
		newDoc = d
	}

	// Theme first: a scene the bundle ships may reference the tokens the bundle
	// ships, so the layer must be live before the document that reads it is walked.
	// The layer is empty-not-nil so applyTokens merges a real (possibly no-op) layer
	// rather than nil-checking, matching the H4 plugin-token contract.
	if themeLayer != nil {
		applyTokens(&patch.PluginTokens{ID: "bundle:" + bundleName, Theme: themeLayer})
	}

	// Then the scene: it replaces the live document outright, because a bundle scene
	// is a complete interface the bundle ships, not a fragment mounted at a `where`
	// (that is `/ui plugin add`'s declarative path). A bundle that ships no scene
	// leaves the current document untouched — a plugin-only or theme-only bundle
	// changes what it changes and nothing more.
	if newDoc != nil {
		*doc = newDoc
	}

	// Then the plugins, last, against the interface that reads their frames. Each
	// config already carries only the gate's granted set (planBundleCompose filled
	// it from the fan-out), so Start hands the child exactly that power (invariant 7).
	// This is the same Start→Add→pump sequence supervisor.Mount runs for a single
	// behavioral plugin, minus the decide/grant it already did: the grant is the
	// plan's, so here it is pure spawn-and-wire.
	for _, cfg := range plan.configs {
		id := cfg.Manifest.ID
		s := supervisor.Start(ctx, cfg)
		reg.Add(id, s)
		mounted[id] = s
		// The pump blocks until the process dies or is Closed, so it runs on its own
		// goroutine — the loop stays free (I3). On a clean Close it DropPlugins the
		// namespace; removing from the routing table on exit closes the window
		// between a process death and the user unmounting it, exactly as
		// supervisor.Mount does.
		go func(id string, s *supervisor.Supervisor) {
			s.DrainInto(store)
			reg.Remove(id)
		}(id, s)
	}
	return nil
}
