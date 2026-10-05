package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

func toolEv(seq int64, name, arg string, ok bool, summary string) fold.Event {
	return fold.Event{Type: "chat.tool", Seq: seq, Payload: map[string]any{
		"name": name, "arg": arg, "ok": ok, "summary": summary}}
}

// Tool calls draw a dot and the call, then the result under an elbow; calls of one
// answer stack with no blank row, and the answer follows after a gap.
func TestToolCallsDrawWithDotAndElbow(t *testing.T) {
	rows := plainRows(t, []fold.Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "what is here"}},
		toolEv(2, "list", ".", true, "Listed 2 entries"),
		toolEv(3, "read", "main.go", true, "Read 12 lines"),
		{Type: "llm.response", Seq: 4, Payload: map[string]any{"text": "A Go program."}},
	})
	want := []string{
		"┃ what is here",
		"",
		"● List(.)",
		"  └ Listed 2 entries",
		"● Read(main.go)",
		"  └ Read 12 lines",
		"",
		"  A Go program.",
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

func TestFailedToolCallIsStyledAsFailure(t *testing.T) {
	r, js := chatDoc(t)
	got := r.RenderFrame(mustDoc(t, js), fold.Fold([]fold.Event{
		toolEv(1, "read", ".env", false, "looks like it holds secrets"),
	})).Styled()
	for _, tok := range []string{"«chat.tool.dot:", "«chat.tool:", "«chat.tool.fail:"} {
		if !strings.Contains(got, tok) {
			t.Errorf("styled frame lacks %s:\n%s", tok, got)
		}
	}
	if strings.Contains(got, "«chat.tool.result:└") {
		t.Errorf("a failure must not use the quiet result style:\n%s", got)
	}
}

func TestLongToolArgumentWrapsUnderItself(t *testing.T) {
	long := "internal/some/very/long/path/" + strings.Repeat("deep/", 10) + "file.go"
	rows := plainRows(t, []fold.Event{toolEv(1, "read", long, true, "Read 3 lines")})
	if len(rows) < 3 {
		t.Fatalf("expected a wrapped call:\n%s", strings.Join(rows, "\n"))
	}
	if !strings.HasPrefix(rows[0], "● Read(") {
		t.Errorf("first row = %q\n%s", rows[0], strings.Join(rows, "\n"))
	}
	if !strings.HasPrefix(rows[1], "  ") || strings.HasPrefix(rows[1], "●") {
		t.Errorf("continuation must hang under the call: %q", rows[1])
	}
	if !strings.Contains(strings.Join(rows, "\n"), "  └ Read 3 lines") {
		t.Errorf("result row missing:\n%s", strings.Join(rows, "\n"))
	}
}
