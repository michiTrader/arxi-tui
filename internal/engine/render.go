package engine

import (
	"fmt"
	"math"
	"strconv"
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

	// curRow is the current template row's fields while a row_template is being
	// instantiated, and nil everywhere else. A `row.<field>` bind resolves
	// against it (D1 / BINDS.md §4.7). It lives on the Renderer rather than in
	// every render method's signature because a row is just extra resolution
	// context the whole subtree under one template instance shares, and
	// threading it through the render methods would be a fresh chance to forget
	// it at each -- the same arithmetic withFocusGlow avoids. renderList saves
	// and restores it around each row, so a template nested inside a template
	// row still resolves its own row rather than its parent's.
	curRow map[string]string

	// AnimTicks is per-node animation phase, host-computed view state fed in
	// per repaint (ADR-0005). For a scroll marquee it is the tick count the
	// node's clock has reached; renderMarquee turns `ticks * speed` into a
	// horizontal offset. It is an *input* separate from fold.State — the fold
	// projects content, never motion (Q9) — so RenderFrame stays a pure
	// function of (document, fold state, phase) and every golden pins a phase
	// exactly as it pins a fold state. nil (the common case, and every golden
	// that is not about motion) means every node's tick count is zero, which is
	// the settled/starting frame.
	AnimTicks map[string]int

	// AnimPhase is per-node one-shot animation phase, the eased fraction of a
	// prop's run the node has reached (ADR-0005 / G-B), host-computed and fed in
	// per repaint like AnimTicks. Where AnimTicks is a continuous scroll's tick
	// count, this is a one-shot reveal/transition's phase in [0,1] after the
	// token's curve is applied — the loop owns the clock and the curve, the
	// renderer only turns the phase into a frame (renderText clips to a
	// phase-wide prefix). It is a separate input from fold.State for the same
	// reason AnimTicks is: the fold projects content, never motion (Q9).
	//
	// The nil-ness carries meaning the value cannot. A nil map is the pure/golden
	// path with no clock driving time, and a reveal node draws settled (its whole
	// text) — the no-op guarantee that keeps a non-motion golden unchanged. A
	// non-nil map with no entry for a node is that node's first appearance in the
	// live loop, phase 0, the start of the reveal; the clock seeds it after this
	// frame. So absent-because-no-clock and absent-because-just-appeared are
	// distinguished by whether the map itself is nil, which is why renderText
	// checks the map before indexing it.
	AnimPhase map[string]float64

	// active, when non-nil, collects the visible nodes that are animating this
	// frame, so the loop can maintain per-node elapsed time and size the ticker
	// (ADR-0005: the ticker runs only while a visible node animates). It is a
	// pointer because the walk is recursive and every sub-renderer shares one
	// accumulator; it is nil in the pure/golden path, where nobody is driving a
	// clock and the activity report has no consumer. RenderFrameActive sets it;
	// RenderFrame leaves it nil.
	active *[]AnimActivity

	// ChatScroll is how many lines the chat pane is scrolled up from the tail,
	// host-owned view state fed in per repaint like the input buffer. Zero (the
	// default, and every golden) follows the tail and windows exactly as the old
	// tail-clip did, so no golden moves. A positive value shifts the visible
	// window that many wrapped lines toward the top, which is what the mouse wheel
	// and scroll keys drive. It is an input separate from fold.State for the same
	// reason the anim phase is: the fold projects content, the host owns where the
	// reader is looking.
	ChatScroll int

	// ChatScrollMax is the render's report back to the host: the largest legal
	// ChatScroll for the frame just drawn (total wrapped chat lines minus the
	// pane's budget, floored at zero). The loop clamps its offset to this after
	// each repaint so a wheel spun past the top does not accumulate dead scroll
	// that a later wheel-down has to unwind before anything moves — the clamp is
	// single-sourced in the renderer, which is the only place that knows the line
	// count and the budget. Zero when the chat fits and there is nothing to scroll.
	ChatScrollMax int
}

// AnimActivity is one visible animating node, reported back to the loop so it
// can decide whether to keep the ticker running and at what rate. NodeID is the
// node's id (the key the loop holds elapsed time under); Paused is the current
// truthiness of the node's pause_when bind, so a marquee frozen by pause_when
// asks for no ticks while still being present.
//
// OneShot and Token were added for the one-shot props (G3 reveal, later G1/G4).
// A continuous scroll ticks forever and reads AnimTicks; a one-shot runs its
// phase 0→1 once over a token's duration and reads AnimPhase. OneShot selects
// which the loop clock drives for this node, and Token names the timing token
// whose duration/curve/fps the loop resolves against the theme (the renderer has
// no theme, so it reports the name and the loop fills the timing in). Both are
// zero for a scroll, which keeps its existing single-rate path unchanged.
//
// Row is enter's per-row stagger index (G4). enter reports one activity per row
// with its position i, and the loop starts row i's own entrance at offset
// i * duration — so a single token, one clock per row id, spreads the rows out
// in time. It is zero for every other one-shot (reveal, transition) and for a
// scroll, so those keep their offset-free single-clock path: a row 0 is a
// one-shot that starts at zero, which is exactly what a lone reveal or
// transition is.
type AnimActivity struct {
	NodeID  string
	Paused  bool
	OneShot bool
	Token   string
	Row     int
}

// child builds a sub-renderer for a nested layout region, inheriting the
// resolution context every node in that region shares: the current template
// row, the animation phase input, and the activity accumulator. It exists so
// "some sub-renderers carry the animation phase and others do not" is
// unrepresentable — the defect class LESSONS.md records for `when` and style
// tokens, where a fix scoped to one construction site left the others silently
// uncovered. A marquee nested in a box, a column or an overlay must scroll and
// report exactly as one at the root does, and threading these by hand at each
// construction site is a fresh chance to forget one.
func (r *Renderer) child(width, height int) Renderer {
	return Renderer{
		Width:     width,
		Height:    height,
		curRow:    r.curRow,
		AnimTicks: r.AnimTicks,
		AnimPhase: r.AnimPhase,
		active:    r.active,
	}
}

// RenderFrame renders a scene document into a frame, binding fold.State into
// the nodes' bind fields. It is the pure entry point: no clock, no activity
// report, the same frame for the same (document, state, phase) every time.
func (r *Renderer) RenderFrame(doc *scene.Document, state fold.State) ui.Frame {
	frame, _ := r.RenderFrameActive(doc, state)
	return frame
}

// RenderFrameActive renders the frame and also reports which visible nodes are
// animating, so the host loop can drive the clock (ADR-0005). The frame it
// returns is byte-identical to RenderFrame's for the same inputs — the activity
// slice is a side channel the loop reads, never a thing that changes a pixel —
// so a golden may call either. The activity report is empty unless a visible
// node carries an animation prop the engine draws.
func (r *Renderer) RenderFrameActive(doc *scene.Document, state fold.State) (ui.Frame, []AnimActivity) {
	if doc == nil || doc.Root == nil {
		return ui.Frame{}, nil
	}
	var collected []AnimActivity
	r.active = &collected
	frame := r.renderNode(doc.Root, state, r.Height)
	return frame, collected
}

