// Package patch is the /ui control surface: the deterministic half of
// PLAN.md's "control surface for the live instance". A command names a change
// to the scene document, the change is applied, and the result is validated
// before anything reaches the screen.
//
// # Why this package edits source bytes rather than the parsed tree
//
// The obvious implementation mutates *scene.Node in place and hands the tree
// back to the renderer. It was rejected on a measurement, not on taste. A
// scene.Document carries an address book — the source bytes, the file name and
// a byte offset per node path — and every refusal the validator produces is
// addressed from it. That book is built by the parser and by nothing else, so
// a Document assembled in memory has no offsets at all. Measured on the tree,
// with one document holding one refusable field:
//
//	parsed from bytes : SOBRIA.json:2:4: node type "text" declares "row_template" …
//	rebuilt by hand   : <scene>: node type "text" declares "row_template" …
//
// Same refusal, same reason, and the address is gone. That matters more here
// than anywhere else in the project, because Phase 2's whole thesis is the
// repair loop — "order → patch → validator error → retry" — and the validator
// error is the only input the retry gets. A /ui surface that mutated the tree
// would work perfectly for every command that succeeds and would degrade the
// diagnostics of exactly the commands that fail, which are the ones the loop
// exists to serve. It would also hand the agent-driven half of Phase 2 a
// second, addressless path into the same document, so the two halves of the
// feature would disagree about what a refusal looks like.
//
// So a patch is a source-to-source transformation: bytes in, bytes out,
// re-parsed through scene.ParseNamed. The new document is addressed in its own
// right, and the addresses point at the text the user would open in an editor.
// Round-trip fidelity was measured before this was built on: re-marshalling
// and re-parsing both shipped scenes is tree-stable and warning-stable
// (SOBRIA 0 warnings before and after, MAXIMUM likewise), so the rewrite does
// not quietly drop a property on the way through.
//
// # Why the document is re-validated rather than trusted
//
// Apply could assume that a command built from a valid vocabulary produces a
// valid document. It does not, because PLAN.md invariant 3 says an invalid
// patch never kills the session — the last good scene stays — and an
// invariant enforced by assumption is an invariant enforced nowhere. Every
// command here goes through the same Validate the boot path uses, so a
// command that composes into a refusable document is refused with an address
// instead of being drawn.
package patch

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// Result is the outcome of applying one /ui command.
//
// Both halves are returned rather than one-or-the-other because the caller
// needs them together: Doc is the scene to draw, Source is what to persist and
// to diff, and Summary is the human sentence the change-diff view shows before
// the change is trusted. A caller that got only the document would have to
// re-serialise it to show a diff, and a second serialisation is a second
// answer to "what did the command do".
type Result struct {
	Doc     *scene.Document
	Source  []byte
	Summary string
}

// Apply runs one /ui command against a scene document's source bytes.
//
// The document is passed as bytes and a name rather than as a *scene.Document
// for the reason the package comment argues: the address book cannot be
// rebuilt from a tree, so the source is the input of record. The name is the
// same one the caller parsed under, so a refusal produced here prints the path
// the user recognises.
//
// On any error the caller keeps the scene it already had. That is invariant 3
// and it is the caller's job, not this function's: returning a partially
// applied document would make the invariant depend on every call site
// remembering to discard it.
func Apply(name string, src []byte, command string) (Result, error) {
	cmd, err := Parse(command)
	if err != nil {
		return Result{}, err
	}
	return cmd.apply(name, src)
}

// Command is one parsed /ui invocation.
//
// The verbs are a closed set and the arguments are strings, which is the whole
// difference between this surface and the agent-driven one. PLAN.md splits the
// control surface in two — "deterministic (commands)" and "user/agent-directed
// (documents)" — and the split is load-bearing: a command the host can name is
// a command the host can validate, log and test, while a document the agent
// proposes can say anything and is therefore checked by the validator alone.
// Widening this struct towards arbitrary JSON would collapse the two halves
// into the weaker one.
type Command struct {
	Verb   string
	Target string
	Key    string
	Value  string
}

