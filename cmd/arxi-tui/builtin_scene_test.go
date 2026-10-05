package main

import (
	"os"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// The reported bug: opened in any folder but the repository, the program booted the
// bare raw scene, because the default scene was a relative file path. The built-in
// scene must boot from anywhere, with no notice, and be the real sobria interface.
func TestTheDefaultSceneBootsFromAnyFolder(t *testing.T) {
	t.Chdir(t.TempDir()) // a folder with no testdata/ in it
	if _, err := os.Stat("testdata/SOBRIA.json"); err == nil {
		t.Fatal("the test folder must not contain the scene file")
	}
	doc, notice, err := resolveStartScene(builtinScene)
	if err != nil || notice != "" {
		t.Fatalf("err=%v notice=%q", err, notice)
	}
	raw, _, _ := resolveStartScene("")
	if len(doc.Root.Children) <= len(raw.Root.Children) {
		t.Fatalf("booted the raw scene (%d nodes) instead of the sobria one (%d)", len(raw.Root.Children), len(doc.Root.Children))
	}
	state := fold.Fold(nil)
	state.HostMode = "ask"
	out := (&engine.Renderer{Width: 100, Height: 24}).RenderFrame(doc, state).Plain()
	if !strings.Contains(out, "ask") {
		t.Errorf("the status row of the sobria scene is missing:\n%s", out)
	}
}

// -raw and an explicit empty -scene keep meaning "the raw scene"; a path is a path.
func TestRawAndExplicitScenesAreStillHonoured(t *testing.T) {
	raw, _, err := resolveStartScene("")
	if err != nil || len(raw.Root.Children) != 3 {
		t.Fatalf("raw scene: %v, %d children", err, len(raw.Root.Children))
	}
	_, notice, err := resolveStartScene("testdata/does-not-exist.json")
	if err != nil || notice != "" {
		t.Errorf("a missing user scene falls back quietly: err=%v notice=%q", err, notice)
	}
}
