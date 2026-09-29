package main

import (
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// These tests pin routeBrowseKey, the term.Key routing of the open installer —
// the impure half installer_browse.go deliberately left out of its pure core.
// They drive it over the three-entry browseIndex fixture (defined in
// installer_browse_test.go) so a routed key's effect is checked against a known
// list, not a mock. The routing decides which pure transition a key means; the
// transitions themselves are pinned separately, so a failure here is a routing
// bug (the wrong key mapped) rather than a transition bug.

// TestRouteBrowseKeyTypesIntoTheQuery proves a plain rune filters the browse: the
// key is routed to typeRune, which appends to the query and re-clamps, so a typed
// substring narrows the match list. It types a query that hits exactly one entry
// and asserts the list is that one — the search working end to end through the
// router, not just the transition.
func TestRouteBrowseKeyTypesIntoTheQuery(t *testing.T) {
	b := newInstallerBrowse(browseReg(t))
	for _, r := range "timer" {
		if res := routeBrowseKey(b, runeKey(r)); res.close || res.installURL != "" {
			t.Fatalf("typing %q returned a non-edit result %+v; a query rune must only edit the browse in place, never close it or start an install", r, res)
		}
	}
	ms := b.matches()
	if len(ms) != 1 || ms[0].ID != "timer" {
		t.Fatalf("query %q matched %d entries %v; a typed rune must reach typeRune so the search narrows the list.\nRemedy: route KeyRunes to typeRune.", b.query, len(ms), matchIDs(ms))
	}
}

// TestRouteBrowseKeyIgnoresAControlChord proves a Ctrl/Alt chord is swallowed, not
// typed: the browse owns the keyboard while open, but a chord is not a query
// character. It routes Ctrl-a and asserts the query is untouched, so a chord
// cannot inject a control rune into the search string.
func TestRouteBrowseKeyIgnoresAControlChord(t *testing.T) {
	b := newInstallerBrowse(browseReg(t))
	chord := term.Key{Type: term.KeyRunes, Runes: []rune{'a'}, Mod: term.ModCtrl}
	if res := routeBrowseKey(b, chord); res.close || res.installURL != "" {
		t.Fatalf("a Ctrl chord returned %+v; it must be swallowed, not acted on", res)
	}
	if b.query != "" {
		t.Fatalf("a Ctrl chord typed %q into the query; a chord is not a query character and must not edit it.\nRemedy: route KeyRunes to typeRune only when no Ctrl/Alt modifier is held.", b.query)
	}
}

// TestRouteBrowseKeyBackspaceEditsTheQuery proves Backspace deletes the last query
// rune: it types a two-rune query, backspaces once, and asserts one rune remains,
// so the delete key reaches the rune-aware transition rather than the chat editor.
func TestRouteBrowseKeyBackspaceEditsTheQuery(t *testing.T) {
	b := newInstallerBrowse(browseReg(t))
	routeBrowseKey(b, runeKey('t'))
	routeBrowseKey(b, runeKey('i'))
	if res := routeBrowseKey(b, term.Key{Type: term.KeyBackspace}); res.close || res.installURL != "" {
		t.Fatalf("Backspace returned %+v; it must only edit the query in place", res)
	}
	if b.query != "t" {
		t.Fatalf("after typing \"ti\" and one Backspace the query is %q, want \"t\"; Backspace must reach the browse's backspace transition.\nRemedy: route KeyBackspace to backspace.", b.query)
	}
}

// TestRouteBrowseKeyArrowsMoveTheSelection proves ↑/↓ move the cursor and wrap:
// Down from the last row lands on the first, and Up from the first lands on the
// last. A router that mapped an arrow to nothing (or to the chat caret) would
// leave the selection frozen, so the wrap is asserted in both directions.
func TestRouteBrowseKeyArrowsMoveTheSelection(t *testing.T) {
	b := newInstallerBrowse(browseReg(t))
	n := len(b.matches())
	if n < 2 {
		t.Fatalf("the browse fixture has %d entries; the wrap test needs at least two", n)
	}
	routeBrowseKey(b, term.Key{Type: term.KeyUp}) // from 0, wrap to the last row
	if b.selected != n-1 {
		t.Fatalf("Up from row 0 landed on %d, want the last row %d; ↑ must reach moveUp, which wraps.\nRemedy: route KeyUp to moveUp.", b.selected, n-1)
	}
	routeBrowseKey(b, term.Key{Type: term.KeyDown}) // from the last row, wrap to 0
	if b.selected != 0 {
		t.Fatalf("Down from the last row landed on %d, want 0; ↓ must reach moveDown, which wraps.\nRemedy: route KeyDown to moveDown.", b.selected)
	}
}

// TestRouteBrowseKeyEscCloses proves Esc reports close: the browse should leave
// the display and the normal scene return. A router that swallowed Esc would trap
// the user in the installer with no non-panic way out.
func TestRouteBrowseKeyEscCloses(t *testing.T) {
	b := newInstallerBrowse(browseReg(t))
	res := routeBrowseKey(b, term.Key{Type: term.KeyEscape})
	if !res.close {
		t.Fatalf("Esc returned %+v; it must report close so the installer can be dismissed without the panic gesture.\nRemedy: route KeyEscape to close.", res)
	}
}

// TestRouteBrowseKeyEnterInstallsTheSelection proves Enter dispatches the selected
// entry's manifest_url — the same URL a typed `/ui plugin install` would carry, so
// a pressed card and a typed line cannot install different bytes. It moves the
// cursor one row down first so the test would catch a router that always installed
// row 0.
func TestRouteBrowseKeyEnterInstallsTheSelection(t *testing.T) {
	b := newInstallerBrowse(browseReg(t))
	routeBrowseKey(b, term.Key{Type: term.KeyDown}) // select row 1
	ms := b.matches()
	want := ms[1].ManifestURL
	res := routeBrowseKey(b, term.Key{Type: term.KeyEnter})
	if res.installURL != want {
		t.Fatalf("Enter on row 1 asked to install %q, want the selected entry's manifest_url %q.\nConsequence: the wrong plugin is fetched, or none.\nRemedy: route KeyEnter to the manifest_url of matches[selected].", res.installURL, want)
	}
}

// TestRouteBrowseKeyEnterOnEmptyListIsANoOp proves Enter over a browse filtered to
// nothing installs nothing: there is no entry under the cursor, so a stray Enter
// must not dispatch a fetch of the empty string. It types a query no entry matches
// and asserts Enter yields an empty installURL and does not close.
func TestRouteBrowseKeyEnterOnEmptyListIsANoOp(t *testing.T) {
	b := newInstallerBrowse(browseReg(t))
	for _, r := range "zzzznomatch" {
		routeBrowseKey(b, runeKey(r))
	}
	if len(b.matches()) != 0 {
		t.Fatalf("the query %q unexpectedly matched %d entries; the no-op-Enter test needs an empty list", b.query, len(b.matches()))
	}
	res := routeBrowseKey(b, term.Key{Type: term.KeyEnter})
	if res.installURL != "" || res.close {
		t.Fatalf("Enter on an empty list returned %+v; with no entry under the cursor it must do nothing, not dispatch an install of \"\".\nRemedy: guard the Enter branch on selected being within the match list.", res)
	}
}

// matchIDs is a test helper: the ids of a match slice, for a readable failure.
func matchIDs(ms []fold.CommunityMatch) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.ID
	}
	return out
}
