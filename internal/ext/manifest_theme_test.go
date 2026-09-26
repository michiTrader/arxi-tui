package ext

import (
	"errors"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/theme"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

// TestThemeReturnsTheDeclaredTokens is H4's positive control on the ext side: a
// declarative manifest may contribute tokens, and Theme() must surface them as a
// resolvable theme so the host can merge them into the active look. If this
// fails, a plugin's styling never reaches the screen.
func TestThemeReturnsTheDeclaredTokens(t *testing.T) {
	m, err := ParseNamed("plugin.json", []byte(validDeclarative))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	thm, err := m.Theme()
	if err != nil {
		t.Fatalf("Theme() refused a well-formed token block: %v; a declarative plugin's tokens are data and must parse into a theme", err)
	}
	if !thm.Has("profit") {
		t.Fatalf("Theme() dropped the declared token 'profit'; the manifest's tokens block must reach the merged theme, or a plugin's styling is silently discarded")
	}
}

// TestThemeIsEmptyWhenNoTokens pins the no-op case: a mount-only plugin
// contributes no tokens, and Theme() must return a usable empty theme rather than
// nil, so the host can compose Merge(active, pluginTheme) unconditionally without
// a nil-check per plugin.
func TestThemeIsEmptyWhenNoTokens(t *testing.T) {
	// Valid because it mounts a fragment; checkEmpty only refuses a manifest with
	// neither mounts nor tokens.
	const mountOnly = `{
	  "id": "tick",
	  "name": "Ticker",
	  "version": "1.0.0",
	  "protocol": "ext/v1",
	  "mounts": [ { "where": "top-right", "fragment": { "type": "text", "text": "hi" } } ]
	}`
	m, err := ParseNamed("plugin.json", []byte(mountOnly))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate refused a mount-only manifest: %v; a plugin that only mounts UI is valid", err)
	}
	thm, err := m.Theme()
	if err != nil {
		t.Fatalf("Theme() on a token-less manifest errored: %v; contributing no tokens is not a fault, it is a no-op layer", err)
	}
	if thm == nil {
		t.Fatalf("Theme() returned nil for a token-less manifest; it must return an empty theme so the host merges a no-op layer rather than nil-checking every plugin")
	}
	if len(thm.Tokens()) != 0 {
		t.Fatalf("Theme() on a token-less manifest defined %d tokens, want 0; an empty contribution must add nothing", len(thm.Tokens()))
	}
}

// TestThemeSurfacesPluginAnimTokens holds Theme() to lifting a plugin's `anim`
// section the same way theme.LoadBytes does for a theme file: a plugin may ship
// timing tokens, and if Theme() dropped them a fragment naming the plugin's anim
// token would fail the load-time undefined-token refusal despite the plugin
// defining it.
func TestThemeSurfacesPluginAnimTokens(t *testing.T) {
	const withAnim = `{
	  "id": "tick",
	  "name": "Ticker",
	  "version": "1.0.0",
	  "protocol": "ext/v1",
	  "tokens": {
	    "profit": { "fg": "green" },
	    "anim": { "tick.pan": { "duration_ms": 0, "curve": "linear", "fps": 20 } }
	  }
	}`
	m, err := ParseNamed("plugin.json", []byte(withAnim))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	thm, err := m.Theme()
	if err != nil {
		t.Fatalf("Theme() refused a plugin carrying an anim section: %v; a plugin's timing tokens are the same shape a theme file's are", err)
	}
	if !thm.HasAnim("tick.pan") {
		t.Fatalf("Theme() dropped the plugin's anim token 'tick.pan'; a prop naming it would then fail the undefined-token refusal even though the plugin defined it")
	}
}

// TestThemeCarriesTheManifestAddressOnAnInvalidBlock is the counterfactual for
// the delegation validateTokensBlock now makes to Theme(): a malformed token must
// be refused with the manifest-addressed file:line, not theme.LoadBytes's bare
// name. If Theme() lost the address, a plugin author would get a refusal they
// cannot locate in their file.
func TestThemeCarriesTheManifestAddressOnAnInvalidBlock(t *testing.T) {
	const badToken = `{
	  "id": "tick",
	  "name": "Ticker",
	  "version": "1.0.0",
	  "protocol": "ext/v1",
	  "tokens": { "profit": { "fg": "not-a-colour" } }
	}`
	m, err := ParseNamed("plugin.json", []byte(badToken))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	_, err = m.Theme()
	if err == nil {
		t.Fatalf("Theme() accepted a token with an invalid colour; a malformed token definition must be refused, or a plugin can smuggle a broken style past the load")
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("Theme() returned %T, want *ext.Error; the refusal must carry the manifest address, not a bare theme error", err)
	}
	if e.Loc.File != "plugin.json" || e.Loc.Line == 0 {
		t.Fatalf("Theme() refusal has loc %+v, want file plugin.json with a non-zero line; a plugin author must be able to open the reported position", e.Loc)
	}
}

// TestPluginTokensMergeOverFactory is the end-to-end H4 assertion at the boundary
// the two packages meet: a plugin's Theme(), layered over the factory theme at the
// plugin position, overrides a factory token of the same name and adds its own,
// while a factory token the plugin does not mention survives. This is the product
// purpose of H4 — enabling a plugin restyles exactly what the plugin declares and
// nothing else.
func TestPluginTokensMergeOverFactory(t *testing.T) {
	const restyler = `{
	  "id": "restyle",
	  "name": "Restyler",
	  "version": "1.0.0",
	  "protocol": "ext/v1",
	  "tokens": {
	    "banner": { "attrs": ["dim"] },
	    "restyle.badge": { "attrs": ["underline"] }
	  }
	}`
	m, err := ParseNamed("plugin.json", []byte(restyler))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	pluginTheme, err := m.Theme()
	if err != nil {
		t.Fatalf("Theme(): %v", err)
	}

	// The plugin sits between factory and user (user absent here): Merge(factory, plugin).
	active := theme.Merge(theme.SOBRIA(), pluginTheme)

	if s := active.Resolve("banner"); s.Attrs != ui.AttrDim {
		t.Fatalf("banner resolved to %v after merging the plugin, want dim: a plugin token must override the same-named factory token (SOBRIA ships banner as bold), or the plugin cannot restyle the shipped look", s.Attrs)
	}
	if s := active.Resolve("restyle.badge"); s.Attrs != ui.AttrUnderline {
		t.Fatalf("restyle.badge resolved to %v, want underline: a plugin's own new token must reach the active theme", s.Attrs)
	}
	if !active.Has("input") {
		t.Fatalf("the factory 'input' token vanished after merging a plugin that never mentions it; enabling a plugin must not strip factory tokens it does not touch")
	}
}
