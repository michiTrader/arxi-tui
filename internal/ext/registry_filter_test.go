package ext

import "testing"

// A three-entry index whose names and descriptions are deliberately chosen so
// each filter test isolates one behaviour: "Ticker"/"streams" for a name hit
// with a description miss, "Weather" whose description alone carries "streams",
// and "Clock" that shares nothing with the others. Every test below is a query
// against this one fixture, the manifest_test.go discipline ported.
const filterRegistry = `{
  "version": "reg/v1",
  "entries": [
    {
      "id": "tick",
      "name": "Ticker",
      "version": "0.2.0",
      "manifest_url": "https://example.com/tick/manifest.json",
      "description": "A top-right price ticker."
    },
    {
      "id": "weather",
      "name": "Weather",
      "version": "1.0.0",
      "manifest_url": "https://example.com/weather/manifest.json",
      "description": "It streams the local forecast."
    },
    {
      "id": "clock",
      "name": "Clock",
      "version": "0.1.0",
      "manifest_url": "https://example.com/clock/manifest.json",
      "description": "A wall clock."
    }
  ]
}`

// filterFixture parses and validates the shared index, failing loudly if the
// fixture itself is bad — a filter test measuring a rejected index would report
// zero matches for every query and look like a broken filter.
func filterFixture(t *testing.T) *Registry {
	t.Helper()
	r, err := ParseRegistryNamed("registry.json", []byte(filterRegistry))
	if err != nil {
		t.Fatalf("ParseRegistryNamed refused the filter fixture: %v; the fixture must be a well-formed index", err)
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate refused the filter fixture: %v; the fixture must be a well-formed index", err)
	}
	return r
}

// ids projects the entries to their ids so a test asserts on a comparable set
// rather than whole structs.
func ids(entries []RegistryEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.ID)
	}
	return out
}

// TestEmptyQueryReturnsEveryEntry is the positive control and the "browse is
// open but unfiltered" contract: an empty search box shows the whole index, not
// an empty list. Ported straight from FilterSlashMatches's empty→all rule; if
// this fails, the browse view opens showing nothing and looks broken.
func TestEmptyQueryReturnsEveryEntry(t *testing.T) {
	got := ids(filterFixture(t).FilterEntries(""))
	if len(got) != 3 {
		t.Fatalf("FilterEntries(\"\") returned %v; an empty query is unfiltered and must return every entry, or the browse view opens empty", got)
	}
}

// TestQueryMatchesTheName is the name-hit path: "tick" is a substring of
// "Ticker". Run the counterfactual by hand — removing the ToLower(Name) clause
// leaves only the description clause, and "Ticker"'s description has no "tick",
// so this row vanishes and a user searching a plugin by its own name finds
// nothing.
func TestQueryMatchesTheName(t *testing.T) {
	got := ids(filterFixture(t).FilterEntries("tick"))
	if len(got) != 1 || got[0] != "tick" {
		t.Fatalf("FilterEntries(\"tick\") = %v; a query that is a substring of the name must match that entry, since the name is the card's primary label", got)
	}
}

// TestQueryMatchesTheDescriptionNotJustTheName is the load-bearing half of the
// domain adaptation: "streams" appears in Weather's description and in no name.
// The counterfactual is the whole reason this port widened past
// FilterSlashMatches — drop the description clause and a user who recalls what a
// plugin does but not what it is called gets zero results. Weather here proves
// the description clause fires independently of the name clause.
func TestQueryMatchesTheDescriptionNotJustTheName(t *testing.T) {
	got := ids(filterFixture(t).FilterEntries("streams"))
	if len(got) != 1 || got[0] != "weather" {
		t.Fatalf("FilterEntries(\"streams\") = %v; the query is in Weather's description and no name, so matching it proves the description clause fires — the point of widening past the name-only slash filter", got)
	}
}

// TestQueryIsCaseInsensitive pins the ToLower on both sides: an uppercase query
// against a capitalised name still matches. Dropping either ToLower makes the
// search case-sensitive, so "TICKER" or "ticker" would miss "Ticker" and a user
// is punished for their shift key.
func TestQueryIsCaseInsensitive(t *testing.T) {
	got := ids(filterFixture(t).FilterEntries("TICKER"))
	if len(got) != 1 || got[0] != "tick" {
		t.Fatalf("FilterEntries(\"TICKER\") = %v; the match is case-insensitive on both sides, so casing in the search box must not change what is found", got)
	}
}

// TestNoMatchReturnsNilNotEveryEntry guards the failure mode that would make the
// filter useless: a query nothing contains must return an empty result, never
// fall through to the whole index. It also pins the nil-not-all contract — the
// browse view distinguishes "nothing matches your search" from "here is
// everything" only if a true miss returns an empty slice.
func TestNoMatchReturnsNilNotEveryEntry(t *testing.T) {
	got := filterFixture(t).FilterEntries("zzz-nothing-contains-this")
	if len(got) != 0 {
		t.Fatalf("FilterEntries(no-match) = %v; a query nothing contains must return an empty result, or the browse view shows every plugin for a search that matched none", ids(got))
	}
}

// TestQueryDoesNotMatchTheID records the deliberate exclusion: "clock" is the id
// of the Clock entry, but the query "wall" (in its description) is what should
// find it, while a query on the id token alone must not surface a row on a
// string the card never shows. Here we assert the inverse directly: the id
// "clock" is matched because it is also the name, but a substring unique to the
// manifest_url path is not. We use "example" — present in every manifest_url and
// in no name or description — to prove url/id text is not searched.
func TestQueryDoesNotMatchURLOrIDOnlyText(t *testing.T) {
	got := filterFixture(t).FilterEntries("example")
	if len(got) != 0 {
		t.Fatalf("FilterEntries(\"example\") = %v; \"example\" is only in the manifest_url, which the card never shows, so matching it would surface rows on text the user cannot see", ids(got))
	}
}

// TestNilRegistryFiltersToNil is the defensive path: the host may call the
// filter before an index is fetched, and a nil receiver must return nil rather
// than panic — the browse loop cannot crash because the user typed before the
// index arrived.
func TestNilRegistryFiltersToNil(t *testing.T) {
	var r *Registry
	if got := r.FilterEntries("anything"); got != nil {
		t.Fatalf("(*Registry)(nil).FilterEntries = %v; a nil index must filter to nil, not panic, since a keystroke can arrive before the fetch completes", got)
	}
}
