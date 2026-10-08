package main

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

// treeOf folds the log, publishes it and returns just the tree block of the detail.
func treeOf(t *testing.T, f *flowScreen, log []fold.Event) string {
	t.Helper()
	st := fold.Fold(log)
	tree, drawn := f.flowTree(&st)
	if !drawn {
		t.Fatalf("no tree was drawn for the log; detail:\n%s", f.detail(&st))
	}
	return tree
}

func TestFlowTreeNestsMembersUnderTheOpenStageAndOpensTheHighlightedOne(t *testing.T) {
	log := append(teamLog()[:5],
		toolEv(6, "tool.call", "backend", "read", nil),
		toolEv(7, "tool.call_completed", "backend", "read", nil),
		toolEv(8, "tool.call", "frontend", "edit", nil),
	)
	got := treeOf(t, &flowScreen{}, log)
	want := strings.Join([]string{
		"build ●",
		"├─ ▸ backend · implementer — ● thinking",
		"│    tools used:",
		"│    ✓ read",
		"└─ frontend · implementer — ● tool",
	}, "\n")
	if got != want {
		t.Fatalf("tree:\n%s\nwant:\n%s", got, want)
	}
}

func TestFlowTreeMovesItsOpenBranchWithTheHighlight(t *testing.T) {
	log := append(teamLog()[:5], toolEv(6, "tool.call", "frontend", "edit", nil))
	f := &flowScreen{sel: 1}
	got := treeOf(t, f, log)
	for _, want := range []string{"├─ backend · implementer", "└─ ▸ frontend · implementer", "     ● edit — running"} {
		if !strings.Contains(got, want) {
			t.Errorf("tree lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "has not used any tool yet") && strings.Contains(got, "│    has not") {
		t.Errorf("the closed branch must not list its tools:\n%s", got)
	}
}

func TestFlowTreeSaysAMemberHasUsedNoTool(t *testing.T) {
	got := treeOf(t, &flowScreen{}, teamLog())
	if !strings.Contains(got, "│    has not used any tool yet") {
		t.Fatalf("tree:\n%s", got)
	}
}

func TestFlowTreeKeepsOnlyTheLatestTools(t *testing.T) {
	log := teamLog()[:5]
	for i := 0; i < 8; i++ {
		seq := int64(10 + 2*i)
		name := string(rune('a' + i))
		log = append(log, toolEv(seq, "tool.call", "backend", name, nil), toolEv(seq+1, "tool.call_completed", "backend", name, nil))
	}
	got := treeOf(t, &flowScreen{}, log)
	if !strings.Contains(got, "last 5 of 8 tool calls") || strings.Contains(got, "✓ c\n") || !strings.Contains(got, "✓ h") {
		t.Fatalf("tree:\n%s", got)
	}
}

func TestFlowTreeIsNotDrawnWithoutAnOpenStageAndTheFlatStoryTakesOver(t *testing.T) {
	// The only stage the log knows has been left: nothing is open to hang members on.
	log := append(teamLog()[:5],
		flowEv(6, "stage.advanced", "", map[string]any{"from": "build", "to": "review", "to_index": float64(1)}))
	st := fold.Fold(log)
	if n := len(st.StageRun); n == 0 || !st.StageRun[n-1].Left {
		t.Fatalf("the test needs a run whose last stage was left, StageRun = %+v", st.StageRun)
	}
	f := &flowScreen{}
	if tree, drawn := f.flowTree(&st); drawn || tree != "" {
		t.Fatalf("a tree was drawn with no open stage: %q", tree)
	}
	if d := f.detail(&st); !strings.Contains(d, "▸ backend") {
		t.Fatalf("the flat member story must stand in:\n%s", d)
	}
}

func TestFlowTreeIsAbsentForAPlainChat(t *testing.T) {
	st := fold.Fold([]fold.Event{flowEv(1, "run.prompt", "", map[string]any{"text": "hi"})})
	if tree, drawn := (&flowScreen{}).flowTree(&st); drawn || tree != "" {
		t.Fatalf("tree = %q", tree)
	}
}

func TestFlowDetailDrawsTheTreeAndNotTheStoryTwice(t *testing.T) {
	st := fold.Fold(teamLog())
	f := &flowScreen{}
	f.publish(&st)
	if n := strings.Count(st.HubDetail, "backend · implementer"); n != 1 {
		t.Fatalf("the highlighted member is told %d times:\n%s", n, st.HubDetail)
	}
	if !strings.Contains(st.HubDetail, "├─ ▸ backend") || !strings.Contains(st.HubDetail, "└─ frontend") {
		t.Fatalf("detail has no tree:\n%s", st.HubDetail)
	}
	f.key(term.Key{Type: term.KeyDown})
	f.publish(&st)
	if !strings.Contains(st.HubDetail, "└─ ▸ frontend") {
		t.Fatalf("the highlight did not move in the tree:\n%s", st.HubDetail)
	}
}

// The renderer must keep every branch on its own row, with the tree glyphs and the
// indentation intact, or the nesting would collapse into one paragraph.
func TestFlowTreeSurvivesTheMarkdownRenderer(t *testing.T) {
	st := fold.Fold(teamLog())
	f := &flowScreen{}
	f.publish(&st)
	var rows []string
	for _, l := range ui.RenderMarkdown(st.HubDetail, 80, "") {
		var b strings.Builder
		for _, sp := range l {
			b.WriteString(sp.Text)
		}
		rows = append(rows, b.String())
	}
	joined := strings.Join(rows, "\n")
	for _, want := range []string{"├─ ▸ backend", "│    has not used any tool yet", "└─ frontend"} {
		found := false
		for _, r := range rows {
			if strings.Contains(r, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("no rendered row contains %q:\n%s", want, joined)
		}
	}
}
