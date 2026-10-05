package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

func chatDoc(t *testing.T) (*Renderer, string) {
	t.Helper()
	return &Renderer{Width: 60, Height: 14}, `{"root":{"type":"stack","children":[
	  {"id":"chat","type":"markdown","bind":"chat.history","grow":1}]}}`
}

func plainRows(t *testing.T, events []fold.Event) []string {
	t.Helper()
	r, js := chatDoc(t)
	got := r.RenderFrame(mustDoc(t, js), fold.Fold(events)).Plain()
	rows := strings.Split(strings.TrimRight(got, "\n"), "\n")
	for i := range rows {
		rows[i] = strings.TrimRight(rows[i], " ")
	}
	return rows
}

// An answer sits two columns in, opposite the user's "┃ " marker, and the usage
// line under it is on the same margin: the exact shape the user asked for.
func TestAnswerIsIndentedAndFollowedByItsUsageLine(t *testing.T) {
	rows := plainRows(t, []fold.Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "hoola"}},
		{Type: "llm.response", Seq: 2, Payload: map[string]any{
			"text": "Hola. ¿En qué te ayudo?", "tokens_in": float64(2), "tokens_out": float64(57), "duration_ms": float64(2100)}},
		{Type: "run.prompt", Seq: 3, Payload: map[string]any{"text": "Gracias"}},
	})
	want := []string{
		"┃ hoola",
		"",
		"  Hola. ¿En qué te ayudo?",
		"",
		"  2s (↑2 ↓57)",
		"",
		"┃ Gracias",
	}
	if len(rows) < len(want) {
		t.Fatalf("too few rows:\n%s", strings.Join(rows, "\n"))
	}
	for i, w := range want {
		if rows[i] != w {
			t.Errorf("row %d = %q, want %q\nall rows:\n%s", i, rows[i], w, strings.Join(rows, "\n"))
		}
	}
}

// A long answer wraps inside the margin: every row of it keeps the two columns.
func TestWrappedAnswerKeepsItsMarginOnEveryRow(t *testing.T) {
	long := strings.Repeat("word ", 30)
	rows := plainRows(t, []fold.Event{
		{Type: "llm.response", Seq: 1, Payload: map[string]any{"text": long}},
	})
	n := 0
	for _, row := range rows {
		if strings.TrimSpace(row) == "" {
			continue
		}
		n++
		if !strings.HasPrefix(row, "  w") {
			t.Errorf("a wrapped row lost the margin: %q", row)
		}
		if len([]rune(row)) > 60 {
			t.Errorf("a wrapped row overflows the frame: %q", row)
		}
	}
	if n < 2 {
		t.Fatalf("the answer did not wrap; the test proves nothing:\n%s", strings.Join(rows, "\n"))
	}
}

// No usage was reported, no usage line is drawn.
func TestNoUsageLineWithoutUsage(t *testing.T) {
	rows := plainRows(t, []fold.Event{
		{Type: "llm.response", Seq: 1, Payload: map[string]any{"text": "hi"}},
	})
	for _, row := range rows[1:] {
		if strings.TrimSpace(row) != "" {
			t.Errorf("an unexpected row under the answer: %q", row)
		}
	}
}

// "! auth: ..." for a warning, "■ Cancelled · <prompt>" for a stopped turn.
func TestWarningAndCancelledLines(t *testing.T) {
	rows := plainRows(t, []fold.Event{
		{Type: "chat.warn", Seq: 1, Payload: map[string]any{"text": "auth: API key setup is unavailable in this WASM session."}},
		{Type: "run.prompt", Seq: 2, Payload: map[string]any{"text": "What can fx do\ndifferently?"}},
		{Type: "chat.cancelled", Seq: 3, Payload: map[string]any{"text": "What can fx do\ndifferently?"}},
	})
	if rows[0] != "! auth: API key setup is unavailable in this WASM session." {
		t.Errorf("warning row = %q", rows[0])
	}
	var cancelled string
	for _, row := range rows {
		if strings.HasPrefix(row, "■") {
			cancelled = row
		}
	}
	if cancelled != "■ Cancelled · What can fx do differently?" {
		t.Errorf("cancelled row = %q\nall rows:\n%s", cancelled, strings.Join(rows, "\n"))
	}
}

func TestFormatDuration(t *testing.T) {
	for ms, want := range map[int64]string{0: "<1s", 400: "<1s", 500: "1s", 2100: "2s", 59400: "59s", 65000: "1m 05s", 600000: "10m 00s"} {
		if got := formatDuration(ms); got != want {
			t.Errorf("formatDuration(%d) = %q, want %q", ms, got, want)
		}
	}
}

// The three new lines are drawn in their own tokens.
func TestNewChatLinesUseTheirOwnTokens(t *testing.T) {
	r, js := chatDoc(t)
	got := r.RenderFrame(mustDoc(t, js), fold.Fold([]fold.Event{
		{Type: "chat.warn", Seq: 1, Payload: map[string]any{"text": "w"}},
		{Type: "llm.response", Seq: 2, Payload: map[string]any{"text": "a", "duration_ms": float64(3000), "tokens_out": float64(5)}},
		{Type: "chat.cancelled", Seq: 3, Payload: map[string]any{"text": "q"}},
	})).Styled()
	for _, tok := range []string{"chat.warn", "chat.usage", "chat.cancel"} {
		if !strings.Contains(got, "«"+tok+":") {
			t.Errorf("no span drawn under %s:\n%s", tok, got)
		}
	}
}
