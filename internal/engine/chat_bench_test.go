package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// workChat is a conversation of n turns shaped like a working session: a question, an answer in
// Markdown (heading, list, a fenced block, a long paragraph) and two tool calls.
func workChat(n int) fold.State {
	var h []fold.ChatLine
	for i := 0; i < n; i++ {
		h = append(h, fold.ChatLine{Role: "user", Text: fmt.Sprintf("question %d: please look at the parser and fix the failing case", i)})
		h = append(h, fold.ChatLine{Role: "tool", Tool: "read", ToolArg: fmt.Sprintf("parser%d.go", i), ToolOK: true, ToolSummary: "Read 12 lines", ToolOutput: strings.Repeat("line of output\n", 12)})
		h = append(h, fold.ChatLine{Role: "tool", Tool: "run", ToolArg: fmt.Sprintf("go test ./... -run T%d", i), ToolOK: true, ToolSummary: "Exit 0", ToolOutput: strings.Repeat("ok  \tpkg\t0.1s\n", 8)})
		h = append(h, fold.ChatLine{Role: "assistant", Text: "## Result " + fmt.Sprint(i) + "\n\n- the first point is that the parser skipped the empty case\n- the second is that tests did not cover it\n\n```go\nfunc f() int {\n\treturn 1\n}\n```\n\n" + strings.Repeat("A long paragraph that wraps over several rows of the terminal. ", 6)})
	}
	return fold.State{History: h}
}

func benchChat(b *testing.B, turns int) {
	st := workChat(turns)
	node := &scene.Node{Bind: "chat.history"}
	for i := 0; i < b.N; i++ {
		r := Renderer{Width: 100, Scrollbar: true}
		r.renderChatPane(node, st, 30)
	}
}

func benchChatCached(b *testing.B, turns int) {
	st := workChat(turns)
	node := &scene.Node{Bind: "chat.history"}
	cache := &ChatRowCache{}
	for i := 0; i < b.N; i++ {
		r := Renderer{Width: 100, Scrollbar: true, ChatCache: cache}
		r.renderChatPane(node, st, 30)
	}
}

func BenchmarkChatCached10(b *testing.B)  { benchChatCached(b, 10) }
func BenchmarkChatCached40(b *testing.B)  { benchChatCached(b, 40) }
func BenchmarkChatCached100(b *testing.B) { benchChatCached(b, 100) }

func BenchmarkChat10(b *testing.B)  { benchChat(b, 10) }
func BenchmarkChat40(b *testing.B)  { benchChat(b, 40) }
func BenchmarkChat100(b *testing.B) { benchChat(b, 100) }
