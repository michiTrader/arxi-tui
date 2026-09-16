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
	type slot struct {
		frame ui.Frame
		grow  int
	}
	slots := make([]slot, 0, len(n.Children))
	fixed := 0
	growSum := 0
	var overlays []*scene.Node
	for _, c := range n.Children {
		if c.Type == "overlay" {
			overlays = append(overlays, c)
			continue
		}
		grow := 0
		if c.Grow != nil {
			grow = *c.Grow
		}
		f := r.renderNode(c, state, budget)
		slots = append(slots, slot{frame: f, grow: grow})
		if grow > 0 {
			growSum += grow
		} else {
			fixed += len(f.Live)
		}
	}
	remaining := budget - fixed

	live := make([]ui.Line, 0, budget)
	for _, s := range slots {
		if s.grow == 0 {
			live = append(live, s.frame.Live...)
			continue
		}
		share := 0
		if remaining > 0 && growSum > 0 {
			share = remaining * s.grow / growSum
		}
		content := s.frame.Live
		if len(content) > share {
			// The tail, not the head: new transcript lines arrive at the
			// bottom, directly above the input. Dropping the tail would hide
			// exactly the lines the user is waiting for.
			content = content[len(content)-share:]
		}
		live = append(live, content...)
		for i := len(content); i < share; i++ {
			live = append(live, ui.Line{})
		}
	}

	// Pad the stack to budget height with blank lines, then overlay any
	// overlay children on top. Overlays are rendered at their anchor position
	// and their lines replace the underlying content where they render.
	for len(live) < budget {
		live = append(live, ui.Line{})
	}
	if len(live) > budget {
		live = live[:budget]
	}

	for _, ov := range overlays {
		f := r.renderOverlay(ov, state)
		if len(f.Live) == 0 {
			continue
		}
		// Overlays with anchor "bottom" sit at the bottom, replacing the
		// last N lines. Overlays with "full" replace everything. Others
		// are positioned by anchorOverlay into the right place.
		lines := f.Live
		if len(lines) >= len(live) {
			// Overlay fills the screen: replace everything.
			live = lines
		} else if len(lines) < len(live) {
			// Place overlay content at the end (bottom anchor) or as
			// returned by anchorOverlay (already positioned with padding).
			// For bottom anchor, replace the tail lines.
			start := len(live) - len(lines)
			for i, l := range lines {
				if start+i < len(live) {
					live[start+i] = l
				}
			}
		}
	}

	return ui.Frame{Live: live, Width: r.Width, Height: len(live)}
}

