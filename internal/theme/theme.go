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
//
// A theme also carries an optional `anim` section: named timing tokens (D4,
// docs/TOKENS.md) the render/emit layer consumes to drive animation props. It
// is a distinct map, not folded into tokens, because a timing token and a style
// token are different in kind — one is data an emitter paints, the other names
// an easing function in the binary — and TOKENS.md signs them into separate
// theme sections precisely so their namespaces cannot collide.
type Theme struct {
	tokens map[string]ui.Style
	anim   map[string]AnimDef
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
// The reserved top-level key `anim` is not a style token: it is the timing-token
// section (D4, docs/TOKENS.md), a map of names to {duration_ms, curve, fps}. It
// is pulled out before the rest is read as style tokens, so a timing name and a
// style name occupy separate namespaces and a theme that defines `anim` does not
// have it mis-parsed as a style whose fg/bg happen to be absent. Each timing
// definition is validated at load (closed curve set, non-negative
// duration_ms/fps) — the same net a bad style token gets, addressed to the key.
//
// An invalid file returns an error with file:line if possible.
func Load(path string) (*Theme, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	// Decode into raw messages first so the `anim` section can be lifted out
	// before the remainder is treated as style tokens. Unmarshalling straight
	// into map[string]tokenDef would silently coerce the anim object into a
	// tokenDef with no fields — an accepted-but-wrong parse, the class this
	// project refuses to ship — and the timing tokens would vanish.
	var rawTop map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawTop); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	var anim map[string]AnimDef
	if raw, ok := rawTop["anim"]; ok {
		delete(rawTop, "anim")
		if err := json.Unmarshal(raw, &anim); err != nil {
			return nil, fmt.Errorf("%s: anim section: %w", path, err)
		}
		for name, def := range anim {
			if err := validateAnimDef(name, def); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
		}
	}

	tokens := make(map[string]ui.Style, len(rawTop))
	for name, rawDef := range rawTop {
		var def tokenDef
		if err := json.Unmarshal(rawDef, &def); err != nil {
			return nil, fmt.Errorf("%s: token %q: %w", path, name, err)
		}
		style, err := parseTokenDef(def)
		if err != nil {
			return nil, fmt.Errorf("%s: token %q: %w", path, name, err)
		}
		tokens[name] = style
	}

	return &Theme{tokens: tokens, anim: anim}, nil
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

// Anim looks up a timing token by name and reports whether it exists. The
// render/emit layer resolves an animation prop's token through this the way the
// emitter resolves a style token through Resolve; the fold never calls it,
// which is the D4/Q9 boundary — a timing token is the animation and stays on
// the far side of the fold line.
func (t *Theme) Anim(name string) (AnimDef, bool) {
	if t == nil || t.anim == nil {
		return AnimDef{}, false
	}
	def, ok := t.anim[name]
	return def, ok
}

// HasAnim reports whether the theme defines a timing token with the given name.
// It is the anim counterpart of Has, used by the scene validator to refuse a
// prop that names a timing token the active theme does not define — the same
// net an undefined style token gets.
func (t *Theme) HasAnim(name string) bool {
	_, ok := t.Anim(name)
	return ok
}

// AnimNames returns the timing-token names the theme defines. The order is
// unspecified, matching Tokens.
func (t *Theme) AnimNames() []string {
	if t == nil || t.anim == nil {
		return nil
	}
	out := make([]string, 0, len(t.anim))
	for name := range t.anim {
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

// SOBRIA returns the factory default theme: dim/bright only, no color. This is
// Scene 2 (the fx-inspired default per PLAN.md Phase 1). A dark terminal gets
// dim dimmer than the default text; a light terminal gets it darker. The theme
// adapts to the terminal's background without OSC 11 queries — dim and bright
// are relative attributes the terminal already resolves.
//
// Token coverage: text (default), input (bright), input.placeholder (dim),
// banner (bright), spinner (dim). Every token the RAW and SOBRIA scenes reference
// is defined here; a missing token is a validation error, not a runtime lookup.
func SOBRIA() *Theme {
	return FromMap(map[string]ui.Style{
		"text": {}, // default: no attributes, terminal's default fg/bg
		"dim":  {Attrs: ui.AttrDim},
		// TOKENS.md signs "header": {"attrs": ["bold"]} and states this
		// theme "defines exactly the tokens the three golden scenes
		// reference, and nothing more". It was missing while SOBRIA
		// referenced it twice, and nothing caught that because the token
		// validator was reading the wrong style key — so the default
		// interface shipped a reference the product refuses in a
		// downloaded scene. Restored from the document rather than
		// removed from the scene: the signed theme is the contract, and
		// dropping the reference would have silently restyled the header
		// row of the shipped look.
		"header":             {Attrs: ui.AttrBold},
		"bright":             {Attrs: ui.AttrBold},
		"input":              {Attrs: ui.AttrBold},
		"input.placeholder":  {Attrs: ui.AttrDim},
		"banner":             {Attrs: ui.AttrBold},
		"spinner":            {Attrs: ui.AttrDim},
		"markdown.heading":   {Attrs: ui.AttrBold},
		"markdown.emphasis":  {Attrs: ui.AttrItalic},
		"markdown.strong":    {Attrs: ui.AttrBold},
		"markdown.code":      {}, // no style: same as surrounding text
		"markdown.codeblock": {Attrs: ui.AttrDim},
		// The change-diff view (PLAN.md ADR-0003) is a host-generated scene,
		// so its three tokens are signed here like any other. They stay
		// colourless to keep sobria's identity -- the meaning is carried by
		// attribute and by column, not by red/green, and a user theme is free
		// to map them to colour: context is de-emphasised, a removed line is
		// struck through, an added line is emphasised.
		"diff.context": {Attrs: ui.AttrDim},
		"diff.del":     {Attrs: ui.AttrStrike},
		"diff.add":     {Attrs: ui.AttrBold},
	})
}
