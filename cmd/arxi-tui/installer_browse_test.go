package main

import (
	"testing"

	"github.com/michiTrader/arxi_tui/internal/ext"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// browseIndex is the three-entry index the browse tests filter and navigate. The
// names and descriptions are chosen so a query can select a known subset: "timer"
// hits exactly the Focus Timer's name, "pomodoro" hits it through its description
// alone, and an empty query lists all three. HTTPS manifest URLs so the index
// validates (validateManifestURL); the identities are distinct so a conversion or
// selection error names the wrong entry rather than a plausible-looking twin.
const browseIndex = `{
  "version": "reg/v1",
  "entries": [
    {
      "id": "ticker",
      "name": "Price Ticker",
      "version": "0.1.0",
      "manifest_url": "https://example.com/ticker/manifest.json",
      "description": "Streams a price into the status row.",
      "preview": "# Ticker\nStreams a price."
    },
    {
      "id": "timer",
      "name": "Focus Timer",
      "version": "1.2.0",
      "manifest_url": "https://example.com/timer/manifest.json",
      "description": "A pomodoro countdown overlay.",
      "preview": "# Focus Timer\nA countdown."
    },
    {
      "id": "weather",
      "name": "Weather Widget",
      "version": "2.0.0",
      "manifest_url": "https://example.com/weather/manifest.json",
      "description": "Shows the local forecast.",
      "preview": "# Weather\nForecast."
    }
  ]
}`

// browseReg parses and validates browseIndex, failing the test if the fixture
// itself is malformed — a broken fixture would make every assertion below measure
// the parser, not the browse.
func browseReg(t *testing.T) *ext.Registry {
	t.Helper()
	r, err := ext.ParseRegistry([]byte(browseIndex))
	if err != nil {
		t.Fatalf("ParseRegistry refused the browse fixture: %v; the fixture must be a well-formed index or the tests measure the parser", err)
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("the browse fixture does not validate: %v; fix the fixture, not the browse", err)
	}
	return r
}

// TestInstallerBrowseFiltersOnQuery proves the search: the browse recomputes its
// matches from the typed query through FilterEntries, so an empty query lists
// every entry and a typed one narrows to the entries whose name or description
// carries it. It types a query that hits exactly one entry's name and asserts the
// list is that one, then clears it and asserts the full list returns — both
// directions, because a browse that ignored the query would pass the empty case
// alone.
func TestInstallerBrowseFiltersOnQuery(t *testing.T) {
	b := newInstallerBrowse(browseReg(t))

	if got := len(b.matches()); got != 3 {
		t.Fatalf("an empty query listed %d entries, want 3; the unfiltered browse must show the whole index (BINDS.md §4.3: empty query lists everything)", got)
	}

	for _, r := range "timer" {
		b.typeRune(r)
	}
	got := b.matches()
	if len(got) != 1 || got[0].Name != "Focus Timer" {
		t.Fatalf("query %q matched %+v; want exactly the Focus Timer\n"+
			"consequence: the search box would show the query but the list would not follow it, so typing filters nothing.\n"+
			"remedy: matches() must recompute through Registry.FilterEntries over the current query.", b.query, got)
	}

	for range "timer" {
		b.backspace()
	}
	if got := len(b.matches()); got != 3 {
		t.Fatalf("after clearing the query the list held %d entries, want 3; a backspaced-empty query must restore the full browse", got)
	}
}

// TestInstallerBrowseMatchesOnDescription guards the one domain adaptation
// FilterEntries made over FilterSlashMatches: a registry browse searches the
// description too, not the name alone, so a user who recalls what a plugin does
// but not its name still finds it. "pomodoro" appears only in the Focus Timer's
// description.
func TestInstallerBrowseMatchesOnDescription(t *testing.T) {
	b := newInstallerBrowse(browseReg(t))
	for _, r := range "pomodoro" {
		b.typeRune(r)
	}
	got := b.matches()
	if len(got) != 1 || got[0].Name != "Focus Timer" {
		t.Fatalf("query %q matched %+v; want the Focus Timer via its description\n"+
			"consequence: the browse would only find plugins by name, so a search on what a plugin does returns nothing.\n"+
			"remedy: FilterEntries must match the description as well as the name.", b.query, got)
	}
}

// TestInstallerBrowseSelectionWrapsAtBothEnds proves the signed wrap (§4.3): ↑
// from the first row lands on the last and ↓ from the last lands on the first, so
// a single navigation key always reaches a row instead of dead-ending. Both
// directions are asserted because a clamp (the natural wrong implementation)
// passes every step except the two wraps.
//
// Counterfactual (run by hand, reported in the commit): replacing move()'s wrap
// with a clamp to [0, n-1] leaves moveUp at 0 and moveDown at n-1, failing both
// wrap assertions here.
func TestInstallerBrowseSelectionWrapsAtBothEnds(t *testing.T) {
	b := newInstallerBrowse(browseReg(t)) // 3 matches, selected 0

	b.moveUp()
	if b.selected != 2 {
		t.Fatalf("moveUp from row 0 landed on %d, want 2 (wrap to the last row)\n"+
			"consequence: ↑ at the top of the list dead-ends, so the user cannot reach the bottom entry by going up.\n"+
			"remedy: move() must wrap, not clamp — ((selected+delta)%%n+n)%%n.", b.selected)
	}
	b.moveDown()
	if b.selected != 0 {
		t.Fatalf("moveDown from the last row landed on %d, want 0 (wrap to the first row)\n"+
			"consequence: ↓ at the bottom dead-ends, so the list has an unreachable wrap the signed contract promises.\n"+
			"remedy: move() must wrap at both ends.", b.selected)
	}
	// A full lap forward returns to 0, proving the step size is one and the wrap
	// is not an off-by-one that skips a row.
	for i := 0; i < 3; i++ {
		b.moveDown()
	}
	if b.selected != 0 {
		t.Fatalf("three moveDown steps over three rows landed on %d, want 0; the cursor must lap exactly once", b.selected)
	}
}

// TestInstallerBrowseClampsSelectionWhenTheListShrinks proves the filter clamp
// (§4.3): a query that shrinks the match list under the cursor pulls the cursor
// back into range, so the browse never publishes a selection past its own list.
// It drives the cursor to the last of three rows, then types a query that leaves
// one match, and asserts the cursor is on that row.
//
// Counterfactual (run by hand, reported in the commit): dropping the
// clampSelection call from typeRune leaves selected at 2 over a one-row list, so
// publish sets community.selected=2 and the renderer's selectedCommunityMatch
// reads out of range — a blank preview and no highlight while Enter would still
// act on the clamped row. This fails here on selected != 0.
func TestInstallerBrowseClampsSelectionWhenTheListShrinks(t *testing.T) {
	b := newInstallerBrowse(browseReg(t))
	b.moveUp() // wrap to the last row, selected 2

	for _, r := range "timer" {
		b.typeRune(r)
	}
	if n := len(b.matches()); n != 1 {
		t.Fatalf("query %q left %d matches, want 1; the fixture or filter changed and this test is no longer exercising the shrink", b.query, n)
	}
	if b.selected != 0 {
		t.Fatalf("after the list shrank to one row the cursor stayed at %d, want 0\n"+
			"consequence: the browse publishes community.selected past its match list, so the preview pane reads out of\n"+
			"range (blank) and no row is highlighted while Enter would still install the clamped row.\n"+
			"remedy: clampSelection must run after every query edit.", b.selected)
	}
}

// TestInstallerBrowsePublishesAConsistentTriple proves publish writes the three
// community.* fields as one snapshot: the query it filtered on, the matches that
// query produced, and a selection that indexes those matches. A repaint that set
// them separately could show a query with a stale list or a highlight past the
// end; publishing them together is the invariant that prevents it.
func TestInstallerBrowsePublishesAConsistentTriple(t *testing.T) {
	b := newInstallerBrowse(browseReg(t))
	for _, r := range "e" { // "e" hits several names/descriptions
		b.typeRune(r)
	}
	b.moveDown()

	var state fold.State
	b.publish(&state)

	if state.CommunityQuery != b.query {
		t.Fatalf("published community.query = %q, want %q; publish must set the query it filtered on", state.CommunityQuery, b.query)
	}
	if len(state.CommunityMatches) != len(b.matches()) {
		t.Fatalf("published %d matches, browse has %d; publish must set the matches the query produced, not a stale list", len(state.CommunityMatches), len(b.matches()))
	}
	if len(state.CommunityMatches) == 0 {
		t.Fatalf("the %q query matched nothing; this test needs a non-empty list to check the selection indexes it", b.query)
	}
	if state.CommunitySelected < 0 || state.CommunitySelected >= len(state.CommunityMatches) {
		t.Fatalf("published community.selected = %d over a %d-row list; the selection must index its own matches\n"+
			"consequence: the renderer reads the selected entry out of range, so the preview is blank and no row is bright.\n"+
			"remedy: publish must set the clamped selection alongside the matches it indexes.", state.CommunitySelected, len(state.CommunityMatches))
	}
}

// TestInstallerBrowseResetsOnOpen proves a fresh browse starts unfiltered with
// the first row highlighted — the reset BINDS.md §4.3 signs for reopening the
// installer (community.selected resets to 0, community.query empty lists all).
func TestInstallerBrowseResetsOnOpen(t *testing.T) {
	b := newInstallerBrowse(browseReg(t))
	if b.query != "" {
		t.Fatalf("a fresh browse opened with query %q, want empty; reopening the installer must clear the search", b.query)
	}
	if b.selected != 0 {
		t.Fatalf("a fresh browse opened with selected %d, want 0; reopening must highlight the first match", b.selected)
	}
	if len(b.matches()) != 3 {
		t.Fatalf("a fresh browse listed %d entries, want the whole index; an empty query is unfiltered", len(b.matches()))
	}
}

// TestInstallerBrowseConvertsEveryEntryField guards the ext→fold boundary: every
// field of a RegistryEntry must reach the CommunityMatch, because a dropped field
// is a silent blank in the browse (a missing version, an empty preview) that
// compiles and renders without error. It compares the converted match against the
// entry field-for-field so a forgotten assignment names the field.
func TestInstallerBrowseConvertsEveryEntryField(t *testing.T) {
	b := newInstallerBrowse(browseReg(t))
	got := b.matches()
	if len(got) != len(b.reg.Entries) {
		t.Fatalf("converted %d matches from %d entries; the conversion must map one match per entry", len(got), len(b.reg.Entries))
	}
	for i, e := range b.reg.Entries {
		want := fold.CommunityMatch{
			ID:          e.ID,
			Name:        e.Name,
			Version:     e.Version,
			ManifestURL: e.ManifestURL,
			Description: e.Description,
			Preview:     e.Preview,
		}
		if got[i] != want {
			t.Errorf("entry %d converted to %+v, want %+v\n"+
				"consequence: a RegistryEntry field dropped in the conversion is a silent blank in the browse — a\n"+
				"missing version or an empty preview pane — that renders without any error.\n"+
				"remedy: matches() must copy every RegistryEntry field into the CommunityMatch.", i, got[i], want)
		}
	}
}

// TestInstallerBrowseEmptyIndexIsAnEmptyBrowse proves a browse over no index (the
// installer opened before its index has been fetched) is an empty browse, not a
// crash: matches() is empty, navigation rests the cursor at 0, and publish sets
// the empty triple. A nil registry reaches FilterEntries' own nil guard.
func TestInstallerBrowseEmptyIndexIsAnEmptyBrowse(t *testing.T) {
	b := newInstallerBrowse(nil)
	if got := len(b.matches()); got != 0 {
		t.Fatalf("a nil index produced %d matches, want 0; an un-fetched browse must be empty, not invented", got)
	}
	b.moveDown()
	b.moveUp()
	if b.selected != 0 {
		t.Fatalf("navigating an empty browse moved the cursor to %d, want 0; there is no row to move to", b.selected)
	}
	var state fold.State
	b.publish(&state)
	if len(state.CommunityMatches) != 0 || state.CommunitySelected != 0 {
		t.Fatalf("an empty browse published %d matches / selected %d, want 0 / 0; the empty state is a no-op", len(state.CommunityMatches), state.CommunitySelected)
	}
}
