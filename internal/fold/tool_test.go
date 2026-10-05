package fold

import "testing"

func toolEvent(seq int64, name, arg string, ok bool, summary string) Event {
	return Event{Type: "chat.tool", Seq: seq, Payload: map[string]any{
		"name": name, "arg": arg, "ok": ok, "summary": summary, "output": "body"}}
}

func roles(st State) []string {
	var out []string
	for _, h := range st.History {
		out = append(out, h.Role)
	}
	return out
}

func TestToolCallsLandBetweenTheQuestionAndTheAnswer(t *testing.T) {
	evs := thinkingEvents()
	evs = append(evs,
		toolEvent(3, "list", ".", true, "Listed 2 entries"),
		toolEvent(4, "read", ".env", false, "looks like it holds secrets"),
		Event{Type: "llm.response", Seq: 5, Payload: map[string]any{"text": "done"}})
	st := Fold(evs)
	got := roles(st)
	want := []string{"user", "tool", "tool", "assistant"}
	if len(got) != len(want) {
		t.Fatalf("roles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("roles = %v, want %v", got, want)
		}
	}
	h := st.History[2]
	if h.Tool != "read" || h.ToolArg != ".env" || h.ToolOK || h.ToolSummary != "looks like it holds secrets" || h.ToolOutput != "body" {
		t.Errorf("tool line = %+v", h)
	}
	if st.History[3].Text != "done" {
		t.Errorf("the answer after the tools must be its own line: %+v", st.History[3])
	}
}

func TestToolCallEndsTheThinkingThatLedToIt(t *testing.T) {
	evs := thinkingEvents("let me look")
	if Fold(evs).ThinkingText == "" {
		t.Fatal("setup: thinking should be showing")
	}
	evs = append(evs, toolEvent(9, "grep", "x", true, "No matches"))
	if got := Fold(evs).ThinkingText; got != "" {
		t.Errorf("ThinkingText = %q after a tool call", got)
	}
}

func TestNamelessToolEventIsIgnored(t *testing.T) {
	st := Fold(append(thinkingEvents(), Event{Type: "chat.tool", Seq: 3, Payload: map[string]any{"arg": "x"}}))
	if len(st.History) != 1 {
		t.Errorf("history = %+v", st.History)
	}
}
