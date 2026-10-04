package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/driver"
)

// This file is the worker half of /provider: the round-trips to the core, run off the
// loop so a slow core (a provider's /models endpoint can take seconds) never freezes
// the screen. One rule governs every message built here: an API key travels in one
// direction only (form -> core), and every error that comes back is scrubbed of it
// before it can reach the banner.

// hubOp is one thing the hub can ask the core to do.
type hubOp int

const (
	opAdd         hubOp = iota // register a provider, then get its models
	opUpdate                   // change URL / key / key variable
	opAddModels                // add model ids by hand
	opDiscover                 // fetch the model list from the provider's API
	opDefault                  // choose the default model
	opToggle                   // enable / disable one model
	opRemoveModel              // delete one model
	opRemove                   // delete a provider and its models
)

// hubWork is one finished request from the hub. Key is the only secret in it.
type hubWork struct {
	Op      hubOp
	Name    string // provider
	BaseURL string
	EnvName string
	Key     string
	Ref     string   // model ref ("provider/id")
	Models  []string // model ids to add by hand
	In, Out *float64 // prices for the models above
	On      bool     // opToggle: the state to set
	Close   bool     // leave the hub when this succeeds
}

// hubCore is everything the hub needs from the core. *driver.NDJSONDriver satisfies
// it; tests substitute a fake.
type hubCore interface {
	Hello() *driver.Hello
	SubmitProviderAdd(ctx context.Context, p driver.ProviderAddParams) (*driver.ProviderAddResult, error)
	SubmitProviderUpdate(ctx context.Context, p driver.ProviderUpdateParams) (*driver.ProviderAddResult, error)
	SubmitProviderRemove(ctx context.Context, name string) (*driver.ProviderRemoveResult, error)
	SubmitProviderList(ctx context.Context) (*driver.ProviderListResult, error)
	SubmitModelList(ctx context.Context) (*driver.ModelListResult, error)
	SubmitModelAdd(ctx context.Context, p driver.ModelAddParams) (*driver.ProviderAddResult, error)
	SubmitModelEnable(ctx context.Context, ref string, on bool) (*driver.ModelEnableResult, error)
	SubmitModelDiscover(ctx context.Context, provider string) (*driver.ModelDiscoverResult, error)
	SubmitModelRemove(ctx context.Context, ref string) (*driver.ModelRemoveResult, error)
	SubmitModelDefault(ctx context.Context, ref string) (*driver.ModelDefaultResult, error)
}

var _ hubCore = (*driver.NDJSONDriver)(nil)

// hubVerbs are the verbs the hub needs implemented, gated together: a core that stores
// keys but cannot discover models would leave the user with a provider and nothing to
// chat with, and the cure is the same one rebuild.
var hubVerbs = []string{
	"provider.add", "provider.update", "provider.remove", "provider.list",
	"model.list", "model.add", "model.enable", "model.disable",
	"model.discover", "model.remove", "model.default", "chat.send",
}

const rebuildRemedy = "rebuild it from this repository with: git pull, then cd core and go build -o ../arxi ./cmd/arxi " +
	"(on Windows: -o ..\\arxi.exe), and point ARXI_BIN at it"

