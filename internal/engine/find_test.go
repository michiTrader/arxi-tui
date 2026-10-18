package engine

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

const findDoc = `{"root":{"type":"stack","children":[
  {"id":"chat","type":"markdown","bind":"chat.history","grow":1,"fit":true},
  {"id":"prompt","type":"input","bind":"user.input","prefix":"┃ ",
   "suffix":{"type":"text","bind":"host.find","style":{"style":"find.bar"}}}]}}`

// findHistory is n answers, "answer 01".."answer n", each one row long.
func findHistory(n int) []fold.ChatLine {
	var h []fold.ChatLine
	for i := 1; i <= n; i++ {
		h = append(h, fold.ChatLine{Role: "assistant", Text: fmt.Sprintf("answer %02d", i)})
	}
	return h
}

// washedText is what wears the given wash token in a frame, pieces joined (the wrapper cuts a
// row into one span per word).
func washedText(f ui.Frame, token string) string {
	var out string
	for _, l := range f.Live {
		for _, s := range l {
			if s.Fill == token {
				out += s.Text
			}
		}
	}
	return out
}

func TestOccurrencesIsCaseBlindAndRuneExact(t *testing.T) {
	low := lowerRunes("Foo bar FOO")
	got := occurrences(low, lowerRunes("foo"))
	if !reflect.DeepEqual(got, [][2]int{{0, 3}, {8, 11}}) {
		t.Errorf("occurrences = %v", got)
	}
	got = occurrences(lowerRunes("Año año"), lowerRunes("AÑO"))
	if !reflect.DeepEqual(got, [][2]int{{0, 3}, {4, 7}}) {
		t.Errorf("accented occurrences = %v", got)
	}
	if occurrences(low, nil) != nil {
		t.Error("an empty word matches nothing")
	}
}

func TestFindStopsCentreEachRowAndClampAtTheEnds(t *testing.T) {
	got := findStops([]int{0, 50, 99}, 100, 10)
	if !reflect.DeepEqual(got, []int{90, 45, 0}) {
		t.Errorf("stops = %v, want [90 45 0]", got)
	}
	if got := findStops([]int{2}, 5, 10); !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("a chat that fits has nothing to scroll: %v", got)
	}
	if findStops(nil, 100, 10) != nil {
		t.Error("no hit, no stop")
	}
}

func TestClampFindKeepsTheIndexInsideTheHits(t *testing.T) {
	for _, c := range [][3]int{{0, 3, 0}, {2, 3, 2}, {3, 3, 2}, {5, 3, 2}, {-1, 3, 0}, {4, 0, 0}} {
		if got := clampFind(c[0], c[1]); got != c[2] {
			t.Errorf("clampFind(%d,%d) = %d, want %d", c[0], c[1], got, c[2])
		}
	}
}

func TestWashRowKeepsTheStyleAndWashesOnlyTheWord(t *testing.T) {
	l := ui.Line{{Text: "say ", Style: "text"}, {Text: "Hello world", Style: "markdown.strong", Fill: "chat.band"}}
	out := washRow(l, lowerRunes("lo w"), findMatchToken)
	if out.Text() != l.Text() {
		t.Fatalf("the wash changed the words: %q", out.Text())
	}
	var washed string
	for _, s := range out {
		if s.Fill == findMatchToken {
			washed += s.Text
			if s.Style != "markdown.strong" {
				t.Errorf("the word lost its style: %+v", s)
			}
		} else if s.Text == "Hel" && s.Fill != "chat.band" {
			t.Errorf("the band outside the hit was lost: %+v", s)
		}
	}
	if washed != "lo w" {
		t.Errorf("washed %q, want %q", washed, "lo w")
	}
	if l[1].Fill != "chat.band" || len(l) != 2 {
		t.Error("washRow touched the row it was given (it belongs to the chat cache)")
	}
}

func TestARowWithAnEscapeIsNotSearched(t *testing.T) {
	l := ui.Line{{Text: "a\x1b[31mred"}}
	if got := findMatchRows([]ui.Line{l}, "red"); got != nil {
		t.Errorf("a row with a terminal escape matched: %v", got)
	}
}

func TestARowWithAnEscapeIsNeverWashedEither(t *testing.T) {
	l := ui.Line{{Text: "a\x1b[31mred"}}
	if out := washRow(l, lowerRunes("red"), findMatchToken); len(out) != 1 || out[0].Fill != "" {
		t.Errorf("a row with a terminal escape was washed: %+v", out)
	}
}

func TestHighlightFindLeavesItsInputAlone(t *testing.T) {
	rows := []ui.Line{{{Text: "one foo", Style: "text"}}, {{Text: "two", Style: "text"}}}
	out := highlightFind(rows, []int{0}, "foo", 0)
	if rows[0][0].Fill != "" || len(rows[0]) != 1 {
		t.Errorf("the rows it was given were washed: %+v", rows[0])
	}
	if len(out[0]) < 2 {
		t.Errorf("the copy was not washed: %+v", out[0])
	}
}

