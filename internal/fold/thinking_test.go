package fold

import (
	"strings"
	"testing"
)

func thinkingEvents(frags ...string) []Event {
	evs := []Event{{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "hi"}}, {Type: "agent.activated", Seq: 2, Payload: map[string]any{"agent": "assistant"}}}
	for i, f := range frags {
		evs = append(evs, Event{Type: "chat.thinking", Seq: int64(3 + i), Payload: map[string]any{"text": f}})
	}
	return evs
}

// Streamed fragments join into one line: pieces of words stay glued, newlines and
// runs of spaces collapse to a single space.
func TestThinkingFragmentsJoinIntoOneLine(t *testing.T) {
	st := Fold(thinkingEvents("The us", "er wants\n\n", "a  greeting", ". I should", " reply."))
	if want := "The user wants a greeting. I should reply."; st.ThinkingText != want {
		t.Fatalf("ThinkingText = %q, want %q", st.ThinkingText, want)
	}
	if !st.AgentWorking {
		t.Fatal("the agent should still be working")
	}
}

// Thinking is never part of the transcript.
func TestThinkingDoesNotEnterTheTranscript(t *testing.T) {
	st := Fold(thinkingEvents("secret reasoning"))
	for _, h := range st.History {
		if strings.Contains(h.Text, "secret") {
			t.Fatalf("thinking leaked into the chat history: %+v", h)
		}
	}
}

// Every way a turn ends clears the thinking, so the next turn starts empty.
func TestThinkingEndsWithTheTurn(t *testing.T) {
	for _, end := range []Event{
		{Type: "llm.response", Payload: map[string]any{"text": "ok"}},
		{Type: "agent.turn_done", Payload: map[string]any{"agent": "assistant"}},
		{Type: "agent.failed", Payload: map[string]any{"agent": "assistant"}},
		{Type: "chat.error", Payload: map[string]any{"text": "boom"}},
		{Type: "chat.cancelled", Payload: map[string]any{}},
	} {
		end.Seq = 99
		if st := Fold(append(thinkingEvents("pondering"), end)); st.ThinkingText != "" {
			t.Errorf("%s left thinking behind: %q", end.Type, st.ThinkingText)
		}
	}
}

// A long reasoning keeps only its recent words, starting on a word boundary.
func TestThinkingKeepsTheRecentWords(t *testing.T) {
	var frags []string
	for i := 0; i < 200; i++ {
		frags = append(frags, "word ")
	}
	got := Fold(thinkingEvents(frags...)).ThinkingText
	if n := len([]rune(got)); n > thinkingKeep {
		t.Fatalf("kept %d characters, want at most %d", n, thinkingKeep)
	}
	if !strings.HasPrefix(got, "word") {
		t.Fatalf("the window starts mid-word: %q", got[:12])
	}
}
