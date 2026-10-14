package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// A model discovered from a service that does not report its window has none, and the bar
// could only say "?". Setting it must be possible from the screen that lists the models,
// for a model that already exists, without removing and re-adding it.

func windowHub(window int) *providerHub {
	d := hubData{
		providers: []driver.ProviderRow{{Name: "apinex2", BaseURL: "https://api.apinex.bond", Models: 2}},
		models: []driver.ModelRow{
			{Provider: "apinex2", ID: "free/other", Enabled: true},
			{Provider: "apinex2", ID: "free/mimo-v2.6-pro", Enabled: true, ContextWindow: window},
		},
	}
	h, _ := newHub(d, hubOpenProviders)
	h.prov, h.model = "apinex2", "apinex2/free/mimo-v2.6-pro"
	h.setLevel(lvModelActions)
	return h
}

func itemIDs(h *providerHub) []string {
	var ids []string
	for _, it := range h.items() {
		ids = append(ids, it.id)
	}
	return ids
}

func TestTheModelMenuOffersToSetTheContextWindow(t *testing.T) {
	h := windowHub(0)
	found := false
	for _, it := range h.items() {
		if it.id == "window" {
			found = true
			if it.status != "not set" {
				t.Errorf("a model with no window says %q, want \"not set\"", it.status)
			}
		}
	}
	if !found {
		t.Fatalf("no \"Set context window\" row in %v", itemIDs(h))
	}
	if got := windowHub(270_000).items(); !strings.Contains(statusOf(got, "window"), "270k") {
		t.Errorf("a known window must be shown on its row, got %q", statusOf(got, "window"))
	}
}

func statusOf(items []hubItem, id string) string {
	for _, it := range items {
		if it.id == id {
			return it.status
		}
	}
	return ""
}

func TestChoosingSetContextWindowOpensAFormAndSubmitsTheWork(t *testing.T) {
	h := windowHub(0)
	for i, it := range h.items() {
		if it.id == "window" {
			h.sel = i
		}
	}
	routeHubKey(h, term.Key{Type: term.KeyEnter})
	if h.level != lvForm || h.form == nil || h.form.kind != formWindow {
		t.Fatalf("Enter on the row should open the window form, level=%v form=%+v", h.level, h.form)
	}
	typeInto(h, "270k")
	w, msg := h.form.submit()
	if msg != "" {
		t.Fatal(msg)
	}
	if w.Op != opSetWindow || w.Ref != "apinex2/free/mimo-v2.6-pro" || w.Window != 270_000 {
		t.Fatalf("work = %+v", w)
	}
}

func TestTheWindowFormStartsFromTheCurrentValueAndRefusesNonsense(t *testing.T) {
	f := newWindowForm("p/m", 128_000)
	if got := f.get(windowFieldLabel); got != "128000" {
		t.Errorf("form should start from the current window, has %q", got)
	}
	for _, bad := range []string{"", "lots", "10", "5G"} {
		f := newWindowForm("p/m", 0)
		f.fields[0].value = bad
		if w, msg := f.submit(); msg == "" {
			t.Errorf("%q must be refused, got work %+v", bad, w)
		}
	}
}

type windowCore struct {
	modelCore
	gotRef    string
	gotWindow int
	err       error
}

func (c *windowCore) SubmitModelUpdate(_ context.Context, ref string, n int) (*driver.ModelUpdateResult, error) {
	c.gotRef, c.gotWindow = ref, n
	if c.err != nil {
		return nil, c.err
	}
	prov, id, _ := strings.Cut(ref, "/")
	return &driver.ModelUpdateResult{Provider: prov, Model: id, ContextWindow: n}, nil
}

func TestTheWorkerSendsTheWindowToTheCoreAndSaysSo(t *testing.T) {
	c := &windowCore{}
	out := runHubWork(context.Background(), c, hubWork{Op: opSetWindow, Ref: "apinex2/free/mimo-v2.6-pro", Window: 270_000})
	if c.gotRef != "apinex2/free/mimo-v2.6-pro" || c.gotWindow != 270_000 {
		t.Fatalf("core got %q %d", c.gotRef, c.gotWindow)
	}
	if !strings.Contains(out.notice, "270k") || !strings.Contains(out.notice, "✓") {
		t.Errorf("notice = %q", out.notice)
	}
	if out.nav != navModelList {
		t.Errorf("after saving, the model list should show, nav=%v", out.nav)
	}

	c = &windowCore{err: errors.New("core says no")}
	out = runHubWork(context.Background(), c, hubWork{Op: opSetWindow, Ref: "a/b", Window: 5000})
	if strings.Contains(out.notice, "✓") || !strings.Contains(out.notice, "core says no") {
		t.Errorf("a refusal must be shown, not hidden behind a tick: %q", out.notice)
	}
}

