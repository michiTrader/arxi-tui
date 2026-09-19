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
	return d.validateBinds(d.Root, nodePathRoot)
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
	"status.active":  true,
	"ui.focus":       true,
	"ui.max":         true,
	"ui.surface":     true,

	// §2 bootstrap set — host survival state the raw scene may display
	"user.input":           true,
	"user.input.submitted": true,
	"host.escape.armed":    true,
	"host.scene.error":     true,
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
	// Check this node's bind.
	if n.Bind != "" && !signedBinds[n.Bind] {
		return &Error{
			Loc: d.locOf(path),
			Msg: fmt.Sprintf("unsigned bind %q in node type %q; every bind must appear in BINDS.md §4.5", n.Bind, n.Type),
		}
	}

	// Check when conditions (they reference the same namespace).
	if n.When != "" {
		// When conditions may use operators like "!=", extract the bind path.
		parts := strings.Fields(n.When)
		if len(parts) > 0 {
			bindPath := parts[0]
			if !signedBinds[bindPath] && bindPath != "" {
				return &Error{
					Loc: d.locOf(path),
					Msg: fmt.Sprintf("unsigned bind %q in when condition of node type %q; every bind must appear in BINDS.md §4.5", bindPath, n.Type),
				}
			}
		}
	}

	// Recurse into children.
	for i, child := range n.Children {
		if err := d.validateBinds(child, childPath(path, i)); err != nil {
			return err
		}
	}

	// Recurse into prefix/suffix/template.
	if prefix := n.PrefixNode(); prefix != nil {
		if err := d.validateBinds(prefix, prefixPath(path)); err != nil {
			return err
		}
	}
	if n.Suffix != nil {
		if err := d.validateBinds(n.Suffix, suffixPath(path)); err != nil {
			return err
		}
	}
	if n.RowTemplate != nil {
		if err := d.validateBinds(n.RowTemplate, templatePath(path)); err != nil {
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

// unrenderedFields are constructions this package validates but internal/engine
// does not draw. Accepting one is the failure mode this project has now paid
// for three times: a style key the validator learned and styleName() did not,
// a border token checked by the validator and dropped by both drawing paths,
// and this. All three report success and show the wrong screen — the outcome
// with no diagnostic anywhere, because clearing validation is precisely the
// signal that says the document is fine.
//
// `row_template` is the most expensive of the three, because it reaches past a
// scene and into the instrument. internal/eval's CollectBinds walks templates
// by name, citing SCENES.md Q10, so a corpus answer that satisfies must_bind
// only inside a template scores converged while the list renders "[…]". That
// is reachable from a case the corpus already ships — raw-add-tasks-panel,
// whose order is "put a tasks panel on the right" — so Phase 2's number could
// have recorded a model success for a document that draws an empty panel.
//
// The fix is a refusal rather than an implementation, deliberately. The field's
// semantics are relative binds (`row.kind`), and `row.*` is signed nowhere in
// BINDS.md: it is Scene 5, which is Phase 3. Rendering it "somehow" now would
// invent format ahead of the phase meant to design it. When the engine learns
// to draw one of these, its entry leaves this map and the guard in
// unrendered_test.go is what notices the map and the renderer disagree.
// `on_press` and `scroll` are the same class one step earlier, and they are
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
// the engine honours it for node *types* — `button`, `switch`, `slider` and
// `sparkline` are documented, unimplemented, and each draws
// [[UNKNOWN NODE TYPE]], so a v0 document keeps booting under v1 and the
// screen says what it could not do. Properties had the opposite behaviour, and
// the silent class was the one the documentation called universal.
//
// They are refused rather than implemented for row_template's reason: both are
// behaviour, which PLAN.md schedules for Phase 3 (`on_press`, the action
// vocabulary of SCENES.md Q18) and Phase 4 (`scroll`, the animation clock of
// Q8/Q9). Inventing either now would build format ahead of the phase meant to
// design it. A refusal costs the author one addressed message and costs the
// project nothing it has to keep.
var unrenderedFields = map[string]string{
	"row_template": "relative binds inside templates are SCENES.md Q10 / Scene 5, " +
		"and the `row.*` namespace they need is signed nowhere in BINDS.md yet",
	"on_press": "the action vocabulary is closed per surface (SCENES.md Q18) and " +
		"dispatch is Phase 3 interaction work; no node type presses anything yet",
	"scroll": "scroll: {speed, pause_when} runs on the host animation clock " +
		"(SCENES.md Q8/Q9, Scene 4), which PLAN.md schedules after the golden set",
}

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
func (d *Document) refuseUnrendered(n *Node, path string) error {
	for _, field := range n.declaredUnrenderedFields() {
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
// validator blind to the sole spelling that actually occurs — SOARIA
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
