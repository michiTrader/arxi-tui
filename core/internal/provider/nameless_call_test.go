package provider

import (
	"context"
	"testing"

	"github.com/michiTrader/arxi/internal/turn"
)

func callsIn(r turn.Response) (names []string) {
	for _, b := range r.Content {
		if b.Type == turn.BlockToolCall && b.ToolCall != nil {
			names = append(names, b.ToolCall.Name)
		}
	}
	return
}

// A gateway sent a tool call whose function had no name and the whole turn died with
// "tool name is required". With real calls beside it the stray one is dropped.
func TestANamelessToolCallBesideARealOneIsDropped(t *testing.T) {
	resp, err := canonicalOpenAIResponse(&chatResponse{ID: "x", Choices: []chatChoice{{FinishReason: "tool_calls", Message: chatResponseMessage{
		ToolCalls: []chatToolCall{
			{ID: "stray", Type: "function", Function: chatToolFunction{Arguments: `{}`}},
			{ID: "ok", Type: "function", Function: chatToolFunction{Name: "read", Arguments: `{"path":"a"}`}},
		},
	}}}})
	if err != nil {
		t.Fatalf("a stray nameless call must not fail the turn: %v", err)
	}
	if got := callsIn(resp); len(got) != 1 || got[0] != "read" || resp.FinishReason != turn.FinishToolCalls {
		t.Errorf("calls = %v, finish = %v", got, resp.FinishReason)
	}
}

// Only a nameless call and no text: the model meant to call something and it arrived
// broken. That is a retryable refusal (asking again usually works), not a dead turn.
func TestOnlyANamelessToolCallIsARetryableRefusal(t *testing.T) {
	resp, err := canonicalOpenAIResponse(&chatResponse{ID: "x", Choices: []chatChoice{{FinishReason: "tool_calls", Message: chatResponseMessage{
		ToolCalls: []chatToolCall{{ID: "chatcmpl-tool-d108", Type: "function", Function: chatToolFunction{Arguments: `{}`}}},
	}}}})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if resp.FinishReason != turn.FinishRefusal || resp.Refusal == nil || resp.Refusal.Code != "http_502" {
		t.Fatalf("want a transient refusal, got finish=%v refusal=%+v", resp.FinishReason, resp.Refusal)
	}
}

// With text next to the nameless call, the text is the answer.
func TestANamelessToolCallBesideTextKeepsTheText(t *testing.T) {
	txt := "done"
	resp, err := canonicalOpenAIResponse(&chatResponse{ID: "x", Choices: []chatChoice{{FinishReason: "stop", Message: chatResponseMessage{
		Content:   &txt,
		ToolCalls: []chatToolCall{{ID: "s", Type: "function"}},
	}}}})
	if err != nil || resp.FinishReason != turn.FinishStop || len(callsIn(resp)) != 0 {
		t.Fatalf("err=%v finish=%v calls=%v", err, resp.FinishReason, callsIn(resp))
	}
}

// A gateway that leaves the id out still gets a usable call.
func TestAToolCallWithoutAnIDGetsOne(t *testing.T) {
	resp, err := canonicalOpenAIResponse(&chatResponse{ID: "x", Choices: []chatChoice{{FinishReason: "tool_calls", Message: chatResponseMessage{
		ToolCalls: []chatToolCall{{Type: "function", Function: chatToolFunction{Name: "read", Arguments: `{"path":"a"}`}}},
	}}}})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got := callsIn(resp); len(got) != 1 {
		t.Fatalf("calls = %v", got)
	}
	for _, b := range resp.Content {
		if b.ToolCall != nil && b.ToolCall.ID == "" {
			t.Error("the call has no id")
		}
	}
}

// The same on the wire the TUI uses (a stream): a fragment that never carries a name.
func TestAStreamedFragmentWithoutANameDoesNotKillTheTurn(t *testing.T) {
	body := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"chatcmpl-tool-1","type":"function","function":{"arguments":"{}"}}]},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	c, _ := sse(t, 200, "text/event-stream", body)
	raw, err := c.CompleteStream(context.Background(), chatRequest{Model: "m"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := canonicalOpenAIResponse(raw)
	if err != nil {
		t.Fatalf("streamed nameless call failed the turn: %v", err)
	}
	if resp.FinishReason != turn.FinishRefusal || resp.Refusal == nil || !resp.Refusal.Retryable {
		t.Errorf("finish=%v refusal=%+v", resp.FinishReason, resp.Refusal)
	}
}

// A name that arrives in a later fragment is still assembled.
func TestAStreamedNameInALaterFragmentIsKept(t *testing.T) {
	body := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"arguments":""}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"read","arguments":"{\"path\":\"a\"}"}}]},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	c, _ := sse(t, 200, "text/event-stream", body)
	raw, err := c.CompleteStream(context.Background(), chatRequest{Model: "m"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := canonicalOpenAIResponse(raw)
	if err != nil || len(callsIn(resp)) != 1 || callsIn(resp)[0] != "read" {
		t.Fatalf("err=%v calls=%v", err, callsIn(resp))
	}
}
