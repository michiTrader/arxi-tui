package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/ext"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// J5 freezes the Scene 7 golden (SCENES.md — COMMUNITY): the community installer
// authored as a scene, rendered through the ordinary engine path. Three concerns
// are pinned here, the three the J1/J3 notes name for J5.
//
// First, the installer scene itself. testdata/COMMUNITY.json is not hand-authored:
// TestCommunityJSONIsTheInstallerSceneOutput proves it is byte-for-byte what
// Registry.InstallerScene() produces from the fixed index in
// testdata/registries/COMMUNITY.registry.json — the same not-authored-by-hand
// guarantee TICKER.json carries for the mount output. So the pinned Scene 7 scene
// is the real host-generated installer, and a drift in the builder is a golden
// diff rather than a divergence nobody sees.
//
// Second and third, J1's preview mode: the frame the installer draws while
// browsing a stranger's entry, where the previewed plugin's not-yet-satisfied
// binds resolve to the manifest's declared mocks (Q16), and the byte-identical
// no-op frame the same scene renders with no preview table at all. Freezing both
// pins the substitution AND its inertness at the composed-frame level, above the
// unit resolver checks in preview_mode_test.go — the golden the J1 note defers to
// J5.

// communityRegistry parses and validates the fixed Scene 7 index. It fails the
// test if the index does not validate, because J5 must start from an index J2
// accepts — otherwise the golden would be measuring the registry loader, not the
// installer scene it builds.
func communityRegistry(t *testing.T) *ext.Registry {
	t.Helper()
	src, err := os.ReadFile("../../testdata/registries/COMMUNITY.registry.json")
	if err != nil {
		t.Fatalf("read COMMUNITY.registry.json: %v", err)
	}
	r, err := ext.ParseRegistryNamed("COMMUNITY.registry.json", src)
	if err != nil {
		t.Fatalf("ParseRegistryNamed(community index): %v", err)
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("the community index does not validate: %v; J5 must start from an index J2 accepts", err)
	}
	return r
}

// communityDoc reads the pinned Scene 7 document — the installer scene contract
// the frame and styled goldens render — and validates it structurally and against
// both themes it might be drawn under (SOBRIA default, Factory backstop). A token
// neither theme signs would refuse the one screen whose whole job is to let the
// user extend the interface, so the shippability premise is checked here rather
// than discovered as an unstyled span in the golden.
func communityDoc(t *testing.T) *scene.Document {
	t.Helper()
	doc, err := scene.ParseFile("../../testdata/COMMUNITY.json")
	if err != nil {
		t.Fatalf("parse COMMUNITY.json: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("the Scene 7 installer golden must validate structurally; got %v", err)
	}
	for _, th := range []struct {
		name string
		th   *theme.Theme
	}{{"SOBRIA", theme.SOBRIA()}, {"Factory", theme.Factory()}} {
		if errs := scene.ValidateTokens(doc, th.th); len(errs) > 0 {
			t.Fatalf("premise broken: the installer names a token %s does not ship, so it is not a\n"+
				"shippable scene; got %v", th.name, errs[0])
		}
	}
	return doc
}

// communityFrame renders the installer at a fixed size, the single source of truth
// the witness and both golden tests share so they cannot drift on which frame is
// asserted. The fold state is empty: the installer is host-generated static chrome
// (baked cards, J3), so it is deterministic without inventing host activity the
// scene is not about.
func communityFrame(t *testing.T) string {
	t.Helper()
	r := Renderer{Width: 80, Height: 30}
	return r.RenderFrame(communityDoc(t), fold.Fold(nil)).Plain()
}

// TestCommunityJSONIsTheInstallerSceneOutput proves testdata/COMMUNITY.json is
// exactly what Registry.InstallerScene() builds from the fixed index — so the
// pinned Scene 7 contract is the real host-generated installer, not a hand-authored
// lookalike. InstallerScene parses its assembled JSON through scene.ParseNamed, so
// doc.Source() is the marshalled bytes the way patch.Mount's res.Source is for
// TICKER.json. UPDATE_GOLDEN=1 regenerates COMMUNITY.json from the builder, the
// only way it is ever written: the fixture cannot drift from InstallerScene,
// because InstallerScene is what produces it.
func TestCommunityJSONIsTheInstallerSceneOutput(t *testing.T) {
	doc, err := communityRegistry(t).InstallerScene()
	if err != nil {
		t.Fatalf("InstallerScene: %v; J5's premise is that the index builds an installer scene", err)
	}
	got := doc.Source()

	goldenPath := "../../testdata/COMMUNITY.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, got, 0644); err != nil {
			t.Fatalf("write COMMUNITY.json: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read %s: %v", goldenPath, err)
	}
	if string(got) != string(want) {
		t.Errorf("COMMUNITY.json is not the InstallerScene output; the pinned Scene 7 scene has drifted\n"+
			"from what Registry.InstallerScene builds from the index.\n--- builder produced ---\n%s\n--- COMMUNITY.json ---\n%s",
			got, string(want))
	}
}

