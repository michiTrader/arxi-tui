package patch

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// sobria returns the shipped default scene's source bytes. The tests patch the
// scene the user actually runs rather than a fixture written to suit them:
// a /ui surface proven only against a two-node toy is a surface proven against
// nothing, since every property this package has to preserve — unknown keys,
// nested branches, `when` gates — exists in the shipped document and not in
// the toy.
func sobria(t *testing.T) (string, []byte) {
	t.Helper()
	const path = "../../testdata/SOBRIA.json"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read the shipped default scene at %s: %v; the /ui tests patch the real scene on purpose, so this is a missing fixture rather than a skippable case", path, err)
	}
	return path, src
}

// TestAPatchedSceneKeepsItsAddresses is the measurement this package was
// designed around, held as a guard so the design cannot be undone quietly.
//
// The tempting refactor is to mutate *scene.Node and skip the re-parse — it is
// shorter, faster, and every passing command behaves identically. What it
// destroys is the address on the refusals of the commands that *fail*, which
// is the only thing the Phase 2 repair loop reads. A guard on the happy path
// would not notice; this one asks the patched document to refuse something and
// checks that the refusal names a line.
func TestAPatchedSceneKeepsItsAddresses(t *testing.T) {
	name, src := sobria(t)

	res, err := Apply(name, src, "/ui style status dim")
	if err != nil {
		t.Fatalf("styling a node that exists in the shipped scene must succeed, and it failed: %v", err)
	}

	// Inject a refusable field into the *patched* source and confirm the
	// refusal is addressed. If Apply had rebuilt the tree in memory, the
	// document it returns would carry no offsets and this refusal would
	// print the bare file name.
	var tree map[string]any
	if err := json.Unmarshal(res.Source, &tree); err != nil {
		t.Fatalf("the patched source must be valid JSON, and it did not parse: %v", err)
	}
	walk(tree, func(node map[string]any) {
		if id, _ := node["id"].(string); id == "status" {
			node["row_template"] = map[string]any{"type": "text"}
		}
	})
	probed, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		t.Fatalf("re-serialising the probed scene failed: %v", err)
	}
	doc, err := scene.ParseNamed(name, probed)
	if err != nil {
		t.Fatalf("the probed scene must still parse: %v", err)
	}
	refusal := doc.Validate()
	if refusal == nil {
		t.Fatal("row_template is refused by the validator, so the probe must produce a refusal; it produced none, which means this guard is no longer measuring what it claims")
	}
	if !strings.Contains(refusal.Error(), name+":") {
		t.Errorf("a refusal from a patched scene lost its address.\n  got:  %v\n  want: a message starting %s:<line>:<col>\nConsequence: the Phase 2 repair loop reads the validator error and nothing else, so an unaddressed refusal makes the model guess which of several hundred lines the engine meant.\nRemedy: keep patches source-to-source — edit the bytes and re-parse through scene.ParseNamed — rather than mutating *scene.Node, which produces a Document with no offset table.", refusal, name)
	}
}

// TestAPatchPreservesKeysTheNodeTypeDoesNotDeclare pins the reason the edit
// walks a generic map rather than the typed tree.
//
// scene.Node drops any key it does not declare. Editing through it would
// delete every unknown property in the document on the way past, and the loss
// would be invisible: the scene still parses, still validates, still draws,
// and the user's field is simply gone. This is the silent-drop class the scene
// package has paid for five times, and a /ui command is the one place where
// the user watches the document change and would still not see it.
func TestAPatchPreservesKeysTheNodeTypeDoesNotDeclare(t *testing.T) {
	name := "custom.json"
	src := []byte(`{
  "root": {
    "id": "root",
    "type": "stack",
    "children": [
      { "id": "transcript", "type": "markdown", "bind": "chat.history", "x_plugin_field": "kept" },
      { "id": "prompt", "type": "input", "bind": "user.input" }
    ]
  }
}`)

	res, err := Apply(name, src, "/ui style prompt dim")
	if err != nil {
		t.Fatalf("styling a node in a hand-written scene must succeed, and it failed: %v", err)
	}
	if !strings.Contains(string(res.Source), "x_plugin_field") {
		t.Errorf("a /ui command deleted a property it was not asked to touch.\n  patched source: %s\nConsequence: every field a future phase adds and every field a third-party plugin fragment carries is silently erased the first time the user runs any /ui command, and the scene still parses, validates and draws — so nothing reports it.\nRemedy: keep the edit on the generic map tree; scene.Node drops undeclared keys, so round-tripping the patch through it cannot preserve them.", res.Source)
	}
}

