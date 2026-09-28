package ext

// LiveInstallerScene is the next J3 follow-up increment (DESIGN-BLOCK-J.md): the
// community installer as a *live* document, the form the keystroke loop will
// drive. Where InstallerScene bakes each index entry as a static card — the
// deliberate deferral J3 shipped and J5 froze as the Scene 7 golden — this
// builder binds the browse list to the community.* view state instead: the
// search input reads community.query, and the entry list is a row_template over
// community.matches (the filtered entries the loop recomputes through
// Registry.FilterEntries on every keystroke). The entries therefore live in the
// fold, not in the builder, which is why this takes no index: the same registry
// that produced the static cards now produces nothing here, because the content
// is host state the loop writes, not chrome baked once. Scene says form, fold
// says content (ADR-0002) — the static builder had to bake because there was no
// signed view state to bind to, and that is exactly what PR #107 signed and
// PR #108 projected.
//
// It is built and pinned before the loop mounts it, the same order TICKER.json
// and COMMUNITY.json pin a mount/build output ahead of the code that consumes
// it: freezing the target document now means the loop increment that swaps this
// onto the display cannot silently change its shape — a drift is a golden diff,
// not an invisible divergence. When that loop lands, the Scene 7 golden moves
// from the static build to this one, in its own mutation family named for the
// live installer, and the static InstallerScene retires the way ui.hidden's
// exemption did once its consumer existed.
//
// Two live affordances the design names are deliberately NOT here yet, each
// because the engine capability it needs is its own increment, and each is the
// honest placeholder rather than a broken half:
//
//   - The search input shows its placeholder, not the typed query. renderInput
//     draws a bound field's value only for user.input today; teaching it to draw
//     any resolved view-state bind (so community.query appears as the user types)
//     is a separate engine change with its own counterfactual, not smuggled in
//     under a scene builder.
//   - The right column stays the static help pane, not the selected entry's
//     preview. A selection-driven preview needs the selected entry's fields as
//     absolute binds (community.selected.preview and friends) — new signed
//     vocabulary — which is the increment after this one. Rendering every match's
//     preview inline instead would just be the static cards again, not a preview
//     pane, so the honest placeholder is the fixed explanation InstallerScene
//     already carries.
//
// The list has no selection highlight either: highlighting the community.selected
// row needs the row's index inside its own scope, which row_template does not
// carry today (no list does — team.members and slash.matches draw every row the
// same). That, too, is a later increment; drawing one row bright is not required
// for the loop to move the selection, only for the user to see which row it is
// on, and the loop increment is where that becomes observable.

import (
	"encoding/json"
	"fmt"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// LiveInstallerScene builds the live community installer as a parsed, validated
// scene document. It is a pure constant: the structure never varies, because the
// content it shows is entirely fold state (community.query, community.matches),
// which is what lets a golden pin the document while the frame the golden renders
// still varies with the folded matches. An empty match list draws a bare browse
// (search box, no rows), the same signed empty state the row_template binds carry
// (team.members, slash.matches all render zero rows when empty), so the un-driven
// installer is not a broken one.
func LiveInstallerScene() (*scene.Document, error) {
	doc := map[string]any{
		"root": map[string]any{
			"type":   "box",
			"border": "single",
			"title":  "Community — browse and install",
			"children": []any{
				installerNotice(),
				map[string]any{
					"type": "row",
					"children": []any{
						map[string]any{"type": "stack", "weight": 1, "children": []any{
							map[string]any{
								"type":        "input",
								"id":          "installer.search",
								"bind":        "community.query",
								"placeholder": "search community plugins",
							},
							liveInstallerList(),
						}},
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
		return nil, fmt.Errorf("could not serialise the live installer scene: %w", err)
	}
	return scene.ParseNamed("installer-live", out)
}

// liveInstallerList is the browsable entry list, a row_template over
// community.matches. Each instance stacks the entry's name, version and
// description — the fields a user scans to choose — read through the row.*
// namespace the §4.7 schema for community.matches signs (row.name/row.version/
// row.description among the six). manifest_url and preview are carried by the
// scope but not drawn here: the URL is the install target the loop acts on, and
// the preview is the right pane's job once selection lands, so drawing them in
// every row would be noise now and duplicate the preview pane later. The row
// template naming only signed fields is what the validator checks at load, so a
// typo here is a load-time refusal, not a silent blank row.
func liveInstallerList() map[string]any {
	return map[string]any{
		"type": "list",
		"id":   "installer.list",
		"bind": "community.matches",
		"row_template": map[string]any{
			"type": "stack",
			"children": []any{
				map[string]any{"type": "text", "bind": "row.name", "style": map[string]any{"style": "header"}},
				map[string]any{"type": "text", "bind": "row.version", "style": map[string]any{"style": "dim"}},
				map[string]any{"type": "text", "bind": "row.description"},
			},
		},
	}
}
