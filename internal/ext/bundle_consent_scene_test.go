package ext

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// The bundle consent screen (J4) is held to the same rules ConsentScene is: it is
// a host-generated scene (ADR-0003), so it must validate under every theme it
// might be drawn under, and — the property no golden guarantees on its own —
// every power a plugin will receive must actually appear on the screen the user
// consents from. A bundle screen that validated but silently omitted a
// capability, an arg or a plugin would be a yes to terms the user was never
// shown, over N plugins at once instead of one.

// secondBehavioralManifest is a second, distinct behavioral manifest so a bundle
// test can assert that a screen with two plugins shows both — a single-plugin
// fixture cannot catch a loop that renders only the first (or the last).
func secondBehavioralManifest() *Manifest {
	return &Manifest{
		ID:           "clock",
		Name:         "World Clock",
		Version:      "2.1.0",
		Protocol:     "ext/v1",
		Executable:   "./clock",
		Args:         []string{"--tz", "UTC"},
		Capabilities: []string{"actions.register"},
	}
}

// TestBundleConsentSceneValidates renders a two-plugin bundle screen and puts it
// through both validators against both themes (SOBRIA default and Factory
// backstop). A bundle screen that referenced a token neither theme signs would be
// refused at load — which means the user is asked to install a bundle whose
// consent screen the engine will not draw.
func TestBundleConsentSceneValidates(t *testing.T) {
	doc, err := BundleConsentScene("Trading Desk", "A ticker and a world clock", []BundlePluginConsent{
		{Manifest: behavioralManifest(), Digest: consentTestDigest},
		{Manifest: secondBehavioralManifest(), Digest: consentTestDigest},
	})
	if err != nil {
		t.Fatalf("BundleConsentScene: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("the bundle consent scene does not validate: %v\n"+
			"consequence: the screen is refused at load, so the user is asked nothing at the moment a bundle wants to install plugins with host powers.\n"+
			"remedy: fix BundleConsentScene so the document it builds is valid.", err)
	}
	for _, th := range []struct {
		name string
		th   *theme.Theme
	}{{"SOBRIA", theme.SOBRIA()}, {"Factory", theme.Factory()}} {
		if errs := scene.ValidateTokens(doc, th.th); len(errs) > 0 {
			t.Errorf("the bundle consent scene references a token %s does not define: %v\n"+
				"consequence: the screen renders only under a theme that happens to sign its tokens; under this one it is refused.\n"+
				"remedy: reference only tokens this theme signs (header/dim/text/banner), or sign the missing one.", th.name, errs[0])
		}
	}
}

// TestBundleConsentSceneShowsEveryPluginsIdentity is the load-bearing property:
// for EVERY plugin needing consent, every field the grant binds to (I-H) must be
// visible. It asserts both plugins' full tuples appear. Counterfactual (run by
// hand): rendering only needsConsent[0], or dropping any capability from
// pluginBlock's loop, fails exactly this test — over two distinct manifests, so a
// loop that renders one plugin twice would also be caught by the second's
// missing fields.
func TestBundleConsentSceneShowsEveryPluginsIdentity(t *testing.T) {
	m1, m2 := behavioralManifest(), secondBehavioralManifest()
	doc, err := BundleConsentScene("Trading Desk", "A ticker and a world clock", []BundlePluginConsent{
		{Manifest: m1, Digest: consentTestDigest},
		{Manifest: m2, Digest: "0000000000000000000000000000000000000000000000000000000000000000"},
	})
	if err != nil {
		t.Fatalf("BundleConsentScene: %v", err)
	}
	screen := strings.Join(collectText(doc.Root), "\n")

	must := []struct{ what, text string }{
		{"bundle name", "Trading Desk"},
		{"bundle description", "A ticker and a world clock"},
	}
	for _, p := range []struct {
		m      *Manifest
		digest string
	}{{m1, consentTestDigest}, {m2, "0000000000000000000000000000000000000000000000000000000000000000"}} {
		must = append(must,
			struct{ what, text string }{"plugin name", p.m.Name},
			struct{ what, text string }{"plugin version", p.m.Version},
			struct{ what, text string }{"plugin id", p.m.ID},
			struct{ what, text string }{"plugin protocol", p.m.Protocol},
			struct{ what, text string }{"plugin executable", p.m.Executable},
			struct{ what, text string }{"plugin digest", p.digest},
		)
		for _, a := range p.m.Args {
			must = append(must, struct{ what, text string }{"plugin arg", a})
		}
		for _, c := range p.m.Capabilities {
			must = append(must, struct{ what, text string }{"plugin capability", c})
		}
	}
	for _, want := range must {
		if !strings.Contains(screen, want.text) {
			t.Errorf("the bundle consent screen does not show %s %q\n"+
				"consequence: the user consents to a bundle whose %s the screen never displayed — a yes to terms they were not shown (I-H), now for one of N plugins at once.\n"+
				"remedy: render %s for every plugin in BundleConsentScene.\n--- screen ---\n%s", want.what, want.text, want.what, want.what, screen)
		}
	}
}