// TestAnUnknownVerbIsRefusedByName holds the boundary between this surface and
// the chat prompt.
//
// The cheap behaviour is to treat an unrecognised /ui line as a normal prompt
// and send it to the model. That is the silent-reinterpretation failure this
// project catalogues: the input reports success, does something else, and the
// user gets no signal that what they typed was never a command.
func TestAnUnknownVerbIsRefusedByName(t *testing.T) {
	_, err := Parse("/ui teleport chat somewhere")
	if err == nil {
		t.Fatal("an unknown /ui verb must be refused; it was accepted.\nConsequence: an unrecognised command is silently reinterpreted as a chat prompt, which reports success and does something the user did not ask for.\nRemedy: refuse unknown verbs in Parse and name the verbs that exist.")
	}
	if !strings.Contains(err.Error(), "teleport") {
		t.Errorf("the refusal must name the verb the user typed.\n  got: %v\nConsequence: the user cannot tell a typo from an unimplemented feature.\nRemedy: include the offending verb in the message.", err)
	}
	for _, verb := range Verbs() {
		if !strings.Contains(err.Error(), verb) {
			t.Errorf("the refusal must list %q among the verbs that exist.\n  got: %v\nConsequence: the message says what is wrong and not what to type instead, which costs the repair loop a turn.\nRemedy: build the message from Verbs().", verb, err)
		}
	}
}

// TestAMissingTargetIsRefusedWithTheIdTheUserTyped covers the other half of
// the same contract: a well-formed command naming a node that is not there.
//
// Doing nothing and reporting success is the worst available behaviour, and it
// is the default one for a map walk that simply finds no match.
func TestAMissingTargetIsRefusedWithTheIdTheUserTyped(t *testing.T) {
	name, src := sobria(t)
	_, err := Apply(name, src, "/ui style no.such.node dim")
	if err == nil {
		t.Fatal("styling a node id that does not exist must be refused; it succeeded.\nConsequence: the command reports success and changes nothing, so the user believes the scene was patched and it was not.\nRemedy: track whether the walk matched, and refuse when it did not.")
	}
	if !strings.Contains(err.Error(), "no.such.node") {
		t.Errorf("the refusal must name the id the user typed.\n  got: %v\nConsequence: with several ids on the line the user cannot tell which one was wrong.\nRemedy: include the target in the message.", err)
	}
}

// TestAPatchThatBreaksTheSceneIsRefusedRatherThanDrawn is invariant 3 for this
// surface: an invalid patch never kills the session.
//
// `/ui set` writes an arbitrary key, which is exactly the command that can
// compose into a document the validator refuses. The invariant is only real if
// Apply re-validates; a version that trusted its own vocabulary would return a
// broken document and the caller would draw it.
func TestAPatchThatBreaksTheSceneIsRefusedRatherThanDrawn(t *testing.T) {
	name, src := sobria(t)
	_, err := Apply(name, src, "/ui set status bind not.a.signed.bind")
	if err == nil {
		t.Fatal("a /ui command that writes an unsigned bind must be refused; it was accepted.\nConsequence: PLAN.md invariant 3 says an invalid patch never kills the session, and a patch that skips validation reaches the renderer with a bind nothing can resolve.\nRemedy: run Validate on the patched document inside Apply, not at the call site.")
	}
	if !strings.Contains(err.Error(), name+":") {
		t.Errorf("the refusal of a bad patch must be addressed.\n  got: %v\nConsequence: the user is told the command failed but not which line of their scene is now wrong.\nRemedy: re-parse the patched bytes through scene.ParseNamed before validating, so the refusal has an offset table to read.", err)
	}
}

