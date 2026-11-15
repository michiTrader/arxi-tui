package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/michiTrader/arxi/internal/model"
	"github.com/michiTrader/arxi/internal/turn"
)

func responsesReq(base string, msgs ...turn.Message) turn.Request {
	return turn.Request{Schema: turn.Schema, Provider: "river", Protocol: model.ProtocolOpenAIResponses,
		BaseURL: base, Model: "deepseek-ai/DeepSeek-V4.1-Flash", MaxTokens: 64, Messages: msgs}
}

func userText(s string) turn.Message {
	return turn.Message{Role: turn.RoleUser, Content: []turn.ContentBlock{{Type: turn.BlockText, Text: s}}}
}

// Consequence if this fails: River-style gateways, which serve a model only on /responses,
// get a Chat Completions body and answer empty. Remedy: keep the request shape below.
func TestResponsesRequestShape(t *testing.T) {
	var got map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &got)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"r1","model":"m","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"pong"}]}],"usage":{"input_tokens":3,"output_tokens":1}}`)
	}))
	defer srv.Close()
	sys := turn.Message{Role: turn.RoleSystem, Content: []turn.ContentBlock{{Type: turn.BlockText, Text: "be brief"}}}
	resp, err := (&Executor{}).CompleteTurn(context.Background(), responsesReq(srv.URL+"/v1", sys, userText("ping")))
	if err != nil || resp.FinishReason != turn.FinishStop {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
	if path != "/v1/responses" {
		t.Errorf("path = %q, want /v1/responses", path)
	}
	if got["instructions"] != "be brief" || got["store"] != false || got["stream"] != false || got["max_output_tokens"] != float64(64) {
		t.Errorf("body = %v", got)
	}
	in, _ := got["input"].([]any)
	if len(in) != 1 {
		t.Fatalf("input = %v; the system text must ride in instructions, not input", got["input"])
	}
	if resp.Content[0].Text != "pong" || resp.Usage.InputTokens != 3 {
		t.Errorf("resp = %+v", resp)
	}
}

func TestResponsesToolCallRoundTrip(t *testing.T) {
	var second map[string]any
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		b, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			io.WriteString(w, `{"id":"r1","status":"completed","output":[{"type":"function_call","call_id":"c1","name":"read","arguments":"{\"path\":\"a\"}"}]}`)
			return
		}
		json.Unmarshal(b, &second)
		io.WriteString(w, `{"id":"r2","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"done"}]}]}`)
	}))
	defer srv.Close()
	tools := []turn.ToolDefinition{{Name: "read", Description: "d", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	req := responsesReq(srv.URL, userText("go"))
	req.Tools = tools
	x := &Executor{}
	resp, err := x.CompleteTurn(context.Background(), req)
	if err != nil || resp.FinishReason != turn.FinishToolCalls {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
	var call *turn.ToolCall
	for _, b := range resp.Content {
		if b.ToolCall != nil {
			call = b.ToolCall
		}
	}
	if call == nil || call.ID != "c1" || call.Name != "read" {
		t.Fatalf("call = %+v", call)
	}
	req.Messages = append(req.Messages,
		turn.Message{Role: turn.RoleAssistant, Content: []turn.ContentBlock{{Type: turn.BlockToolCall, ToolCall: call}}},
		turn.Message{Role: turn.RoleTool, Content: []turn.ContentBlock{{Type: turn.BlockToolResult, ToolResult: &turn.ToolResult{
			CallID: "c1", Content: []turn.ContentBlock{{Type: turn.BlockText, Text: "file body"}}}}}})
	if resp, err = x.CompleteTurn(context.Background(), req); err != nil || resp.FinishReason != turn.FinishStop {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
	in, _ := second["input"].([]any)
	if len(in) != 3 {
		t.Fatalf("input = %v", in)
	}
	fc, _ := in[1].(map[string]any)
	fo, _ := in[2].(map[string]any)
	if fc["type"] != "function_call" || fc["call_id"] != "c1" || fo["type"] != "function_call_output" || fo["output"] != "file body" {
		t.Errorf("tool items = %v / %v", fc, fo)
	}
}

// Consequence if this fails: the user sees "unexpected end of JSON input" and cannot tell
// the route is wrong. Remedy: the message must name the provider-format setting.
func TestResponsesEmptyReplyNamesTheRemedy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	_, err := (&Executor{}).CompleteTurn(context.Background(), responsesReq(srv.URL, userText("x")))
	if err == nil || !strings.Contains(err.Error(), "empty body") || !strings.Contains(err.Error(), "--protocol responses") {
		t.Fatalf("err = %v", err)
	}
}

func TestResponsesIncompleteAndRefusal(t *testing.T) {
	out, _ := canonicalResponsesResponse(&responsesResponse{Status: "incomplete",
		Incomplete: &struct {
			Reason string `json:"reason"`
		}{"max_output_tokens"},
		Output: []responsesOutput{{Type: "message", Content: []responsesOutputPart{{Type: "output_text", Text: "part"}}}}})
	if out.FinishReason != turn.FinishLength {
		t.Errorf("incomplete = %v", out.FinishReason)
	}
	out, _ = canonicalResponsesResponse(&responsesResponse{Status: "completed",
		Output: []responsesOutput{{Type: "message", Content: []responsesOutputPart{{Type: "refusal", Refusal: "no"}}}}})
	if out.FinishReason != turn.FinishRefusal || out.Refusal == nil {
		t.Errorf("refusal = %+v", out)
	}
}

func TestDetectFindsAResponsesOnlyGateway(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {}) // empty 200
	mux.HandleFunc("/v1/responses", apiComplaint)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	if got := DetectProtocol(context.Background(), srv.URL+"/v1", "k", srv.Client()); got != model.ProtocolOpenAIResponses {
		t.Fatalf("got %q", got)
	}
}

func TestParseProtocolAndKnownWire(t *testing.T) {
	for _, w := range []string{"responses", "openai-responses", model.ProtocolOpenAIResponses} {
		if p, ok := model.ParseProtocol(w); !ok || p != model.ProtocolOpenAIResponses {
			t.Errorf("%q -> %q %v", w, p, ok)
		}
	}
	if !model.KnownWire(model.ProtocolOpenAIResponses) || model.KnownWire("x") {
		t.Error("KnownWire wrong")
	}
}
