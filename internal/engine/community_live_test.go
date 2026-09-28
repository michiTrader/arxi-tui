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
// the Scene 7 golden. Two things are pinned. First, that the live document is
// what the ext builder produces (byte-for-byte, the not-hand-authored guarantee
// COMMUNITY.json carries for the static one). Second — and this is the coverage
// the increment exists for — that a non-empty community.matches actually reaches
// the frame through the row_template, so the projection PR #108 landed is
// exercised end-to-end at the composed-frame level, which no test did before.
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

// liveInstallerDoc reads the pinned live-installer document and validates it
// structurally and against both shipped themes, the same shippability premise
// communityDoc checks for the static scene: a token neither SOBRIA nor Factory
// signs would refuse the one screen whose job is to let the user extend the
// interface, and that must fail here as a named premise, not as an unstyled span
// in a frame diff.
func liveInstallerDoc(t *testing.T) *scene.Document {
	t.Helper()
	doc, err := scene.ParseFile("../../testdata/COMMUNITY-LIVE.json")
	if err != nil {
		t.Fatalf("parse COMMUNITY-LIVE.json: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("the live installer golden must validate structurally; got %v", err)
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
	r := Renderer{Width: 80, Height: 30}
	return r.RenderFrame(liveInstallerDoc(t), liveInstallerState()).Plain()
}

// TestLiveInstallerJSONIsTheBuilderOutput proves testdata/COMMUNITY-LIVE.json is
// byte-for-byte what ext.LiveInstallerScene() builds — the same not-authored-by-
// hand guarantee TestCommunityJSONIsTheInstallerSceneOutput carries for the
// static Scene 7. UPDATE_GOLDEN=1 regenerates it from the builder, the only way
// it is ever written, so the fixture cannot drift from the builder that produces
// it.
func TestLiveInstallerJSONIsTheBuilderOutput(t *testing.T) {
	doc, err := ext.LiveInstallerScene()
	if err != nil {
		t.Fatalf("LiveInstallerScene: %v; the increment's premise is that the builder produces a live installer document", err)
	}
	got := doc.Source()

	goldenPath := "../../testdata/COMMUNITY-LIVE.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, got, 0644); err != nil {
			t.Fatalf("write COMMUNITY-LIVE.json: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read %s: %v", goldenPath, err)
	}
	if string(got) != string(want) {
		t.Errorf("COMMUNITY-LIVE.json is not the LiveInstallerScene output; the pinned live installer document\n"+
			"has drifted from what ext.LiveInstallerScene builds.\n--- builder produced ---\n%s\n--- COMMUNITY-LIVE.json ---\n%s",
			got, string(want))
	}
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
// community.matches case to return nil empties the list, and this fails on the
// first entry — the drop the whole increment guards against.
func TestLiveInstallerDrawsEveryMatch(t *testing.T) {
	got := liveInstallerFrame(t)

	if strings.Contains(got, "UNKNOWN NODE TYPE") {
		t.Fatalf("the live installer rendered an unknown node type — the builder emitted a node the engine\n"+
			"cannot draw:\n%s", got)
	}
	// The search input's placeholder heads the left column. It draws regardless of
	// the folded query today, because renderInput shows a bound value only for
	// user.input (documented in LiveInstallerScene as a deferred increment); when
	// that lands, this assertion changes to the query text and the golden moves.
	if !strings.Contains(got, "search community plugins") {
		t.Errorf("the live installer did not draw its search input; got:\n%s\n"+
			"consequence: the browse affordance is gone, so the user cannot tell the installer is working.\n"+
			"remedy: LiveInstallerScene must head the left column with the community.query input.", got)
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
// row.version) is a golden diff rather than a silent regression. UPDATE_GOLDEN=1
// regenerates it.
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
