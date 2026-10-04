package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func plain(lines []Line) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.Text()
	}
	return out
}

func render(md string, w int) []string { return plain(RenderMarkdown(md, w, "text")) }

func TestMarkdownHeadingsAndInline(t *testing.T) {
	got := render("# Title\n\nSome **bold**, *em*, `code` and [site](https://x.io).", 80)
	want := []string{"Title", "", "Some bold, em, code and site (https://x.io)."}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got %q want %q", got, want)
	}
	// the markers must be gone and the styles applied to the right runs
	lines := RenderMarkdown("a **b** `c`", 80, "text")
	styles := map[string]string{}
	for _, s := range lines[0] {
		styles[s.Text] = s.Style
	}
	if styles["b"] != "markdown.strong" || styles["c"] != "markdown.code" || styles["a"] != "text" {
		t.Errorf("inline styles = %v", styles)
	}
}

func TestMarkdownListsHangUnderTheirText(t *testing.T) {
	got := render("- alpha beta gamma delta epsilon\n1. one\n2. two", 16)
	want := []string{"• alpha beta", "  gamma delta", "  epsilon", "1. one", "2. two"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestMarkdownTableIsAligned(t *testing.T) {
	got := render("| Name | Qty |\n|:--|--:|\n| apple | 3 |\n| watermelon | 12 |", 40)
	want := []string{
		"Name       │ Qty",
		"───────────┼────",
		"apple      │   3",
		"watermelon │  12",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("table:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestMarkdownTableFallsBackToRecordsWhenNarrow(t *testing.T) {
	got := strings.Join(render("| Name | Qty | Price |\n|--|--|--|\n| apple | 3 | $1 |", 14), "\n")
	if !strings.Contains(got, "Name: apple") || strings.Contains(got, "┼") {
		t.Fatalf("expected stacked records, got:\n%s", got)
	}
}

func TestMarkdownCodeFenceHasGutterAndHighlight(t *testing.T) {
	lines := RenderMarkdown("```go\nfunc main() { return 42 } // done\n```", 80, "text")
	if len(lines) != 1 || !strings.HasPrefix(lines[0].Text(), "│ func main()") {
		t.Fatalf("fence rendering: %q", plain(lines))
	}
	styles := map[string]bool{}
	for _, s := range lines[0] {
		styles[s.Style] = true
	}
	for _, want := range []string{"markdown.code.keyword", "markdown.code.number", "markdown.code.comment"} {
		if !styles[want] {
			t.Errorf("no span styled %s in %v", want, lines[0])
		}
	}
}

// A document that is still arriving must draw correctly: an unclosed fence is code.
func TestMarkdownUnclosedFenceIsStillCode(t *testing.T) {
	got := render("text\n```\nlet x = 1", 40)
	if got[len(got)-1] != "│ let x = 1" {
		t.Fatalf("got %q", got)
	}
}

func TestMarkdownNeverOverflowsTheWidth(t *testing.T) {
	doc := "# H\n\n- a very long list item that must wrap " + strings.Repeat("word ", 30) +
		"\n\n| a | b |\n|--|--|\n| " + strings.Repeat("x", 50) + " | y |\n\n```\n" + strings.Repeat("z", 90) + "\n```\n> " + strings.Repeat("q ", 40)
	for _, w := range []int{80, 40, 20, 10, 5, 2} {
		for i, l := range RenderMarkdown(doc, w, "text") {
			if got := ansi.StringWidth(l.Text()); got > w {
				t.Errorf("width %d line %d is %d wide: %q", w, i, got, l.Text())
			}
		}
	}
}

func TestMarkdownProseWithPipeIsNotATable(t *testing.T) {
	got := render("use a | b to pipe", 40)
	if len(got) != 1 || got[0] != "use a | b to pipe" {
		t.Fatalf("got %q", got)
	}
}
