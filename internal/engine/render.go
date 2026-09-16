package engine

import (
	"fmt"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

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
		return r.renderRow(n, state)
	case "box":
		return r.renderStack(n, state, budget)
	case "markdown":
		return r.renderMarkdown(n, state, budget)
	case "input":
		return r.renderInput(n, state)
	case "text":
		return r.renderText(n, state)
	case "rule":
		return r.renderRule(n)
	case "marquee":
		return r.renderMarquee(n, state, budget)
	case "overlay":
		return r.renderOverlay(n, state)
	case "list":
		return r.renderList(n, state, budget)
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
func (r *Renderer) renderStack(n *scene.Node, state fold.State, budget int) ui.Frame {
	type slot struct {
		frame ui.Frame
		grow  int
	}
	slots := make([]slot, 0, len(n.Children))
	fixed := 0
	growSum := 0
	for _, c := range n.Children {
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
	return ui.Frame{Live: live, Width: r.Width, Height: budget}
}

// renderRow lays children out horizontally. Phase 0 concatenates the cells of
// every child into one line; weight-based division of the width arrives with
// the first scene that needs it.
func (r *Renderer) renderRow(n *scene.Node, state fold.State) ui.Frame {
	var cells []ui.Span
	for _, child := range n.Children {
		f := r.renderNode(child, state, 1)
		for _, l := range f.Live {
			cells = append(cells, l...)
		}
	}
	style := styleName(n.Style)
	line := ui.Line{ui.Span{Text: "", Fill: style}}
	for _, c := range cells {
		c.Fill = style
		line = append(line, c)
	}
	return ui.Frame{Live: []ui.Line{line}, Width: r.Width, Height: 1}
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
func (r *Renderer) renderRule(n *scene.Node) ui.Frame {
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
		if prefix.Type == "text" {
			cells = append(cells, ui.Span{Text: prefix.Text, Style: styleName(prefix.Style)})
		} else if prefix.Bind != "" {
			cells = append(cells, ui.Span{Text: resolveBind(prefix.Bind, state), Style: styleName(prefix.Style)})
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
// reserves space for it: the stack above subtracts the overlay's height from
// the budget before dividing space among growers.
func (r *Renderer) renderOverlay(n *scene.Node, state fold.State) ui.Frame {
	// An overlay with `when` only renders when that bind is truthy.
	if n.When != "" {
		if !evalWhen(n.When, state) {
			return ui.Frame{Width: r.Width, Height: 0}
		}
	}

	// Layout the overlay's children as a column.
	var lines []ui.Line
	for _, child := range n.Children {
		f := r.renderNode(child, state, r.Height)
		lines = append(lines, f.Live...)
	}
	return ui.Frame{Live: lines, Width: r.Width, Height: len(lines)}
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
