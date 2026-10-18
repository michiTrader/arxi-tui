package provider

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/michiTrader/arxi/internal/model"
	"github.com/michiTrader/arxi/internal/turn"
)

// openAIRequest translates the provider-neutral contract to Chat Completions.
func openAIRequest(req turn.Request) (chatRequest, error) {
	if req.Schema != turn.Schema {
		return chatRequest{}, fmt.Errorf("turn schema %q, want %q", req.Schema, turn.Schema)
	}
	if req.Stream {
		return chatRequest{}, fmt.Errorf("streaming is not implemented for OpenAI Chat Completions")
	}
	out := chatRequest{
		Model: req.Model, MaxTokens: req.MaxTokens,
		Temperature: req.Temperature, Stream: req.Stream,
	}
	// The level is a word of the shared vocabulary; what the model's API calls
	// it is decided per model (internal/model/effort.go).
	wire := model.OpenAIWire(req.Model, req.Effort)
	out.ReasoningEffort = wire.Reasoning
	if wire.Thinking != "" {
		out.Thinking = &chatThinking{Type: wire.Thinking}
	}
	for i, msg := range req.Messages {
		wire, err := openAIMessage(msg)
		if err != nil {
			return chatRequest{}, fmt.Errorf("message %d: %w", i, err)
		}
		out.Messages = append(out.Messages, wire...)
	}
	for _, tool := range req.Tools {
		out.Tools = append(out.Tools, chatTool{Type: "function", Function: chatToolFunction{
			Name: tool.Name, Description: tool.Description, Parameters: tool.InputSchema,
		}})
	}
	return out, nil
}

func openAIMessage(msg turn.Message) ([]chatMessage, error) {
	switch msg.Role {
	case turn.RoleUser:
		if hasImage(msg.Content) {
			parts, err := openAIParts(msg.Content)
			if err != nil {
				return nil, err
			}
			return []chatMessage{{Role: "user", Content: parts}}, nil
		}
		text, err := textContent(msg.Content)
		if err != nil {
			return nil, err
		}
		return []chatMessage{{Role: "user", Content: text}}, nil
	case turn.RoleSystem:
		text, err := textContent(msg.Content)
		if err != nil {
			return nil, err
		}
		return []chatMessage{{Role: string(msg.Role), Content: text}}, nil
	case turn.RoleAssistant:
		var text strings.Builder
		wire := chatMessage{Role: "assistant"}
		for _, block := range msg.Content {
			switch block.Type {
			case turn.BlockText:
				text.WriteString(block.Text)
			case turn.BlockToolCall:
				if block.ToolCall == nil {
					return nil, fmt.Errorf("assistant tool_call block has no call")
				}
				if err := turn.ValidateToolCall(*block.ToolCall); err != nil {
					return nil, err
				}
				wire.ToolCalls = append(wire.ToolCalls, chatToolCall{
					ID: block.ToolCall.ID, Type: "function", Function: chatToolFunction{
						Name: block.ToolCall.Name, Arguments: string(block.ToolCall.Arguments),
					},
				})
			default:
				return nil, fmt.Errorf("assistant content type %q is not supported by OpenAI Chat Completions", block.Type)
			}
		}
		if text.Len() > 0 {
			wire.Content = text.String()
		} else if len(wire.ToolCalls) > 0 {
			wire.Content = nil
		} else {
			wire.Content = ""
		}
		return []chatMessage{wire}, nil
	case turn.RoleTool:
		out := make([]chatMessage, 0, len(msg.Content))
		for _, block := range msg.Content {
			if block.Type != turn.BlockToolResult || block.ToolResult == nil {
				return nil, fmt.Errorf("tool message contains non-result block %q", block.Type)
			}
			text, err := textContent(block.ToolResult.Content)
			if err != nil {
				return nil, fmt.Errorf("tool result %s: %w", block.ToolResult.CallID, err)
			}
			out = append(out, chatMessage{Role: "tool", ToolCallID: block.ToolResult.CallID, Content: text})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported turn role %q", msg.Role)
	}
}

func hasImage(blocks []turn.ContentBlock) bool {
	for _, b := range blocks {
		if b.Type == turn.BlockImage {
			return true
		}
	}
	return false
}

// openAIParts turns a user message that carries pictures into the content
// array form: text parts and image_url parts, in the order they were sent.
func openAIParts(blocks []turn.ContentBlock) ([]map[string]any, error) {
	parts := make([]map[string]any, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case turn.BlockText:
			parts = append(parts, map[string]any{"type": "text", "text": b.Text})
		case turn.BlockImage:
			mt, data, err := imageData(b)
			if err != nil {
				return nil, err
			}
			parts = append(parts, map[string]any{"type": "image_url",
				"image_url": map[string]string{"url": "data:" + mt + ";base64," + data}})
		default:
			return nil, fmt.Errorf("content type %q is not supported by OpenAI Chat Completions", b.Type)
		}
	}
	return parts, nil
}

func textContent(blocks []turn.ContentBlock) (string, error) {
	var text strings.Builder
	for _, block := range blocks {
		if block.Type != turn.BlockText {
			return "", fmt.Errorf("content type %q is not supported by OpenAI Chat Completions", block.Type)
		}
		text.WriteString(block.Text)
	}
	return text.String(), nil
}

