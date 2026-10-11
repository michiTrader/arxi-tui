package patch

import (
	"strings"
	"testing"
)

// `remove` takes a node and its subtree out of the document for good. Counterfactual:
// `hide` leaves the node in the source, which is why it could never replace a banner.
func TestRemoveTakesTheNodeAndItsSubtreeOutOfTheDocument(t *testing.T) {
	name, src := sobria(t)
	res, err := Apply(name, src, "/ui remove banner")
	if err != nil {
		t.Fatal(err)
	}
	if parent, _ := locate(res.Doc.Root, "banner"); parent != nil {
		t.Error("the banner is still in the document")
	}
	if strings.Contains(string(res.Source), "brand.1") {
		t.Error("the banner's children are still in the source")
	}
	if parent, _ := locate(res.Doc.Root, "chat"); parent == nil {
		t.Error("remove took something else with it")
	}
	if res.Summary != `removed "banner"` || len(res.Diff.Lines) == 0 {
		t.Errorf("summary %q, diff lines %d", res.Summary, len(res.Diff.Lines))
	}
}

func TestRemoveRefusesWhatWouldBreakTheScreen(t *testing.T) {
	name, src := sobria(t)
	for line, want := range map[string]string{
		"/ui remove prompt": "input bar",
		"/ui remove nope":   "nope",
		"/ui remove":        "needs one node id",
		"/ui remove a b":    "needs one node id",
	} {
		_, err := Apply(name, src, line)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want it to mention %q", line, err, want)
		}
	}
}

func TestRemoveIsOneOfTheVerbs(t *testing.T) {
	for _, v := range Verbs() {
		if v == "remove" {
			return
		}
	}
	t.Error("remove is not in Verbs()")
}
