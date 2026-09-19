package engine

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

// ansiStringWidth is a thin wrapper around ansi.StringWidth so the renderer
// does not import the ansi package everywhere it is used.
func ansiStringWidth(s string) int {
	return ansi.StringWidth(s)
}

// cutLine returns the columns [left, right) of a line, preserving spans and
// styles. It is used by compositeLine to slice the underlying content around
// an overlay.
func cutLine(l ui.Line, left, right int) ui.Line {
	if right <= left {
		return nil
	}
	var out ui.Line
	col := 0
	for _, s := range l {
		w := ansiStringWidth(s.Text)
		next := col + w
		if next > left && col < right {
			start := left - col
			if start < 0 {
				start = 0
			}
			end := right - col
			if end > w {
				end = w
			}
			out = append(out, ui.Span{
				Text:  ansi.Cut(s.Text, start, end),
				Style: s.Style,
				Fill:  s.Fill,
			})
		}
		col = next
		if col >= right {
			break
		}
	}
	return out
}

// compositeLine paints over onto base at column x, returning the merged line.
// Columns [0, x) come from base; [x, x+over.Width) from over; the rest from base.
func compositeLine(base, over ui.Line, x int) ui.Line {
	out := cutLine(base, 0, x)
	out = append(out, over...)
	return append(out, cutLine(base, x+over.Width(), base.Width())...)
}

// padLine pads or truncates a line to exactly width columns without welding
// the row into one style. Flattening the line into its first span (the previous
// approach) is how a dim menu row came out undimmed: the padding is chrome, and
// chrome must not restyle the content it fills around, nor borrow the style of
// the span it happens to follow — a background on that token would paint the
// whole pad. Truncation goes through cutLine so wide glyphs never split; the
// pad span carries the row's fill (the band convention is that the first span
// speaks for the row) and no style of its own.
func padLine(l ui.Line, width int) ui.Line {
	w := l.Width()
	if w == width || width <= 0 {
		return l
	}
	if w > width {
		return cutLine(l, 0, width)
	}
	pad := strings.Repeat(" ", width-w)
	if len(l) == 0 {
		return ui.Line{{Text: pad}}
	}
	return append(append(ui.Line{}, l...), ui.Span{Text: pad, Fill: l[0].Fill})
}

// truncateText truncates a string to at most width display columns, cutting at
// grapheme boundaries so wide characters never split.
func truncateText(s string, width int) string {
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "")
}

// Renderer turns a scene document into a frame of cells. The render phase is pure: it needs
// no I/O, no clock, and the fold never waits on it. A frame is a snapshot of what
// the scene said to look like, not what happened to it.
type Renderer struct {
	Width  int
	Height int
}

// RenderFrame renders a scene document into a frame, binding fold.State into
// the nodes' bind fields.
func (r *Renderer) RenderFrame(doc *scene.Document, state fold.State) ui.Frame {
	if doc == nil || doc.Root == nil {
		return ui.Frame{}
	}
	return r.renderNode(doc.Root, state, r.Height)
}

// renderNode lays one node out within a budget of rows. Every container passes
// its children budgets, never more than it owns; every leaf clips its content
// to what it was given.
func (r *Renderer) renderNode(n *scene.Node, state fold.State, budget int) ui.Frame {
	switch n.Type {
	case "stack":
		return r.renderStack(n, state, budget)
	case "row":
		return r.renderHorizontal(n, state, budget)
	case "box":
		return r.renderBox(n, state, budget)
	case "markdown":
		return r.renderMarkdown(n, state, budget)
	case "input":
		return r.renderInput(n, state)
	case "text":
		return r.renderText(n, state)
	case "rule":
		return r.renderRule(n, state)
	case "marquee":
		return r.renderMarquee(n, state, budget)
	case "overlay":
		return r.renderOverlay(n, state)
	case "list":
		return r.renderList(n, state, budget)
	case "spinner":
		return r.renderSpinner(n, state)
	default:
		return ui.Frame{
			Live: []ui.Line{
				{ui.Span{Text: "[[UNKNOWN NODE TYPE]]", Style: "error"}},
			},
			Width: r.Width,
		}
	}
}

