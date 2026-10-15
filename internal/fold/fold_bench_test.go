package fold

import (
	"fmt"
	"strings"
	"testing"
)

// workLog is the event log of n turns of a working session as the loop keeps it: a question,
// ONE merged thinking event (the loop merges the fragments, see addEvent in cmd/arxi-tui),
// two tool calls and an answer that arrives in 20 pieces. thinkingFrags > 1 keeps the
// fragments apart, which is the log the loop no longer builds.
func workLog(n, thinkingFrags int) []Event {
	var ev []Event
	seq := int64(0)
	add := func(t string, p map[string]any) {
		seq++
		ev = append(ev, Event{Type: t, Seq: seq, Payload: p})
	}
	for i := 0; i < n; i++ {
		add("run.prompt", map[string]any{"text": fmt.Sprintf("question %d: look at the parser", i)})
		for k := 0; k < thinkingFrags; k++ {
			add("chat.thinking", map[string]any{"text": "thinking about it "})
		}
		add("chat.tool", map[string]any{"name": "read", "arg": "parser.go", "ok": true, "summary": "Read 12 lines", "output": strings.Repeat("line\n", 12)})
		add("chat.tool", map[string]any{"name": "run", "arg": "go test", "ok": true, "summary": "Exit 0", "output": strings.Repeat("ok pkg\n", 8)})
		for k := 0; k < 20; k++ {
			add("llm.response", map[string]any{"text": "a piece of the answer, "})
		}
		add("chat.usage", map[string]any{"context_tokens": float64(1000 + i)})
	}
	return ev
}

func benchFold(b *testing.B, turns, frags int) {
	ev := workLog(turns, frags)
	b.ReportMetric(float64(len(ev)), "events")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Fold(ev)
	}
}

func BenchmarkFold10(b *testing.B)  { benchFold(b, 10, 1) }
func BenchmarkFold40(b *testing.B)  { benchFold(b, 40, 1) }
func BenchmarkFold100(b *testing.B) { benchFold(b, 100, 1) }

// The same with the thinking kept in 30 fragments a turn, to see what merging them saves.
func BenchmarkFoldUnmerged100(b *testing.B) { benchFold(b, 100, 30) }
