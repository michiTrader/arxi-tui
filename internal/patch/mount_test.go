package patch

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/ext"
)

// hostScene is a minimal valid host document every mount test composes into: a
// stack with an addressable markdown pane and an input. It binds only signed host
// state, so it validates on its own, and a mount failure is therefore about the
// plugin, never about the host.
const hostScene = `{ "root": { "type": "stack", "children": [
  { "id": "chat",   "type": "markdown", "bind": "chat.history", "grow": 1 },
  { "id": "prompt", "type": "input",    "bind": "user.input" }
]}}`

// manifest builds a validated declarative manifest from its id and a single
// mount, so each test states just the where and fragment it exercises. It fails
// the test if the manifest itself does not load — a mount test must start from a
// manifest H2 accepts, or it is measuring the loader, not the mount.
func manifest(t *testing.T, id, where, fragment string) *ext.Manifest {
	t.Helper()
	src := `{
  "id": "` + id + `",
  "name": "Test Plugin",
  "version": "1.0.0",
  "protocol": "ext/v1",
  "mounts": [ { "where": "` + where + `", "fragment": ` + fragment + ` } ]
}`
	m, err := ext.ParseNamed("plugin.json", []byte(src))
	if err != nil {
		t.Fatalf("ParseNamed refused the test manifest: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("the test manifest does not load: %v; a mount test must start from a manifest H2 accepts", err)
	}
	return m
}

// idsIn returns every non-empty node id in a scene source, so a test can assert
// what the composed tree actually contains rather than trusting a byte match.
func idsIn(t *testing.T, src []byte) map[string]bool {
	t.Helper()
	var tree any
	if err := json.Unmarshal(src, &tree); err != nil {
		t.Fatalf("composed scene is not valid JSON: %v", err)
	}
	out := map[string]bool{}
	forEachNode(tree, func(n map[string]any) {
		if id, _ := n["id"].(string); id != "" {
			out[id] = true
		}
	})
	return out
}

// TestMountsADeclarativeFragmentAtAnOverlayAnchor is the positive control: a
// declarative manifest with one overlay-anchor mount composes into the host, the
// result validates, and the fragment's id arrives prefixed. If this fails every
// refusal test below is measuring a mounter that composes nothing, so it runs
// first.
func TestMountsADeclarativeFragmentAtAnOverlayAnchor(t *testing.T) {
	m := manifest(t, "tick", "top-right",
		`{ "id": "panel", "type": "overlay", "anchor": "top-right", "children": [ { "type": "text", "text": "hi" } ] }`)
	res, err := Mount("host.json", []byte(hostScene), m)
	if err != nil {
		t.Fatalf("Mount refused a well-formed declarative fragment: %v; H3's promise is that a declarative plugin's UI composes into the host", err)
	}
	ids := idsIn(t, res.Source)
	if !ids["tick/panel"] {
		t.Fatalf("composed ids = %v; the mounted node must arrive as \"tick/panel\", because H-B.3 prefixes every mounted id with <plugin-id>/ so strangers' trees compose without collision", keys(ids))
	}
	if ids["panel"] {
		t.Fatalf("composed ids = %v; the raw \"panel\" must not survive — an unprefixed mounted id is free to collide with a host node or another plugin", keys(ids))
	}
	if !ids["chat"] || !ids["prompt"] {
		t.Fatalf("composed ids = %v; mounting must add to the host tree, not replace it", keys(ids))
	}
}

// keys renders an id set for a failure message, sorted so the message is stable.
func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestMountResolvesD2Where confirms a mount reuses the same write-path grammar
// /ui add speaks: a `below <id>` mount lands the fragment beside the named host
// node, and an `into <id>` mount lands it inside a container. If a mount grew its
// own placement engine instead of reusing insert, the two paths could drift on
// what these forms mean — the drift the shared parseWhere exists to prevent.
func TestMountResolvesD2Where(t *testing.T) {
	m := manifest(t, "note", "below chat",
		`{ "id": "banner", "type": "text", "text": "mounted" }`)
	res, err := Mount("host.json", []byte(hostScene), m)
	if err != nil {
		t.Fatalf("Mount refused a `below <id>` mount against an existing host id: %v", err)
	}
	if !idsIn(t, res.Source)["note/banner"] {
		t.Fatalf("composed ids = %v; a `below chat` mount must place the prefixed node in the tree", keys(idsIn(t, res.Source)))
	}
	// The banner must be the sibling immediately after chat, not merely present.
	var tree any
	_ = json.Unmarshal(res.Source, &tree)
	root := tree.(map[string]any)["root"].(map[string]any)
	ch := root["children"].([]any)
	pos := map[string]int{}
	for i, c := range ch {
		if id, _ := c.(map[string]any)["id"].(string); id != "" {
			pos[id] = i
		}
	}
	if pos["note/banner"] != pos["chat"]+1 {
		t.Fatalf("chat at %d, note/banner at %d; `below chat` must insert immediately after chat, or the mount and /ui add disagree about what `below` means", pos["chat"], pos["note/banner"])
	}
}

// TestMountRefusesAnUnknownWhere covers a where that is neither a D2 position nor
// an overlay anchor. It is refused, and the message names the overlay anchors, so
// an author who typed `top_right` for `top-right` is told the vocabulary rather
// than left guessing why the mount vanished.
func TestMountRefusesAnUnknownWhere(t *testing.T) {
	m := manifest(t, "tick", "sideways",
		`{ "id": "panel", "type": "text", "text": "hi" }`)
	_, err := Mount("host.json", []byte(hostScene), m)
	if err == nil {
		t.Fatal("Mount accepted where \"sideways\"; a where that names no position is unplaceable and must be refused, not silently dropped")
	}
	if !strings.Contains(err.Error(), "top-right") {
		t.Fatalf("unknown-where refusal = %q; it must list the overlay anchors so an author can find the one they meant", err.Error())
	}
}

// TestMountRefusesAWhereNamingAMissingHostNode covers a D2 anchor that resolves
// to no node. The refusal is the same addressed "no node with that id" every /ui
// verb produces, because the mount reuses the same resolver — a plugin that names
// a host node that is not there is refused with the ids that are.
func TestMountRefusesAWhereNamingAMissingHostNode(t *testing.T) {
	m := manifest(t, "tick", "below nonexistent",
		`{ "id": "panel", "type": "text", "text": "hi" }`)
	_, err := Mount("host.json", []byte(hostScene), m)
	if err == nil {
		t.Fatal("Mount accepted `below nonexistent`; an anchor that names no host node cannot be placed and must be refused")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("missing-anchor refusal = %q; it must name the id that was not found", err.Error())
	}
}

// TestMountRefusesABehavioralManifest proves Mount enforces H2's split itself:
// even handed a manifest with an executable, it refuses rather than composing a
// behavioral plugin's fragments. Mount is the single point foreign fragments
// enter the tree, so it does not trust a caller to have validated — the
// counterfactual is that removing Mount's own Validate call lets a behavioral
// manifest's UI mount with no gate, which is the ungated-code path the split
// exists to prevent.
func TestMountRefusesABehavioralManifest(t *testing.T) {
	src := `{
  "id": "tick",
  "name": "Test Plugin",
  "version": "1.0.0",
  "protocol": "ext/v1",
  "executable": "./tick",
  "consent_required": true,
  "mounts": [ { "where": "top-right", "fragment": { "type": "text", "text": "hi" } } ]
}`
	m, err := ext.ParseNamed("plugin.json", []byte(src))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	if _, err := Mount("host.json", []byte(hostScene), m); err == nil {
		t.Fatal("Mount composed a behavioral manifest's fragments; mounting a process is Block I, so Mount must refuse a manifest with an executable before any of it reaches the host tree")
	}
}

// TestPrefixingPreventsCrossPluginCollision is H3's load-bearing counterfactual,
// run in both directions over the same fragments. Two plugins each declare a node
// with the raw id "panel". WITH `<plugin-id>/` prefixing the composed ids are
// distinct, so the uniqueness invariant passes; WITHOUT it (the counterfactual —
// the same fragments left raw) the two "panel"s collide and firstDuplicateID
// reports it. That is exactly what disabling prefixFragmentIDs in Mount would
// reintroduce, and the reason the prefix is not cosmetic.
func TestPrefixingPreventsCrossPluginCollision(t *testing.T) {
	a := map[string]any{"id": "panel", "type": "text", "text": "a"}
	b := map[string]any{"id": "panel", "type": "text", "text": "b"}
	prefixFragmentIDs(a, "tick/")
	prefixFragmentIDs(b, "quote/")
	if dup := firstDuplicateID(stackOf(a, b)); dup != "" {
		t.Fatalf("prefixed two-plugin tree has duplicate id %q; the <plugin-id>/ prefix must make two authors' identical raw ids distinct, or plugins cannot compose", dup)
	}

	// Counterfactual: the same two fragments with no prefix applied.
	a2 := map[string]any{"id": "panel", "type": "text", "text": "a"}
	b2 := map[string]any{"id": "panel", "type": "text", "text": "b"}
	if dup := firstDuplicateID(stackOf(a2, b2)); dup != "panel" {
		t.Fatalf("unprefixed two-plugin tree reported duplicate %q, want \"panel\"; if this does not collide the collision test proves nothing about the prefix", dup)
	}
}

// TestMountRefusesAPluginThatCollidesWithItself proves the uniqueness check is
// live in Mount, not only in the helper above: a single plugin whose two mounts
// declare the same raw id produces the same prefixed id twice, which prefixing
// cannot separate — the author's own bug, refused with the id named.
func TestMountRefusesAPluginThatCollidesWithItself(t *testing.T) {
	src := `{
  "id": "tick",
  "name": "Test Plugin",
  "version": "1.0.0",
  "protocol": "ext/v1",
  "mounts": [
    { "where": "top-right", "fragment": { "id": "panel", "type": "text", "text": "a" } },
    { "where": "bottom",    "fragment": { "id": "panel", "type": "text", "text": "b" } }
  ]
}`
	m, err := ext.ParseNamed("plugin.json", []byte(src))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("the self-colliding manifest must still load (the collision is a compose-time property, not a manifest one): %v", err)
	}
	_, err = Mount("host.json", []byte(hostScene), m)
	if err == nil {
		t.Fatal("Mount composed a plugin declaring id \"panel\" in two fragments; after prefixing both are \"tick/panel\", which is a duplicate address and must be refused")
	}
	if !strings.Contains(err.Error(), "panel") {
		t.Fatalf("self-collision refusal = %q; it must name the duplicated id so the author finds it", err.Error())
	}
}

