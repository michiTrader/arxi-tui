package main

import "github.com/michiTrader/arxi_tui/internal/term"

// The phone arrangement. Termux is a terminal where four things only hold as a set, and
// each of them was learned in arxi_cli_sim before it was learned here:
//
//   - the mouse is not tracked. A tracked tap becomes a mouse report instead of raising
//     Android's soft keyboard, and no escape sequence asks the keyboard back afterwards, so
//     a reader who closed it could never type again;
//   - alternate scroll (?1007) is switched off, so a swipe on the alternate screen is not
//     rewritten into arrow keys nobody pressed;
//   - with the mouse gone, plain up and down scroll the chat (a swipe arrives as them), and
//     the input history moves to ctrl+p and ctrl+n, which historyStep already spells;
//   - Termux does not honour synchronized output (?2026), so a repaint tears. The caret is
//     hidden while the rows are painted and shown again only at the end, and only the rows
//     that changed are painted at all, so an animated border does not shimmer the screen.
//
// scrollArrows and flickerGuard are set once, before the first frame, from the terminal
// the session runs in; mouseOverride is the -mouse flag when it was typed.
var (
	mouseOverride *bool
	scrollArrows  bool
	flickerGuard  bool
)

// wantMouse is whether this session claims the mouse: an explicit -mouse wins either way,
// otherwise every terminal but Termux does.
func wantMouse(termux bool, override *bool) bool {
	if override != nil {
		return *override
	}
	return !termux
}

// phoneArrows is whether plain up and down scroll the chat instead of walking the input
// history. It is only the phone without a mouse: anywhere else a wheel notch is already a
// scroll and the arrows belong to the history.
func phoneArrows(termux, mouse bool) bool { return termux && !mouse }

// phoneScroll reads a key as a chat scroll on the phone arrangement: +1 for up (older),
// -1 for down, 0 for anything else. A key with a modifier is never one, so ctrl+p and
// ctrl+n keep walking the history and alt+arrows keep moving the caret.
func phoneScroll(k term.Key) int {
	if !scrollArrows || k.Mod != 0 {
		return 0
	}
	switch k.Type {
	case term.KeyUp:
		return 1
	case term.KeyDown:
		return -1
	}
	return 0
}

// phoneWheelStep is the notch width: a phone swipe arrives one row at a time, so one row
// per notch keeps the page under the finger. Anywhere else the wheel moves three.
func phoneWheelStep() int {
	if scrollArrows {
		return 1
	}
	return 3
}
