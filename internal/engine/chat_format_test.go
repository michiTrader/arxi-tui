package engine

import (
	"os"
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

// A warning reads "! auth: ..."; a cancelled turn is a failed request and does not
// repeat the prompt, which is already the line above it.
func TestWarningAndCancelledLines(t *testing.T) {
	rows := plainRows(t, []fold.Event{
		{Type: "chat.warn", Seq: 1, Payload: map[string]any{"text": "auth: API key setup is unavailable in this WASM session."}},
		{Type: "run.prompt", Seq: 2, Payload: map[string]any{"text": "cual es la masa de un elefante"}},
		{Type: "chat.cancelled", Seq: 3},
	})
	if rows[0] != "! auth: API key setup is unavailable in this WASM session." {
		t.Errorf("warning row = %q", rows[0])
	}
	want := []string{"┃ cual es la masa de un elefante", "", "✗ request failed: Cancelled"}
	for i, w := range want {
		if rows[2+i] != w {
			t.Errorf("row %d = %q, want %q\nall rows:\n%s", 2+i, rows[2+i], w, strings.Join(rows, "\n"))
		}
	}
	if n := strings.Count(strings.Join(rows, "\n"), "elefante"); n != 1 {
		t.Errorf("the prompt appears %d times; the cancel line must not repeat it", n)
	}
}

func TestFormatDuration(t *testing.T) {
	for ms, want := range map[int64]string{0: "<1s", 400: "<1s", 500: "1s", 2100: "2s", 59400: "59s", 65000: "1m 05s", 600000: "10m 00s"} {
		if got := formatDuration(ms); got != want {
			t.Errorf("formatDuration(%d) = %q, want %q", ms, got, want)
		}
	}
}

// The warning, the usage line and a cancelled turn are drawn in their own tokens.
func TestNewChatLinesUseTheirOwnTokens(t *testing.T) {
	r, js := chatDoc(t)
	got := r.RenderFrame(mustDoc(t, js), fold.Fold([]fold.Event{
		{Type: "chat.warn", Seq: 1, Payload: map[string]any{"text": "w"}},
		{Type: "llm.response", Seq: 2, Payload: map[string]any{"text": "a", "duration_ms": float64(3000), "tokens_out": float64(5)}},
		{Type: "chat.cancelled", Seq: 3},
	})).Styled()
	for _, tok := range []string{"chat.warn", "chat.usage", "chat.error"} {
		if !strings.Contains(got, "«"+tok+":") {
			t.Errorf("no span drawn under %s:\n%s", tok, got)
		}
	}
}

// With the slash menu open, the hint line sits directly under the menu's bottom rule:
// no blank row between them.
func TestSlashMenuHintIsAdjacentToTheBottomRule(t *testing.T) {
	b, err := os.ReadFile("../../testdata/SOBRIA.json")
	if err != nil {
		t.Fatal(err)
	}
	st := fold.Fold(nil)
	st.SlashActive = true
	st.StatusActive = "false"
	st.SlashHint = "  ↑↓ navigate · tab category · enter open · esc close"
	r := Renderer{Width: 70, Height: 24}
	rows := strings.Split(strings.TrimRight(r.RenderFrame(mustDoc(t, string(b)), st).Plain(), "\n"), "\n")
	hint := -1
	for i, row := range rows {
		if strings.Contains(row, "navigate") {
			hint = i
		}
	}
	if hint < 1 {
		t.Fatalf("no hint row:\n%s", strings.Join(rows, "\n"))
	}
	if !strings.HasPrefix(rows[hint-1], "───") {
		t.Errorf("the row above the hint is %q, want the menu's bottom rule", rows[hint-1])
	}
}
