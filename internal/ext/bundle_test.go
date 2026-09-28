package ext

import (
	"strings"
	"testing"
)

// A minimal, well-formed bundle: a version, the identity the consent screen shows,
// and all three contributions (an embedded scene, an embedded theme block, and one
// HTTPS plugin reference). Every refusal test below is a single deviation from
// this, so a failure names exactly the field under test — the registry_test.go /
// manifest_test.go discipline, ported.
const validBundle = `{
  "version": "bundle/v1",
  "name": "Trader Desk",
  "description": "A curated ticker workspace.",
  "scene": { "root": { "type": "text", "text": "welcome" } },
  "theme": { "profit": { "fg": "green" } },
  "plugins": [
    { "manifest_url": "https://example.com/tick/manifest.json" }
  ]
}`

// TestLoadsAValidBundle is the positive control: the whole point of J4 is that a
// well-formed bundle parses and validates, so this runs first — if it fails, every
// refusal test below is measuring a validator that rejects everything.
func TestLoadsAValidBundle(t *testing.T) {
	b, err := ParseBundleNamed("bundle.json", []byte(validBundle))
	if err != nil {
		t.Fatalf("ParseBundleNamed refused a well-formed bundle: %v; a bundle is data and must parse", err)
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("Validate refused a well-formed bundle: %v; J4's promise is that a good bundle loads", err)
	}
	if b.Name != "Trader Desk" || len(b.Plugins) != 1 || b.Plugins[0].ManifestURL != "https://example.com/tick/manifest.json" {
		t.Fatalf("parsed bundle = %+v; the identity, plugin reference and manifest_url must survive the parse", b)
	}
}

// validateBundleSrc parses the given bundle source and returns the Validate error,
// so a one-deviation test reads as "mutate validBundle, assert the refusal." A
// parse failure is fatal here: these tests probe Validate, and a source that does
// not even parse is a broken test, not a refusal under test.
func validateBundleSrc(t *testing.T, src string) error {
	t.Helper()
	b, err := ParseBundleNamed("bundle.json", []byte(src))
	if err != nil {
		t.Fatalf("ParseBundleNamed: %v", err)
	}
	return b.Validate()
}

// TestRefusesABundleWithNoVersion: the version is the closed-set schema tag, so a
// bundle without one cannot be read at all. Counterfactual (run by hand): dropping
// the empty-version check lets an untagged bundle reach the version-set lookup,
// which also refuses it but with a less precise message.
func TestRefusesABundleWithNoVersion(t *testing.T) {
	src := strings.Replace(validBundle, `"version": "bundle/v1",`, "", 1)
	if err := validateBundleSrc(t, src); err == nil {
		t.Fatal("Validate accepted a bundle with no version; the version is the closed-set schema tag the host must recognise, so its absence must be refused")
	}
}

// TestRefusesAnUnknownBundleVersion is the closed-set counterfactual: the bundle
// schema is code on both sides, so a version the host does not recognise is
// refused, not negotiated. Run by hand, dropping the legalBundleVersions check
// lets "bundle/v99" load and the host reads a shape it does not understand as if it
// did — the exact silent-accept the closed set exists to prevent.
func TestRefusesAnUnknownBundleVersion(t *testing.T) {
	src := strings.Replace(validBundle, `"bundle/v1"`, `"bundle/v99"`, 1)
	err := validateBundleSrc(t, src)
	if err == nil {
		t.Fatal("Validate accepted an unknown bundle version; the schema is a closed set, so an unrecognised version must be refused rather than read as if the host understood it")
	}
	if !strings.Contains(err.Error(), "bundle/v1") {
		t.Fatalf("unknown-version refusal = %q; it must list the legal versions so the author knows what the host understands", err.Error())
	}
}

// TestRefusesABundleWithNoName: the name is the label the one consent screen shows,
// and consent to an unnamed share is consent the user could not read.
func TestRefusesABundleWithNoName(t *testing.T) {
	src := strings.Replace(validBundle, `"name": "Trader Desk",`, "", 1)
	if err := validateBundleSrc(t, src); err == nil {
		t.Fatal("Validate accepted a bundle with no name; the name is the consent-screen label, so its absence must be refused")
	}
}

