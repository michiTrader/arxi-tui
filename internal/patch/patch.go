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
	"sort"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/ext"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
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
	// Diff is the line-level change from the source Apply was given to the
	// source it produced, computed against the canonical form of both so that
	// reformatting is not reported as a change. It is the data the change-diff
	// view renders before the patch is trusted (PLAN.md, Phase 2): the Summary
	// says what changed in the user's vocabulary, the Diff shows it in the
	// document's.
	Diff Diff

	// ViewState is set only by the `hide`/`show` verbs, and it is what makes
	// them different in kind from every other verb here. add/move/set/style are
	// source-to-source edits of the scene document; hide/show write host-owned
	// view state (`ui.hidden`, BINDS.md §4.3), which has no representation in the
	// document source at all — the engine walk consumes it as a visibility
	// filter. So these verbs leave Source and Diff untouched (the document the
	// user has does not change) and hand the caller the set mutation to apply to
	// its own `ui.hidden`. A nil pointer means the command was an ordinary
	// source edit; a non-nil one means "do not diff, apply this to view state".
	ViewState *ViewStateOp

	// Tokens is set only by the `plugin` verb (add/remove) and carries the
	// token-layer change the mount or unmount makes, so the host can recompose
	// its active theme (H4). Unlike ViewState it is *not* exclusive with a
	// source edit: a `plugin add` both changes the document (its mounted
	// fragments live in Doc/Source) and contributes tokens, so Tokens rides
	// alongside the source edit rather than replacing it. A `plugin remove`
	// likewise removes both the mounted nodes (Doc/Source) and the token layer
	// (Tokens). A nil pointer means the command touched no token layer — every
	// non-plugin verb leaves it nil.
	Tokens *PluginTokens
}

// PluginTokens is the token-layer change a `plugin add`/`remove` makes, handed
// to the host so it can recompose the active theme. Like ViewStateOp it carries
// the op rather than the resulting theme: the layered composition lives in the
// loop — which holds the factory base and one layer per mounted plugin — so a
// second copy of it here would be a second answer to "which tokens are active".
//
// The precedence TOKENS.md signs is `user > plugin > factory`, and it is the
// order the host layers the three (Merge(Merge(factory, plugin), user)), not
// anything encoded here: this op only says "this plugin's layer changed". ID
// names the plugin whose layer it is — the same id the mounted node prefix uses
// — so the host keys its layer set by it and a remove drops exactly the layer a
// prior add contributed. Theme is the plugin's parsed token block on an add
// (empty, never nil, when the plugin contributes no tokens, so the host merges a
// no-op rather than nil-checking); Remove is true on an unmount, when Theme is
// nil and only the id is needed to drop the layer.
type PluginTokens struct {
	ID     string
	Theme  *theme.Theme
	Remove bool
}

// ViewStateOp is a mutation of host-owned view state produced by `hide`/`show`.
//
// It carries the set operation rather than the resulting set because the set
// lives in the loop, not here: the patch surface is stateless across commands
// (Apply takes the document bytes fresh each call), so it cannot hold the
// accumulated `ui.hidden` and must not try — a second copy of that set would be
// a second answer to "what is hidden". The loop owns the set and applies the op.
type ViewStateOp struct {
	// Hide is the ids to add to `ui.hidden`; Show is the ids to remove. Exactly
	// one is populated per op. `/ui show *` sets ShowAll, which clears the set
	// regardless of its contents — the one operation that names no id.
	Hide    []string
	Show    []string
	ShowAll bool
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
	return ApplyWithFetch(name, src, command, nil)
}

// Fetcher retrieves the bytes of a plugin manifest named by a URL, for the
// `/ui plugin add <url>` verb.
//
// It is an interface, and the network lives behind it, for the same reason
// internal/eval keeps the only real HTTP client out of the loop: this package
// is a pure source-to-source transform everywhere else, and a manifest URL is
// the one input that cannot be resolved without egress. Keeping the fetch
// injected means patch stays testable offline (a fake Fetcher returns fixture
// bytes) and the one real implementation lives in the host, where a network
// call belongs — not compiled into the mutation surface every /ui command
// shares. It returns the name the manifest should be addressed under (the URL)
// alongside the bytes, so a refusal inside a fetched manifest still names where
// it came from.
type Fetcher interface {
	Fetch(rawURL string) (name string, data []byte, err error)
}

