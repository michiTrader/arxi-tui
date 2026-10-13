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
	f := r.renderMarkdown(&scene.Node{Bind: "chat.history"}, barHistory(30), 10)
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
	f := r.renderMarkdown(node, st, 10)
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
	r.renderMarkdown(node, st, 10)
	if r.ChatBar.ThumbTop != 0 {
		t.Errorf("at the oldest line the thumb must sit on the top: %+v", r.ChatBar)
	}
}

// Nothing to scroll: no bar is reported, so a click there is not a grab.
func TestChatScrollbarAbsentWhenEverythingFits(t *testing.T) {
	r := Renderer{Width: 40, Scrollbar: true}
	f := r.renderMarkdown(&scene.Node{Bind: "chat.history"}, barHistory(1), 10)
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