// TestCommunitySceneWitnessesEveryEntry asserts, before the byte-for-byte golden,
// that every entry the index lists actually reached the frame with the content the
// user browses by, and that the search input is drawn. A regression that drops a
// card fails here with a named consequence rather than only as an opaque golden
// diff.
//
// The frame word-wraps and the narrow left column truncates a `text` node's line,
// so neither the whole description nor its longest word is guaranteed on screen;
// the check is the entry's name (which fits one line) plus the first distinctive
// word of its description, which renders at the line's start where truncation
// cannot reach it. The full description-reaches-a-node property is the ext test's
// (TestInstallerSceneShowsEntryContent, over node text); this witnesses that the
// card composed and drew into the frame.
func TestCommunitySceneWitnessesEveryEntry(t *testing.T) {
	got := communityFrame(t)

	if strings.Contains(got, "UNKNOWN NODE TYPE") {
		t.Fatalf("the Scene 7 installer rendered an unknown node type — the builder emitted a node the\n"+
			"engine cannot draw:\n%s", got)
	}
	// The search input's placeholder heads the left column regardless of entry
	// count; a missing one means the browse affordance did not render.
	if !strings.Contains(got, "search community plugins") {
		t.Errorf("the installer did not draw its search input placeholder; got:\n%s\n"+
			"consequence: the browse affordance is gone, so the user cannot tell the installer is working.\n"+
			"remedy: InstallerScene must head the left column with the search input.", got)
	}
	for _, e := range communityRegistry(t).Entries {
		if !strings.Contains(got, e.Name) {
			t.Errorf("entry %q does not show its name %q in the rendered frame; got:\n%s\n"+
				"consequence: a card did not render, so an entry the index lists is invisible in the installer.\n"+
				"remedy: installerCard must draw the entry name.", e.ID, e.Name, got)
		}
		if word := firstDistinctiveWord(e.Description); word != "" && !strings.Contains(got, word) {
			t.Errorf("entry %q does not show %q from its description in the rendered frame; got:\n%s\n"+
				"consequence: the card's description did not reach the screen, so entries are\n"+
				"indistinguishable in the list.\n"+
				"remedy: installerCard must draw the entry description.", e.ID, word, got)
		}
	}
}

// firstDistinctiveWord returns the first whitespace-delimited token of s at least
// five characters long — a word distinctive enough to witness the description but
// early enough on the line that the narrow column's truncation cannot cut it. It
// returns "" when no such word exists.
func firstDistinctiveWord(s string) string {
	for _, w := range strings.Fields(s) {
		if len(w) >= 5 {
			return w
		}
	}
	return ""
}