func canonicalOpenAIResponse(resp *chatResponse) (turn.Response, error) {
	out := turn.Response{Schema: turn.Schema}
	if resp == nil {
		return out, fmt.Errorf("provider returned no response")
	}
	out.ID, out.Model = resp.ID, resp.Model
	out.Usage = turn.Usage{
		InputTokens: resp.Usage.PromptTokens, OutputTokens: resp.Usage.CompletionTokens,
		CacheReadTokens: resp.Usage.PromptTokensDetails.CachedTokens,
	}
	if resp.Error != nil {
		out.FinishReason = turn.FinishRefusal
		out.Refusal = &turn.Refusal{Message: resp.Error.String()}
		return out, nil
	}
	if len(resp.Choices) == 0 {
		return out, fmt.Errorf("provider response %q has no choices", resp.ID)
	}
	choice := resp.Choices[0]
	if choice.Message.Content != nil {
		out.Content = append(out.Content, turn.ContentBlock{Type: turn.BlockText, Text: *choice.Message.Content})
	}
	kept, dropped := 0, 0
	for i, wire := range choice.Message.ToolCalls {
		if wire.Type != "" && wire.Type != "function" {
			return out, fmt.Errorf("provider tool call %q has unsupported type %q", wire.ID, wire.Type)
		}
		// Some gateways (vLLM-style ones among them) stream a call whose function has no
		// name: a stray fragment, not something the model asked for. There is nothing to
		// run, and failing the whole turn over it threw away a good answer, so it is
		// dropped and counted.
		if strings.TrimSpace(wire.Function.Name) == "" {
			dropped++
			continue
		}
		// A gateway may also leave the id out; the call is still well formed.
		if strings.TrimSpace(wire.ID) == "" {
			wire.ID = fmt.Sprintf("call_%d", i)
		}
		// A tool that takes no arguments may stream none at all.
		rawArgs := strings.TrimSpace(wire.Function.Arguments)
		if rawArgs == "" {
			rawArgs = "{}"
		}
		if !json.Valid([]byte(rawArgs)) {
			// The arguments stop in the middle: the call was cut off, either by the
			// output limit (finish "length") or by a stream that ended early. Nothing
			// can be run from half a call, and it is not the model's mistake or the
			// user's, so it is reported as what it is instead of "unexpected EOF".
			return cutToolCall(out, wire.Function.Name, choice.FinishReason), nil
		}
		call, err := turn.NewToolCall(wire.ID, wire.Function.Name, []byte(rawArgs))
		if err != nil {
			// Valid JSON that is not a call's arguments (a bare number, a list, a
			// stream that stopped at a lucky place) is as unusable as half a call, and
			// as likely to be gone on the next ask: a refusal that is asked again, not
			// a turn lost to "decode tool arguments".
			return cutToolCall(out, wire.Function.Name, ""), nil
		}
		out.Content = append(out.Content, turn.ContentBlock{Type: turn.BlockToolCall, ToolCall: &call})
		kept++
	}
	if dropped > 0 && kept == 0 && !hasText(out.Content) {
		// The model meant to call a tool and the call arrived without its name. Asking
		// again usually fixes it, so this is a transient refusal, not a failed turn.
		out.FinishReason = turn.FinishRefusal
		out.Refusal = &turn.Refusal{Code: "http_502", Retryable: true, Message: "the provider sent a tool call with no name (a broken reply, not an answer)"}
		return out, nil
	}
	if presentJSON(choice.Message.FunctionCall) {
		return out, fmt.Errorf("legacy function_call has no provider call ID and cannot be resumed safely")
	}
	out.FinishReason = openAIFinishReason(choice.FinishReason, kept > 0)
	if out.FinishReason == turn.FinishRefusal {
		out.Refusal = &turn.Refusal{Code: "content_filter", Message: "the provider filtered the response"}
	}
	if out.FinishReason == turn.FinishError {
		out.Refusal = &turn.Refusal{Code: "unsupported_finish_reason", Message: fmt.Sprintf("the provider returned unsupported finish reason %q", choice.FinishReason)}
	}
	return out, nil
}

// OutputLimitCode marks the refusal of a reply that ran out of output allowance while it
// was writing a tool call. The tool loop answers it by asking for smaller steps; a
// stream that merely ended early is the transient http_502 and is asked again as it was.
const OutputLimitCode = "output_limit"

// cutToolCall turns a tool call whose arguments were cut off into a refusal, so the turn
// is not lost to a decode error. Nothing in the reply is kept: a call that ran without
// its siblings, or an answer built on a call that did not, would be worse than asking
// again.
func cutToolCall(out turn.Response, name, finish string) turn.Response {
	out.Content = nil
	out.FinishReason = turn.FinishRefusal
	if finish == "length" {
		out.Refusal = &turn.Refusal{Code: OutputLimitCode, Message: fmt.Sprintf(
			"the model ran out of output allowance while writing a call to %q (a long file or edit), so nothing was run. Ask for it in smaller steps, or pick a model with a larger output limit", name)}
		return out
	}
	out.Refusal = &turn.Refusal{Code: "http_502", Retryable: true, Message: fmt.Sprintf(
		"the provider's reply ended in the middle of a call to %q (a broken stream, not an answer)", name)}
	return out
}

func openAIFinishReason(reason string, hasTools bool) turn.FinishReason {
	if hasTools || reason == "tool_calls" || reason == "function_call" {
		return turn.FinishToolCalls
	}
	switch reason {
	case "stop":
		return turn.FinishStop
	case "length":
		return turn.FinishLength
	case "content_filter":
		return turn.FinishRefusal
	default:
		return turn.FinishError
	}
}

func decodeArguments(raw json.RawMessage) (map[string]any, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var args map[string]any
	if err := dec.Decode(&args); err != nil {
		return nil, fmt.Errorf("decode canonical tool arguments: %w", err)
	}
	return args, nil
}

// hasText reports whether any block carries non-blank text.
func hasText(blocks []turn.ContentBlock) bool {
	for _, b := range blocks {
		if b.Type == turn.BlockText && strings.TrimSpace(b.Text) != "" {
			return true
		}
	}
	return false
}
