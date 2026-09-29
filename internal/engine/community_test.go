package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/ext"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// This file pins J1's preview mode at the composed-frame level: the frame the
// installer draws while browsing a stranger's entry, where the previewed plugin's
// not-yet-satisfied binds resolve to the manifest's declared mocks (Q16), and the
// byte-identical no-op frame the same scene renders with no preview table at all.
// Freezing both pins the substitution AND its inertness above the unit resolver
// checks in preview_mode_test.go.
//
// The Scene 7 installer golden itself (COMMUNITY.json/.frame/.styled) moved to the
// live builder when the keystroke loop landed and now lives in
// community_live_test.go; the static-card InstallerScene it replaced retired. This
// file keeps only the preview goldens, which are orthogonal to that move — a
// stranger's manifest is previewed the same way whether the installer bakes cards
// or binds the fold.

// firstDistinctiveWord returns the first whitespace-delimited token of s at least
// five characters long — a word distinctive enough to witness a description but
// early enough on the line that a narrow column's truncation cannot cut it. It
// returns "" when no such word exists. It is shared with the live installer's
// per-match witness (community_live_test.go).
func firstDistinctiveWord(s string) string {
	for _, w := range strings.Fields(s) {
		if len(w) >= 5 {
			return w
		}
	}
	return ""
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