// TestHideAndShowWriteTheViewStateSetWithoutTouchingTheSource pins F3's
// behaviour and, in the same cases, the decision the earlier refusal protected.
//
// The draft implemented `hide` as a `when` naming `ui.hidden` — a scalar bool —
// and the validator refused it because the bind was signed nowhere. D3 then
// signed `ui.hidden` as a *set of node ids* consumed by the engine walk, and
// F3 built it. Two properties are pinned here so the rejected design cannot
// creep back:
//
//   - hide/show do not edit the document. They return a ViewStateOp naming the
//     id and leave Source byte-for-byte unchanged. A `when`-based hide would
//     have rewritten the node; a source that is untouched is the proof the set
//     path is the one taken.
//   - the set distinguishes ids. `/ui hide chat` names `chat` and nothing else,
//     which is the whole reason D3 signed a set and not the scalar that made
//     `/ui hide a` unhide `b`.
func TestHideAndShowWriteTheViewStateSetWithoutTouchingTheSource(t *testing.T) {
	name, src := sobria(t)

	hid, err := Apply(name, src, "/ui hide chat")
	if err != nil {
		t.Fatalf("`/ui hide chat` was refused, but `chat` is an id the scene declares: %v\nConsequence: F3's hide verb cannot address a node the user can see.\nRemedy: resolve the id against the document and, if present, return the set op.", err)
	}
	if hid.ViewState == nil {
		t.Fatalf("`/ui hide chat` produced no ViewState op.\nConsequence: the loop has nothing to apply to ui.hidden, so the command reports success and hides nothing.\nRemedy: return a ViewStateOp from applyViewState.")
	}
	if len(hid.ViewState.Hide) != 1 || hid.ViewState.Hide[0] != "chat" {
		t.Errorf("`/ui hide chat` did not name `chat` as the id to hide.\n  op: %+v\nConsequence: this is the scalar-flag defect D3 forbids — the set must distinguish `chat` from every other id, or `/ui hide chat` hides the wrong node.\nRemedy: put c.Target in ViewStateOp.Hide.", hid.ViewState)
	}
	if string(hid.Source) != string(src) {
		t.Errorf("`/ui hide chat` rewrote the document source.\n  before: %s\n  after:  %s\nConsequence: hide is being implemented as a `when` edit — the rejected scalar design — rather than as the view-state set D3 signed; a document edit means the hide cannot be undone by `/ui show` and survives a reload.\nRemedy: hide/show mutate ui.hidden only; leave Source unchanged.", src, hid.Source)
	}

	shown, err := Apply(name, src, "/ui show chat")
	if err != nil {
		t.Fatalf("`/ui show chat` was refused: %v", err)
	}
	if shown.ViewState == nil || len(shown.ViewState.Show) != 1 || shown.ViewState.Show[0] != "chat" {
		t.Errorf("`/ui show chat` did not name `chat` as the id to reveal.\n  op: %+v\nConsequence: show cannot undo a specific hide.\nRemedy: put c.Target in ViewStateOp.Show.", shown.ViewState)
	}

	all, err := Apply(name, src, "/ui show *")
	if err != nil {
		t.Fatalf("`/ui show *` was refused: %v", err)
	}
	if all.ViewState == nil || !all.ViewState.ShowAll {
		t.Errorf("`/ui show *` did not set ShowAll.\n  op: %+v\nConsequence: the reveal-everything escape valve BINDS.md §4.3 signs does not work, so a user who hid a node whose id later changed can never get it back.\nRemedy: set ViewStateOp.ShowAll for the `*` form.", all.ViewState)
	}
}

// TestHideOfAnUnknownIdIsRefusedWithTheIdAndTheAddress holds hide/show to D2's
// id resolution (D3 gives them the same "id not found" rule as every other
// verb): a hide that names a node the scene does not declare is refused with an
// address, not silently added to the set.
func TestHideOfAnUnknownIdIsRefusedWithTheIdAndTheAddress(t *testing.T) {
	name, src := sobria(t)
	for _, verb := range []string{"hide", "show"} {
		_, err := Apply(name, src, "/ui "+verb+" nosuchnode")
		if err == nil {
			t.Errorf("`/ui %s nosuchnode` must be refused — `nosuchnode` is not an id in the scene — and it was accepted.\nConsequence: the set fills with ids that address nothing, and `/ui show *` is the only way to clear the typo.\nRemedy: resolve the id against the document before returning the op.", verb)
			continue
		}
		if !strings.Contains(err.Error(), "nosuchnode") {
			t.Errorf("`/ui %s`'s refusal must name the id the user typed.\n  got: %v\nRemedy: reuse unknownTargetError.", verb, err)
		}
		if !strings.Contains(err.Error(), name) {
			t.Errorf("`/ui %s`'s refusal must carry the file address.\n  got: %v\nConsequence: a refusal without a location is the bug AGENTS.md names first.\nRemedy: build the message from the document name.", verb, err)
		}
	}
}