// renderHorizontal lays children out horizontally. Weight-based columns divide
// the available width in proportion to their weight values (Scene 3/10 layout).
func (r *Renderer) renderHorizontal(n *scene.Node, state fold.State, budget int) ui.Frame {
	if len(n.Children) == 0 {
		return ui.Frame{Live: []ui.Line{ui.Line{}}, Width: r.Width, Height: 1}
	}

	// Measure each child: how many columns it naturally wants.
	children := n.Children
	widths := make([]int, len(children))
	frames := make([]ui.Frame, len(children))
	totalWeight := 0
	for i, child := range children {
		f := r.renderNode(child, state, 1)
		frames[i] = f
		w := 0
		for _, l := range f.Live {
			w += l.Width()
		}
		widths[i] = w
		wt := 1
		if child.Weight != nil {
			wt = *child.Weight
		}
		totalWeight += wt
	}

	totalWidth := r.Width
	if totalWidth <= 0 {
		totalWidth = 80
	}

	// Allocate width: if the natural widths fit, use them; otherwise distribute
	// proportionally by weight, giving each child at least its natural width
	// if possible.
	natural := 0
	for _, w := range widths {
		natural += w
	}

	var colWidths []int
	if natural <= totalWidth {
		colWidths = widths
	} else {
		// Distribute by weight.
		allocated := 0
		colWidths = make([]int, len(children))
		for i := range widths {
			wt := 1
			if children[i].Weight != nil {
				wt = *children[i].Weight
			}
			share := totalWidth * wt / totalWeight
			allocated += share
			colWidths[i] = share
		}
		// Give the remainder to the first child.
		if rem := totalWidth - allocated; rem > 0 {
			colWidths[0] += rem
		}
	}

	// Build a single line from the column frames, truncating/padding as needed.
	style := styleName(n.Style)
	line := ui.Line{ui.Span{Text: "", Fill: style}}
	for i, f := range frames {
		col := colWidths[i]
		for _, l := range f.Live {
			// Truncate or pad each line to the column width.
			text := l.Text()
			textW := ansiStringWidth(text)
			if textW > col {
				text = truncateText(text, col)
				textW = col
			}
			line = append(line, ui.Span{Text: text, Style: style})
			for j := 0; j < col-textW; j++ {
				line = append(line, ui.Span{Text: " ", Style: style})
			}
		}
	}

	// Pad to full width.
	curW := 0
	for _, s := range line {
		curW += ansiStringWidth(s.Text)
	}
	for curW < totalWidth {
		line = append(line, ui.Span{Text: " ", Style: style})
		curW++
	}

	return ui.Frame{Live: []ui.Line{line}, Width: totalWidth, Height: 1}
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

// renderInput renders the input row: one line, always. The placeholder is the
// prompt spelling the scene chose; the typed buffer rides after it.
func (r *Renderer) renderInput(n *scene.Node, state fold.State) ui.Frame {
	var promptText string
	switch n.Bind {
	case "user.input":
		if state.UserInput == "" {
			promptText = n.Placeholder
		} else {
			promptText = n.Placeholder + state.UserInput
		}
	default:
		promptText = n.Placeholder
	}

	// sobria: the input may carry a string prefix (e.g. "┃ ") rendered as
	// styled leading cells before the prompt text.
	var cells []ui.Span
	if prefix := n.PrefixText(); prefix != "" {
		cells = append(cells, ui.Span{Text: prefix, Style: "input"})
	}

	cells = append(cells, ui.Span{Text: promptText, Style: "input"})

	return ui.Frame{
		Live:   []ui.Line{cells},
		Width:  r.Width,
		Height: 1,
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

// renderBox draws a bordered container with an optional title, then its
// children inside. The border style is configurable: "single" uses light
// box-drawing, "double" uses double box-drawing, "ascii" uses ASCII +/-/|
// (Scene 3 — the maximum dashboard, Scene 5 — config screen).
func (r *Renderer) renderBox(n *scene.Node, state fold.State, budget int) ui.Frame {
	if n.Border == "" {
		// A box with no border is just a stack.
		return r.renderStack(n, state, budget)
	}

	var tl, tr, bl, br, horiz, vert rune
	switch n.Border {
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
		ui.Span{Text: topText, Style: "border"},
	})

	// Inner content: render children as a stack within the inner width/height.
	innerRenderer := Renderer{Width: innerWidth, Height: innerHeight}
	var content ui.Frame
	if len(n.Children) > 0 {
		content = innerRenderer.renderStack(&scene.Node{
			Type:     "stack",
			Children: n.Children,
		}, state, innerHeight)
	} else {
		content = ui.Frame{Live: []ui.Line{ui.Line{}}, Width: innerWidth, Height: innerHeight}
	}

	for _, l := range content.Live {
		// Pad each content line to innerWidth, then wrap in border.
		text := l.Text()
		if w := ansiStringWidth(text); w < innerWidth {
			text += strings.Repeat(" ", innerWidth-w)
		} else if w > innerWidth {
			text = truncateText(text, innerWidth)
		}
		lines = append(lines, ui.Line{
			ui.Span{Text: string(vert), Style: "border"},
			ui.Span{Text: text, Style: styleName(n.Style)},
		})
	}
	// Pad remaining rows if content was shorter than innerHeight.
	for len(lines) < 1+innerHeight {
		pad := strings.Repeat(" ", innerWidth)
		lines = append(lines, ui.Line{
			ui.Span{Text: string(vert), Style: "border"},
			ui.Span{Text: pad, Style: styleName(n.Style)},
		})
	}

	// Bottom border.
	bottomText := string(bl) + strings.Repeat(string(horiz), innerWidth) + string(br)
	lines = append(lines, ui.Line{
		ui.Span{Text: bottomText, Style: "border"},
	})

	return ui.Frame{Live: lines, Width: width, Height: len(lines)}
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

	// If the overlay has a border, wrap the content in one.
	if n.Border != "" {
		lines = r.wrapWithBorder(lines, n, contentWidth)
	}

	// Pad or truncate each line to the content width, and resolve fill styles.
	out := make([]ui.Line, len(lines))
	for i, l := range lines {
		text := l.Text()
		w := ansiStringWidth(text)
		if w < contentWidth {
			pad := strings.Repeat(" ", contentWidth-w)
			// Find the fill style from the first span.
			fill := ""
			if len(l) > 0 {
				fill = l[0].Fill
			}
			out[i] = ui.Line{ui.Span{Text: text + pad, Fill: fill}}
		} else if w > contentWidth {
			out[i] = ui.Line{ui.Span{Text: truncateText(text, contentWidth), Fill: l[0].Fill}}
		} else {
			out[i] = l
		}
	}

	return ui.Frame{Live: out, Width: contentWidth, Height: len(out)}
}

// wrapWithBorder wraps overlay content lines in a border, returning the full
// bordered lines.
func (r *Renderer) wrapWithBorder(lines []ui.Line, n *scene.Node, contentWidth int) []ui.Line {
	var tl, tr, bl, br, horiz, vert rune
	switch n.Border {
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

	width := contentWidth
	if width <= 0 {
		width = 80
	}
	innerWidth := width - 2
	if innerWidth < 0 {
		innerWidth = 0
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
	bordered = append(bordered, ui.Line{ui.Span{Text: topText, Style: "border"}})

	// Content lines wrapped with vertical bars.
	for _, l := range lines {
		text := l.Text()
		if w := ansiStringWidth(text); w < innerWidth {
			text += strings.Repeat(" ", innerWidth-w)
		} else if w > innerWidth {
			text = truncateText(text, innerWidth)
		}
		bordered = append(bordered, ui.Line{
			ui.Span{Text: string(vert), Style: "border"},
			ui.Span{Text: text, Style: styleName(n.Style)},
		})
	}

	// Bottom border.
	bottomText := string(bl) + strings.Repeat(string(horiz), innerWidth) + string(br)
	bordered = append(bordered, ui.Line{ui.Span{Text: bottomText, Style: "border"}})

	return bordered
}

// renderList renders a filterable list of slash command matches. The bind is
// slash.matches (derived from the host's command registry, filtered by the
// typed substring). The list supports count header and category tabs.
func (r *Renderer) renderList(n *scene.Node, state fold.State, budget int) ui.Frame {
	// slash.matches is not a simple string — it is an array of SlashMatch.
	// The renderer walks state.SlashMatches directly.
	matches := state.SlashMatches
	if n.Bind == "slash.matches" && len(matches) == 0 && state.SlashTyped != "" {
		// No matches for the current filter.
		matches = nil
	}

	var lines []ui.Line

	// Count header if requested.
	if n.Count && len(matches) > 0 {
		countText := fmt.Sprintf("%d commands", len(matches))
		lines = append(lines, ui.Line{ui.Span{Text: countText, Style: "dim"}})
	}

	// Render each match as a row. Without a row_template, the default layout is:
	//   <name>  <category>  <description>
	for _, m := range matches {
		name := m.Name
		desc := m.Description
		// Format: "name   description" with padding.
		descText := fmt.Sprintf("%s  %s", name, desc)
		wrapped := ui.WrapSpans([]ui.Span{{Text: descText, Style: "text"}}, r.Width, nil)
		lines = append(lines, wrapped...)
	}

	if len(lines) == 0 {
		// No matches: show a dim placeholder line.
		if state.SlashActive {
			lines = append(lines, ui.Line{ui.Span{Text: "no matches", Style: "dim"}})
		}
	}

	return ui.Frame{Live: lines, Width: r.Width, Height: len(lines)}
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
	case "host.escape.armed":
		if state.EscapeArmed {
			return "true"
		}
		return "false"
	case "host.scene.error":
		return state.SceneError
	default:
		// An unsatisfied bind renders as a placeholder, never a crash —
		// the engine contract that makes community preview (Q16) and forward
		// compatibility possible at the same time (ADR-0003).
		return "[…]"
	}
}

// evalWhen evaluates a `when` bind string as a boolean predicate. A non-empty
// string value is truthy; "false", "0", and "" are falsy.
func evalWhen(bind string, state fold.State) bool {
	val := resolveBind(bind, state)
	switch val {
	case "", "0", "false":
		return false
	default:
		return true
	}
}

// styleName extracts the style token from a node's style map, or returns ""
// if unset. Scene nodes carry styles as a map (e.g. {"style": "dim"}).
func styleName(style map[string]string) string {
	if style == nil {
		return ""
	}
	return style["style"]
}
