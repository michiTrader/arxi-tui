package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sse(t *testing.T, status int, ctype, body string) (*Client, *string) {
	t.Helper()
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = string(b)
		w.Header().Set("Content-Type", ctype)
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL}, &seen
}

const goodStream = `data: {"id":"a","model":"m","choices":[{"delta":{"reasoning_content":"Hmm "}}]}

data: {"choices":[{"delta":{"reasoning":"ok."}}]}

data: {"choices":[{"delta":{"content":"Hel"}}]}

data: {"choices":[{"delta":{"content":"lo"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":6}}

data: [DONE]

`

func TestCompleteStreamAssemblesTheReplyAndReportsThinking(t *testing.T) {
	c, seen := sse(t, 200, "text/event-stream", goodStream)
	var frags []string
	resp, err := c.CompleteStream(context.Background(), chatRequest{Model: "m"}, func(s string) { frags = append(frags, s) })
	if err != nil {
		t.Fatal(err)
	}
	if text, ok := resp.text(); !ok || text != "Hello" {
		t.Errorf("text = %q, %v", text, ok)
	}
	if resp.Usage.PromptTokens != 4 || resp.Usage.CompletionTokens != 6 || resp.finishReason() != "stop" {
		t.Errorf("usage/finish lost: %+v %q", resp.Usage, resp.finishReason())
	}
	if strings.Join(frags, "|") != "Hmm |ok." {
		t.Errorf("thinking = %q", frags)
	}
	if !strings.Contains(*seen, `"stream":true`) || !strings.Contains(*seen, `"include_usage":true`) {
		t.Errorf("request did not ask for a stream with usage: %s", *seen)
	}
}

func TestCompleteStreamKeepsRefusalSemantics(t *testing.T) {
	c, _ := sse(t, 504, "text/html", "gateway timeout")
	_, err := c.CompleteStream(context.Background(), chatRequest{Model: "m"}, nil)
	var api *APIError
	if !errors.As(err, &api) || api.Status != 504 || !api.Retryable() {
		t.Fatalf("err = %v; want a retryable *APIError 504", err)
	}
}

func TestCompleteStreamErrorInsideTheStream(t *testing.T) {
	c, _ := sse(t, 200, "text/event-stream", "data: {\"error\":{\"message\":\"overloaded\",\"type\":\"busy\"}}\n\n")
	_, err := c.CompleteStream(context.Background(), chatRequest{Model: "m"}, nil)
	var api *APIError
	if !errors.As(err, &api) || !strings.Contains(api.Message, "overloaded") {
		t.Fatalf("err = %v; want the in-stream error as an *APIError", err)
	}
}

func TestCompleteStreamAcceptsAServerThatIgnoresStream(t *testing.T) {
	c, _ := sse(t, 200, "application/json", `{"id":"x","choices":[{"message":{"role":"assistant","content":"plain"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	resp, err := c.CompleteStream(context.Background(), chatRequest{Model: "m"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := resp.text(); text != "plain" {
		t.Errorf("text = %q", text)
	}
}

func TestCompleteStreamGarbageAndEmpty(t *testing.T) {
	c, _ := sse(t, 200, "text/event-stream", "data: not json\n\n")
	if _, err := c.CompleteStream(context.Background(), chatRequest{Model: "m"}, nil); err == nil || !strings.Contains(err.Error(), "not JSON") {
		t.Errorf("garbage err = %v", err)
	}
	c, _ = sse(t, 200, "text/event-stream", "")
	if _, err := c.CompleteStream(context.Background(), chatRequest{Model: "m"}, nil); err == nil || !strings.Contains(err.Error(), "empty stream") {
		t.Errorf("empty err = %v", err)
	}
}

func TestCompleteStreamNilCallback(t *testing.T) {
	c, _ := sse(t, 200, "text/event-stream", goodStream)
	if _, err := c.CompleteStream(context.Background(), chatRequest{Model: "m"}, nil); err != nil {
		t.Fatal(err)
	}
}

const toolStream = `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"read","arguments":""}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"list"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a.go\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}

data: [DONE]

`

func TestCompleteStreamAssemblesFragmentedToolCalls(t *testing.T) {
	c, _ := sse(t, 200, "text/event-stream", toolStream)
	resp, err := c.CompleteStream(context.Background(), chatRequest{Model: "m"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := canonicalOpenAIResponse(resp)
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	for _, b := range got.Content {
		if b.ToolCall != nil {
			calls = append(calls, b.ToolCall.ID+" "+b.ToolCall.Name+" "+string(b.ToolCall.Arguments))
		}
	}
	want := `call_a read {"path":"a.go"}|call_b list {}`
	if strings.Join(calls, "|") != want {
		t.Errorf("tool calls = %q, want %q", strings.Join(calls, "|"), want)
	}
	if resp.finishReason() != "tool_calls" || resp.Usage.PromptTokens != 3 {
		t.Errorf("finish/usage lost: %q %+v", resp.finishReason(), resp.Usage)
	}
}

func TestCompleteStreamRefusesAWildToolCallIndex(t *testing.T) {
	c, _ := sse(t, 200, "text/event-stream", `data: {"choices":[{"delta":{"tool_calls":[{"index":9999,"id":"x"}]}}]}`+"\n\n")
	if _, err := c.CompleteStream(context.Background(), chatRequest{Model: "m"}, nil); err == nil {
		t.Fatal("an absurd tool call index must be refused")
	}
}
