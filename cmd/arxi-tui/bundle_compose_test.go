package main

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/ext"
)

// This file covers planBundleCompose, the grant-then-compose planning half of a
// bundle install. GrantBundle's fan-out and BundleConsentScene's presentation are
// proven in internal/ext; resolveBundle's fetch+Decide is proven in
// bundle_install_test.go. The concern here is the join: that the single answer
// grants and produces exactly the supervisor.Configs the loop Starts, each paired to
// its own package and carrying only the gate's granted set; that a rejection plans
// nothing; that a remembered plugin is carried through with its remembered set and
// not re-granted; and that the bundle's embedded scene and theme reach the plan
// verbatim.

// resolveTickBundle resolves bundleWithTick from fixtures against a fresh gate and
// returns the resolution the plan is built from. It is the shared setup for the
// compose tests, the same fixtures resolveBundle's own tests use so the two halves
// agree on what a well-formed bundle looks like.
func resolveTickBundle(t *testing.T, gate *ext.Gate) *bundleResolution {
	t.Helper()
	root := t.TempDir()
	fetch := routingFetcher{byURL: map[string][]byte{
		bundleFetchURL: []byte(bundleWithTick),
		tickPluginURL:  buildInstallBundle(t, behavioralBundleJSON, false),
	}}
	res, err := resolveBundle(bundleFetchURL, fetch, fetch, root, gate)
	if err != nil {
		t.Fatalf("resolving the tick bundle failed: %v; the compose tests need a clean resolution to plan from", err)
	}
	return res
}

// TestPlanBundleComposeGrantsAndPairsEachPluginToItsPackage proves the happy path:
// an accepted bundle grants each not-yet-remembered plugin and yields one
// supervisor.Config per plugin, paired to its own laid-out package (Root non-empty)
// and carrying exactly the capabilities the gate granted (the manifest's declared
// set for a first grant). This is the input the loop Starts.
func TestPlanBundleComposeGrantsAndPairsEachPluginToItsPackage(t *testing.T) {
	gate := ext.NewGate(ext.NewMemoryConsentStore())
	res := resolveTickBundle(t, gate)

	plan, err := planBundleCompose(res, gate, ext.BundleAnswer{})
	if err != nil {
		t.Fatalf("planning an accepted bundle failed: %v; a grant that succeeds must produce a plan, not a refusal", err)
	}
	if len(plan.configs) != 1 {
		t.Fatalf("plan has %d configs, want 1; the compose plan must carry one supervisor.Config per bundle plugin so the loop spawns all of them", len(plan.configs))
	}
	cfg := plan.configs[0]
	if cfg.Manifest.ID != "tick" {
		t.Errorf("config manifest ID = %q, want %q; the config must carry the plugin's own manifest so the loop spawns the right process", cfg.Manifest.ID, "tick")
	}
	if cfg.Root == "" {
		t.Errorf("config Root is empty; without the laid-out tree the manifest's relative executable cannot resolve at spawn, so the pairing to the Installed package is load-bearing")
	}
	if len(cfg.Granted) != 1 || cfg.Granted[0] != "events.emit" {
		t.Errorf("config Granted = %v, want [events.emit]; a first grant carries the manifest's declared set verbatim, and the config must carry the gate's word, not the caller's", cfg.Granted)
	}
}

// TestPlanBundleComposeRejectionPlansNothing proves that a rejected answer grants
// nothing and returns no plan: the loop composes only from a non-nil plan, so a
// rejection that returned an empty-but-non-nil plan could still mount the scene.
// GrantBundle owns the "grant nothing on rejection" rule; this proves the planning
// layer propagates the refusal rather than swallowing it into a mountable plan.
func TestPlanBundleComposeRejectionPlansNothing(t *testing.T) {
	gate := ext.NewGate(ext.NewMemoryConsentStore())
	res := resolveTickBundle(t, gate)

	plan, err := planBundleCompose(res, gate, ext.BundleAnswer{Rejected: true})
	if err == nil {
		t.Fatalf("planning a rejected bundle returned no error; a rejection must abort with ErrBundleRejected so the loop mounts nothing")
	}
	if plan != nil {
		t.Fatalf("planning a rejected bundle returned a non-nil plan; the loop composes from any non-nil plan, so a rejection must return nil rather than an empty plan it could still mount")
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Errorf("rejection error = %q, want it to name the rejection; the loop reports 'you rejected this bundle' distinctly from a grant or fetch failure", err.Error())
	}
}