// TestAnUnaddressableSceneSaysWhichIdsExist covers the case measurement showed
// is the common one rather than the exotic one.
//
// Counted on the scenes this repo ships, roughly half of every document's
// nodes carry no id: SOBRIA 7/15 addressable, MAXIMUM 7/13, RAW 3/4. So "no
// node with that id" is the expected answer to a large share of what a user
// will reasonably try, and the reason is invisible from the screen — the
// status row's model name is a node they can see and point at, and it is
// unaddressable because the document never named it. A bare refusal there
// reads as a broken command.
func TestAnUnaddressableSceneSaysWhichIdsExist(t *testing.T) {
	name, src := sobria(t)
	_, err := Apply(name, src, "/ui style model.name dim")
	if err == nil {
		t.Fatal("`model.name` is a bind in the shipped scene, not a node id, so this must be refused; it succeeded.\nConsequence: the command silently patched something the user did not name.\nRemedy: match on the id field only.")
	}
	for _, id := range []string{"chat", "prompt", "status"} {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("the refusal must list the id %q, which this scene does declare.\n  got: %v\nConsequence: about half the nodes in every shipped scene have no id, so this refusal is the common case; without the list the user cannot tell an unnameable node from a typo.\nRemedy: collect the declared ids during the walk and name them in the message.", id, err)
		}
	}
	if !strings.Contains(err.Error(), "no id") {
		t.Errorf("the refusal must say that further nodes declare no id at all.\n  got: %v\nConsequence: the user hunts for an id that was never written, and concludes /ui is broken.\nRemedy: count the anonymous nodes during the walk and report them.", err)
	}
}

// TestStyleWritesTheSpellingTheRendererReads guards the one drift this repo
// has already paid for, at the one call site where the user cannot see it.
//
// ValidateTokens accepts both style["token"] and style["style"]; the renderer
// reads only style["style"]. A command writing the other spelling produces a
// scene that validates clean and draws unstyled — success reported, wrong
// screen — and the user who typed `/ui style x dim` has no way to inspect
// which key was written.
func TestStyleWritesTheSpellingTheRendererReads(t *testing.T) {
	name, src := sobria(t)
	res, err := Apply(name, src, "/ui style status dim")
	if err != nil {
		t.Fatalf("styling a node that exists must succeed, and it failed: %v", err)
	}

	var tree map[string]any
	if err := json.Unmarshal(res.Source, &tree); err != nil {
		t.Fatalf("the patched source must be valid JSON: %v", err)
	}
	var style map[string]any
	walk(tree, func(node map[string]any) {
		if id, _ := node["id"].(string); id == "status" {
			style, _ = node["style"].(map[string]any)
		}
	})
	if style == nil {
		t.Fatalf("/ui style wrote no style object at all onto its target.\n  patched source: %s", res.Source)
	}
	if got, _ := style["style"].(string); got != "dim" {
		t.Errorf("/ui style did not write the key the renderer reads.\n  style object: %v\nConsequence: ValidateTokens accepts style[\"token\"] too, so the wrong spelling produces a scene that validates clean and renders unstyled — the one outcome that reports success and shows the wrong screen.\nRemedy: write style[\"style\"]; see the Phase 1 entry in README.md for the measured instance of this drift.", style)
	}
}

// TestEveryVerbRoundTripsThroughTheValidator sweeps the verbs rather than
// testing one, because the tests above each exercise a single branch of
// mutate() and a verb added later would be covered by none of them.
//
// The sweep is over Verbs(), which is also what the refusal message and the
// host registry read, so a verb that exists is a verb this runs.
func TestEveryVerbRoundTripsThroughTheValidator(t *testing.T) {
	name, src := sobria(t)
	lines := map[string]string{
		"add":   `/ui add node below status {"type":"text","text":"hello"}`,
		"move":  `/ui move status above chat`,
		"style": "/ui style status dim",
		"set":   "/ui set status text hello",
		"hide":  "/ui hide status",
		"show":  "/ui show status",
	}

	for _, verb := range Verbs() {
		line, ok := lines[verb]
		if !ok {
			t.Errorf("verb %q is in Verbs() and this sweep has no line for it.\nConsequence: a verb reachable from the menu and named in every refusal message is exercised by no test, so its branch of mutate() can be wrong and the suite stays green.\nRemedy: add a line for it here.", verb)
			continue
		}
		res, err := Apply(name, src, line)
		if err != nil {
			t.Errorf("%q must produce a scene the validator accepts, and it did not: %v\nConsequence: the verb is unusable from the interface.\nRemedy: fix the mutation, not the test.", line, err)
			continue
		}
		if res.Summary == "" {
			t.Errorf("%q produced no summary.\nConsequence: the change-diff view has nothing to show the user before the patch is trusted, and PLAN.md requires the agent show what it altered.\nRemedy: add a case to summary().", line)
		}
		if res.Doc == nil {
			t.Errorf("%q returned no document.\nConsequence: the caller has nothing to draw.\nRemedy: return the re-parsed document from apply().", line)
		}
	}
}
