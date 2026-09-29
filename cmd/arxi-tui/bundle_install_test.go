package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/ext"
)

// This file covers resolveBundle, the fetch + per-plugin Decide half of a bundle
// install. The units it composes are proven elsewhere — the bundle parser and its
// refusals in internal/ext/bundle_test.go, the extraction/validation/digest in
// internal/ext, the gate decision in internal/ext/consent_test.go — so the concern
// here is the seam: that the bundle's plugins each flow through the H6/I5 pipeline
// against their own identity, that a failure anywhere fails the whole bundle
// (all-or-nothing), and that a grant remembered for a plugin's exact bytes is found
// by the resolve — the grant-transfer property the rejected aggregate-identity
// reading (DESIGN-BLOCK-J.md) would have destroyed.

// routingFetcher is a patch.Fetcher that answers by URL, so one test can drive both
// injected fetchers (the bundle JSON and each plugin archive) from a fixture map
// with no server. A URL with no entry returns an error naming it, so a test that
// mis-wires a reference fails loudly rather than fetching empty bytes.
type routingFetcher struct {
	byURL map[string][]byte
}

func (f routingFetcher) Fetch(rawURL string) (string, []byte, error) {
	data, ok := f.byURL[rawURL]
	if !ok {
		return "", nil, fmt.Errorf("routingFetcher: no fixture for %q", rawURL)
	}
	return rawURL, data, nil
}

// tickPluginURL is the archive URL the test bundles reference. The bytes it maps to
// are buildInstallBundle's well-formed behavioral package (the tick plugin), so the
// per-plugin pipeline lays out a real digested tree and the gate decides against a
// real identity.
const tickPluginURL = "https://example.test/tick.tar.gz"

// bundleWithTick is a well-formed bundle/v1 sharing one plugin (the tick archive)
// plus an embedded scene, so it contributes something and passes checkEmpty. The
// scene is minimal and valid; resolveBundle validates it but composes nothing, so a
// single root text node is enough to get past RefuseEmpty.
const bundleWithTick = `{
  "version": "bundle/v1",
  "name": "Trading Desk",
  "description": "A ticker plugin and a dashboard scene.",
  "scene": {"root": {"type": "text", "text": "Desk"}},
  "plugins": [{"manifest_url": "https://example.test/tick.tar.gz"}]
}`

// bundleFetchURL is the URL the bundle JSON itself is fetched from.
const bundleFetchURL = "https://example.test/desk.bundle.json"

// TestResolveBundleFetchesLaysOutAndDecidesEachPlugin proves the happy path: a
// well-formed bundle fetches, validates, and yields one decision per plugin — with
// a real laid-out package and a NeedsConsent decision, because the gate is empty.
// This is the input the consent screen and GrantBundle consume, produced without a
// grant or a spawn (Q15 across a bundle).
func TestResolveBundleFetchesLaysOutAndDecidesEachPlugin(t *testing.T) {
	root := t.TempDir()
	gate := ext.NewGate(ext.NewMemoryConsentStore())
	fetch := routingFetcher{byURL: map[string][]byte{
		bundleFetchURL: []byte(bundleWithTick),
		tickPluginURL:  buildInstallBundle(t, behavioralBundleJSON, false),
	}}

	res, err := resolveBundle(bundleFetchURL, fetch, fetch, root, gate)
	if err != nil {
		t.Fatalf("resolving a well-formed bundle failed: %v; a bundle whose plugin lays out cleanly must resolve to a decision, not a refusal", err)
	}
	if res.bundle.Name != "Trading Desk" {
		t.Errorf("resolved bundle name = %q, want %q; the consent screen leads with this identity and must read it from the fetched bytes", res.bundle.Name, "Trading Desk")
	}
	if len(res.decisions) != 1 || len(res.installed) != 1 {
		t.Fatalf("resolved %d decisions and %d installed, want 1 each; decisions and installed must be one-per-plugin and index-aligned so a grant pairs back to its tree", len(res.decisions), len(res.installed))
	}
	if got := res.decisions[0].Decision.Status; got != ext.DecisionNeedsConsent {
		t.Errorf("first-sight decision = %v, want DecisionNeedsConsent; an empty gate cannot remember a grant, so the bundle must ask", got)
	}
	if res.decisions[0].Digest == "" || res.installed[0].Digest != res.decisions[0].Digest {
		t.Errorf("decision digest %q and installed digest %q must be the real, equal PackageDigest; the grant binds to this identity and a placeholder would break the remembered-grant contract", res.decisions[0].Digest, res.installed[0].Digest)
	}
}

// TestResolveBundlePropagatesTheBundleFetchError proves a bundle-JSON fetch failure
// stops the thread before any plugin is touched: the error comes back and nothing
// is laid out. A bundle whose own document cannot be read is not a partial bundle.
func TestResolveBundlePropagatesTheBundleFetchError(t *testing.T) {
	root := t.TempDir()
	gate := ext.NewGate(ext.NewMemoryConsentStore())
	fetch := routingFetcher{byURL: map[string][]byte{}} // no fixture for the bundle URL

	res, err := resolveBundle(bundleFetchURL, fetch, fetch, root, gate)
	if err == nil {
		t.Fatalf("resolving with an unfetchable bundle URL returned no error; a bundle whose document cannot be fetched must fail, not resolve empty")
	}
	if res != nil {
		t.Errorf("a fetch failure returned a non-nil resolution; nothing may be decided when the bundle itself did not load")
	}
}

