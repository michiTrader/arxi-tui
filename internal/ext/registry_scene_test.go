package ext

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// The installer is a host-generated scene (ADR-0003), so it is held to exactly
// the rules a user's scene is, and to one more no golden guarantees on its own:
// every entry the index lists must become a node that is actually installable —
// carrying both an id and an on_press, the pair press.go requires to ring a node
// (cmd/arxi-tui/press.go). An installer that validated but drew cards no key
// could reach would be a catalogue you can read and cannot use, the precise
// failure the whole scene exists to prevent.

// twoEntryRegistry is a small, well-formed index with two entries, so the
// per-entry assertions below cover more than a single card and a failure names
// which entry went wrong. It is built as a struct literal rather than parsed
// because InstallerScene reads only the exported Entries — the parse path is
// registry_test.go's subject, not this file's.
func twoEntryRegistry() *Registry {
	return &Registry{
		Version: "reg/v1",
		Entries: []RegistryEntry{
			{
				ID:          "tick",
				Name:        "Ticker",
				Version:     "0.2.0",
				ManifestURL: "https://example.com/tick/manifest.json",
				Description: "A top-right price ticker.",
				Preview:     "# Ticker\n\nStreams a sparkline.",
			},
			{
				ID:          "clock",
				Name:        "Clock",
				Version:     "1.0.0",
				ManifestURL: "https://example.com/clock/manifest.json",
				Description: "A header clock.",
				Preview:     "# Clock\n\nShows the time.",
			},
		},
	}
}

// TestInstallerSceneValidates renders the installer and puts it through both
// validators, against both themes it might be drawn under (the shipped SOBRIA
// default and the Factory backstop). An installer scene that referenced a token
// neither theme signs would be refused at load — which for this view means the
// one screen whose job is to let the user extend the interface is the screen the
// engine will not draw.
func TestInstallerSceneValidates(t *testing.T) {
	doc, err := twoEntryRegistry().InstallerScene()
	if err != nil {
		t.Fatalf("InstallerScene: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("the installer scene does not validate: %v\n"+
			"consequence: the community installer is refused at load, so the user cannot browse or install anything from inside the TUI.\n"+
			"remedy: fix InstallerScene so the document it builds is valid.", err)
	}
	for _, th := range []struct {
		name string
		th   *theme.Theme
	}{{"SOBRIA", theme.SOBRIA()}, {"Factory", theme.Factory()}} {
		if errs := scene.ValidateTokens(doc, th.th); len(errs) > 0 {
			t.Errorf("the installer scene references a token %s does not define: %v\n"+
				"consequence: the installer renders only under a theme that happens to sign its tokens; under this one it is refused.\n"+
				"remedy: reference only tokens this theme signs, or sign the missing one.", th.name, errs[0])
		}
	}
}

// TestInstallerSceneMakesEveryEntryInstallable is the load-bearing property: for
// every entry in the index there is a node carrying both id "install:<id>" and
// on_press "cmd:/ui plugin add <manifest_url>". Both are required — press.go
// rings only nodes that have an id AND an on_press — so a card missing either is
// drawn but unreachable, and the manifest_url must be the exact one the entry
// names or the install fetches the wrong plugin. Counterfactual (run by hand):
// dropping either key, or the URL, from installerCard fails exactly this test.
func TestInstallerSceneMakesEveryEntryInstallable(t *testing.T) {
	r := twoEntryRegistry()
	doc, err := r.InstallerScene()
	if err != nil {
		t.Fatalf("InstallerScene: %v", err)
	}
	press := collectPressable(doc.Root)
	for _, e := range r.Entries {
		id := "install:" + e.ID
		want := "cmd:/ui plugin add " + e.ManifestURL
		got, ok := press[id]
		if !ok {
			t.Errorf("entry %q has no pressable node with id %q\n"+
				"consequence: the card is drawn but no key can focus it, so the entry can be read and never installed (press.go rings only nodes with id+on_press).\n"+
				"remedy: give installerCard both an id and an on_press.", e.ID, id)
			continue
		}
		if got != want {
			t.Errorf("entry %q installs via on_press %q, want %q\n"+
				"consequence: pressing the card installs a different URL than the entry names — the wrong plugin, or a malformed command.\n"+
				"remedy: build the on_press as cmd:/ui plugin add <manifest_url> from the entry's own manifest_url.", e.ID, got, want)
		}
	}
}

