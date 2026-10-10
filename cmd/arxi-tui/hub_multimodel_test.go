package main

import (
	"reflect"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/term"
)

func enterKey(h *providerHub) hubKeyResult {
	return routeHubKey(h, term.Key{Type: term.KeyEnter})
}

func TestCatalogFormTakesSeveralModelIDs(t *testing.T) {
	h, _ := newHub(hubData{}, hubOpenProviders)
	e, _ := catalogByID("openrouter")
	h.openForm(newAddForm(e))
	h.paste("sk-test-key")
	if r := enterKey(h); r.work != nil {
		t.Fatal("Enter on the key must move to the model ids, not submit")
	}
	typeInto(h, "model-a, model-b model-c")
	r := enterKey(h)
	if r.work == nil {
		t.Fatalf("no work: %q", r.notice)
	}
	want := []string{"model-a", "model-b", "model-c"}
	if r.work.Op != opAdd || !reflect.DeepEqual(r.work.Models, want) {
		t.Errorf("work = %+v, want models %v", r.work, want)
	}
}

func TestCatalogFormWithoutModelIDsFetchesThem(t *testing.T) {
	e, _ := catalogByID("openrouter")
	f := newAddForm(e)
	f.fields[0].value = "sk-test-key"
	w, msg := f.submit()
	if msg != "" || len(w.Models) != 0 {
		t.Errorf("msg=%q models=%v; empty ids mean 'fetch from the service'", msg, w.Models)
	}
}

func TestTheKeyFieldStillRefusesSpaces(t *testing.T) {
	h, _ := newHub(hubData{}, hubOpenProviders)
	e, _ := catalogByID("openrouter")
	h.openForm(newAddForm(e))
	h.paste("sk-te st\n")
	if got := h.form.fields[0].value; got != "sk-test" {
		t.Errorf("key = %q, want whitespace stripped", got)
	}
}
