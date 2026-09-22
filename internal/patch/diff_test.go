package patch

import (
	"strings"
	"testing"
)

// The diff is the data the change-diff view renders before a patch is trusted,
// so its correctness is the ship gate's correctness. These tests pin the two
// properties the view depends on: reformatting is never shown as a change, and
// the change that is shown is exactly the one the command made.

// TestDiffIgnoresReformatting is the property the file comment argues for: a
// patch re-serialises the whole document, so a diff against a differently
// formatted original must not report every line as changed. If it did, the
// user would be asked to approve a wall of noise and could not see the one line
// that matters.
func TestDiffIgnoresReformatting(t *testing.T) {
	// Same document, three formattings: minified, tab-indented, and already
	// canonical. The diff against the canonical form must be empty for all.
	canon := `{
  "root": {
    "children": [
      {
        "bind": "chat.history",
        "id": "chat",
        "type": "markdown"
      }
    ],
    "type": "stack"
  }
}`
	variants := map[string]string{
		"minified":     `{"root":{"type":"stack","children":[{"id":"chat","type":"markdown","bind":"chat.history"}]}}`,
		"tab-indented": "{\n\t\"root\": {\n\t\t\"type\": \"stack\",\n\t\t\"children\": [\n\t\t\t{\"id\":\"chat\",\"type\":\"markdown\",\"bind\":\"chat.history\"}\n\t\t]\n\t}\n}",
		"canonical":    canon,
	}
	for name, src := range variants {
		t.Run(name, func(t *testing.T) {
			d, err := DiffSource([]byte(src), []byte(canon))
			if err != nil {
				t.Fatalf("DiffSource: %v", err)
			}
			if d.Changed() {
				t.Errorf("a reformatting was reported as a change; the view would ask the user to approve formatting noise instead of the real edit\n%s", renderUnified(d))
			}
		})
	}
}

// TestDiffShowsOnlyTheChangedLine pins the other half: when one property
// changes, exactly the lines carrying it differ, and everything else is equal.
// A diff that over-reported would bury the change; one that under-reported
// would hide it, and hiding a change behind an approval gate is the failure the
// gate exists to prevent.
func TestDiffShowsOnlyTheChangedLine(t *testing.T) {
	before := `{"root":{"type":"text","id":"x","style":{"style":"dim"}}}`
	after := `{"root":{"type":"text","id":"x","style":{"style":"banner"}}}`

	d, err := DiffSource([]byte(before), []byte(after))
	if err != nil {
		t.Fatalf("DiffSource: %v", err)
	}
	if !d.Changed() {
		t.Fatalf("a real style change was reported as no change:\n%s", renderUnified(d))
	}

	var dels, ins []string
	for _, l := range d.Lines {
		switch l.Op {
		case OpDelete:
			dels = append(dels, strings.TrimSpace(l.Text))
		case OpInsert:
			ins = append(ins, strings.TrimSpace(l.Text))
		}
	}
	if len(dels) != 1 || len(ins) != 1 {
		t.Fatalf("expected one line removed and one added, got %d removed / %d added\n%s",
			len(dels), len(ins), renderUnified(d))
	}
	if !strings.Contains(dels[0], `"dim"`) {
		t.Errorf("removed line should carry the old value %q, got %q", "dim", dels[0])
	}
	if !strings.Contains(ins[0], `"banner"`) {
		t.Errorf("added line should carry the new value %q, got %q", "banner", ins[0])
	}
}

// TestDiffLineNumbersAreConsistent guards the numbers a side-by-side view uses
// to place lines: an equal line carries both, a delete carries only the old,
// an insert only the new, and each side's numbers increase without a gap. A
// view that trusted a wrong number would draw the change on the wrong row.
func TestDiffLineNumbersAreConsistent(t *testing.T) {
	before := `{"root":{"type":"text","id":"x","style":{"style":"dim"}}}`
	after := `{"root":{"type":"text","id":"x","style":{"style":"banner"}}}`

	d, err := DiffSource([]byte(before), []byte(after))
	if err != nil {
		t.Fatalf("DiffSource: %v", err)
	}

	oldSeen, newSeen := 0, 0
	for _, l := range d.Lines {
		switch l.Op {
		case OpEqual:
			if l.OldLine != oldSeen+1 || l.NewLine != newSeen+1 {
				t.Errorf("equal line broke numbering: old %d (want %d), new %d (want %d)",
					l.OldLine, oldSeen+1, l.NewLine, newSeen+1)
			}
			oldSeen, newSeen = l.OldLine, l.NewLine
		case OpDelete:
			if l.OldLine != oldSeen+1 {
				t.Errorf("delete broke old numbering: got %d, want %d", l.OldLine, oldSeen+1)
			}
			if l.NewLine != 0 {
				t.Errorf("delete carries a new line number %d, should be 0", l.NewLine)
			}
			oldSeen = l.OldLine
		case OpInsert:
			if l.NewLine != newSeen+1 {
				t.Errorf("insert broke new numbering: got %d, want %d", l.NewLine, newSeen+1)
			}
			if l.OldLine != 0 {
				t.Errorf("insert carries an old line number %d, should be 0", l.OldLine)
			}
			newSeen = l.NewLine
		}
	}
}

// TestApplyPopulatesTheDiff ties the diff to the surface it serves: a real
// /ui command's Result carries a diff that changed, so the view has something
// to show without recomputing it from Source.
func TestApplyPopulatesTheDiff(t *testing.T) {
	src := []byte(`{"root":{"type":"stack","children":[{"id":"note","type":"text","bind":"chat.history","style":{"style":"dim"}}]}}`)
	res, err := Apply("probe.json", src, "/ui style note banner")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !res.Diff.Changed() {
		t.Fatalf("Apply produced a Result whose diff reports no change, but the command restyled a node:\n%s", renderUnified(res.Diff))
	}
	var addedBanner bool
	for _, l := range res.Diff.Lines {
		if l.Op == OpInsert && strings.Contains(l.Text, `"banner"`) {
			addedBanner = true
		}
	}
	if !addedBanner {
		t.Errorf("the diff does not show the new style token being added\n%s", renderUnified(res.Diff))
	}
}

// renderUnified is a test helper: a git-style unified rendering used only in
// failure messages, so a diff assertion that fails shows the diff rather than a
// count the reader then has to reconstruct.
func renderUnified(d Diff) string {
	var b strings.Builder
	for _, l := range d.Lines {
		switch l.Op {
		case OpEqual:
			b.WriteString("  " + l.Text + "\n")
		case OpDelete:
			b.WriteString("- " + l.Text + "\n")
		case OpInsert:
			b.WriteString("+ " + l.Text + "\n")
		}
	}
	return b.String()
}
