package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/driver"
)

// Editing a model from the providers hub: its id (the name) and the price the user
// declares for it. These tests pin what the screen offers, what a form submits, and what
// a core that cannot do it costs.

func editHub(canEdit bool) *providerHub {
	h, _ := newHub(hubData{
		models: []driver.ModelRow{
			{Provider: "acme", ID: "fast", Enabled: true, Price: &driver.ModelPrice{In: 1.5, Out: 7}},
			{Provider: "acme", ID: "plain", Enabled: true},
		},
		canEditModels: canEdit,
	}, hubOpenProviders)
	h.prov, h.model = "acme", "acme/fast"
	h.setLevel(lvModelActions)
	return h
}

func itemIDs(h *providerHub) string {
	var ids []string
	for _, it := range h.items() {
		ids = append(ids, it.id)
	}
	return strings.Join(ids, ",")
}

func TestAModelCanBeEditedFromItsActions(t *testing.T) {
	h := editHub(true)
	if got := itemIDs(h); got != "default,toggle,edit,remove" {
		t.Fatalf("the model's actions are %s; want an edit row before remove.\nConsequence: there is nowhere in the TUI to rename a model or change its price.", got)
	}
	for i, it := range h.items() {
		if it.id == "edit" {
			h.sel = i
		}
	}
	if r := h.enter(); r.work != nil {
		t.Fatalf("choosing Edit must open a form, not send anything: %+v", r.work)
	}
	if h.form == nil || h.form.kind != formEditModel {
		t.Fatalf("no edit form opened: %+v", h.form)
	}
	if got := h.form.get(labelModelID); got != "fast" {
		t.Errorf("the form starts with id %q, want the model's own", got)
	}
	if h.form.get(labelPriceIn) != "1.5" || h.form.get(labelPriceOut) != "7" {
		t.Errorf("the form starts with prices %q/%q, want the declared 1.5/7 so the user edits instead of retyping",
			h.form.get(labelPriceIn), h.form.get(labelPriceOut))
	}
}

func TestACoreThatCannotEditModelsOnlyLosesTheEditRow(t *testing.T) {
	h := editHub(false)
	if got := itemIDs(h); got != "default,toggle,remove" {
		t.Errorf("an old core's model actions are %s; the rest of the hub must keep working without the edit row", got)
	}
}

func TestTheCoreVerbIsNotRequiredForTheHubToOpen(t *testing.T) {
	for _, v := range hubVerbs {
		if v == "model.update" {
			t.Fatal("model.update is in hubVerbs: a core without it would lose the whole hub, not just the edit row")
		}
	}
	if err := requireHubVerbs(&driver.Hello{Implemented: append([]string{}, hubVerbs...)}); err != nil {
		t.Errorf("a core without model.update is refused: %v", err)
	}
}

func submitEdit(t *testing.T, mutate func(f *hubForm)) (hubWork, string) {
	t.Helper()
	f := newEditModelForm(driver.ModelRow{Provider: "acme", ID: "fast", Price: &driver.ModelPrice{In: 1.5, Out: 7}})
	mutate(f)
	return f.submit()
}

func setField(f *hubForm, label, v string) {
	for i := range f.fields {
		if f.fields[i].label == label {
			f.fields[i].value = v
		}
	}
}

func TestTheEditFormSendsOnlyWhatChanged(t *testing.T) {
	// Only the id.
	w, msg := submitEdit(t, func(f *hubForm) { setField(f, labelModelID, "faster") })
	if msg != "" || w.NewID == nil || *w.NewID != "faster" || w.In != nil || w.NoPrice || w.Ref != "acme/fast" {
		t.Errorf("rename only: %+v %q", w, msg)
	}
	// Only the price.
	w, msg = submitEdit(t, func(f *hubForm) { setField(f, labelPriceIn, "2"); setField(f, labelPriceOut, "9") })
	if msg != "" || w.NewID != nil || w.In == nil || *w.In != 2 || *w.Out != 9 {
		t.Errorf("price only: %+v %q", w, msg)
	}
	// Both.
	w, msg = submitEdit(t, func(f *hubForm) {
		setField(f, labelModelID, "faster")
		setField(f, labelPriceIn, "0")
		setField(f, labelPriceOut, "0")
	})
	if msg != "" || w.NewID == nil || w.In == nil || *w.In != 0 || *w.Out != 0 {
		t.Errorf("both, with a zero price (a real claim): %+v %q", w, msg)
	}
	// Emptying both prices drops the declared one.
	w, msg = submitEdit(t, func(f *hubForm) { setField(f, labelPriceIn, ""); setField(f, labelPriceOut, "") })
	if msg != "" || !w.NoPrice || w.In != nil {
		t.Errorf("clearing the price: %+v %q", w, msg)
	}
}

