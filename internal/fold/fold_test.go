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

// TestSessionTokensUsedDerivedFromBudgetAndCost verifies session.tokens_used
// computation per BINDS.md §4.1: budget_usd minus cumulative cost_usd, in
// microunits.
func TestSessionTokensUsedDerivedFromBudgetAndCost(t *testing.T) {
	events := []Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{
			"budget_usd": 10.0, // 10000 microunits
		}},
		{Type: "llm.response", Seq: 2, Payload: map[string]any{
			"text":       "first",
			"tokens_in":  100.0,
			"tokens_out": 50.0,
			"cost_usd":   0.003, // 3 microunits
		}},
		{Type: "llm.response", Seq: 3, Payload: map[string]any{
			"text":       "second",
			"tokens_in":  200.0,
			"tokens_out": 75.0,
			"cost_usd":   0.007, // 7 microunits
		}},
	}
	s := Fold(events)

	// Budget 10000 microunits - cost 10 microunits = 9990 remaining
	if s.SessionTokensUsed != 9990 {
		t.Errorf("session.tokens_used: got %d, want 9990", s.SessionTokensUsed)
	}
}

// TestTodosCountDerivedFromTodosList verifies todos.count tracks the length
// of agent.todos.
func TestTodosCountDerivedFromTodosList(t *testing.T) {
	events := []Event{
		{Type: "agent.blocked", Seq: 1, Payload: map[string]any{
			"task":       "waiting for approval",
			"blocked_on": "approval",
			"actor":      "backend",
		}},
		{Type: "agent.blocked", Seq: 2, Payload: map[string]any{
			"task":       "waiting for tool result",
			"blocked_on": "tool",
			"actor":      "frontend",
		}},
	}
	s := Fold(events)

	if s.TodosCount != 2 {
		t.Errorf("todos.count: got %d, want 2", s.TodosCount)
	}
	if len(s.Todos) != 2 {
		t.Errorf("len(agent.todos): got %d, want 2", len(s.Todos))
	}

	// Unblock one: count should drop to 1
	events = append(events, Event{Type: "agent.unblocked", Seq: 3, Payload: map[string]any{
		"actor":      "backend",
		"blocked_on": "approval",
	}})
	s = Fold(events)
	if s.TodosCount != 1 {
		t.Errorf("todos.count after unblock: got %d, want 1", s.TodosCount)
	}
}

// TestAgentBlockedSurfaceBinds verifies agent.blocked.* binds (BINDS.md §4.2)
// capture the most recent blocked event's fields.
func TestAgentBlockedSurfaceBinds(t *testing.T) {
	events := []Event{
		{Type: "agent.blocked", Seq: 1, Payload: map[string]any{
			"task":        "waiting for approval",
			"blocked_on":  "approval",
			"actor":       "backend",
			"blocked_ref": map[string]any{"inbox_id": "abc123"},
		}},
	}
	s := Fold(events)

	if s.BlockedOn != "approval" {
		t.Errorf("agent.blocked.blocked_on: got %q, want 'approval'", s.BlockedOn)
	}
	if s.BlockedActor != "backend" {
		t.Errorf("agent.blocked.actor: got %q, want 'backend'", s.BlockedActor)
	}
	if s.BlockedRef == nil {
		t.Fatal("agent.blocked.blocked_ref: got nil, want map")
	}
	if id, ok := s.BlockedRef["inbox_id"].(string); !ok || id != "abc123" {
		t.Errorf("agent.blocked.blocked_ref[inbox_id]: got %v, want 'abc123'", s.BlockedRef["inbox_id"])
	}

	// After unblock, the surface should clear
	events = append(events, Event{Type: "agent.unblocked", Seq: 2, Payload: map[string]any{
		"actor":      "backend",
		"blocked_on": "approval",
	}})
	s = Fold(events)
	if s.BlockedRef != nil {
		t.Errorf("agent.blocked.blocked_ref after unblock: got %v, want nil", s.BlockedRef)
	}
	if s.BlockedOn != "" {
		t.Errorf("agent.blocked.blocked_on after unblock: got %q, want empty", s.BlockedOn)
	}
}

// TestTeamMembersTracksAgentActivity verifies team.members array is populated
// from agent.activated / agent.turn_done / agent.failed events.
func TestTeamMembersTracksAgentActivity(t *testing.T) {
	events := []Event{
		{Type: "agent.activated", Seq: 1, Payload: map[string]any{
			"agent": "backend",
			"role":  "backend",
		}},
		{Type: "llm.response", Seq: 2, Payload: map[string]any{
			"agent":      "backend",
			"text":       "response",
			"tokens_in":  10.0,
			"tokens_out": 20.0,
			"cost_usd":   0.005,
		}},
		{Type: "agent.turn_done", Seq: 3, Payload: map[string]any{"agent": "backend"}},
	}
	s := Fold(events)

	if len(s.TeamMembers) != 1 {
		t.Fatalf("team.members: got %d members, want 1", len(s.TeamMembers))
	}
	m := s.TeamMembers[0]
	if m.ID != "backend" {
		t.Errorf("member.id: got %q, want 'backend'", m.ID)
	}
	if m.Role != "backend" {
		t.Errorf("member.role: got %q, want 'backend'", m.Role)
	}
	if m.State != "idle" {
		t.Errorf("member.state: got %q, want 'idle' (turn_done)", m.State)
	}
	if m.Busy {
		t.Error("member.busy: got true, want false after turn_done")
	}
	if m.Turns != 1 {
		t.Errorf("member.turns: got %d, want 1", m.Turns)
	}
	if m.SpentUSD != 0.005 {
		t.Errorf("member.spent_usd: got %f, want 0.005", m.SpentUSD)
	}
}

// TestUsageDeltaFormatsTokenCounts verifies usage.delta format per BINDS.md §4.1.
func TestUsageDeltaFormatsTokenCounts(t *testing.T) {
	events := []Event{
		{Type: "llm.response", Seq: 1, Payload: map[string]any{
			"tokens_in":  1250.0,
			"tokens_out": 500.0,
		}},
	}
	s := Fold(events)

	// shortNum formats >=1000 as K with one decimal: 1250/1000 = 1.25 → "1.2"
	want := "+i1.2 +o500"
	if s.UsageDelta != want {
		t.Errorf("usage.delta: got %q, want %q", s.UsageDelta, want)
	}
}

// TestQuiescentDiagnosisCaptured verifies run.quiescent.diagnosis bind.
func TestQuiescentDiagnosisCaptured(t *testing.T) {
	events := []Event{
		{Type: "run.quiescent", Seq: 1, Payload: map[string]any{
			"diagnosis": "stage X advances with manual approval",
		}},
	}
	s := Fold(events)

	if s.QuiescentDiag != "stage X advances with manual approval" {
		t.Errorf("run.quiescent.diagnosis: got %q", s.QuiescentDiag)
	}
}

// TestViewStateBindsDefaultCorrectly verifies ui.* defaults per BINDS.md §4.3.
func TestViewStateBindsDefaultCorrectly(t *testing.T) {
	s := Fold(nil) // empty log

	if s.UISurface != "chat" {
		t.Errorf("ui.surface default: got %q, want 'chat'", s.UISurface)
	}
	if s.UIFocus != "" {
		t.Errorf("ui.focus default: got %q, want empty (null)", s.UIFocus)
	}
	if s.UIMax != "" {
		t.Errorf("ui.max default: got %q, want empty (null)", s.UIMax)
	}
}
