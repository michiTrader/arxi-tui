package scene

import (
	"fmt"
	"sort"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/theme"
)

// TokenError records a reference to an undefined token in a scene. LESSONS.md
// carries this rule over from arxi-sim's theme audit with the polarity
// inverted — the vocabulary is open, so it is the *reference* that is checked —
// but the error discipline is unchanged: "a referenced-but-undefined token is
// an error with file:line, and an unused token is a warning".
type TokenError struct {
	Token    string // The token name that was not found in the theme
	NodeType string // The type of node that referenced it
	Loc      Loc    // Where the reference is, for the file:line rule
}

func (e TokenError) Error() string {
	msg := fmt.Sprintf("node type %q references undefined token %q", e.NodeType, e.Token)
	if e.Loc == (Loc{}) {
		return msg
	}
	return e.Loc.String() + ": " + msg
}

// Validate checks that every bind and when string referenced in the scene is
// in the signed inventory (docs/BINDS.md §4.5). This is the exit criterion:
// an unsigned bind is a load-time error. Returns the first validation error
// encountered, or nil if the document is valid.
//
// §4.5 specifies the refusal exactly — "a file:line error pointing at the
// offending node" — so the walk carries each node's access path and the error
// resolves it to a position through the document's address book.
func (d *Document) Validate() error {
	if d == nil || d.Root == nil {
		return nil
	}
	if err := d.validateBinds(d.Root, nodePathRoot); err != nil {
		return err
	}
	return d.validateIDs()
}

// PluginScope is the schema a mounted plugin fragment is validated against: the
// plugin's id — whose `<id>.` prefix opens the plugin bind namespace — and the
// binds the plugin declares, each mapped to the node kind that value is legal
// under. It is the plugin-namespace analogue of a row_template's RowSchema
// (§4.7): the manifest's `binds` map is a plugin's schema the same way a source
// list's RowSchema is a template's schema, and the validator consults it as the
// single source.
//
// scene cannot import ext — the arch seam keeps the loader UI-free and, in the
// other direction, keeps this validator free of the manifest parser — so the
// caller projects the manifest's own `binds` map into this scene-owned shape.
// That projection is a read, never a copy into a second inventory (the same
// discipline SignedBinds documents): a plugin's declared binds must feed the
// one validator directly, or the manifest and the check that gates it drift.
type PluginScope struct {
	// ID is the plugin id; its bind namespace is ID + ".". A declarative plugin
	// (no executable) still has an id and thus a namespace — an empty one, since
	// it declares no binds, which is exactly why using it is refused.
	ID string
	// Binds maps each declared, fully-qualified `<id>.<field>` bind to its
	// declared kind (the node kind the value is legal under). Empty for a
	// declarative plugin, which streams nothing.
	Binds map[string]string
}

// ValidateWithPlugin is Validate for a document being composed from a plugin's
// fragments: a bind in the plugin's own `<id>.` namespace resolves iff the
// plugin declares it (H-C / BINDS.md §4.4), rather than being refused as
// unsigned. Every other bind is checked exactly as Validate checks it, so a host
// bind still resolves against the signed inventory and an unsigned foreign bind
// is still refused. Validate is this method with no plugin scope.
func (d *Document) ValidateWithPlugin(scope *PluginScope) error {
	if d == nil || d.Root == nil {
		return nil
	}
	if err := d.validateBindsScoped(d.Root, nodePathRoot, nil, scope); err != nil {
		return err
	}
	return d.validateIDs()
}

// bindKindNodeTypes maps a declared plugin bind's `kind` to the node types that
// may carry it. It is closed for overlayAnchors' reason (internal/patch): a kind
// is the axis a value rides on the render side — a scalar string versus a numeric
// series — so a kind the engine can draw nowhere would validate here and render
// nowhere. The set starts at the two Scene 6 uses (BINDS.md §4.4): "text" is a
// node that draws a scalar string, "series" is the sparkline's numeric axis. It
// widens per node type as a signed change, never silently.
var bindKindNodeTypes = map[string]map[string]bool{
	"text":   {"text": true, "marquee": true},
	"series": {"sparkline": true},
}