// TestCommunitySceneMatchesGolden freezes the plain frame. UPDATE_GOLDEN=1
// regenerates it. This is the durable pin: a change to the installer builder, the
// card shape, or the engine's rendering of it shows up here as a reviewable golden
// diff.
func TestCommunitySceneMatchesGolden(t *testing.T) {
	got := communityFrame(t)

	goldenPath := "../../testdata/COMMUNITY.frame"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	if got != string(want) {
		t.Errorf("Scene 7 installer frame does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

// TestCommunitySceneStyledGolden freezes the styled frame, so a dropped or changed
// style token on the installer is a golden diff rather than a silent regression.
// UPDATE_GOLDEN=1 regenerates it.
func TestCommunitySceneStyledGolden(t *testing.T) {
	r := Renderer{Width: 80, Height: 30}
	got := r.RenderFrame(communityDoc(t), fold.Fold(nil)).Styled()

	goldenPath := "../../testdata/COMMUNITY.styled"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	if got != string(want) {
		t.Errorf("Scene 7 installer styled output does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

// previewScene is the chrome the installer draws for a browsed entry: a titled
// box whose two text nodes bind the previewed plugin's `<plugin-id>.*` fields.
// tick.price is mocked by the manifest and tick.status is declared with no mock,
// so one field witnesses the substitution and the other witnesses the honest gap
// that survives it. It is authored inline like tickerHost — the pinned artifact is
// the rendered frame, not this fragment — and its binds are a stranger's, so it is
// rendered without host validation exactly as preview_mode_test's nodes are.
const previewScene = `{ "root": { "type": "box", "border": "single",
  "title": "Preview: Community Ticker", "children": [
  { "type": "stack", "children": [
    { "type": "text", "text": "price:" },
    { "id": "price",  "type": "text", "bind": "tick.price" },
    { "type": "text", "text": "status:" },
    { "id": "status", "type": "text", "bind": "tick.status" }
  ]}
]}}`

// previewMocks loads the behavioral manifest with declared mocks and projects its
// preview table. It fails the test if the manifest does not parse, because the
// preview frame must start from a table the ext builder actually produces — a mock
// invented in the test would pin a frame no manifest yields.
func previewMocks(t *testing.T) map[string]string {
	t.Helper()
	src, err := os.ReadFile("../../testdata/plugins/COMMUNITY-PREVIEW.manifest.json")
	if err != nil {
		t.Fatalf("read COMMUNITY-PREVIEW.manifest.json: %v", err)
	}
	m, err := ext.ParseNamed("COMMUNITY-PREVIEW.manifest.json", src)
	if err != nil {
		t.Fatalf("ParseNamed(preview manifest): %v", err)
	}
	mocks := m.PreviewMocks()
	if mocks["tick.price"] == "" {
		t.Fatalf("premise broken: the preview manifest declares no tick.price mock, so the preview frame\n"+
			"would witness nothing; got table %v", mocks)
	}
	return mocks
}

// previewFrame renders previewScene with a preview table attached (mocks != nil)
// or with none (mocks == nil), the shared frame the witness and both preview
// goldens assert so they cannot drift on which frame is pinned.
func previewFrame(t *testing.T, mocks map[string]string) string {
	t.Helper()
	doc, err := scene.ParseDocument([]byte(previewScene))
	if err != nil {
		t.Fatalf("parse previewScene: %v", err)
	}
	r := Renderer{Width: 48, Height: 10, PreviewMocks: mocks}
	return r.RenderFrame(doc, fold.Fold(nil)).Plain()
}

// TestCommunityPreviewFrameWitnessesTheMocks is the composed-frame half of J1: the
// installer's preview pane, rendered with the browsed manifest's mock table, shows
// the mocked bind as the stranger's declared placeholder text and the un-mocked
// bind as the honest "no value" placeholder — both on one frame, so the test
// cannot pass by the two halves quietly agreeing. Counterfactual (run by hand):
// neutering resolveBindRow's preview lookup drops "$1.23" and this fails.
func TestCommunityPreviewFrameWitnessesTheMocks(t *testing.T) {
	got := previewFrame(t, previewMocks(t))

	if !strings.Contains(got, "$1.23") {
		t.Errorf("the preview frame did not render the manifest's tick.price mock; got:\n%s\n"+
			"consequence: the installer's preview pane shows a stranger's scene as a wall of placeholders,\n"+
			"so a user cannot see what a plugin looks like before installing it (Q16 / Scene 7).\n"+
			"remedy: resolveBindRow must consult PreviewMocks before the placeholder.", got)
	}
	// tick.status is declared with no mock, so it stays the honest placeholder even
	// under preview — the "no declared value" state, distinct from a mocked one.
	if !strings.Contains(got, placeholderValue) {
		t.Errorf("the preview frame did not render the placeholder for the un-mocked tick.status; got:\n%s\n"+
			"consequence: an entry that mocks only some of its binds cannot be previewed without the\n"+
			"un-mocked ones drawing something other than the honest gap.\n"+
			"remedy: a preview miss must fall through to the placeholder, as a live-snapshot miss does.", got)
	}
}

// TestCommunityPreviewNilFrameIsTheHonestGap pins the no-op frame: the same preview
// scene with no table at all draws every bind as the placeholder, which is what a
// pre-J1 render produced. Freezing it beside the mocked frame makes the difference
// between "browsing an entry" and "not browsing" a reviewable two-golden diff, and
// guards the SOBRIA-zero-row trap — that adding preview mode leaves the un-browsed
// frame byte-identical. The mocked value must be absent here, or the "no-op" frame
// is quietly showing a mock.
func TestCommunityPreviewNilFrameIsTheHonestGap(t *testing.T) {
	got := previewFrame(t, nil)

	if strings.Contains(got, "$1.23") {
		t.Errorf("the nil-preview frame rendered a mock value; got:\n%s\n"+
			"consequence: preview mode is not inert with no table, so entering the installer would rewrite\n"+
			"the un-browsed frame — the trap where an inert change silently moves an unrelated golden.\n"+
			"remedy: a nil PreviewMocks must resolve every plugin bind to the placeholder.", got)
	}
	if !strings.Contains(got, placeholderValue) {
		t.Fatalf("precondition: with no table both binds must be the placeholder; got:\n%s", got)
	}
}

// TestCommunityPreviewFrameMatchesGolden freezes the preview frame (mocks attached).
// UPDATE_GOLDEN=1 regenerates it.
func TestCommunityPreviewFrameMatchesGolden(t *testing.T) {
	got := previewFrame(t, previewMocks(t))

	goldenPath := "../../testdata/COMMUNITY-PREVIEW.frame"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	if got != string(want) {
		t.Errorf("Scene 7 preview frame does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

// TestCommunityPreviewNilFrameMatchesGolden freezes the no-op frame (no table).
// UPDATE_GOLDEN=1 regenerates it. Pinned separately from the mocked frame so a
// regression that made preview leak into the un-browsed render is a diff on this
// file, not a silent merge of the two.
func TestCommunityPreviewNilFrameMatchesGolden(t *testing.T) {
	got := previewFrame(t, nil)

	goldenPath := "../../testdata/COMMUNITY-PREVIEW-NIL.frame"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	if got != string(want) {
		t.Errorf("Scene 7 nil-preview frame does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}