// renderStack is the vertical layout, and it is where the contraction order is
// enforced. Children are measured first; every child with no `grow` is fixed
// and takes its natural height unconditionally — an input row, a status line,
// a banner never shrink because a scene asked for too much content (Q6: the
// elastic panes contract first, the input never). Only then is what remains
// divided among the growers in proportion to their `grow`, and a grower that
// produces less than its share still owns the whole share: the space is
// reserved, which is what pins the input to the bottom of the screen instead
// of letting it ride up under the transcript.
//
// Overlay children are excluded from space accounting: they float on top of
// the rendered content rather than flowing in the vertical axis (Q5 — the menu
// is an overlay, not a stack row, so the transcript never jumps).
func (r *Renderer) renderStack(n *scene.Node, state fold.State, budget int) ui.Frame {
	// First pass: render fixed children to measure their natural heights,
	// and compute how much space is available for grow children.
	type slot struct {
		node  *scene.Node
		grow  int
		frame ui.Frame
		line  int // Starting line index in the final frame (set in second pass)
	}
	type renderedOverlay struct {
		node  *scene.Node
		frame ui.Frame
	}
	children := n.Children
	slots := make([]slot, len(children))
	fixed := 0
	growSum := 0
	var overlays []renderedOverlay
	// A bottom overlay inserts its rows into the flow beneath the input, so
	// its height must come out of the budget before the growers divide the
	// rest. Reserving it here is what keeps the frame inside the terminal:
	// without it the frame grows past the last row, the terminal scrolls on
	// repaint, and every absolute position — the caret included — lands on a
	// row it does not belong to (the Q6 contraction order: elastic panes
	// contract first; the input, banner and footer never).
	bottomOverlayHeight := 0
	for i, c := range children {
		if c.Type == "overlay" {
			// Pre-render once: the same frame is measured now and placed
			// later, so an overlay whose content depends on state is never
			// rendered twice with two different answers.
			of := r.renderOverlay(c, state)
			if len(of.Live) == 0 {
				continue // hidden (`when` false) or empty: no rows, no reservation
			}
			overlays = append(overlays, renderedOverlay{node: c, frame: of})
			if c.Anchor == "" || c.Anchor == "bottom" {
				bottomOverlayHeight += len(of.Live)
			}
			continue
		}
		grow := 0
		if c.Grow != nil {
			grow = *c.Grow
		}
		slots[i] = slot{node: c, grow: grow}
		if grow > 0 {
			growSum += grow
		} else {
			f := r.renderNode(c, state, budget)
			slots[i].frame = f
			fixed += len(f.Live)
		}
	}
	remaining := budget - fixed - bottomOverlayHeight

	// Second pass: render grow children with their proportional share as budget.
	// Fixed children are appended as-is; grow children fill their share and
	// reserve it even when short (the elastic pane pins the input to the
	// bottom instead of letting it ride up under the transcript).
	// caret is the terminal cursor expressed as a row of this frame rather
	// than of the child that hosts it: every row already placed above the
	// input is added to the input's own line. The host is named by the node
	// type and not by the frame's Cursor because {0,0} is a legal caret
	// position and every other renderer leaves that zero value behind —
	// reading it as a caret would park the terminal inside a text node.
	caret := ui.Cursor{Hidden: true}

	live := make([]ui.Line, 0, budget)
	for i := range slots {
		s := &slots[i]
		s.line = len(live) // Record starting line for this slot
		if s.grow == 0 {
			if s.node != nil && s.node.Type == "input" {
				caret = ui.Cursor{Line: len(live) + s.frame.Cursor.Line, Col: s.frame.Cursor.Col}
			}
			live = append(live, s.frame.Live...)
			continue
		}
		share := 0
		if remaining > 0 && growSum > 0 {
			share = remaining * s.grow / growSum
		}
		f := r.renderNode(s.node, state, share)
		grown := f.Live
		// Grow children reserve their full share even when content is short.
		// This is what pins the input to the bottom of the screen instead
		// of letting it ride up under the transcript.
		if len(grown) > share {
			grown = grown[len(grown)-share:]
		}
		if s.node != nil && s.node.Type == "input" {
			caret = ui.Cursor{Line: len(live) + f.Cursor.Line, Col: f.Cursor.Col}
		}
		live = append(live, grown...)
		for len(grown) < share {
			live = append(live, ui.Line{})
			grown = append(grown, ui.Line{})
		}
	}

	// Trim to budget if we overflowed; do not pad — padding is the caller's
	// concern (RenderFrame pads to Height, renderBox pads its inner content).
	if len(live) > budget && budget >= 0 {
		live = live[:budget]
	}
	// A caret whose row was cut away has no cell to sit on, and reporting one
	// would send the terminal past the last row this frame painted.
	if !caret.Hidden && caret.Line >= len(live) {
		caret.Hidden = true
	}

	// Find the first input node to determine where bottom overlays insert.
	// Bottom overlays appear AFTER the input in the frame (below it on screen),
	// so the slash menu floats beneath the input bar.
	inputEndLine := len(live)
	for i := range slots {
		if slots[i].node != nil && slots[i].node.Type == "input" {
			// Input is always 1 line tall; overlay goes after it.
			inputEndLine = slots[i].line + len(slots[i].frame.Live)
			break
		}
	}

	// insertAfterInput advances with each bottom overlay placed, so two of
	// them keep their document order instead of stacking reversed.
	insertAfterInput := inputEndLine
	for _, ov := range overlays {
		f := ov.frame
		lines := f.Live
		anchor := ov.node.Anchor
		switch anchor {
		case "full":
			if len(lines) >= len(live) {
				live = lines
			}
		case "top-right", "top-left", "top":
			// Composite at the top, merging columns at the overlay's x offset.
			n := len(lines)
			if n > len(live) {
				n = len(live)
			}
			x := 0
			if anchor == "top-right" || anchor == "top" {
				x = r.Width - f.Width
				if x < 0 {
					x = 0
				}
			}
			for i := 0; i < n; i++ {
				live[i] = compositeLine(live[i], lines[i], x)
			}
		default:
			// "bottom" inserts AFTER the input (below it on screen), not over
			// the tail. The height these rows occupy was already reserved in
			// the first pass, so the frame does not grow past the terminal and
			// the caret's absolute row keeps meaning what it says.
			insertAt := insertAfterInput
			if insertAt > len(live) {
				insertAt = len(live)
			}
			// Insert overlay lines at insertAt, shifting any tail below it down.
			tail := append([]ui.Line{}, live[insertAt:]...)
			live = append(live[:insertAt], lines...)
			live = append(live, tail...)
			insertAfterInput += len(lines)
			// The caret stays in the input, which did not move, so no adjustment needed.
		}
	}

	return ui.Frame{Live: live, Width: r.Width, Height: len(live), Cursor: caret}
}

