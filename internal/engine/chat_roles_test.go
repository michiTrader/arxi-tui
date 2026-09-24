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

// The marker is only half the differentiation; the user asked for their turns to
// read in white. A user turn's spans must carry the userTurnToken (which sobria
// maps to white) and an agent turn's must not, or the two voices are the same
// colour again. Counterfactual: wrapping the user turn under the pane's default
// token instead of userTurnToken leaves no chat.user span and fails the first
// check.
func TestChatColoursUserTurnsNotAgentTurns(t *testing.T) {
	state := fold.State{History: []fold.ChatLine{
		{Role: "user", Text: "what is the plan"},
		{Role: "assistant", Text: "here is the plan"},
	}}
	node := &scene.Node{Bind: "chat.history"}
	r := Renderer{Width: 80}

	styled := strings.Split(r.renderMarkdown(node, state, -1).Styled(), "\n")

	// Styled() interleaves «token:word» spans, so match on a distinctive word
	// rather than the whole phrase, which the span markers break up.
	var userLine, agentLine string
	for _, l := range styled {
		if strings.Contains(l, ":what»") {
			userLine = l
		}
		if strings.Contains(l, ":here»") {
			agentLine = l
		}
	}

	if !strings.Contains(userLine, "«"+userTurnToken+":") {
		t.Fatalf("user turn %q is not styled with the %q token; it will not read in the user's colour", userLine, userTurnToken)
	}
	if strings.Contains(agentLine, "«"+userTurnToken+":") {
		t.Fatalf("agent turn %q wears the user's token; the colour no longer separates the voices", agentLine)
	}
}