// requireHubVerbs decides from the hello whether this core can run the hub and chat.
// It checks the implemented list, not the declared one: a build can declare a verb it
// never wired, and that answers not_implemented forever.
func requireHubVerbs(hello *driver.Hello) error {
	if hello == nil {
		return fmt.Errorf("cmd/arxi-tui/hub_work.go: no hello to gate on; the handshake must complete first")
	}
	have := map[string]bool{}
	for _, t := range hello.Implemented {
		have[t] = true
	}
	var missing []string
	for _, v := range hubVerbs {
		if !have[v] {
			missing = append(missing, v)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("this arxi core is too old (it lacks %s); %s", strings.Join(missing, ", "), rebuildRemedy)
	}
	return nil
}

// hubNav says where the hub goes after a successful request.
type hubNav int

const (
	navStay      hubNav = iota // leave the level alone
	navProviders               // back to the provider list
	navActions                 // the provider's actions
	navModels                  // the provider's model list
	navModelList               // the model list stays, minus the acted-on row
)

// hubOutcome is the worker's one message back to the loop.
type hubOutcome struct {
	notice  string
	data    hubData
	hasData bool // data is a fresh read (an empty one is valid)
	opened  bool // the answer to an open request: show the hub
	ok      bool // the request went through: drop the form
	nav     hubNav
	prov    string
	closeUI bool
}

// scrub removes the secret from a message that is about to be shown. The core is
// tested never to echo a key, but the transport and the OS wrap errors we do not
// control; this is the last line of defence and it is cheap.
func scrub(msg, secret string) string {
	if secret == "" {
		return msg
	}
	return strings.ReplaceAll(msg, secret, "[key hidden]")
}

// readHubData reads the three things the screens show: providers, models, default.
func readHubData(ctx context.Context, core hubCore) (hubData, error) {
	var d hubData
	pl, err := core.SubmitProviderList(ctx)
	if err != nil {
		return d, fmt.Errorf("reading providers failed: %w", err)
	}
	ml, err := core.SubmitModelList(ctx)
	if err != nil {
		return d, fmt.Errorf("reading models failed: %w", err)
	}
	d.providers, d.models = pl.Providers, ml.Models
	if df, err := core.SubmitModelDefault(ctx, ""); err == nil && df != nil {
		d.def = df.Default
	}
	return d, nil
}

// startHubOpen reads the core's state and then asks the loop to show the hub.
func startHubOpen(ctx context.Context, core hubCore, done chan<- hubOutcome) {
	go func() {
		d, err := readHubData(ctx, core)
		if err != nil {
			done <- hubOutcome{notice: err.Error()}
			return
		}
		done <- hubOutcome{data: d, hasData: true, opened: true}
	}()
}

// startHubWork runs one request on the worker.
func startHubWork(ctx context.Context, core hubCore, w hubWork, done chan<- hubOutcome) {
	go func() { done <- runHubWork(ctx, core, w) }()
}

// runHubWork performs one request and reads the state back, so the screen always shows
// what the core now holds, not what the loop hopes it holds.
func runHubWork(ctx context.Context, core hubCore, w hubWork) hubOutcome {
	fail := func(err error) hubOutcome {
		return hubOutcome{notice: scrub(err.Error(), w.Key)}
	}
	out := hubOutcome{ok: true, closeUI: w.Close}

	switch w.Op {
	case opAdd:
		res, err := core.SubmitProviderAdd(ctx, driver.ProviderAddParams{
			Name: w.Name, BaseURL: w.BaseURL, APIKeyEnv: w.EnvName, APIKey: w.Key,
		})
		if err != nil {
			return fail(err)
		}
		out.prov, out.nav = w.Name, navModels
		msg := "✓ " + w.Name + " added"
		if res != nil && res.KeyStored {
			msg = "✓ " + w.Name + " added, key stored"
		}
		if len(w.Models) > 0 {
			added, errs := addModels(ctx, core, w)
			msg += ", " + plural(added, "model", "models")
			if len(errs) > 0 {
				msg += "; not added: " + scrub(strings.Join(errs, "; "), w.Key)
			}
		} else if res != nil && len(res.Models) > 0 {
			msg += ", " + plural(len(res.Models), "model", "models")
		} else {
			n, err := core.SubmitModelDiscover(ctx, w.Name)
			if err != nil {
				msg += ", but fetching its models failed: " + scrub(err.Error(), w.Key) +
					". Open it and choose Add models by hand, or Fetch models once it is fixed"
				out.nav = navActions
			} else {
				msg += ", " + plural(n.Found, "model", "models") + " found"
			}
		}
		out.notice = msg

	case opUpdate:
		if _, err := core.SubmitProviderUpdate(ctx, driver.ProviderUpdateParams{
			Name: w.Name, BaseURL: w.BaseURL, APIKeyEnv: w.EnvName, APIKey: w.Key,
		}); err != nil {
			return fail(err)
		}
		out.prov, out.nav, out.notice = w.Name, navActions, "✓ "+w.Name+" updated"

	case opAddModels:
		added, errs := addModels(ctx, core, w)
		if added == 0 {
			return hubOutcome{notice: "no model was added: " + strings.Join(errs, "; ")}
		}
		out.prov, out.nav = w.Name, navModels
		out.notice = "✓ " + plural(added, "model", "models") + " added to " + w.Name
		if len(errs) > 0 {
			out.notice += "; not added: " + strings.Join(errs, "; ")
		}

	case opDiscover:
		n, err := core.SubmitModelDiscover(ctx, w.Name)
		if err != nil {
			return fail(err)
		}
		out.prov, out.nav = w.Name, navModels
		out.notice = fmt.Sprintf("✓ %s: %s found, %d new", w.Name, plural(n.Found, "model", "models"), n.Added)

	case opDefault:
		res, err := core.SubmitModelDefault(ctx, w.Ref)
		if err != nil {
			return fail(err)
		}
		out.prov, out.nav = res.Provider, navModelList
		out.notice = "✓ chat model: " + res.Model + " [" + res.Provider + "]"

	case opToggle:
		res, err := core.SubmitModelEnable(ctx, w.Ref, w.On)
		if err != nil {
			return fail(err)
		}
		out.nav = navModelList
		state := "disabled"
		if res.Enabled {
			state = "enabled"
		}
		out.notice = "✓ " + res.Model + " " + state

	case opRemoveModel:
		res, err := core.SubmitModelRemove(ctx, w.Ref)
		if err != nil {
			return fail(err)
		}
		out.nav = navModelList
		out.notice = "✓ " + res.Model + " removed"

	case opRemove:
		if _, err := core.SubmitProviderRemove(ctx, w.Name); err != nil {
			return fail(err)
		}
		out.nav = navProviders
		out.notice = "✓ " + w.Name + " removed"

	default:
		return hubOutcome{notice: "unknown request"}
	}

	d, err := readHubData(ctx, core)
	if err != nil {
		out.notice += " (the screen could not refresh: " + err.Error() + ")"
		return out
	}
	// Models typed by hand are the user's explicit choice: when nothing is chosen yet,
	// the first becomes the chat model so a first message works at once. A discovered
	// list is left alone -- picking one of two hundred silently would be a guess.
	if d.def == "" && len(w.Models) > 0 && (w.Op == opAdd || w.Op == opAddModels) {
		if res, err := core.SubmitModelDefault(ctx, w.Name+"/"+w.Models[0]); err == nil {
			d.def = res.Default
			out.notice += "; chat model: " + w.Models[0]
		}
	}
	if d.def == "" && len(d.enabledModels()) > 0 {
		out.notice += ". Choose the model to chat with: /models"
	}
	out.data, out.hasData = d, true
	return out
}

// addModels adds each id by hand and returns how many went in and the failures.
func addModels(ctx context.Context, core hubCore, w hubWork) (int, []string) {
	added := 0
	var errs []string
	for _, id := range w.Models {
		if _, err := core.SubmitModelAdd(ctx, driver.ModelAddParams{Provider: w.Name, Model: id, In: w.In, Out: w.Out}); err != nil {
			errs = append(errs, id+": "+err.Error())
			continue
		}
		added++
	}
	return added, errs
}

// apply folds a worker outcome into the hub and says whether the hub should close.
func (h *providerHub) apply(o hubOutcome) (notice string, closeHub bool) {
	h.working = ""
	if o.hasData {
		h.setData(o.data)
	}
	if o.ok {
		if h.form != nil {
			h.wipe()
		}
		if o.closeUI {
			return o.notice, true
		}
		switch o.nav {
		case navProviders:
			h.setLevel(lvProviders)
		case navActions:
			h.prov = o.prov
			h.setLevel(lvActions)
		case navModels:
			h.prov = o.prov
			h.setLevel(lvModels)
		case navModelList:
			if h.level == lvModelActions {
				h.setLevel(lvModels)
			}
		}
	}
	return o.notice, false
}
