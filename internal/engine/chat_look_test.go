package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

func renderLook(t *testing.T, r Renderer, hist []fold.ChatLine) []string {
	t.Helper()
	if r.Width == 0 {
		r.Width = 80
	}
	out := r.renderMarkdown(&scene.Node{Bind: "chat.history"}, fold.State{History: hist}, -1).Plain()
	rows := strings.Split(out, "\n")
	for i := range rows {
		rows[i] = strings.TrimRight(rows[i], " ")
	}
	return rows
}

func toolRow(tool, arg, summary string) fold.ChatLine {
	return fold.ChatLine{Role: "tool", Tool: tool, ToolArg: arg, ToolOK: true, ToolSummary: summary}
}

// The request that started this: a tool with one row of extra information shows it on the
// call's own row.
func TestOneRowToolResultSitsBesideTheCall(t *testing.T) {
	rows := renderLook(t, Renderer{}, []fold.ChatLine{toolRow("read", "arxi.json", "arxi.json does not exist")})
	if len(rows) != 1 || rows[0] != "● Read(arxi.json) - arxi.json does not exist" {
		t.Fatalf("rows = %q", rows)
	}
}

// A result that does not fit beside the call is not cut to make the point: it goes under it.
func TestALongResultGoesUnderTheCall(t *testing.T) {
	long := strings.Repeat("word ", 20)
	rows := renderLook(t, Renderer{Width: 60}, []fold.ChatLine{toolRow("read", "a.go", long)})
	if rows[0] != "● Read(a.go)" || !strings.HasPrefix(rows[1], "  └ word") {
		t.Fatalf("rows = %q", rows)
	}
}

// A command that printed output keeps the elbow: the outcome is the head of a block.
func TestACommandWithOutputKeepsTheElbow(t *testing.T) {
	h := toolRow("run", "go test", "Exit 0 in 2s")
	h.ToolOutput = "exit code 0\nok\nfine"
	rows := renderLook(t, Renderer{}, []fold.ChatLine{h})
	if rows[0] != "● Run(go test)" || rows[1] != "  └ Exit 0 in 2s" || rows[2] != "    ok" {
		t.Fatalf("rows = %q", rows)
	}
}

// The format is the user's: inline off, another separator, another glyph, another title.
func TestToolFormatFollowsTheLook(t *testing.T) {
	look := DefaultChatLook()
	look.ToolInline = false
	look.ToolElbow = "-> "
	rows := renderLook(t, Renderer{Look: &look}, []fold.ChatLine{toolRow("read", "a", "ok")})
	if rows[0] != "● Read(a)" || rows[1] != "  -> ok" {
		t.Fatalf("inline off: %q", rows)
	}
	look = DefaultChatLook()
	look.ToolSep = " :: "
	look.ToolDot = "> "
	look.ToolTitles = map[string]string{"read": "Open"}
	rows = renderLook(t, Renderer{Look: &look}, []fold.ChatLine{toolRow("read", "a", "ok")})
	if rows[0] != "> Open(a) :: ok" {
		t.Fatalf("separator, dot, title: %q", rows)
	}
}

// The reported spacing: blank rows at the edges of a reply or a question, and the empty
// reply of a turn that only called tools, are not drawn as rows of nothing.
func TestNoUnnecessaryBlankRows(t *testing.T) {
	rows := renderLook(t, Renderer{}, []fold.ChatLine{
		{Role: "user", Text: "q\n\n"},
		toolRow("read", "a", "x"),
		{Role: "assistant", Text: "\n\nAnswer\n\n\n", DurationMS: 2000},
		{Role: "assistant", Text: "", DurationMS: 0},
		{Role: "user", Text: "next"},
	})
	want := []string{"┃ q", "", "● Read(a) - x", "", "  Answer", "", "  2s", "", "┃ next"}
	if strings.Join(rows, "|") != strings.Join(want, "|") {
		t.Fatalf("rows = %q\nwant   %q", rows, want)
	}
}

func TestTurnGapIsTheUsers(t *testing.T) {
	look := DefaultChatLook()
	look.TurnGap = 0
	rows := renderLook(t, Renderer{Look: &look}, []fold.ChatLine{{Role: "user", Text: "q"}, {Role: "assistant", Text: "a"}})
	if strings.Join(rows, "|") != "┃ q|  a" {
		t.Fatalf("rows = %q", rows)
	}
}

func TestUsageLineFormatIsTheUsers(t *testing.T) {
	h := fold.ChatLine{DurationMS: 2000, TokensIn: 5, TokensOut: 7}
	if got := usageLine(h, DefaultChatLook()); got != "2s (↑5 ↓7)" {
		t.Fatalf("default = %q", got)
	}
	l := DefaultChatLook()
	l.UsageFormat, l.UsageTokens = "{tokens} in {time}", "{in}/{out} tok"
	if got := usageLine(h, l); got != "5/7 tok in 2s" {
		t.Fatalf("custom = %q", got)
	}
}

// What the user wrote shows three rows and says how many it hides; ctrl+o opens it whole.
func TestUserQuestionIsCutToThreeRowsAndCtrlOOpensIt(t *testing.T) {
	q := fold.ChatLine{Role: "user", Text: "one\ntwo\nthree\nfour\nfive\nsix"}
	rows := renderLook(t, Renderer{}, []fold.ChatLine{q})
	want := []string{"┃ one", "┃ two", "┃ three", "┃ … +3 lines (ctrl+o to expand)"}
	if strings.Join(rows, "|") != strings.Join(want, "|") {
		t.Fatalf("rows = %q", rows)
	}
	rows = renderLook(t, Renderer{ExpandTools: true}, []fold.ChatLine{q})
	if len(rows) != 6 || rows[5] != "┃ six" {
		t.Fatalf("expanded = %q", rows)
	}
	// Three rows, or fewer, are drawn as they are.
	rows = renderLook(t, Renderer{}, []fold.ChatLine{{Role: "user", Text: "one\ntwo\nthree"}})
	if len(rows) != 3 {
		t.Fatalf("three rows cut: %q", rows)
	}
	// A wrapped paragraph counts by the rows it takes.
	rows = renderLook(t, Renderer{Width: 20}, []fold.ChatLine{{Role: "user", Text: strings.Repeat("word ", 30)}})
	if len(rows) != 4 || !strings.Contains(rows[3], "lines") {
		t.Fatalf("wrapped = %q", rows)
	}
}

func TestUserQuestionLimitIsTheUsers(t *testing.T) {
	look := DefaultChatLook()
	look.UserMaxLines = 0
	q := fold.ChatLine{Role: "user", Text: "1\n2\n3\n4\n5"}
	if rows := renderLook(t, Renderer{Look: &look}, []fold.ChatLine{q}); len(rows) != 5 {
		t.Fatalf("0 means all: %q", rows)
	}
	look.UserMaxLines = 2
	if rows := renderLook(t, Renderer{Look: &look}, []fold.ChatLine{q}); len(rows) != 3 {
		t.Fatalf("2 rows + note: %q", rows)
	}
}

// The floating header keeps its own two rows whatever the transcript limit says.
func TestPinnedHeaderIsStillTwoRows(t *testing.T) {
	if pinnedRowsMax != 2 {
		t.Fatalf("pinnedRowsMax = %d", pinnedRowsMax)
	}
}