// ApplyWithFetch is Apply with a Fetcher supplied, for the one verb that needs
// it. `/ui plugin add <url>` cannot be resolved without fetching the manifest,
// so the host passes its network Fetcher here; every other verb ignores it, and
// Apply is the fetch-free spelling for the callers (and tests) that never touch
// the plugin path. A nil Fetcher is legal: only `plugin add` consults it, and
// it is refused with a clear message rather than a nil-pointer panic when the
// context has no network.
func ApplyWithFetch(name string, src []byte, command string, fetch Fetcher) (Result, error) {
	cmd, err := Parse(command)
	if err != nil {
		return Result{}, err
	}
	return cmd.apply(name, src, fetch)
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

	// Where and Fragment carry the `add` verb's extra arguments, which the
	// other verbs do not have. `add` is the first verb whose argument is a
	// *position* rather than a node's own id, so it needs the write-path
	// addressing vocabulary docs/ADDRESSING.md signs: Where holds the resolved
	// form ("above"/"below"/"into"/"into_top"/"below_input"/"above_input") and
	// Target the anchor id it acts on (empty for the two semantic anchors, whose
	// whole point is to address the input node's role without naming its id).
	// Fragment is the raw JSON of the node to insert, kept as bytes rather than
	// re-tokenised so a text value with runs of spaces survives verbatim.
	Where    string
	Fragment string

	// Subject is the id of the node `move` relocates. `move` reuses the same
	// Where/Target the add resolver reads — Target is the anchor of the where
	// clause, not the node being moved — so the moved node needs its own field.
	// Keeping them distinct is what lets `move status above chat` name two nodes
	// without either meaning shadowing the other.
	Subject string
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
	body := strings.TrimSpace(line)
	// Strip a leading "/ui" or "ui" from the raw string as well as from the
	// token list. `add` reads its fragment out of the raw remainder, not out of
	// re-joined fields, because a text value with runs of spaces must survive
	// verbatim — Fields would collapse them.
	for _, p := range []string{"/ui", "ui"} {
		if body == p {
			body = ""
			break
		}
		if strings.HasPrefix(body, p+" ") {
			body = strings.TrimSpace(body[len(p):])
			break
		}
	}

	fields := strings.Fields(body)
	if len(fields) == 0 {
		return Command{}, fmt.Errorf("/ui needs a verb: one of %s", strings.Join(Verbs(), ", "))
	}

	verb := fields[0]
	args := fields[1:]
	switch verb {
	case "add":
		// /ui add node <where> <fragment>. Parsed against the raw body so the
		// fragment keeps its exact bytes.
		return parseAdd(body)
	case "move":
		// /ui move <id> <where>. Parsed against the raw body so it reads the
		// same where grammar `add` does, through the one shared parser.
		return parseMove(body)
	case "style":
		// /ui style <node-id> <token>
		if len(args) != 2 {
			return Command{}, fmt.Errorf("/ui style needs a node id and a style token: /ui style <node-id> <token>")
		}
		return Command{Verb: "style", Target: args[0], Value: args[1]}, nil
	case "hide", "show":
		// hide/show write the `ui.hidden` view-state set BINDS.md §4.3 signs
		// (D3), which is a different kind of change from every other verb: not a
		// source edit but a set of node ids the engine walk consumes as a
		// visibility filter, so a node draws iff its `when` is truthy and its id
		// is not a member. The set-and-walk shape is not a detail — it was the
		// decision the earlier refusal here protected. A scalar `ui.hidden`
		// would make `/ui hide a` unhide `b` because every other `ui.*` row is a
		// single id; a `when`-based hide cannot be spelled because this engine
		// has no negation and an unresolved bind is falsy by signed contract.
		// The set consumed by the walk sidesteps both, and D3 signed exactly it.
		//
		// `/ui show *` clears the whole set — the one form that names no id, so
		// it is parsed before the id-arity check below.
		if verb == "show" && len(args) == 1 && args[0] == "*" {
			return Command{Verb: "show", Target: "*"}, nil
		}
		if len(args) != 1 {
			return Command{}, fmt.Errorf("/ui %s needs a node id: /ui %s <id> (or `/ui show *` to reveal all)", verb, verb)
		}
		return Command{Verb: verb, Target: args[0]}, nil
	case "set":
		// /ui set <node-id> <key> <value…>
		if len(args) < 3 {
			return Command{}, fmt.Errorf("/ui set needs a node id, a key and a value: /ui set <node-id> <key> <value>")
		}
		return Command{Verb: "set", Target: args[0], Key: args[1], Value: strings.Join(args[2:], " ")}, nil
	case "plugin":
		// /ui plugin add <url>  or  /ui plugin remove <id>. Unlike the other
		// verbs this one takes a subcommand rather than a node id, because a
		// plugin is not a node in the document: `add` fetches a manifest and
		// mounts its fragments (H3/H6) and `remove` drops the mounted subtree
		// (Unmount). The subcommand is parsed here, in the one grammar reader, so
		// the fetch/mount dispatch in apply reads a resolved Command rather than
		// re-splitting the line — the same reason `add`/`move` resolve their
		// where clause in Parse and not in apply.
		return parsePlugin(args)
	default:
		return Command{}, fmt.Errorf("unknown /ui verb %q: expected one of %s", verb, strings.Join(Verbs(), ", "))
	}
}

