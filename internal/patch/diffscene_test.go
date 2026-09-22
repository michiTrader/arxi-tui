package patch

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// The diff view is a host-generated scene (ADR-0003), so it is held to exactly
// the rules a user's scene is: it must validate, and every token it references
// must be defined in the theme. If either failed, the one surface whose whole
// job is to be trusted before a change is applied would be the surface the
// engine refuses to draw.

// TestDiffSceneValidates renders a real patch's diff to a scene and puts it
// through both validators, against both themes it might be drawn under (the
// shipped SOBRIA default and the Factory backstop). A diff scene that
// referenced a token neither theme signs would be refused at load, which for
// this view means the user is shown nothing at the moment they most need to see
// the change.
func TestDiffSceneValidates(t *testing.T) {
	src := []byte(`{"root":{"type":"stack","children":[{"id":"note","type":"text","bind":"chat.history","style":{"style":"dim"}}]}}`)
	res, err := Apply("probe.json", src, "/ui style note banner")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	doc, err := res.Diff.Scene(res.Summary)
	if err != nil {
		t.Fatalf("Diff.Scene: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("the diff scene does not validate: %v\n"+
			"consequence: the change-diff view is refused at load, so the user is shown nothing at the moment they most need to see what the agent changed.\n"+
			"remedy: fix Diff.Scene so the document it builds is valid.", err)
	}
	for _, th := range []struct {
		name string
		th   *theme.Theme
	}{{"SOBRIA", theme.SOBRIA()}, {"Factory", theme.Factory()}} {
		if errs := scene.ValidateTokens(doc, th.th); len(errs) > 0 {
			t.Errorf("the diff scene references a token %s does not define: %v\n"+
				"consequence: the diff view renders only under a theme that happens to sign its tokens; under this one it is refused.\n"+
				"remedy: sign diff.context/diff.del/diff.add in this theme, or stop referencing the missing one.", th.name, errs[0])
		}
	}
}

// TestDiffSceneMarksBothSides checks the scene actually carries the change on
// the right side: the old value under diff.del, the new under diff.add. A view
// that validated but drew both columns as plain context would satisfy the test
// above and still hide the change — the failure this whole gate exists to
// prevent.
func TestDiffSceneMarksBothSides(t *testing.T) {
	before := []byte(`{"root":{"type":"text","id":"x","style":{"style":"dim"}}}`)
	after := []byte(`{"root":{"type":"text","id":"x","style":{"style":"banner"}}}`)
	d, err := DiffSource(before, after)
	if err != nil {
		t.Fatalf("DiffSource: %v", err)
	}
	doc, err := d.Scene("restyled x")
	if err != nil {
		t.Fatalf("Diff.Scene: %v", err)
	}

	delText, addText := collectByToken(doc.Root)
	if !strings.Contains(strings.Join(delText, "\n"), `"dim"`) {
		t.Errorf("the old value %q is not marked with diff.del; removed lines: %v", "dim", delText)
	}
	if !strings.Contains(strings.Join(addText, "\n"), `"banner"`) {
		t.Errorf("the new value %q is not marked with diff.add; added lines: %v", "banner", addText)
	}
}

// collectByToken walks the diff scene and returns the literal text of every
// node styled diff.del and diff.add, so a test can assert the change landed on
// the correct side without depending on the exact tree shape.
func collectByToken(n *scene.Node) (del, add []string) {
	if n == nil {
		return nil, nil
	}
	if n.Type == "text" && n.Style != nil {
		switch n.Style["style"] {
		case "diff.del":
			del = append(del, n.Text)
		case "diff.add":
			add = append(add, n.Text)
		}
	}
	for _, c := range n.Children {
		d, a := collectByToken(c)
		del = append(del, d...)
		add = append(add, a...)
	}
	return del, add
}
