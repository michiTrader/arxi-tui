package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// renderWithPreview renders a hand-built node with a J1 preview table (and an
// optional live plugin snapshot) attached, the two inputs the installer loop
// hands the renderer while browsing an entry. It mirrors renderWithPlugins: a
// hand-built tree rather than a golden, because an empty preview table is the
// default and moves no golden — the property only appears once an entry is being
// previewed. The maps are plain map[string]string on purpose: that IS the
// renderer's contract with the host (ext builds the table; the engine only
// resolves a path to a value), so building them here tests exactly the boundary
// the renderer owns and keeps the engine test off the ext package.
func renderWithPreview(t *testing.T, n *scene.Node, state fold.State, plugins, preview map[string]string) string {
	t.Helper()
	r := &Renderer{Width: 80, Height: 24, PluginValues: plugins, PreviewMocks: preview}
	var b strings.Builder
	for _, l := range r.renderNode(n, state, 24).Live {
		b.WriteString(l.Text())
		b.WriteString("\n")
	}
	return b.String()
}

// TestPreviewMockSubstitutesForThePlaceholder is the positive half of J1: a text
// node bound to an un-mounted plugin's `<plugin-id>.*` field draws the manifest's
// declared mock instead of the placeholder, which is the whole point of the
// installer's preview pane (Q16 / Scene 7). With no live snapshot the same bind
// is unsatisfied and today falls to "[…]"; the preview table is what turns it into
// the stranger's declared placeholder text.
func TestPreviewMockSubstitutesForThePlaceholder(t *testing.T) {
	node := &scene.Node{Type: "text", Bind: "tick.price"}
	got := renderWithPreview(t, node, fold.State{}, nil, map[string]string{"tick.price": "$MOCK"})
	if !strings.Contains(got, "$MOCK") {
		t.Errorf("a previewed bind did not render its manifest mock; got:\n%s\n"+
			"consequence: the installer's preview pane shows a stranger's scene as a wall of \"[…]\" placeholders, so a user cannot tell what a plugin looks like before installing it — the browse-before-trust step Scene 7 exists for (Q16).\n"+
			"remedy: resolveBindRow must consult PreviewMocks before falling through to resolveBind's placeholder.", got)
	}
}

// TestNilPreviewMocksIsAByteIdenticalNoOp pins the no-op guarantee J1 rests on:
// a preview table only substitutes the keys it actually holds, so a non-previewed
// bind renders identically whether the table is nil or merely lacks that key. If
// this drifted, adding preview mode would move goldens that have nothing to do
// with preview — the SOBRIA-zero-row trap AGENTS.md records, where a change that
// should be inert silently rewrites an unrelated frame.
func TestNilPreviewMocksIsAByteIdenticalNoOp(t *testing.T) {
	node := &scene.Node{Type: "text", Bind: "model.name"}
	state := fold.State{ModelName: "opus"}

	withNil := renderWithPreview(t, node, state, nil, nil)
	// A preview table that does not mention model.name must not perturb it: the
	// fold-backed bind resolves exactly as with no table at all.
	withUnrelated := renderWithPreview(t, node, state, nil, map[string]string{"tick.price": "$MOCK"})
	if withNil != withUnrelated {
		t.Errorf("a preview table changed a bind it does not hold:\nnil table:\n%s\nunrelated table:\n%s\n"+
			"consequence: preview mode is not inert on the binds it is not previewing, so entering the installer would rewrite frames it should leave untouched and every unrelated golden would move.\n"+
			"remedy: PreviewMocks must substitute only paths present in the map; a miss falls through unchanged.", withNil, withUnrelated)
	}
	if !strings.Contains(withNil, "opus") {
		t.Fatalf("precondition: the fold-backed bind must still render its value; got:\n%s", withNil)
	}
}

