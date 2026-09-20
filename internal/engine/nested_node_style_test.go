package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// A node reached through `prefix`, `suffix` or `row_template` must honour the
// style token it declares, exactly as a node reached through `children` must.
//
// # The gap, and how it stayed open
//
// TestEveryNodeDrawsItsOwnContentUnderItsOwnToken sweeps renderNode's dispatch
// with a node built by nodeDrawsOwnContent: `&scene.Node{Type: ty, Text: …,
// Style: …}`. That node has no Prefix, no Suffix and no RowTemplate, so the
// three fields in which a scene may nest another node are never populated and
// the spans they produce are never examined. The sweep is over node *types*,
// and these are node *positions* — no type name identifies them, so no
// widening of that loop reaches here.
//
// Its companion, TestStylingAShippedSceneChangesItsFrame, was supposed to be
// the reachability half. It cannot be. It stamps the witness with
// restyleOffendingNodes, which recurses through `n.Children` alone and stamps
// only node types the sweep has *already* flagged. Both halves of that are
// load-bearing in the wrong direction: it walks past every nested node, and it
// is armed only while its sibling is already failing. Measured on the clean
// tree, it reports
//
//	--- SKIP: TestStylingAShippedSceneChangesItsFrame
//	    no node type in SOBRIA currently drops its declared token
//
// and it has skipped since the commit that introduced it — the fix commit,
// which silenced the sweep and the reachability check in the same breath. A
// check that fires only when another check is already red is not a second
// check; it is a duplicate of the first, and it is switched off precisely when
// it would be the only evidence.
//
// # The injection
//
// Dropping the declared token on both nested spans in renderMarquee —
//
//	cells = append(cells, ui.Span{Text: …, Style: ""})   // prefix
//	cells = append(cells, ui.Span{Text: …, Style: ""})   // suffix
//
// — left **the entire suite green**, goldens included. SOBRIA's thinking
// marquee is on screen with a dim prefix and a dim usage suffix, and nothing
// in the tree could tell that both had turned plain.
//
// The reason the goldens miss it is the sharpest part. Exactly one test renders
// that marquee with its `agent.working` gate open, TestSobriaSceneMarqueeGolden,
// and its comment says it checks "prefix and suffix". It asserts on
// `f.Plain()` — the frame with styling discarded — so it reads the prefix and
// suffix text and is constitutionally unable to see a token. The styled golden
// does not cover it either: SOBRIA.styled is folded from an event list ending
// in agent.turn_done, so agent.working is false and the marquee renders zero
// rows. Grep confirms it: "Thinking" appears 0 times in SOBRIA.styled.
//
// So the styled surface of every nested node was unmeasured, and the one test
// named for it was measuring the other half of it.
//
// # Why this guard renders the owning node rather than the document
//
// The nested spans are emitted by the *parent's* renderer — renderMarquee
// builds the prefix, main and suffix cells itself — so the question is what
// that parent draws. Rendering the owning node directly through renderNode also
// steps around the `when` gate that hides the marquee in a default fold, which
// is the trap the first probe for this fell into: a state with ThinkingText set
// but AgentWorking false renders nothing, and "nothing changed" then reads as a
// dropped token. That was measured, not guessed — the probe reported
// `frame moved=false` for a path that does honour its token.
//
// # Why the population is swept out of the shipped scenes
//
// A hand-written list of "the marquee has a prefix and a suffix" is the second
// inventory this package has been burned by four times. The scenes are walked
// and every nested node found is held to the rule, so a scene that gains a
// row_template or a suffix tomorrow is covered the day it lands.

const nestedStyleWitness = "NESTEDSTYLEWITNESS"

// nestedBranches are the fields of scene.Node that hold another node. They are
// the same four the validator recurses through minus `children`, which the
// existing guards already cover — validateBinds, collectWarnings,
// collectTokenErrors and collectBinds all walk children, prefix, suffix and
// row_template, and every one of them names this exact set.
var nestedBranches = []string{"prefix", "suffix", "row_template"}

