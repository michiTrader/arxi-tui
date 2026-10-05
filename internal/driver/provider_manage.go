package driver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// This file holds the provider-management verbs added with the /provider hub:
// update, remove, model discovery, the default model and direct chat. They share one
// request helper because they all follow the same shape (send, read one response,
// decode the result); the older verbs in provider.go predate it.

// call sends one request under the connection lock and decodes a successful result
// into out. A refusal comes back as the core's own error, named by verb.
func (d *NDJSONDriver) call(ctx context.Context, id, verb string, params map[string]any, out any) error {
	req := protoRequest{ID: id, Type: verb, Params: params}

	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.enc.Encode(req); err != nil {
		return fmt.Errorf("ndjson: send %s: %w", verb, err)
	}
	resp, err := d.readResponse(ctx)
	if err != nil {
		return err
	}
	if !resp.OK {
		return resp.refusal(verb)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(resp.Result, out); err != nil {
		return fmt.Errorf("ndjson: decode %s result: %w", verb, err)
	}
	return nil
}

// ProviderUpdateParams are the wire parameters of provider.update. Empty strings
// are omitted, so the core keeps the stored value. APIKeyEnv "none" clears the
// variable name. APIKey is a secret: it travels only in the request body.
type ProviderUpdateParams struct {
	Name      string
	BaseURL   string
	APIKeyEnv string
	APIKey    string
}

// SubmitProviderUpdate changes the URL, key or key variable of a registered provider.
func (d *NDJSONDriver) SubmitProviderUpdate(ctx context.Context, p ProviderUpdateParams) (*ProviderAddResult, error) {
	if p.Name == "" {
		return nil, fmt.Errorf("ndjson: provider.update needs a provider name")
	}
	params := map[string]any{"name": p.Name}
	if p.BaseURL != "" {
		params["base_url"] = p.BaseURL
	}
	if p.APIKeyEnv != "" {
		params["api_key_env"] = p.APIKeyEnv
	}
	if p.APIKey != "" {
		params["api_key"] = p.APIKey
	}
	var r ProviderAddResult
	if err := d.call(ctx, "provider-update", "provider.update", params, &r); err != nil {
		return nil, err
	}
	if r.Name == "" {
		return nil, fmt.Errorf("ndjson: provider.update returned ok with no provider name")
	}
	return &r, nil
}

// ProviderRemoveResult is the answer to provider.remove.
type ProviderRemoveResult struct {
	Name    string `json:"name"`
	Removed bool   `json:"removed"`
}

// SubmitProviderRemove deletes a provider, its models and its stored key.
func (d *NDJSONDriver) SubmitProviderRemove(ctx context.Context, name string) (*ProviderRemoveResult, error) {
	if name == "" {
		return nil, fmt.Errorf("ndjson: provider.remove needs a provider name")
	}
	var r ProviderRemoveResult
	if err := d.call(ctx, "provider-remove", "provider.remove", map[string]any{"name": name}, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ModelDiscoverResult is the answer to model.discover: how many models the
// provider's /models endpoint listed and how many were new.
type ModelDiscoverResult struct {
	Provider string   `json:"provider"`
	Found    int      `json:"found"`
	Added    int      `json:"added"`
	Models   []string `json:"models"`
}

// SubmitModelDiscover asks the core to list a provider's models from its API and
// register the ones it does not have yet.
func (d *NDJSONDriver) SubmitModelDiscover(ctx context.Context, provider string) (*ModelDiscoverResult, error) {
	if provider == "" {
		return nil, fmt.Errorf("ndjson: model.discover needs a provider name")
	}
	var r ModelDiscoverResult
	if err := d.call(ctx, "model-discover", "model.discover", map[string]any{"provider": provider}, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ModelRemoveResult is the answer to model.remove.
type ModelRemoveResult struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Removed  bool   `json:"removed"`
}

// SubmitModelRemove deletes one model from its provider.
func (d *NDJSONDriver) SubmitModelRemove(ctx context.Context, ref string) (*ModelRemoveResult, error) {
	if ref == "" {
		return nil, fmt.Errorf("ndjson: model.remove needs a model ref")
	}
	var r ModelRemoveResult
	if err := d.call(ctx, "model-remove", "model.remove", map[string]any{"model": ref}, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ModelDefaultResult is the answer to model.default. Default is "provider/id", or
// empty when no default is chosen.
type ModelDefaultResult struct {
	Default  string `json:"default"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// SubmitModelDefault reads the default model (ref "") or sets it.
func (d *NDJSONDriver) SubmitModelDefault(ctx context.Context, ref string) (*ModelDefaultResult, error) {
	params := map[string]any{}
	if ref != "" {
		params["model"] = ref
	}
	var r ModelDefaultResult
	if err := d.call(ctx, "model-default", "model.default", params, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ChatTurn is one earlier message sent as context with a chat.send.
type ChatTurn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// ChatSendParams are the wire parameters of chat.send. Model may be empty: the core
// then uses the default model, or the only usable one.
type ChatSendParams struct {
	Prompt  string
	Model   string
	System  string
	History []ChatTurn
	// Effort is the thinking level: "minimal", "low", "medium" or "high". Empty
	// sends nothing, and the model decides.
	Effort string
	// OnThinking, when set, is called with each fragment of the model's thinking
	// as the core streams it, before the answer comes back. The call still
	// returns the whole answer. A core that does not know how to stream falls
	// back to a plain call and never calls it.
	OnThinking func(fragment string)
	// Workdir, when set, lets the model look into that folder with read-only
	// tools. Each call the core runs on its behalf is handed to OnTool. A core
	// that does not know the parameter makes the call fail with
	// ErrToolsUnsupported, so the caller can say so and ask again without it.
	Workdir string
	OnTool  func(ToolCall)
}

// ToolCall is one tool the core ran for the model. OK is false when the tool
// refused or failed; Summary then says why. Output is the (capped) text the
// model was given.
type ToolCall struct {
	ID      string
	Name    string
	Arg     string
	OK      bool
	Summary string
	Output  string
}

// ErrToolsUnsupported is returned when the core is too old to give the model tools.
var ErrToolsUnsupported = errors.New("this arxi core cannot give the model tools (it does not know workdir)")

// ChatSendResult is the model's answer with the usage the core measured.
type ChatSendResult struct {
	Text         string `json:"text"`
	Model        string `json:"model"`
	Provider     string `json:"provider"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

// SubmitChatSend sends one prompt to a model and waits for the whole answer. The
// history rides as a JSON string, the shape the core's string-only params accept.
func (d *NDJSONDriver) SubmitChatSend(ctx context.Context, p ChatSendParams) (*ChatSendResult, error) {
	if p.Prompt == "" {
		return nil, fmt.Errorf("ndjson: chat.send needs a prompt")
	}
	params := map[string]any{"prompt": p.Prompt}
	if p.Model != "" {
		params["model"] = p.Model
	}
	if p.System != "" {
		params["system"] = p.System
	}
	if p.Effort != "" {
		params["effort"] = p.Effort
	}
	if len(p.History) > 0 {
		b, err := json.Marshal(p.History)
		if err != nil {
			return nil, fmt.Errorf("ndjson: encode chat history: %w", err)
		}
		params["history"] = string(b)
	}
	if p.Workdir != "" {
		params["workdir"] = p.Workdir
	}
	var r ChatSendResult
	if p.OnThinking == nil && p.Workdir == "" {
		if err := d.call(ctx, "chat-send", "chat.send", params, &r); err != nil {
			return nil, err
		}
		return &r, nil
	}
	if p.OnThinking != nil {
		params["stream_thinking"] = true
	}
	err := d.callWatching(ctx, "chat-send", "chat.send", params, &r, p.OnThinking, p.OnTool)
	var ref *Refusal
	if errors.As(err, &ref) && ref.Code == "bad_params" {
		switch {
		case strings.Contains(ref.Message, "workdir"):
			return nil, fmt.Errorf("%w: %s", ErrToolsUnsupported, ref.Message)
		case strings.Contains(ref.Message, "stream_thinking"):
			// An older core refuses the parameter it does not know. The turn it
			// would have streamed is simply asked again the plain way.
			delete(params, "stream_thinking")
			if p.Workdir != "" {
				err = d.callWatching(ctx, "chat-send", "chat.send", params, &r, nil, p.OnTool)
			} else {
				err = d.call(ctx, "chat-send", "chat.send", params, &r)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// callWatching is call for a request the core answers with notifications first.
// A line with a type and no id is a notification: chat.thinking goes to
// onThinking, chat.tool to onTool, and every other kind is skipped. Either
// callback may be nil. The first line carrying an id
// is the response.
func (d *NDJSONDriver) callWatching(ctx context.Context, id, verb string, params map[string]any, out any, onThinking func(string), onTool func(ToolCall)) error {
	req := protoRequest{ID: id, Type: verb, Params: params}

	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.enc.Encode(req); err != nil {
		return fmt.Errorf("ndjson: send %s: %w", verb, err)
	}
	for {
		line, err := d.readLine(ctx)
		if err != nil {
			return fmt.Errorf("ndjson: read response: %w", err)
		}
		var head struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Text    string `json:"text"`
			CallID  string `json:"call_id"`
			Name    string `json:"name"`
			Arg     string `json:"arg"`
			OK      bool   `json:"ok"`
			Summary string `json:"summary"`
			Output  string `json:"output"`
		}
		if err := json.Unmarshal([]byte(line), &head); err != nil {
			return fmt.Errorf("ndjson: response is not JSON: %w", err)
		}
		if head.ID == "" && head.Type != "" {
			switch {
			case head.Type == "chat.thinking" && head.Text != "" && onThinking != nil:
				onThinking(head.Text)
			case head.Type == "chat.tool" && onTool != nil:
				onTool(ToolCall{ID: head.CallID, Name: head.Name, Arg: head.Arg, OK: head.OK, Summary: head.Summary, Output: head.Output})
			}
			continue
		}
		var resp protoResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			return fmt.Errorf("ndjson: response is not JSON: %w", err)
		}
		if !resp.OK {
			return resp.refusal(verb)
		}
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("ndjson: decode %s result: %w", verb, err)
		}
		return nil
	}
}
