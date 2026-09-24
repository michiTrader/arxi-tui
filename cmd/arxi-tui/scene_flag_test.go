package main

import (
	"testing"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// hasNodeID reports whether any node in the document's tree carries id. It walks
// Children only, which is enough for the top-level ids these tests key on; the
// point is to prove which *document* booted, not to audit every position.
func hasNodeID(doc *scene.Document, id string) bool {
	if doc == nil || doc.Root == nil {
		return false
	}
	var walk func(n *scene.Node) bool
	walk = func(n *scene.Node) bool {
		if n == nil {
			return false
		}
		if n.ID == id {
			return true
		}
		for _, c := range n.Children {
			if walk(c) {
				return true
			}
		}
		return false
	}
	return walk(doc.Root)
}

// The -scene flag is the only thing that lets a tester boot a document other
// than the shipped default, so its seam is exercised directly rather than
// through run(), which owns a tty. A temp scene carrying a unique id stands in
// for "some document other than the default": resolveStartScene must return
// *that* document. Counterfactual: reverting run() to ignore its argument and
// hardcode the default makes the seam return the default, which lacks this id,
// and this fails — a silently-ignored flag is caught here, not by a green boot
// that happens to draw the default.
func TestSceneFlagLoadsTheNamedDocument(t *testing.T) {
	path := writeScene(t, "named.json", `{ "root": { "type": "stack", "children": [
    { "id": "chat", "type": "markdown", "bind": "chat.history" },
    { "id": "picked-by-flag", "type": "text", "text": "loaded via -scene" }
]}}`)

	doc, notice, err := resolveStartScene(path)
	if err != nil {
		t.Fatalf("resolveStartScene(%s): %v", path, err)
	}
	if notice != "" {
		t.Fatalf("a scene that loads clean must carry no notice; got %q\n"+
			"Consequence: a spurious notice would print host.scene.error over a scene that validated, telling the user a good load failed.", notice)
	}
	if !hasNodeID(doc, "picked-by-flag") {
		t.Fatalf("resolveStartScene ignored the path and booted the default (no \"picked-by-flag\" node).\n" +
			"Consequence: -scene cannot select a document, so a tester can never boot the animation or subagents scenes the flag exists to reach.\n" +
			"Remedy: run() must pass its scenePath argument into resolveStartScene, not a hardcoded default.")
	}
}

// -scene "" is the start-time half of invariant 6: it must restore the factory
// raw scene regardless of what a captured on-disk scene would draw, and it must
// present the raw scene as the intended document (no error notice), not as the
// wreckage of a failed load.
func TestEmptySceneFlagIsTheRawEscapeHatch(t *testing.T) {
	doc, notice, err := resolveStartScene("")
	if err != nil {
		t.Fatalf("resolveStartScene(\"\"): %v", err)
	}
	if notice != "" {
		t.Fatalf(`-scene "" is the intended raw scene, not a failed load, so it carries no notice; got %q`+"\n"+
			"Consequence: the escape hatch would accuse itself of an error every time it fired.", notice)
	}
	if !hasNodeID(doc, "chat") || !hasNodeID(doc, "prompt") {
		t.Fatalf(`-scene "" did not load the factory raw scene (missing "chat"/"prompt").` + "\n" +
			"Consequence: the start-time escape hatch would hand back some other document instead of the raw one, which is exactly what invariant 6 forbids a scene from being able to do.")
	}
}
