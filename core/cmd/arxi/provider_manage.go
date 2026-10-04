package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/michiTrader/arxi/internal/model"
	"github.com/michiTrader/arxi/internal/modelstore"
	"github.com/michiTrader/arxi/internal/provider"
	"github.com/michiTrader/arxi/internal/secretstore"
	"github.com/michiTrader/arxi/internal/turn"
)

// This file holds what a person does to providers AFTER they exist: change the
// endpoint or key, remove one, ask it which models it serves, drop a model,
// choose the model chat uses, and chat. Like provider_ops.go it is shared by the
// CLI and the NDJSON protocol, so both do the same thing in the same order.

// providerStore opens the store without exiting, for the protocol handlers.
func providerStore() (*modelstore.Store, error) { return modelstore.Open(providerDir) }

func cleanName(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// updateProvider changes a provider's endpoint and/or credential variable and,
// when apiKey is not empty, replaces its stored key. Empty arguments leave that
// field alone; the models and their enabled flags are never touched, which is
// the reason this verb exists instead of "remove, then add again".
//
// keyEnv "none" clears the variable name (a local server needs none).
func updateProvider(name, baseURL, keyEnv, apiKey string) (model.Provider, bool, error) {
	store, err := providerStore()
	if err != nil {
		return model.Provider{}, false, err
	}
	p, err := store.Load(cleanName(name))
	if err != nil {
		return model.Provider{}, false, err
	}
	if apiKey != "" {
		if err := secretstore.CheckKey(apiKey); err != nil {
			return model.Provider{}, false, badInvocation{fmt.Errorf("provider %q was not changed: %w", p.Name, err)}
		}
	}
	if u := strings.TrimRight(strings.TrimSpace(baseURL), "/"); u != "" {
		p.BaseURL = u
		// An explicit endpoint other than the vendor's own is documented as
		// OpenAI-compatible, exactly as model.New decides at registration.
		if tableURL, _, ok := model.Known(p.Name); !ok || u != tableURL {
			p.Protocol = model.ProtocolOpenAIChatCompletions
		} else if kp, ok := model.KnownProtocol(p.Name); ok {
			p.Protocol = kp
		}
	}
	switch k := strings.TrimSpace(keyEnv); k {
	case "":
	case "none":
		p.APIKeyEnv = ""
	default:
		p.APIKeyEnv = k
	}
	if err := p.Validate(); err != nil {
		return model.Provider{}, false, badInvocation{err}
	}
	if err := store.Save(p); err != nil {
		return model.Provider{}, false, err
	}
	keyStored := false
	if apiKey != "" {
		if err := storeKey(p.Name, apiKey); err != nil {
			return model.Provider{}, false, fmt.Errorf("provider %q was updated but its key was not stored: %w", p.Name, err)
		}
		keyStored = true
	}
	return p, keyStored, nil
}

// removeProvider forgets a provider: its record, its stored key, and the
// default model if that pointed into it. A key is deleted with the provider
// because a key nobody can see in any list is a secret left lying around.
func removeProvider(name string) error {
	store, err := providerStore()
	if err != nil {
		return err
	}
	name = cleanName(name)
	if _, err := store.Load(name); err != nil {
		return err
	}
	if err := store.Remove(name); err != nil {
		return err
	}
	if secrets, err := secretsOpen(); err == nil {
		if err := secrets.Delete(name); err != nil {
			return fmt.Errorf("provider %q was removed but its stored key could not be deleted: %w", name, err)
		}
	}
	if ref, _ := store.Default(); strings.HasPrefix(ref, name+"/") {
		_ = store.ClearDefault()
	}
	return nil
}

// discovered is the result of asking a provider what it serves.
type discovered struct {
	Provider string   `json:"provider"`
	Found    int      `json:"found"`
	Added    int      `json:"added"`
	Models   []string `json:"models"`
}

// discoverTimeout bounds one listing; a listing is a single small GET.
const discoverTimeout = 30 * time.Second

// discoverModels asks the provider's endpoint for its models and adds the ones
// the store does not have yet, enabled. Existing models (and any price or
// enabled flag the user set) are left exactly as they were.
func discoverModels(ctx context.Context, name string) (discovered, error) {
	store, err := providerStore()
	if err != nil {
		return discovered{}, err
	}
	p, err := store.Load(cleanName(name))
	if err != nil {
		return discovered{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, discoverTimeout)
	defer cancel()
	c := &provider.Client{BaseURL: p.BaseURL, APIKeyEnv: p.APIKeyEnv, Provider: p.Name, Secret: secretsLookup}
	ids, err := c.ListModels(ctx, p.EffectiveProtocol())
	if err != nil {
		return discovered{}, fmt.Errorf("could not list the models of %s: %w", p.Name, err)
	}
	have := map[string]bool{}
	for _, m := range p.Models {
		have[m.ID] = true
	}
	out := discovered{Provider: p.Name, Found: len(ids), Models: ids}
	for _, id := range ids {
		if have[id] {
			continue
		}
		if err := p.AddModel(id, nil); err != nil {
			continue // an id the model package refuses is skipped, not fatal
		}
		out.Added++
	}
	if out.Added > 0 {
		if err := store.Save(p); err != nil {
			return discovered{}, err
		}
	}
	return out, nil
}

// removeModel deletes a model from the provider that owns it, and clears the
// default if it was the default.
func removeModel(ref string) (provider, id string, err error) {
	store, err := providerStore()
	if err != nil {
		return "", "", err
	}
	p, id, err := store.Owner(ref)
	if err != nil {
		return "", "", err
	}
	kept := p.Models[:0:0]
	for _, m := range p.Models {
		if m.ID != id {
			kept = append(kept, m)
		}
	}
	p.Models = kept
	if err := store.Save(p); err != nil {
		return "", "", err
	}
	if cur, _ := store.Default(); cur == p.Name+"/"+id {
		_ = store.ClearDefault()
	}
	return p.Name, id, nil
}

// defaultModel reads (ref == "") or sets the model chat uses.
func defaultModel(ref string) (string, error) {
	store, err := providerStore()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(ref) == "" {
		return store.ResolveDefault()
	}
	ps, err := store.List()
	if err != nil {
		return "", err
	}
	// Resolve, not Owner: a default must be a model chat can actually call, so
	// a disabled model is refused here with the resolver's own remedy.
	res, err := model.Resolve(ps, ref)
	if err != nil {
		return "", err
	}
	p, id, err := store.SetDefault(res.Provider + "/" + res.Model)
	if err != nil {
		return "", err
	}
	return p + "/" + id, nil
}

// chatTurn is one earlier message of the conversation, as the TUI keeps it.
type chatTurn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// maxChatHistory bounds how much of the conversation is replayed to the model.
// The TUI owns the transcript; the core only needs the recent turns, and an
// unbounded replay is an unbounded bill.
const maxChatHistory = 40

// chatTimeout bounds one chat call. Long enough for a slow reasoning model,
// short enough that a hung endpoint reports itself instead of freezing the UI.
const chatTimeout = 3 * time.Minute

// chatResult is what one chat call returns.
type chatResult struct {
	Text         string `json:"text"`
	Model        string `json:"model"`
	Provider     string `json:"provider"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

// chatSend sends one prompt, with earlier turns, to a model and returns the
// reply. It bills the provider directly; it starts no run, needs no agent and
// writes nothing to disk.
func chatSend(ctx context.Context, prompt, historyJSON, system, ref string) (chatResult, error) {
	return chatSendEffort(ctx, prompt, historyJSON, system, ref, "")
}

// chatEfforts are the thinking levels a caller may ask for. "auto" and the empty
// string both mean "send nothing and let the model decide".
var chatEfforts = map[string]bool{"minimal": true, "low": true, "medium": true, "high": true}

// normalizeEffort maps what a caller typed to what goes on the wire.
func normalizeEffort(effort string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(effort))
	if e == "" || e == "auto" {
		return "", nil
	}
	if !chatEfforts[e] {
		return "", badInvocation{fmt.Errorf("effort %q is not one of auto, minimal, low, medium, high", effort)}
	}
	return e, nil
}

// chatSendEffort is chatSend with a thinking level.
func chatSendEffort(ctx context.Context, prompt, historyJSON, system, ref, effort string) (chatResult, error) {
	level, err := normalizeEffort(effort)
	if err != nil {
		return chatResult{}, err
	}
	if strings.TrimSpace(prompt) == "" {
		return chatResult{}, badInvocation{errors.New("there is nothing to send: the message is empty")}
	}
	var history []chatTurn
	if strings.TrimSpace(historyJSON) != "" {
		if err := json.Unmarshal([]byte(historyJSON), &history); err != nil {
			return chatResult{}, badInvocation{fmt.Errorf("history is not a JSON list of {role,text}: %w", err)}
		}
	}
	if len(history) > maxChatHistory {
		history = history[len(history)-maxChatHistory:]
	}
	store, err := providerStore()
	if err != nil {
		return chatResult{}, err
	}
	ps, err := store.List()
	if err != nil {
		return chatResult{}, err
	}
	if ref = strings.TrimSpace(ref); ref == "" {
		if ref, err = store.ResolveDefault(); err != nil {
			return chatResult{}, err
		}
	}
	if ref == "" {
		ref, err = onlyUsableModel(ps)
		if err != nil {
			return chatResult{}, err
		}
	}
	res, err := model.Resolve(ps, ref)
	if err != nil {
		return chatResult{}, err
	}

	var messages []turn.Message
	text := func(role turn.Role, s string) turn.Message {
		return turn.Message{Role: role, Content: []turn.ContentBlock{{Type: turn.BlockText, Text: s}}}
	}
	if s := strings.TrimSpace(system); s != "" {
		messages = append(messages, text(turn.RoleSystem, s))
	}
	for _, h := range history {
		if strings.TrimSpace(h.Text) == "" {
			continue
		}
		switch h.Role {
		case "user":
			messages = append(messages, text(turn.RoleUser, h.Text))
		case "assistant":
			messages = append(messages, text(turn.RoleAssistant, h.Text))
		}
	}
	messages = append(messages, text(turn.RoleUser, prompt))

	ctx, cancel := context.WithTimeout(ctx, chatTimeout)
	defer cancel()
	exec := &provider.Executor{}
	resp, err := exec.CompleteTurn(ctx, turn.Request{
		Schema: turn.Schema, Provider: res.Provider, Protocol: res.Protocol,
		BaseURL: res.BaseURL, APIKeyEnv: res.APIKeyEnv, Model: res.Model,
		MaxTokens: 4096, Messages: messages, Effort: level,
	})
	if err != nil {
		return chatResult{}, fmt.Errorf("%s/%s: %w", res.Provider, res.Model, err)
	}
	if resp.FinishReason == turn.FinishRefusal {
		msg := "the model refused"
		if resp.Refusal != nil && resp.Refusal.Message != "" {
			msg = resp.Refusal.Message
		}
		return chatResult{}, fmt.Errorf("%s/%s: %s", res.Provider, res.Model, msg)
	}
	reply := responseText(resp)
	if strings.TrimSpace(reply) == "" {
		reply = "(the model sent an empty reply)"
	}
	return chatResult{
		Text: reply, Model: res.Model, Provider: res.Provider,
		InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens,
	}, nil
}

// onlyUsableModel picks the model to chat with when none was named and no
// default is set: the single enabled model, if there is exactly one. With none
// or several, guessing would bill the wrong model, so it asks instead.
func onlyUsableModel(ps []model.Provider) (string, error) {
	var refs []string
	for _, p := range ps {
		for _, m := range p.Models {
			if m.Enabled {
				refs = append(refs, p.Name+"/"+m.ID)
			}
		}
	}
	switch len(refs) {
	case 0:
		if len(ps) == 0 {
			return "", errors.New("no provider is set up yet: add one with /provider")
		}
		return "", errors.New("no model is available yet: open /provider, pick a provider and discover or add its models")
	case 1:
		return refs[0], nil
	default:
		return "", errors.New("no model is selected: choose one with /model")
	}
}

// ---- protocol handlers -----------------------------------------------------

// handleProviderUpdate answers `provider.update`. Like provider.add it reports
// that a key was stored and never what it was.
func handleProviderUpdate(params map[string]any) (any, error) {
	p, keyStored, err := updateProvider(stringParam(params, "name"), stringParam(params, "base_url"),
		stringParam(params, "api_key_env"), stringParam(params, "api_key"))
	if err != nil {
		return nil, err
	}
	return providerAdded{Provider: p, KeyStored: keyStored}, nil
}

func handleProviderRemove(params map[string]any) (any, error) {
	name := cleanName(stringParam(params, "name"))
	if err := removeProvider(name); err != nil {
		return nil, err
	}
	return struct {
		Name    string `json:"name"`
		Removed bool   `json:"removed"`
	}{name, true}, nil
}

func handleModelDiscover(params map[string]any) (any, error) {
	return discoverModels(context.Background(), stringParam(params, "provider"))
}

func handleModelRemove(params map[string]any) (any, error) {
	p, id, err := removeModel(stringParam(params, "model"))
	if err != nil {
		return nil, err
	}
	return struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Removed  bool   `json:"removed"`
	}{p, id, true}, nil
}

// handleModelDefault reads the default when no model is given and sets it
// otherwise. The result always carries the current default ("" when none).
func handleModelDefault(params map[string]any) (any, error) {
	ref, err := defaultModel(stringParam(params, "model"))
	if err != nil {
		return nil, err
	}
	provider, id := model.ParseRef(ref)
	return struct {
		Default  string `json:"default"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}{ref, provider, id}, nil
}

func handleChatSend(params map[string]any) (any, error) {
	return chatSendEffort(context.Background(), stringParam(params, "prompt"), stringParam(params, "history"),
		stringParam(params, "system"), stringParam(params, "model"), stringParam(params, "effort"))
}