// TestRefusesABundleWithNoDescription: the description is the one-line summary the
// consent screen shows, and a share with none is one the user accepts blind.
func TestRefusesABundleWithNoDescription(t *testing.T) {
	src := strings.Replace(validBundle, `"description": "A curated ticker workspace.",`, "", 1)
	if err := validateBundleSrc(t, src); err == nil {
		t.Fatal("Validate accepted a bundle with no description; the description is the consent-screen summary, so its absence must be refused")
	}
}

// TestRefusesAnEmptyBundle: a share that carries no scene, no theme and no plugins
// changes nothing, and an accepted no-op is a grant that bought nothing. Run by
// hand, dropping checkEmpty accepts a bundle that installs nothing — the empty
// grant the check exists to reject.
func TestRefusesAnEmptyBundle(t *testing.T) {
	src := `{
  "version": "bundle/v1",
  "name": "Nothing",
  "description": "Contributes nothing."
}`
	if err := validateBundleSrc(t, src); err == nil {
		t.Fatal("Validate accepted a bundle with no scene, theme or plugins; a share that changes nothing is a no-op grant and must be refused")
	}
}

// TestABundleWithOnlyOneContributionValidates pins the other side of checkEmpty:
// any single contribution is enough, so a scene-only, a theme-only and a
// plugins-only bundle each load. Without this, a checkEmpty that demanded all
// three (or the wrong boolean) would pass the empty test above and still be wrong.
func TestABundleWithOnlyOneContributionValidates(t *testing.T) {
	cases := map[string]string{
		"scene only":   `{"version":"bundle/v1","name":"S","description":"d","scene":{"root":{"type":"text","text":"hi"}}}`,
		"theme only":   `{"version":"bundle/v1","name":"T","description":"d","theme":{"profit":{"fg":"green"}}}`,
		"plugins only": `{"version":"bundle/v1","name":"P","description":"d","plugins":[{"manifest_url":"https://example.com/m.json"}]}`,
	}
	for name, src := range cases {
		if err := validateBundleSrc(t, src); err != nil {
			t.Fatalf("%s: Validate refused a bundle with a single contribution: %v; any one of scene/theme/plugins satisfies checkEmpty", name, err)
		}
	}
}

// TestRefusesABundleSceneWithNoRoot: the embedded scene is the interface the bundle
// ships, so a scene that declares no "root" has nothing to draw — the RefuseEmpty
// contract. Run by hand, dropping the RefuseEmpty call accepts a rootless scene,
// which the parser keeps as a document that renders nothing, indistinguishable from
// a broken share.
func TestRefusesABundleSceneWithNoRoot(t *testing.T) {
	src := `{"version":"bundle/v1","name":"S","description":"d","scene":{"notroot":{"type":"text"}}}`
	err := validateBundleSrc(t, src)
	if err == nil {
		t.Fatal("Validate accepted a bundle scene with no root; a scene with nothing to draw is a broken interface and must be refused")
	}
	if !strings.Contains(err.Error(), "root") {
		t.Fatalf("rootless-scene refusal = %q; it must name the missing root so the author knows what to add", err.Error())
	}
}

// TestRefusesAMalformedBundleThemeBlock: the theme block is checked through the one
// token validator a theme file gets, so a bad colour is refused here. Run by hand,
// skipping validateThemeBlock lets an unparseable token definition reach the merge
// at mount, where it fails far from the bundle the author can fix.
func TestRefusesAMalformedBundleThemeBlock(t *testing.T) {
	src := strings.Replace(validBundle, `"theme": { "profit": { "fg": "green" } }`, `"theme": { "profit": { "fg": "notacolor" } }`, 1)
	if err := validateBundleSrc(t, src); err == nil {
		t.Fatal("Validate accepted a bundle theme block with an invalid colour; the token block is checked by the same validator a theme file gets, so a malformed token must be refused")
	}
}

