package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/term"
)

func imagesHub(takes bool) *providerHub {
	d := hubData{
		providers: []driver.ProviderRow{{Name: "p", BaseURL: "https://x", Models: 2}},
		models: []driver.ModelRow{
			{Provider: "p", ID: "other", Enabled: true},
			{Provider: "p", ID: "m", Enabled: true, Images: takes},
		},
	}
	h, _ := newHub(d, hubOpenProviders)
	h.prov, h.model = "p", "p/m"
	h.setLevel(lvModelActions)
	return h
}

func TestTheModelMenuSaysWhetherTheModelTakesImages(t *testing.T) {
	if got := statusOf(imagesHub(false).items(), "images"); !strings.HasPrefix(got, "no") {
		t.Errorf("a model nobody said anything about shows %q, want no", got)
	}
	if got := statusOf(imagesHub(true).items(), "images"); !strings.HasPrefix(got, "yes") {
		t.Errorf("a model set to take images shows %q, want yes", got)
	}
	// the other model's flag must not leak onto this row
	d := imagesHub(false).data
	d.models[0].Images = true
	h, _ := newHub(d, hubOpenProviders)
	h.prov, h.model = "p", "p/m"
	h.setLevel(lvModelActions)
	if got := statusOf(h.items(), "images"); !strings.HasPrefix(got, "no") {
		t.Errorf("another model's images leaked: %q", got)
	}
}

func pressImages(h *providerHub) hubKeyResult {
	for i, it := range h.items() {
		if it.id == "images" {
			h.sel = i
		}
	}
	return routeHubKey(h, term.Key{Type: term.KeyEnter})
}

func TestEnterOnAcceptsImagesFlipsItBothWays(t *testing.T) {
	r := pressImages(imagesHub(false))
	if r.work == nil || r.work.Op != opSetImages || r.work.Ref != "p/m" || !r.work.On {
		t.Fatalf("turning on: %+v", r.work)
	}
	r = pressImages(imagesHub(true))
	if r.work == nil || r.work.Op != opSetImages || r.work.On {
		t.Fatalf("turning off: %+v", r.work)
	}
}

type imagesCore struct {
	modelCore
	gotRef string
	gotOn  bool
	err    error
}

func (c *imagesCore) SubmitModelImages(_ context.Context, ref string, on bool) (*driver.ModelUpdateResult, error) {
	c.gotRef, c.gotOn = ref, on
	if c.err != nil {
		return nil, c.err
	}
	return &driver.ModelUpdateResult{Model: ref}, nil
}

func TestTheWorkerTellsTheCoreAndSaysWhatChanged(t *testing.T) {
	c := &imagesCore{}
	out := runHubWork(context.Background(), c, hubWork{Op: opSetImages, Ref: "p/m", On: true})
	if c.gotRef != "p/m" || !c.gotOn || !strings.Contains(out.notice, "now takes images") || out.nav != navModelList {
		t.Fatalf("core got %q %v, outcome %+v", c.gotRef, c.gotOn, out)
	}
	out = runHubWork(context.Background(), c, hubWork{Op: opSetImages, Ref: "p/m", On: false})
	if !strings.Contains(out.notice, "no longer takes images") {
		t.Fatalf("notice = %q", out.notice)
	}
}

func TestAnOldCoreFailsTheImagesRowWithAnExplanation(t *testing.T) {
	c := &imagesCore{err: driver.ErrImagesUnsupported}
	out := runHubWork(context.Background(), c, hubWork{Op: opSetImages, Ref: "p/m", On: true})
	if out.ok || !strings.Contains(out.notice, "cannot take images") {
		t.Fatalf("outcome = %+v", out)
	}
	_ = errors.New
}

func TestTheModelsFormAsksAboutImagesAndSendsTheAnswer(t *testing.T) {
	for in, want := range map[string]bool{"": false, "no": false, "yes": true, "Y": true, "sí": true} {
		f := newModelsForm("p")
		f.fields[0].value = "m1"
		for i := range f.fields {
			if f.fields[i].label == imagesFieldLabel {
				f.fields[i].value = in
			}
		}
		w, msg := f.submit()
		if msg != "" {
			t.Fatalf("%q refused: %s", in, msg)
		}
		if w.Images != want {
			t.Errorf("answer %q gave Images=%v, want %v", in, w.Images, want)
		}
	}
	f := newModelsForm("p")
	f.fields[0].value = "m1"
	f.fields[len(f.fields)-1].value = "maybe"
	if _, msg := f.submit(); !strings.Contains(msg, "yes or no") || !strings.Contains(msg, "Accepts images") {
		t.Errorf("nonsense answer: %q", msg)
	}
}

type addCore struct {
	modelCore
	added []driver.ModelAddParams
}

func (c *addCore) SubmitModelAdd(_ context.Context, p driver.ModelAddParams) (*driver.ProviderAddResult, error) {
	c.added = append(c.added, p)
	return &driver.ProviderAddResult{}, nil
}

func TestModelsAddedByHandCarryTheImagesAnswerToTheCore(t *testing.T) {
	c := &addCore{}
	runHubWork(context.Background(), c, hubWork{Op: opAddModels, Name: "p", Models: []string{"a", "b"}, Images: true})
	if len(c.added) != 2 || !c.added[0].Images || !c.added[1].Images {
		t.Fatalf("core got %+v", c.added)
	}
}
