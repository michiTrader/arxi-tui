package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	return updateProviderWire(name, baseURL, "", keyEnv, apiKey)
}

// updateProviderWire is updateProvider with an optional explicit wire. A new endpoint,
// or "auto", detects the wire again; "openai" / "anthropic" force it.
func updateProviderWire(name, baseURL, protocol, keyEnv, apiKey string) (model.Provider, bool, error) {
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
	want, ok := model.ParseProtocol(protocol)
	if !ok {
		return model.Provider{}, false, errBadProtocol(protocol)
	}
	u := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if u != "" {
		p.BaseURL = u
	}
	tableURL, _, inTable := model.Known(p.Name)
	switch {
	case want != "":
		p.Protocol = want
	case inTable && p.BaseURL == tableURL:
		// The vendor's own endpoint speaks the vendor's own wire.
		if kp, ok := model.KnownProtocol(p.Name); ok && u != "" {
			p.Protocol = kp
		}
	case u != "" || strings.TrimSpace(protocol) != "":
		// Something about the endpoint changed (or "auto" was asked): decide again.
		key := apiKey
		if key == "" {
			if k, ok, _ := secretsLookup(p.Name); ok {
				key = k
			} else if p.APIKeyEnv != "" {
				key = os.Getenv(p.APIKeyEnv)
			}
		}
		if base, found := detectEndpoint(p.BaseURL, key); found != "" {
			p.BaseURL, p.Protocol = base, found
		} else {
			p.Protocol = model.ProtocolOpenAIChatCompletions
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

// chatAttempts is how many times one chat message is tried when the provider answers
// with a transient refusal (a rate limit, a gateway timeout, an overload). The wait
// before try n+1 grows with each failure (see retryWait), so six tries ride out a
// provider that says "Retry in 27s" without hammering one that is struggling. All of
// these are variables so tests need not sleep.
var (
	chatAttempts = 6
	chatBackoff  = 2 * time.Second
	// chatMaxBackoff caps the growing wait; chatMaxWait is the longest a provider
	// may ask for (Retry-After) before the call gives up instead of waiting.
	chatMaxBackoff = 30 * time.Second
	chatMaxWait    = 90 * time.Second
	// chatRetryTotal is the most time one chat call spends waiting between tries; it is
	// added to chatTimeout so a patient retry is not cut short by the call's own deadline.
	chatRetryTotal = 5 * time.Minute
	// chatRetryMargin is added to a wait the provider asked for, so the retry lands just
	// after its window opens and not on its edge.
	chatRetryMargin = time.Second
)

// transientRefusal reports whether a refusal is the provider saying "not now" rather
// than "not this": 408, 429 and the gateway errors 502, 503, 504 and 529 (overloaded).
// A 500 is left alone on purpose, because it more often means the request itself broke
// the provider. A 429 that is really a billing wall ("insufficient_quota", "buy a
// subscription") is permanent: waiting never fixes it.
func transientRefusal(r *turn.Refusal) bool {
	if r == nil {
		return false
	}
	switch r.Code {
	case "http_408", "http_429", "http_502", "http_503", "http_504", "http_529":
		return !billingWall(r.Message)
	}
	return false
}

// billingWall reports whether a refusal message says the account (not the moment) is
// the problem: out of quota, no subscription, no credit.
func billingWall(msg string) bool {
	m := strings.ToLower(msg)
	for _, w := range []string{"insufficient_quota", "billing", "subscription", "exceeded your current quota", "insufficient balance", "insufficient credit", "payment required"} {
		if strings.Contains(m, w) {
			return true
		}
	}
	return false
}

// retryWait is how long to wait before the next try: the provider's own ask when it
// made one (Retry-After, or "Retry in 27s" in the message) plus a second of margin,
// otherwise chatBackoff doubled for every failure so far, capped at chatMaxBackoff.
// The boolean is false when the provider asked for longer than chatMaxWait: the call
// then gives up and says so, instead of freezing the chat.
func retryWait(failures int, asked time.Duration) (time.Duration, bool) {
	if asked > 0 {
		if asked > chatMaxWait {
			return asked, false
		}
		return asked + chatRetryMargin, true
	}
	d := chatBackoff
	for i := 1; i < failures && d < chatMaxBackoff; i++ {
		d *= 2
	}
	if d > chatMaxBackoff {
		d = chatMaxBackoff
	}
	return d, true
}

// refusalText is the sentence the user reads when the provider refused. A bare
// gateway body such as "error code: 504" says nothing, so the usual causes get a
// plain explanation that keeps the original words.
func refusalText(r *turn.Refusal, tries int) string {
	msg := "the model refused"
	if r != nil && strings.TrimSpace(r.Message) != "" {
		msg = r.Message
	}
	if r == nil {
		return msg
	}
	if billingWall(r.Message) {
		return fmt.Sprintf("the provider will not serve this model for this account (%s): that is billing, not the moment, so retrying does not help. Pick another model with /model, or fix the plan with the provider. Provider said: %s", strings.TrimPrefix(r.Code, "http_"), shortReason(msg))
	}
	switch r.Code {
	case "http_403":
		if strings.TrimSpace(r.Message) == "" {
			return "the provider's gateway refused the request with an empty 403 and kept doing so on every retry (nothing was sent to a model or billed). It is not your key: gateways in front of an API do this to an unpredictable share of requests. Try again in a moment"
		}
	case "http_504", "http_502", "http_503":
		return fmt.Sprintf("the provider timed out or is overloaded (%s, tried %d times). A long answer from a slow model is the usual cause: ask for something shorter, lower /effort, or pick another model with /model. Provider said: %s", strings.TrimPrefix(r.Code, "http_"), tries, msg)
	case "http_429":
		return fmt.Sprintf("the provider is rate limiting this key (429, tried %d times%s). Send the message again in a moment, or pick another model with /model. Provider said: %s", tries, giveUpNote(r), msg)
	}
	return msg
}

// chatResult is what one chat call returns.
type chatResult struct {
	Text         string `json:"text"`
	Model        string `json:"model"`
	Provider     string `json:"provider"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	// ContextTokens is how much the model held at the last ask of the turn: that ask's
	// input plus its answer. InputTokens adds every ask of the turn, and a tool loop asks
	// again with the whole conversation each round, so it is the cost of the turn and says
	// nothing about how full the context is; this is the figure for that.
	ContextTokens int `json:"context_tokens,omitempty"`
}

// chatSend sends one prompt, with earlier turns, to a model and returns the
// reply. It bills the provider directly; it starts no run, needs no agent and
// writes nothing to disk.
func chatSend(ctx context.Context, prompt, historyJSON, system, ref string) (chatResult, error) {
	return chatSendEffort(ctx, prompt, historyJSON, system, ref, "")
}

// normalizeEffort maps what a caller typed to the word that goes on. "auto" and
// the empty string both mean "send nothing and let the model decide". Which words
// a particular model takes is checked once the model is known (chatSendEffort).
func normalizeEffort(effort string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(effort))
	if e == "" || e == "auto" {
		return "", nil
	}
	if !model.IsEffortWord(e) {
		return "", badInvocation{fmt.Errorf("effort %q is not one of auto, %s", effort, strings.Join(model.EffortWords, ", "))}
	}
	return e, nil
}

// checkEffort refuses a level the resolved model does not take, naming the ones
// it does, before anything is billed.
func checkEffort(res model.Resolution, level string) error {
	if level == "" || model.EffortAllowed(res.Protocol, res.Model, level) {
		return nil
	}
	have := model.EffortLevels(res.Protocol, res.Model)
	if len(have) == 0 {
		return badInvocation{fmt.Errorf("%s/%s takes no thinking level; leave the effort unset", res.Provider, res.Model)}
	}
	return badInvocation{fmt.Errorf("%s/%s does not take effort %q; it takes: %s", res.Provider, res.Model, level, strings.Join(have, ", "))}
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
	if err := checkEffort(res, level); err != nil {
		return chatResult{}, err
	}

	if box := toolsFrom(ctx); box != nil {
		system = strings.TrimSpace(system + " " + toolsHint(box.box.Root(), box.edits) + " " + envHint(box.box.Root(), box.runs) + " " + runsHint(box.runs) + " " + webHint(box.web))
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

	ctx, cancel := context.WithTimeout(ctx, chatTimeout+chatRetryTotal)
	defer cancel()
	exec := &provider.Executor{OnWire: rememberWire}
	req := turn.Request{
		Schema: turn.Schema, Provider: res.Provider, Protocol: res.Protocol,
		BaseURL: res.BaseURL, APIKeyEnv: res.APIKeyEnv, Model: res.Model,
		MaxTokens: 4096, Messages: messages, Effort: level,
	}
	// complete asks the model once, retrying a transient refusal. Tokens are
	// added up over every ask of the turn, so a tool loop reports its true cost.
	var used turn.Usage
	var last turn.Usage
	var waited time.Duration
	tries := 0
	complete := func(req turn.Request) (turn.Response, error) {
		for attempt := 1; ; attempt++ {
			resp, err := exec.CompleteTurn(ctx, req)
			tries = attempt
			if err != nil {
				return resp, fmt.Errorf("%s/%s: %w", res.Provider, res.Model, err)
			}
			used.InputTokens += resp.Usage.InputTokens
			used.OutputTokens += resp.Usage.OutputTokens
			last = resp.Usage
			if resp.FinishReason != turn.FinishRefusal || !transientRefusal(resp.Refusal) || attempt >= chatAttempts {
				return resp, nil
			}
			// A rate limit, a gateway timeout or an overloaded provider often clears on
			// its own, and the refusal carries no answer, so asking again repeats nothing
			// the user paid for. The wait is what the provider asked for when it said, and
			// otherwise grows with every failure, so a struggling provider is not hammered
			// and a patient one is not given up on after three quick tries.
			wait, ok := retryWait(attempt, time.Duration(resp.Refusal.RetryAfterMs)*time.Millisecond)
			if !ok || waited+wait > chatRetryTotal {
				return resp, nil
			}
			waited += wait
			notifyRetry(ctx, chatRetryNotification{
				Type: "chat.retry", Provider: res.Provider, Model: res.Model,
				Attempt: attempt + 1, Of: chatAttempts, WaitMs: wait.Milliseconds(),
				Status: strings.TrimPrefix(resp.Refusal.Code, "http_"), Reason: shortReason(resp.Refusal.Message),
			})
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return resp, fmt.Errorf("%s/%s: %w", res.Provider, res.Model, ctx.Err())
			}
		}
	}
	var resp turn.Response
	if box := toolsFrom(ctx); box != nil {
		resp, err = runToolLoop(ctx, box, &req, complete)
	} else {
		resp, err = complete(req)
	}
	if err != nil {
		return chatResult{}, err
	}
	if resp.FinishReason == turn.FinishRefusal {
		return chatResult{}, fmt.Errorf("%s/%s: %s", res.Provider, res.Model, refusalText(resp.Refusal, tries))
	}
	reply := responseText(resp)
	if strings.TrimSpace(reply) == "" {
		reply = "(the model sent an empty reply)"
	}
	return chatResult{
		Text: reply, Model: res.Model, Provider: res.Provider,
		InputTokens: used.InputTokens, OutputTokens: used.OutputTokens,
		ContextTokens: last.InputTokens + last.OutputTokens,
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
	p, keyStored, err := updateProviderWire(stringParam(params, "name"), stringParam(params, "base_url"), stringParam(params, "protocol"),
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

// thinkingNotification is one fragment of the model's reasoning, sent on the
// connection while a chat.send that asked for it is still running. It has a type
// and no id, which is how every notification on this wire is told from a response.
type thinkingNotification struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// handleChatSendThinking is handleChatSend for a caller that wants to watch the
// model think. The reply, the retries and the refusal text are the same ones:
// only the way the provider is called changes (streamed), and each reasoning
// fragment is written to the connection as it arrives.
func handleChatSendThinking(streams *connStreams, params map[string]any) (any, error) {
	w := streams.w
	ctx := withRetryNotice(context.Background(), func(n chatRetryNotification) { _ = w.write(n) })
	if boolParam(params, "stream_thinking") {
		ctx = provider.WithThinking(ctx, func(fragment string) {
			_ = w.write(thinkingNotification{Type: "chat.thinking", Text: fragment})
		})
	}
	if stringParam(params, "workdir") == "" && stringParam(params, "runs") != "" {
		return nil, badInvocation{errors.New("runs needs a workdir to run the commands in")}
	}
	if stringParam(params, "workdir") == "" && stringParam(params, "web") != "" {
		return nil, badInvocation{errors.New("web needs a workdir: it travels with the tools")}
	}
	if dir := stringParam(params, "workdir"); dir != "" {
		var err error
		ctx, err = withTools(ctx, dir, stringParam(params, "edits"), streams.ask,
			func(n chatToolNotification) { _ = w.write(n) })
		if err != nil {
			return nil, err
		}
		if ctx, err = withRuns(ctx, stringParam(params, "runs")); err != nil {
			return nil, err
		}
		if ctx, err = withWeb(ctx, stringParam(params, "web")); err != nil {
			return nil, err
		}
		if ctx, err = withClientTools(ctx, stringParam(params, "client_tools"), streams.callClient); err != nil {
			return nil, err
		}
	} else if stringParam(params, "client_tools") != "" {
		return nil, badInvocation{errors.New("client_tools needs a workdir: they travel with the tools")}
	}
	return chatSendEffort(ctx, stringParam(params, "prompt"), stringParam(params, "history"),
		stringParam(params, "system"), stringParam(params, "model"), stringParam(params, "effort"))
}

func handleChatSend(params map[string]any) (any, error) {
	if stringParam(params, "workdir") != "" || stringParam(params, "edits") != "" || stringParam(params, "runs") != "" || stringParam(params, "web") != "" || stringParam(params, "client_tools") != "" {
		return nil, badInvocation{errors.New("workdir, edits, runs, web and client_tools need a live connection to report the tool calls on")}
	}
	return chatSendEffort(context.Background(), stringParam(params, "prompt"), stringParam(params, "history"),
		stringParam(params, "system"), stringParam(params, "model"), stringParam(params, "effort"))
}

// rememberWire records, on the model, the wire that worked after the provider's own
// wire answered a web page, so the next call goes straight to it. A failure to save
// costs one extra request next time and never fails the call that already succeeded.
func rememberWire(providerName, modelID, protocol string) {
	store, err := providerStore()
	if err != nil {
		return
	}
	p, err := store.Load(cleanName(providerName))
	if err != nil {
		return
	}
	for i := range p.Models {
		if p.Models[i].ID == modelID {
			p.Models[i].Protocol = protocol
			_ = store.Save(p)
			return
		}
	}
}

// chatRetryNotification is written to the connection each time a chat call is about to
// wait and ask again, so the client can show "retrying in 27s (2/6)" instead of a spinner
// that looks frozen. It has a type and no id, like every notification on this wire.
type chatRetryNotification struct {
	Type     string `json:"type"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	// Attempt is the try about to be made, Of how many there are.
	Attempt int `json:"attempt"`
	Of      int `json:"of"`
	// WaitMs is how long the call waits before that try.
	WaitMs int64 `json:"wait_ms"`
	// Status is the HTTP status that caused the wait ("429"), Reason the provider's
	// words, shortened.
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type retryNoticeKey struct{}

// withRetryNotice returns a context whose chat call reports each wait to fn.
func withRetryNotice(ctx context.Context, fn func(chatRetryNotification)) context.Context {
	return context.WithValue(ctx, retryNoticeKey{}, fn)
}

func notifyRetry(ctx context.Context, n chatRetryNotification) {
	if fn, _ := ctx.Value(retryNoticeKey{}).(func(chatRetryNotification)); fn != nil {
		fn(n)
	}
}

// shortReason trims a provider's message to one readable line.
func shortReason(msg string) string {
	msg = strings.Join(strings.Fields(msg), " ")
	if r := []rune(msg); len(r) > 140 {
		return string(r[:140]) + "…"
	}
	return msg
}

// giveUpNote adds why the call stopped waiting when the provider asked for more than the
// chat is willing to wait.
func giveUpNote(r *turn.Refusal) string {
	if d := time.Duration(r.RetryAfterMs) * time.Millisecond; d > chatMaxWait {
		return fmt.Sprintf("; the provider asked for %s, longer than the %s this chat waits", d.Round(time.Second), chatMaxWait)
	}
	return ""
}
