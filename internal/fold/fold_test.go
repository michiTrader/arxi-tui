package fold

import "testing"

// TestFoldProjectsChatHistory verifies the fold turns run.prompt + llm.response
// events into chat.history, which is the one run-state bind Phase 0 needs.
func TestFoldProjectsChatHistory(t *testing.T) {
	events := []Event{
		// Simulated core log: user asks, assistant answers.
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "hola"}},
		{Type: "llm.response", Seq: 2, Payload: map[string]any{"text": "Hola! ¿En qué puedo ayudarte?"}},
	}

	s := Fold(events)

	want := "hola\n\nHola! ¿En qué puedo ayudarte?"
	if got := s.ChatHistoryMarkdown(); got != want {
		t.Errorf("ChatHistoryMarkdown: got %q, want %q", got, want)
	}

	if len(s.History) != 2 {
		t.Errorf("expected 2 history lines, got %d", len(s.History))
	}
	if s.History[0].Role != "user" || s.History[0].Text != "hola" {
		t.Errorf("first line: got %+v", s.History[0])
	}
	if s.History[1].Role != "assistant" || s.History[1].Text != "Hola! ¿En qué puedo ayudarte?" {
		t.Errorf("second line: got %+v", s.History[1])
	}
}

// TestFoldAppendableResponse verifies streaming deltas accumulate.
func TestFoldAppendableResponse(t *testing.T) {
	events := []Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "hi"}},
		// two delta events for same response
		{Type: "llm.response", Seq: 2, Payload: map[string]any{"text": "Hello"}},
		{Type: "llm.response", Seq: 3, Payload: map[string]any{"text": ", world!"}},
	}
	s := Fold(events)
	if len(s.History) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(s.History))
	}
	if got := s.History[1].Text; got != "Hello, world!" {
		t.Errorf("expected streaming response 'Hello, world!', got %q", got)
	}
}

// TestFoldDeterministic verifies two identical runs produce the same state:
// the property that makes replay and golden testing meaningful.
func TestFoldDeterministic(t *testing.T) {
	events := []Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "test"}},
		{Type: "llm.response", Seq: 2, Payload: map[string]any{"text": "ok"}},
	}
	a := Fold(events)
	b := Fold(events)
	if a.ChatHistoryMarkdown() != b.ChatHistoryMarkdown() {
		t.Error("two folds of the same log produced different states: replay is worthless")
	}
}