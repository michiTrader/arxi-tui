package fold

import "testing"

// A line the core took into a running turn is a user line of the conversation, where it
// happened, and the answer after it follows it.
// Consequence: without it the line the model read would not show, and the answer would
// read as a reply to a question the screen never had.
func TestSteeredLineIsAUserLineInPlace(t *testing.T) {
	st := Fold([]Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "build it"}},
		{Type: "agent.activated", Seq: 2, Payload: map[string]any{"agent": "assistant"}},
		{Type: "chat.steered", Seq: 3, Payload: map[string]any{"text": "use tabs"}},
		{Type: "llm.response", Seq: 4, Payload: map[string]any{"text": "ok"}},
	})
	var roles []string
	for _, h := range st.History {
		roles = append(roles, h.Role+":"+h.Text)
	}
	want := []string{"user:build it", "user:use tabs", "assistant:ok"}
	if len(roles) != len(want) {
		t.Fatalf("history %q, want %q", roles, want)
	}
	for i := range want {
		if roles[i] != want[i] {
			t.Fatalf("history %q, want %q", roles, want)
		}
	}
}

// An empty steered line adds nothing.
func TestSteeredEmptyAddsNothing(t *testing.T) {
	st := Fold([]Event{{Type: "chat.steered", Seq: 1, Payload: map[string]any{"text": ""}}})
	if len(st.History) != 0 {
		t.Fatalf("an empty line entered the conversation: %+v", st.History)
	}
}
