package patch

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// `/ui set` stored every value as text, so an object property could never be set with
// it: the refusal said the border shape was the whole typed object, and the guide taught
// exactly that command. A model made about twenty attempts at it in one conversation.
func TestSetWritesJSONValuesAsJSON(t *testing.T) {
	src := []byte(`{"root":{"type":"stack","children":[{"id":"f","type":"box","border":"round","children":[{"type":"input","id":"p","bind":"user.input"}]}]}}`)
	cases := []struct {
		line string
		want string // substring of the resulting source
	}{
		{`/ui set f border {"shape":"round","style":"rainbow"}`, `"style": "rainbow"`},
		{`/ui set f border "heavy"`, `"border": "heavy"`},
		{`/ui set f border heavy`, `"border": "heavy"`},
		{`/ui set f style rainbow`, `"style": {`},
		{`/ui set f style {"style":"rainbow"}`, `"style": "rainbow"`},
		{`/ui set f grow 2`, `"grow": 2`},
		{`/ui set f fit true`, `"fit": true`},
	}
	for _, c := range cases {
		res, err := Apply("x", src, c.line)
		if err != nil {
			t.Errorf("%s: %v", c.line, err)
			continue
		}
		if !strings.Contains(string(res.Source), c.want) {
			t.Errorf("%s: the source lacks %q:\n%s", c.line, c.want, res.Source)
		}
	}
	// the object really is an object: the border reads back its style
	res, err := Apply("x", src, `/ui set f border {"shape":"round","style":"rainbow"}`)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	if node := findByID(res.Doc.Root, "f"); node != nil {
		found = node.BorderStyleName() == "rainbow" && node.BorderShape() == "round"
	}
	if !found {
		t.Fatal("the frame did not take shape round and style rainbow")
	}
	// counterfactual: text properties stay text even when they look like numbers
	res, err = Apply("x", src, `/ui set f title 2024`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Source), `"title": "2024"`) {
		t.Errorf("a title typed as 2024 must stay text:\n%s", res.Source)
	}
}

func findByID(n *scene.Node, id string) *scene.Node {
	if n == nil {
		return nil
	}
	if n.ID == id {
		return n
	}
	for _, c := range n.Children {
		if got := findByID(c, id); got != nil {
			return got
		}
	}
	return nil
}
