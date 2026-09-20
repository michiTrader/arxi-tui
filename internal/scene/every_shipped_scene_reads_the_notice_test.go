package scene

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Every scene this product ships must read host.scene.error.
//
// # Why this is a guard and not three careful edits
//
// The defect that produced it was not that a node was missing from a document.
// It was that the *delivery* of every diagnostic this engine computes depends
// on a property of the shipped documents that nothing checked: the host writes
// the notice into fold state, and whether a single character of it reaches a
// user is decided by whether the active scene happens to contain a node bound
// to that field.
//
// Measured on the built binary, before the fix: a scene with one unsigned bind
// produced twenty-four blank rows, and a shipped SOBRIA carrying an
// unimplemented property drew normally and named it nowhere — while the full
// addressed diagnosis sat in host state in both cases. The validator, the
// warning path, the locator and the renderer were all correct and all silent.
//
// Fixing the three documents by hand closes today's instance and leaves the
// same trap armed for the fourth scene. This package has watched four
// hand-maintained inventories and three of them drifted; the comments in
// vocabulary.go argue at length that the answer is to derive or to audit, never
// to remember. The eleven golden scenes in SCENES.md are a list that is going
// to grow, and every one added without this node is a scene that ships with the
// diagnostics switched off — invisibly, because no test of the computation can
// see it.
//
// # Why it lives in internal/scene
//
// The question is about the documents, not about the host that loads them or
// the engine that draws them. cmd/arxi-tui has a guard for the delivery path
// (that the notice reaches a frame); this one asks whether the *fixtures*
// satisfy the precondition that guard depends on, which is a fact about the
// scene corpus. It also means a fixture added for an engine test, with no host
// in the picture, is still held to it.
//
// # Why it is a refusal and not a warning
//
// A scene the product ships is not a scene a user wrote. BINDS.md §2 signs
// host.scene.error as "the one bind the scene may render but the core never
// provides", and PLAN.md's invariant 3 requires the notice "on screen" — for
// documents under this repository's control that is an obligation, not a
// preference. A user's own scene may of course omit it and accept the silence;
// nothing here constrains user documents.
func TestEveryShippedSceneBindsTheNotice(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	var scenes []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			scenes = append(scenes, e.Name())
		}
	}

	// The floor that stops a vacuous pass. A directory walk that silently
	// stopped matching would report no shipped scenes and agree with
	// itself perfectly — the failure mode the sibling audits in this
	// package each carry a floor against, one of them because it happened.
	if len(scenes) < 3 {
		t.Fatalf("found %d scene fixtures in %s (%v); the audit is reading the wrong\n"+
			"directory and would pass while every shipped scene was silent", len(scenes), dir, scenes)
	}

	for _, name := range scenes {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			doc, err := ParseDocument(data)
			if err != nil {
				t.Fatalf("%s does not parse: %v", name, err)
			}
			if doc.Root == nil {
				t.Fatalf("%s has no root", name)
			}

			if !bindsTheNotice(doc.Root) {
				t.Errorf("%s binds no node to host.scene.error.\n"+
					"consequence: every diagnostic this engine computes is delivered through that one\n"+
					"field, so while this scene is active the host can say nothing at all. A refused\n"+
					"user scene falls back to a blank screen; an unknown property, a misspelled\n"+
					"\"children\" that drops a subtree, an unsigned node type — all load, report success,\n"+
					"and name themselves nowhere. The notice is still computed in full, with its\n"+
					"file:line: address, and is discarded one function short of a pixel. That silence\n"+
					"is indistinguishable from a crash, an ignored edit, or an engine bug, and it is\n"+
					"invisible to every test of the computation, all of which pass.\n"+
					"remedy: add the gated notice node to %s:\n"+
					"  { \"id\": \"notice\", \"type\": \"text\", \"bind\": \"host.scene.error\",\n"+
					"    \"when\": \"host.scene.error\", \"style\": {\"style\": \"banner\"} }\n"+
					"The when gate keeps it costless: the field is signed \"text | null\", null means the\n"+
					"active scene validated, so a clean boot renders zero rows for it and the goldens\n"+
					"do not move.", name, name)
			}
		})
	}
}

