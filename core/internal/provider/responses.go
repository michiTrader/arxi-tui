package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/michiTrader/arxi/internal/turn"
)

// The OpenAI Responses wire (POST {base}/responses). Some gateways serve a model only
// here: the same model on /chat/completions answers 200 with an EMPTY body, which used to
// surface as "unexpected end of JSON input". The provider-neutral contract in
// internal/turn is translated to and from this shape, so nothing above the provider
// package knows which wire a turn used.

type responsesItem struct {
	Type string `json:"type,omitempty"`
	Role string `json:"role,omitempty"`
	// Content is a string for plain text or a list of typed parts when a picture rides along.
	Content any `json:"content,omitempty"`

	// function_call
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`

	// function_call_output
	Output string `json:"output,omitempty"`
}

type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type responsesRequest struct {
	Model           string          `json:"model"`
	Instructions    string          `json:"instructions,omitempty"`
	Input           []responsesItem `json:"input"`
	Tools           []responsesTool `json:"tools,omitempty"`
	MaxOutputTokens int             `json:"max_output_tokens,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	Stream          bool            `json:"stream"`
	// Store is always false: arxi keeps the history itself and resends it, so the provider
	// has no reason to keep a copy of the conversation.
	Store bool `json:"store"`
}

type responsesOutputPart struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`
	Refusal string `json:"refusal,omitempty"`
}

type responsesOutput struct {
	Type      string                `json:"type"`
	Content   []responsesOutputPart `json:"content,omitempty"`
	CallID    string                `json:"call_id,omitempty"`
	ID        string                `json:"id,omitempty"`
	Name      string                `json:"name,omitempty"`
	Arguments string                `json:"arguments,omitempty"`
}

type responsesError struct {
	Message string `json:"message"`
	Code    any    `json:"code,omitempty"`
	Type    string `json:"type,omitempty"`
}

func (e *responsesError) String() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("%v", e.Code)
}

type responsesResponse struct {
	ID         string            `json:"id"`
	Model      string            `json:"model"`
	Status     string            `json:"status"`
	Output     []responsesOutput `json:"output"`
	OutputText string            `json:"output_text,omitempty"`
	Error      *responsesError   `json:"error,omitempty"`
	Incomplete *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details,omitempty"`
	Usage struct {
		InputTokens        int `json:"input_tokens"`
		OutputTokens       int `json:"output_tokens"`
		InputTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"input_tokens_details"`
	} `json:"usage"`
}

// responsesTurnRequest translates the provider-neutral request to the Responses wire.
func responsesTurnRequest(req turn.Request) (responsesRequest, error) {
	if req.Schema != turn.Schema {
		return responsesRequest{}, fmt.Errorf("turn schema %q, want %q", req.Schema, turn.Schema)
	}
	if req.Stream {
		return responsesRequest{}, fmt.Errorf("streaming is not implemented for OpenAI Responses")
	}
	out := responsesRequest{Model: req.Model, MaxOutputTokens: req.MaxTokens, Temperature: req.Temperature}
	var system []string
	for i, msg := range req.Messages {
		switch msg.Role {
		case turn.RoleSystem:
			text, err := textContent(msg.Content)
			if err != nil {
				return responsesRequest{}, fmt.Errorf("message %d: %w", i, err)
			}
			system = append(system, text)
		case turn.RoleUser:
			if hasImage(msg.Content) {
				parts := make([]map[string]any, 0, len(msg.Content))
				for _, b := range msg.Content {
					switch b.Type {
					case turn.BlockText:
						parts = append(parts, map[string]any{"type": "input_text", "text": b.Text})
					case turn.BlockImage:
						mt, data, err := imageData(b)
						if err != nil {
							return responsesRequest{}, fmt.Errorf("message %d: %w", i, err)
						}
						parts = append(parts, map[string]any{"type": "input_image",
							"image_url": "data:" + mt + ";base64," + data})
					default:
						return responsesRequest{}, fmt.Errorf("message %d: content type %q is not supported by OpenAI Responses", i, b.Type)
					}
				}
				out.Input = append(out.Input, responsesItem{Type: "message", Role: "user", Content: parts})
				continue
			}
			text, err := textContent(msg.Content)
			if err != nil {
				return responsesRequest{}, fmt.Errorf("message %d: %w", i, err)
			}
			out.Input = append(out.Input, responsesItem{Type: "message", Role: "user", Content: text})
		case turn.RoleAssistant:
			var text strings.Builder
			var calls []responsesItem
			for _, b := range msg.Content {
				switch b.Type {
				case turn.BlockText:
					text.WriteString(b.Text)
				case turn.BlockToolCall:
					if b.ToolCall == nil {
						return responsesRequest{}, fmt.Errorf("message %d: assistant tool_call block has no call", i)
					}
					if err := turn.ValidateToolCall(*b.ToolCall); err != nil {
						return responsesRequest{}, fmt.Errorf("message %d: %w", i, err)
					}
					calls = append(calls, responsesItem{Type: "function_call", CallID: b.ToolCall.ID,
						Name: b.ToolCall.Name, Arguments: string(b.ToolCall.Arguments)})
				default:
					return responsesRequest{}, fmt.Errorf("message %d: assistant content type %q is not supported by OpenAI Responses", i, b.Type)
				}
			}
			if text.Len() > 0 {
				out.Input = append(out.Input, responsesItem{Type: "message", Role: "assistant", Content: text.String()})
			}
			out.Input = append(out.Input, calls...)
		case turn.RoleTool:
			for _, b := range msg.Content {
				if b.Type != turn.BlockToolResult || b.ToolResult == nil {
					return responsesRequest{}, fmt.Errorf("message %d: tool message contains non-result block %q", i, b.Type)
				}
				text, err := textContent(b.ToolResult.Content)
				if err != nil {
					return responsesRequest{}, fmt.Errorf("tool result %s: %w", b.ToolResult.CallID, err)
				}
				out.Input = append(out.Input, responsesItem{Type: "function_call_output",
					CallID: b.ToolResult.CallID, Output: text})
			}
		default:
			return responsesRequest{}, fmt.Errorf("unsupported turn role %q", msg.Role)
		}
	}
	out.Instructions = strings.Join(system, "\n\n")
	for _, t := range req.Tools {
		out.Tools = append(out.Tools, responsesTool{Type: "function", Name: t.Name,
			Description: t.Description, Parameters: t.InputSchema})
	}
	return out, nil
}

// canonicalResponsesResponse translates a Responses reply to the provider-neutral one.
func canonicalResponsesResponse(resp *responsesResponse) (turn.Response, error) {
	out := turn.Response{Schema: turn.Schema}
	if resp == nil {
		return out, fmt.Errorf("provider returned no response")
	}
	out.ID, out.Model = resp.ID, resp.Model
	out.Usage = turn.Usage{InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens,
		CacheReadTokens: resp.Usage.InputTokensDetails.CachedTokens}
	if resp.Error != nil {
		out.FinishReason = turn.FinishRefusal
		out.Refusal = &turn.Refusal{Message: resp.Error.String()}
		return out, nil
	}
	var text strings.Builder
	refusal := ""
	kept := 0
	for i, item := range resp.Output {
		switch item.Type {
		case "message":
			for _, p := range item.Content {
				switch p.Type {
				case "output_text":
					text.WriteString(p.Text)
				case "refusal":
					refusal += p.Refusal
				}
			}
		case "function_call":
			if strings.TrimSpace(item.Name) == "" {
				continue
			}
			id := item.CallID
			if strings.TrimSpace(id) == "" {
				id = item.ID
			}
			if strings.TrimSpace(id) == "" {
				id = fmt.Sprintf("call_%d", i)
			}
			args := strings.TrimSpace(item.Arguments)
			if args == "" {
				args = "{}"
			}
			if !json.Valid([]byte(args)) {
				return cutToolCall(out, item.Name, ""), nil
			}
			call, err := turn.NewToolCall(id, item.Name, []byte(args))
			if err != nil {
				return cutToolCall(out, item.Name, ""), nil
			}
			if text.Len() > 0 {
				out.Content = append(out.Content, turn.ContentBlock{Type: turn.BlockText, Text: text.String()})
				text.Reset()
			}
			out.Content = append(out.Content, turn.ContentBlock{Type: turn.BlockToolCall, ToolCall: &call})
			kept++
		}
		// reasoning items and anything else are not part of the visible answer.
	}
	if text.Len() == 0 && kept == 0 && resp.OutputText != "" {
		text.WriteString(resp.OutputText)
	}
	if text.Len() > 0 {
		out.Content = append(out.Content, turn.ContentBlock{Type: turn.BlockText, Text: text.String()})
	}
	switch {
	case refusal != "" && kept == 0 && text.Len() == 0:
		out.FinishReason = turn.FinishRefusal
		out.Refusal = &turn.Refusal{Code: "refusal", Message: refusal}
	case kept > 0:
		out.FinishReason = turn.FinishToolCalls
	case resp.Status == "incomplete" && resp.Incomplete != nil && resp.Incomplete.Reason == "max_output_tokens":
		out.FinishReason = turn.FinishLength
	case resp.Status == "incomplete" && resp.Incomplete != nil && resp.Incomplete.Reason == "content_filter":
		out.FinishReason = turn.FinishRefusal
		out.Refusal = &turn.Refusal{Code: "content_filter", Message: "the provider filtered the response"}
	default:
		out.FinishReason = turn.FinishStop
	}
	return out, nil
}

// CompleteResponses performs one call on the Responses wire. Its error contract is that of
// Complete: *APIError for an answered refusal, anything else for transport.
func (c *Client) CompleteResponses(ctx context.Context, req responsesRequest) (*responsesResponse, error) {
	if req.Model == "" {
		return nil, errors.New("a Responses call needs a model; the caller resolved nothing")
	}
	key, err := c.credential()
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode the request for %s: %w", req.Model, err)
	}
	url := strings.TrimSuffix(c.BaseURL, "/") + "/responses"
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build the request for %s: %w", req.Model, err)
	}
	hreq.Header.Set("User-Agent", UserAgent)
	hreq.Header.Set("Content-Type", "application/json")
	if key != "" {
		hreq.Header.Set("Authorization", "Bearer "+key)
	}
	client := c.HTTP
	if client == nil {
		client = DefaultHTTP()
	}
	resp, err := client.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("call %s for model %s: %w", url, req.Model, err)
	}
	defer resp.Body.Close()
	c.noteRateLimit(resp.Header)
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read the reply from %s for model %s: %w", url, req.Model, err)
	}
	if werr := webReplyError(url, resp.StatusCode, raw, req.Model); werr != nil {
		return nil, werr
	}
	parsed, decErr := decodeResponsesBody(raw)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := strings.TrimSpace(truncate(string(raw), 512))
		if decErr == nil && parsed.Error != nil {
			msg = parsed.Error.String()
		}
		apiErr := &APIError{Status: resp.StatusCode, Message: msg, Model: req.Model, RetryAfter: retryAfterHeader(resp.Header)}
		if decErr == nil {
			return parsed, apiErr
		}
		return nil, apiErr
	}
	if decErr != nil {
		if len(bytes.TrimSpace(raw)) == 0 {
			return nil, emptyReplyError(url, req.Model)
		}
		return nil, decErr
	}
	if parsed.Error != nil {
		return parsed, &APIError{Status: resp.StatusCode, Message: parsed.Error.String(), Model: req.Model}
	}
	return parsed, nil
}

func decodeResponsesBody(raw []byte) (*responsesResponse, error) {
	var out responsesResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("provider reply is not JSON (%v): %s", err, truncate(string(raw), 200))
	}
	return &out, nil
}

// emptyReplyError explains an empty 200: the route answers but serves nothing for this
// model, which is how a gateway that only offers /responses treats /chat/completions.
// Said in terms of the remedy because "unexpected end of JSON input" names neither.
func emptyReplyError(url, model string) error {
	return fmt.Errorf("%s answered with an empty body for model %s: the provider does not serve this model on this route, "+
		"nothing was billed. Set the provider's API format to the one its docs show (for a /v1/responses endpoint: "+
		"arxi provider update <name> --protocol responses, or choose it in /provider -> edit -> API format)", url, model)
}