// TestPreviewMockFeedsAWhenGateButATrueMissDoesNot is the invariant J1's comment
// promises: because display and `when` share one resolver, a mock is a real
// string a gate reads as truthy, while a bind the table has NO mock for stays the
// falsy placeholder. So preview flips a gate only for the binds it is actually
// mocking — it can never silently open a gate for a field the previewed manifest
// never declared. Both directions run over one gate so the test cannot pass by
// the two halves quietly agreeing.
func TestPreviewMockFeedsAWhenGateButATrueMissDoesNot(t *testing.T) {
	gated := &scene.Node{Type: "text", When: "tick.ready", Text: "READY"}

	// A mock for the gate bind: present, non-empty → truthy → the node draws.
	shown := renderWithPreview(t, gated, fold.State{}, nil, map[string]string{"tick.ready": "true"})
	if !strings.Contains(shown, "READY") {
		t.Errorf("a node gated on a previewed bind did not render when the mock was present; got:\n%s\n"+
			"consequence: preview cannot show a stranger's `when`-gated chrome, so any part of a scene guarded by a plugin bind is invisible in preview even though the manifest mocks it.\n"+
			"remedy: evalWhenRow must resolve through the same PreviewMocks table so a mock is truthy.", shown)
	}

	// No mock for the gate bind (a true miss): the placeholder is falsy → hidden.
	hidden := renderWithPreview(t, gated, fold.State{}, nil, map[string]string{"tick.price": "$MOCK"})
	if strings.Contains(hidden, "READY") {
		t.Errorf("a node gated on an un-mocked bind rendered in preview; got:\n%s\n"+
			"consequence: preview mode opens `when` gates for binds it has no mock for, so a previewed scene shows chrome its manifest never promised — the fail-open direction evalWhen's comment calls the dangerous one.\n"+
			"remedy: a bind absent from PreviewMocks must resolve to the falsy placeholder, not a truthy value.", hidden)
	}
}

// TestPreviewMissStillDrawsThePlaceholderNeverCrashes is the counter-field rule
// under preview: a previewed bind the table does not hold degrades to "[…]", not a
// panic and not an empty string. This is what lets the installer preview a scene
// whose manifest mocks only some of its binds — the unmocked ones stay honest
// placeholders instead of tearing the render down.
func TestPreviewMissStillDrawsThePlaceholderNeverCrashes(t *testing.T) {
	node := &scene.Node{Type: "text", Bind: "tick.unmocked"}
	got := renderWithPreview(t, node, fold.State{}, nil, map[string]string{"tick.price": "$MOCK"})
	if !strings.Contains(got, placeholderValue) {
		t.Errorf("a previewed bind with no mock rendered %q, not the placeholder %q\n"+
			"consequence: an entry that mocks only some of its binds cannot be previewed without the unmocked ones drawing something other than the honest \"no value\" placeholder — a step toward the crash the counter-field rule forbids.\n"+
			"remedy: a preview miss must fall through to resolveBind's default placeholder, exactly as a live-snapshot miss does.", strings.TrimSpace(got), placeholderValue)
	}
}

// TestLivePluginValueWinsOverAPreviewMock pins the resolver ordering: PreviewMocks
// is consulted AFTER the live snapshot, so a plugin that is actually mounted shows
// its real frame, never the stale mock. This matters for the edge where an entry
// being previewed is also already installed — the user must see what it really
// does now, not the author's declared placeholder. Reversing the order would make
// a mock mask a live value, which is the opposite of "a live mount shows the
// value, not the mock" (§I-G).
func TestLivePluginValueWinsOverAPreviewMock(t *testing.T) {
	node := &scene.Node{Type: "text", Bind: "tick.price"}
	got := renderWithPreview(t, node, fold.State{},
		map[string]string{"tick.price": "$LIVE"},
		map[string]string{"tick.price": "$MOCK"})
	if !strings.Contains(got, "$LIVE") || strings.Contains(got, "$MOCK") {
		t.Errorf("a preview mock masked a live plugin value; got:\n%s\nwant the live value $LIVE, not the mock $MOCK\n"+
			"consequence: previewing an already-mounted plugin shows the author's declared placeholder over the plugin's real stream, so the preview lies about what the running plugin is doing (§I-G).\n"+
			"remedy: resolveBindRow must check PluginValues before PreviewMocks — the live frame wins.", got)
	}
}
