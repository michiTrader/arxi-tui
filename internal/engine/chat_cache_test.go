package engine

import (
	"reflect"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

// mixedChat has every kind of item the chat draws, including the ones that draw nothing.
func mixedChat() fold.State {
	return fold.State{History: []fold.ChatLine{
		{Role: "user", Text: "hello there, " + strings.Repeat("long words ", 12)},
		{Role: "user", Command: "/ui", Text: "make the border red"},
		{Role: "tool", Tool: "read", ToolArg: "a.go", ToolOK: true, ToolSummary: "Read 3 lines", ToolOutput: strings.Repeat("x\n", 9)},
		{Role: "assistant"}, // a turn that only called tools: no rows, no gap
		{Role: "tool", Tool: "edit", ToolArg: "b.go", ToolOK: true, ToolSummary: "Edited", ToolDiff: "@@ -1 +1 @@\n-a\n+b\n"},
		{Role: "approval", Tool: "run", ToolArg: "rm -rf x", Text: "run it?"},
		{Role: "assistant", Text: "## Title\n\n- one\n- two\n\n" + strings.Repeat("words ", 40), DurationMS: 2000, TokensIn: 2, TokensOut: 57},
		{Role: "warn", Text: "careful"},
		{Role: "note", Text: "a note"},
		{Role: "error", Text: "it failed"},
		{Role: "assistant", Text: "   ", DurationMS: 1500},
	}}
}

func chatRows(r *Renderer, st fold.State, budget int) []ui.Line {
	return r.renderChatPane(&scene.Node{Bind: "chat.history"}, st, budget).Live
}

// The cache may never change what is drawn: for every layout setting, a frame drawn through
// a cache that has already seen other frames equals the same frame drawn with none.
func TestChatCacheDrawsExactlyWhatNoCacheDraws(t *testing.T) {
	hits := 0
	st := mixedChat()
	looks := []*ChatLook{nil, {ToolDot: "* ", UserMarker: "> ", AssistantIndent: 4, TurnGap: 2}}
	for _, w := range []int{20, 40, 77, 120} {
		for _, ps := range []string{"", PromptBand, PromptPlain} {
			for _, ex := range []bool{false, true} {
				for li, lk := range looks {
					for _, sb := range []bool{false, true} {
						mk := func(c *ChatRowCache) *Renderer {
							return &Renderer{Width: w, PromptStyle: ps, ExpandTools: ex, Look: lk, Scrollbar: sb, ChatCache: c}
						}
						want := chatRows(mk(nil), st, 1000)
						cache := &ChatRowCache{}
						for round := 0; round < 2; round++ { // second round: all from the cache
							got := chatRows(mk(cache), st, 1000)
							if !reflect.DeepEqual(want, got) {
								t.Fatalf("round %d w=%d ps=%q expand=%v look=%d bar=%v: cached frame differs", round, w, ps, ex, li, sb)
							}
						}
						hits += cache.Hits
					}
				}
			}
		}
	}
	if hits == 0 {
		t.Fatal("the cache was never used")
	}
}

// A conversation that grows frame by frame draws the same with and without the cache.
func TestChatCacheGrowingConversationEqualsFreshLayout(t *testing.T) {
	cache := &ChatRowCache{}
	all := mixedChat().History
	for n := 1; n <= len(all); n++ {
		st := fold.State{History: all[:n]}
		want := chatRows(&Renderer{Width: 50}, st, 1000)
		got := chatRows(&Renderer{Width: 50, ChatCache: cache}, st, 1000)
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("after %d items the cached frame differs", n)
		}
	}
}

// The point of the cache: items already laid out are not laid out again.
func TestChatCacheSkipsLayoutOfItemsAlreadyDrawn(t *testing.T) {
	cache := &ChatRowCache{}
	st := workChat(5)
	n := len(st.History)
	draw := func(s fold.State) { chatRows(&Renderer{Width: 60, ChatCache: cache}, s, 20) }

	draw(st)
	if cache.Misses != n || cache.Hits != 0 {
		t.Fatalf("first frame: misses=%d hits=%d, want %d and 0", cache.Misses, cache.Hits, n)
	}
	draw(st)
	if cache.Misses != n || cache.Hits != n {
		t.Fatalf("same frame again: misses=%d hits=%d, want %d and %d", cache.Misses, cache.Hits, n, n)
	}
	// One more item (a new message): only it is laid out.
	more := fold.State{History: append(append([]fold.ChatLine{}, st.History...), fold.ChatLine{Role: "user", Text: "next"})}
	draw(more)
	if cache.Misses != n+1 {
		t.Fatalf("a new item should cost one layout: misses=%d, want %d", cache.Misses, n+1)
	}
	// The last answer growing (streaming) lays out that one item, not the rest.
	grown := fold.State{History: append([]fold.ChatLine{}, more.History...)}
	grown.History[n] = fold.ChatLine{Role: "user", Text: "next, and more"}
	before := cache.Misses
	draw(grown)
	if cache.Misses != before+1 {
		t.Fatalf("a changed item should cost one layout: misses=%d, want %d", cache.Misses, before+1)
	}
}

