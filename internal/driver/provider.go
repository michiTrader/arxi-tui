package driver

import (
	"context"
	"encoding/json"
	"fmt"
)

// This file is the TUI's driver half of Block K2: the provider/model
// management verbs, the siblings of SubmitRunStart. They are the honest
// TUI-first path the K2 proposal signs -- the TUI drives providers over the
// same NDJSON protocol it drives runs over, rather than shelling out to the
// `arxi provider add` CLI and parsing its human-facing stdout. The wire schema
// is read from arxi source (internal/model, internal/modelstore,
// internal/surface), not guessed; see docs/DESIGN-BLOCK-K2-PROVIDERS.md.
//
// Each method keeps the contract SubmitRunStart established: a map of only the
// parameters the caller actually chose (an optional unset is omitted, not sent
// as ""), ok:false surfaced as a *Refusal rather than a zero result that reads
// as success, and a fail-loud guard on an ok result that names nothing -- an
// unverifiable success is worse than a refusal, which at least says so.

// ProviderModel is one callable model a provider offers and whether it may be
// used. It mirrors arxi's model.Model (id + enabled), the shape model.New
// stamps on registration (the first model enabled, the rest disabled).
type ProviderModel struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

// ProviderAddParams are the wire parameters of a provider.add request. Only
// Name is required; the core's model.New fills BaseURL from its known-provider
// table when omitted and refuses an unknown provider that supplies none, so the
// client sends only what the user typed and lets the core raise its own
// documented refusal rather than guessing a default endpoint.
//
// APIKeyEnv is the NAME of an environment variable, never a key -- the §20.1
// invariant arxi enforces in validateKeyEnv (it refuses a value that looks like
// a secret). The TUI carries only the name across the wire; the single
// enforcement site stays on the arxi side so a client that mistakenly sends a
// key is refused by the core, not by a second validator the TUI would have to
// keep in sync.
type ProviderAddParams struct {
	Name      string
	BaseURL   string
	APIKeyEnv string
}

// ProviderAddResult is the registered provider snapshot the core returns:
// arxi's model.Provider (name, protocol, base_url, api_key_env, models).
type ProviderAddResult struct {
	Name      string          `json:"name"`
	Protocol  string          `json:"protocol"`
	BaseURL   string          `json:"base_url"`
	APIKeyEnv string          `json:"api_key_env"`
	Models    []ProviderModel `json:"models"`
}

// SubmitProviderAdd sends a provider.add request and returns the registered
// provider. An empty name is refused locally -- the core's model.New refuses it
// too (validateName), but borrowing that round-trip would name the failure
// after a send the client never meant to make, the same reason SubmitRunCancel
// refuses an empty run id rather than letting the core answer for it.
func (d *NDJSONDriver) SubmitProviderAdd(ctx context.Context, p ProviderAddParams) (*ProviderAddResult, error) {
	if p.Name == "" {
		return nil, fmt.Errorf("ndjson: provider.add needs a provider name; " +
			"an unnamed provider addresses no record to write")
	}
	params := map[string]any{"name": p.Name}
	if p.BaseURL != "" {
		params["base_url"] = p.BaseURL
	}
	if p.APIKeyEnv != "" {
		params["api_key_env"] = p.APIKeyEnv
	}

	req := protoRequest{ID: "provider-add", Type: "provider.add", Params: params}

	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.enc.Encode(req); err != nil {
		return nil, fmt.Errorf("ndjson: send provider.add: %w", err)
	}
	resp, err := d.readResponse(ctx)
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, resp.refusal("provider.add")
	}

	var result ProviderAddResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("ndjson: decode provider.add result: %w", err)
	}
	// A provider.add that answered ok but named no provider is unverifiable: the
	// name is how model list and model enable reach it, so an empty one is a
	// silent dead end rather than a registered provider the user can use.
	if result.Name == "" {
		return nil, fmt.Errorf("ndjson: provider.add returned ok with no provider " +
			"name; a registered provider the host cannot name is unreachable by " +
			"model.list and model.enable")
	}
	return &result, nil
}

// ModelRow is one row of model.list: a model, which provider owns it, and
// whether it is enabled. The provider is carried per row because model.list is
// the single read for both "what providers" and "what models" (a provider with
// models answers both), the K2 proposal's choice over a separate provider.list.
type ModelRow struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	Enabled  bool   `json:"enabled"`
}

// ModelListResult wraps the rows model.list returns. An empty slice is a valid
// answer -- no provider has been registered yet -- so there is no fail-loud
// guard here the way there is on provider.add: nothing is named because there
// is honestly nothing to name.
type ModelListResult struct {
	Models []ModelRow `json:"models"`
}

// SubmitModelList sends a model.list request and returns the rows. It takes no
// parameters and mutates nothing (arxi marks it Idempotent), so it is safe to
// call on every repaint a provider screen needs; this increment calls it only
// from the /model list and /provider list commands.
func (d *NDJSONDriver) SubmitModelList(ctx context.Context) (*ModelListResult, error) {
	req := protoRequest{ID: "model-list", Type: "model.list"}

	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.enc.Encode(req); err != nil {
		return nil, fmt.Errorf("ndjson: send model.list: %w", err)
	}
	resp, err := d.readResponse(ctx)
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, resp.refusal("model.list")
	}

	var result ModelListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("ndjson: decode model.list result: %w", err)
	}
	return &result, nil
}

// ModelEnableResult is the core's answer to model.enable/disable: which model
// was addressed, its resulting state, and whether anything changed. Changed is
// carried because arxi's SetEnabled returns it so the command can say "already
// enabled" instead of reporting a success that did nothing -- a command that
// reports a change it did not make is how a user concludes the setting is
// broken.
type ModelEnableResult struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Enabled  bool   `json:"enabled"`
	Changed  bool   `json:"changed"`
}

// SubmitModelEnable sends model.enable (on=true) or model.disable (on=false)
// for a model ref ("provider/id" or a bare "id"). The two verbs share this one
// method because they are one toggle; the verb string is chosen from on so a
// refusal is addressable to the act the caller requested. An empty ref is
// refused locally -- the core's store.Owner refuses it too, but naming it here
// keeps the failure at the call site rather than borrowing a not_found for a
// send the client never meant to make.
func (d *NDJSONDriver) SubmitModelEnable(ctx context.Context, ref string, on bool) (*ModelEnableResult, error) {
	verb := "model.enable"
	if !on {
		verb = "model.disable"
	}
	if ref == "" {
		return nil, fmt.Errorf("ndjson: %s needs a model ref; an empty ref "+
			"addresses no model to toggle", verb)
	}

	req := protoRequest{ID: "model-enable", Type: verb, Params: map[string]any{"model": ref}}

	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.enc.Encode(req); err != nil {
		return nil, fmt.Errorf("ndjson: send %s: %w", verb, err)
	}
	resp, err := d.readResponse(ctx)
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, resp.refusal(verb)
	}

	var result ModelEnableResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("ndjson: decode %s result: %w", verb, err)
	}
	// An enable that answered ok but named no model is unverifiable: the caller
	// cannot confirm which model it toggled, so the "changed" answer is
	// meaningless. Fail loud rather than report a toggle on nothing.
	if result.Model == "" {
		return nil, fmt.Errorf("ndjson: %s returned ok with no model named; a "+
			"toggle the host cannot attribute to a model is unverifiable", verb)
	}
	return &result, nil
}
