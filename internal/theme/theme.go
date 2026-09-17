// Package theme is the token resolver: it reads theme files (JSON maps of token
// names to style definitions) and resolves token names to ui.Style at emit time.
// The namespace is open by design — users and plugins may mint tokens — and the
// validator checks references, not inventory.
package theme

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/michiTrader/arxi_tui/internal/ui"
)

// Theme is a map of token names to styles. The zero Theme is valid and resolves
// every token to the zero style (no color, no attributes).
type Theme struct {
	tokens map[string]ui.Style
}

// tokenDef is the JSON representation of a style definition in a theme file.
type tokenDef struct {
	FG    string   `json:"fg,omitempty"`
	BG    string   `json:"bg,omitempty"`
	Attrs []string `json:"attrs,omitempty"`
}

// Load reads a theme from a JSON file. The file must be a JSON object mapping
// token names to style definitions. Each definition has optional fg, bg, and
// attrs fields. Unknown fields are ignored for forward compatibility.
//
// An invalid file returns an error with file:line if possible.
func Load(path string) (*Theme, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	var raw map[string]tokenDef
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	tokens := make(map[string]ui.Style, len(raw))
	for name, def := range raw {
		style, err := parseTokenDef(def)
		if err != nil {
			return nil, fmt.Errorf("%s: token %q: %w", path, name, err)
		}
		tokens[name] = style
	}

	return &Theme{tokens: tokens}, nil
}

// FromMap creates a Theme from an in-memory map of token names to styles. This
// is how the factory theme is compiled in, and how plugin tokens are merged.
func FromMap(tokens map[string]ui.Style) *Theme {
	return &Theme{tokens: tokens}
}

// Resolve looks up a token name and returns its style. If the token is not
// defined, it returns the zero style (no color, no attributes). The resolver
// never fails: an undefined token is not an error at resolution time, because
// the validator already checked that every referenced token exists.
func (t *Theme) Resolve(name string) ui.Style {
	if t == nil || t.tokens == nil {
		return ui.Style{}
	}
	return t.tokens[name]
}

// Has reports whether the theme defines a token with the given name.
func (t *Theme) Has(name string) bool {
	if t == nil || t.tokens == nil {
		return false
	}
	_, ok := t.tokens[name]
	return ok
}

// Tokens returns the set of token names defined in the theme. The order is
// unspecified.
func (t *Theme) Tokens() []string {
	if t == nil || t.tokens == nil {
		return nil
	}
	out := make([]string, 0, len(t.tokens))
	for name := range t.tokens {
		out = append(out, name)
	}
	return out
}

// parseTokenDef converts a JSON token definition to a ui.Style.
func parseTokenDef(def tokenDef) (ui.Style, error) {
	var style ui.Style

	if def.FG != "" {
		c, err := ui.ParseColor(def.FG)
		if err != nil {
			return ui.Style{}, fmt.Errorf("fg: %w", err)
		}
		style.FG = c
	}

	if def.BG != "" {
		c, err := ui.ParseColor(def.BG)
		if err != nil {
			return ui.Style{}, fmt.Errorf("bg: %w", err)
		}
		style.BG = c
	}

	for _, attr := range def.Attrs {
		a, err := parseAttr(attr)
		if err != nil {
			return ui.Style{}, fmt.Errorf("attrs: %w", err)
		}
		style.Attrs |= a
	}

	return style, nil
}

// parseAttr converts an attribute name to a ui.Attr bit.
func parseAttr(name string) (ui.Attr, error) {
	switch name {
	case "bold":
		return ui.AttrBold, nil
	case "dim":
		return ui.AttrDim, nil
	case "italic":
		return ui.AttrItalic, nil
	case "underline":
		return ui.AttrUnderline, nil
	case "reverse":
		return ui.AttrReverse, nil
	case "strike":
		return ui.AttrStrike, nil
	default:
		return 0, fmt.Errorf("unknown attribute %q (want: bold, dim, italic, underline, reverse, strike)", name)
	}
}
