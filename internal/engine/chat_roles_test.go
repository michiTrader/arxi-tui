package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// The chat pane must visibly distinguish the user's turns from the agent's, or
// the transcript reads as one voice and a reader cannot tell a question from an
// answer — the gap the user reported. A user turn carries the "❯ " marker; an
// agent turn does not. Counterfactual: dropping the role check (marking neither
// or both) fails one half of this test.
func TestChatMarksUserTurnsNotAgentTurns(t *testing.T) {
	state := fold.State{History: []fold.ChatLine{
		{Role: "user", Text: "what is the plan"},
		{Role: "assistant", Text: "here is the plan"},
	}}
	node := &scene.Node{Bind: "chat.history"}
	r := Renderer{Width: 80}

	lines := strings.Split(r.renderMarkdown(node, state, -1).Plain(), "\n")

	var userLine, agentLine string
	for _, l := range lines {
		if strings.Contains(l, "what is the plan") {
			userLine = l
		}
		if strings.Contains(l, "here is the plan") {
			agentLine = l
		}
	}

	if !strings.HasPrefix(strings.TrimRight(userLine, " "), userTurnMarker) {
		t.Fatalf("user turn %q lacks the %q marker; the reader cannot tell it from an agent reply", userLine, userTurnMarker)
	}
	if strings.Contains(agentLine, userTurnMarker) {
		t.Fatalf("agent turn %q wears the user marker; the marker no longer distinguishes the voices", agentLine)
	}
}