// nestedCandidate is one nested node found in a shipped scene: the node that
// owns it, the branch it hangs off, and where it came from for the address.
type nestedCandidate struct {
	scene  string
	path   string
	branch string
	owner  *scene.Node
}

// nestedStyleState is populated for every bind the shipped scenes reach through
// a nested node. An empty fold makes a bound nested node draw nothing, and a
// span that is not drawn cannot carry a token — the frames would match and the
// guard would report a dropped style for a node that was simply absent. That is
// the same false-alarm shape the `when` gate produced in this file's first
// probe.
func nestedStyleState() fold.State {
	s := fold.Fold(nil)
	s.AgentWorking = true
	s.ThinkingText = "Looking at the code..."
	s.UsageIn = 25
	s.UsageOut = 35
	s.DeriveUsageDelta()
	s.ModelName = "openai/gpt-4o"
	s.UserInput = "typed text"
	s.SlashActive = true
	s.SlashMatches = []fold.SlashMatch{
		{Name: "/help", Category: "General", Description: "Show available commands"},
		{Name: "/quit", Category: "General", Description: "Leave the session"},
	}
	s.History = []fold.ChatLine{{Role: "user", Text: "hola"}}
	return s
}

// withNestedStyle returns a copy of owner whose nested node on the given branch
// declares the witness token.
//
// It round-trips through JSON rather than mutating the tree, for two reasons
// measured while writing this file. Node.PrefixNode() decodes PrefixRaw into a
// fresh value, so stamping what it returns changes a copy and the frame cannot
// move — a probe that did exactly that reported a false negative. And rebuilding
// the owner from its own serialised form leaves the original untouched, so the
// baseline render cannot be contaminated by the stamped one.
func withNestedStyle(t *testing.T, owner *scene.Node, branch string) (*scene.Node, bool) {
	t.Helper()

	raw, err := json.Marshal(owner)
	if err != nil {
		t.Fatalf("marshal node: %v", err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("unmarshal node: %v", err)
	}

	nestedRaw, ok := obj[branch]
	if !ok {
		return nil, false
	}
	// A `prefix` may be a bare string ("❯ "), which is chrome the owning
	// node styles, not a node with a token of its own. Reported as not a
	// candidate rather than as a failure: there is no declaration here to
	// honour.
	var nested map[string]json.RawMessage
	if err := json.Unmarshal(nestedRaw, &nested); err != nil {
		return nil, false
	}

	nested["style"] = json.RawMessage(`{"style":"` + nestedStyleWitness + `"}`)
	stampedNested, err := json.Marshal(nested)
	if err != nil {
		t.Fatalf("marshal nested node: %v", err)
	}
	obj[branch] = stampedNested

	stamped, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal stamped node: %v", err)
	}
	var out scene.Node
	if err := json.Unmarshal(stamped, &out); err != nil {
		t.Fatalf("unmarshal stamped node: %v", err)
	}
	return &out, true
}

