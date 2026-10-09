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
	// Edits lets the model change files in Workdir: "deny" (the default, it may
	// only look), "ask" (every change is put to OnApproval first) or "allow".
	// A core that does not know the parameter makes the call fail with
	// ErrEditsUnsupported.
	Edits string
	// OnApproval is asked about each change when Edits is "ask", and blocks the
	// turn until it answers. It must return false when ctx ends. With no
	// OnApproval every change is declined.
	OnApproval func(ctx context.Context, a Approval) bool
	// Runs lets the model run shell commands in Workdir: "deny" (the default),
	// "ask" (every command is put to OnApproval first, with no diff) or "allow".
	// It is independent of Edits. A core that does not know the parameter makes
	// the call fail with ErrRunsUnsupported.
	Runs string
	// Web lets the model read web pages: "deny" (the default), "ask" (every address
	// is put to OnApproval first) or "allow". It needs Workdir, because it travels
	// with the tools. A core that does not know the parameter makes the call fail
	// with ErrWebUnsupported.
	Web string
	// ClientTools are tools this client lends the model and runs itself: the core
	// offers them and hands every call to OnClientTool, whose answer goes back to the
	// model. They need Workdir, because they travel with the tools. A core that does
	// not know the parameter makes the call fail with ErrClientToolsUnsupported.
	ClientTools []ClientToolDef
	// OnClientTool runs one call of a client tool. It may block (to ask the user) and
	// must return when ctx ends. With no OnClientTool every call fails.
	OnClientTool func(ctx context.Context, c ClientToolCall) ClientToolResult
}

// ClientToolDef is one tool a client lends the model: what the model is told about it.
type ClientToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

// ClientToolCall is one call the model made to a client tool.
type ClientToolCall struct {
	CallID    string
	Name      string
	Arguments json.RawMessage
}

// ClientToolResult is the client's answer: Text is what the model reads; Arg,
// Summary and Diff are what the user's tool line shows.
type ClientToolResult struct {
	OK      bool   `json:"ok"`
	Text    string `json:"text"`
	Arg     string `json:"arg,omitempty"`
	Summary string `json:"summary,omitempty"`
	Diff    string `json:"diff,omitempty"`
}

// Approval is a change the model wants to make and the core is holding until the
// user decides. Diff is a ready-to-show line diff.
type Approval struct {
	CallID  string
	Name    string
	Arg     string
	Summary string
	Diff    string
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
	// Diff is the change a write or edit made; empty for the other tools.
	Diff string
}

// ErrEditsUnsupported is returned when the core is too old to let the model change files.
var ErrEditsUnsupported = errors.New("this arxi core cannot let the model change files (it does not know edits)")

// ErrRunsUnsupported is returned when the core is too old to let the model run commands.
var ErrRunsUnsupported = errors.New("this arxi core cannot let the model run commands (it does not know runs)")

// ErrWebUnsupported is returned when the core is too old to let the model read the web.
var ErrWebUnsupported = errors.New("this arxi core cannot let the model read web pages (it does not know web)")

// ErrClientToolsUnsupported is returned when the core is too old to take tools the
// client runs itself.
var ErrClientToolsUnsupported = errors.New("this arxi core cannot take tools the client runs (it does not know client_tools)")

// ErrToolsUnsupported is returned when the core is too old to give the model tools.
var ErrToolsUnsupported = errors.New("this arxi core cannot give the model tools (it does not know workdir)")

