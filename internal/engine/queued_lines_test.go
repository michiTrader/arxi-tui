package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// A line sent while the agent works is shown under the conversation, marked as waiting,
// in the order it will go; once it is sent it is an ordinary question.
func TestQueuedLinesAreDrawnUnderTheConversationInOrder(t *testing.T) {
	rows := plainRows(t, []fold.Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "first"}},
		{Type: "chat.queued", Seq: 2, Payload: map[string]any{"text": "second"}},
		{Type: "chat.queued", Seq: 3, Payload: map[string]any{"text": "third"}},
	})
	got := strings.Join(rows, "\n")
	for _, want := range []string{"┃ first", "┃ second  (queued)", "┃ third  (queued)"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Index(got, "second") > strings.Index(got, "third") {
		t.Errorf("queued lines out of order:\n%s", got)
	}
}

func TestASentQueuedLineStopsBeingMarkedAsWaiting(t *testing.T) {
	rows := plainRows(t, []fold.Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "first"}},
		{Type: "chat.queued", Seq: 2, Payload: map[string]any{"text": "second"}},
		{Type: "llm.response", Seq: 3, Payload: map[string]any{"text": "ok"}},
		{Type: "run.prompt", Seq: 4, Payload: map[string]any{"text": "second"}},
	})
	if got := strings.Join(rows, "\n"); strings.Contains(got, "(queued)") {
		t.Errorf("a line that was sent is still marked as waiting:\n%s", got)
	}
}