// Verbs is the closed list of /ui verbs, exported so the host's command
// registry and the refusal messages read from one place.
//
// `add` and `move` both address a *position* (docs/ADDRESSING.md's `where`
// vocabulary), which is why they could only land once that vocabulary was
// signed (D2). They ship in that order for one structural reason: `add`'s
// argument is a brand-new subtree, so it cannot already be an ancestor of its
// insertion point and needs no cycle refusal, whereas `move` relocates an
// existing node and must refuse a move into that node's own subtree — the one
// piece of the write path `add` gets to skip. `set` and `style` need no
// addressing at all: they name a node by the id it already has and write a
// property the engine already reads. `hide` and `show` also name a node by its
// existing id, but they write no property: they mutate the `ui.hidden`
// view-state set (BINDS.md §4.3, D3), which the engine walk reads as a
// visibility filter, so the document source is untouched. `plugin` is the verb
// that reaches outside the document entirely: `plugin add <url>` fetches a
// manifest and composes its fragments (H3/H6), `plugin remove <id>` drops them
// (Unmount) — it addresses a plugin by its id, not a node, and `add` is the one
// verb that needs a Fetcher because its argument names bytes the host has not
// read yet.
func Verbs() []string {
	return []string{"add", "move", "set", "style", "hide", "show", "plugin"}
}

// parsePlugin reads the `plugin` verb's `add <url>` / `remove <id>` subcommand.
//
// The subcommand is carried in Key and its one argument in Value, reusing the
// Command fields the other verbs already have rather than growing a
// plugin-specific struct: a plugin command is still one verb and one string,
// which is the closed shape Command exists to keep (widening it towards
// arbitrary JSON is the collapse its comment warns against). `add`'s argument
// is a URL and `remove`'s is a plugin id — both opaque single tokens here — so
// neither needs the where-clause machinery `add`/`move` carry.
func parsePlugin(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("/ui plugin needs a subcommand: /ui plugin add <url> or /ui plugin remove <id>")
	}
	switch sub := args[0]; sub {
	case "add":
		// The URL is a single token: a manifest URL with a space in it is not a
		// URL, and splitting on spaces would let a fat-fingered second word be
		// silently dropped rather than refused.
		if len(args) != 2 {
			return Command{}, fmt.Errorf("/ui plugin add needs exactly one manifest URL: /ui plugin add <url>")
		}
		return Command{Verb: "plugin", Key: "add", Value: args[1]}, nil
	case "remove":
		if len(args) != 2 {
			return Command{}, fmt.Errorf("/ui plugin remove needs exactly one plugin id: /ui plugin remove <id>")
		}
		return Command{Verb: "plugin", Key: "remove", Value: args[1]}, nil
	default:
		return Command{}, fmt.Errorf("unknown /ui plugin subcommand %q: expected add or remove", sub)
	}
}

