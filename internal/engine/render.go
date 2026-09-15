package engine

import (
	"github.com/michiTrader/arxi_tui/internal/ui"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// Renderer turns a scene into a frame of cells. The render phase is pure: it needs
// no I/O, no clock, and the fold never waits on it. A frame is a snapshot of what
// the scene said to look like, not what happened to it.
type Renderer struct {
	// Width and Height are the terminal dimensions. They come from the host,
	// not the scene.
	Width  int
	Height int
}

// RenderFrame renders one scene document into one frame. The frame is a single
// picture, not an animation — that is the contract.
func (r *Renderer) RenderFrame(doc *scene.Document) ui.Frame {
	if doc == nil || doc.Root == nil {
		return ui.Frame{}
	}
	return r.renderNode(doc.Root, 0)
}

func (r *Renderer) renderNode(n *scene.Node, depth int) ui.Frame {
	switch n.Type {
	case "stack":
		return r.renderStack(n)
	case "row":
		return r.renderRow(n)
	case "box":
		return r.renderBox(n)
	case "markdown":
		return r.renderMarkdown(n)
	case "input":
		return r.renderInput(n)
	case "text":
		return r.renderText(n)
	default:
		// Unknown node types render as placeholder; the golden test catches this.
		return ui.Frame{
			Live: []ui.Line{
				{{Text: "[[UNKNOWN NODE TYPE]]", Style: "error"}},
			},
			Width: r.Width,
		}
	}
}

// renderStack lays children out vertically, one after another.
func (r *Renderer) renderStack(n *scene.Node) ui.Frame {
	var live []ui.Line
	for _, child := range n.Children {
		f := r.renderNode(child, len(live))
		for _, l := range f.Live {
			if len(live) < r.Height {
				live = append(live, l)
			}
		}
	}
	return ui.Frame{Live: live, Width: r.Width, Height: r.Height}
}

// renderRow lays children out horizontally.
func (r *Renderer) renderRow(n *scene.Node) ui.Frame {
	// Row: horizontal composition using weight/grow for proportion.
	// For Phase 0, we just concatenate.
	var cells []ui.Span
	for _, child := range n.Children {
		f := r.renderNode(child, 0)
		for _, l := range f.Live {
			for _, s := range l {
				cells = append(cells, s)
			}
		}
	}
	return ui.Frame{Live: []ui.Line{cells}, Width: r.Width, Height: 1}
}

// renderBox is a container with optional border.
func (r *Renderer) renderBox(n *scene.Node) ui.Frame {
	// Phase 0: box is a simple container; border/title come later.
	f := r.renderStack(n)
	return f
}

// renderMarkdown turns a markdown node into lines of spans.
func (r *Renderer) renderMarkdown(n *scene.Node) ui.Frame {
	// Phase 0: plain text, no styling. Markdown parser comes later.
	txt := n.Text
	lines := []ui.Line{}
	for _, l := range splitLines(txt) {
		lines = append(lines, ui.Line{{Text: l, Style: "text"}})
	}
	return ui.Frame{Live: lines, Width: r.Width, Height: len(lines)}
}

// renderInput renders an input node as its prompt + placeholder line.
func (r *Renderer) renderInput(n *scene.Node) ui.Frame {
	prefix := ""
	if n.Prefix != nil {
		prefix = n.Prefix.Text
	}
	ph := n.Placeholder
	txt := prefix + ph
	return ui.Frame{
		Live:   []ui.Line{{{Text: txt, Style: "input"}}},
		Width:  r.Width,
		Height: 1,
	}
}

// renderText renders a plain text node.
func (r *Renderer) renderText(n *scene.Node) ui.Frame {
	txt := n.Text
	style := "text"
	for k := range n.Style {
		style = k // First style wins for Phase 0
		break
	}
	return ui.Frame{
		Live:   []ui.Line{{{Text: txt, Style: style}}},
		Width:  r.Width,
		Height: 1,
	}
}

// splitLines splits s on \n.
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