package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// The chat pane scrolls: the mouse wheel raises the host's ChatScroll and the
// renderer must window the transcript that many lines up from the tail, clamp at
// the top, and report the ceiling back so the loop can pin its offset. Without
// this the pane only ever showed the tail and older turns were unreachable —
// the bug this test guards. Counterfactual: reverting renderMarkdown to the
// unconditional tail-clip ignores ChatScroll and fails every scrolled case.
func TestChatPaneScrollsAndClampsFromTheTail(t *testing.T) {
	// Six one-line turns. With a wide pane each renders on its own row, with one
	// blank row between turns: 6 messages + 5 blanks = 11 wrapped lines.
	var hist []fold.ChatLine
	for i := 0; i < 6; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		hist = append(hist, fold.ChatLine{Role: role, Text: "MSG" + string(rune('0'+i))})
	}
	state := fold.State{History: hist}
	node := &scene.Node{Bind: "chat.history"}

	const budget = 3
	r := Renderer{Width: 80}

	// Following the tail (ChatScroll 0): the newest turn is visible, the oldest
	// is not, and the reported ceiling is total(11) - budget(3) = 8.
	r.ChatScroll = 0
	f := r.renderMarkdown(node, state, budget)
	got := f.Plain()
	if !strings.Contains(got, "MSG5") || strings.Contains(got, "MSG0") {
		t.Fatalf("tail view should show the newest turn (MSG5) and not the oldest (MSG0); got:\n%s", got)
	}
	if r.ChatScrollMax != 8 {
		t.Fatalf("ChatScrollMax = %d, want 8 (11 lines - 3 budget); the loop clamps its wheel offset to this", r.ChatScrollMax)
	}

	// Scrolled to the top (ChatScroll past the ceiling): the oldest turn is now
	// visible and the newest is not, proving the window moved and clamped rather
	// than ignoring the offset.
	r.ChatScroll = 100
	f = r.renderMarkdown(node, state, budget)
	got = f.Plain()
	if !strings.Contains(got, "MSG0") || strings.Contains(got, "MSG5") {
		t.Fatalf("scrolled-to-top view should show the oldest turn (MSG0) and not the newest (MSG5); got:\n%s", got)
	}
	if r.ChatScrollMax != 8 {
		t.Fatalf("ChatScrollMax = %d after over-scroll, want 8 (still clamped to the real ceiling)", r.ChatScrollMax)
	}
}

// A chat shorter than the pane has nothing to scroll: ChatScrollMax must stay 0
// so the loop pins the offset to the tail, and the frame is unchanged from the
// no-scroll path (the guarantee that keeps every existing golden byte-for-byte).
func TestChatPaneThatFitsReportsNoScroll(t *testing.T) {
	state := fold.State{History: []fold.ChatLine{
		{Role: "user", Text: "only line"},
	}}
	node := &scene.Node{Bind: "chat.history"}
	r := Renderer{Width: 80, ChatScroll: 5}
	f := r.renderMarkdown(node, state, 10)
	if r.ChatScrollMax != 0 {
		t.Fatalf("ChatScrollMax = %d for a chat that fits, want 0; a non-zero ceiling would let the wheel scroll a pane with nothing above it", r.ChatScrollMax)
	}
	if got := f.Plain(); !strings.Contains(got, "only line") {
		t.Fatalf("the single turn must still render; got:\n%s", got)
	}
}
