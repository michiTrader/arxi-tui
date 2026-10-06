package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// longChat is a question followed by an answer of 20 numbered rows, then a second
// question and a short answer.
func longChat() fold.State {
	var answer []string
	for i := 1; i <= 20; i++ {
		answer = append(answer, fmt.Sprintf("row%02d", i))
	}
	return fold.State{History: []fold.ChatLine{
		{Role: "user", Text: "FIRSTQ"},
		{Role: "assistant", Text: strings.Join(answer, "\n\n")},
		{Role: "user", Text: "SECONDQ"},
		{Role: "assistant", Text: "ANSWER2a\n\nANSWER2b\n\nANSWER2c\n\nANSWER2d\n\nANSWER2e\n\nANSWER2f"},
	}}
}

func pinWindow(t *testing.T, state fold.State, scroll, budget int) string {
	t.Helper()
	r := Renderer{Width: 60, ChatScroll: scroll}
	f := r.renderMarkdown(&scene.Node{Bind: "chat.history"}, state, budget)
	return f.Plain()
}

func TestScrolledChatPinsTheQuestionThatScrolledOff(t *testing.T) {
	got := pinWindow(t, longChat(), 14, 8)
	lines := strings.Split(got, "\n")
	if !strings.Contains(lines[0], "FIRSTQ") {
		t.Fatalf("first row should repeat the question being read; got:\n%s", got)
	}
	if len(lines) != 8 {
		t.Errorf("window is %d rows, want exactly the budget of 8:\n%s", len(lines), got)
	}
	if !strings.Contains(got, "row") {
		t.Errorf("pin should sit above part of the answer:\n%s", got)
	}
}

func TestFollowingTheTailPinsNothing(t *testing.T) {
	got := pinWindow(t, longChat(), 0, 8)
	if strings.Contains(got, "FIRSTQ") {
		t.Errorf("the tail view must not repeat an old question:\n%s", got)
	}
}

func TestAJumpOntoTheQuestionItselfPinsNothing(t *testing.T) {
	// Scrolled all the way to the top the question is the first row on screen
	// already; repeating it would print the same line twice in a row.
	got := pinWindow(t, longChat(), 1000, 8)
	if n := strings.Count(got, "FIRSTQ"); n != 1 {
		t.Errorf("question appears %d times, want 1:\n%s", n, got)
	}
}

func TestPinNeverDuplicatesARowStillOnScreen(t *testing.T) {
	// Sweep every offset: the question must never show twice in one window.
	state := longChat()
	for s := 0; s <= 60; s++ {
		got := pinWindow(t, state, s, 8)
		if n := strings.Count(got, "FIRSTQ"); n > 1 {
			t.Fatalf("scroll %d: question appears %d times:\n%s", s, n, got)
		}
		if n := strings.Count(got, "SECONDQ"); n > 1 {
			t.Fatalf("scroll %d: second question appears %d times:\n%s", s, n, got)
		}
	}
}

func TestPinNamesTheTurnTheReaderIsInside(t *testing.T) {
	state := longChat()
	// Find an offset whose window starts inside the second answer, below SECONDQ.
	for s := 0; s <= 60; s++ {
		got := pinWindow(t, state, s, 8)
		first := strings.Split(got, "\n")[0]
		if strings.Contains(first, "SECONDQ") && strings.Contains(got, "ANSWER2") {
			return
		}
	}
	t.Error("no scroll position pinned the second question over its own answer")
}

func TestPinIsCappedAndMarksTheCut(t *testing.T) {
	long := strings.Repeat("word ", 60) // wraps onto many rows at width 60
	state := fold.State{History: []fold.ChatLine{
		{Role: "user", Text: long},
		{Role: "assistant", Text: strings.Repeat("line\n\n", 30)},
	}}
	// Scroll so the whole question has left the window.
	got := pinWindow(t, state, 30, 10)
	lines := strings.Split(got, "\n")
	pinned := 0
	for _, l := range lines {
		if strings.Contains(l, "word") {
			pinned++
		}
	}
	if pinned == 0 || pinned > pinnedRowsMax {
		t.Fatalf("pinned %d rows of the question, want 1..%d:\n%s", pinned, pinnedRowsMax, got)
	}
	if !strings.Contains(got, "…") {
		t.Errorf("a cut question should say so:\n%s", got)
	}
}

func TestTinyWindowDoesNotPin(t *testing.T) {
	got := pinWindow(t, longChat(), 14, 3)
	if strings.Contains(got, "FIRSTQ") {
		t.Errorf("a 3-row window has no room for a header:\n%s", got)
	}
}