// TestResolveBundleRefusesAnInvalidBundle proves the validator runs before any
// plugin fetch: a bundle whose plugin reference is plaintext http:// is refused by
// Bundle.Validate (the HTTPS-only security rule) and no archive is fetched. The
// counterfactual is the scheme: the same bundle over https resolves (proven above),
// so the refusal is the scheme check firing, not an unrelated parse failure.
func TestResolveBundleRefusesAnInvalidBundle(t *testing.T) {
	root := t.TempDir()
	gate := ext.NewGate(ext.NewMemoryConsentStore())
	insecure := strings.Replace(bundleWithTick, "https://example.test/tick.tar.gz", "http://example.test/tick.tar.gz", 1)
	fetch := routingFetcher{byURL: map[string][]byte{
		bundleFetchURL: []byte(insecure),
		// The plugin archive is deliberately NOT provided: if validation is skipped
		// and the fetch is attempted, the routing fetcher's "no fixture" error proves
		// the plugin was reached, which is itself a failure of the ordering this test
		// pins.
	}}

	_, err := resolveBundle(bundleFetchURL, fetch, fetch, root, gate)
	if err == nil {
		t.Fatalf("resolving a bundle with an http:// plugin URL returned no error; the HTTPS-only rule must refuse a plaintext manifest reference before it is fetched")
	}
	if !strings.Contains(err.Error(), "https") {
		t.Errorf("refusal did not name the scheme rule: %v; a bundle-level validation refusal must name why the reference is rejected, not surface as a fetch error", err)
	}
}

// TestResolveBundleFailsWholeBundleOnAPluginFailure proves all-or-nothing: a plugin
// whose archive cannot be fetched fails the entire bundle, named by its URL, rather
// than shrinking the bundle to the plugins that loaded. A scene wired to a plugin
// that never arrived is a scene with dead binds (DESIGN-BLOCK-J.md).
func TestResolveBundleFailsWholeBundleOnAPluginFailure(t *testing.T) {
	root := t.TempDir()
	gate := ext.NewGate(ext.NewMemoryConsentStore())
	fetch := routingFetcher{byURL: map[string][]byte{
		bundleFetchURL: []byte(bundleWithTick),
		// tickPluginURL intentionally absent: the plugin archive fetch fails.
	}}

	res, err := resolveBundle(bundleFetchURL, fetch, fetch, root, gate)
	if err == nil {
		t.Fatalf("a bundle whose plugin could not be fetched resolved without error; all-or-nothing means one broken plugin fails the whole bundle")
	}
	if res != nil {
		t.Errorf("a plugin failure returned a non-nil resolution; a partially-loaded bundle must never be handed to the consent screen")
	}
	if !strings.Contains(err.Error(), tickPluginURL) {
		t.Errorf("the failure did not name the broken plugin URL: %v; the user must learn which reference in the share is the broken one", err)
	}
}

// TestResolveBundleFindsARememberedGrant proves grant transfer across the boundary
// the rejected aggregate-identity reading would have broken: a grant remembered for
// a plugin's exact per-manifest identity is found by resolveBundle, so the plugin
// comes back DecisionRemembered and the consent screen will list it as
// trusted-no-new-power rather than re-asking. The digest is taken from a first
// resolve (the only place the real PackageDigest is computed), the plugin is
// granted-and-remembered against its own identity, and a second resolve must see it.
//
// Counterfactual, run by hand: granting a *different* digest (a byte flipped) and
// re-resolving returns DecisionNeedsConsent, because the identity no longer matches
// — which is the property proving the grant is keyed per-manifest, not per-bundle.
func TestResolveBundleFindsARememberedGrant(t *testing.T) {
	root := t.TempDir()
	gate := ext.NewGate(ext.NewMemoryConsentStore())
	fetch := routingFetcher{byURL: map[string][]byte{
		bundleFetchURL: []byte(bundleWithTick),
		tickPluginURL:  buildInstallBundle(t, behavioralBundleJSON, false),
	}}

	first, err := resolveBundle(bundleFetchURL, fetch, fetch, root, gate)
	if err != nil {
		t.Fatalf("first resolve failed: %v", err)
	}
	d := first.decisions[0]
	if _, err := gate.Grant(d.Manifest, d.Digest, d.Manifest.Capabilities, true); err != nil {
		t.Fatalf("granting the plugin against its own identity failed: %v", err)
	}

	second, err := resolveBundle(bundleFetchURL, fetch, fetch, root, gate)
	if err != nil {
		t.Fatalf("second resolve failed: %v", err)
	}
	if got := second.decisions[0].Decision.Status; got != ext.DecisionRemembered {
		t.Fatalf("after a remembered grant for the plugin's own bytes, resolve decided %v, want DecisionRemembered; a bundle grant must transfer to a later sight of the same identity, or the aggregate-identity reading has crept back in", got)
	}
}