// apply performs the source-to-source edit and re-validates the result.
func (c Command) apply(name string, src []byte, fetch Fetcher) (Result, error) {
	switch c.Verb {
	case "add":
		return c.applyAdd(name, src)
	case "move":
		return c.applyMove(name, src)
	case "hide", "show":
		return c.applyViewState(name, src)
	case "plugin":
		return c.applyPlugin(name, src, fetch)
	}

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
	var ids []string
	unaddressable := 0
	edited := walk(tree, func(node map[string]any) {
		if _, isNode := node["type"]; isNode {
			if id, _ := node["id"].(string); id != "" {
				ids = append(ids, id)
			} else {
				unaddressable++
			}
		}
		if id, _ := node["id"].(string); id != c.Target {
			return
		}
		found = true
		c.mutate(node)
	})
	if !found {
		return Result{}, unknownTargetError(name, c.Target, ids, unaddressable)
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

	// The diff is computed against the source Apply was handed, not against a
	// second re-serialisation, so it is exactly the change the caller will
	// persist. DiffSource canonicalises both sides, so passing the raw src is
	// correct even when it was not canonically formatted.
	diff, err := DiffSource(src, out)
	if err != nil {
		return Result{}, fmt.Errorf("%s: could not diff the patched scene: %w", name, err)
	}

	return Result{Doc: doc, Source: out, Summary: c.summary(), Diff: diff}, nil
}

// applyViewState handles `hide`/`show`, the two verbs that write the
// `ui.hidden` view-state set rather than the document source.
//
// It parses and validates the document for one reason only — to resolve the
// target id against the ids the scene actually declares, so `/ui hide typo` is
// refused with the same addressed "no node with that id" message every other
// verb produces (D3 gives hide/show D2's id resolution). The document is not
// edited: hide/show change what the walk *draws*, not what the document *says*,
// so Source is returned unchanged, Diff stays empty, and the mutation the
// caller must apply to its own `ui.hidden` set travels in Result.ViewState.
//
// `show *` names no id and so skips the resolution: clearing the set is defined
// even when the scene has changed since something was hidden, which is the
// escape valve for exactly that case.
func (c Command) applyViewState(name string, src []byte) (Result, error) {
	doc, err := scene.ParseNamed(name, src)
	if err != nil {
		return Result{}, err
	}
	if err := doc.RefuseEmpty(); err != nil {
		return Result{}, err
	}
	// Invariant 3 holds here too: a command never draws a document the boot
	// path would refuse. hide/show do not edit the source, but validating it is
	// how a scene that was hand-built or arrived broken is caught before the
	// command reports success against it.
	if err := doc.Validate(); err != nil {
		return Result{}, err
	}

	if c.Verb == "show" && c.Target == "*" {
		return Result{
			Doc:       doc,
			Source:    src,
			Summary:   c.summary(),
			ViewState: &ViewStateOp{ShowAll: true},
		}, nil
	}

	var tree any
	if err := json.Unmarshal(src, &tree); err != nil {
		return Result{}, fmt.Errorf("%s: scene is not valid JSON: %w", name, err)
	}
	found := false
	var ids []string
	unaddressable := 0
	walk(tree, func(node map[string]any) {
		if _, isNode := node["type"]; isNode {
			if id, _ := node["id"].(string); id != "" {
				ids = append(ids, id)
			} else {
				unaddressable++
			}
		}
		if id, _ := node["id"].(string); id == c.Target {
			found = true
		}
	})
	if !found {
		return Result{}, unknownTargetError(name, c.Target, ids, unaddressable)
	}

	op := &ViewStateOp{}
	if c.Verb == "hide" {
		op.Hide = []string{c.Target}
	} else {
		op.Show = []string{c.Target}
	}
	return Result{Doc: doc, Source: src, Summary: c.summary(), ViewState: op}, nil
}

// applyPlugin handles the `plugin` verb: `remove` unmounts a plugin's subtree,
// `add` fetches a manifest and mounts its fragments (H6, the declarative path).
//
// Both halves delegate to the H3 composer (Mount/Unmount), which already carries
// the id prefixing, the uniqueness invariant, the behavioral-manifest refusal and
// the invariant-3 re-validation — so this method is only the fetch-and-parse step
// `add` needs and the argument routing `remove` does not. `remove` is a pure
// source-to-source edit and needs no Fetcher; `add` cannot proceed without one,
// and a nil Fetcher is refused with a reason rather than dereferenced.
//
// The manifest is parsed but not separately validated here: Mount runs
// m.Validate() itself (it is the single point foreign fragments enter the tree),
// so a behavioral manifest fetched from a URL is refused by the same gate a local
// one is, and validating twice would be a second answer to "is this manifest
// legal".
func (c Command) applyPlugin(name string, src []byte, fetch Fetcher) (Result, error) {
	switch c.Key {
	case "remove":
		return Unmount(name, src, c.Value)
	case "add":
		if fetch == nil {
			return Result{}, fmt.Errorf("/ui plugin add %q: no plugin fetcher is available in this context, so the manifest cannot be retrieved; plugin add reaches the network and is only wired where the host supplies a Fetcher", c.Value)
		}
		manifestName, data, err := fetch.Fetch(c.Value)
		if err != nil {
			return Result{}, err
		}
		m, err := ext.ParseNamed(manifestName, data)
		if err != nil {
			return Result{}, err
		}
		return Mount(name, src, m)
	default:
		// parsePlugin only ever produces add/remove, so this is unreachable
		// unless a Command is built by hand with a bad Key; naming the key is the
		// remedy for that construction bug.
		return Result{}, fmt.Errorf("/ui plugin: unknown subcommand %q; expected add or remove", c.Key)
	}
}

// not have, and it lists the ids that do exist.
//
// The listing is not politeness. Measured on the scenes this repo ships,
// roughly half of every document's nodes carry no id at all:
//
//	SOBRIA.json   7/15 addressable  (8 nodes with no id)
//	MAXIMUM.json  7/13 addressable  (6 nodes with no id)
//	RAW.json      3/4  addressable  (1 node  with no id)
//
// So "no node with that id" is the *expected* answer for a large share of
// what a user will reasonably try, and the reason is invisible from the
// screen: the status row's model name is a node the user can see and point
// at, and it is unaddressable because the document never named it. A bare
// refusal there reads as "the command is broken". Naming the available ids
// and counting the anonymous nodes turns it into the one thing the user can
// act on, and it tells the truth about why: the ids are the scene author's to
// give, and /ui cannot invent them.
//
// Inventing them was the alternative and it is refused for the same reason
// `add` and `move` are absent. A synthesised address — an index path, a
// bind name, a type occurrence — is a second way to name a node, and it would
// be a naming vocabulary designed here rather than in SCENES.md, unstable
// across any edit that reorders a list, and unreviewable because nothing else
// in the project speaks it. The scene format already has exactly one way to
// name a node and this surface uses it.
func unknownTargetError(name, target string, ids []string, unaddressable int) error {
	if len(ids) == 0 {
		return fmt.Errorf("%s: no node in this scene declares an id, so /ui cannot address any of them; add an \"id\" to the node you want to change", name)
	}
	sort.Strings(ids)
	msg := fmt.Sprintf("%s: no node with id %q in this scene; /ui addresses nodes by the id they declare, and this scene declares: %s",
		name, target, strings.Join(ids, ", "))
	if unaddressable > 0 {
		msg += fmt.Sprintf(" (%d further node(s) declare no id and cannot be addressed until the scene gives them one)", unaddressable)
	}
	return fmt.Errorf("%s", msg)
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
	case "set":
		node[c.Key] = c.setValue()
	}
}