func TestTheEditFormRefusesWhatIsWrongNamingTheField(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate func(f *hubForm)
		want   string
	}{
		"nothing changed": {func(f *hubForm) {}, "nothing changed"},
		"1.50 is 1.5":     {func(f *hubForm) { setField(f, labelPriceIn, "1.50") }, "nothing changed"},
		"half a price":    {func(f *hubForm) { setField(f, labelPriceOut, "") }, "both prices or neither"},
		"negative":        {func(f *hubForm) { setField(f, labelPriceIn, "-1") }, "Price in"},
		"not a number":    {func(f *hubForm) { setField(f, labelPriceIn, "abc") }, "Price in"},
		"empty id":        {func(f *hubForm) { setField(f, labelModelID, "") }, "Model id is required"},
	} {
		t.Run(name, func(t *testing.T) {
			w, msg := submitEdit(t, tc.mutate)
			if msg == "" || !strings.Contains(msg, tc.want) || w.Op == opUpdateModel {
				t.Errorf("got work=%+v msg=%q; want a refusal mentioning %q", w, msg, tc.want)
			}
		})
	}
}

// editCore records the update it was asked for and answers like the real core.
type editCore struct {
	modelCore
	mu2  sync.Mutex
	got  *driver.ModelUpdateParams
	fail error
}

func (c *editCore) SubmitModelUpdate(_ context.Context, p driver.ModelUpdateParams) (*driver.ModelUpdateResult, error) {
	c.mu2.Lock()
	defer c.mu2.Unlock()
	c.got = &p
	if c.fail != nil {
		return nil, c.fail
	}
	prov, id, _ := strings.Cut(p.Ref, "/")
	res := &driver.ModelUpdateResult{Provider: prov, Model: id, Was: id, Changed: true}
	if p.NewID != nil {
		res.Model = *p.NewID
	}
	return res, nil
}

func TestTheWorkerSendsTheEditAndSaysWhatHappened(t *testing.T) {
	c := &editCore{}
	c.models = []driver.ModelRow{{Provider: "acme", ID: "faster", Enabled: true}}
	id, in, out := "faster", 2.0, 9.0
	o := runHubWork(context.Background(), c, hubWork{Op: opUpdateModel, Ref: "acme/fast", NewID: &id, In: &in, Out: &out})
	if !o.ok || o.nav != navModels || o.prov != "acme" {
		t.Fatalf("outcome %+v; the hub must go back to the provider's models", o)
	}
	if c.got == nil || c.got.Ref != "acme/fast" || c.got.NewID == nil || *c.got.NewID != "faster" || *c.got.In != 2 || *c.got.Out != 9 {
		t.Errorf("the core was asked %+v", c.got)
	}
	if !strings.Contains(o.notice, "fast is now faster") || !strings.Contains(o.notice, "price updated") {
		t.Errorf("notice %q must say what changed", o.notice)
	}
	if !o.hasData {
		t.Error("the screen must show what the core now holds, not what the loop hopes it holds")
	}
}

func TestAFailedEditIsShownAndKeepsTheForm(t *testing.T) {
	c := &editCore{fail: errors.New(`provider "acme" already offers a model called "faster"`)}
	id := "faster"
	o := runHubWork(context.Background(), c, hubWork{Op: opUpdateModel, Ref: "acme/fast", NewID: &id})
	if o.ok || !strings.Contains(o.notice, "already offers") {
		t.Errorf("outcome %+v; a refused edit must surface the core's reason and not close the form", o)
	}
}

func TestTheEditorWorksOutFromHelloWhetherTheCoreCanEdit(t *testing.T) {
	with := &editCore{}
	with.models = []driver.ModelRow{{Provider: "a", ID: "m"}}
	d, err := readHubData(context.Background(), helloCore{with, append(append([]string{}, hubVerbs...), "model.update")})
	if err != nil || !d.canEditModels {
		t.Errorf("a core that implements model.update: canEditModels=%v err=%v", d.canEditModels, err)
	}
	d, err = readHubData(context.Background(), helloCore{with, append([]string{}, hubVerbs...)})
	if err != nil || d.canEditModels {
		t.Errorf("a core without it: canEditModels=%v err=%v", d.canEditModels, err)
	}
}

// helloCore is a core that implements exactly the listed verbs.
type helloCore struct {
	*editCore
	verbs []string
}

func (h helloCore) Hello() *driver.Hello { return &driver.Hello{Implemented: h.verbs} }
