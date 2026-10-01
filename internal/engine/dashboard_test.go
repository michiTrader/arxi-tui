package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// dashboardState seeds the four panes with enough content that each draws
// something real — chat history, a task list, a team roster, a model and token
// count — and leaves UIMax empty, the boot state in which the 2×2 grid shows and
// no pane is maximized. ui.max is host view state no arxi-core event produces
// (BINDS.md §4.3, Q21), so a test builds the State the host would, the way the
// config and community tests seed their host-owned binds.
func dashboardState() fold.State {
	return fold.State{
		History: []fold.ChatLine{
			{Role: "user", Text: "status?"},
			{Role: "assistant", Text: "All green."},
		},
		Todos: []fold.TodoItem{
			{Task: "wire /max dispatch", Actor: "backend"},
			{Task: "pin the golden", Actor: "backend"},
		},
		TeamMembers: []fold.TeamMember{
			{ID: "backend", State: "idle", Role: "backend"},
			{ID: "frontend", State: "idle", Role: "frontend"},
		},
		ModelName:         "openai/gpt-5.1",
		SessionTokensUsed: 1234,
		UIMax:             "",
	}
}

func dashboardDoc(t *testing.T) *scene.Document {
	t.Helper()
	data, err := os.ReadFile("../../testdata/DASHBOARD.json")
	if err != nil {
		t.Fatalf("read DASHBOARD.json: %v", err)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("DASHBOARD.json does not validate: %v", err)
	}
	return doc
}

// TestDashboardMaxBindsResolveFromUIMax pins the two derived binds Scene 10 adds
// against the one field they read. ui.max.none is the inversion that gates the
// grid; ui.max.is.<id> is the family that gates each maximized pane. Both must be
// pure functions of UIMax, because that is the single source of truth the design
// chose over mirrored fields — a resolver that ignored UIMax would gate on a
// constant and the dashboard would be stuck in one layout.
func TestDashboardMaxBindsResolveFromUIMax(t *testing.T) {
	// Nothing maximized: the grid gate is open, every pane gate is closed.
	none := fold.State{UIMax: ""}
	if got := resolveBind("ui.max.none", none); got != "true" {
		t.Errorf("ui.max.none with empty UIMax = %q, want \"true\"; the grid would be hidden at boot, the one state it must show", got)
	}
	if got := resolveBind("ui.max.is.chat", none); got != "false" {
		t.Errorf("ui.max.is.chat with empty UIMax = %q, want \"false\"; a maximized pane would draw with nothing maximized", got)
	}

	// chat maximized: the grid gate closes, exactly the chat pane's gate opens.
	chat := fold.State{UIMax: "chat"}
	if got := resolveBind("ui.max.none", chat); got != "false" {
		t.Errorf("ui.max.none with UIMax=chat = %q, want \"false\"; the grid would draw on top of the maximized pane", got)
	}
	if got := resolveBind("ui.max.is.chat", chat); got != "true" {
		t.Errorf("ui.max.is.chat with UIMax=chat = %q, want \"true\"; the maximized pane the user asked for would not draw", got)
	}
	if got := resolveBind("ui.max.is.team", chat); got != "false" {
		t.Errorf("ui.max.is.team with UIMax=chat = %q, want \"false\"; a second pane would draw maximized alongside chat", got)
	}
}

// TestDashboardGridShowsOnlyWhenNothingMaximized pins the layout swap at the
// frame: with nothing maximized the 2×2 grid draws and no maximized pane does;
// with one pane maximized the grid is gone and exactly that pane fills the frame.
// The discriminators are the affordances — "⤢ max" appears only on the grid
// panes, "⤡ restore" only on a maximized one — so each assertion names a string
// that cannot appear in the other state.
func TestDashboardGridShowsOnlyWhenNothingMaximized(t *testing.T) {
	doc := dashboardDoc(t)
	r := Renderer{Width: 80, Height: 24}

	grid := r.RenderFrame(doc, dashboardState()).Plain()
	if !strings.Contains(grid, "⤢ max") {
		t.Errorf("with nothing maximized the grid's maximize buttons are absent; the 2×2 did not draw:\n%s", grid)
	}
	if strings.Contains(grid, "(maximized)") {
		t.Errorf("with nothing maximized a maximized pane drew anyway; ui.max.is.<id> gated open on an empty UIMax:\n%s", grid)
	}

	st := dashboardState()
	st.UIMax = "chat"
	max := r.RenderFrame(doc, st).Plain()
	if !strings.Contains(max, "Chat (maximized)") || !strings.Contains(max, "⤡ restore") {
		t.Errorf("with chat maximized its full pane did not draw; ui.max.is.chat gated closed on UIMax=chat:\n%s", max)
	}
	if strings.Contains(max, "⤢ max") {
		t.Errorf("with chat maximized the 2×2 grid still drew; ui.max.none gated open on a non-empty UIMax:\n%s", max)
	}
	if strings.Contains(max, "Team (maximized)") || strings.Contains(max, "Status (maximized)") {
		t.Errorf("with chat maximized another pane also drew maximized; ui.max.is.<id> is not an equality against UIMax:\n%s", max)
	}
}

// TestDashboardSceneMatchesGolden pins the plain frame of the boot state (the
// 2×2 grid, nothing maximized) — the last of the eleven golden scenes. Regenerate
// with UPDATE_GOLDEN=1 go test ./internal/...
func TestDashboardSceneMatchesGolden(t *testing.T) {
	doc := dashboardDoc(t)
	r := Renderer{Width: 80, Height: 24}
	got := r.RenderFrame(doc, dashboardState()).Plain()

	goldenPath := "../../testdata/DASHBOARD.frame"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run UPDATE_GOLDEN=1 to create it): %v", err)
	}
	if got != string(want) {
		t.Errorf("DASHBOARD render does not match golden; a layout or bind change moved it.\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

// TestDashboardSceneStyledGolden pins the styled frame of the same state, so a
// token change on a pane shows as a golden diff rather than passing silently —
// the styled sweep LESSONS.md records a plain-only golden missing.
func TestDashboardSceneStyledGolden(t *testing.T) {
	doc := dashboardDoc(t)
	r := Renderer{Width: 80, Height: 24}
	got := r.RenderFrame(doc, dashboardState()).Styled()

	goldenPath := "../../testdata/DASHBOARD.styled"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run UPDATE_GOLDEN=1 to create it): %v", err)
	}
	if got != string(want) {
		t.Errorf("DASHBOARD styled render does not match golden.\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}
