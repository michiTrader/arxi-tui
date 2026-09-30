package main

import "testing"

// TestRowFocusKeyRoundTrips is the core contract: a target encoded by
// rowFocusKey is recovered exactly by parseRowFocusKey. The loop Tabs onto a
// synthetic row key and, when Enter fires, must recover the same (node, row) to
// expand and dispatch that element's on_press — if the pair does not survive the
// round-trip the press addresses the wrong row, the silent-wrong-frame this repo
// holds worse than an error.
func TestRowFocusKeyRoundTrips(t *testing.T) {
	cases := []struct {
		nodeID string
		row    int
	}{
		{"go", 0},
		{"approve", 7},
		{"open-agent", 123},
	}
	for _, c := range cases {
		key := rowFocusKey(c.nodeID, c.row)
		gotID, gotRow, ok := parseRowFocusKey(key)
		if !ok || gotID != c.nodeID || gotRow != c.row {
			t.Errorf("rowFocusKey(%q,%d) round-tripped to (%q,%d,ok=%v); want (%q,%d,ok=true)\n"+
				"consequence: a focused template row cannot be recovered at Enter, so the press\n"+
				"dispatches the wrong element's on_press or none at all.\n"+
				"remedy: rowFocusKey and parseRowFocusKey must be exact inverses.",
				c.nodeID, c.row, gotID, gotRow, ok, c.nodeID, c.row)
		}
	}
}

// TestRowFocusKeysAreDistinctPerRow proves the encoding separates two rows of
// one template node — the whole reason a row target is (RowIndex, NodeID) and
// not the authored id alone, which repeats across every instantiated row.
func TestRowFocusKeysAreDistinctPerRow(t *testing.T) {
	if a, b := rowFocusKey("go", 0), rowFocusKey("go", 1); a == b {
		t.Errorf("rowFocusKey produced the same key %q for rows 0 and 1\n"+
			"consequence: two instantiated rows collide in the focus ring, so Tabbing to one\n"+
			"and pressing dispatches the other — the id-repeats-across-rows bug the pair exists to avoid.\n"+
			"remedy: the row index must be part of the key.", a)
	}
}

// TestParseRowFocusKeyRejectsAPlainNodeID is the load-bearing discriminator: an
// ordinary author id carries no marker and must decode to ok=false, so the
// dispatcher routes it to findPressable rather than treating it as a row target.
// If a plain id ever reported ok=true the dispatcher would look it up in the
// row-press enumeration, where it does not belong, and drop the press.
func TestParseRowFocusKeyRejectsAPlainNodeID(t *testing.T) {
	for _, id := range []string{"go", "", "agent-list", "cmd:/agent be"} {
		if _, _, ok := parseRowFocusKey(id); ok {
			t.Errorf("parseRowFocusKey(%q) reported ok=true for a string with no row marker\n"+
				"consequence: an ordinary focused node is mistaken for a template-row target, so Enter\n"+
				"looks it up in the row-press list, finds nothing, and drops the press.\n"+
				"remedy: only a key carrying the NUL marker rowFocusKey writes may decode as a row target.", id)
		}
	}
}

// TestParseRowFocusKeyRejectsANonNumericIndex is the counterfactual for the
// index half: a marker with a non-integer or negative tail is not a key this
// package produced, so it must decode to ok=false rather than silently yielding
// row 0. A truncated or corrupted focus string must not resolve to a real row.
func TestParseRowFocusKeyRejectsANonNumericIndex(t *testing.T) {
	for _, key := range []string{
		"go" + rowFocusMarker,        // empty index
		"go" + rowFocusMarker + "x",  // non-numeric
		"go" + rowFocusMarker + "-1", // negative
	} {
		if id, row, ok := parseRowFocusKey(key); ok {
			t.Errorf("parseRowFocusKey(%q) reported ok=true (id=%q,row=%d) for a malformed index\n"+
				"consequence: a corrupted focus string resolves to a real row, dispatching a press\n"+
				"the user never made against an element chosen by an accident of parsing.\n"+
				"remedy: the tail after the marker must be a non-negative integer or the key is not a row target.",
				key, id, row)
		}
	}
}
