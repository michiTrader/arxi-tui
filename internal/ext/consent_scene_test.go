package ext

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// The consent screen is a host-generated scene (ADR-0003), so it is held to
// exactly the rules a user's scene is, and to one more the diff view is not:
// the identity tuple the grant binds to must actually appear on screen. A
// consent screen that validated but silently omitted a capability, an arg or
// the version would be a yes to terms the user was never shown — the precise
// failure the whole gate exists to prevent.

// consentTestDigest is a fixed stand-in for the loader's PackageDigest. The view
// does not compute it (identity.go does); it only renders what it is handed, so
// a literal is enough and keeps the golden and these assertions deterministic.
const consentTestDigest = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// TestConsentSceneValidates renders the consent screen and puts it through both
// validators, against both themes it might be drawn under (the shipped SOBRIA
// default and the Factory backstop). A consent scene that referenced a token
// neither theme signs would be refused at load — which for this view means the
// user is asked to grant powers to a plugin whose consent screen the engine
// will not draw.
func TestConsentSceneValidates(t *testing.T) {
	doc, err := ConsentScene(behavioralManifest(), consentTestDigest)
	if err != nil {
		t.Fatalf("ConsentScene: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("the consent scene does not validate: %v\n"+
			"consequence: the consent screen is refused at load, so the user is asked nothing at the moment a behavioral plugin wants host powers.\n"+
			"remedy: fix ConsentScene so the document it builds is valid.", err)
	}
	for _, th := range []struct {
		name string
		th   *theme.Theme
	}{{"SOBRIA", theme.SOBRIA()}, {"Factory", theme.Factory()}} {
		if errs := scene.ValidateTokens(doc, th.th); len(errs) > 0 {
			t.Errorf("the consent scene references a token %s does not define: %v\n"+
				"consequence: the consent screen renders only under a theme that happens to sign its tokens; under this one it is refused.\n"+
				"remedy: reference only tokens this theme signs (header/dim/text/banner), or sign the missing one.", th.name, errs[0])
		}
	}
}

// TestConsentSceneShowsEveryIdentityField is the property no golden guarantees on
// its own: every field the grant is bound to (I-H) must be visible on the screen
// the user consents from. It asserts each tuple component and each requested
// capability appears in the rendered text. Counterfactual (run by hand):
// dropping any capability from ConsentScene's loop, or any identity row, fails
// exactly this test — the golden alone would still pass, because a byte-for-byte
// match to a fixture that itself omitted the field proves only that the omission
// is stable.
func TestConsentSceneShowsEveryIdentityField(t *testing.T) {
	m := behavioralManifest()
	doc, err := ConsentScene(m, consentTestDigest)
	if err != nil {
		t.Fatalf("ConsentScene: %v", err)
	}
	screen := strings.Join(collectText(doc.Root), "\n")

	must := []struct {
		what, text string
	}{
		{"name", m.Name},
		{"version", m.Version},
		{"id", m.ID},
		{"protocol", m.Protocol},
		{"executable", m.Executable},
		{"digest", consentTestDigest},
	}
	for _, arg := range m.Args {
		must = append(must, struct{ what, text string }{"arg", arg})
	}
	for _, c := range m.Capabilities {
		must = append(must, struct{ what, text string }{"capability", c})
	}
	for _, want := range must {
		if !strings.Contains(screen, want.text) {
			t.Errorf("the consent screen does not show %s %q\n"+
				"consequence: the user consents to a grant bound to a %s the screen never displayed — a yes to terms they were not shown (I-H).\n"+
				"remedy: render %s in ConsentScene.\n--- screen ---\n%s", want.what, want.text, want.what, want.what, screen)
		}
	}
}

// TestConsentSceneNamesNoPowersWhenNoneDeclared pins the empty case: a manifest
// that requests no capabilities must say so, not render a blank where the list
// would be. "Runs powerless" and "the list failed to render" must not look the
// same to the user deciding whether to trust the plugin.
func TestConsentSceneNamesNoPowersWhenNoneDeclared(t *testing.T) {
	m := behavioralManifest()
	m.Capabilities = nil
	doc, err := ConsentScene(m, consentTestDigest)
	if err != nil {
		t.Fatalf("ConsentScene: %v", err)
	}
	screen := strings.Join(collectText(doc.Root), "\n")
	if !strings.Contains(screen, "no host powers") {
		t.Errorf("a capability-less manifest does not state it runs with no host powers\n"+
			"consequence: an empty capability list reads as a rendering failure, not as a plugin that asked for nothing.\n"+
			"remedy: render the explicit no-powers line when Capabilities is empty.\n--- screen ---\n%s", screen)
	}
}

// collectText walks the consent scene and returns the literal text of every text
// node, so a test can assert what the user reads without depending on the exact
// tree shape (a box wrapping a stack wrapping rows). It mirrors diffscene_test's
// collectByToken but keeps every line rather than only the tokened ones.
func collectText(n *scene.Node) []string {
	if n == nil {
		return nil
	}
	var out []string
	if n.Type == "text" {
		out = append(out, n.Text)
	}
	for _, c := range n.Children {
		out = append(out, collectText(c)...)
	}
	return out
}