// The frame, end to end: the renderer jumps to the hit, washes it, reports the stops.
func TestTheChatJumpsToTheHitAndWashesIt(t *testing.T) {
	doc := mustDoc(t, findDoc)
	st := fold.State{History: findHistory(60)} // no bar text: the word must be found in the chat
	r := &Renderer{Width: 40, Height: 12}
	tail := r.RenderFrame(doc, st).Plain()
	if strings.Contains(tail, "answer 07") || !strings.Contains(tail, "answer 60") {
		t.Fatalf("premise: the tail should show answer 60 and not 07:\n%s", tail)
	}
	r = &Renderer{Width: 40, Height: 12, ChatFind: "answer 07", ChatFindJump: true}
	f := r.RenderFrame(doc, st)
	if !strings.Contains(f.Plain(), "answer 07") {
		t.Fatalf("the chat did not jump to the hit:\n%s", f.Plain())
	}
	if len(r.ChatFindStops) != 1 || r.ChatScroll != r.ChatFindStops[0] || r.ChatScroll == 0 {
		t.Errorf("stops=%v scroll=%d", r.ChatFindStops, r.ChatScroll)
	}
	if got := washedText(f, findCurrentToken); got != "answer 07" {
		t.Errorf("washed %q, want %q", got, "answer 07")
	}
}

func TestWithoutJumpTheChatStaysWhereTheReaderLeftIt(t *testing.T) {
	doc := mustDoc(t, findDoc)
	st := fold.State{History: findHistory(60)}
	r := &Renderer{Width: 40, Height: 12, ChatFind: "answer 07", ChatScroll: 3}
	f := r.RenderFrame(doc, st)
	if r.ChatScroll != 3 || !strings.Contains(f.Plain(), "answer 5") {
		t.Errorf("the chat moved without being asked (scroll %d)", r.ChatScroll)
	}
	if len(r.ChatFindStops) != 1 {
		t.Errorf("the hit is still counted: %v", r.ChatFindStops)
	}
}

func TestBackWalksFromTheNewestHitToTheOldestAndStops(t *testing.T) {
	doc := mustDoc(t, findDoc)
	st := fold.State{History: findHistory(60)}
	want := []string{"answer 59", "answer 58", "answer 50"} // "answer 5" hits 50-59, newest first
	for i, back := range []int{0, 1, 9} {
		r := &Renderer{Width: 40, Height: 12, ChatFind: "answer 5", ChatFindBack: back, ChatFindJump: true}
		f := r.RenderFrame(doc, st)
		current := 0
		for _, l := range f.Live {
			isCur := false
			for _, sp := range l {
				if sp.Fill == findCurrentToken {
					isCur = true
				}
			}
			if isCur {
				current++
				if !strings.Contains(l.Text(), want[i]) {
					t.Errorf("back=%d: the current hit is %q, want %q", back, strings.TrimSpace(l.Text()), want[i])
				}
			}
		}
		if current != 1 {
			t.Errorf("back=%d: %d rows wear find.current, want exactly 1 (%q)\n%s", back, current, want[i], f.Plain())
		}
		if len(r.ChatFindStops) != 10 {
			t.Errorf("back=%d: %d hits, want 10", back, len(r.ChatFindStops))
		}
	}
	r := &Renderer{Width: 40, Height: 12, ChatFind: "answer 5", ChatFindBack: 99, ChatFindJump: true}
	r.RenderFrame(doc, st)
	if r.ChatFindBack != 9 {
		t.Errorf("past the oldest hit the index is %d, want it clamped to 9", r.ChatFindBack)
	}
}

func TestTheHitsAreWashedOnTheScrollbarPathToo(t *testing.T) {
	doc := mustDoc(t, findDoc)
	st := fold.State{History: findHistory(60)}
	r := &Renderer{Width: 40, Height: 12, Scrollbar: true, ChatFind: "answer 07", ChatFindJump: true}
	f := r.RenderFrame(doc, st)
	if !strings.Contains(f.Plain(), "answer 07") || r.ChatScroll == 0 || len(r.ChatFindStops) != 1 {
		t.Errorf("the jump is lost behind the scrollbar: scroll=%d stops=%v\n%s", r.ChatScroll, r.ChatFindStops, f.Plain())
	}
}

