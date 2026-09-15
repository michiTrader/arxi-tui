package driver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// TestReplayParsesLogEvents verifies that Replay correctly decodes an NDJSON
// log file produced by the arxi core into fold events the engine can consume.
//
// The arxi core writes kernel.Event objects (spec/events.md): each line is a
// JSON object with at least seq, type, payload. The fold only needs those three
// fields; Replay must extract them and ignore the metadata (id, ts, source,
// correlation_id, etc.) so arxi-tui does not depend on internal/kernel.
func TestReplayParsesLogEvents(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.ndjson")

	// Lines a real arxi core run would produce. The extra fields (id, ts,
	// source, depth) must be ignored by Replay — they are arxi's metadata
	// and arxi-tui's fold has no use for them.
	lines := []string{
		`{"seq":1,"id":"e1","ts":"2026-09-15T10:00:00Z","type":"run.prompt","source":"runtime","depth":0,"payload":{"text":"hola"}}`,
		`{"seq":2,"id":"e2","ts":"2026-09-15T10:00:01Z","type":"llm.response","source":"runtime","depth":1,"payload":{"text":"Hola! ¿En qué puedo ayudarte?"}}`,
		`{"seq":3,"id":"e3","ts":"2026-09-15T10:00:02Z","type":"run.prompt","source":"runtime","depth":0,"payload":{"text":"gracias"}}`,
		`{"seq":4,"id":"e4","ts":"2026-09-15T10:00:03Z","type":"llm.response","source":"runtime","depth":1,"payload":{"text":"De nada."}}`,
		``, // a blank line must be skipped, not an error
	}

	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(logPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	events, err := Replay(logPath)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}

	want := []fold.Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "hola"}},
		{Type: "llm.response", Seq: 2, Payload: map[string]any{"text": "Hola! ¿En qué puedo ayudarte?"}},
		{Type: "run.prompt", Seq: 3, Payload: map[string]any{"text": "gracias"}},
		{Type: "llm.response", Seq: 4, Payload: map[string]any{"text": "De nada."}},
	}

	if len(events) != len(want) {
		t.Fatalf("expected %d events, got %d: %+v", len(want), len(events), events)
	}

	for i, ev := range events {
		if ev.Type != want[i].Type || ev.Seq != want[i].Seq {
			t.Errorf("event %d: got Type=%q Seq=%d, want Type=%q Seq=%d",
				i, ev.Type, ev.Seq, want[i].Type, want[i].Seq)
		}
		// Check payload text field
		gotText, _ := ev.Payload["text"].(string)
		wantText, _ := want[i].Payload["text"].(string)
		if gotText != wantText {
			t.Errorf("event %d payload.text: got %q, want %q", i, gotText, wantText)
		}
	}
}

// TestReplayIgnoresExtraFields verifies that unknown JSON fields in the log
// do not cause errors. The arxi core's kernel.Event has fields the fold
// does not need; Replay must not break when they appear.
func TestReplayIgnoresExtraFields(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.ndjson")

	line := `{"seq":1,"type":"llm.response","id":"e1","ts":"2026-01-01T00:00:00Z","scope":"run:r1","source":"runtime","actor":"backend","correlation_id":"c1","caused_by":["e0"],"depth":1,"payload":{"text":"hello","cost_usd":0.01}}`
	if err := os.WriteFile(logPath, []byte(line+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	events, err := Replay(logPath)
	if err != nil {
		t.Fatalf("Replay should not fail on extra fields: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "llm.response" || events[0].Seq != 1 {
		t.Errorf("got %+v", events[0])
	}
}

// TestReplayEmptyFile verifies an empty log produces no events and no error.
func TestReplayEmptyFile(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.ndjson")

	if err := os.WriteFile(logPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	events, err := Replay(logPath)
	if err != nil {
		t.Fatalf("Replay of empty file: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

// TestReplayMissingFile verifies a missing log returns an error with context.
func TestReplayMissingFile(t *testing.T) {
	_, err := Replay("/nonexistent/path/events.ndjson")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

// TestReplayMatchesFoldDeterministic verifies that a Replay'd log folded by
// fold.Fold produces the same state as constructing the same events inline.
// This is the reproducibility property from LESSONS.md: two folds of the same
// log produce the same state, or the replay is worthless.
func TestReplayMatchesFoldDeterministic(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.ndjson")

	logContent := `{"seq":1,"type":"run.prompt","payload":{"text":"hola"}}
{"seq":2,"type":"llm.response","payload":{"text":"Hola!"}}
{"seq":3,"type":"run.prompt","payload":{"text":"gracias"}}
{"seq":4,"type":"llm.response","payload":{"text":"De nada."}}
`
	if err := os.WriteFile(logPath, []byte(logContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Fold from the replayed log
	replayed, err := Replay(logPath)
	if err != nil {
		t.Fatal(err)
	}
	stateA := fold.Fold(replayed)

	// Fold from the same events constructed inline
	inline := []fold.Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "hola"}},
		{Type: "llm.response", Seq: 2, Payload: map[string]any{"text": "Hola!"}},
		{Type: "run.prompt", Seq: 3, Payload: map[string]any{"text": "gracias"}},
		{Type: "llm.response", Seq: 4, Payload: map[string]any{"text": "De nada."}},
	}
	stateB := fold.Fold(inline)

	if stateA.ChatHistoryMarkdown() != stateB.ChatHistoryMarkdown() {
		t.Error("replay + fold produced different state from inline fold: " +
			"replay is worthless")
	}
}
