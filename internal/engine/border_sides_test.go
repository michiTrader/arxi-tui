package engine

import (
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// A border may give each side its own style token, so a frame's top and bottom can be
// two different colours (or two different animations). Both ways a frame is drawn, the
// box and the bordered overlay, must honour it.
func TestEachSideOfAFrameCanWearItsOwnToken(t *testing.T) {
	src := `{"root":{"type":"box","border":{"shape":"round","style":"border","top":"warn","bottom":"dim"},
	  "children":[{"type":"text","text":"hi"}]}}`
	doc, err := scene.ParseDocument([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	r := Renderer{Width: 20, Height: 6}
	f := r.RenderFrame(doc, fold.State{})
	first, last := f.Live[0][0], f.Live[len(f.Live)-1][0]
	if first.Style != "warn" || last.Style != "dim" {
		t.Fatalf("top wears %q, bottom wears %q; want warn and dim", first.Style, last.Style)
	}
	// The sides that name nothing keep the border's own style.
	var left string
	for _, l := range f.Live[1 : len(f.Live)-1] {
		left = l[0].Style
	}
	if left != "border" {
		t.Errorf("an unnamed side wears %q, want the border's own style", left)
	}

	// And the overlay path.
	ov := `{"root":{"type":"overlay","anchor":"bottom","border":{"shape":"single","top":"warn","bottom":"dim"},
	  "children":[{"type":"text","text":"menu"}]}}`
	d2, err := scene.ParseDocument([]byte(ov))
	if err != nil {
		t.Fatal(err)
	}
	g := r.RenderFrame(d2, fold.State{})
	if len(g.Live) < 3 || g.Live[0][0].Style != "warn" || g.Live[len(g.Live)-1][0].Style != "dim" {
		t.Errorf("the overlay frame ignored the per-side tokens")
	}
}
