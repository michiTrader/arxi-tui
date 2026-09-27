package engine

import (
	"os"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/ext"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// The consent screen is a host-generated scene (PLAN.md ADR-0003), so it is
// pinned the way every other scene is: a golden of the rendered frame. This is
// the reachability half — proof that ext.ConsentScene draws, under the engine,
// the identity block and the requested capabilities the user consents from. It
// is the consent analogue of TestDiffViewGolden. UPDATE_GOLDEN=1 regenerates it.
//
// The manifest is a fixed behavioral one (an executable, args and two declared
// capabilities) so the golden witnesses the whole tuple the grant binds to on
// screen; the digest is a literal because the view renders the digest it is
// handed rather than computing one (that is the loader's job, identity.go).
func TestConsentViewGolden(t *testing.T) {
	m := &ext.Manifest{
		ID:           "tick",
		Name:         "Community Ticker",
		Version:      "1.0.0",
		Protocol:     "ext/v1",
		Executable:   "./tick",
		Args:         []string{"--interval", "5s"},
		Capabilities: []string{"actions.register", "events.emit"},
	}
	doc, err := ext.ConsentScene(m, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	if err != nil {
		t.Fatalf("ConsentScene: %v", err)
	}

	// The consent view does not read fold state -- it is literal text authored by
	// the host from the manifest -- so it renders against the empty state, going
	// through the same renderer as every other scene with nothing special-cased.
	r := Renderer{Width: 72, Height: 24}
	got := r.RenderFrame(doc, fold.Fold(nil)).Styled()

	goldenPath := "../../testdata/CONSENT.styled"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	if got != string(want) {
		t.Errorf("consent view styled output does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}