func TestNoFindLeavesTheChatRowsAlone(t *testing.T) {
	doc := mustDoc(t, findDoc)
	st := fold.State{History: findHistory(60)}
	a := (&Renderer{Width: 40, Height: 12}).RenderFrame(doc, st).Styled()
	b := (&Renderer{Width: 40, Height: 12, ChatFind: ""}).RenderFrame(doc, st).Styled()
	if a != b || strings.Contains(a, findMatchToken) {
		t.Error("an empty word must not change the frame")
	}
}

func TestTheWordIsFoundWhateverItsCase(t *testing.T) {
	doc := mustDoc(t, findDoc)
	st := fold.State{History: findHistory(60)}
	r := &Renderer{Width: 40, Height: 12, ChatFind: "ANSWER 07", ChatFindJump: true}
	f := r.RenderFrame(doc, st)
	if len(r.ChatFindStops) != 1 || !strings.Contains(f.Plain(), "answer 07") {
		t.Errorf("an upper-case word found %d rows:\n%s", len(r.ChatFindStops), f.Plain())
	}
	if got := washedText(f, findCurrentToken); got != "answer 07" {
		t.Errorf("an upper-case word found the row but washed %q", got)
	}
}

func TestTheChatCacheIsNotWashedByAFind(t *testing.T) {
	doc := mustDoc(t, findDoc)
	st := fold.State{History: findHistory(8)}
	cache := &ChatRowCache{}
	(&Renderer{Width: 40, Height: 20, ChatCache: cache, ChatFind: "answer"}).RenderFrame(doc, st)
	f := (&Renderer{Width: 40, Height: 20, ChatCache: cache}).RenderFrame(doc, st)
	if washedText(f, findMatchToken)+washedText(f, findCurrentToken) != "" {
		t.Fatal("a closed finder still shows a wash: the cached rows were written to")
	}
}

// --- the bar in the input row -------------------------------------------------------

func TestTheBarSitsAtTheRightEdgeOfTheInputRow(t *testing.T) {
	doc := mustDoc(t, findDoc)
	st := fold.State{HostFind: "find 2/5: foo "}
	f := (&Renderer{Width: 40, Height: 6}).RenderFrame(doc, st)
	last := strings.Split(f.Plain(), "\n")
	row := last[len(last)-1]
	if !strings.HasSuffix(row, "find 2/5: foo ") || ansiStringWidth(row) != 40 {
		t.Errorf("the bar is not at the right edge of a 40-wide row: %q", row)
	}
	if !strings.HasPrefix(row, "┃ ") {
		t.Errorf("the prefix is gone: %q", row)
	}
	var styled bool
	for _, l := range f.Live {
		for _, s := range l {
			if s.Text == "find 2/5: foo " && s.Style == "find.bar" {
				styled = true
			}
		}
	}
	if !styled {
		t.Error("the bar does not carry the find.bar token its scene declared")
	}
}

func TestNoBarWhenTheFinderIsClosed(t *testing.T) {
	doc := mustDoc(t, findDoc)
	a := (&Renderer{Width: 40, Height: 6}).RenderFrame(doc, fold.State{}).Plain()
	if strings.Contains(a, "find") {
		t.Errorf("a closed finder drew something: %q", a)
	}
}

func TestTheTypedLineWrapsInWhatTheBarLeaves(t *testing.T) {
	doc := mustDoc(t, findDoc)
	text := strings.Repeat("word ", 10)
	st := fold.State{UserInput: text, UserInputCaret: len([]rune(text)), HostFind: "find: "}
	f := (&Renderer{Width: 30, Height: 8}).RenderFrame(doc, st)
	var inRows []string
	for _, r := range strings.Split(f.Plain(), "\n") {
		if strings.HasPrefix(r, "┃") {
			inRows = append(inRows, r)
		}
	}
	if len(inRows) < 3 {
		t.Fatalf("the line should wrap in what the bar leaves:\n%s", f.Plain())
	}
	if !strings.HasSuffix(inRows[0], "find: ") {
		t.Errorf("the bar is on the first row only: %q", inRows[0])
	}
	for i, r := range inRows {
		if ansiStringWidth(r) > 30 {
			t.Errorf("row %d overflows: %q", i, r)
		}
		if i > 0 && strings.Contains(r, "find") {
			t.Errorf("the bar repeats on row %d", i)
		}
	}
	inRows[0] = strings.TrimSuffix(inRows[0], "find: ")
	joined := strings.ReplaceAll(strings.Join(inRows, " "), "┃", "")
	if got := strings.Join(strings.Fields(joined), " "); got != strings.TrimSpace(text) {
		t.Errorf("the bar swallowed part of the typed line: %q", got)
	}
}

