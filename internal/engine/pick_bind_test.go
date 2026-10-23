package engine

import (
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// Each group has its own choice: picking in one group never changes another's gates.
// Consequence: two tab sets on a screen would flip each other, as they did on ui.max.
// Remedy: ui.pick.is.<group>.<name> compares against that group's entry only.
func TestPickFamiliesAreIndependentPerGroup(t *testing.T) {
	st := fold.State{UIPick: map[string]string{"a": "one", "b": "two"}}
	for bind, want := range map[string]string{
		"ui.pick.is.a.one": "true",
		"ui.pick.is.a.two": "false",
		"ui.pick.is.b.two": "true",
		"ui.pick.is.b.one": "false",
		"ui.pick.is.c.one": "false",
		"ui.pick.none.a":   "false",
		"ui.pick.none.b":   "false",
		"ui.pick.none.c":   "true",
		"ui.pick.is.a":     placeholderValue, // malformed: no choice, falls to the placeholder
		"ui.pick.is..one":  placeholderValue,
		"ui.pick.none.":    placeholderValue,
	} {
		if got := resolveBind(bind, st); got != want {
			t.Errorf("resolveBind(%q) = %q, want %q", bind, got, want)
		}
	}
	// With no choices at all every group is "none" and nothing is picked.
	empty := fold.State{}
	if resolveBind("ui.pick.none.a", empty) != "true" || resolveBind("ui.pick.is.a.one", empty) != "false" {
		t.Errorf("an empty state must read as nothing picked")
	}
}
