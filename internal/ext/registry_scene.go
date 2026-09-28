package ext

// InstallerScene is J3 of Block J (DESIGN-BLOCK-J.md): the community installer
// authored as a scene document rather than as bespoke UI code. The whole thesis
// of the project is that the interface is a document the running instance can
// generate and rewrite, so the installer is generated the same way the diff view
// is (patch.Diff.Scene): a map[string]any assembled here, marshalled, and
// re-parsed through scene.ParseNamed so the host authors this view through
// exactly the parse+validate path a user's own scene takes (the dogfooding
// ADR-0003 is about). It returns a parsed *scene.Document rather than a fragment
// so a golden can pin it (that golden is J5) and the caller can render it
// directly.
//
// One deviation from the design's recommended default is deliberate and recorded
// here so it reads as a decision, not an oversight. The default sketch wants a
// `list` whose `bind` is the registry entries with a `row_template`, plus a
// search `input` bound to a host view field. But our list/row_template machinery
// iterates fold state (rowScopesFor over team.members/agent.todos/slash.matches),
// so a live-bind installer would require minting a signed `community.*` array
// bind, a row schema, a fold field and host-loop wiring to populate and filter it
// on every keystroke — the interactive half the design itself defers to H8 and
// beyond. The design's *primary* testability requirement is the stronger guide:
// "a pure index -> *scene.Document, testable by a golden the way Diff.Scene is",
// and Diff.Scene bakes its content as static nodes. So this bakes each entry as
// a pressable card, which needs no frozen-doc vocabulary expansion, validates and
// renders today, and pins a golden now. Live search filtering and a
// selection-driven preview pane are the follow-up increment, and they are the
// part that needs the signed view-state bind; baking here does not foreclose it.
//
// Install is H8's `cmd:` action, not a new mechanism: each card carries
// on_press "cmd:/ui plugin add <manifest_url>", dispatched through the same
// command surface a typed line takes (cmd/arxi-tui/press.go). The registry
// discovers a URL and the existing H6 install path does the rest — the entry's
// manifest_url is fetched, Validated and consent-gated exactly as a hand-typed
// /ui plugin add would be. The registry grants nothing the install pipeline does
// not already gate.

import (
	"encoding/json"
	"fmt"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// InstallerScene builds the community installer for this registry as a parsed,
// validated scene document. It is a pure function of the index: the same
// registry always produces the same scene, which is what lets a golden pin it.
//
// The layout is the two-column shape the design specifies (a row split into two
// stacks, the diff-view shape): the left stack is the browsable entry list with a
// search input at its head, and the right stack is a help/preview pane. Each
// entry is a pressable card carrying its own install action, so focusing it (Tab)
// and pressing Enter installs it (H8). An empty registry still produces a valid
// scene — the list is simply headed by the search box with no cards under it,
// which is a thin browse rather than a broken one, the same call PreviewMocks and
// the preview-optional entry made.
func (r *Registry) InstallerScene() (*scene.Document, error) {
	entries := make([]any, 0, len(r.Entries)+1)

	// The search input heads the left column. It is laid out now but not yet
	// wired to filter the list: filtering is the interactive half (a host view
	// field updated per keystroke) that waits on the same view-state bind the
	// live-list version would need. Placing it here fixes the column's shape so
	// the follow-up adds behaviour without moving the golden's structure.
	entries = append(entries, map[string]any{
		"type":        "input",
		"id":          "installer.search",
		"placeholder": "search community plugins",
	})

	for _, e := range r.Entries {
		entries = append(entries, installerCard(e))
	}

	doc := map[string]any{
		"root": map[string]any{
			"type":   "box",
			"border": "single",
			"title":  "Community — browse and install",
			"children": []any{
				map[string]any{
					"type": "row",
					"children": []any{
						map[string]any{"type": "stack", "weight": 1, "children": entries},
						map[string]any{"type": "stack", "weight": 1, "children": []any{
							installerHelp(),
						}},
					},
				},
			},
		},
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("could not serialise the installer scene: %w", err)
	}
	return scene.ParseNamed("installer", out)
}

// installerCard is one registry entry as a pressable card: its name, description
// and inline preview stacked, with the install action on the container so a press
// anywhere on the card installs it. The id is required alongside on_press for the
// node to be pressable at all (cmd/arxi-tui/press.go only rings nodes that have
// both), and it is derived from the entry id so a focus: action or a test can
// address a specific card. Every field the registry entry carries is used here —
// name, description and preview are drawn, manifest_url becomes the install
// target — so the golden exercises the whole entry shape rather than a subset.
func installerCard(e RegistryEntry) map[string]any {
	return map[string]any{
		"type":     "stack",
		"id":       "install:" + e.ID,
		"on_press": "cmd:/ui plugin add " + e.ManifestURL,
		"children": []any{
			map[string]any{"type": "text", "text": e.Name},
			map[string]any{"type": "text", "text": e.Description},
			map[string]any{"type": "markdown", "text": e.Preview},
		},
	}
}

// installerHelp is the right column: a markdown pane that documents the install
// flow. It is deliberately static content, not a per-entry preview, because
// showing "the selected entry's preview" needs the selection view state that the
// interactive follow-up adds; a fixed explanation is the honest placeholder that
// still tells the user what pressing a card does and that the manifest is gated
// the same way a typed install is. When selection lands, this pane narrows to the
// focused entry's preview and the golden moves in its own mutation family.
func installerHelp() map[string]any {
	return map[string]any{
		"type": "markdown",
		"text": "Focus an entry with Tab and press Enter to install it. " +
			"Each entry installs from its signed manifest_url through the same " +
			"consent gate as /ui plugin add — the registry only points at the " +
			"manifest, it grants nothing on its own.",
	}
}