// TestRefusesAPluginWithNoManifestURL: a plugin reference is exactly its
// manifest_url — the only thing that turns it into an install — so a reference
// without one names no plugin to fetch.
func TestRefusesAPluginWithNoManifestURL(t *testing.T) {
	src := strings.Replace(validBundle, `{ "manifest_url": "https://example.com/tick/manifest.json" }`, `{ }`, 1)
	if err := validateBundleSrc(t, src); err == nil {
		t.Fatal("Validate accepted a plugin reference with no manifest_url; the manifest_url is the only field that turns a reference into an install, so its absence must be refused")
	}
}

// TestRefusesANonHTTPSPluginURL is the security counterfactual, run for both an
// http:// and a file:// scheme: a shared bundle is attacker-controlled data, and a
// plaintext or local-path manifest_url would let it redirect the install to swapped
// code or a path on the user's disk. Run by hand, dropping the scheme check accepts
// both, opening the man-in-the-middle window the HTTPS-only rule closes.
func TestRefusesANonHTTPSPluginURL(t *testing.T) {
	for _, bad := range []string{"http://example.com/tick/manifest.json", "file:///etc/passwd"} {
		src := strings.Replace(validBundle, "https://example.com/tick/manifest.json", bad, 1)
		err := validateBundleSrc(t, src)
		if err == nil {
			t.Fatalf("Validate accepted a plugin manifest_url %q; a manifest is fetched over HTTPS only, so a non-HTTPS scheme must be refused", bad)
		}
		if !strings.Contains(err.Error(), "https") {
			t.Fatalf("non-HTTPS refusal for %q = %q; it must name the HTTPS-only rule so the author knows the fix", bad, err.Error())
		}
	}
}

// TestBundleSceneMayReferencePluginBinds is the load-bearing "do not over-refuse"
// guarantee, and the reason the pure core does NOT run the scene's bind validator.
// A curated bundle's whole purpose is a scene wired to the plugins it bundles, so a
// scene referencing a bundled plugin's `<id>.*` namespace must pass Validate — those
// binds are resolved at mount against the fetched plugins' scopes, exactly as the
// registry defers a fetched manifest to install. Counterfactual (run by hand):
// replacing the parse+RefuseEmpty in validateScene with a full doc.Validate() fails
// this test, because tick.price is unsigned until the tick manifest is fetched —
// which would make every plugin-wired bundle unshareable, defeating J4.
func TestBundleSceneMayReferencePluginBinds(t *testing.T) {
	src := `{
  "version": "bundle/v1",
  "name": "Wired",
  "description": "A scene bound to a bundled plugin.",
  "scene": { "root": { "type": "text", "bind": "tick.price" } },
  "plugins": [ { "manifest_url": "https://example.com/tick/manifest.json" } ]
}`
	if err := validateBundleSrc(t, src); err != nil {
		t.Fatalf("Validate refused a bundle scene that references a bundled plugin's bind: %v; those binds are validated at mount against the fetched plugin, not in the pure core — refusing here makes every plugin-wired bundle unshareable", err)
	}
}

// TestBundleRefusalCarriesAnAddress: a refusal without a location is a bug
// (AGENTS.md, invariant 4). The bundle reuses the manifest/registry addressing, so
// a field-level refusal points at the key the author wrote. This pins that the
// wiring is live rather than degrading every error to the file-only address.
func TestBundleRefusalCarriesAnAddress(t *testing.T) {
	src := strings.Replace(validBundle, `"bundle/v1"`, `"bundle/v99"`, 1)
	err := validateBundleSrc(t, src)
	if err == nil {
		t.Fatal("expected the unknown-version refusal")
	}
	if !strings.Contains(err.Error(), "bundle.json:") {
		t.Fatalf("refusal = %q; it must carry a file:line address (bundle.json:line:col), not just a reason", err.Error())
	}
}