// TestBundleConsentSceneListsRememberedPluginsWithoutRedetailing pins the
// new-vs-whole-cost distinction (DESIGN-BLOCK-J J4): an already-remembered plugin
// must appear (the install still mounts it) but must NOT show its capability
// block (it grants no new power). The counterfactual is built in: the remembered
// plugin's name is present, its unique capability is not.
func TestBundleConsentSceneListsRememberedPluginsWithoutRedetailing(t *testing.T) {
	needs := behavioralManifest()                 // capability "events.emit" is unique to it
	trusted := secondBehavioralManifest()         // will be marked Remembered
	trusted.Capabilities = []string{"net.listen"} // a capability that must NOT appear
	doc, err := BundleConsentScene("Mixed", "one new, one trusted", []BundlePluginConsent{
		{Manifest: needs, Digest: consentTestDigest, Remembered: false},
		{Manifest: trusted, Digest: consentTestDigest, Remembered: true},
	})
	if err != nil {
		t.Fatalf("BundleConsentScene: %v", err)
	}
	screen := strings.Join(collectText(doc.Root), "\n")

	if !strings.Contains(screen, trusted.Name) {
		t.Errorf("an already-trusted plugin is not named on the bundle screen\n"+
			"consequence: the install mounts a plugin the user cannot see named, showing the new cost but hiding the whole cost (DESIGN-BLOCK-J J4).\n"+
			"remedy: list every remembered plugin as trusted-no-new-power.\n--- screen ---\n%s", screen)
	}
	if strings.Contains(screen, "net.listen") {
		t.Errorf("an already-trusted plugin's capability is re-detailed on the bundle screen\n"+
			"consequence: a remembered plugin grants no new power, but the screen shows a capability block as if the user were granting it again — noise that hides which grants are actually new.\n"+
			"remedy: list remembered plugins by name only, without their capability block.\n--- screen ---\n%s", screen)
	}
	// The plugin that DOES need consent still shows its unique power in full.
	if !strings.Contains(screen, "events.emit") {
		t.Errorf("a plugin needing consent does not show its capability on the bundle screen\n"+
			"consequence: the user grants a power the screen never displayed.\n"+
			"remedy: render the full capability block for every not-yet-remembered plugin.\n--- screen ---\n%s", screen)
	}
}

// TestBundleConsentSceneNamesNoPluginBundle pins the scene+theme-only case: a
// bundle that installs no plugins must still show the confirm as a named screen,
// stating it runs no plugin code — "installs nothing runnable" and "the plugin
// list failed to render" must not look the same, the bundle analogue of
// ConsentScene's "runs powerless" row.
func TestBundleConsentSceneNamesNoPluginBundle(t *testing.T) {
	doc, err := BundleConsentScene("Just A Theme", "recolours the interface", nil)
	if err != nil {
		t.Fatalf("BundleConsentScene: %v", err)
	}
	screen := strings.Join(collectText(doc.Root), "\n")
	if !strings.Contains(screen, "Just A Theme") {
		t.Errorf("a plugin-less bundle does not show its name\n"+
			"consequence: the user is asked to install a bundle the screen never named.\n"+
			"remedy: always render the bundle identity block.\n--- screen ---\n%s", screen)
	}
	if !strings.Contains(screen, "runs no plugin code") {
		t.Errorf("a plugin-less bundle does not state it runs no plugin code\n"+
			"consequence: an empty plugin list reads as a rendering failure, not as a bundle that installs only a scene/theme.\n"+
			"remedy: render the explicit no-plugin-code line when no plugin appears.\n--- screen ---\n%s", screen)
	}
}

// TestBundleConsentSceneAlwaysDrawsTheAnswerKeys pins the loop contract the golden
// protects: the y/n/r keys the consent-reading layer will bind must be drawn on
// every bundle screen, including the plugin-less one, the same way the slash menu
// draws the keys before the loop dispatches them.
func TestBundleConsentSceneAlwaysDrawsTheAnswerKeys(t *testing.T) {
	for _, tc := range []struct {
		name    string
		plugins []BundlePluginConsent
	}{
		{"two plugins", []BundlePluginConsent{{Manifest: behavioralManifest(), Digest: consentTestDigest}, {Manifest: secondBehavioralManifest(), Digest: consentTestDigest}}},
		{"no plugins", nil},
	} {
		doc, err := BundleConsentScene("B", "d", tc.plugins)
		if err != nil {
			t.Fatalf("BundleConsentScene (%s): %v", tc.name, err)
		}
		screen := strings.Join(collectText(doc.Root), "\n")
		for _, key := range []string{"[y]", "[n]", "[r]"} {
			if !strings.Contains(screen, key) {
				t.Errorf("the %s bundle screen does not draw the %s key\n"+
					"consequence: the consent-reading loop binds a key the screen never told the user about — a keypress with no visible affordance.\n"+
					"remedy: draw y/n/r on every bundle consent screen.\n--- screen ---\n%s", tc.name, key, screen)
			}
		}
	}
}