// ChatSendResult is the model's answer with the usage the core measured.
type ChatSendResult struct {
	Text         string `json:"text"`
	Model        string `json:"model"`
	Provider     string `json:"provider"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	// ContextTokens is what the model held at the last ask of the turn (0 from an older
	// core). InputTokens is the sum over every ask, which a tool loop inflates.
	ContextTokens int `json:"context_tokens"`
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
	if p.Edits != "" && p.Workdir != "" {
		params["edits"] = p.Edits
	}
	if p.Runs != "" && p.Workdir != "" {
		params["runs"] = p.Runs
	}
	if p.Web != "" && p.Workdir != "" {
		params["web"] = p.Web
	}
	if len(p.ClientTools) > 0 && p.Workdir != "" {
		b, err := json.Marshal(p.ClientTools)
		if err != nil {
			return nil, fmt.Errorf("ndjson: encode client tools: %w", err)
		}
		params["client_tools"] = string(b)
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
	err := d.callWatching(ctx, "chat-send", "chat.send", params, &r, p.OnThinking, p.OnTool, p.OnApproval, p.OnClientTool)
	var ref *Refusal
	if errors.As(err, &ref) && ref.Code == "bad_params" {
		// Only the part that names what was refused: the rest of the message lists
		// everything the core does take, which would match every parameter below.
		refused := refusedPart(ref.Message)
		switch {
		// Checked first: "client_tools" is the one name that contains none of the
		// others, but a later one might, and the newest parameter is the likeliest
		// to be the one an older core refuses.
		case strings.Contains(refused, "client_tools"):
			return nil, fmt.Errorf("%w: %s", ErrClientToolsUnsupported, ref.Message)
		case strings.Contains(refused, "workdir"):
			return nil, fmt.Errorf("%w: %s", ErrToolsUnsupported, ref.Message)
		case strings.Contains(refused, "edits"):
			return nil, fmt.Errorf("%w: %s", ErrEditsUnsupported, ref.Message)
		case strings.Contains(refused, "runs"):
			return nil, fmt.Errorf("%w: %s", ErrRunsUnsupported, ref.Message)
		case strings.Contains(refused, "web"):
			return nil, fmt.Errorf("%w: %s", ErrWebUnsupported, ref.Message)
		case strings.Contains(refused, "stream_thinking"):
			// An older core refuses the parameter it does not know. The turn it
			// would have streamed is simply asked again the plain way.
			delete(params, "stream_thinking")
			if p.Workdir != "" {
				err = d.callWatching(ctx, "chat-send", "chat.send", params, &r, nil, p.OnTool, p.OnApproval, p.OnClientTool)
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

// refusedPart is the start of a bad_params message, before the list of what the core
// does take ("chat.send does not take runs. It takes: edits, workdir, ...").
func refusedPart(msg string) string {
	if i := strings.Index(msg, ". It takes"); i >= 0 {
		return msg[:i]
	}
	return msg
}

// callWatching is call for a request the core answers with notifications first.
// A line with a type and no id is a notification: chat.thinking goes to
// onThinking, chat.tool to onTool, chat.approval to onApproval (whose answer goes
// back as a chat.decision line while the turn waits), and every other kind is
// skipped. Any callback may be nil; with no onApproval every change is declined.
// The first line carrying an id is the response.
//
// chat.client_tool goes to onClient, and its answer goes back as a
// chat.client_result line the same way; with no onClient the call fails, so the
// turn goes on and the model is told.
func (d *NDJSONDriver) callWatching(ctx context.Context, id, verb string, params map[string]any, out any, onThinking func(string), onTool func(ToolCall), onApproval func(context.Context, Approval) bool, onClient func(context.Context, ClientToolCall) ClientToolResult) error {
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
			Diff    string `json:"diff"`
			// Arguments is a client tool call's input, kept raw for the client.
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal([]byte(line), &head); err != nil {
			return fmt.Errorf("ndjson: response is not JSON: %w", err)
		}
		if head.ID == "" && head.Type != "" {
			switch {
			case head.Type == "chat.thinking" && head.Text != "" && onThinking != nil:
				onThinking(head.Text)
			case head.Type == "chat.tool" && onTool != nil:
				onTool(ToolCall{ID: head.CallID, Name: head.Name, Arg: head.Arg, OK: head.OK, Summary: head.Summary, Output: head.Output, Diff: head.Diff})
			case head.Type == "chat.approval":
				allow := onApproval != nil && onApproval(ctx, Approval{CallID: head.CallID, Name: head.Name, Arg: head.Arg, Summary: head.Summary, Diff: head.Diff})
				if err := d.enc.Encode(map[string]any{"type": "chat.decision", "call_id": head.CallID, "allow": allow}); err != nil {
					return fmt.Errorf("ndjson: answer %s: %w", head.CallID, err)
				}
			case head.Type == "chat.client_tool":
				res := ClientToolResult{Text: head.Name + " is not available in this client"}
				if onClient != nil {
					res = onClient(ctx, ClientToolCall{CallID: head.CallID, Name: head.Name, Arguments: head.Arguments})
				}
				if err := d.enc.Encode(map[string]any{"type": "chat.client_result", "call_id": head.CallID,
					"ok": res.OK, "text": res.Text, "arg": res.Arg, "summary": res.Summary, "diff": res.Diff}); err != nil {
					return fmt.Errorf("ndjson: answer %s: %w", head.CallID, err)
				}
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

// BlueprintStage is one stage of a validated blueprint.
type BlueprintStage struct {
	Name        string `json:"name"`
	AdvanceWhen string `json:"advance_when"`
	OnTimeout   string `json:"on_timeout"`
	TimeoutMs   int64  `json:"timeout_ms"`
}

// BlueprintMember is one member of a validated blueprint. Empty Stages means the
// member takes part in every stage, which is how the blueprint declares it.
type BlueprintMember struct {
	Name     string   `json:"name"`
	Role     string   `json:"role"`
	Model    string   `json:"model"`
	Tools    []string `json:"tools"`
	Advisory bool     `json:"advisory"`
	Stages   []string `json:"stages"`
}

// BlueprintWatcher is one watcher of a validated blueprint.
type BlueprintWatcher struct {
	Agent   string `json:"agent"`
	Pattern string `json:"pattern"`
	Action  string `json:"action"`
	Tool    string `json:"tool"`
}

// BlueprintInfo is the structured answer of blueprint.validate: everything needed
// to draw a team's architecture without running it.
type BlueprintInfo struct {
	Name            string             `json:"name"`
	SHA             string             `json:"sha"`
	Workspace       string             `json:"workspace"`
	WorkspaceReason string             `json:"workspace_reason"`
	Stages          []BlueprintStage   `json:"stages"`
	Members         []BlueprintMember  `json:"members"`
	Watchers        []BlueprintWatcher `json:"watchers"`
}

// SubmitBlueprintValidate asks the core to load and validate the blueprint file at
// path and describe it. A blueprint the core refuses comes back as an error whose
// text says why, so a screen can show the reason beside the file.
func (d *NDJSONDriver) SubmitBlueprintValidate(ctx context.Context, path string) (*BlueprintInfo, error) {
	if path == "" {
		return nil, fmt.Errorf("ndjson: blueprint.validate with an empty path; there is no blueprint to read")
	}
	var r BlueprintInfo
	if err := d.call(ctx, "blueprint-validate", "blueprint.validate", map[string]any{"path": path}, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// AgentRow is one stored agent or team as agent.list reports it. A file that does
// not load carries its reason in Error instead of being dropped.
type AgentRow struct {
	Name    string            `json:"name"`
	Path    string            `json:"path"`
	SHA     string            `json:"sha"`
	Members []BlueprintMember `json:"members"`
	Stages  []string          `json:"stages"`
	Error   string            `json:"error"`
}

// AgentListResult wraps agent.list. Empty is a valid answer.
type AgentListResult struct {
	Agents []AgentRow `json:"agents"`
}

// SubmitAgentList reads every stored agent and team. It mutates nothing.
func (d *NDJSONDriver) SubmitAgentList(ctx context.Context) (*AgentListResult, error) {
	var r AgentListResult
	if err := d.call(ctx, "agent-list", "agent.list", map[string]any{}, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// AgentCreateParams are the wire parameters of agent.create. Empty fields are
// omitted so the core applies its own defaults (and the role's).
type AgentCreateParams struct {
	Name     string
	Model    string
	Role     string
	Tools    []string
	Advisory bool
}

// AgentCreateResult is what agent.create answers.
type AgentCreateResult struct {
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	Tools    []string `json:"tools"`
	Advisory bool     `json:"advisory"`
	RoleNote string   `json:"role_note"`
}

// SubmitAgentCreate writes one agent. It never overwrites: a taken name is
// refused with the core's own sentence.
func (d *NDJSONDriver) SubmitAgentCreate(ctx context.Context, p AgentCreateParams) (*AgentCreateResult, error) {
	if strings.TrimSpace(p.Name) == "" {
		return nil, fmt.Errorf("ndjson: agent.create with an empty name; an agent is addressed by its name")
	}
	params := map[string]any{"name": p.Name}
	if p.Model != "" {
		params["model"] = p.Model
	}
	if p.Role != "" {
		params["role"] = p.Role
	}
	if len(p.Tools) > 0 {
		params["tools"] = strings.Join(p.Tools, ",")
	}
	if p.Advisory {
		params["advisory"] = true
	}
	var r AgentCreateResult
	if err := d.call(ctx, "agent-create", "agent.create", params, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// BlueprintCreateParams are the wire parameters of blueprint.create.
type BlueprintCreateParams struct {
	Name    string
	Members []string
	Stages  []string
}

// BlueprintCreateResult is what blueprint.create answers.
type BlueprintCreateResult struct {
	Name    string   `json:"name"`
	Path    string   `json:"path"`
	Members []string `json:"members"`
	Stages  []string `json:"stages"`
}

// SubmitBlueprintCreate composes a team out of stored agents.
func (d *NDJSONDriver) SubmitBlueprintCreate(ctx context.Context, p BlueprintCreateParams) (*BlueprintCreateResult, error) {
	if strings.TrimSpace(p.Name) == "" {
		return nil, fmt.Errorf("ndjson: blueprint.create with an empty name; a team is addressed by its name")
	}
	if len(p.Members) == 0 {
		return nil, fmt.Errorf("ndjson: blueprint.create with no members; a team with nobody in it never takes a turn")
	}
	params := map[string]any{"name": p.Name, "members": strings.Join(p.Members, ",")}
	if len(p.Stages) > 0 {
		params["stages"] = strings.Join(p.Stages, ",")
	}
	var r BlueprintCreateResult
	if err := d.call(ctx, "blueprint-create", "blueprint.create", params, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// BlueprintStageParams changes the rules of one stage of a stored blueprint. Empty
// fields are left as they are; TimeoutMs nil leaves the timeout alone and 0 removes it.
type BlueprintStageParams struct {
	Name        string
	Stage       string
	AdvanceWhen string
	TimeoutMs   *int64
	OnTimeout   string
}

// BlueprintStageResult is the stages as the core saved them.
type BlueprintStageResult struct {
	Name   string           `json:"name"`
	Stages []BlueprintStage `json:"stages"`
}

// SubmitBlueprintStage changes how one stage of a stored blueprint advances or when
// it gives up. The core validates the whole file before replacing it, so a refusal
// leaves the file as it was and comes back as its own sentence.
func (d *NDJSONDriver) SubmitBlueprintStage(ctx context.Context, p BlueprintStageParams) (*BlueprintStageResult, error) {
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Stage) == "" {
		return nil, fmt.Errorf("ndjson: blueprint.stage needs the team and the stage to change")
	}
	params := map[string]any{"name": p.Name, "stage": p.Stage}
	if p.AdvanceWhen != "" {
		params["advance_when"] = p.AdvanceWhen
	}
	if p.OnTimeout != "" {
		params["on_timeout"] = p.OnTimeout
	}
	if p.TimeoutMs != nil {
		params["timeout_ms"] = *p.TimeoutMs
	}
	var r BlueprintStageResult
	if err := d.call(ctx, "blueprint-stage", "blueprint.stage", params, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// BlueprintMemberParams changes one member of a stored blueprint. A nil field is
// left as it is; a pointer to an empty string (or an empty tool list) removes it.
type BlueprintMemberParams struct {
	Name     string
	Member   string
	Model    *string
	Role     *string
	Tools    *[]string
	Advisory *bool
}

// BlueprintMemberResult is the members as the core saved them.
type BlueprintMemberResult struct {
	Name    string            `json:"name"`
	Members []BlueprintMember `json:"members"`
}

// SubmitBlueprintMember changes the model, role, tools or advisory flag of one
// member of a stored blueprint. The core validates the whole file before replacing
// it, so a refusal leaves the file as it was and comes back as its own sentence.
func (d *NDJSONDriver) SubmitBlueprintMember(ctx context.Context, p BlueprintMemberParams) (*BlueprintMemberResult, error) {
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Member) == "" {
		return nil, fmt.Errorf("ndjson: blueprint.member needs the blueprint and the member to change")
	}
	params := map[string]any{"name": p.Name, "member": p.Member}
	if p.Model != nil {
		params["model"] = *p.Model
	}
	if p.Role != nil {
		params["role"] = *p.Role
	}
	if p.Tools != nil {
		params["tools"] = strings.Join(*p.Tools, ",")
	}
	if p.Advisory != nil {
		params["advisory"] = *p.Advisory
	}
	var r BlueprintMemberResult
	if err := d.call(ctx, "blueprint-member", "blueprint.member", params, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// BlueprintWatchParams adds, replaces or removes one watcher of a stored blueprint.
// A watcher is identified by the member it wakes and the event pattern; Action is
// activate, notify or run_tool (empty means activate), and Tool goes with run_tool.
type BlueprintWatchParams struct {
	Name    string
	Agent   string
	Pattern string
	Action  string
	Tool    string
	Remove  bool
}

// BlueprintWatchResult is the watchers as the core saved them.
type BlueprintWatchResult struct {
	Name     string             `json:"name"`
	Watchers []BlueprintWatcher `json:"watchers"`
}

// SubmitBlueprintWatch saves or removes one watcher of a stored blueprint. The core
// validates the whole file before replacing it, so a refusal leaves the file as it
// was and comes back as its own sentence.
func (d *NDJSONDriver) SubmitBlueprintWatch(ctx context.Context, p BlueprintWatchParams) (*BlueprintWatchResult, error) {
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Agent) == "" || strings.TrimSpace(p.Pattern) == "" {
		return nil, fmt.Errorf("ndjson: blueprint.watch needs the blueprint, the member and the events to watch")
	}
	params := map[string]any{"name": p.Name, "agent": p.Agent, "pattern": p.Pattern}
	if p.Action != "" {
		params["action"] = p.Action
	}
	if p.Tool != "" {
		params["tool"] = p.Tool
	}
	if p.Remove {
		params["remove"] = true
	}
	var r BlueprintWatchResult
	if err := d.call(ctx, "blueprint-watch", "blueprint.watch", params, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// TriggerRecord is one stored trigger as the core reports it. On and Then are the
// strings the trigger was created with; the Last* fields say what has happened.
type TriggerRecord struct {
	Name            string  `json:"name"`
	ID              string  `json:"id"`
	On              string  `json:"on"`
	Then            string  `json:"then"`
	Budget          float64 `json:"budget"`
	BudgetPeriod    string  `json:"budget_period"`
	OnMissed        string  `json:"on_missed"`
	Overlap         string  `json:"overlap"`
	Status          string  `json:"status"`
	CreatedAt       string  `json:"created_at"`
	LastFiredAt     string  `json:"last_fired_at"`
	LastScheduledAt string  `json:"last_scheduled_at"`
	LastStatus      string  `json:"last_status"`
}

// TriggerRow is a trigger and when it fires next. Next is RFC 3339 (UTC) when the
// core could work it out; otherwise NextAbsent says why (paused, external).
type TriggerRow struct {
	Record     TriggerRecord `json:"record"`
	Next       string        `json:"next"`
	NextAbsent string        `json:"next_absent"`
	Note       string        `json:"note"`
	Missed     int           `json:"missed"`
}

// TriggerListResult wraps trigger.list. Empty is a valid answer.
type TriggerListResult struct {
	Triggers []TriggerRow `json:"triggers"`
}

// SubmitTriggerList reads every stored trigger. It mutates nothing.
func (d *NDJSONDriver) SubmitTriggerList(ctx context.Context) (*TriggerListResult, error) {
	var r TriggerListResult
	if err := d.call(ctx, "trigger-list", "trigger.list", map[string]any{}, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// TriggerCreateParams are the wire parameters of trigger.create. Budget and
// BudgetPeriod are mandatory: the core refuses a trigger with no spend ceiling.
type TriggerCreateParams struct {
	Name         string
	On           string
	Then         string
	Budget       float64
	BudgetPeriod string
}

// SubmitTriggerCreate stores one trigger. It never overwrites: a taken name is
// refused with the core's own sentence.
func (d *NDJSONDriver) SubmitTriggerCreate(ctx context.Context, p TriggerCreateParams) (*TriggerRow, error) {
	switch {
	case strings.TrimSpace(p.Name) == "":
		return nil, fmt.Errorf("ndjson: trigger.create with an empty name; a trigger is addressed by its name")
	case p.Budget <= 0:
		return nil, fmt.Errorf("ndjson: trigger.create without a spend ceiling above zero; an unattended run with no limit is refused")
	}
	params := map[string]any{
		"name": p.Name, "on": p.On, "then": p.Then,
		"budget": p.Budget, "budget_period": p.BudgetPeriod,
	}
	var r TriggerRow
	if err := d.call(ctx, "trigger-create", "trigger.create", params, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// SubmitTriggerResume lets a paused trigger fire again. Resuming an active trigger
// is not an error.
func (d *NDJSONDriver) SubmitTriggerResume(ctx context.Context, name string) (*TriggerRow, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("ndjson: trigger.resume with an empty name")
	}
	var r TriggerRow
	if err := d.call(ctx, "trigger-resume", "trigger.resume", map[string]any{"name": name}, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// SubmitTriggerPause stops a trigger from firing. Pausing a paused trigger is not
// an error.
func (d *NDJSONDriver) SubmitTriggerPause(ctx context.Context, name string) (*TriggerRow, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("ndjson: trigger.pause with an empty name")
	}
	var r TriggerRow
	if err := d.call(ctx, "trigger-pause", "trigger.pause", map[string]any{"name": name}, &r); err != nil {
		return nil, err
	}
	return &r, nil
}