func TestTheDetailExplainsWhyNoWindowAndHowToSetIt(t *testing.T) {
	var st fold.State
	h := windowHub(0)
	h.publish(&st)
	if !strings.Contains(st.HubDetail, "not known") || !strings.Contains(st.HubDetail, "Set context window") {
		t.Errorf("detail should explain, got %q", st.HubDetail)
	}
	st = fold.State{}
	h = windowHub(270_000)
	h.publish(&st)
	if !strings.Contains(st.HubDetail, "270k") {
		t.Errorf("detail should show the window, got %q", st.HubDetail)
	}
}

// TestSettingTheWindowAgainstTheRealCoreReachesTheStatusBar is the whole story of the
// report: a service that does not report windows, a model chosen from it, the bar saying
// "ctx ?", the window typed once in the providers screen, and the bar then saying
// "34k/270k". The core is the real binary; only the service is faked.
func TestSettingTheWindowAgainstTheRealCoreReachesTheStatusBar(t *testing.T) {
	bin := buildCore(t)
	work := t.TempDir()
	t.Setenv("ARXI_SECRETS_DIR", filepath.Join(work, "secrets"))
	t.Setenv("ARXI_PROVIDERS_DIR", filepath.Join(work, "providers"))
	t.Setenv("ARXI_BIN", bin)
	t.Setenv("HOME", work)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(work, "cfg"))
	t.Setenv("ARXI_CONTEXT_WINDOW", "")
	t.Chdir(work)

	const key = "sk-test-0123456789abcdefghijklmnop"
	srv, _ := fakeLLM(t, key) // lists models without any window, like the real service
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	drv, _, err := openServeDriver(ctx, bin)
	if err != nil {
		t.Fatal(err)
	}
	defer drv.Close()
	hub := drv.(*serveDriver).Hub()

	out := runHubWork(ctx, hub, hubWork{Op: opAdd, Name: "apinex2", BaseURL: srv.URL + "/v1", Key: key})
	if !out.ok {
		t.Fatalf("add: %s", out.notice)
	}
	ref := "apinex2/smart-2"
	if got := out.data.windowOf(ref); got != 0 {
		t.Fatalf("the service reports no window, yet arxi believes %d", got)
	}
	if bar := statusUsage(34_000, 100, 10, out.data.windowOf(ref), autoCompactSetting{}); !strings.Contains(bar, "ctx 34k/? ") {
		t.Fatalf("before: %q", bar)
	}

	out = runHubWork(ctx, hub, hubWork{Op: opSetWindow, Ref: ref, Window: 270_000})
	if !out.ok || !strings.Contains(out.notice, "✓") {
		t.Fatalf("set window: %q", out.notice)
	}
	if got := out.data.windowOf(ref); got != 270_000 {
		t.Fatalf("after setting, the refreshed read says %d", got)
	}
	if bar := statusUsage(34_000, 100, 10, out.data.windowOf(ref), autoCompactSetting{}); !strings.HasPrefix(bar, "ctx 34k/270k (12%)") {
		t.Fatalf("after: %q", bar)
	}

	// a figure no model has is refused by the core, and nothing is stored
	bad := runHubWork(ctx, hub, hubWork{Op: opSetWindow, Ref: ref, Window: 7})
	if bad.ok || strings.Contains(bad.notice, "✓") {
		t.Fatalf("a 7-token window was accepted: %q", bad.notice)
	}
	// a window set by hand survives discovering again (the service still reports none)
	runHubWork(ctx, hub, hubWork{Op: opDiscover, Name: "apinex2"})
	d, err := readHubData(ctx, hub)
	if err != nil {
		t.Fatal(err)
	}
	if got := d.windowOf(ref); got != 270_000 {
		t.Fatalf("discovering again wiped the window: %d", got)
	}
}