// renderHorizontal lays children out horizontally. Weight-based columns divide
// the available width in proportion to their weight values (Scene 3/10 layout).
// Each column renders at full budget height; the output height is the max of
// the children's natural heights.
func (r *Renderer) renderHorizontal(n *scene.Node, state fold.State, budget int) ui.Frame {
	children := n.Children
	if len(children) == 0 {
		return ui.Frame{Live: []ui.Line{ui.Line{}}, Width: r.Width, Height: 1}
	}

	// Drop children whose `when` is unsatisfied before measuring, so a row does
	// not reserve space (or render padding) for content the host has hidden.
	// This is what lets the status row swap the live line for the menu hint:
	// only the truthy set of children lays out.
	visible := make([]*scene.Node, 0, len(children))
	for _, c := range children {
		if c.When != "" && !evalWhen(c.When, state) {
			continue
		}
		visible = append(visible, c)
	}
	if len(visible) == 0 {
		return ui.Frame{Live: []ui.Line{ui.Line{}}, Width: r.Width, Height: 1}
	}
	children = visible

	totalWidth := r.Width
	if totalWidth <= 0 {
		totalWidth = 80
	}

	// Measure each child's natural width first.
	naturalWidths := make([]int, len(children))
	frames := make([]ui.Frame, len(children))
	for i, child := range children {
		// Render with full budget to get natural height; use a large width
		// to avoid truncation during measurement.
		mr := Renderer{Width: totalWidth, Height: r.Height}
		f := mr.renderNode(child, state, budget)
		frames[i] = f
		w := 0
		for _, l := range f.Live {
			if lw := l.Width(); lw > w {
				w = lw
			}
		}
		naturalWidths[i] = w
	}

	// Allocate column widths: children with explicit weight share the
	// available space proportionally; children without weight take their
	// natural width and do not consume extra space.
	weighted := false
	totalWeight := 0
	for _, child := range children {
		if child.Weight != nil {
			wt := *child.Weight
			totalWeight += wt
			weighted = true
		}
	}

	colWidths := make([]int, len(children))
	if weighted && totalWeight > 0 {
		// Weighted layout: distribute by weight.
		allocated := 0
		for i, child := range children {
			wt := 1
			if child.Weight != nil {
				wt = *child.Weight
			}
			colWidths[i] = totalWidth * wt / totalWeight
			allocated += colWidths[i]
		}
		if rem := totalWidth - allocated; rem > 0 {
			// Give the remainder to the first weighted child.
			for i, child := range children {
				if child.Weight != nil {
					colWidths[i] += rem
					break
				}
			}
		}
	} else {
		// Natural layout: each child takes its natural width; remaining
		// space is filled with blanks after the last child.
		copy(colWidths, naturalWidths)
	}

	// Re-render children that need a specific column width (weighted children
	// and children whose natural width exceeds the allocation).
	for i, child := range children {
		if weighted && child.Weight != nil {
			if colWidths[i] != totalWidth {
				subR := Renderer{Width: colWidths[i], Height: r.Height}
				frames[i] = subR.renderNode(child, state, budget)
			}
		}
	}

	// Find the max height across all children.
	maxHeight := 0
	for _, f := range frames {
		if len(f.Live) > maxHeight {
			maxHeight = len(f.Live)
		}
	}

	// Build output lines: for each row, composite the columns side by side.
	// Spans from each child are carried through with their own styles so a
	// row does not flatten into one token (the status row needs "live"
	// bright and the rest dim on the same line). Truncation respects glyph
	// boundaries via cutLine; padding is appended as a styleless span so it
	// never restyles the token it sits beside.
	out := make([]ui.Line, maxHeight)
	for i := range out {
		var line ui.Line
		for ci, f := range frames {
			col := colWidths[ci]
			if i < len(f.Live) {
				src := f.Live[i]
				taken := 0
				for _, s := range src {
					if taken >= col {
						break
					}
					room := col - taken
					sw := ansiStringWidth(s.Text)
					if sw <= room {
						line = append(line, s)
						taken += sw
					} else {
						// Span overflows the column: cut it to fit.
						cut := cutLine(ui.Line{s}, 0, room)
						line = append(line, cut...)
						taken += room
					}
				}
			}
			for line.Width() < col {
				line = append(line, ui.Span{Text: " "})
			}
		}
		// Pad to full width.
		for line.Width() < totalWidth {
			line = append(line, ui.Span{Text: " "})
		}
		out[i] = line
	}

	return ui.Frame{Live: out, Width: totalWidth, Height: len(out)}
}

