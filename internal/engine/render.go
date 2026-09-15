package engine

import (
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

// Renderer turns a scene into a frame of cells. The render phase is pure: it needs
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
		// Phase 0: a box lays out like a stack; border and title arrive with
		// style tokens, not before.
		return r.renderStack(n, state, budget)
	case "markdown":
		return r.renderMarkdown(n, state, budget)
	case "input":
		return r.renderInput(n, state)
	case "text":
		return r.renderText(n)
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
	return ui.Frame{Live: []ui.Line{cells}, Width: r.Width, Height: 1}
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
	span := ui.Span{Text: promptText, Style: "input"}
	return ui.Frame{
		Live:   []ui.Line{{span}},
		Width:  r.Width,
		Height: 1,
	}
}

func (r *Renderer) renderText(n *scene.Node) ui.Frame {
	span := ui.Span{Text: n.Text, Style: "text"}
	return ui.Frame{
		Live:   []ui.Line{{span}},
		Width:  r.Width,
		Height: 1,
	}
}
