package patch

import (
	"testing"

	"github.com/michiTrader/arxi_tui/internal/ext"
)

// tokenManifest builds a validated declarative manifest that contributes both a
// token block and one overlay fragment, so a mount test can assert the token
// layer the Result carries. It fails the test if the manifest does not load — a
// token test must start from a manifest H2 accepts, or it measures the loader.
func tokenManifest(t *testing.T, id, tokens string) *ext.Manifest {
	t.Helper()
	src := `{
  "id": "` + id + `",
  "name": "Test Plugin",
  "version": "1.0.0",
  "protocol": "ext/v1",
  "tokens": ` + tokens + `,
  "mounts": [ { "where": "top-right", "fragment": { "id": "badge", "type": "text", "text": "x" } } ]
}`
	m, err := ext.ParseNamed("plugin.json", []byte(src))
	if err != nil {
		t.Fatalf("ParseNamed refused the token manifest: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("the token manifest does not load: %v; a token test must start from a manifest H2 accepts", err)
	}
	return m
}

// TestMountCarriesThePluginTokenLayer is H4's positive control on the patch
// side: a mount's Result must hand the host the plugin's parsed token block,
// keyed by the plugin id, so the loop can layer it over factory. Without it the
// fragment mounts but its own tokens never reach the active theme, and the
// plugin renders unstyled — the silent half-mount H4 exists to close.
func TestMountCarriesThePluginTokenLayer(t *testing.T) {
	m := tokenManifest(t, "tick", `{ "profit": { "fg": "green" }, "loss": { "fg": "red" } }`)

	res, err := Mount("host.json", []byte(hostScene), m)
	if err != nil {
		t.Fatalf("Mount refused a valid token manifest: %v", err)
	}
	if res.Tokens == nil {
		t.Fatal("Mount returned no token layer for a plugin that declares tokens.\nConsequence: the host has no plugin theme to merge, so the mounted fragment renders unstyled while the manifest's tokens sit parsed-but-unused — a mount that reports success and shows the wrong screen.\nRemedy: Mount must set Result.Tokens from m.Theme().")
	}
	if res.Tokens.ID != "tick" {
		t.Errorf("token layer id = %q, want \"tick\".\nConsequence: the host keys its layer set by id; a wrong id means a later `/ui plugin remove tick` drops the wrong layer or none.\nRemedy: set PluginTokens.ID to m.ID.", res.Tokens.ID)
	}
	if res.Tokens.Remove {
		t.Error("a mount produced a token layer marked Remove.\nConsequence: the host would drop this plugin's tokens instead of adding them.\nRemedy: Mount sets Remove=false (an add contributes a layer).")
	}
	if res.Tokens.Theme == nil {
		t.Fatal("token layer carries a nil theme on a mount.\nConsequence: the host merges nothing; the declared tokens are lost.\nRemedy: PluginTokens.Theme is m.Theme(), never nil.")
	}
	for _, tok := range []string{"profit", "loss"} {
		if !res.Tokens.Theme.Has(tok) {
			t.Errorf("mounted token layer is missing %q.\nConsequence: a fragment naming the plugin's own token resolves to the zero style, so the plugin's colours never ship.\nRemedy: m.Theme() must parse every token the manifest declares.", tok)
		}
	}
}

// TestMountWithoutTokensCarriesAnEmptyLayer pins the no-op case: a plugin that
// mounts only fragments still hands the host a non-nil, empty theme, so the loop
// merges a no-op rather than nil-checking. The empty-not-nil contract is what
// lets applyPluginTokens treat every add the same way.
func TestMountWithoutTokensCarriesAnEmptyLayer(t *testing.T) {
	m := manifest(t, "quiet", "top-right", `{ "type": "text", "text": "x" }`)

	res, err := Mount("host.json", []byte(hostScene), m)
	if err != nil {
		t.Fatalf("Mount refused a valid token-free manifest: %v", err)
	}
	if res.Tokens == nil || res.Tokens.Theme == nil {
		t.Fatal("a token-free mount produced a nil token layer.\nConsequence: the host must nil-check every add instead of merging a uniform no-op.\nRemedy: Mount sets Result.Tokens with m.Theme(), which returns an empty theme when no tokens are declared.")
	}
	if res.Tokens.Theme.Has("profit") {
		t.Error("a token-free plugin's layer resolves a token it never declared.\nConsequence: the empty-layer contract is broken; a merge would inject phantom tokens.\nRemedy: m.Theme() returns FromMap(nil) when the manifest has no tokens.")
	}
}

// TestUnmountSignalsTokenRemoval pins the inverse: an unmount hands the host a
// Remove op keyed by the plugin id, so the loop drops exactly the layer the add
// contributed. Removal names no host token — the layer set is keyed by id — so a
// recompose from the factory base cannot strip a factory or user token.
func TestUnmountSignalsTokenRemoval(t *testing.T) {
	m := tokenManifest(t, "tick", `{ "profit": { "fg": "green" } }`)
	mounted, err := Mount("host.json", []byte(hostScene), m)
	if err != nil {
		t.Fatalf("Mount refused a valid token manifest: %v", err)
	}

	res, err := Unmount("host.json", mounted.Source, "tick")
	if err != nil {
		t.Fatalf("Unmount refused a mounted plugin: %v", err)
	}
	if res.Tokens == nil {
		t.Fatal("Unmount returned no token op.\nConsequence: the mounted nodes go but the plugin's tokens stay in the active theme forever — an unmount that only half-reverses the mount.\nRemedy: Unmount sets Result.Tokens with Remove=true.")
	}
	if !res.Tokens.Remove {
		t.Error("Unmount's token op is not marked Remove.\nConsequence: the host would re-add the layer instead of dropping it.\nRemedy: set PluginTokens.Remove=true.")
	}
	if res.Tokens.ID != "tick" {
		t.Errorf("Unmount token op id = %q, want \"tick\".\nConsequence: the host drops the wrong layer, or none.\nRemedy: PluginTokens.ID names the removed plugin.", res.Tokens.ID)
	}
}
