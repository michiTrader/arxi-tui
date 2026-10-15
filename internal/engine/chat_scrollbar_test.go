package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

func barHistory(n int) fold.State {
	var h []fold.ChatLine
	for i := 0; i < n; i++ {
		h = append(h, fold.ChatLine{Role: "assistant", Text: "row"})
	}
	return fold.State{History: h}
}

// Off by default: no column is held back and nothing is drawn, so no golden moves.
func TestChatScrollbarIsOffUnlessAsked(t *testing.T) {
	r := Renderer{Width: 40}
	f := r.renderChatPane(&scene.Node{Bind: "chat.history"}, barHistory(30), 10)
	if strings.ContainsAny(f.Plain(), "┃│") || r.ChatBar.Height != 0 {
		t.Fatalf("a bar showed without Scrollbar:\n%s", f.Plain())
	}
}

// With more transcript than window the last column is a track with a thumb; the thumb rides the
// bottom at the tail and the top at the oldest line, and the report names the same rows.
func TestChatScrollbarThumbFollowsTheOffset(t *testing.T) {
	st := barHistory(30)
	node := &scene.Node{Bind: "chat.history"}
	r := Renderer{Width: 40, Scrollbar: true}
	f := r.renderChatPane(node, st, 10)
	rows := strings.Split(f.Plain(), "\n")
	if len(rows) != 10 {
		t.Fatalf("window is %d rows, want 10", len(rows))
	}
	for i, l := range rows {
		if got := []rune(l); len(got) != 40 {
			t.Errorf("row %d is %d cells wide, want 40: %q", i, len(got), l)
		}
	}
	b := r.ChatBar
	if b.Height != 10 || b.Col != 39 || b.Max != r.ChatScrollMax || b.ThumbLen < 1 {
		t.Fatalf("bar report wrong: %+v (max %d)", b, r.ChatScrollMax)
	}
	if b.ThumbTop+b.ThumbLen != 10 {
		t.Errorf("at the tail the thumb must sit on the bottom: %+v", b)
	}
	for i, l := range rows {
		want := '│'
		if i >= b.ThumbTop && i < b.ThumbTop+b.ThumbLen {
			want = '┃'
		}
		if c := []rune(l)[39]; c != want {
			t.Errorf("row %d bar cell %q, want %q", i, c, want)
		}
	}
	r = Renderer{Width: 40, Scrollbar: true, ChatScroll: 1 << 20}
	r.renderChatPane(node, st, 10)
	if r.ChatBar.ThumbTop != 0 {
		t.Errorf("at the oldest line the thumb must sit on the top: %+v", r.ChatBar)
	}
}

// Nothing to scroll: no bar is reported, so a click there is not a grab.
func TestChatScrollbarAbsentWhenEverythingFits(t *testing.T) {
	r := Renderer{Width: 40, Scrollbar: true}
	f := r.renderChatPane(&scene.Node{Bind: "chat.history"}, barHistory(1), 10)
	if strings.ContainsAny(f.Plain(), "┃│") || r.ChatBar.Height != 0 {
		t.Fatalf("a bar showed with nothing to scroll: %+v\n%s", r.ChatBar, f.Plain())
	}
}

// A scene with rows above the chat: the bar's rows are reported in the frame's coordinates,
// not the pane's, or a click would grab the wrong cell.
func TestChatScrollbarTopIsInFrameCoordinates(t *testing.T) {
	grow := 1
	doc := &scene.Document{Root: &scene.Node{Type: "stack", Children: []*scene.Node{
		{Type: "text", Text: "BANNER"},
		{Type: "text", Text: "SECOND"},
		{Type: "markdown", Bind: "chat.history", Grow: &grow},
		{Type: "text", Text: "FOOT"},
	}}}
	r := Renderer{Width: 40, Height: 14, Scrollbar: true}
	f := r.RenderFrame(doc, barHistory(30))
	b := r.ChatBar
	if b.Height == 0 {
		t.Fatalf("no bar reported:\n%s", f.Plain())
	}
	rows := strings.Split(f.Plain(), "\n")
	if b.Top != 2 {
		t.Fatalf("bar Top = %d, want 2 (two fixed rows above the chat)", b.Top)
	}
	for i := 0; i < b.Height; i++ {
		if got := []rune(rows[b.Top+i])[b.Col]; got != '│' && got != '┃' {
			t.Errorf("frame row %d col %d is %q, not a bar cell", b.Top+i, b.Col, got)
		}
	}
	if strings.ContainsAny(rows[0], "┃│") || strings.ContainsAny(rows[len(rows)-1], "┃│") {
		t.Errorf("the bar leaked outside the chat pane:\n%s", f.Plain())
	}
}

