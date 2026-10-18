package provider

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi/internal/turn"
)

func cutReply(finish, args string) *chatResponse {
	return &chatResponse{ID: "x", Choices: []chatChoice{{FinishReason: finish, Message: chatResponseMessage{
		ToolCalls: []chatToolCall{{ID: "chatcmpl-tool-1", Type: "function", Function: chatToolFunction{Name: "edit", Arguments: args}}},
	}}}}
}

// A call whose arguments stop in the middle used to fail the whole turn with "decode tool
// arguments: unexpected EOF". A stream that just ended early is a broken reply to be asked
// again, like the nameless call.
func TestAToolCallCutByAnEarlyEndIsAskedAgain(t *testing.T) {
	resp, err := canonicalOpenAIResponse(cutReply("tool_calls", `{"path":"a.go","new_string":"func main() {`))
	if err != nil {
		t.Fatalf("a cut call must not fail the turn: %v", err)
	}
	if resp.FinishReason != turn.FinishRefusal || resp.Refusal == nil || resp.Refusal.Code != "http_502" || !resp.Refusal.Retryable {
		t.Fatalf("want a retryable refusal, got finish=%v refusal=%+v", resp.FinishReason, resp.Refusal)
	}
	if len(callsIn(resp)) != 0 {
		t.Errorf("half a call must not be run: %v", callsIn(resp))
	}
	if strings.Contains(resp.Refusal.Message, "EOF") || !strings.Contains(resp.Refusal.Message, "edit") {
		t.Errorf("the message should name the call, not the decoder: %q", resp.Refusal.Message)
	}
}

// Cut at the output limit, asking again would write the same long call and be cut at the
// same place, so it is NOT retryable, and it says why.
func TestAToolCallCutByTheOutputLimitIsNotRetriedAndSaysWhy(t *testing.T) {
	resp, err := canonicalOpenAIResponse(cutReply("length", `{"path":"a.go","new_string":"func main() {`))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	r := resp.Refusal
	if resp.FinishReason != turn.FinishRefusal || r == nil || r.Code != OutputLimitCode || r.Retryable {
		t.Fatalf("want the output-limit refusal, got finish=%v refusal=%+v", resp.FinishReason, r)
	}
	for _, want := range []string{"output allowance", "smaller steps"} {
		if !strings.Contains(r.Message, want) {
			t.Errorf("message should say %q: %q", want, r.Message)
		}
	}
}

// Counterfactual: a whole call, and one that takes no arguments, still run.
func TestACompleteToolCallIsNotTakenForACutOne(t *testing.T) {
	for _, args := range []string{`{"path":"a.go"}`, ``, `{}`} {
		resp, err := canonicalOpenAIResponse(cutReply("tool_calls", args))
		if err != nil || len(callsIn(resp)) != 1 || resp.FinishReason != turn.FinishToolCalls {
			t.Errorf("args %q: err=%v calls=%v finish=%v", args, err, callsIn(resp), resp.FinishReason)
		}
	}
}

// A whole call beside a cut one must not run alone: the model meant them together, and the
// first may depend on the second. The whole reply is refused and asked for again.
func TestAWholeCallBesideACutOneDoesNotRunAlone(t *testing.T) {
	txt := "I will do both"
	resp, err := canonicalOpenAIResponse(&chatResponse{ID: "x", Choices: []chatChoice{{FinishReason: "tool_calls", Message: chatResponseMessage{
		Content: &txt,
		ToolCalls: []chatToolCall{
			{ID: "a", Type: "function", Function: chatToolFunction{Name: "read", Arguments: `{"path":"a"}`}},
			{ID: "b", Type: "function", Function: chatToolFunction{Name: "edit", Arguments: `{"path":"a","new_string":"x`}},
		},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(callsIn(resp)) != 0 || hasText(resp.Content) || resp.FinishReason != turn.FinishRefusal {
		t.Errorf("calls=%v text=%v finish=%v; want nothing kept and a refusal", callsIn(resp), hasText(resp.Content), resp.FinishReason)
	}
}

// Arguments that are valid JSON but not an object (a list, a bare number) cannot be a
// call either: the turn is asked again instead of failing with "decode tool arguments".
func TestArgumentsThatAreNotAnObjectAreAskedAgainNotFatal(t *testing.T) {
	for _, args := range []string{`[]`, `1`, `"x"`, `null`} {
		resp, err := canonicalOpenAIResponse(cutReply("tool_calls", args))
		if err != nil {
			t.Errorf("args %q: must not fail the turn: %v", args, err)
			continue
		}
		if resp.FinishReason != turn.FinishRefusal || resp.Refusal == nil || resp.Refusal.Code != "http_502" || !resp.Refusal.Retryable {
			t.Errorf("args %q: want a retryable refusal, got finish=%v refusal=%+v", args, resp.FinishReason, resp.Refusal)
		}
		if len(callsIn(resp)) != 0 {
			t.Errorf("args %q: nothing may be run: %v", args, callsIn(resp))
		}
	}
}
