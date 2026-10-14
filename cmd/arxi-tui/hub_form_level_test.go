package main

import (
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// A form is a level of its own (lvForm). Saving it drops the form, so the hub must also
// leave that level: staying on lvForm with no form made the next repaint dereference nil
// and arxi died with a panic right after "Set context window" -> Enter.

func TestSavingTheWindowFormLeavesTheFormLevelAndRepaints(t *testing.T) {
	h := windowHub(0)
	// Highlight "Set context window" and open its form, as the user does.
	for i, id := range itemIDs(h) {
		if id == "window" {
			h.sel = i
		}
	}
	h.enter()
	if h.level != lvForm || h.form == nil {
		t.Fatalf("Enter on the window row must open the form, got level %v form %v", h.level, h.form)
	}

	h.apply(hubOutcome{ok: true, nav: navModelList, notice: "✓ saved"})

	if h.level == lvForm {
		t.Fatalf("the hub is still on the form level after the form was saved")
	}
	if h.level != lvModels {
		t.Errorf("after saving, the hub should show the model list, got level %v", h.level)
	}
	var st fold.State
	h.publish(&st) // the repaint that used to panic
	if len(st.HubRows) == 0 {
		t.Errorf("the repaint after saving drew no rows")
	}
}

// Whatever the outcome of a form (every nav target, closing or staying), the hub must
// never be left on lvForm with no form, and must repaint and take keys without panicking.
func TestNoFormOutcomeLeavesTheHubOnAFormLevelWithoutAForm(t *testing.T) {
	navs := []hubNav{navStay, navProviders, navActions, navModels, navModelList}
	froms := []hubLevel{lvProviders, lvCatalog, lvActions, lvModels, lvModelActions, lvSearch}
	for _, from := range froms {
		for _, nav := range navs {
			h := windowHub(0)
			h.setLevel(from)
			h.openForm(newWindowForm(h.model, 0))
			h.apply(hubOutcome{ok: true, nav: nav})
			if h.level == lvForm {
				t.Errorf("from %v nav %v: still on the form level", from, nav)
			}
			var st fold.State
			h.publish(&st)
			routeHubKey(h, term.Key{Type: term.KeyEnter})
			routeHubKey(h, term.Key{Type: term.KeyEscape})
		}
	}
}

// If some path ever leaves the form level without a form, painting must recover rather
// than take the whole program down.
func TestPaintingAFormLevelWithoutAFormRecoversInsteadOfPanicking(t *testing.T) {
	h := windowHub(0)
	h.openForm(newWindowForm(h.model, 0))
	h.form = nil // the broken state
	var st fold.State
	h.publish(&st)
	if h.level == lvForm {
		t.Errorf("the hub stayed on the form level with no form")
	}
	if len(st.HubRows) == 0 {
		t.Errorf("nothing was drawn")
	}
}

// The same broken state must not kill a key press either (Enter reads the form).
func TestAKeyOnAFormLevelWithoutAFormRecoversInsteadOfPanicking(t *testing.T) {
	for _, k := range []term.Key{{Type: term.KeyEnter}, {Type: term.KeyTab}, {Type: term.KeyBackspace}, {Type: term.KeyRunes, Runes: []rune("x")}, {Type: term.KeyDown}} {
		h := windowHub(0)
		h.openForm(newWindowForm(h.model, 0))
		h.form = nil
		routeHubKey(h, k)
		if h.level == lvForm {
			t.Errorf("key %v left the hub on the form level with no form", k.Type)
		}
	}
}
