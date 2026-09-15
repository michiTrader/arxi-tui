// Package ui is the cell/Frame substrate: the span, line and frame types every
// renderer above them speaks, copied from arxi-sim (by copy, never import, as
// PLAN.md requires). Its load-bearing rule carries over with it: a Frame is
// mode-blind. Whatever turns state into a Frame — here, the scene engine —
// knows nothing about terminals, and the only code that touches a real terminal
// lives below this package, in internal/term.
package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Span is a run of text under one named style. The name is a theme key, not a
// colour: it is resolved at emit time. That indirection buys three things at
// once. Golden files stay readable, a theme can be swapped without re-rendering,
// and downgrading to a 16-colour terminal is purely an emit concern.
//
// Fill is a second key, and it exists because one span could otherwise carry only
// one style. A diff draws a coloured band across the whole row and a syntax
// highlighter draws a green comment on top of it, and those are two different
// decisions by two different pieces of code: the band belongs to the row, the
// green belongs to the token. Emit merges them — Style over Fill — so the token
// keeps its colour without punching a hole in the band. Text and Width ignore it,
// which is why a wash can never move a golden file.
type Span struct {
	Text  string
	Style string
	Fill  string
}

// Line is one terminal row. It must never be wider than the frame that holds it.
type Line []Span

// Text is the line with the styling forgotten.
func (l Line) Text() string {
	var b strings.Builder
	for _, s := range l {
		b.WriteString(s.Text)
	}
	return b.String()
}

// Width is the display width of the line, counting graphemes the way a terminal
// does rather than counting bytes or runes.
func (l Line) Width() int {
	w := 0
	for _, s := range l {
		w += ansi.StringWidth(s.Text)
	}
	return w
}

// TrimRight drops trailing spaces. It exists for the line that is nothing but a
// marker: a reasoning block that has opened and has no text yet draws "✻ ", and
// that blank is not decoration, it is a cell. Callers that mean their trailing
// spaces — a status bar filling a row with a background colour — simply do not call
// this.
func (l Line) TrimRight() Line {
	out := append(Line{}, l...)
	for len(out) > 0 {
		last := len(out) - 1
		t := strings.TrimRight(out[last].Text, " ")
		if t == out[last].Text {
			break
		}
		if t == "" {
			out = out[:last]
			continue
		}
		out[last].Text = t
		break
	}
	return out
}

// Cursor is where the caret sits, as a position inside Live.
type Cursor struct {
	Line   int
	Col    int
	Hidden bool
}

// Frame is one complete picture of the interface.
//
// Committed lines are finished: they belong to the terminal's scrollback and will
// never be drawn again. Live lines are the region we own and repaint every frame.
// The split is the whole design — the emitter can repaint in place only because the
// renderer declares, per frame, which lines it is done with.
//
// While a session is running the answer is none of them. Handing a row over means
// promising never to address it again, and a resize breaks that promise for us: the
// row re-wraps where we cannot reach it. So a frame with a height keeps its whole
// transcript live, and Committed is filled only by a frame with no height — the last
// one, or a fold — where there is no live region left to hold.
type Frame struct {
	Committed []Line
	Live      []Line
	Cursor    Cursor
	Width     int
	Height    int

	// NextVisualChange is the number of shared animation ticks after this frame at
	// which its appearance may next differ. Zero means the frame is stable. It is
	// scheduling metadata only: emitters and plain-text renderers ignore it.
	NextVisualChange int

	// Scroll is where this frame ended up looking. See Scroll.
	Scroll Scroll

	// Scrollback marks this frame as one whose window rides the tail of a transcript
	// whose history is being written to the terminal as the window leaves it. Only such
	// a frame may move the emitter's high-water mark: the other full-frame views — the
	// roster, the config editor, a consent — repaint the same screen with content that
	// is not the transcript, and neither their rows nor their scroll offsets say
	// anything about where history ends.
	Scrollback bool

	// Side is where the column beside the transcript ended up, in the coordinates of Live: X is
	// the column the prose stopped at, Y the row of Live the transcript window begins on, and W
	// and H the columns and rows the side column actually covers. It is the zero value when there
	// is no such column — a frame with no height, a surface that cannot hold one, a screen too
	// narrow to give the columns up, or a window that came out with no rows in it.
	//
	// It is reported because a pointer arrives as a row and a column of the terminal and nothing
	// else, and the only honest way to turn that into "the reader took hold of the scrollbar" is to
	// be told where the scrollbar was drawn. A caller working it out for itself would have to
	// recompute the wrap point and count the chrome above the transcript — the renderer's own
	// arithmetic kept in a second place, which is what TranscriptWidth and PromptRows exist to
	// prevent, and which would go wrong silently, a row at a time, the next time the chrome above
	// the transcript changed.
	Side Rect
}