// renderNode lays one node out within a budget of rows. Every container passes
// its children budgets, never more than it owns; every leaf clips its content
// to what it was given.
//
// The `when` gate is applied here rather than per node type, because `when` is
// a universal property in SCENES.md and this is the one function every node
// passes through. It used to be honoured in exactly two places — a `row`
// filtered its children, and an `overlay` gated itself — so a gated node drew
// unconditionally anywhere else, and the axis was the parent rather than the
// node: the same `text` hid under a `row` and drew under a `stack`.
func (r *Renderer) renderNode(n *scene.Node, state fold.State, budget int) ui.Frame {
	if hiddenByWhenRow(n, state, r.curRow) {
		return ui.Frame{Width: r.Width, Height: 0}
	}
	n = withFocusGlow(n, state)
	n = r.withTransition(n)
	// enter (G4) is the last wrapper, and it wraps the type switch rather than a
	// single node the way withTransition does, because its axis is the container's
	// rows, not one node's style: row:true staggers the children (or the rows of a
	// row_template) into the frame, row:false dims the whole rendered subtree as
	// one unit. Both need the node's ordinary rendering first, so enter delegates
	// to renderByType and post-processes the frame.
	if n.Enter != nil {
		return r.renderEnter(n, state, budget)
	}
	return r.renderByType(n, state, budget)
}

// renderByType is renderNode's dispatch on the node's type, split out so enter
// (G4) can render a node's ordinary content and then stagger or dim it. Every
// path that is not an enter reaches it straight from renderNode, so the split is
// invisible to them.
func (r *Renderer) renderByType(n *scene.Node, state fold.State, budget int) ui.Frame {
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
		return r.renderUnknownType(n)
	}
}