// renderMarkdown renders a bound markdown pane, wrapped to the frame width.
// Wrapping goes through the ported Line/Span machinery, so a row can never end
// in bare air or overflow the frame no matter what the fold hands it.
func (r *Renderer) renderMarkdown(n *scene.Node, state fold.State, budget int) ui.Frame {
	var lines []ui.Line
	switch n.Bind {
	case "chat.history":
		for _, h := range state.History {
			lines = append(lines, ui.WrapText(h.Text, "text", r.Width, nil)...)
			lines = append(lines, ui.Line{}) // one blank row between turns
		}
		if len(lines) > 0 {
			lines = lines[:len(lines)-1]
		}
	case "thinking.text":
		lines = append(lines, ui.WrapText(state.ThinkingText, "text", r.Width, nil)...)
	default:
		lines = append(lines, ui.WrapText(n.Text, "text", r.Width, nil)...)
	}
	if budget >= 0 && len(lines) > budget {
		lines = lines[len(lines)-budget:]
	}
	return ui.Frame{Live: lines, Width: r.Width}
}

// renderInput renders the input row: one line, always, and the column the
// terminal caret belongs in.
//
// The placeholder is drawn only while the line is empty, and under its own
// token so a theme can dim it. It used to be concatenated in front of the
// typed buffer under the same token as the text, which is how "ask anything,
// or / for commands" came out welded to the first word the human typed: a hint
// is not part of the line, and the moment there is a line it is gone.
//
// The caret is reported and never drawn. A reversed cell standing in for a
// cursor is the single most obvious tell that a TUI is not a native prompt,
// and the position is the one thing the terminal cannot work out for itself.
func (r *Renderer) renderInput(n *scene.Node, state fold.State) ui.Frame {
	var cells []ui.Span

	// sobria: the input may carry a string prefix (e.g. "┃ ") rendered as
	// styled leading cells before the text. The caret starts after it, because
	// the prefix is chrome and the human types to the right of chrome.
	prefix := n.PrefixText()
	if prefix != "" {
		cells = append(cells, ui.Span{Text: prefix, Style: "input"})
	}
	col := ansiStringWidth(prefix)

	if n.Bind == "user.input" && state.UserInput != "" {
		cells = append(cells, ui.Span{Text: state.UserInput, Style: "input"})
		col += ansiStringWidth(state.UserInput)
	} else {
		cells = append(cells, ui.Span{Text: n.Placeholder, Style: "input.placeholder"})
	}

	return ui.Frame{
		Live:   []ui.Line{cells},
		Width:  r.Width,
		Height: 1,
		Cursor: ui.Cursor{Line: 0, Col: col},
	}
}

// renderText renders a static text line or a bound text value. The sobria
// status row uses text nodes with bind ("agent.mode", "model.name") so the
// host resolves the bind into a styled span.
func (r *Renderer) renderText(n *scene.Node, state fold.State) ui.Frame {
	style := styleName(n.Style)
	text := n.Text
	if n.Bind != "" {
		text = resolveBind(n.Bind, state)
	}
	return ui.Frame{
		Live:   []ui.Line{{ui.Span{Text: text, Style: style}}},
		Width:  r.Width,
		Height: 1,
	}
}

// renderRule draws a single full-width horizontal rule.
func (r *Renderer) renderRule(n *scene.Node, state fold.State) ui.Frame {
	width := r.Width
	if width <= 0 {
		width = 1
	}
	// The rule glyph: a row of box-drawing horizontal dashes, matching the
	// sobria aesthetic of rules instead of frames (PLAN.md §2).
	rule := strings.Repeat("─", width)
	return ui.Frame{
		Live:   []ui.Line{{ui.Span{Text: rule, Style: "rule"}}},
		Width:  r.Width,
		Height: 1,
	}
}

// borderStyleName is the token the frame glyphs of n are drawn under.
//
// A node may name one through the object border form that SCENES.md Scene 3
// signs — {"shape": "single", "style": "warn"} — and when it does, that is the
// reference ValidateTokens checked, so it is the one that has to reach the
// screen. Both drawing paths used to stamp the literal "border" and never ask,
// which meant the declared token was validated and then discarded: a typo was
// refused with an address while a correct value did nothing, and the field
// looked wired precisely because the refusal proved something was reading it.
//
// The bare string form ("border": "single") carries no token and keeps
// "border", which is not merely a default but a pinned one: MAXIMUM declares
// its borders that way and its styled golden fixes six spans under that name,
// so reading the style unconditionally would move a factory golden no feature
// asked to move and break invariant 1.
func borderStyleName(n *scene.Node) string {
	if name := n.BorderStyleName(); name != "" {
		return name
	}
	return "border"
}