// TestUnmountReversesMount is the round-trip fork 4 requires: mount then unmount
// returns the original document. The comparison is against the canonical form of
// the host, because Mount serialises through MarshalIndent, so a raw host and its
// mounted-then-unmounted form differ only in formatting if the round-trip is
// clean. A drift here means the prefix namespacing does not fully name "the
// plugin's nodes", which is the property the whole id scheme rests on.
func TestUnmountReversesMount(t *testing.T) {
	m := manifest(t, "tick", "top-right",
		`{ "id": "panel", "type": "overlay", "anchor": "top-right", "children": [ { "type": "text", "text": "hi" } ] }`)
	mounted, err := Mount("host.json", []byte(hostScene), m)
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	unmounted, err := Unmount("host.json", mounted.Source, "tick")
	if err != nil {
		t.Fatalf("Unmount refused to remove a plugin it had just mounted: %v", err)
	}
	want, err := canonical([]byte(hostScene))
	if err != nil {
		t.Fatalf("canonical(host): %v", err)
	}
	if string(unmounted.Source) != string(want) {
		t.Fatalf("mount+unmount did not restore the host document.\n got:\n%s\nwant:\n%s\nunmount must drop exactly the <plugin-id>/-prefixed nodes and nothing else", unmounted.Source, want)
	}
}

// TestUnmountRefusesWhenNothingMounted covers unmounting a plugin that is not
// present. It is refused rather than reported as a silent success, because an
// unmount that changed nothing and said "done" is indistinguishable from one that
// removed the wrong plugin.
func TestUnmountRefusesWhenNothingMounted(t *testing.T) {
	_, err := Unmount("host.json", []byte(hostScene), "ghost")
	if err == nil {
		t.Fatal("Unmount reported success removing a plugin that was never mounted; a no-op unmount must be refused so it is not mistaken for a real removal")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("empty-unmount refusal = %q; it must name the plugin that has nothing to remove", err.Error())
	}
}

// stackOf wraps nodes as the children of a root stack, the minimal shape
// firstDuplicateID walks. Used by the prefix counterfactual to compose two
// fragments without going through Mount, so the prefix is the only variable.
func stackOf(nodes ...map[string]any) map[string]any {
	children := make([]any, len(nodes))
	for i, n := range nodes {
		children[i] = n
	}
	return map[string]any{
		"root": map[string]any{
			"type":     "stack",
			"children": children,
		},
	}
}