// The renderer says at which offsets one of the reader's own messages is at the top of the
// window, so shift+up/shift+down can walk them. Going to a stop must put that message first.
func TestChatUserStopsPutTheReadersMessageAtTheTop(t *testing.T) {
	var h []fold.ChatLine
	for i := 0; i < 6; i++ {
		h = append(h, fold.ChatLine{Role: "user", Text: "ask " + string(rune('A'+i))},
			fold.ChatLine{Role: "assistant", Text: "a1\na2\na3\na4\na5\na6"})
	}
	node := &scene.Node{Bind: "chat.history"}
	r := Renderer{Width: 60}
	r.renderChatPane(node, fold.State{History: h}, 8)
	if len(r.ChatUserStops) == 0 {
		t.Fatal("a chat taller than its window reported no message stops")
	}
	for i := 1; i < len(r.ChatUserStops); i++ {
		if r.ChatUserStops[i] <= r.ChatUserStops[i-1] {
			t.Fatalf("stops must ascend (nearest the tail first): %v", r.ChatUserStops)
		}
	}
	// The j-th stop (nearest the tail first) is the j-th newest message of the reader's, and
	// at it that message is the FIRST row, exactly: not a pinned copy of it above some other
	// row (the renderer pins the owning prompt whenever the window starts inside a turn), and
	// not the row before or after it.
	if len(r.ChatUserStops) != 6 {
		t.Fatalf("six messages, stops %v", r.ChatUserStops)
	}
	for j, s := range r.ChatUserStops {
		rr := Renderer{Width: 60, ChatScroll: s}
		f := rr.renderChatPane(node, fold.State{History: h}, 8)
		want := "ask " + string(rune('A'+5-j))
		if first := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(f.Live[0].Text()), "┃")); first != want {
			t.Errorf("stop %d (offset %d): the top row is %q, want %q", j, s, first, want)
		}
		if under := f.Live[1].Text() + f.Live[2].Text(); !strings.Contains(under, "a1") {
			t.Errorf("stop %d (offset %d): under the message there is %q, want the start of its answer", j, s, under)
		}
	}
	// Nothing to scroll: no stops, so the keys have nothing to walk.
	r2 := Renderer{Width: 60}
	r2.renderChatPane(node, fold.State{History: h[:2]}, 20)
	if len(r2.ChatUserStops) != 0 {
		t.Errorf("a chat that fits reported stops %v", r2.ChatUserStops)
	}
}

// ---- the input window: a tall text is a window that follows the caret ----

const inputDoc = `{"root":{"type":"stack","children":[
  {"type":"input","bind":"user.input","prefix":"┃ ","placeholder":"type"}]}}`

const inputInBoxDoc = `{"root":{"type":"stack","children":[
  {"type":"box","border":"rounded","children":[
    {"type":"input","bind":"user.input","prefix":"┃ ","placeholder":"type"}]}]}}`

// longInputState is a text of n short lines, "L01".."Ln", with the caret at caretLine (0-based).
func longInputState(n, caretLine int) fold.State {
	var lines []string
	for i := 1; i <= n; i++ {
		lines = append(lines, "L"+string(rune('0'+i/10))+string(rune('0'+i%10)))
	}
	text := strings.Join(lines, "\n")
	caret := 0
	for i := 0; i < caretLine; i++ {
		caret += len(lines[i]) + 1
	}
	return fold.State{UserInput: text, UserInputCaret: caret}
}

func renderInputDoc(t *testing.T, docJSON string, r *Renderer, st fold.State) ([]string, int) {
	t.Helper()
	doc, err := scene.ParseDocument([]byte(docJSON))
	if err != nil {
		t.Fatal(err)
	}
	f := r.RenderFrame(doc, st)
	return strings.Split(f.Plain(), "\n"), f.Cursor.Line
}