// renderBox draws a bordered container with an optional title, then its
// children inside. The border style is configurable: "single" uses light
// box-drawing, "double" uses double box-drawing, "ascii" uses ASCII +/-/|
// (Scene 3 — the maximum dashboard, Scene 5 — config screen).
func (r *Renderer) renderBox(n *scene.Node, state fold.State, budget int) ui.Frame {
	if !n.HasBorder() {
		// A box with no border is just a stack.
		return r.renderStack(n, state, budget)
	}

	var tl, tr, bl, br, horiz, vert rune
	switch n.BorderShape() {
	case "double":
		tl, tr, bl, br = '╔', '╗', '╚', '╝'
		horiz, vert = '═', '║'
	case "ascii":
		tl, tr, bl, br = '+', '+', '+', '+'
		horiz, vert = '-', '|'
	default: // "single"
		tl, tr, bl, br = '┌', '┐', '└', '┘'
		horiz, vert = '─', '│'
	}

	frameStyle := borderStyleName(n)

	width := r.Width
	if width <= 0 {
		width = 80
	}

	// The box frame takes 2 columns on each side for the border.
	innerWidth := width - 2
	if innerWidth < 0 {
		innerWidth = 0
	}
	// Content budget: the top and bottom borders consume 2 rows; if the box
	// has a title, the top border is replaced by a title row (still 1 row).
	innerHeight := budget - 2
	if innerHeight < 0 {
		innerHeight = 0
	}

	var lines []ui.Line

	// Top border, with title if present.
	topText := string(tl) + strings.Repeat(string(horiz), innerWidth) + string(tr)
	if n.Title != "" {
		// Title replaces the left part of the top border.
		// Layout: ┌ title ────────
		titleText := n.Title
		if innerWidth > len(titleText)+4 {
			topText = string(tl) + " " + titleText + " " +
				strings.Repeat(string(horiz), innerWidth-len(titleText)-2) + string(tr)
		}
	}
	lines = append(lines, ui.Line{
		ui.Span{Text: topText, Style: frameStyle},
	})

	// Inner content: render children as a stack clipped to innerHeight.
	// The box does not pad beyond its children's natural height — a short
	// content block renders top border + content + bottom border, and the
	// surrounding stack provides any vertical spacing (Q6: fixed children
	// take only what they need).
	innerRenderer := Renderer{Width: innerWidth, Height: innerHeight}
	var content ui.Frame
	if len(n.Children) > 0 {
		content = innerRenderer.renderStack(&scene.Node{
			Type:     "stack",
			Children: n.Children,
		}, state, innerHeight)
	} else {
		content = ui.Frame{Live: []ui.Line{ui.Line{}}, Width: innerWidth}
	}

	for _, l := range content.Live {
		text := l.Text()
		if w := ansiStringWidth(text); w < innerWidth {
			text += strings.Repeat(" ", innerWidth-w)
		} else if w > innerWidth {
			text = truncateText(text, innerWidth)
		}
		lines = append(lines, ui.Line{
			ui.Span{Text: string(vert), Style: frameStyle},
			ui.Span{Text: text, Style: styleName(n.Style)},
			ui.Span{Text: string(vert), Style: frameStyle},
		})
	}
	// Clip content to innerHeight if it overflowed.
	for len(lines) > 1+innerHeight {
		lines = lines[:1+innerHeight]
	}

	// Bottom border.
	bottomText := string(bl) + strings.Repeat(string(horiz), innerWidth) + string(br)
	lines = append(lines, ui.Line{
		ui.Span{Text: bottomText, Style: frameStyle},
	})

	// The inner stack answered in its own coordinates; a row of the box is one
	// below the content (the top border) and a column is one right of it (the
	// left border), so the caret has to move with them or it lands on the
	// frame's own glyphs.
	caret := ui.Cursor{Hidden: true}
	if !content.Cursor.Hidden && content.Cursor.Line+1 < len(lines)-1 {
		caret = ui.Cursor{Line: content.Cursor.Line + 1, Col: content.Cursor.Col + 1}
	}

	return ui.Frame{Live: lines, Width: width, Height: len(lines), Cursor: caret}
}

// renderSpinner renders a spinner node, which shows an active indicator glyph
// when its bind is truthy and a dim placeholder when it is not. Used by Scene 9
// for subagent state indicators (thinking/waiting/idle).
func (r *Renderer) renderSpinner(n *scene.Node, state fold.State) ui.Frame {
	active := false
	if n.Bind != "" {
		active = evalWhen(n.Bind, state)
	}
	var glyph string
	if active {
		glyph = "⠋"
	} else {
		glyph = "·"
	}
	style := styleName(n.Style)
	if style == "" {
		style = "dim"
	}
	return ui.Frame{
		Live:   []ui.Line{{ui.Span{Text: glyph, Style: style}}},
		Width:  r.Width,
		Height: 1,
	}
}

// renderMarquee renders a scrolling text line. The bind is thinking.text,
// which streams as partial text; the marquee scrolls only its bound text (Q1).
// Prefix and suffix are rendered as styled leading/trailing cells; the suffix
// may itself carry a bind (usage.delta).
func (r *Renderer) renderMarquee(n *scene.Node, state fold.State, budget int) ui.Frame {
	// Resolve the bind value.
	var text string
	switch n.Bind {
	case "thinking.text":
		text = state.ThinkingText
	case "":
		text = n.Text
	default:
		text = n.Text
	}

	// The marquee collapses to nothing when there is no text to show,
	// matching the empty-state contract for thinking.text.
	if text == "" {
		return ui.Frame{Width: r.Width, Height: 0}
	}

	var cells []ui.Span

	// Prefix: a child node with text+style (or bind+style) rendered before the
	// main scrolling text.
	prefix := n.PrefixNode()
	if prefix != nil {
		// A prefix is a text-bearing node: either Type=="text" or an
		// untyped node with Text set (the sobria prefix omits the type).
		if prefix.Bind != "" {
			cells = append(cells, ui.Span{Text: resolveBind(prefix.Bind, state), Style: styleName(prefix.Style)})
		} else {
			cells = append(cells, ui.Span{Text: prefix.Text, Style: styleName(prefix.Style)})
		}
	}

	// If the text is shorter than the width, show it whole; otherwise scroll.
	// Phase 0: static display (no animation) — the host clock animation comes later.
	cells = append(cells, ui.Span{Text: text, Style: styleName(n.Style)})

	// Suffix: a child node (bind or text) rendered after the main text.
	// The sobria marquee's suffix binds usage.delta with style "dim".
	if n.Suffix != nil {
		if n.Suffix.Bind != "" {
			cells = append(cells, ui.Span{Text: resolveBind(n.Suffix.Bind, state), Style: styleName(n.Suffix.Style)})
		} else if n.Suffix.Text != "" {
			cells = append(cells, ui.Span{Text: n.Suffix.Text, Style: styleName(n.Suffix.Style)})
		}
	}

	return ui.Frame{
		Live:   []ui.Line{cells},
		Width:  r.Width,
		Height: 1,
	}
}