func TestTheTextWrapsBesideTheBarNotUnderIt(t *testing.T) {
	doc := mustDoc(t, findDoc)
	// 30 letters fit beside a 6-cell bar (40 - 2 prefix - 6 = 32 room)
	st := fold.State{UserInput: strings.Repeat("x", 30), UserInputCaret: 30, HostFind: "find: "}
	n := 0
	for _, r := range strings.Split((&Renderer{Width: 40, Height: 8}).RenderFrame(doc, st).Plain(), "\n") {
		if strings.HasPrefix(r, "┃") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("premise: 30 letters fit one row beside the bar: %d rows", n)
	}
	// 36 fit the 38 columns of a bar-less row but not the 32 beside the bar: they must wrap
	st = fold.State{UserInput: strings.Repeat("x", 36), UserInputCaret: 36, HostFind: "find: "}
	n = 0
	for _, r := range strings.Split((&Renderer{Width: 40, Height: 8}).RenderFrame(doc, st).Plain(), "\n") {
		if strings.HasPrefix(r, "┃") {
			n++
		}
	}
	if n != 2 {
		t.Errorf("36 letters beside a bar should wrap onto 2 rows, got %d", n)
	}
}

func TestTheCaretMovesToTheBarWhileItIsTyped(t *testing.T) {
	doc := mustDoc(t, findDoc)
	st := fold.State{UserInput: "hello", UserInputCaret: 5, HostFind: "find: x "}
	f := (&Renderer{Width: 40, Height: 6}).RenderFrame(doc, st)
	if f.Cursor.Col == 39 {
		t.Fatal("premise: without SuffixCaret the caret stays in the message")
	}
	f = (&Renderer{Width: 40, Height: 6, SuffixCaret: true}).RenderFrame(doc, st)
	if f.Cursor.Col != 39 {
		t.Errorf("caret column %d, want 39 (the end of the bar)", f.Cursor.Col)
	}
	f = (&Renderer{Width: 40, Height: 6, SuffixCaret: true}).RenderFrame(doc, fold.State{UserInput: "hello", UserInputCaret: 5})
	if f.Cursor.Col != 2+5 {
		t.Errorf("no bar: caret column %d, want 7", f.Cursor.Col)
	}
}

func TestALongWordInTheBarKeepsItsEndAndHalfThePane(t *testing.T) {
	doc := mustDoc(t, findDoc)
	st := fold.State{HostFind: "find: " + strings.Repeat("a", 100) + "END "}
	f := (&Renderer{Width: 40, Height: 6}).RenderFrame(doc, st)
	rows := strings.Split(f.Plain(), "\n")
	row := rows[len(rows)-1]
	if ansiStringWidth(row) != 40 || !strings.HasSuffix(row, "END ") {
		t.Errorf("a long word must keep its tail (the part being typed) in a 40-wide row: %q", row)
	}
	if strings.Count(row, "a") > 20 {
		t.Errorf("the bar takes more than half the pane: %q", row)
	}
}

const findBoxDoc = `{"root":{"type":"stack","children":[
  {"type":"box","border":"rounded","children":[
    {"type":"input","bind":"user.input","prefix":"┃ ",
     "suffix":{"type":"text","bind":"host.find","style":{"style":"find.bar"}}}]}]}}`

// A prompt inside a box is drawn by a child renderer; the caret flag must travel with it.
func TestTheBarCaretSurvivesAPromptInsideABox(t *testing.T) {
	doc := mustDoc(t, findBoxDoc)
	st := fold.State{UserInput: "hi", UserInputCaret: 2, HostFind: "find: "}
	plain := (&Renderer{Width: 40, Height: 8}).RenderFrame(doc, st)
	bar := (&Renderer{Width: 40, Height: 8, SuffixCaret: true}).RenderFrame(doc, st)
	if bar.Cursor == plain.Cursor {
		t.Errorf("the caret did not move to the bar inside a box: %+v", bar.Cursor)
	}
	if !strings.Contains(bar.Plain(), "find: ") {
		t.Errorf("no bar inside the box:\n%s", bar.Plain())
	}
}

const findWhenDoc = `{"root":{"type":"stack","children":[
  {"type":"input","bind":"user.input","prefix":"┃ ",
   "suffix":{"type":"text","bind":"host.find","when":"agent.working"}}]}}`

// The suffix is a node like any other: its `when` decides whether it is there.
func TestTheBarHonoursItsOwnWhen(t *testing.T) {
	doc := mustDoc(t, findWhenDoc)
	off := (&Renderer{Width: 40, Height: 4}).RenderFrame(doc, fold.State{HostFind: "find: "}).Plain()
	if strings.Contains(off, "find") {
		t.Errorf("the bar drew although its `when` is false:\n%s", off)
	}
	on := (&Renderer{Width: 40, Height: 4}).RenderFrame(doc, fold.State{HostFind: "find: ", AgentWorking: true}).Plain()
	if !strings.Contains(on, "find: ") {
		t.Errorf("the bar did not draw although its `when` is true:\n%s", on)
	}
}
