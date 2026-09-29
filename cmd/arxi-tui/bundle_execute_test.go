package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/ext"
	"github.com/michiTrader/arxi_tui/internal/ext/supervisor"
	"github.com/michiTrader/arxi_tui/internal/patch"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// These tests pin executeBundleComposePlan's compose half without spawning a
// process: they use plans with no supervisor configs (a scene- and/or theme-only
// bundle, which is legal per checkEmpty) so the theme-application and
// document-replacement steps and their atomicity are exercised directly. The
// Start→Add→pump step for configs is the same sequence supervisor.Mount runs and is
// proven there; what is new here is the order and the all-or-nothing parse.

// composeDeps returns fresh, unused-but-valid dependencies for an executor call
// whose plan has no plugin configs, plus a capturing applyTokens and the doc pointer
// the test asserts against.
func composeDeps() (doc *scene.Document, applied *[]*patch.PluginTokens, apply func(*patch.PluginTokens), store *ext.PluginStore, reg *supervisor.Registry, mounted map[string]*supervisor.Supervisor) {
	doc = &scene.Document{}
	captured := []*patch.PluginTokens{}
	applied = &captured
	apply = func(op *patch.PluginTokens) { *applied = append(*applied, op) }
	store = ext.NewPluginStore()
	reg = supervisor.NewRegistry()
	mounted = map[string]*supervisor.Supervisor{}
	return
}

func TestExecuteBundlePlanReplacesSceneAndAppliesTheme(t *testing.T) {
	docSrc, applied, apply, store, reg, mounted := composeDeps()
	doc := docSrc
	plan := &bundleComposePlan{
		scene: json.RawMessage(`{"root":{"type":"text","text":"Desk"}}`),
		theme: json.RawMessage(`{"profit":{"fg":"green"}}`),
	}
	if err := executeBundleComposePlan(context.Background(), "Trading Desk", plan, &doc, apply, store, reg, mounted); err != nil {
		t.Fatalf("composing a valid scene+theme bundle failed: %v; the loop would report a broken install for a bundle that is fine", err)
	}
	if doc == docSrc {
		t.Fatal("the live document was not replaced by the bundle's embedded scene; the bundle shipped an interface and the user would still see the old one")
	}
	if len(*applied) != 1 {
		t.Fatalf("the bundle theme produced %d token layers, want exactly 1; a bundle that ships a theme must contribute one layer", len(*applied))
	}
	if id := (*applied)[0].ID; !strings.HasPrefix(id, "bundle:") {
		t.Fatalf("the bundle theme layer is keyed %q; it must be prefixed 'bundle:' so it cannot collide with a plugin id a /ui plugin remove looks up", id)
	}
}

func TestExecuteBundlePlanThemeOnlyLeavesDocumentUntouched(t *testing.T) {
	docSrc, applied, apply, store, reg, mounted := composeDeps()
	doc := docSrc
	plan := &bundleComposePlan{theme: json.RawMessage(`{"profit":{"fg":"green"}}`)}
	if err := executeBundleComposePlan(context.Background(), "Theme Pack", plan, &doc, apply, store, reg, mounted); err != nil {
		t.Fatalf("composing a theme-only bundle failed: %v", err)
	}
	if doc != docSrc {
		t.Fatal("a theme-only bundle replaced the document; a bundle that ships no scene must leave the current interface untouched")
	}
	if len(*applied) != 1 {
		t.Fatalf("a theme-only bundle applied %d layers, want 1", len(*applied))
	}
}

func TestExecuteBundlePlanAbortsWholeOnAMalformedScene(t *testing.T) {
	// Atomicity: a parse failure must abort with NOTHING composed. The theme is
	// valid and would apply, but the scene is malformed — so the executor must parse
	// both before touching any live state and, finding the scene bad, apply neither
	// the theme nor the document. A version that applied the theme before parsing the
	// scene would leave a half-composed workspace, the exact tear the design forbids.
	docSrc, applied, apply, store, reg, mounted := composeDeps()
	doc := docSrc
	plan := &bundleComposePlan{
		scene: json.RawMessage(`{"root": not valid json`),
		theme: json.RawMessage(`{"profit":{"fg":"green"}}`),
	}
	err := executeBundleComposePlan(context.Background(), "Broken Desk", plan, &doc, apply, store, reg, mounted)
	if err == nil {
		t.Fatal("a bundle with a malformed embedded scene composed without error; the broken interface would be swapped onto the display")
	}
	if doc != docSrc {
		t.Fatal("a malformed-scene bundle still replaced the document; the parse must precede the swap so a bad scene composes nothing")
	}
	if len(*applied) != 0 {
		t.Fatalf("a malformed-scene bundle applied %d theme layers; the theme must not be applied when the scene aborts (workspace atomicity)", len(*applied))
	}
}
