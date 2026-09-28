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
// The search input shows the typed query: renderInput now resolves any
// view-state bind, so community.query appears in the box as the loop writes it
// (this was the first deferred affordance, landed in its own engine increment
// with a both-directions counterfactual). The selected row wears a leading
// marker too: the engine synthesizes a per-row row.selected boolean from
// community.selected and the row_template gates a caret glyph on
// `when: "row.selected"` (see liveInstallerList) — the second deferred
// affordance. The right column is now the selected entry's preview, not a static
// help pane: it binds community.selected.{name,version,preview}, which the engine
// resolves by indexing community.matches with community.selected (see
// selectedCommunityMatch and renderMarkdown's bind resolution) — the third and
// last deferred affordance, landed here.
//
// What remains is not an affordance of this document but the host loop that
// drives it: the keystroke loop that writes the community.* fold fields via
// Registry.FilterEntries on every keystroke, moves community.selected on ↑/↓, and
// swaps this document onto the display. When it lands, the Scene 7 golden moves
// from the static build to this one, in its own mutation family named for the
// live installer, and the static InstallerScene retires the way ui.hidden's
// exemption did once its consumer existed.

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
							liveInstallerPreview(),
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
// row.description among the seven). manifest_url and preview are carried by the
// scope but not drawn here: the URL is the install target the loop acts on, and
// the preview is the right pane's job once selection lands, so drawing them in
// every row would be noise now and duplicate the preview pane later. The row
// template naming only signed fields is what the validator checks at load, so a
// typo here is a load-time refusal, not a silent blank row.
//
// The selected row wears a leading marker. The name line is a row of a
// `when: "row.selected"` caret glyph and the name text; on the one row whose
// index equals community.selected the glyph draws and the name shifts right,
// on every other row the gated glyph is absent and the name sits at column
// zero. This is the highlight community.selected exists to drive (BINDS.md
// §4.3), expressed with only signed mechanism: a per-row boolean and the same
// `when` gate Scene 9 uses for its per-row spinner. It is a marker rather than a
// brightened style because this engine's `when` shows or hides a node, it does
// not switch one node's token between two values, and a marker that appears is a
// standard, unambiguous selection cue that needs no second, negated node.
func liveInstallerList() map[string]any {
	return map[string]any{
		"type": "list",
		"id":   "installer.list",
		"bind": "community.matches",
		"row_template": map[string]any{
			"type": "stack",
			"children": []any{
				map[string]any{
					"type": "row",
					"children": []any{
						map[string]any{"type": "text", "when": "row.selected", "text": "> ", "style": map[string]any{"style": "header"}},
						map[string]any{"type": "text", "bind": "row.name", "style": map[string]any{"style": "header"}},
					},
				},
				map[string]any{"type": "text", "bind": "row.version", "style": map[string]any{"style": "dim"}},
				map[string]any{"type": "text", "bind": "row.description"},
			},
		},
	}
}

// liveInstallerPreview is the right column: the selected entry's preview, driven
// by community.selected. Where the static InstallerScene right pane was fixed
// help text — the honest placeholder while there was no selection view state to
// bind — this stacks the selected entry's name, version and preview blurb,
// resolved through the community.selected.* binds the engine projects by indexing
// community.matches with community.selected (BINDS.md §4.3). It reads the
// selection absolutely rather than through row.*, because a pane outside the list
// has no row scope: the row_template's row.name is the entry under the cursor
// *within the list*, and this pane is a sibling of the list, not a row of it.
//
// The name and version are single-line text; the preview is a markdown node so
// the entry's multi-line blurb wraps to the narrow column rather than welding
// onto one line as a text node would. When nothing is selected — an empty browse,
// or a frame before the host's first clamp — every community.selected.* bind
// resolves to "", so the pane draws blank, the same no-op the empty match list
// gives the browse. This is the third deferred live affordance; the help text it
// replaces is not lost, it was always a stand-in for exactly this pane, as its
// own comment in registry_scene.go said.
func liveInstallerPreview() map[string]any {
	return map[string]any{
		"type": "stack",
		"children": []any{
			map[string]any{"type": "text", "bind": "community.selected.name", "style": map[string]any{"style": "header"}},
			map[string]any{"type": "text", "bind": "community.selected.version", "style": map[string]any{"style": "dim"}},
			map[string]any{"type": "markdown", "bind": "community.selected.preview"},
		},
	}
}
