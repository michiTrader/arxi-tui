package ext

import (
	"encoding/json"
	"fmt"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// BundlePluginConsent is one plugin's place on the bundle consent screen: the
// fetched-and-validated manifest whose identity the grant will bind to, the
// package digest that identity is computed over (identity.go), and whether the
// gate already remembers a grant for this exact identity.
//
// Remembered is the DESIGN-BLOCK-J J4 distinction between the *new* cost of the
// install and its *whole* cost: a plugin the gate already remembers grants no new
// power, but it is still part of the interface the bundle mounts, so the screen
// lists it as trusted-no-new-power rather than hiding it. Hiding it would show
// the user only the plugins asking for consent and let a bundle mount a fourth,
// already-trusted plugin the user never saw named here.
type BundlePluginConsent struct {
	Manifest   *Manifest
	Digest     string
	Remembered bool
}

// BundleConsentScene authors the one screen a bundle install shows (J4). It is
// the bundle sibling of ConsentScene: same authored-through-ParseNamed path, same
// tokens both themes sign, same "every granted power is visible" contract — but
// over N plugins under one bundle identity instead of one plugin.
//
// # Why one screen and N grants, not one aggregate grant
//
// DESIGN-BLOCK-J J4 ("How 'one consent screen' aggregates N grants") settles this:
// the aggregation is of the decision and its presentation, never of the identity.
// Each plugin keeps its own per-manifest Identity(m, digest), so a grant made here
// transfers to the same plugin installed standalone or carried by a second bundle,
// and a bundle that changes one component does not silently revoke the grants on
// the others. This function is the presentation half of that decision: it renders
// the whole cost of the install as one screen, and the loop layer fans the single
// y/r/n answer out to one Gate.Grant per not-yet-remembered plugin (a later
// increment, exactly as ConsentScene's view landed before its loop).
//
// # What it must show
//
// The bundle identity (name + description) leads, because that is what the user
// chose to install and the label a remembered decision would be recalled by. Then
// one identity+capability block per plugin that needs consent — the same tuple
// ConsentScene shows, for the same reason: consent to powers the screen hid is not
// consent. Already-remembered plugins are listed compactly as trusted-no-new-power
// so the install is never blind about what it mounts. A bundle with no plugin
// needing consent (scene+theme only, or every plugin already remembered) still
// shows the screen as a named confirm — install is never blind even when it grants
// nothing new, mirroring ConsentScene's explicit no-powers row.
func BundleConsentScene(name, description string, plugins []BundlePluginConsent) (*scene.Document, error) {
	rows := make([]any, 0, 16)

	// Bundle identity block. name is the human handle the (possible) remembered
	// decision is recalled by; description is the one-line summary the bundle's
	// required `description` field exists to supply. Both lead so the user reads
	// what the whole share is before the per-plugin cost.
	rows = append(rows,
		textRow(name, "header"),
		textRow(description, "dim"),
		textRow("", ""),
	)

	// The per-plugin cost, split into the two halves DESIGN-BLOCK-J names: the
	// plugins that need a fresh grant (shown in full, the identity tuple the grant
	// binds to) and the plugins already trusted (named, but not re-detailed —
	// they add no new power, only presence).
	var needsConsent, remembered []BundlePluginConsent
	for _, p := range plugins {
		if p.Manifest == nil {
			// A nil manifest is a caller bug (the loop hands a fetched, validated
			// manifest), but the view is total: name the gap rather than panic, so a
			// misuse produces a visible screen instead of a crash at the moment the
			// user is about to grant powers.
			rows = append(rows, textRow("  • (a plugin reference could not be read)", "dim"))
			continue
		}
		if p.Remembered {
			remembered = append(remembered, p)
		} else {
			needsConsent = append(needsConsent, p)
		}
	}

	if len(needsConsent) > 0 {
		rows = append(rows, textRow("This bundle installs these plugins:", "header"))
		for _, p := range needsConsent {
			rows = append(rows, pluginBlock(p)...)
		}
	}

	if len(remembered) > 0 {
		rows = append(rows, textRow("Already trusted (no new power granted):", "header"))
		for _, p := range remembered {
			m := p.Manifest
			rows = append(rows, textRow(fmt.Sprintf("  • %s  v%s", m.Name, m.Version), "dim"))
		}
		rows = append(rows, textRow("", ""))
	}

	if len(needsConsent) == 0 && len(remembered) == 0 {
		// A scene+theme-only bundle: nothing runs code, so there is no capability to
		// grant, but the confirm is still shown by name. This is the bundle analogue
		// of ConsentScene's "runs powerless" row — "installs no plugins" and "the
		// plugin list failed to render" must not look the same.
		rows = append(rows, textRow("This bundle installs a scene and/or theme; it runs no plugin code.", ""), textRow("", ""))
	}

	// The prompt. One answer for the whole bundle: the design's all-or-nothing rule
	// (a scene wired to a rejected plugin is a scene with dead binds), and the same
	// y/r/n vocabulary consentAnswerForKey speaks for a single plugin. When nothing
	// needs a fresh grant, `y` is a plain confirm and `r` remembers nothing new, but
	// the keys are still drawn so the loop's contract is pinned by the golden the
	// same way the single-plugin screen pins it.
	rows = append(rows,
		textRow("Install this bundle?  [y] grant all   [n] reject", "banner"),
		textRow("[r] grant all and remember each plugin", "banner"),
	)

	doc := map[string]any{
		"root": map[string]any{
			"type":     "box",
			"border":   "single",
			"title":    "A bundle is requesting consent",
			"children": []any{map[string]any{"type": "stack", "children": rows}},
		},
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("could not serialise the bundle consent scene: %w", err)
	}
	return scene.ParseNamed("bundle-consent", out)
}

// pluginBlock renders the full identity+capability block for one plugin needing
// consent — the same shape and tokens ConsentScene uses for its single plugin, so
// the bundle screen and the standalone screen show a plugin's terms identically.
// The grant the user makes here binds to exactly this tuple, so the whole tuple is
// shown: name+version handle, id/protocol machine identity, the runs line (what
// actually executes), the digest the grant binds to, and each requested power on
// its own row.
func pluginBlock(p BundlePluginConsent) []any {
	m := p.Manifest
	rows := []any{
		textRow(fmt.Sprintf("  %s  v%s", m.Name, m.Version), "header"),
		textRow(fmt.Sprintf("    id: %s   protocol: %s", m.ID, m.Protocol), "dim"),
		textRow("    runs: "+runLine(m), ""),
		textRow("    package digest: "+p.Digest, "dim"),
	}
	if len(m.Capabilities) == 0 {
		rows = append(rows, textRow("    capabilities: (none — this plugin runs with no host powers)", "dim"))
	} else {
		rows = append(rows, textRow("    capabilities requested:", ""))
		for _, c := range m.Capabilities {
			rows = append(rows, textRow("      • "+c, ""))
		}
	}
	rows = append(rows, textRow("", ""))
	return rows
}