// renderOverlay renders a floating panel anchored to a corner/edge. The
// overlay is only visible when its `when` bind is satisfied (Q5). The layout
// in renderStack handles positioning: overlays float above content and replace
// the lines they cover.
//
// Anchors (SCENES.md Q5): "bottom" pins to the lower edge (sobria menu),
// "top-right" to the upper right corner (Scene 3 token panel), "full" spans
// the whole screen. The anchor is metadata for the stack's positioning pass;
// renderOverlay returns content lines that the stack anchors into place.
func (r *Renderer) renderOverlay(n *scene.Node, state fold.State) ui.Frame {
	// An overlay with `when` only renders when that bind is truthy.
	if n.When != "" {
		if !evalWhen(n.When, state) {
			return ui.Frame{Width: r.Width, Height: 0}
		}
	}

	// Resolve the content width: use min_width if set and smaller than the frame,
	// otherwise use the full frame width.
	contentWidth := r.Width
	if n.MinWidth != nil && *n.MinWidth < contentWidth {
		contentWidth = *n.MinWidth
	}

	// Layout the overlay's children as a vertical column within the content width.
	innerRenderer := Renderer{Width: contentWidth, Height: r.Height}
	var lines []ui.Line
	for _, child := range n.Children {
		f := innerRenderer.renderNode(child, state, r.Height)
		for _, l := range f.Live {
			lines = append(lines, l)
		}
	}

	if len(lines) == 0 {
		return ui.Frame{Width: r.Width, Height: 0}
	}

	// If the overlay has a border, wrap the content in one. The border adds
	// 2 columns (one per side), so the total width becomes contentWidth + 2.
	totalWidth := contentWidth
	if n.HasBorder() {
		lines = r.wrapWithBorder(lines, n, contentWidth)
		totalWidth = contentWidth + 2
	}

	// Pad or truncate each line to the total width, span by span: the pad is
	// chrome and must not restyle the row it fills (see padLine).
	out := make([]ui.Line, len(lines))
	for i, l := range lines {
		out[i] = padLine(l, totalWidth)
	}

	return ui.Frame{Live: out, Width: totalWidth, Height: len(out)}
}

// wrapWithBorder wraps overlay content lines in a border, returning the full
// bordered lines.
func (r *Renderer) wrapWithBorder(lines []ui.Line, n *scene.Node, contentWidth int) []ui.Line {
	frameStyle := borderStyleName(n)

	var tl, tr, bl, br, horiz, vert rune
	switch n.BorderShape() {
	case "double":
		tl, tr, bl, br = '╔', '╗', '╚', '╝'
		horiz, vert = '═', '║'
	case "ascii":
		tl, tr, bl, br = '+', '+', '+', '+'
		horiz, vert = '-', '|'
	default:
		tl, tr, bl, br = '┌', '┐', '└', '┘'
		horiz, vert = '─', '│'
	}

	innerWidth := contentWidth
	if innerWidth <= 0 {
		innerWidth = 80
	}

	var bordered []ui.Line

	// Top border with optional title.
	topText := string(tl) + strings.Repeat(string(horiz), innerWidth) + string(tr)
	if n.Title != "" {
		titleText := n.Title
		if innerWidth > len(titleText)+4 {
			topText = string(tl) + " " + titleText + " " +
				strings.Repeat(string(horiz), innerWidth-len(titleText)-2) + string(tr)
		}
	}
	bordered = append(bordered, ui.Line{ui.Span{Text: topText, Style: frameStyle}})

	// Content lines wrapped with vertical bars, span by span: a bordered
	// overlay keeps each span's own token instead of welding the row into the
	// box's style (the same weld padLine removed from the borderless path).
	for _, l := range lines {
		row := append(ui.Line{}, ui.Span{Text: string(vert), Style: frameStyle})
		row = append(row, padLine(l, innerWidth)...)
		row = append(row, ui.Span{Text: string(vert), Style: frameStyle})
		bordered = append(bordered, row)
	}

	// Bottom border.
	bottomText := string(bl) + strings.Repeat(string(horiz), innerWidth) + string(br)
	bordered = append(bordered, ui.Line{ui.Span{Text: bottomText, Style: frameStyle}})

	return bordered
}