// Parse reads a /ui command line into a Command.
//
// It accepts the line with or without the leading "/ui", because the host
// strips the slash prefix before dispatch in one path and not in the other,
// and a parser that only accepted one spelling would make the two call sites
// disagree about what a valid command is.
//
// Unknown verbs are refused by name and the message lists what exists. The
// alternative — treating an unknown /ui line as a chat prompt — is the failure
// this project has catalogued repeatedly: an input that is silently
// reinterpreted reports success and does the wrong thing, and the user gets no
// signal that the command they typed is not a command.
func Parse(line string) (Command, error) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) > 0 && (fields[0] == "/ui" || fields[0] == "ui") {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return Command{}, fmt.Errorf("/ui needs a verb: one of %s", strings.Join(Verbs(), ", "))
	}

	verb := fields[0]
	args := fields[1:]
	switch verb {
	case "style":
		// /ui style <node-id> <token>
		if len(args) != 2 {
			return Command{}, fmt.Errorf("/ui style needs a node id and a style token: /ui style <node-id> <token>")
		}
		return Command{Verb: "style", Target: args[0], Value: args[1]}, nil
	case "hide", "show":
		// /ui hide <node-id> / /ui show <node-id>
		if len(args) != 1 {
			return Command{}, fmt.Errorf("/ui %s needs a node id: /ui %s <node-id>", verb, verb)
		}
		return Command{Verb: verb, Target: args[0]}, nil
	case "set":
		// /ui set <node-id> <key> <value…>
		if len(args) < 3 {
			return Command{}, fmt.Errorf("/ui set needs a node id, a key and a value: /ui set <node-id> <key> <value>")
		}
		return Command{Verb: "set", Target: args[0], Key: args[1], Value: strings.Join(args[2:], " ")}, nil
	default:
		return Command{}, fmt.Errorf("unknown /ui verb %q: expected one of %s", verb, strings.Join(Verbs(), ", "))
	}
}

// Verbs is the closed list of /ui verbs, exported so the host's command
// registry and the refusal messages read from one place.
//
// Three verbs, and the omissions are deliberate rather than unfinished. `add`
// and `move` are in PLAN.md's sketch of this surface and are not here, because
// both have to answer "where" — a position in a tree — and the addressing
// vocabulary for that (`below_input`, anchors, relative placement) is Scene 5
// and Phase 3 work that BINDS.md does not yet sign. Shipping a guessed spelling
// now is exactly what `row_template` was refused for: inventing format ahead of
// the phase meant to design it. The three here need no new vocabulary at all —
// they address a node by the id it already has and write a property the engine
// already reads — so they are the part of the surface that can be correct
// today.
func Verbs() []string { return []string{"hide", "set", "show", "style"} }

// apply performs the source-to-source edit and re-validates the result.
func (c Command) apply(name string, src []byte) (Result, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(src, &root); err != nil {
		return Result{}, fmt.Errorf("%s: scene is not a JSON object: %w", name, err)
	}

	// The edit walks a generic map tree rather than []*scene.Node on purpose.
	// scene.Node drops any key it does not declare, so editing through it
	// would silently delete every unknown property in the document on the way
	// past — and "unknown property" includes every field a future phase adds
	// and every field a third-party plugin fragment carries. A /ui command
	// that rewrote an unrelated part of the user's scene would be the worst
	// possible bug in a surface whose entire promise is that the user's
	// document is theirs.
	var tree any
	if err := json.Unmarshal(src, &tree); err != nil {
		return Result{}, fmt.Errorf("%s: scene is not valid JSON: %w", name, err)
	}

	found := false
	edited := walk(tree, func(node map[string]any) {
		if id, _ := node["id"].(string); id != c.Target {
			return
		}
		found = true
		c.mutate(node)
	})
	if !found {
		return Result{}, fmt.Errorf("%s: no node with id %q in this scene; /ui addresses nodes by the id they declare", name, c.Target)
	}

	out, err := json.MarshalIndent(edited, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("%s: could not re-serialise the patched scene: %w", name, err)
	}

	doc, err := scene.ParseNamed(name, out)
	if err != nil {
		return Result{}, err
	}
	if err := doc.RefuseEmpty(); err != nil {
		return Result{}, err
	}
	// Invariant 3: the patched document is held to the same rules as a
	// document loaded from disk. A command cannot buy its way past the
	// validator by virtue of having been typed by the user.
	if err := doc.Validate(); err != nil {
		return Result{}, err
	}

	return Result{Doc: doc, Source: out, Summary: c.summary()}, nil
}