// Anything the layout depends on throws the rows away: width, prompt style, expanded tools,
// the look, the pane's style token.
func TestChatCacheInvalidatesWhenTheLayoutContextChanges(t *testing.T) {
	st := mixedChat()
	base := func() Renderer { return Renderer{Width: 60} }
	cases := map[string]func(r *Renderer){
		"width":  func(r *Renderer) { r.Width = 61 },
		"prompt": func(r *Renderer) { r.PromptStyle = PromptBand },
		"expand": func(r *Renderer) { r.ExpandTools = true },
		"look":   func(r *Renderer) { r.Look = &ChatLook{UserMarker: "» "} },
	}
	for name, change := range cases {
		cache := &ChatRowCache{}
		a := base()
		a.ChatCache = cache
		chatRows(&a, st, 1000)
		missesBefore := cache.Misses
		b := base()
		b.ChatCache = cache
		change(&b)
		chatRows(&b, st, 1000)
		if cache.Misses != missesBefore+len(st.History) {
			t.Errorf("%s changed but the rows were reused (misses %d -> %d)", name, missesBefore, cache.Misses)
		}
		// and the new frame is what a fresh layout gives
		c := base()
		change(&c)
		if !reflect.DeepEqual(chatRows(&c, st, 1000), chatRows(&b, st, 1000)) {
			t.Errorf("%s: the frame after the change is not a fresh layout", name)
		}
	}
	// The token comes from the node's style.
	cache := &ChatRowCache{}
	key := scene.StyleTokenKeys()[0]
	for _, style := range []map[string]string{nil, {key: "dim"}} {
		r := &Renderer{Width: 60, ChatCache: cache}
		r.renderChatPane(&scene.Node{Bind: "chat.history", Style: style}, st, 1000)
	}
	if cache.Hits != 0 {
		t.Errorf("a different style token reused rows: hits=%d", cache.Hits)
	}
}

// What the cache holds is bounded by the conversation drawn last: a /clear empties it, and
// the versions of a streaming item that are gone are not kept.
func TestChatCacheForgetsWhatIsNoLongerDrawn(t *testing.T) {
	cache := &ChatRowCache{}
	st := workChat(4)
	chatRows(&Renderer{Width: 60, ChatCache: cache}, st, 20)
	if cache.Len() != len(st.History) {
		t.Fatalf("holds %d items, want %d", cache.Len(), len(st.History))
	}
	chatRows(&Renderer{Width: 60, ChatCache: cache}, fold.State{}, 20)
	if cache.Len() != 0 {
		t.Fatalf("after a clear it still holds %d items", cache.Len())
	}
	var streaming []fold.ChatLine
	for i := 1; i <= 30; i++ {
		streaming = []fold.ChatLine{{Role: "assistant", Text: strings.Repeat("w ", i)}}
		chatRows(&Renderer{Width: 60, ChatCache: cache}, fold.State{History: streaming}, 20)
	}
	if cache.Len() != 1 {
		t.Fatalf("a streaming answer left %d versions behind, want 1", cache.Len())
	}
}

// Whoever receives the rows may edit them; the next frame must not see that.
func TestChatCacheRowsAreNotSharedWithTheCaller(t *testing.T) {
	cache := &ChatRowCache{}
	st := fold.State{History: []fold.ChatLine{{Role: "user", Text: "hello"}, {Role: "assistant", Text: "answer **bold**"}}}
	first := chatRows(&Renderer{Width: 40, ChatCache: cache}, st, 1000)
	// an independent deep copy (not cloneRows, which is what is under test)
	var want []ui.Line
	for _, l := range first {
		c := ui.Line{}
		if l == nil {
			c = nil
		}
		for _, sp := range l {
			c = append(c, sp)
		}
		want = append(want, c)
	}
	for _, l := range first {
		for i := range l {
			l[i].Text = "XXX"
		}
	}
	// from the miss
	second := chatRows(&Renderer{Width: 40, ChatCache: cache}, st, 1000) // a hit
	if !reflect.DeepEqual(second, want) {
		t.Fatal("editing the rows of a frame changed the next one")
	}
	for _, l := range second {
		for i := range l {
			l[i].Text = "YYY"
		}
	}
	third := chatRows(&Renderer{Width: 40, ChatCache: cache}, st, 1000)
	if !reflect.DeepEqual(third, want) {
		t.Fatal("editing rows that came from the cache changed the next frame")
	}
}

// The cache reaches a chat pane that sits inside other nodes.
func TestChatCacheReachesChildRenderers(t *testing.T) {
	r := Renderer{Width: 30, Height: 10, ChatCache: &ChatRowCache{}}
	c := r.child(20, 5)
	if c.ChatCache != r.ChatCache {
		t.Fatal("child renderer lost the chat cache")
	}
}