// A text taller than the allowed rows is cut to a window that holds the caret row, with an
// arrow on the first/last row when there is more above/below.
func TestALongTypedLineScrollsWithTheCaret(t *testing.T) {
	r := &Renderer{Width: 30, Height: 30, InputMaxRows: 5}
	rows, cur := renderInputDoc(t, inputDoc, r, longInputState(20, 19))
	if len(rows) != 5 {
		t.Fatalf("the window is %d rows, want 5:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[4], "L20") || r.InputTop != 15 || cur != 4 {
		t.Errorf("caret at the end must show the tail: top=%d cursor=%d rows=%q", r.InputTop, cur, rows)
	}
	if !strings.HasPrefix(rows[0], "↑") || strings.Contains(rows[4], "↓") {
		t.Errorf("more above, nothing below: %q", rows)
	}
	r = &Renderer{Width: 30, Height: 30, InputMaxRows: 5}
	rows, cur = renderInputDoc(t, inputDoc, r, longInputState(20, 0))
	if !strings.Contains(rows[0], "L01") || r.InputTop != 0 || cur != 0 {
		t.Errorf("caret at the start must show the head: top=%d cursor=%d", r.InputTop, cur)
	}
	if strings.Contains(rows[0], "↑") || !strings.HasPrefix(rows[4], "↓") {
		t.Errorf("nothing above, more below: %q", rows)
	}
	r = &Renderer{Width: 30, Height: 30, InputMaxRows: 5}
	rows, _ = renderInputDoc(t, inputDoc, r, longInputState(20, 9))
	if !strings.HasPrefix(rows[0], "↑") || !strings.HasPrefix(rows[4], "↓") || r.InputTop > 9 || r.InputTop+4 < 9 {
		t.Errorf("a caret in the middle has both arrows and sits inside the window: top=%d %q", r.InputTop, rows)
	}
}

func TestAShortTypedLineIsDrawnWhole(t *testing.T) {
	r := &Renderer{Width: 30, Height: 30, InputMaxRows: 5}
	rows, _ := renderInputDoc(t, inputDoc, r, longInputState(5, 2))
	if len(rows) != 5 || strings.ContainsAny(strings.Join(rows, ""), "↑↓") || r.InputTop != 0 {
		t.Errorf("a text that fits must be whole, without arrows: %q", rows)
	}
	r = &Renderer{Width: 30, Height: 30, InputMaxRows: 5}
	rows, _ = renderInputDoc(t, inputDoc, r, longInputState(5, 4))
	if len(rows) != 5 || strings.ContainsAny(strings.Join(rows, ""), "↑↓") {
		t.Errorf("a text of exactly the allowed rows must be whole, without arrows: %q", rows)
	}
	r = &Renderer{Width: 30, Height: 30, InputMaxRows: 5}
	rows, _ = renderInputDoc(t, inputDoc, r, longInputState(6, 5))
	if len(rows) != 5 {
		t.Errorf("one row over the limit already makes a window: %d rows", len(rows))
	}
}

// The renderer starts from where the last frame left the window: it moves only when the caret
// leaves it, and then by the least that brings the caret back (one row at the edge).
func TestTheInputWindowMovesByOneRowAtItsEdges(t *testing.T) {
	st := longInputState(20, 9)
	r := &Renderer{Width: 30, Height: 30, InputMaxRows: 5, InputScroll: 7}
	renderInputDoc(t, inputDoc, r, st) // window 7..11 holds row 9
	if r.InputTop != 7 {
		t.Errorf("a caret inside the window must not move it: top=%d", r.InputTop)
	}
	r = &Renderer{Width: 30, Height: 30, InputMaxRows: 5, InputScroll: 9}
	renderInputDoc(t, inputDoc, r, longInputState(20, 8)) // caret one row above 9..13
	if r.InputTop != 8 {
		t.Errorf("caret one row above the window: the window moves one row, top=%d", r.InputTop)
	}
	r = &Renderer{Width: 30, Height: 30, InputMaxRows: 5, InputScroll: 8}
	renderInputDoc(t, inputDoc, r, longInputState(20, 8)) // caret on the window's first row
	if r.InputTop != 8 {
		t.Errorf("a caret on the first row of the window must not move it: top=%d", r.InputTop)
	}
	r = &Renderer{Width: 30, Height: 30, InputMaxRows: 5, InputScroll: 4}
	renderInputDoc(t, inputDoc, r, longInputState(20, 9)) // 4..8, caret one row below
	if r.InputTop != 5 {
		t.Errorf("caret one row below the window: the window moves one row, top=%d", r.InputTop)
	}
	r = &Renderer{Width: 30, Height: 30, InputMaxRows: 5, InputScroll: 99}
	renderInputDoc(t, inputDoc, r, longInputState(20, 19))
	if r.InputTop != 15 {
		t.Errorf("a stale window past the end is pulled back: top=%d", r.InputTop)
	}
	r = &Renderer{Width: 30, Height: 30, InputMaxRows: 5, InputScroll: -3}
	renderInputDoc(t, inputDoc, r, longInputState(20, 0))
	if r.InputTop != 0 {
		t.Errorf("a negative window is pulled to 0: top=%d", r.InputTop)
	}
}

// With no explicit limit the input may take a third of the screen, never fewer than 3 rows.
func TestTheInputDefaultLimitIsAThirdOfTheScreen(t *testing.T) {
	for _, c := range []struct{ h, want int }{{30, 10}, {9, 3}, {4, 3}} {
		r := &Renderer{Width: 30, Height: c.h}
		rows, _ := renderInputDoc(t, inputDoc, r, longInputState(40, 39))
		if len(rows) != c.want {
			t.Errorf("height %d: %d rows, want %d", c.h, len(rows), c.want)
		}
	}
}

// The input sits in a box in some scenes: the window state must reach the child renderer and the
// top must come back out.
func TestALongInputInsideABoxKeepsItsWindow(t *testing.T) {
	r := &Renderer{Width: 40, Height: 30, InputMaxRows: 5}
	renderInputDoc(t, inputInBoxDoc, r, longInputState(20, 19))
	if r.InputTop != 15 {
		t.Errorf("inside a box the window was not applied/reported: top=%d", r.InputTop)
	}
	r = &Renderer{Width: 40, Height: 30, InputMaxRows: 5, InputScroll: 12}
	renderInputDoc(t, inputInBoxDoc, r, longInputState(20, 13))
	if r.InputTop != 12 {
		t.Errorf("inside a box the host's scroll was lost: top=%d", r.InputTop)
	}
}

func TestHostPasteBindShowsTheHintOnlyWhileSet(t *testing.T) {
	const doc = `{"root":{"type":"stack","children":[{"type":"text","bind":"host.paste","when":"host.paste"}]}}`
	r := &Renderer{Width: 40, Height: 5}
	rows, _ := renderInputDoc(t, doc, r, fold.State{HostPaste: "paste again to expand"})
	if !strings.Contains(strings.Join(rows, ""), "paste again to expand") {
		t.Errorf("the bound hint was not drawn: %q", rows)
	}
	rows, _ = renderInputDoc(t, doc, r, fold.State{})
	if strings.TrimSpace(strings.Join(rows, "")) != "" {
		t.Errorf("an empty hint must draw nothing: %q", rows)
	}
}

// ---- copying: what a selection takes is what is on the screen ----

// With nothing to scroll the bar has no cell, and rows are not padded out to its column: a
// padded row is copied with its trailing spaces. The text keeps the width it was laid out in.
func TestNoBarMeansNoPaddingToCopy(t *testing.T) {
	st := barHistory(2)
	r := Renderer{Width: 40, Scrollbar: true}
	f := r.renderChatPane(&scene.Node{Bind: "chat.history"}, st, 10)
	if r.ChatBar.Height != 0 {
		t.Fatalf("two rows must not need a bar: %+v", r.ChatBar)
	}
	for i, l := range f.Live {
		if txt := l.Text(); txt != strings.TrimRight(txt, " ") {
			t.Errorf("row %d carries trailing spaces to be copied: %q", i, txt)
		}
	}
	// the same text, laid out the same, as with the bar column held back
	plain := Renderer{Width: 39}
	g := plain.renderChatPane(&scene.Node{Bind: "chat.history"}, st, 10)
	if len(f.Live) != len(g.Live) {
		t.Fatalf("rows differ: %d vs %d", len(f.Live), len(g.Live))
	}
	for i := range f.Live {
		if f.Live[i].Text() != g.Live[i].Text() {
			t.Errorf("row %d moved: %q vs %q", i, f.Live[i].Text(), g.Live[i].Text())
		}
	}
}

// When there IS a bar every row still reaches the bar's column, so the thumb sits in a straight
// line (this is what the padding is for, and the only place it stays).
func TestARealBarStillAlignsEveryRow(t *testing.T) {
	r := Renderer{Width: 40, Scrollbar: true}
	f := r.renderChatPane(&scene.Node{Bind: "chat.history"}, barHistory(30), 10)
	if r.ChatBar.Height == 0 {
		t.Fatal("30 rows must need a bar")
	}
	for i, l := range f.Live {
		if w := l.Width(); w != 40 {
			t.Errorf("row %d is %d wide, want 40", i, w)
		}
	}
}