// stringKeys are the node properties whose value is always text, so a value that happens
// to look like a number or a word of JSON ("1", "true") stays the text it was typed as.
var stringKeys = map[string]bool{
	"id": true, "type": true, "bind": true, "when": true, "placeholder": true, "text": true,
	"anchor": true, "filter_by": true, "title": true, "on_press": true, "layout": true,
}

// setValue turns what was typed after `/ui set <id> <key>` into the JSON value the node
// property holds. Everything used to be stored as a string except style_by, so
// `border {"shape":"round","style":"rainbow"}` wrote the text of the object, `grow 1` wrote
// "1", and `style rainbow` wrote a string where an object is needed: all refused, and each
// refusal told the author to write what it had just written. A model asked for an animated
// frame made some twenty attempts against it. The value is now read as JSON when it is
// JSON (an object, an array, a number, true or false, or a quoted string), `style <token>`
// means what `/ui style` means, and anything else is the text it was typed as.
func (c Command) setValue() any {
	v := strings.TrimSpace(c.Value)
	if c.Key == "style" {
		var obj map[string]any
		if json.Unmarshal([]byte(v), &obj) == nil && obj != nil {
			return obj
		}
		return map[string]any{"style": strings.Trim(v, `"`)}
	}
	var got any
	if err := json.Unmarshal([]byte(v), &got); err != nil {
		return c.Value
	}
	switch t := got.(type) {
	case map[string]any, []any:
		return t
	case string:
		return t // "round" typed with its quotes
	case nil:
		return c.Value
	default: // number or bool
		if stringKeys[c.Key] {
			return c.Value
		}
		return t
	}
}

// summary is the sentence the change-diff view shows before the patch is
// trusted. It states what changed in the user's own vocabulary, because a
// diff of re-indented JSON is not a description of a change.
func (c Command) summary() string {
	switch c.Verb {
	case "add":
		return c.addSummary()
	case "move":
		return c.moveSummary()
	case "style":
		return fmt.Sprintf("styled %q as %q", c.Target, c.Value)
	case "set":
		return fmt.Sprintf("set %s of %q to %q", c.Key, c.Target, c.Value)
	case "hide":
		return fmt.Sprintf("hid %q", c.Target)
	case "show":
		if c.Target == "*" {
			return "revealed every hidden node"
		}
		return fmt.Sprintf("revealed %q", c.Target)
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
