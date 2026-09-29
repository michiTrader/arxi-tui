package ext

import (
	"testing"

	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// The community installer is a host-generated scene (ADR-0003), so it is held to
// exactly the rules a user's scene is. Since the keystroke loop landed, the
// installer is LiveInstallerScene: it binds its entry list to the community.* view
// state the loop writes rather than baking cards from an index, so there is no
// per-entry-installable property to check here — the entries are fold state, and
// that they reach the frame through the row_template is proven at the composed-
// frame level in internal/engine (community_live_test.go). What this package still
// owns is the load-time premise: the document validates and names only tokens the
// shipped themes sign.

// TestLiveInstallerSceneValidates renders the live installer and puts it through
// both validators, against both themes it might be drawn under (the shipped SOBRIA
// default and the Factory backstop). An installer scene that referenced a token
// neither theme signs would be refused at load — which for this view means the one
// screen whose job is to let the user extend the interface is the screen the engine
// will not draw. LiveInstallerScene takes no index (its content is the fold), so a
// single rendering covers every state: the structure never varies.
func TestLiveInstallerSceneValidates(t *testing.T) {
	doc, err := LiveInstallerScene()
	if err != nil {
		t.Fatalf("LiveInstallerScene: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("the live installer scene does not validate: %v\n"+
			"consequence: the community installer is refused at load, so the user cannot browse or install anything from inside the TUI.\n"+
			"remedy: fix LiveInstallerScene so the document it builds is valid.", err)
	}
	for _, th := range []struct {
		name string
		th   *theme.Theme
	}{{"SOBRIA", theme.SOBRIA()}, {"Factory", theme.Factory()}} {
		if errs := scene.ValidateTokens(doc, th.th); len(errs) > 0 {
			t.Errorf("the live installer scene references a token %s does not define: %v\n"+
				"consequence: the installer renders only under a theme that happens to sign its tokens; under this one it is refused.\n"+
				"remedy: reference only tokens this theme signs, or sign the missing one.", th.name, errs[0])
		}
	}
}

// TestLiveInstallerSceneHasTheSearchInput pins the one structural affordance the
// browse cannot open without: the search input heading the left column, bound to
// community.query. It is checked here (rather than only in a frame golden) so a
// builder that dropped the input is a named load-time failure, not an opaque diff.
func TestLiveInstallerSceneHasTheSearchInput(t *testing.T) {
	doc, err := LiveInstallerScene()
	if err != nil {
		t.Fatalf("LiveInstallerScene: %v", err)
	}
	search := findInstallerNode(doc.Root, "installer.search")
	if search == nil {
		t.Fatalf("the live installer has no node with id %q\n"+
			"consequence: the browse has no search box, so the keystroke loop's community.query is written to a field nothing draws.\n"+
			"remedy: head the left column with the search input bound to community.query.", "installer.search")
	}
	if search.Bind != "community.query" {
		t.Errorf("the search input binds %q, want %q\n"+
			"consequence: the box does not show what the loop types, so the search reads as dead.\n"+
			"remedy: bind installer.search to community.query.", search.Bind, "community.query")
	}
}

// findInstallerNode walks the scene for the node with the given id, or nil.
func findInstallerNode(n *scene.Node, id string) *scene.Node {
	if n == nil {
		return nil
	}
	if n.ID == id {
		return n
	}
	for _, c := range n.Children {
		if found := findInstallerNode(c, id); found != nil {
			return found
		}
	}
	return nil
}
