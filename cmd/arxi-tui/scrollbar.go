package main

import "github.com/michiTrader/arxi_tui/internal/engine"

// The scrollbar's mouse side. The renderer draws the bar and reports where it is (engine.ChatBar);
// these two functions turn a pointer position into what the chat should do about it.

// barGrab says whether a press at (col,row) is on the bar, and where in the thumb it took hold.
// A press on the thumb keeps the point it grabbed, so the thumb does not jump under the pointer;
// a press on the empty track grabs the middle of the thumb, which makes the thumb come to the
// pointer and the drag that follows carry on from there.
func barGrab(b engine.ChatBar, col, row int) (grab int, ok bool) {
	if b.Height <= 0 || col != b.Col || row < b.Top || row >= b.Top+b.Height {
		return 0, false
	}
	rel := row - b.Top
	if rel >= b.ThumbTop && rel < b.ThumbTop+b.ThumbLen {
		return rel - b.ThumbTop, true
	}
	return b.ThumbLen / 2, true
}

// barScroll is the chat offset for a thumb held at grab and dragged to row. The top of the bar
// is the oldest line (the largest offset) and the bottom is the latest (zero); a row outside the
// bar pins to the nearest end, so a drag that leaves the column still behaves.
func barScroll(b engine.ChatBar, row, grab int) int {
	span := b.Height - b.ThumbLen
	if span <= 0 || b.Max <= 0 {
		return 0
	}
	top := row - b.Top - grab
	if top < 0 {
		top = 0
	}
	if top > span {
		top = span
	}
	// Round to nearest so the thumb lands where it was drawn: the renderer places it with the
	// inverse of this.
	return b.Max - (top*b.Max+span/2)/span
}
