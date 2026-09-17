package ui

import (
	"fmt"
	"strings"
)

// ThemeResolver resolves token names to styles. This is the minimal interface
// an emitter needs from a theme, so the ui package never imports theme directly.
type ThemeResolver interface {
	// Resolve returns the style for a token name. A missing token returns a zero
	// style (no color, no attributes) rather than an error, so the frame always
	// renders even when the theme is incomplete.
	Resolve(token string) Style
}

// ANSI renders the frame as styled text using ANSI escape codes. Token names in
// Span.Style and Span.Fill are resolved through the theme, and the resulting
// styles are merged (Style over Fill) per span.
//
// This is the emit path: the only place that knows tokens become colours and
// colours become escape codes. The renderer produces frames with token names,
// and this function turns them into terminal sequences.
func (f Frame) ANSI(theme ThemeResolver) string {
	if theme == nil {
		// No theme: fall back to Plain(). This path exists so callers that
		// have not yet wired a theme still render something.
		return f.Plain()
	}

	var b strings.Builder
	for _, l := range f.Committed {
		emitLine(&b, l, theme)
		b.WriteByte('\n')
	}
	for i, l := range f.Live {
		if i > 0 {
			b.WriteByte('\n')
		}
		emitLine(&b, l, theme)
	}
	return b.String()
}

// emitLine writes one line as ANSI-styled text. Each span's Style token is
// resolved, layered over its Fill token (if present), and emitted as SGR codes.
func emitLine(b *strings.Builder, l Line, theme ThemeResolver) {
	for _, s := range l {
		style := theme.Resolve(s.Style)
		if s.Fill != "" {
			fill := theme.Resolve(s.Fill)
			style = style.Over(fill)
		}
		if !style.IsZero() {
			b.WriteString(styleToSGR(style))
		}
		b.WriteString(s.Text)
		if !style.IsZero() {
			b.WriteString("\033[0m") // reset after styled span
		}
	}
}

// styleToSGR converts a Style into an ANSI SGR (Select Graphic Rendition) escape
// sequence. This is where ui.Color and ui.Attr become terminal codes.
func styleToSGR(s Style) string {
	var codes []string

	// Attributes first (bold, dim, italic, etc.)
	if s.Has(AttrBold) {
		codes = append(codes, "1")
	}
	if s.Has(AttrDim) {
		codes = append(codes, "2")
	}
	if s.Has(AttrItalic) {
		codes = append(codes, "3")
	}
	if s.Has(AttrUnderline) {
		codes = append(codes, "4")
	}
	if s.Has(AttrReverse) {
		codes = append(codes, "7")
	}
	if s.Has(AttrStrike) {
		codes = append(codes, "9")
	}

	// Foreground color
	if s.FG.Kind == ColorIndex {
		if s.FG.R < 8 {
			codes = append(codes, fmt.Sprintf("%d", 30+s.FG.R))
		} else if s.FG.R < 16 {
			codes = append(codes, fmt.Sprintf("%d", 82+s.FG.R))
		} else {
			codes = append(codes, fmt.Sprintf("38;5;%d", s.FG.R))
		}
	} else if s.FG.Kind == ColorRGB {
		codes = append(codes, fmt.Sprintf("38;2;%d;%d;%d", s.FG.R, s.FG.G, s.FG.B))
	}

	// Background color
	if s.BG.Kind == ColorIndex {
		if s.BG.R < 8 {
			codes = append(codes, fmt.Sprintf("%d", 40+s.BG.R))
		} else if s.BG.R < 16 {
			codes = append(codes, fmt.Sprintf("%d", 92+s.BG.R))
		} else {
			codes = append(codes, fmt.Sprintf("48;5;%d", s.BG.R))
		}
	} else if s.BG.Kind == ColorRGB {
		codes = append(codes, fmt.Sprintf("48;2;%d;%d;%d", s.BG.R, s.BG.G, s.BG.B))
	}

	if len(codes) == 0 {
		return ""
	}
	return "\033[" + strings.Join(codes, ";") + "m"
}