// bindKindList renders the closed kind set for an error message, sorted so the
// message is stable across runs.
func bindKindList() string {
	out := make([]string, 0, len(bindKindNodeTypes))
	for k := range bindKindNodeTypes {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// bindKindNodeTypeList renders the node types a kind is legal on, sorted, for the
// wrong-node-type refusal.
func bindKindNodeTypeList(kind string) string {
	types := bindKindNodeTypes[kind]
	out := make([]string, 0, len(types))
	for t := range types {
		out = append(out, t)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// signedBinds is the §4.5 inventory: every bind a scene may reference. A bind
// not in this list fails validation at load time.
//
// This map is the *runtime* inventory and it is deliberately a Go literal
// rather than the parsed document: arxi ships as one static binary, so the
// validator cannot depend on docs/BINDS.md existing on the user's disk. The
// document stays the contract, and TestSignedInventoryMatchesDocument is what
// keeps the two identical — a row added to the doc without a line here (or the
// reverse) fails the suite. That test exists because this map had already
// drifted from the signed document in both directions: it carried four binds
// signed nowhere (agent.status, banner.text, session.tokens) while rejecting
// eighteen that BINDS.md §4 signs and internal/fold already computes
// (slash.selected, team.members, ui.focus, usage.in/out, …). Drift in this
// direction is the expensive one: Phase 2's eval corpus measures the repair
// loop — order, patch, file:line error, retry — so a validator that rejects a
// signed bind teaches the model to avoid the vocabulary the product documents.
// MaxIsBindPrefix is the signed prefix of the ui.max.is.<id> family (BINDS.md
// §4.3). It is exported and named once here so the validator and the engine's
// resolveBind share the one spelling and cannot drift on the family the whole
// click-to-maximize gate turns on — the same discipline run.start and the
// provider verbs name their strings once.
const MaxIsBindPrefix = "ui.max.is."

var signedBinds = map[string]bool{
	// §4.1 run state
	"chat.history":            true,
	"thinking.text":           true,
	"agent.working":           true,
	"agent.mode":              true,
	"agent.todos":             true,
	"model.name":              true,
	"usage.in":                true,
	"usage.out":               true,
	"usage.delta":             true,
	"session.tokens_used":     true,
	"session.new_milestone":   true,
	"team.members":            true,
	"todos.count":             true,
	"run.quiescent.diagnosis": true,

	// §4.2 agent blocked / remedy surface
	"agent.blocked.blocked_ref": true,
	"agent.blocked.blocked_on":  true,
	"agent.blocked.actor":       true,

	// §4.3 view state (arxi-tui's own contract)
	"slash.active":   true,
	"slash.typed":    true,
	"slash.matches":  true,
	"slash.selected": true,
	"slash.hint":     true,
	"model.active":   true,
	"model.matches":  true,
	"status.active":  true,
	"ui.focus":       true,
	"ui.max":         true,
	"ui.max.none":    true,
	"ui.surface":     true,
	"ui.hidden":      true,

	// §4.3 view state — the run's actor label (M2 follow-up). Host view state,
	// not a run-state projection: the host resolves the actor from the run.start
	// config it sends (resolveRunStartParams), so it knows the label a round-trip
	// before any run.started could echo it. Signed so the shipped status row and
	// the validator agree; the empty default is falsy, so the when-gated node
	// moves no default golden until a run is being followed.
	"host.run.actor": true,

	// §4.3 view state — the community installer (Scene 7, J3 follow-up). The
	// live half of the installer scene: a search query, its filtered matches,
	// and a selection cursor, in the slash.* mould. Signed so the InstallerScene
	// builder and the validator agree on the vocabulary; the fold fields and the
	// keystroke loop that populate them are the deferred live half (BINDS.md
	// §4.6, DESIGN-BLOCK-J.md J3 follow-up).
	"community.query":    true,
	"community.matches":  true,
	"community.selected": true,

	// §4.3/§4.7 view state — the /config screen's two list binds (Scene 5, E5).
	// Host-owned view state like the community.* triple, projected by the fold and
	// consumed by a `list`: config.categories is the left-rail group list,
	// config.settings the row_template that mixes a switch and an input by row.
	"config.categories": true,
	"config.settings":   true,

	// §4.3/§4.7 view state — the provider hub (/provider). Host-composed
	// text and one list; see fold.State for why the host composes them (the API key
	// must never be in State, so a form's key row is published already masked).
	"hub.title":  true,
	"hub.rows":   true,
	"hub.hint":   true,
	"hub.detail": true,

	// §4.3 view state — the selected community entry's scalar projection (Scene
	// 7, J3 follow-up). community.selected is an index; these resolve it against
	// community.matches to the selected entry's own fields, so the installer's
	// right pane can preview what the cursor is on. They are the absolute-bind
	// analogue of the row.* schema §4.7 signs for community.matches — the same
	// entry fields, addressed by the selection rather than per row — signed as
	// the pane consumes them (name/version/preview), not the whole namespace
	// ahead of a consumer.
	"community.selected.name":    true,
	"community.selected.version": true,
	"community.selected.preview": true,

	// §2 bootstrap set — host survival state the raw scene may display
	"user.input":           true,
	"user.input.submitted": true,
	"host.escape.armed":    true,
	"host.scene.error":     true,
	"host.cwd":             true,
	"host.effort":          true,
	"host.mode":            true,
	"host.thinking":        true,
}

// SignedBinds returns the §4.5 inventory: every bind path a scene may
// reference, sorted. The slice is freshly built on each call, so a caller
// cannot mutate the validator's inventory by holding onto it.
//
// This exists for one reason that is worth stating, because the obvious
// alternative is cheaper and wrong. Phase 2's repair loop has to tell the model
// which binds exist; without that, the first turn of every case is spent
// guessing the vocabulary, and the corpus measures recall of an undocumented
// list instead of the repair loop PLAN.md asked it to measure. The cheap
// alternative is to write the list out again in the runner's prompt — which
// would be the *fourth* copy of the inventory (docs/BINDS.md §4, signedBinds
// here, and the two audit directions in binds_audit_test.go). That map has
// already drifted from the document in both directions once, and the guard
// test only exists because it did. A hand-copied prompt list would drift the
// same way, silently, and its failure mode is the expensive one: the model is
// told a signed bind does not exist, avoids it, and the corpus records that as
// the model's failure rather than the prompt's.
//
// Exporting the inventory rather than the map keeps the validator the single
// source: there is no second list to keep in step, so there is no third drift
// guard to write.
func SignedBinds() []string {
	out := make([]string, 0, len(signedBinds))
	for bind := range signedBinds {
		out = append(out, bind)
	}
	sort.Strings(out)
	return out
}

// validateBinds walks a subtree, carrying the node's access path so a refusal
// can name where it happened. The path is threaded as an argument rather than
// stored on Node because the tree is also built by hand and by future patch
// code, and a position field would then be a field that is sometimes a lie.
func (d *Document) validateBinds(n *Node, path string) error {
	return d.validateBindsScoped(n, path, nil, nil)
}

// validateIDs enforces the ADDRESSING.md §3 id-uniqueness invariant: within one
// document, no two nodes may carry the same non-empty id. An id is an address —
// `focus_glow` keys on `id == ui.focus` and `ui.max` / `cmd:/max <pane>` address
// a pane by id (internal/engine/render.go), and `/ui move <id>` resolves its
// subject by id — so a duplicate makes every one of those targets ambiguous, and
// the ambiguity is latent independent of the write path (two nodes sharing an id
// already double-glow or fight over the maximised slot). It is therefore a
// load-time refusal, not just a check at the /ui-verb boundary where F1/F2 first
// enforced it: a document the user hand-wrote with a duplicate id is as unsafe as
// one a verb would have produced, so the invariant belongs on the validator every
// load path runs, with a file:line error naming the id and both offending nodes.
//
// Scope is per Document by construction — this walks one parsed tree, so the
// eval-corpus fixtures that embed several documents in one file are never
// cross-flagged (ADDRESSING.md §3). The empty id is exempt: an unnamed node is
// not addressable and most nodes legitimately carry none. The traversal mirrors
// validateBindsScoped (children, prefix, suffix, row_template) so a node reached
// only through a prefix/suffix/template is held to the invariant too — its id is
// as much an address as a child's.
func (d *Document) validateIDs() error {
	seen := make(map[string]string) // non-empty id -> path of its first occurrence
	return d.collectIDs(d.Root, nodePathRoot, seen)
}

func (d *Document) collectIDs(n *Node, path string, seen map[string]string) error {
	if n == nil {
		return nil
	}
	if n.ID != "" {
		if first, dup := seen[n.ID]; dup {
			return &Error{
				Loc: d.locOf(path),
				Msg: fmt.Sprintf("duplicate node id %q: an id is an address (focus, ui.max and /ui move resolve a node by it), so it must be unique within a document (docs/ADDRESSING.md §3); first declared at %s, declared again at %s", n.ID, d.locOf(first), d.locOf(path)),
			}
		}
		seen[n.ID] = path
	}
	for i, child := range n.Children {
		if err := d.collectIDs(child, childPath(path, i), seen); err != nil {
			return err
		}
	}
	if prefix := n.PrefixNode(); prefix != nil {
		if err := d.collectIDs(prefix, prefixPath(path), seen); err != nil {
			return err
		}
	}
	if n.Suffix != nil {
		if err := d.collectIDs(n.Suffix, suffixPath(path), seen); err != nil {
			return err
		}
	}
	if n.RowTemplate != nil {
		if err := d.collectIDs(n.RowTemplate, templatePath(path), seen); err != nil {
			return err
		}
	}
	return nil
}

// rowSchemas signs, per array-of-objects bind, the `row.<field>` names a
// row_template over it may address (BINDS.md §4.7). It is the validation
// authority; the engine's rowScopesFor produces values under these same keys,
// and a test holds the two identical so the vocabulary the validator accepts
// and the vocabulary the renderer draws cannot drift apart.
var rowSchemas = map[string]map[string]bool{
	"team.members":  {"row.id": true, "row.state": true, "row.role": true, "row.busy": true, "row.turns": true, "row.spent_usd": true},
	"agent.todos":   {"row.task": true, "row.blocked_on": true, "row.actor": true},
	"slash.matches": {"row.name": true, "row.category": true, "row.description": true},
	// The community installer's entry cards (Scene 7, J3 follow-up). A card
	// renders row.name + row.description and its on_press is
	// `cmd:/ui plugin add {row.manifest_url}` (H8 interpolation), so every
	// human-readable and install-driving field the card touches is addressable.
	// row.selected is the one field here with no CommunityMatch column behind it:
	// the engine synthesizes it per row from community.selected so the template
	// can gate a highlight on `when: "row.selected"` (the row.busy idiom), and it
	// is signed alongside the element fields so that gate validates like any other
	// row bind rather than being refused as an unsigned row.* name.
	"community.matches": {"row.id": true, "row.name": true, "row.version": true, "row.manifest_url": true, "row.description": true, "row.preview": true, "row.selected": true},
	// The /config screen's two lists (Scene 5, E5). config.categories is a
	// single-column group list. config.settings carries the setting's label,
	// the toggle state (row.enabled, read by a `switch`) and the text value
	// (row.value, read by an `input`), plus two synthesized discriminator
	// booleans: row.is_toggle and row.is_text. Those two are the config analogue
	// of community.matches' row.selected — a per-row boolean the engine computes
	// from the element's Kind, signed here rather than in the element schema
	// because they are not data the host stored but the answer to "which node
	// type does this row draw". This engine's `when` has no comparison operator,
	// so the template gates its two sibling nodes on these booleans rather than
	// on a `row.kind == "toggle"` it cannot write; a bare row.kind string would
	// be dead without an operator to compare it, exactly the row.index argument.
	"config.categories": {"row.name": true},
	"config.settings":   {"row.label": true, "row.enabled": true, "row.value": true, "row.is_toggle": true, "row.is_text": true},
	// The providers screen's model list (Scene 12, K2 follow-up). row.provider and
	// row.model are the model's displayed columns; row.enabled is the toggle state
	// a `switch` reads and the gate the "disable" button draws on. row.ref and
	// row.disabled are the two synthesized fields — the config.settings
	// row.is_toggle/row.is_text idiom: row.ref is the enable/disable command's
	// interpolated argument (provider/id), built in the projection so the on_press
	// cannot drift from the ref model.list emits; row.disabled is the inverse of
	// Enabled, gating the "enable" button, because this engine's `when` has no
	// operator to write `row.enabled == false` with.
	// The /login screen's list. row.marker is the same fixed-width gutter the
	// providers screen wears; row.label and row.status are the two columns.
	"hub.rows": {"row.line": true, "row.status": true},
}

// RowSchema returns the signed `row.<field>` names for a list bind, or nil if
// the bind carries no row schema. Exported so the engine can prove its own
// projection keys match this contract rather than restating it.
func RowSchema(bind string) map[string]bool { return rowSchemas[bind] }

// RowSchemas returns the §4.7 inventory: for every list bind that carries a row
// schema, the sorted `row.<field>` names a row_template over it may address.
// Both the map and each slice are freshly built on every call.
//
// It exists for the reason SignedBinds does, one namespace over. The repair loop
// has to tell the model which names exist, and SignedBinds deliberately lists
// only the absolute namespace -- a `row.*` name is legal solely inside a
// row_template, so it cannot be in a flat list a scene may use anywhere. Without
// this the prompt told the model nothing about the relative vocabulary, and a
// model asked to show a team member's turn count had no way to learn the field
// is `row.turns`: the case then scored `incomplete` against the model for a
// vocabulary the harness never showed it. Exporting the inventory rather than
// copying it into the prompt keeps the validator the single source, so there is
// no fifth hand-maintained copy of the list to drift.
func RowSchemas() map[string][]string {
	out := make(map[string][]string, len(rowSchemas))
	for bind, fields := range rowSchemas {
		names := make([]string, 0, len(fields))
		for f := range fields {
			names = append(names, f)
		}
		sort.Strings(names)
		out[bind] = names
	}
	return out
}

// validateBindsScoped walks a subtree, carrying the access path (so a refusal
// names where it happened) and the row scope in effect. The scope is non-nil
// only inside a row_template: it holds the source list's bind and the
// `row.<field>` names that template may address (D1 / BINDS.md §4.7). A `row.*`
// bind is checked against it, and refused with an address outside any template.
// validateBindsScoped walks a subtree, carrying the access path (so a refusal
// names where it happened), the row scope in effect, and the plugin scope in
// effect. The row scope is non-nil only inside a row_template: it holds the
// source list's bind and the `row.<field>` names that template may address (D1 /
// BINDS.md §4.7). The plugin scope is document-wide when set (this document is
// being composed from one plugin's fragments), so it is passed unchanged at every
// recursion rather than opened at a subtree: a plugin bind is legal anywhere in
// its own fragment, not only under a marker node. A `row.*` bind is checked
// against the row scope; a `<plugin-id>.*` bind against the plugin scope.
func (d *Document) validateBindsScoped(n *Node, path string, scope map[string]bool, pscope *PluginScope) error {
	// Check this node's bind.
	if err := d.validateOneBind(n.Bind, "bind", n, path, scope, pscope); err != nil {
		return err
	}

	// Check when conditions (they reference the same namespace). A `when` may
	// carry an operator like "!=", so the bind is its leading field.
	if n.When != "" {
		if parts := strings.Fields(n.When); len(parts) > 0 && parts[0] != "" {
			if err := d.validateOneBind(parts[0], "when condition", n, path, scope, pscope); err != nil {
				return err
			}
		}
	}

	// A scroll is honoured on a marquee and refused with an address anywhere
	// else (G2 / SCENES.md Scene 4). Checked here, in the walk that reaches
	// prefix and suffix too, so a scroll on a nested node is refused at its own
	// position rather than only at the root.
	if err := d.validateScroll(n, path, scope, pscope); err != nil {
		return err
	}

	// A reveal is honoured on a text node and refused with an address elsewhere
	// (G3 / SCENES.md Scene 4). Same walk, same reason as scroll above: a reveal
	// on a nested node is refused at its own position.
	if err := d.validateReveal(n, path); err != nil {
		return err
	}

	// An enter with row:true needs a stagger token and rows to stagger over
	// (G4 / SCENES.md Scene 4). Same walk, same reason as scroll and reveal
	// above: a mis-declared enter on a nested node is refused at its own position.
	if err := d.validateEnter(n, path); err != nil {
		return err
	}

	// An on_press names an action in the closed cmd:/focus:/answer: grammar
	// (H8 / BINDS.md §4.8). Checked here, in the same walk, so a malformed action
	// on a nested node is refused at its own position; the row scope is passed so
	// a {row.<field>} interpolation inside a template is checked against the row
	// schema (Q20).
	if err := d.validateOnPress(n, path, scope); err != nil {
		return err
	}

	// A {row.<field>} reference in drawn text is refused with an address: only
	// on_press interpolates, so anywhere else the braces would be painted
	// literally (see validateNoRowInterpolationInText).
	if err := d.validateNoRowInterpolationInText(n, path); err != nil {
		return err
	}

	// Recurse into children, carrying the same scopes: a node nested under a
	// template row is still inside that template and may still read row.*, and a
	// node anywhere in a plugin fragment may still read the plugin namespace.
	for i, child := range n.Children {
		if err := d.validateBindsScoped(child, childPath(path, i), scope, pscope); err != nil {
			return err
		}
	}
	if prefix := n.PrefixNode(); prefix != nil {
		if err := d.validateBindsScoped(prefix, prefixPath(path), scope, pscope); err != nil {
			return err
		}
	}
	if n.Suffix != nil {
		if err := d.validateBindsScoped(n.Suffix, suffixPath(path), scope, pscope); err != nil {
			return err
		}
	}
	// A row_template opens a new scope from this list's own bind. A template
	// over a bind that signs no row schema (a scalar, or an unknown source) is
	// refused here: a template has no rows to instantiate over, and the author
	// is better told that than left with a list that silently draws nothing.
	if n.RowTemplate != nil {
		childScope := rowSchemas[n.Bind]
		if childScope == nil {
			return &Error{
				Loc: d.locOf(path),
				Msg: fmt.Sprintf("row_template on node type %q binds %q, which signs no row schema in BINDS.md §4.7; a template needs an array-of-objects bind (team.members, agent.todos, slash.matches) to instantiate rows over", n.Type, n.Bind),
			}
		}
		if err := d.validateBindsScoped(n.RowTemplate, templatePath(path), childScope, pscope); err != nil {
			return err
		}
	}

	// The node's own binds and everything below it are checked first, and
	// only then is an unrendered field refused. Order matters: a mistyped
	// bind inside a template is the more specific complaint, and a reader
	// who wrote "totaly.invented" is better served by being told which bind
	// is unsigned than by being told the containing field is unsupported.
	if err := d.refuseUnrendered(n, path); err != nil {
		return err
	}

	return nil
}

// validateOneBind refuses a bind that is neither a signed absolute bind nor a
// legal relative one. A `row.*` bind is legal only inside a row_template and
// only when its field is in that template's source schema; a `<plugin-id>.*` bind
// is legal only inside that plugin's fragment and only when the plugin declares
// it (H-C / BINDS.md §4.4); every other bind must appear in the §4.5 inventory.
// `where` names the field for the message ("bind" or "when condition").
func (d *Document) validateOneBind(bind, where string, n *Node, path string, scope map[string]bool, pscope *PluginScope) error {
	if bind == "" {
		return nil
	}
	if strings.HasPrefix(bind, "row.") {
		if scope == nil {
			return &Error{
				Loc: d.locOf(path),
				Msg: fmt.Sprintf("relative bind %q in %s of node type %q is only legal inside a row_template (BINDS.md §4.7); there is no row to be relative to here", bind, where, n.Type),
			}
		}
		if !scope[bind] {
			return &Error{
				Loc: d.locOf(path),
				Msg: fmt.Sprintf("relative bind %q in %s of node type %q is not in the row schema of the enclosing list (BINDS.md §4.7); check the field name against the list's element type", bind, where, n.Type),
			}
		}
		return nil
	}
	// A signed host bind resolves first, so a plugin id can never shadow host
	// state: even a plugin whose id spells a host namespace (id "agent",
	// "agent.working") cannot capture the host field, because the inventory is
	// consulted before the plugin scope.
	if signedBinds[bind] {
		return nil
	}
	// The ui.max.is.<id> family (BINDS.md §4.3): truthy when ui.max equals the
	// suffix, the comparison `when` has no operator for. It is signed as a family
	// rather than four exact binds so a downloaded dashboard names its own panes
	// without a host change, the ui.plugin.<id> shape. The suffix must be
	// non-empty: "ui.max.is." with nothing after it compares ui.max to "", which
	// is what ui.max.none already answers, so an empty suffix is a scene mistake
	// (it would gate the maximized view on "nothing maximized") rather than a
	// legal address.
	if rest, ok := strings.CutPrefix(bind, MaxIsBindPrefix); ok {
		if rest == "" {
			return &Error{
				Loc: d.locOf(path),
				Msg: fmt.Sprintf("bind %q in %s of node type %q names no pane id after %q; the ui.max.is.<id> family (BINDS.md §4.3) gates on which pane is maximized, so it needs the pane id — use ui.max.none to gate on nothing being maximized", bind, where, n.Type, MaxIsBindPrefix),
			}
		}
		return nil
	}
	// Not a host bind. It is a legal plugin bind iff a plugin scope is in effect
	// and this bind sits in that plugin's own `<id>.` namespace; the declared-vs-
	// used check then decides it. A bind outside the scope's namespace (a
	// different prefix, or no scope at all) is an ordinary unsigned bind.
	if pscope != nil && strings.HasPrefix(bind, pscope.ID+".") {
		return d.validatePluginBind(bind, where, n, path, pscope)
	}
	return &Error{
		Loc: d.locOf(path),
		Msg: fmt.Sprintf("unsigned bind %q in %s of node type %q; every bind must appear in BINDS.md §4.5", bind, where, n.Type),
	}
}

// validatePluginBind is the H-C declared-vs-used check for a bind already known
// to sit in the scope plugin's own namespace (BINDS.md §4.4). It refuses three
// ways, each named so the author knows which half to fix:
//
//   - The plugin declares no binds at all (a declarative manifest: no executable,
//     so no `binds`). Its namespace is empty because it streams nothing, so every
//     use is undeclared — the load-time face of the declarative/behavioral split
//     (ADR-0006). A zero-code plugin may bind only host fields.
//   - The plugin declares binds but not this field. The field name is wrong or
//     the declaration is missing; the manifest's `binds` map is the single source.
//   - The field is declared but used under a node type its `kind` cannot draw.
//     The kind is the axis the value rides (a scalar string vs a numeric series),
//     the plugin-bind analogue of the axis a scroll/reveal prop rides, so the
//     wrong pairing is refused rather than rendered as a silent mismatch.
func (d *Document) validatePluginBind(bind, where string, n *Node, path string, pscope *PluginScope) error {
	kind, declared := pscope.Binds[bind]
	if !declared {
		if len(pscope.Binds) == 0 {
			return &Error{
				Loc: d.locOf(path),
				Msg: fmt.Sprintf("bind %q in %s of node type %q is in plugin %q's own namespace, but plugin %q declares no binds — a declarative plugin (no executable) streams nothing, so its namespace is empty and the use is undeclared (BINDS.md §4.4); a zero-code plugin may bind only host fields", bind, where, n.Type, pscope.ID, pscope.ID),
			}
		}
		return &Error{
			Loc: d.locOf(path),
			Msg: fmt.Sprintf("bind %q in %s of node type %q is in plugin %q's namespace but the plugin declares no such field in its binds map (BINDS.md §4.4); declare it in the manifest, or correct the field name", bind, where, n.Type, pscope.ID),
		}
	}
	legal, known := bindKindNodeTypes[kind]
	if !known {
		return &Error{
			Loc: d.locOf(path),
			Msg: fmt.Sprintf("plugin %q declares bind %q with kind %q, which is not a known bind kind; the set is closed at %s (BINDS.md §4.4) and widens per node as a signed change", pscope.ID, bind, kind, bindKindList()),
		}
	}
	if !legal[n.Type] {
		return &Error{
			Loc: d.locOf(path),
			Msg: fmt.Sprintf("bind %q is declared kind %q but is used on node type %q, which cannot draw that kind (BINDS.md §4.4); a %q bind is legal on node types %s — the kind is the axis the value rides, so the wrong node would draw nothing", bind, kind, n.Type, kind, bindKindNodeTypeList(kind)),
		}
	}
	return nil
}

// validateScroll refuses a scroll that cannot be honoured, with an address.
//
// scroll rides the horizontal-offset axis, and only a marquee draws that axis
// (SCENES.md Scene 4, G-B). A scroll on any other node type is a scene defect
// the same way a row.* bind outside a template is: the author wrote a prop the
// format cannot honour there, and is better told than left with a node that
// silently ignores it. The refusal names `scroll` so the universal-property
// audit — which asks that every universal be honoured or refused-by-name — can
// attribute it, and carries a Loc so the repair loop can find it.
//
// speed is refused when non-positive rather than clamped: a zero or negative
// speed either never advances or runs the marquee backward, and clamping would
// hide the author's mistake behind a marquee that looks stuck. pause_when, when
// present, is a bind and is checked as one — the same net a `when` gets — so a
// misspelled pause bind is refused here rather than silently never pausing.
func (d *Document) validateScroll(n *Node, path string, scope map[string]bool, pscope *PluginScope) error {
	if n.Scroll == nil {
		return nil
	}
	if n.Type != "marquee" {
		return &Error{
			Loc: d.locOf(path),
			Msg: fmt.Sprintf("node type %q declares scroll, which rides the horizontal-offset axis only a marquee draws (SCENES.md Scene 4); a scroll on a %q node is a scene defect — move it onto a marquee or remove it", n.Type, n.Type),
		}
	}
	if n.Scroll.Speed <= 0 {
		return &Error{
			Loc: d.locOf(path),
			Msg: fmt.Sprintf("marquee declares scroll with speed %d; speed is cells-per-tick and must be positive (SCENES.md Scene 4) — a non-positive speed ticks forever without moving, which is a defect, not a pause (use pause_when to hold the marquee)", n.Scroll.Speed),
		}
	}
	if n.Scroll.PauseWhen != "" {
		if err := d.validateOneBind(n.Scroll.PauseWhen, "scroll pause_when", n, path, scope, pscope); err != nil {
			return err
		}
	}
	return nil
}

// validateReveal refuses a reveal that cannot be honoured, with an address.
//
// reveal rides the character-count axis — a growing prefix of the node's own
// text — and only a text node draws that axis (SCENES.md Scene 4, G-B). The
// other content-bearing nodes are excluded on purpose rather than by oversight:
// a marquee already owns the horizontal-offset axis (scroll), and composing two
// motions on one node is a fifth-axis question G-B does not sign; markdown lays
// out multiple lines, so "a growing prefix of the content" has no single
// meaning there. A reveal on any of them is a scene defect the same way a
// row.* bind outside a template is: the author wrote a prop the format cannot
// honour there, and is better told than left with a node that silently ignores
// it.
//
// The anim token is not checked here. A token's existence is a theme question —
// ValidateTokens answers it against the active theme, the same place a style
// token is checked — and this walk has no theme. An empty Anim is legal: it
// means anim.default (Q8).
func (d *Document) validateReveal(n *Node, path string) error {
	if n.Reveal == nil {
		return nil
	}
	if n.Type != "text" {
		return &Error{
			Loc: d.locOf(path),
			Msg: fmt.Sprintf("node type %q declares reveal, which rides the character-count axis only a text node draws (SCENES.md Scene 4); a reveal on a %q node is a scene defect — move it onto a text node or remove it", n.Type, n.Type),
		}
	}
	return nil
}

// validateEnter refuses an enter that cannot be honoured, with an address.
//
// enter rides the row-count axis: with row:true the container staggers its
// rows, so it needs both a stagger interval to schedule against and rows to
// schedule (SCENES.md Scene 4, G-B). Two refusals follow, and neither is a
// no-op the way a silent drop would be:
//
//   - row:true with no stagger token. A stagger names the inter-row delay; with
//     no interval there is nothing to stagger, so the author wrote a scheduler
//     with no schedule. It is refused rather than defaulted because the whole
//     point of row:true is the delay, and a zero delay is the row:false case the
//     author did not ask for — telling them is cheaper than drawing something
//     they did not mean.
//   - row:true on a node with no rows. The row-count axis is a container's:
//     enter staggers `children` (a stack/row/box/overlay) or the rows of a
//     row_template (a list). A row:true enter on a leaf — a text or a marquee,
//     with neither children nor a template — has no rows to bring in one at a
//     time, and is a scene defect the same way a reveal off a text node is: the
//     axis a prop rides is part of its signature.
//
// row:false carries neither refusal: the whole-container entrance applies to
// any node (it dims the node's subtree as one unit, and a leaf's subtree is
// itself), exactly as transition is universal. The stagger token, when row:true
// names one, is checked against the active theme by ValidateTokens rather than
// here, for validateReveal's reason: this walk has no theme, and an empty
// stagger under row:true is already refused above before any token lookup would
// run.
func (d *Document) validateEnter(n *Node, path string) error {
	if n.Enter == nil || !n.Enter.Row {
		return nil
	}
	if n.Enter.Stagger == "" {
		return &Error{
			Loc: d.locOf(path),
			Msg: fmt.Sprintf("node type %q declares enter with row:true but no stagger token; row:true is the per-row scheduler and its stagger names the inter-row delay (SCENES.md Scene 4) — a stagger with no interval has nothing to schedule, so name a timing token or drop row:true for the whole-container entrance", n.Type),
		}
	}
	if len(n.Children) == 0 && n.RowTemplate == nil {
		return &Error{
			Loc: d.locOf(path),
			Msg: fmt.Sprintf("node type %q declares enter with row:true but has no rows to stagger; the row-count axis brings a container's children or a list's row_template in one at a time (SCENES.md Scene 4), and a %q node with neither has none — move the enter onto the container, or use row:false for the whole-node entrance", n.Type, n.Type),
		}
	}
	return nil
}

// validateOnPress refuses an on_press whose action is malformed, with an address.
//
// on_press graduated from the unrenderedFields refusal to this typed one in H8,
// the same graduation scroll (G2) and reveal (G3) made: the field was refused
// wholesale while the action vocabulary was unsigned, and now that BINDS.md §4.8
// signs the closed prefix set the validator checks the value rather than
// rejecting the key. Unlike scroll and reveal there is no node-type refusal —
// on_press is universal (SCENES.md Scene 8, Q18), any node may be pressable — so
// the only thing to refuse is a value the closed grammar does not accept.
//
// ParseAction is the single reader of the grammar (the host dispatcher calls the
// same function), so a prefix outside cmd:/focus:/answer:, an empty argument, an
// unknown answer kind, and the reserved ext: arm are all refused here with the
// message ParseAction composes — the same net every other field gets, addressed
// with file:line so the Phase 2 repair loop can act on it.
//
// When the on_press sits inside a row_template its argument may interpolate
// {row.<field>} (Q20), and each such reference is checked against the enclosing
// template's row schema exactly as a bare row.* bind is (§4.7): a {row.foo} the
// element type does not declare is a load-time refusal, not a token that silently
// fails to expand at press time. A {row.*} outside any template is refused the
// same way a bare row.* bind outside a template is — relative interpolation needs
// a row to be relative to.
func (d *Document) validateOnPress(n *Node, path string, scope map[string]bool) error {
	if n.OnPress == "" {
		return nil
	}
	action, err := ParseAction(n.OnPress)
	if err != nil {
		return &Error{Loc: d.locOf(path), Msg: fmt.Sprintf("node type %q: %s", n.Type, err)}
	}
	for _, token := range interpolationTokens(action.Arg) {
		if scope == nil {
			return &Error{
				Loc: d.locOf(path),
				Msg: fmt.Sprintf("on_press on node type %q interpolates %q, but relative interpolation is only legal inside a row_template (BINDS.md §4.7/§4.8); there is no row to be relative to here", n.Type, "{"+token+"}"),
			}
		}
		if !scope[token] {
			return &Error{
				Loc: d.locOf(path),
				Msg: fmt.Sprintf("on_press on node type %q interpolates %q, which is not in the row schema of the enclosing list (BINDS.md §4.7); check the field name against the list's element type", n.Type, "{"+token+"}"),
			}
		}
	}
	return nil
}

// does not draw. Accepting one is the failure mode this project has now paid
// for three times: a style key the validator learned and styleName() did not,
// a border token checked by the validator and dropped by both drawing paths,
// and this. All three report success and show the wrong screen — the outcome
// with no diagnostic anywhere, because clearing validation is precisely the
// signal that says the document is fine.
//
// `row_template` was the most expensive of these, because it reaches past a
// scene and into the instrument: internal/eval's CollectBinds walks templates
// by name, citing SCENES.md Q10, so a corpus answer that satisfied must_bind
// only inside a template would have scored converged while the list rendered
// "[…]". It was refused until D1 signed the `row.*` namespace (BINDS.md §4.7)
// and the engine's renderRowTemplate learned to draw it; its entry has left
// this map, and validateBinds now checks relative binds against the source
// list's row schema. That graduation is exactly what this map exists to permit:
// a field leaves the moment the renderer draws it, and unrendered_test.go is
// what notices if the map and the renderer ever disagree again.
//
// `on_press` and `scroll` were the same class one step earlier, and they are
// the reason this map's generality had to be real before they could be added.
// SCENES.md calls both universal; neither was a field on Node, so
// encoding/json discarded the key without a word — a scene declaring either
// parsed, validated and rendered byte-identically to one that did not. That is
// worse than the unrendered-field case above, because there was no field for
// the audit to enumerate: it reported full coverage *because* the property was
// missing.
//
// It also broke a rule the project signs elsewhere. PLAN.md's
// forward-compatibility contract is "unknown-but-parseable is a warning", and
// the engine honours it for node *types* — `button`, `switch` and
// `slider` are documented, unimplemented, and each draws
// [[UNKNOWN NODE TYPE]], so a v0 document keeps booting under v1 and the
// screen says what it could not do. (`sparkline` graduated: it draws a plugin
// `series` as block glyphs, so it renders rather than placeholding.) Properties
// had the opposite behaviour, and the silent class was the one the
// documentation called universal.
//
// `scroll` has since graduated (G2): its render semantics are signed (SCENES.md
// Scene 4, G-B) and the host clock is signed (ADR-0005), so the engine draws it
// on a marquee and validateScroll refuses it elsewhere with an address — a
// rendering plus a typed refusal, no longer a blanket unrendered-field refusal.
// `on_press` has now graduated too (H8): BINDS.md §4.8 signs the closed action
// vocabulary (cmd:/focus:/answer:), validateOnPress refuses a malformed action
// with an address, and the host loop dispatches a well-formed one — so a valid
// on_press loads and is acted on rather than refused wholesale. It was the last
// standing entry, so the map is empty today.
//
// An empty map is not a dead mechanism. The refusal machinery stays wired and is
// exercised by the runtime-probe tests (unrendered_test.go injects an entry and
// asserts the validator's behaviour changes), and the moment a field is added to
// Node that the engine cannot yet draw and no verb yet reads, it earns an entry
// here and gets an addressed refusal for free — the forward-compatibility net
// PLAN.md signs. The three historical entries (row_template D1, scroll G2,
// on_press H8) each left the moment their behaviour landed, which is exactly the
// graduation this map exists to permit.
var unrenderedFields = map[string]string{}

// refuseUnrendered reports a field the validator understands and the renderer
// ignores. The message says "not yet rendered" rather than "invalid" on
// purpose: the document is well-formed and the author spelled the field
// correctly, so telling them it is malformed sends them hunting for a typo
// that is not there. Phase 2's repair loop reads these messages, and a wrong
// diagnosis costs a turn the corpus then charges to the model.
//
// It asks which unrendered fields *this node declares*, rather than assuming
// the answer. The first version took the map's only key as a constant —
// `unrenderedFields["row_template"]` — and was called only from the
// RowTemplate arm, so the two halves agreed by coincidence and the map's
// shape was decoration. That made the audit in unrendered_audit_test.go
// unsound in the direction nobody checks: its advertised remedy is "add it to
// scene.unrenderedFields so the validator refuses it with an address", and
// taking that advice silenced the audit while refusing nothing. A guard whose
// documented remedy is a no-op is worse than no guard, because it converts a
// real finding into a closed ticket.
// It asks two sources and refuses a field named by either, because neither
// alone answers the question. `declaredUnrenderedFields` reconstructs the
// answer from the node's *values*, and omitempty makes that reconstruction
// lossy in exactly one direction: a key written with its type's zero value
// (`"on_press": ""`) marshals away, so the node cannot report it. That is not
// a hypothetical spelling — it is what an author writes while clearing a
// property they are mid-way through removing, and it was accepted silently
// while `"scroll": null` beside it was refused, the difference being only
// that json.RawMessage keeps its bytes and a string does not. The parser's
// `declaredKeys` is the exact record of what the source wrote, so it closes
// that hole.
//
// The union rather than a replacement, and the reason is measured: a Document
// built by hand — in a test, or by the patch path Phase 2 designs — has no
// source text and therefore no declaredKeys at all. Reading only the parser's
// record would refuse nothing for those, turning this guard off for every
// caller that does not come from a file, which is the direction that deletes
// a guard rather than loosening it.
func (d *Document) refuseUnrendered(n *Node, path string) error {
	declared := n.declaredUnrenderedFields()
	seen := make(map[string]bool, len(declared))
	for _, field := range declared {
		seen[field] = true
	}
	for _, key := range d.declaredKeys[path] {
		if !seen[key] {
			seen[key] = true
			declared = append(declared, key)
		}
	}
	// Sorted for the same reason declaredUnrenderedFields sorts: with two
	// unrendered fields on one node, an address that moves between runs is
	// not an address.
	sort.Strings(declared)

	for _, field := range declared {
		because, ok := unrenderedFields[field]
		if !ok {
			continue
		}
		return &Error{
			Loc: d.locOf(path),
			Msg: fmt.Sprintf("node type %q declares %q, which is accepted by the validator "+
				"but not yet rendered by the engine (%s); the field would be silently "+
				"dropped, so it is refused instead", n.Type, field, because),
		}
	}
	return nil
}

// ValidateTokens checks that every token referenced in the scene is defined in
// the theme. It walks the entire tree and collects all undefined token
// references, so the caller sees the full scope of the problem in one pass.
// Nodes without a style.token attribute are skipped — an absent token is not an error.
func ValidateTokens(doc *Document, thm *theme.Theme) []TokenError {
	var errs []TokenError
	if doc != nil && doc.Root != nil {
		doc.collectTokenErrors(doc.Root, nodePathRoot, thm, &errs)
	}
	return errs
}

// styleTokenKeys are the keys a node may name its style token under.
//
// Two rather than one, and the second is the one that matters in practice:
// "style" is what the three golden scenes, every example in SCENES.md and
// TOKENS.md, and styleName() in the render path all use, while "token" was the
// only key this validator originally read. Checking just "token" made the
// validator blind to the sole spelling that actually occurs — SOBRIA
// referenced the undefined token "header" twice and validated clean, so the
// factory interface failed the rule the product enforces on downloaded scenes.
//
// "token" is kept rather than replaced because it is already written into
// corpus cases and tests, and silently rejecting it would turn a validator fix
// into a format break. Accepting both costs one extra lookup; TOKENS.md's
// promise is that every style reference is checked, and a key this validator
// understood yesterday is a reference.
var styleTokenKeys = [...]string{"token", "style"}

// StyleTokenKeys returns the keys a node may name its style token under, in
// the order the validator reads them. The slice is freshly built per call so a
// caller cannot mutate the validator's list by holding onto it.
//
// This is exported for the same reason SignedBinds is, and against the same
// cheaper-and-wrong alternative. Accepting a key here is a statement that
// scenes may be written that way, and every consumer of that statement — above
// all the render path, which has to turn the reference into a style — must read
// the same list or the statement is only half true. It already was: the
// validator learned "style" while styleName() in internal/engine kept reading
// only its own spelling, so a scene using the other accepted key validated
// clean and drew unstyled. That failure is silent by construction, because
// passing validation is exactly the signal that says nothing is wrong.
//
// The alternative was to write the key list out again in the render path. That
// is how this package's bind inventory drifted in both directions once already,
// and a style-key copy would drift the same way with a worse symptom: binds fail
// loudly at load, a missed style key just quietly renders the wrong screen.
func StyleTokenKeys() []string {
	return append([]string(nil), styleTokenKeys[:]...)
}

func (d *Document) collectTokenErrors(n *Node, path string, thm *theme.Theme, errs *[]TokenError) {
	// Check whichever key this node declares its style token under. At most
	// one error per node: the two keys are spellings of the same reference,
	// so reporting both would address the same node twice and make the
	// count of offenders depend on how the scene was spelled.
	for _, key := range styleTokenKeys {
		tokenName, ok := n.Style[key]
		if !ok || tokenName == "" {
			continue
		}
		if !thm.Has(tokenName) {
			*errs = append(*errs, TokenError{
				Token:    tokenName,
				NodeType: n.Type,
				Loc:      d.locOf(path),
			})
		}
		break
	}

	// Check border style token if present.
	if borderStyle := n.BorderStyleName(); borderStyle != "" {
		if !thm.Has(borderStyle) {
			*errs = append(*errs, TokenError{
				Token:    borderStyle,
				NodeType: n.Type + " border",
				Loc:      d.locOf(path),
			})
		}
	}

	// Check the reveal's timing token against the theme's anim section (G3).
	// A reveal resolves anim.default when it names none (Q8), so the token to
	// check is its Anim or "default"; either way an animation prop naming a
	// timing token the active theme does not define fails the load with an
	// address, the same net a style token gets (TOKENS.md). HasAnim, not Has:
	// the anim section is a separate namespace, so a style token spelled like
	// the timing token must not satisfy this.
	if n.Reveal != nil {
		token := n.Reveal.Anim
		if token == "" {
			token = "default"
		}
		if !thm.HasAnim(token) {
			*errs = append(*errs, TokenError{
				Token:    token,
				NodeType: n.Type + " reveal",
				Loc:      d.locOf(path),
			})
		}
	}

	// Check the transition's timing token against the theme's anim section (G1).
	// Same net as the reveal above and for the same reason (Q8, TOKENS.md): an
	// entrance resolves anim.default when it names none, and a transition naming a
	// timing token the active theme does not define fails the load with an address
	// rather than silently never animating. HasAnim, not Has: the anim section is a
	// separate namespace, so a style token spelled like the timing token must not
	// satisfy this. Unlike scroll and reveal there is no node-type refusal to pair
	// this with — the intensity axis is universal (see node.go) — so this token
	// check is transition's only load-time refusal.
	if n.Transition != nil {
		token := n.Transition.Anim
		if token == "" {
			token = "default"
		}
		if !thm.HasAnim(token) {
			*errs = append(*errs, TokenError{
				Token:    token,
				NodeType: n.Type + " transition",
				Loc:      d.locOf(path),
			})
		}
	}

	// Check the enter's stagger token against the theme's anim section (G4).
	// Only row:true consults a stagger — it is the inter-row delay the scheduler
	// runs against — and validateEnter has already refused a row:true enter with
	// an empty stagger, so a token reaching here is a non-empty name that must
	// resolve. row:false runs the whole-container entrance on anim.default and
	// names no stagger, so it is not checked here (there is no token to undefine).
	// HasAnim, not Has: the anim section is a separate namespace, so a style token
	// spelled like the stagger token must not satisfy this. NodeType names "enter"
	// so a reader can tell which prop's token was undefined when a node carries
	// more than one.
	if n.Enter != nil && n.Enter.Row && n.Enter.Stagger != "" {
		if !thm.HasAnim(n.Enter.Stagger) {
			*errs = append(*errs, TokenError{
				Token:    n.Enter.Stagger,
				NodeType: n.Type + " enter",
				Loc:      d.locOf(path),
			})
		}
	}

	// Recurse into children for nested structures (box nodes, overlays).
	for i, child := range n.Children {
		d.collectTokenErrors(child, childPath(path, i), thm, errs)
	}

	// Recurse into prefix/suffix nodes.
	if prefix := n.PrefixNode(); prefix != nil {
		d.collectTokenErrors(prefix, prefixPath(path), thm, errs)
	}
	if n.Suffix != nil {
		d.collectTokenErrors(n.Suffix, suffixPath(path), thm, errs)
	}
	if n.RowTemplate != nil {
		d.collectTokenErrors(n.RowTemplate, templatePath(path), thm, errs)
	}
}

// drawnTextFields names the string fields of a node the engine paints as
// authored, in the order a refusal reports them. They are the places an author
// (or a model) is tempted to write `{row.turns}` expecting a value, because
// on_press accepts exactly that spelling.
func drawnTextFields(n *Node) []struct{ name, value string } {
	return []struct{ name, value string }{
		{"text", n.Text},
		{"title", n.Title},
		{"placeholder", n.Placeholder},
	}
}

// validateNoRowInterpolationInText refuses a `{row.` reference in a node's drawn
// text. The substitution exists only for on_press (ExpandRowInterpolation runs at
// press time); a text node's `text` is painted verbatim, so `"text": "{row.turns}•"`
// validated clean and drew the braces on screen. That is the accepted-but-not-drawn
// class this validator exists to close, and the one outcome that reports success
// while showing the wrong screen: a model told "valid" has nothing to repair, so the
// eval corpus recorded a missing counter as the model's failure when the engine had
// let the mistake through.
//
// It is refused here, with the file:line of the node, rather than made to work.
// Interpolating in text is a format change -- a new signed rule, and a decision
// about escaping a literal brace -- that was deliberately deferred; the refusal
// names the working spelling (bind: "row.<field>") so the repair is one edit, and
// docs/DECISIONS-DEFERRED.md records the deferred alternative so the refusal is not
// mistaken for the final design.
//
// The check is a plain substring test on "{row." rather than a parse of
// well-formed tokens: a malformed `{row.turns` is the same author intent and the
// same silent literal on screen, and a refusal that required a closing brace would
// let that variant through. It applies outside templates too, because outside a
// template there is no row at all and the braces are equally dead.
func (d *Document) validateNoRowInterpolationInText(n *Node, path string) error {
	for _, f := range drawnTextFields(n) {
		if !strings.Contains(f.value, "{row.") {
			continue
		}
		return &Error{
			Loc: d.locOf(path),
			Msg: fmt.Sprintf("node type %q has %q containing %q, but {row.<field>} is substituted only in on_press; in drawn text the braces would appear literally on screen. To show a row field, bind it instead: a text node with \"bind\": \"row.<field>\" inside the row_template (a decorator such as a bullet goes in a separate text node)", n.Type, f.name, "{row."),
		}
	}
	return nil
}
