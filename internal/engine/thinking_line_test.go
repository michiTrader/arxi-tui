package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

func sobriaDoc(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../testdata/SOBRIA.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The Thinking line is on screen from the moment the turn starts, before the
// model has thought a word: its label alone keeps it alive, and no spinner row
// stands beside it any more.
func TestThinkingLineShowsItsLabelBeforeAnyThinking(t *testing.T) {
	st := fold.State{AgentWorking: true, StatusActive: "true", HostThinking: "• Thinking (2s) "}
	got := drawDoc(t, sobriaDoc(t), st)
	if !strings.Contains(got, "• Thinking (2s)") {
		t.Fatalf("no Thinking label while working:\n%s", got)
	}
	if strings.Contains(got, "working") {
		t.Fatalf("the old working row is back:\n%s", got)
	}
}

// Idle: nothing of the line draws, even with a label left in the state.
func TestThinkingLineIsAbsentWhenIdle(t *testing.T) {
	st := fold.State{StatusActive: "true", HostThinking: "• Thinking (2s) ", ThinkingText: "stale"}
	if got := drawDoc(t, sobriaDoc(t), st); strings.Contains(got, "Thinking") || strings.Contains(got, "stale") {
		t.Fatalf("the Thinking line drew while idle:\n%s", got)
	}
}

// The thinking scrolls sideways when it is wider than the room after the label,
// and the line reports itself as animating so the loop keeps ticking.
func TestThinkingScrollsAndReportsActivity(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(sobriaDoc(t)))
	if err != nil {
		t.Fatal(err)
	}
	text := "alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima mike november"
	st := fold.State{AgentWorking: true, StatusActive: "true", HostThinking: "• Thinking (1s) ", ThinkingText: text}

	r0 := Renderer{Width: 40, Height: 12, AnimTicks: map[string]int{"thinking": 0}}
	f0, active := r0.RenderFrameActive(doc, st)
	r5 := Renderer{Width: 40, Height: 12, AnimTicks: map[string]int{"thinking": 5}}
	f5, _ := r5.RenderFrameActive(doc, st)

	line := func(plain string) string {
		for _, l := range strings.Split(plain, "\n") {
			if strings.Contains(l, "Thinking") {
				return strings.TrimRight(l, " ")
			}
		}
		return ""
	}
	l0, l5 := line(f0.Plain()), line(f5.Plain())
	if !strings.HasPrefix(l0, "• Thinking (1s) alpha") {
		t.Fatalf("tick 0 should show the head of the thinking after the label, got %q", l0)
	}
	if !strings.HasPrefix(l5, "• Thinking (1s) ") || l5 == l0 {
		t.Fatalf("tick 5 did not move: %q vs %q", l5, l0)
	}
	if strings.Contains(l5, "alpha") || !strings.Contains(l5, "bravo") {
		t.Fatalf("tick 5 should have scrolled the words along, got %q", l5)
	}
	moving := false
	for _, a := range active {
		if a.NodeID == "thinking" {
			moving = true
		}
	}
	if !moving {
		t.Fatal("a scrolling Thinking line must report activity, or the ticker is never armed")
	}
}
