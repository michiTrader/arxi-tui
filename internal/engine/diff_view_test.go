package engine

import (
	"os"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/patch"
)

// The change-diff view is a host-generated scene (PLAN.md ADR-0003), so it is
// pinned the same way every other scene is: a golden of the rendered frame.
// This is the reachability half the ADR promised B4 would deliver — proof that
// the Diff -> scene fragment path draws, under the engine, the two columns with
// the changed lines marked. UPDATE_GOLDEN=1 regenerates the fixture.
//
// The diff is built from a real /ui command rather than hand-written line
// slices, so the golden exercises the whole path the user's change takes:
// Apply produces the new source and the Diff, Diff.Scene authors the view, and
// the engine renders it. A regression anywhere along that chain moves the
// golden, which is a review event and not noise.
func TestDiffViewGolden(t *testing.T) {
	src := []byte(`{"root":{"type":"stack","children":[` +
		`{"id":"chat","type":"markdown","bind":"chat.history"},` +
		`{"id":"note","type":"text","bind":"model.name","style":{"style":"dim"}}` +
		`]}}`)

	res, err := patch.Apply("SOBRIA.json", src, "/ui style note banner")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	doc, err := res.Diff.Scene(res.Summary)
	if err != nil {
		t.Fatalf("Diff.Scene: %v", err)
	}

	// The diff view does not read fold state -- it is literal text authored by
	// the host -- so it renders against the empty state. Rendering it with a
	// state at all is the point: it goes through the same renderer as every
	// other scene, with nothing special-cased for it.
	r := Renderer{Width: 72, Height: 24}
	got := r.RenderFrame(doc, fold.Fold(nil)).Styled()

	goldenPath := "../../testdata/DIFF.styled"
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
		t.Errorf("diff view styled output does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}