// renderList renders a filterable list of slash command matches. The bind is
// slash.matches (derived from the host's command registry, filtered by the
// typed substring). The list supports count header and category tabs.
func (r *Renderer) renderList(n *scene.Node, state fold.State, budget int) ui.Frame {
	var lines []ui.Line

	switch n.Bind {
	case "agent.todos":
		// Each todo renders as: "task  (blockedOn, actor)" — one line per
		// entry. An empty list renders the placeholder.
		for _, t := range state.Todos {
			taskText := t.Task
			if t.BlockedOn != "" || t.Actor != "" {
				detail := ""
				if t.BlockedOn != "" {
					detail = t.BlockedOn
				}
				if t.Actor != "" {
					if detail != "" {
						detail += ", "
					}
					detail += t.Actor
				}
				taskText += fmt.Sprintf("  (%s)", detail)
			}
			wrapped := ui.WrapSpans([]ui.Span{{Text: taskText, Style: "text"}}, r.Width, nil)
			lines = append(lines, wrapped...)
		}
		if len(lines) == 0 {
			lines = append(lines, ui.Line{ui.Span{Text: "no tasks", Style: "dim"}})
		}

	case "slash.matches":
		// slash.matches is an array of SlashMatch from the host's command
		// registry, filtered by the typed substring.
		matches := state.SlashMatches
		if len(matches) == 0 && state.SlashTyped != "" {
			matches = nil
		}

		// Count header: the menu's orientation line. It is chrome, so it is
		// dim — sobria emphasizes by brightening text, and nothing about a
		// count is emphasis.
		if n.Count && len(matches) > 0 {
			lines = append(lines, ui.Line{ui.Span{
				Text:  fmt.Sprintf("Commands %d · type to filter", len(matches)),
				Style: "dim",
			}})
			// A blank row separates the count header from the command rows so
			// the orientation line does not read as the first option. The row
			// inherits the list's style (dim) through the pad, not its own token.
			lines = append(lines, ui.Line{ui.Span{Text: ""}})
		}

		// The name column is padded from the longest name in the current
		// match set, not from a global one: descriptions line up without
		// reserving width for a word nobody is looking at.
		nameW := 0
		for _, m := range matches {
			if w := ansiStringWidth(m.Name); w > nameW {
				nameW = w
			}
		}

		// The selected row is the bright one and every other row is dim.
		// Sobria has no color and paints no backgrounds: emphasis is
		// brightening text, so the highlighted command is the only row at
		// full brightness (docs/BINDS.md §4.3, slash.selected). An index the
		// filter shrank past clamps to the last row, never to nothing: Enter
		// will submit the clamped row, so the menu has to show it — a menu
		// with no highlight while Enter still acts is a menu that lies.
		selected := state.SlashSelected
		if selected >= len(matches) {
			selected = len(matches) - 1
		}
		if selected < 0 || !state.SlashActive {
			selected = -1 // no row is the bright one
		}

		for i, m := range matches {
			style := "dim"
			if i == selected {
				style = "text"
			}
			lines = append(lines, slashRow(m, nameW, r.Width, style))
		}

		if len(lines) == 0 {
			if state.SlashActive {
				lines = append(lines, ui.Line{ui.Span{Text: "no matches", Style: "dim"}})
			}
		}

	default:
		// Unknown bind: render placeholder.
		lines = append(lines, ui.Line{ui.Span{Text: "[…]", Style: "dim"}})
	}

	return ui.Frame{Live: lines, Width: r.Width, Height: len(lines)}
}

// slashRow builds one menu row: name, two spaces, description, and the
// category right-aligned at the frame's edge. The row comes out exactly width
// columns wide, so the overlay's pad pass is a no-op and the category stays
// flush right. On a narrow overlay the description is the column that gives
// up ground, never the category: a category that jumps column to column as
// the filter narrows is unreadable, and a clipped description still says more
// than a missing one.
func slashRow(m fold.SlashMatch, nameW, width int, style string) ui.Line {
	catW := ansiStringWidth(m.Category)
	desc := m.Description
	if room := width - nameW - 2 - 1 - catW; room >= 0 && ansiStringWidth(desc) > room {
		desc = truncateText(desc, room)
	}
	row := ui.Line{{Text: m.Name, Style: style}}
	if gap := nameW - ansiStringWidth(m.Name); gap > 0 {
		row = append(row, ui.Span{Text: strings.Repeat(" ", gap), Style: style})
	}
	row = append(row, ui.Span{Text: "  " + desc, Style: style})
	if pad := width - nameW - 2 - ansiStringWidth(desc) - catW; pad > 0 {
		row = append(row, ui.Span{Text: strings.Repeat(" ", pad), Style: style})
	}
	return append(row, ui.Span{Text: m.Category, Style: style})
}