// TestPlanBundleComposeCarriesTheEmbeddedSceneVerbatim proves the bundle's scene
// reaches the plan as the raw bytes it was parsed from — the loop parses and mounts
// it, so a plan that dropped it would compose the plugins but never show the
// interface the bundle shipped. bundleWithTick embeds a scene, so the plan's scene
// must be non-empty and hold what the bundle declared.
func TestPlanBundleComposeCarriesTheEmbeddedSceneVerbatim(t *testing.T) {
	gate := ext.NewGate(ext.NewMemoryConsentStore())
	res := resolveTickBundle(t, gate)

	plan, err := planBundleCompose(res, gate, ext.BundleAnswer{})
	if err != nil {
		t.Fatalf("planning failed: %v", err)
	}
	if len(plan.scene) == 0 {
		t.Fatalf("plan carries no scene; bundleWithTick ships a scene, and a plan that drops it composes the plugins but never mounts the interface the bundle exists to deliver")
	}
	if !strings.Contains(string(plan.scene), "Desk") {
		t.Errorf("plan scene = %s, want it to carry the bundle's embedded document; the loop mounts these bytes, so they must be the ones the bundle declared", string(plan.scene))
	}
}

// TestPlanBundleComposeRemembersGrantThenCarriesItWithoutRegranting proves grant
// transfer through the plan: after a plugin's grant is remembered, a fresh
// resolution decides it Remembered and the plan carries its remembered set into a
// config WITHOUT a second Grant. This is GrantBundle's trusted-no-new-power branch
// producing a spawnable config: the loop must spawn ALL of the bundle's plugins, not
// only the ones it just prompted for, so a remembered plugin still yields a config.
func TestPlanBundleComposeRemembersGrantThenCarriesItWithoutRegranting(t *testing.T) {
	gate := ext.NewGate(ext.NewMemoryConsentStore())

	// First install: resolve, then plan with Remember so the grant is persisted
	// against the plugin's identity.
	first := resolveTickBundle(t, gate)
	if _, err := planBundleCompose(first, gate, ext.BundleAnswer{Remember: true}); err != nil {
		t.Fatalf("first plan (with remember) failed: %v; the grant transfer test needs the first grant to persist", err)
	}

	// Second sight of the same bundle: the gate now remembers the plugin, so the
	// resolution decides it Remembered.
	second := resolveTickBundle(t, gate)
	if got := second.decisions[0].Decision.Status; got != ext.DecisionRemembered {
		t.Fatalf("second-sight decision = %v, want DecisionRemembered; a grant persisted for these exact bytes must be found on the next resolution or grant transfer is broken", got)
	}

	// Plan the remembered bundle. A remembered plugin is NOT re-granted, but it must
	// still produce a config carrying its remembered set — the loop spawns it too.
	plan, err := planBundleCompose(second, gate, ext.BundleAnswer{})
	if err != nil {
		t.Fatalf("planning a remembered bundle failed: %v; a bundle of already-trusted plugins still installs, granting nothing new", err)
	}
	if len(plan.configs) != 1 {
		t.Fatalf("plan has %d configs, want 1; a remembered plugin must still yield a config so the loop spawns every plugin the bundle ships, not only the freshly-consented ones", len(plan.configs))
	}
	if got := plan.configs[0].Granted; len(got) != 1 || got[0] != "events.emit" {
		t.Errorf("remembered config Granted = %v, want [events.emit]; a remembered plugin carries its remembered set, so the spawned process has exactly the power the earlier consent granted", got)
	}
}