// Scroll is the window the frame was drawn through, reported back so that a caller
// moving the view does not have to reproduce the renderer's arithmetic.
//
// Above is the offset the frame was actually drawn at, after clamping, and it is what
// a caller stores back into Viewport.ScrollTop. Asking for row 900 of a 200-row
// transcript is not an error — it is a page key near the end of the log — and the
// answer is the last window rather than a complaint, so the clamped truth has to come
// back out or the caller's idea of where it is drifts a page further away with every
// press. Below is how many rows are still underneath the window, which makes Below == 0
// the test for "the view is on the tail and following the conversation". Rows is the
// height of the window, which is what a page key moves by.
type Scroll struct {
	Above int
	Below int
	Rows  int
}

// Plain renders the frame as unstyled text, committed part first. This is what
// golden files compare, and what --instant prints.
func (f Frame) Plain() string {
	var b strings.Builder
	for _, l := range f.Committed {
		b.WriteString(l.Text())
		b.WriteByte('\n')
	}
	for i, l := range f.Live {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(l.Text())
	}
	return b.String()
}

// Overflow returns the lines that are wider than the frame. A renderer that
// overflows corrupts inline mode, because the terminal wraps a line we thought
// we owned and every position after it is off by one. Callers treat a non-empty
// result as a bug, not as something to clamp.
func (f Frame) Overflow() []int {
	var bad []int
	if f.Width <= 0 {
		return nil
	}
	for i, l := range f.Committed {
		if l.Width() > f.Width {
			bad = append(bad, i)
		}
	}
	for i, l := range f.Live {
		if l.Width() > f.Width {
			bad = append(bad, len(f.Committed)+i)
		}
	}
	return bad
}

// Rect is a widget's allotted area.
type Rect struct{ X, Y, W, H int }

// Viewport is what the renderer is told about the surface it draws on. It carries
// capabilities, never a mode: "can I own a fixed right-hand column" is a question
// render may ask, "am I on the alternate screen" is not.
type Viewport struct {
	Width  int
	Height int

	// SidePanels says a fixed column beside the transcript can be held, which scrollback
	// cannot: a row printed into history belongs to the terminal, and a column drawn down
	// the edge of one would be a column drawn again on every frame. It is a capability and
	// not a request, but on a surface with a height it is taken up unconditionally — see
	// TranscriptWidth, which reserves the columns whether or not anything is installed to
	// fill them, because a wrap point that appears and disappears is worse than a narrow one.
	SidePanels bool
	FixedTop   bool // a fixed top band can be held

	// Scrollback says the terminal's own history is where this transcript's older rows
	// live: the window is always its tail, and every row the window leaves behind is
	// committed to history as it goes. A surface that says so is one the reader scrolls
	// with the terminal's own gesture — a finger on Termux — so the renderer keeps no
	// view of its own to move. Scrolled is not the reader's here, it is the caller's,
	// and it asks for one exact row at the top of the window, unclamped past the tail
	// if that is what the caller needs, because only the caller knows why. ScrollTop
	// doubles as the floor the window's top may not retreat below: a screen that grows
	// would otherwise walk the top back over rows history already holds, and re-showing
	// a committed row is printing it twice.
	Scrollback bool

	// Scrolled and ScrollTop are where the human is looking, and they are a pair:
	// ScrollTop is read only when Scrolled is set. The zero value therefore means
	// "show the tail", which is what every caller that does not scroll wants — a fold,
	// a pipe, a golden file — and it is also the state a session spends most of its
	// life in, so following the conversation costs no bookkeeping at all.
	//
	// ScrollTop counts rows from the top of the transcript, not from the bottom, and
	// that direction is the whole reason a scrolled view holds still. A row arriving
	// while the human reads changes how far away the tail is, so an offset measured
	// from the bottom would have to be corrected on every event just to keep the same
	// text on screen. Measured from the top, nothing needs correcting: new rows pile up
	// below the window, the way they do in a pager.
	Scrolled  bool
	ScrollTop int
}
