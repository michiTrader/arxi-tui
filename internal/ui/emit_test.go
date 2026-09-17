package ui

import (
	"strings"
	"testing"
)

// mockTheme is a minimal theme for testing the emit path.
type mockTheme struct {
	defs map[string]Style
}

func (m *mockTheme) Resolve(token string) Style {
	if m.defs == nil {
		return Style{}
	}
	return m.defs[token]
}

func TestANSI_WithoutTheme(t *testing.T) {
	f := Frame{
		Live: []Line{
			{{Text: "hello", Style: "text"}},
		},
	}
	got := f.ANSI(nil)
	want := "hello"
	if got != want {
		t.Errorf("ANSI(nil): got %q, want %q", got, want)
	}
}

func TestANSI_WithForegroundColor(t *testing.T) {
	theme := &mockTheme{
		defs: map[string]Style{
			"keyword": {FG: Idx(4)}, // blue
		},
	}
	f := Frame{
		Live: []Line{
			{{Text: "func", Style: "keyword"}},
		},
	}
	got := f.ANSI(theme)
	// Index 4 (blue) in the first 8 colors: SGR 34
	if !strings.Contains(got, "\033[34m") {
		t.Errorf("ANSI(): expected \\033[34m (blue), got %q", got)
	}
	if !strings.Contains(got, "func") {
		t.Errorf("ANSI(): text missing, got %q", got)
	}
	if !strings.Contains(got, "\033[0m") {
		t.Errorf("ANSI(): expected reset \\033[0m, got %q", got)
	}
}

func TestANSI_WithBackgroundColor(t *testing.T) {
	theme := &mockTheme{
		defs: map[string]Style{
			"error": {BG: Idx(1)}, // red background
		},
	}
	f := Frame{
		Live: []Line{
			{{Text: "FAIL", Style: "error"}},
		},
	}
	got := f.ANSI(theme)
	// Index 1 (red) background in first 8: SGR 41
	if !strings.Contains(got, "\033[41m") {
		t.Errorf("ANSI(): expected \\033[41m (red bg), got %q", got)
	}
}

func TestANSI_WithAttributes(t *testing.T) {
	theme := &mockTheme{
		defs: map[string]Style{
			"bold": {Attrs: AttrBold},
			"dim":  {Attrs: AttrDim},
		},
	}
	f := Frame{
		Live: []Line{
			{{Text: "strong", Style: "bold"}, {Text: " ", Style: ""}, {Text: "weak", Style: "dim"}},
		},
	}
	got := f.ANSI(theme)
	if !strings.Contains(got, "\033[1m") {
		t.Errorf("ANSI(): expected \\033[1m (bold), got %q", got)
	}
	if !strings.Contains(got, "\033[2m") {
		t.Errorf("ANSI(): expected \\033[2m (dim), got %q", got)
	}
}

func TestANSI_StyleOverFill(t *testing.T) {
	theme := &mockTheme{
		defs: map[string]Style{
			"band":    {BG: Idx(4)}, // blue background
			"comment": {FG: Idx(2)}, // green foreground
			"text":    {FG: Idx(7)}, // white foreground
		},
	}
	f := Frame{
		Live: []Line{
			{{Text: "// note", Style: "comment", Fill: "band"}},
		},
	}
	got := f.ANSI(theme)
	// Should have both green FG (32) and blue BG (44)
	if !strings.Contains(got, "32") {
		t.Errorf("ANSI(): expected green FG (32), got %q", got)
	}
	if !strings.Contains(got, "44") {
		t.Errorf("ANSI(): expected blue BG (44), got %q", got)
	}
}

func TestANSI_RGB(t *testing.T) {
	theme := &mockTheme{
		defs: map[string]Style{
			"accent": {FG: Color{Kind: ColorRGB, R: 255, G: 100, B: 50}},
		},
	}
	f := Frame{
		Live: []Line{
			{{Text: "orange", Style: "accent"}},
		},
	}
	got := f.ANSI(theme)
	// RGB FG: \033[38;2;R;G;Bm
	if !strings.Contains(got, "38;2;255;100;50") {
		t.Errorf("ANSI(): expected RGB sequence 38;2;255;100;50, got %q", got)
	}
}

func TestANSI_MultipleLines(t *testing.T) {
	theme := &mockTheme{
		defs: map[string]Style{
			"text": {FG: Idx(7)},
		},
	}
	f := Frame{
		Live: []Line{
			{{Text: "first", Style: "text"}},
			{{Text: "second", Style: "text"}},
		},
	}
	got := f.ANSI(theme)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Errorf("ANSI(): expected 2 lines, got %d: %q", len(lines), got)
	}
}

func TestANSI_UndefinedToken(t *testing.T) {
	// A token the theme does not define should render as unstyled text.
	theme := &mockTheme{
		defs: map[string]Style{},
	}
	f := Frame{
		Live: []Line{
			{{Text: "plain", Style: "undefined"}},
		},
	}
	got := f.ANSI(theme)
	// Should not contain any escape codes, just the text
	if strings.Contains(got, "\033[") {
		t.Errorf("ANSI(): undefined token should produce no escapes, got %q", got)
	}
	if got != "plain" {
		t.Errorf("ANSI(): got %q, want %q", got, "plain")
	}
}

func TestStyleToSGR_ZeroStyle(t *testing.T) {
	s := Style{}
	got := styleToSGR(s)
	if got != "" {
		t.Errorf("styleToSGR(zero): got %q, want empty string", got)
	}
}

func TestStyleToSGR_256Color(t *testing.T) {
	// Index >= 16 uses the 256-color palette
	s := Style{FG: Idx(196)} // bright red in 256-color
	got := styleToSGR(s)
	if !strings.Contains(got, "38;5;196") {
		t.Errorf("styleToSGR(256-color): expected 38;5;196, got %q", got)
	}
}