// renderUnknownType draws the placeholder for a node type this engine does not
// build, and it names the type because the name is the whole diagnostic.
//
// The placeholder used to be the constant string `[[UNKNOWN NODE TYPE]]`, and
// measured on the tree with the suite green, that made two different mistakes
// produce one identical pixel:
//
//	{"type": "button"}  -> validates clean, draws [[UNKNOWN NODE TYPE]]
//	{"type": "buton"}   -> validates clean, draws [[UNKNOWN NODE TYPE]]
//
// The first is the forward-compatibility rule working as PLAN.md signs it: a
// documented v0 primitive that Phase 3 will build, and the screen honestly
// says the engine could not draw it. The second is a typo, and the author has
// been handed the same sentence as the person who did nothing wrong. Nothing
// anywhere — parse, validate, warnings, frame — mentions the six letters that
// would end the search, so the remedy is to re-read the document and hope.
//
// That is the defect class this repository has already paid for four times
// under a different name: the layer that knows the answer does not say it.
// `refuseUnrendered` in the scene package argues the same point for properties
// and calls the diagnosis, not the severity, the thing worth getting right —
// telling an author "invalid" sends them hunting for a typo that is not there,
// and telling them nothing sends them hunting for one that is.
//
// Naming the type is the cheap half of the fix and is deliberately all this
// function does. Whether a *later* engine could be right about this type —
// LESSONS.md's question that separates a documented gap from a misspelling —
// needs an inventory of the planned vocabulary, which belongs to the package
// that owns the format, not to the renderer. Inventing that list here would be
// format invented in the engine, the objection that keeps row_template and
// on_press refused.
//
// The `[[UNKNOWN NODE TYPE` prefix is preserved verbatim: three golden tests
// and the boot loop assert its *absence* by substring, and a rename would
// silently switch off all four.
func (r *Renderer) renderUnknownType(n *scene.Node) ui.Frame {
	// An absent type is its own mistake and reads terribly as `""`: a node
	// that never declared a type is not a node whose type is the empty
	// string, and `[[UNKNOWN NODE TYPE ""]]` invites a hunt for a quoting
	// bug. Say which of the two happened.
	label := fmt.Sprintf("[[UNKNOWN NODE TYPE %q]]", n.Type)
	if n.Type == "" {
		label = "[[UNKNOWN NODE TYPE: the node declares no \"type\"]]"
	}
	return ui.Frame{
		Live: []ui.Line{
			{ui.Span{Text: label, Style: "error"}},
		},
		Width: r.Width,
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
		// A child the host has hidden reserves nothing. renderNode already
		// returns an empty frame for it, so a fixed child costs no rows
		// either way; a *grow* child would otherwise still take its share of
		// the remaining space and paint that share as blank rows — hiding the
		// content while keeping the hole it sat in.
		if hiddenByWhenRow(c, state, r.curRow) {
			continue
		}
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
			// The input is as tall as its wrapped rows (one line when it fits),
			// and the overlay goes after whatever that came to; using the frame's
			// own height keeps this correct now that a long line grows the input.
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
	//
	// renderNode gates the drawing; this pass gates the *column*, which is the
	// part it cannot do from inside a child. A hidden child returning an empty
	// frame would still be counted here, divide the width with its siblings,
	// and leave its share as padding.
	visible := make([]*scene.Node, 0, len(children))
	for _, c := range children {
		if hiddenByWhenRow(c, state, r.curRow) {
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
		mr := r.child(totalWidth, r.Height)
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
				subR := r.child(colWidths[i], r.Height)
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

// userTurnMarker is prepended to a user's chat turn so the transcript visibly
// separates the human's words from the agent's. It matches the "❯ " prompt glyph
// the factory scenes already use for the input line, so the same symbol means
// "you" at the point of typing and in the history above.
const userTurnMarker = "❯ "

// userTurnToken paints the user's own turns in the transcript. It is a distinct
// token from the pane's default so the theme can lift the human's words above
// the agent's — sobria maps it to white — which is the second half of telling
// the voices apart, the marker being the first. It is minted by the engine like
// "text" and "dim" are: a theme that does not define it resolves to the zero
// style, so a user turn simply falls back to the pane's ordinary colour rather
// than failing to render.
const userTurnToken = "chat.user"

// renderMarkdown renders a bound markdown pane, wrapped to the frame width.
// Wrapping goes through the ported Line/Span machinery, so a row can never end
// in bare air or overflow the frame no matter what the fold hands it.
func (r *Renderer) renderMarkdown(n *scene.Node, state fold.State, budget int) ui.Frame {
	var lines []ui.Line

	// "text" is the token the pane mints when the scene declares none. A
	// declared token replaces it, because the transcript is content this node
	// draws itself and SCENES.md signs `style` on every node, not only on
	// `text` ones. The token is handed to the wrapper up front rather than
	// applied to the spans afterwards: WrapText splits into spans, and
	// rewriting them post hoc is the same weld padLine exists to remove.
	token := styleNameOr(n.Style, "text")

	switch n.Bind {
	case "chat.history":
		for _, h := range state.History {
			text := h.Text
			turnToken := token
			if h.Role == "user" {
				// Mark the user's own turns so the transcript does not read as a
				// single voice. Without a marker a reader cannot tell what they
				// asked from what the agent answered — the differentiation the
				// chat was missing. The marker is prepended to the text before
				// wrapping so it rides the first row of the turn; agent turns stay
				// unmarked, which is the default voice of the pane. The turn is
				// wrapped under userTurnToken so it also carries the user's colour,
				// while an agent turn keeps the pane's own token.
				text = userTurnMarker + text
				turnToken = userTurnToken
			}
			lines = append(lines, ui.WrapText(text, turnToken, r.Width, nil)...)
			lines = append(lines, ui.Line{}) // one blank row between turns
		}
		if len(lines) > 0 {
			lines = lines[:len(lines)-1]
		}
	case "thinking.text":
		lines = append(lines, ui.WrapText(state.ThinkingText, token, r.Width, nil)...)
	default:
		lines = append(lines, ui.WrapText(n.Text, token, r.Width, nil)...)
	}
	if budget >= 0 && len(lines) > budget {
		if n.Bind == "chat.history" {
			// The chat pane is scrollable: window it bottom-anchored, then shift
			// up by the host's ChatScroll (clamped), and report the clamp back so
			// the loop can pin its offset. ChatScroll==0 lands the window on the
			// last `budget` lines — byte-for-byte the old tail-clip, so no golden
			// moves — while a positive offset reveals older turns above.
			max := len(lines) - budget
			s := r.ChatScroll
			if s < 0 {
				s = 0
			}
			if s > max {
				s = max
			}
			r.ChatScrollMax = max
			start := max - s
			lines = lines[start : start+budget]
		} else {
			lines = lines[len(lines)-budget:]
		}
	}
	return ui.Frame{Live: lines, Width: r.Width}
}

// renderInput renders the input row and the column the terminal caret belongs
// in. The typed line wraps: when it is wider than the pane it flows onto further
// rows, and the caret is reported as a (row, col) inside them.
//
// A single-line input was the source of a reported bug — the moment the text
// filled the width, the caret column ran off the right edge and the terminal
// clamped it into the bottom-right corner, where the arrow keys could move it
// nowhere in any direction. Wrapping the line and mapping the rune caret onto the
// visual rows is what lets the caret sit on the glyph the user is about to edit
// no matter how long the line grows, which is the whole point of arrow-key motion.
//
// The placeholder is drawn only while the line is empty, and under its own token
// so a theme can dim it. It used to be concatenated in front of the typed buffer
// under the same token as the text, which is how "ask anything, or / for
// commands" came out welded to the first word the human typed: a hint is not part
// of the line, and the moment there is a line it is gone.
//
// The caret is reported and never drawn. A reversed cell standing in for a cursor
// is the single most obvious tell that a TUI is not a native prompt, and the
// position is the one thing the terminal cannot work out for itself.
func (r *Renderer) renderInput(n *scene.Node, state fold.State) ui.Frame {
	// sobria: the input may carry a string prefix (e.g. "┃ ") rendered as styled
	// leading cells before the text. The caret starts after it, because the prefix
	// is chrome and the human types to the right of chrome.
	prefix := n.PrefixText()
	prefixW := ansiStringWidth(prefix)

	// Empty line: draw the placeholder on one row, caret just past the prefix. The
	// placeholder keeps its own token; the typed line takes the declared token (or
	// "input"), and the paragraph above records what welding the two cost once.
	if !(n.Bind == "user.input" && state.UserInput != "") {
		var cells ui.Line
		if prefix != "" {
			cells = append(cells, ui.Span{Text: prefix, Style: "input"})
		}
		cells = append(cells, ui.Span{Text: n.Placeholder, Style: "input.placeholder"})
		return ui.Frame{
			Live:   []ui.Line{cells},
			Width:  r.Width,
			Height: 1,
			Cursor: ui.Cursor{Line: 0, Col: prefixW},
		}
	}

	// The text wraps into a column `room` wide — the pane minus the prefix — so
	// every visual row, first and continuation, has the same room and the caret
	// column arithmetic is uniform. The prefix is repeated on every row rather
	// than blanked to spaces on the continuations: the "┃ " bar marks the whole
	// input as one field, and a reader typing a multi-line prompt sees the same
	// left edge on every line instead of a marked first line over a floating tail.
	// The caret column is still prefixW+col because the repeated prefix has the
	// same width the old indent did, so the arithmetic below is unchanged.
	inputStyle := styleNameOr(n.Style, "input")
	room := r.Width - prefixW
	if room < 1 {
		room = 1
	}
	rowsText := inputVisualRows(state.UserInput, room)

	live := make([]ui.Line, 0, len(rowsText))
	for _, rt := range rowsText {
		var cells ui.Line
		if prefix != "" {
			cells = append(cells, ui.Span{Text: prefix, Style: "input"})
		}
		cells = append(cells, ui.Span{Text: rt, Style: inputStyle})
		live = append(live, cells)
	}

	row, col := inputCaretRowCol(state.UserInput, state.UserInputCaret, room)
	return ui.Frame{
		Live:   live,
		Width:  r.Width,
		Height: len(live),
		Cursor: ui.Cursor{Line: row, Col: prefixW + col},
	}
}

// inputVisualRows splits the typed text into visual rows: it breaks on an
// explicit newline (a multi-line prompt the user pasted or entered with
// Shift/Ctrl+Enter) and, within each line, wraps at `room` display columns so a
// line wider than the pane flows onto continuation rows. It walks runes and
// measures display width so a wide glyph is never split across the wrap, and it
// walks them the same way inputCaretRowCol does so the drawn rows and the caret
// can never disagree about where a break falls. When the last content row fills
// the width exactly a trailing empty row is added: an editor puts the caret
// where the next character will land, and after a full row that is a fresh row
// below — not welded to the last cell, where the terminal would clamp it. A text
// ending in a newline already ends on an empty row, so no extra one is added.
func inputVisualRows(text string, room int) []string {
	if room <= 0 {
		return []string{text}
	}
	rs := []rune(text)
	var rows []string
	line := make([]rune, 0, room)
	col := 0
	flush := func() {
		rows = append(rows, string(line))
		line = line[:0]
		col = 0
	}
	for _, r := range rs {
		if r == '\n' {
			flush()
			continue
		}
		w := ansiStringWidth(string(r))
		if col+w > room {
			flush()
		}
		line = append(line, r)
		col += w
	}
	rows = append(rows, string(line))
	if col >= room && col > 0 {
		rows = append(rows, "")
	}
	return rows
}

// inputCaretRowCol maps a rune caret index into the (row, col) it occupies once
// the text is laid out, walking the runes exactly as inputVisualRows does: an
// explicit newline advances to the start of the next row and occupies no column,
// and a run wider than `room` wraps. col is a display width, so a wide glyph
// before the caret advances it two cells — a rune count would drift on CJK/emoji
// input. A caret that exactly fills a row sits at the start of the next one,
// matching the trailing empty row inputVisualRows appends, so the cursor is
// already where the next glyph will wrap to.
func inputCaretRowCol(text string, caret, room int) (row, col int) {
	if room <= 0 {
		return 0, 0
	}
	rs := []rune(text)
	if caret < 0 {
		caret = 0
	}
	if caret > len(rs) {
		caret = len(rs)
	}
	for i := 0; i < caret; i++ {
		if rs[i] == '\n' {
			row++
			col = 0
			continue
		}
		w := ansiStringWidth(string(rs[i]))
		if col+w > room {
			row++
			col = 0
		}
		col += w
	}
	if col >= room {
		row++
		col = 0
	}
	return row, col
}

// InputCaretVerticalMove returns the caret index one wrapped row up (dir < 0) or
// down (dir > 0) from the current caret, keeping the display column as near the
// original as the target row allows. It wraps the text at `room` exactly as
// renderInput draws it — through inputCaretRowCol — so the caret the user watches
// move is the caret this computes. With no row in that direction (Up on the first
// row, Down on the last) the caret is returned unchanged: a vertical key at the
// edge is a no-op, not a jump to the start or end, which is what an editor does
// and what arxi_cli_sim's line editor does.
func InputCaretVerticalMove(text string, caret, room, dir int) int {
	if room <= 0 || dir == 0 {
		return caret
	}
	rs := []rune(text)
	if caret < 0 {
		caret = 0
	}
	if caret > len(rs) {
		caret = len(rs)
	}
	// Every caret index's (row, col), computed the one way the renderer does, so a
	// vertical move can never disagree with where the row breaks are drawn.
	rows := make([]int, len(rs)+1)
	cols := make([]int, len(rs)+1)
	for i := range rows {
		rows[i], cols[i] = inputCaretRowCol(text, i, room)
	}
	targetRow := rows[caret] + dir
	if targetRow < 0 {
		return caret
	}
	wantCol := cols[caret]
	best := -1
	for i := range rows {
		if rows[i] != targetRow {
			continue
		}
		if cols[i] >= wantCol {
			best = i // first index at or past the wanted column
			break
		}
		best = i // otherwise the furthest column this shorter row reaches
	}
	if best < 0 {
		return caret // no row in that direction
	}
	return best
}

// renderText renders a static text line or a bound text value. The sobria
// status row uses text nodes with bind ("agent.mode", "model.name") so the
// host resolves the bind into a styled span.
func (r *Renderer) renderText(n *scene.Node, state fold.State) ui.Frame {
	style := styleName(n.Style)
	text := n.Text
	if n.Bind != "" {
		text = resolveBindRow(n.Bind, state, r.curRow)
	}
	if n.Reveal != nil {
		text = r.revealPrefix(n, text)
	}
	return ui.Frame{
		Live:   []ui.Line{{ui.Span{Text: text, Style: style}}},
		Width:  r.Width,
		Height: 1,
	}
}

// revealPrefix returns the share of a text node's content its reveal shows this
// frame (G3), and reports the node active so the host clock keeps ticking while
// it is mid-reveal (ADR-0005). It is the character-count axis SCENES.md Scene 4
// signs: the renderer already draws a width-clipped prefix of any text, and a
// reveal only moves where that clip falls as the phase rises — it invents no new
// rendering.
//
// The phase is host-computed and curve-eased (the loop owns the clock, G-B) and
// arrives as AnimPhase[id]: a fraction in [0,1] of the content's display width
// to show. It is cut at grapheme boundaries the way truncateText already cuts,
// so a reveal never splits a wide character.
//
// The activity report is unconditional for a visible reveal with content, the
// way scroll reports itself active whenever it overflows: a reveal node is an
// animating node, present or settled. The clock decides settled-ness from
// elapsed against the token's duration and stops forcing ticks once past it — a
// settled reveal is present but quiet, the same shape as a paused marquee. The
// renderer only says "this node reveals, on this token"; it does not own the
// clock.
//
// nil AnimPhase draws the whole text: the pure/golden path has no clock, so a
// reveal renders settled, the no-op guarantee that leaves a non-motion golden
// unchanged. A non-nil map missing this id is the first appearance in the live
// loop — phase 0, nothing shown — which is why the map is checked for nil before
// it is indexed.
func (r *Renderer) revealPrefix(n *scene.Node, text string) string {
	if text == "" {
		// Nothing to reveal, and nothing to animate: an empty string has no
		// prefixes to grow through, so it reports no activity either — the loop
		// must not arm a ticker for a node that will never change.
		return text
	}
	if r.active != nil {
		*r.active = append(*r.active, AnimActivity{
			NodeID:  n.ID,
			OneShot: true,
			Token:   n.Reveal.Anim,
		})
	}
	if r.AnimPhase == nil {
		return text // no clock: settled
	}
	phase := r.AnimPhase[n.ID] // absent → 0.0 → the start of the reveal
	if phase >= 1 {
		return text
	}
	if phase <= 0 {
		return ""
	}
	width := ansi.StringWidth(text)
	shown := int(math.Round(phase * float64(width)))
	if shown <= 0 {
		return ""
	}
	if shown >= width {
		return text
	}
	return ansi.Cut(text, 0, shown)
}

// renderRule draws a single full-width horizontal rule.
func (r *Renderer) renderRule(n *scene.Node, state fold.State) ui.Frame {
	width := r.Width
	if width <= 0 {
		width = 1
	}
	// The rule glyph: a row of box-drawing horizontal dashes, matching the
	// sobria aesthetic of rules instead of frames (PLAN.md §2).
	//
	// "rule" is the token this node mints for itself, and a token the scene
	// declares replaces it: a separator the author asked to have dimmed is a
	// separator that draws dim. It used to be stamped unconditionally, so the
	// declaration validated and did nothing.
	rule := strings.Repeat("─", width)
	return ui.Frame{
		Live:   []ui.Line{{ui.Span{Text: rule, Style: styleNameOr(n.Style, "rule")}}},
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
	innerRenderer := r.child(innerWidth, innerHeight)
	var content ui.Frame
	if len(n.Children) > 0 {
		content = innerRenderer.renderStack(&scene.Node{
			Type:     "stack",
			Children: n.Children,
		}, state, innerHeight)
	} else {
		content = ui.Frame{Live: []ui.Line{ui.Line{}}, Width: innerWidth}
	}

	// Content rows are carried through span by span, keeping each child's own
	// token, exactly as wrapWithBorder does for the bordered overlay. This
	// loop used to flatten the row with l.Text() and re-emit it as a single
	// span under the box's style, which discarded every declaration the
	// children had made: a list's `dim` empty state came out bare inside
	// MAXIMUM's Tasks panel, and a child asking for `warn` inside a banner box
	// was painted `banner`.
	//
	// It is the same weld padLine was written to remove ("chrome must not
	// restyle the content it fills around") and that wrapWithBorder already
	// honours. Three of the four bordered/borderless drawing paths obeyed the
	// rule and this one did not, which is why the guard sweeps the matrix
	// rather than testing a box.
	//
	// The box's own token still reaches the screen: padLine's pad span carries
	// no style, so styleName(n.Style) is applied to it here — chrome styling
	// chrome. That is what MAXIMUM's banner relies on, and the distinction
	// between padding the box adds and content the box wraps is the whole line
	// this change draws.
	boxStyle := styleName(n.Style)
	for _, l := range content.Live {
		row := ui.Line{ui.Span{Text: string(vert), Style: frameStyle}}
		for _, s := range padLine(l, innerWidth) {
			if s.Style == "" {
				s.Style = boxStyle
			}
			row = append(row, s)
		}
		row = append(row, ui.Span{Text: string(vert), Style: frameStyle})
		lines = append(lines, row)
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
		active = evalWhenRow(n.Bind, state, r.curRow)
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

	// Prefix and suffix are composed around the main text on one line, so
	// their widths come out of the budget the main text scrolls within. Both
	// are built first — the suffix too, even though it is drawn last — because
	// the marquee window has to know how much room the main text actually has
	// before it can slice it.
	//
	// hiddenByWhen is asked here, and at the suffix below, because a nested
	// node never passes through renderNode — the owning renderer reads
	// prefix.Bind/.Text/.Style itself, so renderNode's gate at the top of the
	// dispatch never sees it. This is the third recurrence of one defect:
	// `when` was once honoured by a row and an overlay only, the fix moved the
	// gate into renderNode to make "some node types obey and others do not"
	// unrepresentable — and that fix is scoped to node *types*, while a prefix
	// is a node *position*, which no widening of a type switch reaches.
	// Measured before this line existed: a prefix declaring `when` rendered
	// byte-identically under `agent.working` and `!agent.working`, and the
	// validator accepted both.
	var prefixSpan, suffixSpan *ui.Span
	prefix := n.PrefixNode()
	if prefix != nil && !hiddenByWhenRow(prefix, state, r.curRow) {
		// A prefix is a text-bearing node: either Type=="text" or an
		// untyped node with Text set (the sobria prefix omits the type).
		if prefix.Bind != "" {
			prefixSpan = &ui.Span{Text: resolveBindRow(prefix.Bind, state, r.curRow), Style: styleName(prefix.Style)}
		} else {
			prefixSpan = &ui.Span{Text: prefix.Text, Style: styleName(prefix.Style)}
		}
	}
	if n.Suffix != nil && !hiddenByWhenRow(n.Suffix, state, r.curRow) {
		if n.Suffix.Bind != "" {
			suffixSpan = &ui.Span{Text: resolveBindRow(n.Suffix.Bind, state, r.curRow), Style: styleName(n.Suffix.Style)}
		} else if n.Suffix.Text != "" {
			suffixSpan = &ui.Span{Text: n.Suffix.Text, Style: styleName(n.Suffix.Style)}
		}
	}

	// The scroll prop (G2) turns the static main text into a marquee: instead
	// of clipping to the head, it advances a window over the content. The clock
	// is host-owned (ADR-0005) — the phase arrives as AnimTicks[id], the tick
	// count the node has reached — and the axis is horizontal offset, one the
	// renderer already draws whenever it clips text. So this only chooses which
	// window to slice at time t; it invents no new rendering (SCENES.md Scene 4).
	//
	// It scrolls only when the content is wider than the room the prefix and
	// suffix leave it — a marquee that fits has nothing to move — and it reports
	// its activity to the loop only then, so the ticker is armed only while a
	// node genuinely moves (ADR-0005: the ticker runs only while a visible node
	// animates). pause_when's truthiness rides along in the report; the loop
	// freezes the tick count while it holds, so the offset stops here without
	// the renderer owning a pause policy.
	mainStyle := styleName(n.Style)
	marqueeText := text
	if n.Scroll != nil {
		budget := r.Width
		if prefixSpan != nil {
			budget -= ansi.StringWidth(prefixSpan.Text)
		}
		if suffixSpan != nil {
			budget -= ansi.StringWidth(suffixSpan.Text)
		}
		if ansi.StringWidth(text) > budget && budget > 0 {
			offset := 0
			if cycle := ansi.StringWidth(text) + marqueeGap; cycle > 0 {
				offset = (r.AnimTicks[n.ID] * n.Scroll.Speed) % cycle
			}
			marqueeText = marqueeWindow(text, offset, budget)
			if r.active != nil {
				*r.active = append(*r.active, AnimActivity{
					NodeID: n.ID,
					Paused: n.Scroll.PauseWhen != "" && evalWhen(n.Scroll.PauseWhen, state),
				})
			}
		}
	}

	var cells []ui.Span
	if prefixSpan != nil {
		cells = append(cells, *prefixSpan)
	}
	cells = append(cells, ui.Span{Text: marqueeText, Style: mainStyle})
	if suffixSpan != nil {
		cells = append(cells, *suffixSpan)
	}

	return ui.Frame{
		Live:   []ui.Line{cells},
		Width:  r.Width,
		Height: 1,
	}
}

// marqueeGap is the run of blank cells between the tail of a scrolling marquee
// and its head reappearing, so the wrap reads as a gap rather than the last
// word running straight into the first.
const marqueeGap = 4

// marqueeWindow returns the `budget`-wide window of `content` starting at
// display column `offset`, wrapping so the head follows the tail after
// marqueeGap blank cells. It is the width-aware slice truncateText already does
// for the static case, only with a moving start: the content is laid end to end
// with its gap and itself, and ansi.Cut takes the columns the offset points at,
// so a wide character is never split.
func marqueeWindow(content string, offset, budget int) string {
	doubled := content + strings.Repeat(" ", marqueeGap) + content
	return ansi.Cut(doubled, offset, offset+budget)
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
	// An overlay with `when` only renders when that bind is truthy, and both
	// of this method's callers now ask before calling: renderNode gates every
	// node, and renderStack — which reaches renderOverlay directly, to measure
	// the overlay before the growers divide the budget — skips hidden children
	// in the same pass.
	//
	// This method used to ask a third time. That third copy was not extra
	// safety, it was mutual masking: deleting the stack's skip left this one
	// hiding the overlay, and deleting this one left the stack's skip hiding
	// it, so neither deletion failed a test and each looked unnecessary while
	// the other stood. Removing it is what makes the stack's skip the single
	// thing that can be measured for this path.

	// Resolve the content width: use min_width if set and smaller than the frame,
	// otherwise use the full frame width.
	contentWidth := r.Width
	if n.MinWidth != nil && *n.MinWidth < contentWidth {
		contentWidth = *n.MinWidth
	}

	// Layout the overlay's children as a vertical column within the content width.
	innerRenderer := r.child(contentWidth, r.Height)
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

	// A row_template turns the list into one instance of the template per
	// element of the array its bind names (Scene 5/9, D1 / BINDS.md §4.7). Each
	// element's fields answer the template's `row.*` binds; the rest of the
	// render path is the ordinary one, so a template row is just a subtree drawn
	// with a row in scope. This precedes the bind-specific rendering below: a
	// list that carries a template asked for the template, whatever its bind.
	if n.RowTemplate != nil {
		return r.renderRowTemplate(n, state, budget)
	}

	// The token for an ordinary, unemphasized row. The list mints one per
	// bind ("text" for a todo, "dim" for a non-selected command) and a token
	// the scene declares replaces it: the rows are content this node draws
	// itself, and a list the author asked to dim used to validate and render
	// unchanged.
	//
	// The *selected* command row is deliberately excluded below. Its
	// brightness is not decoration but the only indication of what Enter will
	// submit — "a menu with no highlight while Enter still acts is a menu
	// that lies" (BINDS.md §4.3) — so a declared token replaces the resting
	// style of the rows and never erases the highlight distinguishing one.
	// That is the same line the container guard draws from the other side: a
	// declaration may replace a default, not a semantic.
	rowToken := styleNameOr(n.Style, "text")

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
			wrapped := ui.WrapSpans([]ui.Span{{Text: taskText, Style: rowToken}}, r.Width, nil)
			lines = append(lines, wrapped...)
		}
		if len(lines) == 0 {
			// The empty state mints "dim" rather than rowToken's "text"
			// default: "no tasks" is the list reporting absence, and sobria
			// de-emphasizes that. A declared token still replaces it, which
			// is what MAXIMUM's Tasks panel now carries to the frame.
			lines = append(lines, ui.Line{ui.Span{Text: "no tasks", Style: styleNameOr(n.Style, "dim")}})
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
			// The resting rows take the declared token when there is one and
			// "dim" when there is not. The selected row stays "text"
			// unconditionally: it answers "what does Enter do", and a scene
			// dimming its menu must still be able to say which row is live.
			style := styleNameOr(n.Style, "dim")
			if i == selected {
				style = "text"
			}
			lines = append(lines, slashRow(m, nameW, r.Width, style))
		}

		if len(lines) == 0 {
			if state.SlashActive {
				lines = append(lines, ui.Line{ui.Span{Text: "no matches", Style: styleNameOr(n.Style, "dim")}})
			}
		}

	default:
		// Unknown bind: render placeholder.
		//
		// The placeholder keeps "dim" whatever the scene declares. It is not
		// the list's content but the engine reporting that it has none — the
		// "[…]" that made nine signed binds look drawn — and letting a scene
		// restyle that would let a document dress up the engine's own
		// admission of a gap.
		lines = append(lines, ui.Line{ui.Span{Text: "[…]", Style: "dim"}})
	}

	return ui.Frame{Live: lines, Width: r.Width, Height: len(lines)}
}

// renderRowTemplate instantiates a list's row_template once per element of the
// array its bind names, resolving each `row.<field>` against that element
// (BINDS.md §4.7). The row scope is set on the Renderer and restored after, so
// a template nested in a template row still resolves its own element and the
// caller's row is not clobbered. An empty array draws nothing, which is the
// signed empty state for the list binds this serves (team.members, agent.todos,
// slash.matches all render zero rows when empty).
func (r *Renderer) renderRowTemplate(n *scene.Node, state fold.State, budget int) ui.Frame {
	saved := r.curRow
	defer func() { r.curRow = saved }()

	var lines []ui.Line
	for _, row := range rowScopesFor(n.Bind, state) {
		r.curRow = row
		frame := r.renderNode(n.RowTemplate, state, budget)
		lines = append(lines, frame.Live...)
	}
	return ui.Frame{Live: lines, Width: r.Width, Height: len(lines)}
}

// rowScopesFor returns one row scope per element of a known array bind, each a
// map from the full `row.<field>` bind string to that element's value. The
// field names are the schemas scene.RowSchemas signs (BINDS.md §4.7); the
// validator refuses a row_template over any other bind at load time, so the
// default here draws nothing rather than guessing a projection.
//
// The keys are the full "row.<field>" strings, not bare fields, so the lookup
// in resolveBindRow is a direct map read on the bind it already holds.
func rowScopesFor(bind string, state fold.State) []map[string]string {
	switch bind {
	case "team.members":
		rows := make([]map[string]string, 0, len(state.TeamMembers))
		for _, m := range state.TeamMembers {
			rows = append(rows, map[string]string{
				"row.id":        m.ID,
				"row.state":     m.State,
				"row.role":      m.Role,
				"row.busy":      boolField(m.Busy),
				"row.turns":     strconv.FormatUint(uint64(m.Turns), 10),
				"row.spent_usd": strconv.FormatFloat(m.SpentUSD, 'f', -1, 64),
			})
		}
		return rows
	case "agent.todos":
		rows := make([]map[string]string, 0, len(state.Todos))
		for _, t := range state.Todos {
			rows = append(rows, map[string]string{
				"row.task":       t.Task,
				"row.blocked_on": t.BlockedOn,
				"row.actor":      t.Actor,
			})
		}
		return rows
	case "slash.matches":
		rows := make([]map[string]string, 0, len(state.SlashMatches))
		for _, m := range state.SlashMatches {
			rows = append(rows, map[string]string{
				"row.name":        m.Name,
				"row.category":    m.Category,
				"row.description": m.Description,
			})
		}
		return rows
	default:
		return nil
	}
}

// boolField renders a bool row field as the truthy/falsy string evalWhenRow
// reads, so `when: "row.busy"` gates a per-row node (Scene 9's spinner).
func boolField(b bool) string {
	if b {
		return "true"
	}
	return "false"
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
// resolveBindRow resolves a bind that may be relative to a template row. A
// `row.<field>` bind reads the current row's field (D1 / BINDS.md §4.7); with
// no row in scope it is the falsy placeholder, exactly like any unresolved
// bind, so a relative bind that leaks outside a template degrades rather than
// crashes. Every other bind is absolute and delegates to resolveBind unchanged.
func resolveBindRow(bind string, state fold.State, row map[string]string) string {
	if strings.HasPrefix(bind, "row.") {
		if row == nil {
			return placeholderValue
		}
		if v, ok := row[bind]; ok {
			return v
		}
		return placeholderValue
	}
	return resolveBind(bind, state)
}

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
// hiddenByWhen reports whether a node declares a `when` the host does not
// satisfy. A node with no `when` is always visible: absence of a gate is not a
// closed gate, and reading it as one would blank every scene in the tree.
//
// It is one function rather than an inlined comparison because the gate is
// asked in two different registers and both must agree. renderNode asks it to
// decide whether to *draw*, and renderStack's measuring pass asks it to decide
// whether to *reserve*. Those are separable — a grow child can be skipped by
// the first and still take its share of the budget from the second, which
// hides the content and keeps the blank rows it occupied — and two spellings
// of the same predicate is how they drift apart.
func hiddenByWhen(n *scene.Node, state fold.State) bool {
	return hiddenByWhenRow(n, state, nil)
}

// hiddenByWhenRow is hiddenByWhen with a template row in scope, so a per-row
// `when: "row.busy"` gates that row's node against its own element (Scene 9).
func hiddenByWhenRow(n *scene.Node, state fold.State, row map[string]string) bool {
	if n == nil {
		return false
	}
	// ui.hidden (BINDS.md §4.3, D3) is the user's manual override, and it
	// composes with the scene's own `when` by conjunction: a node draws only
	// when its gate is truthy AND its id is not in the hidden set — either one
	// removes it, order does not matter. The membership test lives in this
	// shared predicate rather than at the renderNode chokepoint on purpose. A
	// node reached through `prefix` or `suffix` never passes through renderNode
	// (renderMarquee reads those fields straight out of the struct), so a filter
	// placed only at the chokepoint would silently exempt exactly the node
	// positions LESSONS.md records the `when` gate itself falling into three
	// times. hiddenByWhenRow is the one predicate both the draw register
	// (renderNode) and the reserve register (renderStack's measuring pass) ask,
	// so gating here keeps "hidden" a single answer no position can disagree
	// with.
	if n.ID != "" && state.UIHidden[n.ID] {
		return true
	}
	if n.When == "" {
		return false
	}
	return !evalWhenRow(n.When, state, row)
}

// withFocusGlow returns the node the rest of the render path should draw: the
// node itself, or a shallow copy whose style token is the one its focus_glow
// names, when this node is the one `ui.focus` points at.
//
// It is called from renderNode rather than from the twelve sites that resolve
// n.Style, and that placement is the whole design. This engine has produced
// the per-node-type defect twice: `when` was honoured in a row and an overlay
// only, so a gated node drew unconditionally everywhere else, and a style
// token was read by four node types and dropped by the rest. Both times the
// property worked on whatever type the first test reached for. renderNode is
// the one function every node passes through, so honouring the glow here makes
// "some node types glow and others do not" unrepresentable rather than merely
// tested for.
//
// It rewrites the style map rather than passing a token alongside it because
// every downstream site already resolves a token out of n.Style through
// styleName/styleNameOr. Threading a second parameter through twelve call
// sites would mean twelve chances to forget it — the same arithmetic that
// produced the two defects above. A node whose style the glow replaces is
// still just a node with a style.
//
// The copy is shallow and local: the document must not be mutated, because
// the same document renders again on the next repaint with focus possibly
// elsewhere, and a glow written into the tree would be permanent. That is the
// bug shape of the arxi-sim remembered-row: state keyed to the wrong lifetime.
func withFocusGlow(n *scene.Node, state fold.State) *scene.Node {
	if n == nil || n.FocusGlow == nil || n.FocusGlow.Style == "" {
		return n
	}
	// The glow is keyed to id equality with ui.focus. An empty id can never
	// be the focused node: ui.focus is null at boot and BINDS.md's empty
	// state for it is "no node is focused", so matching "" against "" would
	// glow every id-less node in the tree the moment nothing had focus —
	// the inverse of what the property means.
	if n.ID == "" || state.UIFocus != n.ID {
		return n
	}

	glowed := *n
	glowed.Style = make(map[string]string, len(n.Style)+1)
	for k, v := range n.Style {
		glowed.Style[k] = v
	}
	// Written under every key styleName consults, not just the canonical
	// one. styleName returns the first key that is set, so writing only
	// "style" would leave a node that spelled its token "token" wearing its
	// old value — the two-spelling defect PR #4 fixed in the validator,
	// reappearing here because the glow would honour one spelling of a
	// reference the rest of the engine accepts in two.
	for _, key := range scene.StyleTokenKeys() {
		glowed.Style[key] = n.FocusGlow.Style
	}
	return &glowed
}

// transitionDimToken is the intensity a node wears while its entrance
// (transition, G1) is running. It is the theme's dim token — the low end of the
// SGR intensity axis SCENES.md Scene 4 signs the prop onto — named as a constant
// rather than spelled at the write site so the one place the entrance's start
// intensity is decided is greppable. A theme that defines no `dim` resolves it to
// no style (normal), which degrades the entrance to normal→settled rather than
// refusing; the token is injected by the renderer, not written by the scene, so
// it never reaches ValidateTokens.
const transitionDimToken = "dim"

// withTransition returns the node the rest of the render path should draw while
// a transition (G1) is running: a shallow copy dimmed to the theme's dim
// intensity, or the node itself once the entrance has settled. It also reports
// the node active so the host clock keeps ticking through the entrance
// (ADR-0005).
//
// It sits in renderNode beside withFocusGlow, and for the same reason: the SGR
// intensity axis a transition rides is universal, and renderNode is the one
// function every node passes through, so "some node types transition and others
// do not" is unrepresentable rather than merely tested for. It is a method
// rather than the free function withFocusGlow is because it reads two things a
// pure (node, state) transform cannot: the per-node phase the loop computed
// (r.AnimPhase) and the activity accumulator (r.active).
//
// Why dim-while-running, settled-when-done and nothing in between. The intensity
// axis is discrete — the SGR vocabulary this project ships is dim, normal and
// bold — so the two endpoints G-B signs ("0 draws the node dim, 1 draws it in
// its ordinary style") are the whole of it. A finer ramp would need true opacity
// or colour interpolation, which G-B refuses as the fifth axis. This is the axis
// being honest about its own resolution, not a shortcut: reveal's character
// count is continuous and draws a growing prefix, intensity is not and draws two
// states.
func (r *Renderer) withTransition(n *scene.Node) *scene.Node {
	if n == nil || n.Transition == nil {
		return n
	}
	// A transitioning node is an animating node, present or settled: report it
	// active with its one-shot marker and token, the way a reveal reports itself.
	// The clock — not the renderer — decides settled-ness from elapsed against the
	// token's duration and stops forcing ticks once past it, so reporting
	// unconditionally is correct and a settled transition is present but quiet.
	if r.active != nil {
		*r.active = append(*r.active, AnimActivity{
			NodeID:  n.ID,
			OneShot: true,
			Token:   n.Transition.Anim,
		})
	}
	// nil AnimPhase is the pure/golden path with no clock: the node draws settled,
	// the no-op guarantee that keeps every non-motion golden unchanged. A non-nil
	// map missing this id is the first appearance in the live loop — phase 0, the
	// dim start — so the map's nil-ness is checked before it is indexed, exactly as
	// revealPrefix does.
	if r.AnimPhase == nil {
		return n
	}
	if r.AnimPhase[n.ID] >= 1 {
		return n // settled: the node's own style
	}

	// Running: the node wears the theme's dim intensity. The style map is
	// rewritten rather than a token threaded alongside it, for withFocusGlow's
	// reason — every downstream site resolves a token out of n.Style through
	// styleName, so a copy with its own map keeps a transitioning node just a node
	// with a style. It is written under every key styleName consults, not only the
	// canonical one, so a node spelling its token "token" dims too: the
	// two-spelling obligation withFocusGlow documents, at the second place the
	// engine writes a token.
	//
	// The copy is shallow and local: the document must not be mutated, because the
	// same document renders again next repaint with the phase advanced, and a dim
	// token written into the tree would be permanent — the arxi-sim remembered-row
	// shape withFocusGlow's comment names.
	dimmed := *n
	dimmed.Style = make(map[string]string, len(n.Style)+1)
	for k, v := range n.Style {
		dimmed.Style[k] = v
	}
	for _, key := range scene.StyleTokenKeys() {
		dimmed.Style[key] = transitionDimToken
	}
	return &dimmed
}

// renderEnter draws Scene 4's enter (G4): the staggered list entrance. It is the
// scheduler G-B signs — a composition of the intensity axis transition rides and
// the row-count axis a container already draws — not a fifth axis. The node's
// ordinary content is rendered first (renderByType), and enter then decides,
// per row, which already-expressible frame to emit: a row not yet reached is not
// drawn, a row mid-entrance is dimmed, a row past its entrance is settled. The
// clock chooses which of those by time; the renderer only maps a phase to a
// frame, exactly as reveal and transition do.
//
// row:false is the degenerate whole-container entrance and is handled first: it
// is transition applied to the whole rendered subtree rather than to one node's
// own style. transition (G1) deliberately left subtree dimming to enter, because
// a container has no own content to dim; this is where that dimming lives.
func (r *Renderer) renderEnter(n *scene.Node, state fold.State, budget int) ui.Frame {
	if !n.Enter.Row {
		f := r.renderByType(n, state, budget)
		return r.enterWhole(n, f)
	}
	return r.renderEnterRows(n, state, budget)
}

// enterWhole applies the whole-container entrance (row:false) to an
// already-rendered frame: the subtree draws dim while the entrance runs and in
// its settled styles once the phase reaches 1. It is transition's dim→settled
// rule (withTransition) lifted from a node's style token to every span of a
// rendered frame, and it reads the phase the same way — a nil map is the
// pure/golden path (settled, so no golden moves), and a non-nil map with no
// entry is the first live frame (phase 0, dim), the seam withTransition
// documents. The container reports itself active on the default token (Row 0),
// so the loop drives one clock for the whole unit exactly as it does for a
// transition.
func (r *Renderer) enterWhole(n *scene.Node, f ui.Frame) ui.Frame {
	if r.active != nil {
		*r.active = append(*r.active, AnimActivity{NodeID: n.ID, OneShot: true, Token: "", Row: 0})
	}
	if r.AnimPhase == nil {
		return f
	}
	if r.AnimPhase[n.ID] >= 1 {
		return f
	}
	return dimFrame(f)
}

// enterRowKey is the clock key for row i of the enter on container id. The rows
// of one enter each run their own one-shot clock, so each needs a distinct id,
// and it is derived from the container's id the way the clock keys everything
// else — which inherits the container from reveal/transition: a node the author
// gave no id shares the empty key with every other id-less node, an accepted
// limitation the shipped scenes avoid by giving animated nodes ids. The
// separator is a NUL, which no author-written id contains, so the row keys
// cannot collide with an ordinary node id.
func enterRowKey(id string, i int) string {
	return id + "\x00enter\x00" + strconv.Itoa(i)
}

// renderEnterRows staggers a container's rows into the frame (row:true). The
// rows are the children of a container or the instantiations of a row_template,
// each rendered to its own frame and then placed in whatever state its personal
// clock has reached. Every row is reported active regardless of whether it is
// drawn yet, because the clock starts a row's offset countdown from the frame it
// first appears in the report: withholding an undrawn row would stop its own
// arrival from ever being scheduled.
//
// The rows are composed as a plain vertical sequence, which is the row-count
// axis the design names ("the list fills top-to-bottom"). A container's own
// chrome — a box border, an overlay anchor — is not part of that sequence and is
// not redrawn here; a node that needs its frame to enter as a unit uses
// row:false, which does redraw it. This keeps the row:true path a scheduler over
// rows rather than a second layout engine.
func (r *Renderer) renderEnterRows(n *scene.Node, state fold.State, budget int) ui.Frame {
	rows := r.enterRowFrames(n, state, budget)

	var lines []ui.Line
	for i, rf := range rows {
		if r.active != nil {
			*r.active = append(*r.active, AnimActivity{
				NodeID:  enterRowKey(n.ID, i),
				OneShot: true,
				Token:   n.Enter.Stagger,
				Row:     i,
			})
		}
		drawn, dim := r.enterRowState(n.ID, i)
		if !drawn {
			continue
		}
		f := rf
		if dim {
			f = dimFrame(rf)
		}
		lines = append(lines, f.Live...)
		if len(lines) >= budget {
			lines = lines[:budget]
			break
		}
	}
	return ui.Frame{Live: lines, Width: r.Width, Height: len(lines)}
}

// enterRowFrames renders each of an enter container's rows to its own frame, in
// order. A row_template list stands each element of its bound array up as a row
// (the same scopes renderRowTemplate uses); any other container stands each
// visible child up as a row. renderNode is used per row so a row keeps its own
// props — its when gate, its own transition or reveal — under the enter that
// schedules its arrival.
func (r *Renderer) enterRowFrames(n *scene.Node, state fold.State, budget int) []ui.Frame {
	var out []ui.Frame
	if n.RowTemplate != nil {
		saved := r.curRow
		defer func() { r.curRow = saved }()
		for _, row := range rowScopesFor(n.Bind, state) {
			r.curRow = row
			out = append(out, r.renderNode(n.RowTemplate, state, budget))
		}
		return out
	}
	for _, c := range n.Children {
		if hiddenByWhenRow(c, state, r.curRow) {
			continue
		}
		out = append(out, r.renderNode(c, state, budget))
	}
	return out
}

// enterRowState maps row i's clock phase to what the renderer draws: not drawn
// (the row has not reached its offset), dim (mid-entrance), or settled. It reads
// the phase the way reveal and transition read theirs, with one inversion the
// row-count axis forces: a non-nil map with no entry for the row is a row the
// clock has not started, which for a staggered list is a row that has not
// appeared — so it is *not drawn*, where the same absent entry means "dim start"
// for a transition that is already present. A nil map is still the pure/golden
// path, where every row draws settled so no golden moves.
func (r *Renderer) enterRowState(id string, i int) (drawn, dim bool) {
	if r.AnimPhase == nil {
		return true, false
	}
	phase, ok := r.AnimPhase[enterRowKey(id, i)]
	if !ok {
		return false, false
	}
	if phase >= 1 {
		return true, false
	}
	return true, true
}

// dimFrame returns a copy of a frame with every span drawn at the theme's dim
// intensity — the frame-level analogue of withTransition rewriting a node's
// style token. It is how enter dims a whole subtree (row:false) or a whole row
// (row:true) as one unit: the intensity axis is discrete (dim / settled), so a
// running entrance is the dim frame and a settled one is the frame untouched,
// with no intermediate. Fill is left alone — a wash is not intensity — and the
// copy is deep enough that the original spans (drawn again next repaint with the
// phase advanced) are never mutated.
func dimFrame(f ui.Frame) ui.Frame {
	out := f
	out.Live = make([]ui.Line, len(f.Live))
	for i, line := range f.Live {
		nl := make(ui.Line, len(line))
		for j, sp := range line {
			sp.Style = transitionDimToken
			nl[j] = sp
		}
		out.Live[i] = nl
	}
	return out
}

func evalWhen(bind string, state fold.State) bool {
	return evalWhenRow(bind, state, nil)
}

// evalWhenRow is evalWhen with a template row in scope; see resolveBindRow.
func evalWhenRow(bind string, state fold.State, row map[string]string) bool {
	val := resolveBindRow(bind, state, row)
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

// styleNameOr is styleName with the token the renderer would otherwise mint,
// and it encodes which of the two wins.
//
// A node that declares a token has said what it wants drawn; a token the
// renderer chose (a list's `dim` empty state, markdown's `text`, the rule's
// `rule`) is a default for the case where the author said nothing. Four
// content types used to ignore the declaration and emit the minted token
// unconditionally, which validated clean — `style` is a universal property in
// SCENES.md and ValidateTokens checks it on every node type — and drew an
// unchanged screen. That is the failure this project has now hit five times:
// a construction the validator accepts and the engine does not read, silent
// precisely because clearing validation is the signal that says the document
// is fine.
//
// The fallback is not cosmetic. Passing "" where a token is minted today would
// strip the styling off every scene that declares nothing, which is all three
// shipped goldens, and invariant 1 says the factory scene draws byte-identical
// frames. Declaring a token replaces the default; declaring none keeps it.
func styleNameOr(style map[string]string, minted string) string {
	if name := styleName(style); name != "" {
		return name
	}
	return minted
}