// TestInstallerSceneShowsEntryContent checks each entry's browse content actually
// reaches the screen: its name, its description and its preview. A card that
// validated and was pressable but drew none of these would be a blank row the
// user cannot tell apart from the next — the failure a validate-only check misses.
// Counterfactual (run by hand): dropping any of the three child nodes from
// installerCard fails exactly this test for that field.
func TestInstallerSceneShowsEntryContent(t *testing.T) {
	r := twoEntryRegistry()
	doc, err := r.InstallerScene()
	if err != nil {
		t.Fatalf("InstallerScene: %v", err)
	}
	screen := strings.Join(collectDrawnText(doc.Root), "\n")
	for _, e := range r.Entries {
		for _, want := range []struct{ what, text string }{
			{"name", e.Name},
			{"description", e.Description},
			{"preview", e.Preview},
		} {
			if !strings.Contains(screen, want.text) {
				t.Errorf("entry %q does not show its %s %q\n"+
					"consequence: the card is missing content the user browses by, so entries are indistinguishable in the list.\n"+
					"remedy: render the %s in installerCard.\n--- screen ---\n%s", e.ID, want.what, want.text, want.what, screen)
			}
		}
	}
}

// TestInstallerSceneEmptyRegistryStillValidates pins the empty case: an index
// with no entries must still produce a valid, browsable scene — the search input
// with no cards under it — not a refusal or a blank. "No plugins listed yet" and
// "the installer failed to build" must not look the same, and an empty index is
// the ordinary state of a brand-new registry, not an error.
func TestInstallerSceneEmptyRegistryStillValidates(t *testing.T) {
	doc, err := (&Registry{Version: "reg/v1"}).InstallerScene()
	if err != nil {
		t.Fatalf("InstallerScene on an empty registry: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("the empty-registry installer does not validate: %v\n"+
			"consequence: a new registry with no entries yet shows nothing instead of an empty browse; the user sees a broken screen, not an empty one.\n"+
			"remedy: keep InstallerScene total over the empty index.", err)
	}
	if press := collectPressable(doc.Root); len(press) != 0 {
		t.Errorf("empty registry produced %d pressable nodes, want 0; an index with no entries has nothing to install", len(press))
	}
	if search := findByID(doc.Root, "installer.search"); search == nil {
		t.Errorf("empty registry has no search input\n" +
			"consequence: the browse affordance vanishes exactly when the list is empty, which is when a user most needs to know the installer is working.\n" +
			"remedy: head the left column with the search input regardless of entry count.")
	}
}

// collectPressable walks the scene and returns a map from the id of every
// pressable node (one carrying both an id and an on_press, press.go's rule) to
// its on_press action, so a test can assert the installable set without depending
// on the exact tree shape.
func collectPressable(n *scene.Node) map[string]string {
	out := map[string]string{}
	var walk func(*scene.Node)
	walk = func(n *scene.Node) {
		if n == nil {
			return
		}
		if n.ID != "" && n.OnPress != "" {
			out[n.ID] = n.OnPress
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(n)
	return out
}

// collectDrawnText walks the scene and returns the literal text of every node
// that draws text — both text and markdown nodes — so a test can assert what the
// user reads. It differs from collectText (consent_scene_test.go) by including
// markdown, because the installer draws each entry's preview through a markdown
// node and a text-only walk would silently miss it.
func collectDrawnText(n *scene.Node) []string {
	if n == nil {
		return nil
	}
	var out []string
	if n.Type == "text" || n.Type == "markdown" {
		out = append(out, n.Text)
	}
	for _, c := range n.Children {
		out = append(out, collectDrawnText(c)...)
	}
	return out
}

// findByID walks the scene for the node with the given id, or nil.
func findByID(n *scene.Node, id string) *scene.Node {
	if n == nil {
		return nil
	}
	if n.ID == id {
		return n
	}
	for _, c := range n.Children {
		if found := findByID(c, id); found != nil {
			return found
		}
	}
	return nil
}
