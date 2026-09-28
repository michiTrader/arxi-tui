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

// This file pins the LiveInstallerScene increment (DESIGN-BLOCK-J.md J3
// follow-up): the community installer as a live document that binds to the
// community.* view state, rather than the static-card InstallerScene J5 froze as
// the Scene 7 golden. The coverage the increment exists for is that a non-empty
// community.matches actually reaches the frame through the row_template, so the
// projection PR #108 landed is exercised end-to-end at the composed-frame level,
// which no test did before.
//
// The live installer is NOT a twelfth scene: it is the in-progress variant of
// Scene 7 that will replace the static one when the keystroke loop mounts it, at
// which point COMMUNITY.json moves to this builder in its own mutation family.
// So it is rendered straight from ext.LiveInstallerScene() rather than pinned as
// a top-level testdata/*.json fixture — the same choice previewScene makes for
// the preview chrome, and for the same reason: a top-level .json is enumerated as
// a shipped scene by the progress audit (it would report a phantom Scene 12) and
// by the nested-node sweep. Rendering from the builder still catches drift — the
// .frame/.styled goldens ARE the builder output, so a builder change is a golden
// diff — without inventing a scene the documents do not describe.
//
// The fold state here is NON-empty on purpose, unlike communityFrame's empty
// fold for the static scene: the static installer bakes its content, so its
// deterministic frame needs no host activity, but the live installer's whole
// point is that its content is the folded matches. An empty fold would render a
// bare browse and witness nothing about the binding, so the golden that proves
// the wiring must fold the matches the loop will later write.

// liveInstallerMatches is the folded browse state the live-frame goldens render:
// two filtered entries with a query, the shape Registry.FilterEntries produces
// for the loop. It is fixed here (not read from the registry index) because the
// live scene reads the fold, not the index — the golden pins what the engine
// draws from view state, and inventing the state here is the point, not a
// shortcut around a fixture.
func liveInstallerState() fold.State {
	return fold.State{
		CommunityQuery: "tick",
		CommunityMatches: []fold.CommunityMatch{
			{
				ID:          "community-ticker",
				Name:        "Community Ticker",
				Version:     "0.1.0",
				ManifestURL: "https://example.com/ticker/manifest.json",
				Description: "A streaming price ticker for the status row.",
				Preview:     "# Ticker\nStreams a price into the status row.",
			},
			{
				ID:          "focus-timer",
				Name:        "Focus Timer",
				Version:     "1.2.0",
				ManifestURL: "https://example.com/focus/manifest.json",
				Description: "A pomodoro countdown mounted as an overlay.",
				Preview:     "# Focus Timer\nA pomodoro countdown overlay.",
			},
		},
	}
}

