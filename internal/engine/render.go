package engine

import (
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/ui"
	"github.com/michiTrader/arxi_tui/internal/scene"
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
	return r.renderNode(doc.Root, state)
}

func (r *Renderer) renderNode(n *scene.Node, state fold.State) ui.Frame {
	switch n.Type {
	case "stack":
		return r.renderStack(n, state)
	case "row":
		return r.renderRow(n, state)
	case "box":
		return r.renderStack(n, state)
	case "markdown":
		return r.renderMarkdown(n, state)
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

func (r *Renderer) renderStack(n *scene.Node, state fold.State) ui.Frame {
	var live []ui.Line
	for _, child := range n.Children {
		f := r.renderNode(child, state)
		for _, l := range f.Live {
			if len(live) < r.Height {
				live = append(live, l)
			}
		}
	}
	return ui.Frame{Live: live, Width: r.Width, Height: r.Height}
}

func (r *Renderer) renderRow(n *scene.Node, state fold.State) ui.Frame {
	var cells []ui.Span
	for _, child := range n.Children {
		f := r.renderNode(child, state)
		for _, l := range f.Live {
			cells = append(cells, l...)
		}
	}
	return ui.Frame{Live: []ui.Line{cells}, Width: r.Width, Height: 1}
}

func (r *Renderer) renderMarkdown(n *scene.Node, state fold.State) ui.Frame {
	switch n.Bind {
	case "chat.history":
		txt := state.ChatHistoryMarkdown()
		return ui.Frame{Live: splitToLines(txt), Width: r.Width}
	case "thinking.text":
		return ui.Frame{Live: splitToLines(state.ThinkingText), Width: r.Width}
	default:
		return ui.Frame{Live: splitToLines(n.Text), Width: r.Width}
	}
}

func (r *Renderer) renderInput(n *scene.Node, state fold.State) ui.Frame {
	var promptText string
	switch n.Bind {
	case "user.input":
		if state.UserInput == "" {
			promptText = n.Placeholder // placeholder is the prompt
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

func splitToLines(s string) []ui.Line {
	if s == "" {
		return nil
	}
	lines := make([]ui.Line, 0)
	for _, l := range splitLines(s) {
		span := ui.Span{Text: l, Style: "text"}
		lines = append(lines, ui.Line{span})
	}
	return lines
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}