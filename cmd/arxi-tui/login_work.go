package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/driver"
)

// This file is the worker half of /login: the round-trips to the core, run off the
// loop so a slow core never freezes the screen. The one rule here is that the API key
// travels in exactly one direction (wizard -> core) and every message that comes back
// is scrubbed of it before it reaches the banner.

// loginOutcome is the worker's one message back to the loop.
type loginOutcome struct {
	notice  string
	rows    []driver.ProviderRow
	hasRows bool // rows is a fresh provider.list answer (an empty list is valid)
	open    bool // the answer to the open request: show the wizard
	saved   bool // the save went through: wipe the form and return to the list
}

// loginOpenRefusal says why this connection cannot run /login, or "" when it can.
func loginOpenRefusal(drv Driver) string {
	pm, ok := drv.(providerManager)
	if !ok {
		return noLiveCoreNotice
	}
	if _, ok := drv.(loginManager); !ok {
		return noLiveCoreNotice
	}
	if err := requireLoginVerbs(pm.Hello()); err != nil {
		return "/login: " + err.Error()
	}
	return ""
}

// startLoginOpen reads the credential state, then asks the loop to show the wizard.
func startLoginOpen(ctx context.Context, lm loginManager, done chan<- loginOutcome) {
	go func() {
		res, err := lm.SubmitProviderList(ctx)
		if err != nil {
			done <- loginOutcome{notice: "/login: reading providers failed: " + err.Error()}
			return
		}
		done <- loginOutcome{open: true, rows: res.Providers, hasRows: true}
	}()
}

// startLoginSave runs one finished form on the worker.
func startLoginSave(ctx context.Context, lm loginManager, act loginAction, done chan<- loginOutcome) {
	go func() { done <- runLoginWork(ctx, lm, act) }()
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

// runLoginWork performs the save: add or key, then the optional model, then a
// refreshed provider list so the status column shows the new state.
func runLoginWork(ctx context.Context, lm loginManager, act loginAction) loginOutcome {
	fail := func(err error) loginOutcome {
		return loginOutcome{notice: "/login: " + scrub(err.Error(), act.Key)}
	}

	var notice string
	if act.Existing {
		if _, err := lm.SubmitProviderKey(ctx, act.Name, act.Key); err != nil {
			return fail(err)
		}
		notice = "✓ key stored for " + act.Name
	} else {
		res, err := lm.SubmitProviderAdd(ctx, driver.ProviderAddParams{
			Name: act.Name, BaseURL: act.BaseURL, APIKeyEnv: act.EnvName, APIKey: act.Key,
		})
		if err != nil {
			return fail(err)
		}
		if res != nil && res.KeyStored {
			notice = "✓ key stored for " + act.Name
		} else {
			notice = "✓ " + act.Name + " configured"
		}
	}

	out := loginOutcome{saved: true}
	if act.Model != "" {
		if _, err := lm.SubmitModelAdd(ctx, driver.ModelAddParams{
			Provider: act.Name, Model: act.Model, In: act.In, Out: act.Out,
		}); err != nil {
			notice = fmt.Sprintf("%s, but the model %q was not added: %s",
				notice, act.Model, scrub(err.Error(), act.Key))
		}
	}
	out.notice = notice

	if list, err := lm.SubmitProviderList(ctx); err == nil {
		out.rows, out.hasRows = list.Providers, true
	}
	return out
}

// finishSave drops the form (and with it the key) and returns to the provider list.
func (w *loginWizard) finishSave() {
	w.wipe()
	w.step = stepProvider
}