// liveInstallerDoc builds the live installer straight from ext.LiveInstallerScene
// and validates it structurally and against both shipped themes. It renders the
// builder output rather than a pinned file so the .frame/.styled goldens cannot
// drift from the builder that produces them — the not-hand-authored guarantee,
// held here without a top-level .json fixture the progress audit would count as a
// scene. The token check is the same shippability premise communityDoc makes: a
// token neither SOBRIA nor Factory signs would refuse the one screen whose job is
// to let the user extend the interface, and that must fail here as a named
// premise, not as an unstyled span in a frame diff.
func liveInstallerDoc(t *testing.T) *scene.Document {
	t.Helper()
	doc, err := ext.LiveInstallerScene()
	if err != nil {
		t.Fatalf("LiveInstallerScene: %v; the increment's premise is that the builder produces a live installer document", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("the live installer document must validate structurally; got %v", err)
	}
	for _, th := range []struct {
		name string
		th   *theme.Theme
	}{{"SOBRIA", theme.SOBRIA()}, {"Factory", theme.Factory()}} {
		if errs := scene.ValidateTokens(doc, th.th); len(errs) > 0 {
			t.Fatalf("premise broken: the live installer names a token %s does not ship, so it is not a\n"+
				"shippable scene; got %v", th.name, errs[0])
		}
	}
	return doc
}

// liveInstallerFrame renders the live installer over the folded browse state, the
// single frame the witness and both goldens share so they cannot drift on which
// frame is asserted.
func liveInstallerFrame(t *testing.T) string {
	t.Helper()
	return liveInstallerFrameWith(t, liveInstallerState())
}

// liveInstallerFrameWith renders the live installer over an arbitrary view state.
// It exists so the search-input witness can drive community.query directly —
// with a distinctive query and with an empty one — without disturbing the shared
// golden state the sibling tests pin.
func liveInstallerFrameWith(t *testing.T, state fold.State) string {
	t.Helper()
	r := Renderer{Width: 80, Height: 30}
	return r.RenderFrame(liveInstallerDoc(t), state).Plain()
}

// TestLiveInstallerDrawsEveryMatch is the reason this increment has a test at
// all: it asserts that each folded community.matches entry reached the frame
// through the row_template, so the community.matches projection (rowScopesFor,
// PR #108) is proven at the composed-frame level and not just at the unit
// resolver. A regression that stopped instantiating the template — or a builder
// that dropped the list — fails here with a named consequence, ahead of the
// opaque byte golden.
//
// Counterfactual (run by hand, reported in the commit): reverting rowScopesFor's
// community.matches case to range an empty slice empties the list, and this fails
// on the first entry — the drop the whole increment guards against.
func TestLiveInstallerDrawsEveryMatch(t *testing.T) {
	got := liveInstallerFrame(t)

	if strings.Contains(got, "UNKNOWN NODE TYPE") {
		t.Fatalf("the live installer rendered an unknown node type — the builder emitted a node the engine\n"+
			"cannot draw:\n%s", got)
	}
	// The search input now shows the folded community.query, not its placeholder:
	// renderInput resolves any view-state bind, so with a query folded the hint is
	// replaced by the query text. The absence of the placeholder is the composed-
	// frame witness of that change; the query text itself is asserted with a
	// distinctive value in TestLiveInstallerSearchInputShowsTheQuery, because "tick"
	// is a substring of a description below and would pass here by coincidence.
	if strings.Contains(got, "search community plugins") {
		t.Errorf("the live installer still drew its search placeholder while community.query was folded; got:\n%s\n"+
			"consequence: the typed query does not reach the box, so the search reads as dead the moment the user types.\n"+
			"remedy: renderInput must draw the placeholder only when the resolved bind value is empty.", got)
	}
	for _, m := range liveInstallerState().CommunityMatches {
		if !strings.Contains(got, m.Name) {
			t.Errorf("match %q did not show its name %q in the rendered frame; got:\n%s\n"+
				"consequence: a folded community.matches entry did not reach the frame, so the live list is\n"+
				"not drawing what the keystroke loop writes.\n"+
				"remedy: rowScopesFor must instantiate the row_template over community.matches, and\n"+
				"liveInstallerList must bind row.name.", m.ID, m.Name, got)
		}
		if word := firstDistinctiveWord(m.Description); word != "" && !strings.Contains(got, word) {
			t.Errorf("match %q did not show %q from its description in the rendered frame; got:\n%s\n"+
				"consequence: the row's description did not reach the screen, so matches are\n"+
				"indistinguishable in the live list.\n"+
				"remedy: liveInstallerList must bind row.description.", m.ID, word, got)
		}
	}
}

// TestLiveInstallerSearchInputShowsTheQuery proves the increment the sibling
// goldens moved for: renderInput now projects community.query, so the search box
// shows what the keystroke loop wrote instead of the frozen placeholder. The
// query used here appears in no match name or description, so a plain-frame
// witness cannot pass on a coincidental substring of the list below it.
//
// Both directions are asserted, because a one-directional check would pass on the
// old engine too. A non-empty query must replace the placeholder — the behavior
// added — and an empty query must restore it: renderInput collapses a resolved-
// empty bind back to the hint, and that half is the guard against a change that
// draws the query but forgets community.query starts empty, which would strand
// the user with no hint at all. On the old engine (bound value drawn only for
// user.input) the first assertion fails, so this test is armed on exactly the
// state it exists to certify.
func TestLiveInstallerSearchInputShowsTheQuery(t *testing.T) {
	const distinctive = "quux-not-in-any-row"

	withQuery := liveInstallerFrameWith(t, fold.State{CommunityQuery: distinctive})
	if !strings.Contains(withQuery, distinctive) {
		t.Errorf("the search input did not show the folded community.query %q; got:\n%s\n"+
			"consequence: the user types into the installer and the box never changes, so the search reads as dead.\n"+
			"remedy: renderInput must resolve n.Bind through resolveBind, not render a value only for user.input.", distinctive, withQuery)
	}
	if strings.Contains(withQuery, "search community plugins") {
		t.Errorf("the search input still drew its placeholder while a query was folded; got:\n%s\n"+
			"consequence: the placeholder and the typed query cannot both be the line — the hint welds onto the query.\n"+
			"remedy: renderInput must draw the placeholder only when the resolved value is empty.", withQuery)
	}

	empty := liveInstallerFrameWith(t, fold.State{CommunityQuery: ""})
	if !strings.Contains(empty, "search community plugins") {
		t.Errorf("the search input did not draw its placeholder for an empty community.query; got:\n%s\n"+
			"consequence: the installer opens with no hint, so the user cannot tell the box is a search field.\n"+
			"remedy: renderInput must fall back to the placeholder when the resolved bind is empty.", empty)
	}
}

// TestLiveInstallerHighlightsTheSelectedRow proves the row-selection highlight:
// the row whose index equals community.selected wears the leading marker and no
// other row does. It drives community.selected across both entries so the marker
// must MOVE — a one-position check would pass on an engine that marked a fixed
// row (e.g. always the first) regardless of the cursor, which is the exact defect
// a synthesized-per-row boolean has to avoid.
//
// The assertion is on marker+name glued together, searched frame-wide, rather
// than on the line the name sits on. That changed when the preview pane landed:
// the selected entry's name now appears twice in the frame — once in its list
// row and once as the preview heading — so "the first line containing the name"
// became ambiguous and could pick the preview occurrence, which never carries the
// marker. The marker is only ever emitted by the list row_template, so marker+name
// identifies the marked list row uniquely wherever it lands, and its absence for
// the other entry proves exactly one row is marked. This is strictly stronger than
// the old line lookup: gluing the marker to a specific name proves the marker is
// on THAT row, which is the property the line lookup was approximating.
//
// Counterfactual (run by hand, reported in the commit): neutering rowScopesFor
// to synthesize row.selected as boolField(false) for every row drops the marker
// entirely, and this fails on the "selected row is not marked" assertion — the
// highlight the increment adds. Pinning it to boolField(true) marks every row and
// fails the "unselected row is marked" assertion. Both directions are guarded.
func TestLiveInstallerHighlightsTheSelectedRow(t *testing.T) {
	const marker = "> "

	base := liveInstallerState()
	names := []string{base.CommunityMatches[0].Name, base.CommunityMatches[1].Name}

	for selected := 0; selected < len(names); selected++ {
		state := liveInstallerState()
		state.CommunitySelected = selected
		frame := liveInstallerFrameWith(t, state)

		if !strings.Contains(frame, marker+names[selected]) {
			t.Errorf("with community.selected=%d the selected row %q was not marked (no %q in the frame); got:\n%s\n"+
				"consequence: the user cannot see which entry Enter will install, so the cursor the loop moves is invisible.\n"+
				"remedy: rowScopesFor must set row.selected true on the row whose index equals community.selected, and\n"+
				"liveInstallerList must gate the marker on when:\"row.selected\".", selected, names[selected], marker+names[selected], frame)
		}

		other := 1 - selected
		if strings.Contains(frame, marker+names[other]) {
			t.Errorf("with community.selected=%d the unselected row %q was also marked (%q present); got:\n%s\n"+
				"consequence: every row is highlighted, so the highlight distinguishes nothing — a menu with no single\n"+
				"bright row while Enter still acts is a menu that lies about what it will do.\n"+
				"remedy: rowScopesFor must set row.selected true on exactly one row (index == community.selected).", selected, names[other], marker+names[other], frame)
		}
	}
}

// TestLiveInstallerPreviewPaneShowsTheSelectedEntry proves the third live
// affordance: the right pane previews the entry community.selected points at, and
// the preview MOVES with the selection. It drives community.selected across two
// entries and asserts the selected entry's preview blurb is on screen and the
// other's is not.
//
// The witness is the preview *blurb*, not the name or version, and that choice is
// load-bearing. A name or version appears in every list row already, so "the
// selected name is on screen" is true no matter what the pane draws — it would
// pass on a pane that showed nothing. The preview field is the one entry field
// the list does not render, so a sentinel placed in it can only reach the frame
// through community.selected.preview and the pane that binds it. Each blurb is a
// distinct sentinel present nowhere else — not in the name, version or
// description — so a plain-frame Contains cannot pass on a coincidental substring,
// the same discipline TestLiveInstallerSearchInputShowsTheQuery uses for the
// query.
//
// Both directions are asserted: the selected blurb present proves the pane draws
// the selection, and the other blurb absent proves it draws only the selection —
// a pane that dumped every entry's preview, or one pinned to the first entry,
// fails the second assertion. On an engine that dropped the markdown bind (the
// pre-change default that drew n.Text alone) neither blurb appears and the first
// assertion fails, so the test is armed on the state it certifies.
func TestLiveInstallerPreviewPaneShowsTheSelectedEntry(t *testing.T) {
	// Sentinels with no spaces or hyphens so WrapText keeps each on one line in
	// the narrow pane, and a "zqx" prefix no name/version/description carries.
	blurbs := []string{"zqxPreviewOfAlpha", "zqxPreviewOfBeta"}
	state := fold.State{
		CommunityMatches: []fold.CommunityMatch{
			{Name: "Alpha", Version: "1.0.0", Description: "the first entry", Preview: blurbs[0]},
			{Name: "Beta", Version: "2.0.0", Description: "the second entry", Preview: blurbs[1]},
		},
	}

	for selected := 0; selected < len(blurbs); selected++ {
		s := state
		s.CommunitySelected = selected
		frame := liveInstallerFrameWith(t, s)

		if !strings.Contains(frame, blurbs[selected]) {
			t.Errorf("with community.selected=%d the preview pane did not show the selected entry's preview %q; got:\n%s\n"+
				"consequence: the right pane does not follow the selection, so moving the cursor previews nothing — the\n"+
				"selection-driven preview the increment adds is dead.\n"+
				"remedy: liveInstallerPreview must bind community.selected.preview, resolveBind must project it from\n"+
				"selectedCommunityMatch, and renderMarkdown must resolve the bind rather than draw its literal text.", selected, blurbs[selected], frame)
		}

		other := 1 - selected
		if strings.Contains(frame, blurbs[other]) {
			t.Errorf("with community.selected=%d the preview pane also showed the unselected entry's preview %q; got:\n%s\n"+
				"consequence: the pane shows more than the selection, so it is the static card list again, not a preview of\n"+
				"the one entry the cursor is on.\n"+
				"remedy: community.selected.preview must resolve the single selected entry (selectedCommunityMatch), not\n"+
				"every match.", selected, blurbs[other], frame)
		}
	}
}

// TestLiveInstallerFrameMatchesGolden freezes the plain frame over the folded
// browse state. UPDATE_GOLDEN=1 regenerates it. This is the durable pin: a change
// to the live builder, the row template, or the engine's rendering of
// community.matches is a reviewable golden diff here.
func TestLiveInstallerFrameMatchesGolden(t *testing.T) {
	got := liveInstallerFrame(t)

	goldenPath := "../../testdata/COMMUNITY-LIVE.frame"
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
		t.Errorf("live installer frame does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

// TestLiveInstallerStyledGolden freezes the styled frame, so a dropped or changed
// style token on the live installer (the header on row.name, the dim on
// row.version) is a golden diff rather than a silent regression. It also carries
// the nested-node token coverage for this document: the row_template's header and
// dim spans are exercised here with the matches folded, which is why the
// package-wide nested sweep (which folds an empty state) does not need to reach
// this variant. UPDATE_GOLDEN=1 regenerates it.
func TestLiveInstallerStyledGolden(t *testing.T) {
	r := Renderer{Width: 80, Height: 30}
	got := r.RenderFrame(liveInstallerDoc(t), liveInstallerState()).Styled()

	goldenPath := "../../testdata/COMMUNITY-LIVE.styled"
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
		t.Errorf("live installer styled output does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}
