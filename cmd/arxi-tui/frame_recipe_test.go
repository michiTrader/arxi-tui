package main

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/patch"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// The recipe the guide gives for a rounded frame round the input bar works on the
// factory scene: the two commands apply, the document validates, and the frame is round.
func TestTheGuidesRoundFrameRecipeWorksOnTheFactoryScene(t *testing.T) {
	doc := builtinDoc(t)
	src := doc.Source()
	for _, line := range []string{
		`/ui add node above prompt {"id":"input_frame","type":"box","border":"round"}`,
		`/ui move prompt into input_frame`,
	} {
		res, err := patch.Apply(doc.Name(), src, line)
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		src = res.Source
	}
	d, err := scene.ParseNamed(doc.Name(), src)
	if err != nil {
		t.Fatalf("the edited scene does not parse: %v", err)
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("the edited scene does not validate: %v", err)
	}
	if !strings.Contains(string(src), `"round"`) {
		t.Fatalf("the round border did not land in the document")
	}
}