// collectNestedCandidates walks a shipped scene and returns every node that
// hangs another node off prefix, suffix or row_template.
func collectNestedCandidates(t *testing.T, sceneName, path string, n *scene.Node, out *[]nestedCandidate) {
	t.Helper()
	if n == nil {
		return
	}
	for _, branch := range nestedBranches {
		if _, ok := withNestedStyle(t, n, branch); ok {
			*out = append(*out, nestedCandidate{scene: sceneName, path: path, branch: branch, owner: n})
		}
	}
	for i, c := range n.Children {
		collectNestedCandidates(t, sceneName, path+".children["+itoa(i)+"]", c, out)
	}
	// The nested nodes are themselves walked: a row template may carry a
	// prefix, and nothing says the nesting is one level deep.
	collectNestedCandidates(t, sceneName, path+".prefix", n.PrefixNode(), out)
	collectNestedCandidates(t, sceneName, path+".suffix", n.Suffix, out)
	collectNestedCandidates(t, sceneName, path+".row_template", n.RowTemplate, out)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestEveryNestedNodeHonoursItsOwnToken(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	var candidates []nestedCandidate
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		doc, err := scene.ParseFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		collectNestedCandidates(t, e.Name(), "root", doc.Root, &candidates)
	}

	// The floor. If the walk stops finding nested nodes — a renamed field,
	// a scene rewritten without a marquee, a ReadDir pointed at the wrong
	// directory — this file reports success having rendered nothing, which
	// is the state the whole tree was in before it was written. Three is
	// what the shipped scenes carry today: SOBRIA's marquee prefix and
	// suffix, and MAXIMUM's marquee prefix.
	if len(candidates) < 3 {
		t.Fatalf("found only %d nested node(s) in the shipped scenes; the walk is not reaching them\n"+
			"consequence: a green result here would certify the prefix/suffix/row_template styling\n"+
			"surface without rendering any of it — the exact state in which dropping both of\n"+
			"renderMarquee's nested tokens left the whole suite passing.\n"+
			"remedy: confirm scene.Node still spells these branches %v and that testdata carries a\n"+
			"scene using one.", len(candidates), nestedBranches)
	}

	r := &Renderer{Width: 80, Height: 24}
	state := nestedStyleState()

	draw := func(n *scene.Node) string {
		return frameSignature(r.renderNode(n, state, 24))
	}

	var dropped, undrawn []string
	for _, c := range candidates {
		stamped, ok := withNestedStyle(t, c.owner, c.branch)
		if !ok {
			t.Fatalf("premise broken: %s %s.%s was collected as a candidate and then could not be stamped",
				c.scene, c.path, c.branch)
		}

		before := draw(c.owner)
		after := draw(stamped)

		// A candidate whose owner draws nothing cannot demonstrate
		// anything either way, and passing it silently is the skip
		// bucket this package keeps closing. It is reported instead: the
		// state above is meant to make every shipped node draw, so an
		// empty frame means the state has drifted from the scenes, not
		// that the node is exempt.
		if strings.TrimSpace(before) == "" {
			undrawn = append(undrawn, c.scene+" "+c.path+"."+c.branch+" (its owning "+c.owner.Type+" node drew an empty frame)")
			continue
		}

		if before == after {
			dropped = append(dropped, c.scene+" "+c.path+"."+c.branch+" (owning node type "+c.owner.Type+")")
		}
	}

	sort.Strings(dropped)
	sort.Strings(undrawn)

	if len(dropped) > 0 {
		t.Errorf("%d nested node(s) declare a style token and render without it:\n  %s\n\n"+
			"consequence: the silent drop, on the one styling surface no guard in this package\n"+
			"reached. `style` is universal in SCENES.md and ValidateTokens is type-agnostic, so both\n"+
			"validators accept the declaration and the frame is unchanged — nothing refuses, there is\n"+
			"no address to repair from, and Phase 2's repair loop has no input.\n"+
			"Measured: blanking the token on renderMarquee's prefix and suffix spans left the entire\n"+
			"suite green, goldens included. TestSobriaSceneMarqueeGolden renders that marquee and is\n"+
			"named for its prefix and suffix, but asserts on f.Plain(), which discards styling; the\n"+
			"styled golden folds to agent.working=false, so the marquee draws zero rows there.\n"+
			"remedy: apply styleName(nested.Style) to the spans the owning renderer emits for its\n"+
			"nested nodes, as renderMarquee does for both of its.",
			len(dropped), strings.Join(dropped, "\n  "))
	}

	if len(undrawn) > 0 {
		t.Errorf("%d nested node(s) could not be exercised because their owning node drew nothing:\n  %s\n\n"+
			"consequence: an unexercised candidate is a silent skip, and a silent skip here reads as\n"+
			"a pass. This guard's whole subject is a surface that went unmeasured while looking\n"+
			"measured.\n"+
			"remedy: extend nestedStyleState so the owning node draws — it is meant to populate every\n"+
			"bind the shipped scenes reach.",
			len(undrawn), strings.Join(undrawn, "\n  "))
	}

	t.Logf("swept %d nested node(s) across the shipped scenes", len(candidates))
}