// mutate writes this command's change into one node of the generic tree.
func (c Command) mutate(node map[string]any) {
	switch c.Verb {
	case "style":
		// The key is "style" inside the style object, not "token". Both
		// spellings validate — ValidateTokens accepts either — but only this
		// one is read by the renderer, and writing the other would produce a
		// scene that validates clean and draws unstyled. That is the exact
		// defect the README records under Phase 1, and a /ui command is the
		// last place it should be reintroduced, because the user typing
		// `/ui style x dim` has no way to see which spelling was written.
		style, _ := node["style"].(map[string]any)
		if style == nil {
			style = map[string]any{}
		}
		style["style"] = c.Value
		node["style"] = style
	case "hide":
		// Hiding is a `when` that never holds rather than a deletion: the node
		// keeps its id, so `/ui show` can find it again. A delete would make
		// hide irreversible from the command surface, and the user would have
		// to edit the file to undo a thing they did from inside the interface
		// — which inverts the promise this surface exists to make.
		node["when"] = hiddenCondition
	case "show":
		// Only the condition this surface wrote is removed. A `when` the user
		// or a scene author put there is theirs, and dropping it would make
		// /ui show a silent unhide of rows the document meant to gate.
		if w, _ := node["when"].(string); w == hiddenCondition {
			delete(node, "when")
		}
	case "set":
		node[c.Key] = c.Value
	}
}

// hiddenCondition is the `when` /ui hide writes.
//
// It names a bind that is signed and always absent, so the node is gated off
// by the engine's ordinary `when` evaluation rather than by a special case in
// the renderer. Using a real condition rather than inventing a "hidden": true
// property keeps hiding inside the format the user already has — the equality
// AGENTS.md calls the product — and means the change-diff view shows the user
// a line they could have written themselves.
const hiddenCondition = "ui.hidden"

// summary is the sentence the change-diff view shows before the patch is
// trusted. It states what changed in the user's own vocabulary, because a
// diff of re-indented JSON is not a description of a change.
func (c Command) summary() string {
	switch c.Verb {
	case "style":
		return fmt.Sprintf("styled %q as %q", c.Target, c.Value)
	case "hide":
		return fmt.Sprintf("hid %q", c.Target)
	case "show":
		return fmt.Sprintf("showed %q", c.Target)
	case "set":
		return fmt.Sprintf("set %s of %q to %q", c.Key, c.Target, c.Value)
	default:
		return c.Verb
	}
}

// walk visits every JSON object in the tree and calls fn on each, returning
// the tree so the caller reads the traversal as a transformation.
//
// It descends into arrays and into every object value, not only into
// "children". The node-bearing branches of the format are already four
// (children, prefix, suffix, row_template) and the count has grown once per
// phase; a walker that knew their names would fall behind the format silently,
// and the failure would be a /ui command that reports success and edits
// nothing because the node it targeted was under a branch the walker did not
// know about. Visiting everything cannot fall behind.
func walk(v any, fn func(map[string]any)) any {
	switch t := v.(type) {
	case map[string]any:
		fn(t)
		for k, child := range t {
			t[k] = walk(child, fn)
		}
		return t
	case []any:
		for i, child := range t {
			t[i] = walk(child, fn)
		}
		return t
	default:
		return v
	}
}