// bindsTheNotice reports whether any node in the tree reads host.scene.error.
//
// It walks the parsed tree rather than grepping the file, and the difference is
// the point: a substring search would be satisfied by the field name appearing
// in a comment, in a `when` with no `bind`, or inside an unrelated string, and
// this repository has already had one audit go quietly blind because it matched
// text instead of structure — a regex over a case clause stopped finding the arm
// it had covered all along the moment the arm gained a second label.
//
// Both `bind` and `when` are accepted as the read, because a node gated on the
// field but bound to something else is still a node that reacts to it, and a
// scene may legitimately choose to show the notice through a container.
func bindsTheNotice(n *Node) bool {
	if n == nil {
		return false
	}
	const field = "host.scene.error"
	if n.Bind == field || n.When == field {
		return true
	}
	for _, c := range n.Children {
		if bindsTheNotice(c) {
			return true
		}
	}
	return false
}

// The compiled-in factory scenes are held to the same rule, and they are
// checked here by reading the source rather than by importing it.
//
// cmd/arxi-tui is a main package; internal/scene cannot import it, and moving
// the constants into a library package to make them testable would be
// rearranging the product to suit a test. The fixtures above are what the
// binary loads on a normal run, and the constants are what it falls back to
// when those are missing or refused — which is precisely the path where the
// notice matters most, so leaving them unchecked would exempt the one document
// that is on screen at the worst moment.
func TestTheCompiledFactoryScenesBindTheNoticeToo(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "cmd", "arxi-tui", "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}

	// The constants are assembled from a shared fragment, so the check is
	// that the fragment exists, binds the field, and is gated. Verifying
	// the composition itself belongs to cmd/arxi-tui's own tests, which
	// render the constants and assert on the frame.
	const fragment = `const factoryNoticeNode = `
	idx := indexOf(string(src), fragment)
	if idx < 0 {
		t.Fatalf("main.go declares no factoryNoticeNode.\n" +
			"consequence: the fallback scene is the document on screen exactly when the user's\n" +
			"own scene was refused — the moment the notice is most needed — and without this\n" +
			"fragment it has no node to draw it on.\n" +
			"remedy: keep the shared notice-node constant and splice it into every factory scene.")
	}

	line := string(src[idx : idx+min(len(string(src))-idx, 400)])
	var node map[string]any
	if raw := backquoted(line); raw != "" {
		if err := json.Unmarshal([]byte(raw), &node); err != nil {
			t.Fatalf("factoryNoticeNode is not valid JSON: %v\n"+
				"consequence: it is spliced into every factory scene, so a malformed fragment breaks\n"+
				"the fallback for every refusal at once — the one document that must always parse.", err)
		}
	} else {
		t.Fatalf("could not read the factoryNoticeNode literal out of main.go")
	}

	if node["bind"] != "host.scene.error" {
		t.Errorf("factoryNoticeNode binds %v, not host.scene.error\n"+
			"consequence: the fallback scene renders something other than the refusal that put it\n"+
			"on screen.", node["bind"])
	}
	if node["when"] != "host.scene.error" {
		t.Errorf("factoryNoticeNode is gated on %v, not host.scene.error\n"+
			"consequence: an ungated notice row costs a row of the two-node raw scene on every\n"+
			"clean boot, which is invariant 1, and is the always-on warning light that\n"+
			"TestAValidSceneReportsNoNotice rejects one layer up.", node["when"])
	}
}

// indexOf is strings.Index, named locally so the two helpers above read as one
// small parser rather than as string manipulation scattered through the tests.
func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// backquoted returns the contents of the first backquoted Go string literal in
// s, or "" when there is none.
func backquoted(s string) string {
	start := -1
	for i := 0; i < len(s); i++ {
		if s[i] == '`' {
			if start < 0 {
				start = i + 1
				continue
			}
			return s[start:i]
		}
	}
	return ""
}