// resolveBind resolves a bind string into its current value from fold.State.
// This is the read-only projection layer: scenes name addresses, the host
// computes them (ADR-0003). Unknown binds render as a placeholder.
func resolveBind(bind string, state fold.State) string {
	switch bind {
	case "chat.history":
		return state.ChatHistoryMarkdown()
	case "thinking.text":
		return state.ThinkingText
	case "agent.working":
		if state.AgentWorking {
			return "true"
		}
		return "false"
	case "agent.mode":
		return state.AgentMode
	case "model.name":
		return state.ModelName
	case "usage.in":
		return fmt.Sprintf("%d", state.UsageIn)
	case "usage.out":
		return fmt.Sprintf("%d", state.UsageOut)
	case "usage.delta":
		return state.UsageDelta
	case "user.input":
		return state.UserInput
	case "slash.active":
		if state.SlashActive {
			return "true"
		}
		return "false"
	case "slash.hint":
		return state.SlashHint
	case "status.active":
		return state.StatusActive
	case "host.escape.armed":
		if state.EscapeArmed {
			return "true"
		}
		return "false"
	case "host.scene.error":
		return state.SceneError
	case "session.tokens_used":
		// The fifth instance of the checked-but-never-drawn class, and the
		// first where the case was already here. The four before it were
		// missing — no case, so the bind fell to the default and drew "[…]".
		// This one answered, and answered with a constant: it returned the
		// literal "0" on the stated grounds that the budget was "not yet
		// wired from run.started". That was true when it was written and is
		// not true now — fold.go captures budget_usd into BudgetMicrounits
		// on run.started, accumulates cost_usd into CostMicrounits on every
		// llm.response, and deriveSessionTokensUsed() subtracts them into
		// SessionTokensUsed on every fold.
		//
		// A stale justification is worse than a missing case, because both
		// of the signals that would expose one are inverted. A missing bind
		// draws the placeholder, which at least looks wrong on screen; this
		// drew "0", which is the bind's own signed empty state, so the
		// screen looked correct at every budget. And the audits read case
		// labels out of this source to decide a bind is handled, so the
		// presence of the label satisfied the structural guard while the
		// body ignored the fold entirely.
		//
		// The remedy is to read the field rather than restate the claim: the
		// projection follows fold.State, so it cannot go stale again when
		// the wiring behind it changes.
		return fmt.Sprintf("%d", state.SessionTokensUsed)

	// The scalar binds below are signed in docs/BINDS.md §4, accepted by
	// validate.go, and computed by internal/fold on every event — and until
	// this block existed, none of them were read here. They fell to the
	// default and drew "[…]", which is the checked-but-never-drawn failure
	// this project has now paid for four times: the document validates, the
	// screen is wrong, and no diagnostic fires anywhere, because clearing
	// validation is exactly the signal that says the scene is fine.
	//
	// What separates this instance from row_template — refused rather than
	// implemented, because its semantics were unsigned — is that nothing was
	// left to design. The fold already maintains every field named here, so
	// the value existed, was correct, and was discarded one function short of
	// the frame. Projecting it invents no format.
	case "todos.count":
		return fmt.Sprintf("%d", state.TodosCount)
	case "run.quiescent.diagnosis":
		return state.QuiescentDiag
	case "agent.blocked.actor":
		return state.BlockedActor
	case "agent.blocked.blocked_on":
		return state.BlockedOn
	case "slash.typed":
		return state.SlashTyped
	case "slash.selected":
		return fmt.Sprintf("%d", state.SlashSelected)
	case "ui.focus":
		return state.UIFocus
	case "ui.max":
		return state.UIMax
	case "ui.surface":
		return state.UISurface

	default:
		// An unsatisfied bind renders as a placeholder, never a crash —
		// the engine contract that makes community preview (Q16) and forward
		// compatibility possible at the same time (ADR-0003).
		return placeholderValue
	}
}

// placeholderValue is what an unresolvable bind displays. It is named rather
// than repeated so evalWhen can recognise it: the difference between "the
// value is this text" and "there is no value" is invisible once both are
// strings, and that ambiguity is what let an unresolved gate read as true.
const placeholderValue = "[…]"

// evalWhen evaluates a `when` bind string as a boolean predicate. A non-empty
// string value is truthy; "false", "0", and "" are falsy.
//
// The placeholder is falsy, and that is a decision rather than a detail. An
// unresolvable bind still *displays* as "[…]" so a scene from a newer build
// draws instead of crashing (ADR-0003) — but visibility is a different
// question from display, and "I cannot resolve this" is not an affirmative
// answer to it. Treating the placeholder as an ordinary non-empty string made
// every unresolved gate render its node ON, which for `ui.max` inverted the
// signed default precisely: BINDS.md gives "null — no pane is maximized", and
// Scene 10 gates each pane on it, so the unknown state painted maximized
// chrome over a screen with nothing maximized.
//
// That direction is the dangerous one. A dropped value degrades toward the
// empty state and the user sees less than they asked for; a gate that fails
// open degrades toward chrome they never asked for and cannot dismiss.
func evalWhen(bind string, state fold.State) bool {
	val := resolveBind(bind, state)
	switch val {
	case "", "0", "false", placeholderValue:
		return false
	default:
		return true
	}
}

// styleName extracts the style token from a node's style map, or returns ""
// if unset. Scene nodes carry styles as a map (e.g. {"style": "dim"}).
//
// The keys come from scene.StyleTokenKeys rather than being spelled here, and
// that indirection is the whole point of the function. This read used to be
// style["style"] alone while ValidateTokens accepted "token" as well, so a
// scene using the other accepted spelling passed validation and then rendered
// with no style — the one failure mode that reports success, since clearing
// validation is precisely the signal that says the document is fine. A refusal
// would have named the fix; this said nothing and drew the wrong screen.
//
// Reading the validator's own list means the two cannot disagree again: a key
// the validator stops accepting stops being read here, and one it starts
// accepting is honoured without a second edit anybody has to remember.
func styleName(style map[string]string) string {
	if style == nil {
		return ""
	}
	for _, key := range scene.StyleTokenKeys() {
		if name := style[key]; name != "" {
			return name
		}
	}
	return ""
}
