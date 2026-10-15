package engine

import (
	"fmt"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

// ChatRowCache keeps the rows of the chat items already laid out, so a frame only lays out
// what is new. The host builds a fresh Renderer for every frame, so the cache is a pointer
// the host owns and hands to each one (Renderer.ChatCache); nil means no cache, and the
// frame is byte-identical either way.
//
// An item's rows depend on the item and on the layout context (width, prompt style, whether
// tools are expanded, the look, the pane's style token). The context is checked once per
// frame: if any part of it changed, everything is dropped. After each frame the items that
// were not asked for are dropped too, so the cache never holds more than the conversation
// on screen (a /clear or an item that kept growing while streamed leaves nothing behind).
type ChatRowCache struct {
	// Hits and Misses count items served from the cache and items laid out, since the cache
	// was made. They exist so a test can prove the layout was skipped, not guessed.
	Hits, Misses int

	ctx   chatCacheCtx
	items map[fold.ChatLine]chatCacheItem
	frame int
}

type chatCacheCtx struct {
	width       int
	promptStyle string
	expand      bool
	token       string
	look        string
}

type chatCacheItem struct {
	rows []ui.Line
	seen int
}

// begin opens a frame: it drops everything when the layout context is not the one the rows
// were made in.
func (c *ChatRowCache) begin(ctx chatCacheCtx) {
	if c.items == nil || c.ctx != ctx {
		c.items = map[fold.ChatLine]chatCacheItem{}
		c.ctx = ctx
	}
	c.frame++
}

// end closes the frame: items nobody asked for in it are forgotten.
func (c *ChatRowCache) end() {
	for k, v := range c.items {
		if v.seen != c.frame {
			delete(c.items, k)
		}
	}
}

// Len is how many items the cache holds.
func (c *ChatRowCache) Len() int {
	if c == nil {
		return 0
	}
	return len(c.items)
}

// cloneRows copies the rows (lines and spans), so a caller that edits what it was given can
// never change what the cache holds.
func cloneRows(rows []ui.Line) []ui.Line {
	if rows == nil {
		return nil
	}
	out := make([]ui.Line, len(rows))
	for i, l := range rows {
		if l != nil {
			out[i] = make(ui.Line, len(l))
			copy(out[i], l)
		}
	}
	return out
}

// lookKey is a text that changes when any part of the look does.
func lookKey(lk ChatLook) string { return fmt.Sprintf("%#v", lk) }

// cachedChatItemRows is chatItemRows through the cache, when there is one. The rows it
// returns belong to the cache: the caller must not edit them (renderMarkdown copies the
// window it hands out, the only rows that leave the pane).
func (r *Renderer) cachedChatItemRows(h fold.ChatLine, token string, lk ChatLook) []ui.Line {
	c := r.ChatCache
	if c == nil {
		return r.chatItemRows(h, token, lk)
	}
	if it, ok := c.items[h]; ok {
		c.Hits++
		it.seen = c.frame
		c.items[h] = it
		return it.rows
	}
	c.Misses++
	rows := r.chatItemRows(h, token, lk)
	c.items[h] = chatCacheItem{rows: rows, seen: c.frame}
	return rows
}
