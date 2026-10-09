package engine

import (
	"fmt"

	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

func toolEv(seq int64, name, arg string, ok bool, summary string) fold.Event {
	return fold.Event{Type: "chat.tool", Seq: seq, Payload: map[string]any{
		"name": name, "arg": arg, "ok": ok, "summary": summary}}
}

// Tool calls draw a dot and the call, then the result under an elbow; calls of one
// answer stack with no blank row, and the answer follows after a gap.
func TestToolCallsDrawWithDotAndElbow(t *testing.T) {
	rows := plainRows(t, []fold.Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "what is here"}},
		toolEv(2, "list", ".", true, "Listed 2 entries"),
		toolEv(3, "read", "main.go", true, "Read 12 lines"),
		{Type: "llm.response", Seq: 4, Payload: map[string]any{"text": "A Go program."}},
	})
	want := []string{
		"┃ what is here",
		"",
		"● List(.)",
		"  └ Listed 2 entries",
		"● Read(main.go)",
		"  └ Read 12 lines",
		"",
		"  A Go program.",
	}
	if len(rows) < len(want) {
		t.Fatalf("too few rows:\n%s", strings.Join(rows, "\n"))
	}
	for i, w := range want {
		if rows[i] != w {
			t.Errorf("row %d = %q, want %q\nall rows:\n%s", i, rows[i], w, strings.Join(rows, "\n"))
		}
	}
}

func TestFailedToolCallIsStyledAsFailure(t *testing.T) {
	r, js := chatDoc(t)
	got := r.RenderFrame(mustDoc(t, js), fold.Fold([]fold.Event{
		toolEv(1, "read", ".env", false, "looks like it holds secrets"),
	})).Styled()
	for _, tok := range []string{"«chat.tool.dot.fail:", "«chat.tool:", "«chat.tool.fail:"} {
		if !strings.Contains(got, tok) {
			t.Errorf("styled frame lacks %s:\n%s", tok, got)
		}
	}
	if strings.Contains(got, "«chat.tool.result:└") {
		t.Errorf("a failure must not use the quiet result style:\n%s", got)
	}
}

func TestLongToolArgumentWrapsUnderItself(t *testing.T) {
	long := "internal/some/very/long/path/" + strings.Repeat("deep/", 10) + "file.go"
	rows := plainRows(t, []fold.Event{toolEv(1, "read", long, true, "Read 3 lines")})
	if len(rows) < 3 {
		t.Fatalf("expected a wrapped call:\n%s", strings.Join(rows, "\n"))
	}
	if !strings.HasPrefix(rows[0], "● Read(") {
		t.Errorf("first row = %q\n%s", rows[0], strings.Join(rows, "\n"))
	}
	if !strings.HasPrefix(rows[1], "  ") || strings.HasPrefix(rows[1], "●") {
		t.Errorf("continuation must hang under the call: %q", rows[1])
	}
	if !strings.Contains(strings.Join(rows, "\n"), "  └ Read 3 lines") {
		t.Errorf("result row missing:\n%s", strings.Join(rows, "\n"))
	}
}

const sampleDiff = "    1   package main\n    2   \n    3 -func main() {}\n    3 +func main() { println(1) }\n    4   // end"

func editEv(seq int64, diff string) fold.Event {
	return fold.Event{Type: "chat.tool", Seq: seq, Payload: map[string]any{
		"name": "edit", "arg": "main.go", "ok": true, "summary": "Added 1 line, removed 1 line", "diff": diff}}
}

// An edit shows its diff under the result, indented under the elbow.
func TestEditDrawsItsDiffUnderTheResult(t *testing.T) {
	rows := plainRows(t, []fold.Event{editEv(1, sampleDiff)})
	want := []string{
		"● Edit(main.go)",
		"  └ Added 1 line, removed 1 line",
		"        1   package main",
		"        2",
		"        3 -func main() {}",
		"        3 +func main() { println(1) }",
		"        4   // end",
	}
	if len(rows) < len(want) {
		t.Fatalf("too few rows:\n%s", strings.Join(rows, "\n"))
	}
	for i, w := range want {
		if strings.TrimRight(rows[i], " ") != strings.TrimRight(w, " ") {
			t.Errorf("row %d = %q, want %q", i, rows[i], w)
		}
	}
}

func TestDiffRowsAreColouredByWhatHappenedToThem(t *testing.T) {
	r, js := chatDoc(t)
	got := r.RenderFrame(mustDoc(t, js), fold.Fold([]fold.Event{editEv(1, sampleDiff)})).Styled()
	for _, want := range []string{"«chat.diff.del:", "«chat.diff.add:", "«chat.diff.ctx:"} {
		if !strings.Contains(got, want) {
			t.Errorf("styled frame lacks %s:\n%s", want, got)
		}
	}
	if strings.Contains(got, "«chat.diff.add:        3 -") || strings.Contains(got, "«chat.diff.del:        3 +") {
		t.Errorf("added and removed rows are mixed up:\n%s", got)
	}
}

// A long change is cut and counted by the rows it takes on screen, so a diff can never
// push the conversation away or spill past the frame.
func TestBigDiffIsCutAndCounted(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&b, "%5d + line %d %s\n", i, i, strings.Repeat("x", 300))
	}
	rows := plainRows(t, []fold.Event{editEv(1, b.String())})
	text := strings.Join(rows, "\n")
	if !strings.Contains(text, "more lines (ctrl+o to expand)") {
		t.Errorf("the hidden rows are not counted:\n%s", text)
	}
	if strings.Contains(text, "line 20 ") {
		t.Errorf("rows past the cap were drawn:\n%s", text)
	}
	for _, row := range rows {
		if w := ansi.StringWidth(row); w > 100 {
			t.Errorf("row is %d wide, past the frame: %q", w, row)
		}
	}
}

// A row wider than the frame continues on the rows below it, under the code and with no
// line number, instead of ending in "…": the text of a change is the point of showing it.
// This is the change a user could not read: one long line of JSON.
func TestALongDiffRowContinuesBelowWithoutALineNumber(t *testing.T) {
	long := `"border": "{type:round, color:input} /ui set prompt when agent.working /ui add node below_input {id:working_spacer, type:text, text:\" \", when:agent.working}"`
	diff := fmt.Sprintf("%5d   %s\n%5d + %s\n%5d   %s\n", 93, `"bind": "user.input",`, 94, long, 95, `"id": "prompt",`)
	rows := plainRows(t, []fold.Event{editEv(1, diff)})
	var added []string
	in := false
	for _, r := range rows {
		switch {
		case strings.Contains(r, "94 +"):
			in = true
			added = append(added, r)
		case in && strings.TrimSpace(r) != "" && !strings.Contains(r, "95"):
			added = append(added, r)
		default:
			in = false
		}
	}
	if len(added) < 2 {
		t.Fatalf("the long row should continue on a second row:\n%s", strings.Join(rows, "\n"))
	}
	for _, r := range added {
		if strings.Contains(r, "…") {
			t.Errorf("a row was cut with an ellipsis: %q", r)
		}
	}
	for _, r := range added[1:] {
		if strings.ContainsAny(strings.TrimSpace(r)[:1], "0123456789") && strings.Contains(r, " + ") {
			t.Errorf("a continuation row carries a line number: %q", r)
		}
	}
	// Nothing was lost: the pieces joined are the whole line.
	var joined strings.Builder
	for i, r := range added {
		if i == 0 {
			r = r[strings.Index(r, "+")+2:]
		}
		joined.WriteString(strings.TrimSpace(r))
	}
	if !strings.Contains(strings.ReplaceAll(joined.String(), " ", ""), strings.ReplaceAll(long, " ", "")) {
		t.Errorf("text was lost between the rows:\n got %q\nwant %q", joined.String(), long)
	}
}

func TestToolWithoutADiffDrawsNoDiff(t *testing.T) {
	rows := plainRows(t, []fold.Event{toolEv(1, "read", "main.go", true, "Read 3 lines")})
	for _, r := range rows {
		if strings.Contains(r, "more lines") {
			t.Errorf("unexpected row %q", r)
		}
	}
}

func approvalEv(seq int64) fold.Event {
	return fold.Event{Type: "chat.approval", Seq: seq, Payload: map[string]any{
		"name": "edit", "arg": "main.go", "summary": "Added 1 line, removed 1 line", "diff": sampleDiff}}
}

// A change waiting for the user shows the diff and the question; the answer takes
// the question away.
func TestAWaitingChangeShowsTheDiffAndTheQuestion(t *testing.T) {
	text := strings.Join(tallRows(t, []fold.Event{approvalEv(1)}), "\n")
	for _, want := range []string{"● Edit(main.go)", "3 -func main() {}", "3 +func main() { println(1) }", "Allow this change?  y yes · n no (Esc)"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, text)
		}
	}
	after := strings.Join(tallRows(t, []fold.Event{approvalEv(1),
		{Type: "chat.decided", Seq: 2, Payload: map[string]any{"allow": true}}, editEv(3, sampleDiff)}), "\n")
	if strings.Contains(after, "Allow this change?") || strings.Count(after, "● Edit(main.go)") != 1 {
		t.Errorf("the question must go once it is answered, leaving one Edit line:\n%s", after)
	}
}

func TestAWaitingChangeShowsMoreOfTheDiffThanADoneOne(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&b, "%5d + line %d\n", i, i)
	}
	ev := approvalEv(1)
	ev.Payload["diff"] = b.String()
	text := strings.Join(tallRows(t, []fold.Event{ev}), "\n")
	if !strings.Contains(text, "line 30") || strings.Contains(text, "more lines") {
		t.Errorf("the user must see the whole change before allowing it:\n%s", text)
	}
}

func TestADeclinedChangeLeavesNoQuestionBehind(t *testing.T) {
	text := strings.Join(tallRows(t, []fold.Event{approvalEv(1), {Type: "chat.cancelled", Seq: 2}}), "\n")
	if strings.Contains(text, "Allow this change?") {
		t.Errorf("a cancelled turn must not keep asking:\n%s", text)
	}
}

// tallRows is plainRows on a frame tall enough for a whole change.
func tallRows(t *testing.T, events []fold.Event) []string {
	t.Helper()
	r, js := chatDoc(t)
	r.Height = 80
	rows := strings.Split(strings.TrimRight(r.RenderFrame(mustDoc(t, js), fold.Fold(events)).Plain(), "\n"), "\n")
	for i := range rows {
		rows[i] = strings.TrimRight(rows[i], " ")
	}
	return rows
}

func TestACommandWaitingIsAskedAboutAsACommand(t *testing.T) {
	ev := fold.Event{Type: "chat.approval", Seq: 1, Payload: map[string]any{
		"name": "run", "arg": "go test ./...", "summary": "in /proj",
	}}
	text := strings.Join(tallRows(t, []fold.Event{ev}), "\n")
	for _, want := range []string{"Run(go test ./...)", "in /proj", "Allow this command?", "y yes"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q missing from:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Allow this change?") {
		t.Errorf("a command is not a change:\n%s", text)
	}
}

func TestACommandThatFailedIsDrawnAsAFailure(t *testing.T) {
	ev := fold.Event{Type: "chat.tool", Seq: 1, Payload: map[string]any{
		"name": "run", "arg": "go test ./...", "ok": false, "summary": "Exit 1 in 2.3s",
	}}
	text := strings.Join(tallRows(t, []fold.Event{ev}), "\n")
	if !strings.Contains(text, "Run(go test ./...)") || !strings.Contains(text, "└ Exit 1 in 2.3s") {
		t.Errorf("rows:\n%s", text)
	}
}

// ---- output that can be opened ------------------------------------------------

func runEv(seq int64, ok bool, summary, output string) fold.Event {
	return fold.Event{Type: "chat.tool", Seq: seq, Payload: map[string]any{
		"name": "run", "arg": "go test ./...", "ok": ok, "summary": summary, "output": output}}
}

func numbered(n int) string {
	var b strings.Builder
	b.WriteString("exit code 0\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "output %d\n", i)
	}
	return b.String()
}

// tallRowsExpanded is tallRows with the view Ctrl+O switches on.
func tallRowsExpanded(t *testing.T, events []fold.Event) string {
	t.Helper()
	r, js := chatDoc(t)
	r.Height = 120
	r.ExpandTools = true
	return r.RenderFrame(mustDoc(t, js), fold.Fold(events)).Plain()
}

func TestACommandsOutputShowsAGlimpseAndCountsTheRest(t *testing.T) {
	text := strings.Join(tallRows(t, []fold.Event{runEv(1, true, "Exit 0 in 2s", numbered(40))}), "\n")
	for _, want := range []string{"Run(go test ./...)", "└ Exit 0 in 2s", "output 1", "output 5", "… +35 lines (ctrl+o to expand)"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q missing:\n%s", want, text)
		}
	}
	if strings.Contains(text, "output 6") {
		t.Errorf("rows past the glimpse were drawn:\n%s", text)
	}
	if strings.Contains(text, "exit code 0") {
		t.Errorf("the exit code is already in the outcome line:\n%s", text)
	}
}

func TestExpandingShowsTheWholeOutputAndNoHint(t *testing.T) {
	text := tallRowsExpanded(t, []fold.Event{runEv(1, true, "Exit 0 in 2s", numbered(40))})
	if !strings.Contains(text, "output 40") || !strings.Contains(text, "output 6") {
		t.Errorf("expanded view is missing rows:\n%s", text)
	}
	if strings.Contains(text, "ctrl+o") {
		t.Errorf("an expanded view has nothing left to expand:\n%s", text)
	}
}

func TestAShortOutputIsShownWholeWithoutAHint(t *testing.T) {
	text := strings.Join(tallRows(t, []fold.Event{runEv(1, true, "Exit 0 in 0s", numbered(3))}), "\n")
	if !strings.Contains(text, "output 3") || strings.Contains(text, "ctrl+o") {
		t.Errorf("rows:\n%s", text)
	}
}

func TestACommandWithNoOutputDrawsOnlyItsOutcome(t *testing.T) {
	for _, out := range []string{"exit code 0\n(no output)", "exit code 0", ""} {
		rows := tallRows(t, []fold.Event{runEv(1, true, "Exit 0 in 0s", out)})
		text := strings.Join(rows, "\n")
		if strings.Contains(text, "(no output)") || strings.Contains(text, "ctrl+o") {
			t.Errorf("output %q drew:\n%s", out, text)
		}
	}
}

func TestOnlyACommandsOutputIsDrawn(t *testing.T) {
	// A read's text is the file the user already has; it is not repeated in the chat.
	ev := toolEv(1, "read", "main.go", true, "Read 3 lines")
	ev.Payload["output"] = "     1\tpackage main\n     2\t\n     3\tfunc main() {}\n"
	text := strings.Join(tallRows(t, []fold.Event{ev}), "\n")
	if strings.Contains(text, "package main") {
		t.Errorf("a read's content leaked into the conversation:\n%s", text)
	}
}

func TestExpandingAlsoOpensACutDiff(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&b, "%5d + line %d\n", i, i)
	}
	ev := editEv(1, b.String())
	if got := strings.Join(tallRows(t, []fold.Event{ev}), "\n"); !strings.Contains(got, "more lines (ctrl+o to expand)") {
		t.Errorf("a cut diff must say how to open it:\n%s", got)
	}
	if got := tallRowsExpanded(t, []fold.Event{ev}); !strings.Contains(got, "line 30") || strings.Contains(got, "more lines") {
		t.Errorf("expanded diff:\n%s", got)
	}
}

func TestCarriageReturnsAndTabsInOutputDoNotBreakTheRows(t *testing.T) {
	text := strings.Join(tallRows(t, []fold.Event{runEv(1, true, "Exit 0 in 0s", "exit code 0\nprogress 10%\rprogress 100%\nok\tpkg\t0.3s")}), "\n")
	if strings.ContainsAny(text, "\r\t") {
		t.Errorf("control characters reached the frame: %q", text)
	}
	if !strings.Contains(text, "ok    pkg    0.3s") {
		t.Errorf("tabs should become spaces:\n%s", text)
	}
}

// The dot is the outcome at a glance: white when the call worked, red when it
// failed, blue while it waits for an answer.
func TestToolDotFollowsTheOutcome(t *testing.T) {
	r, js := chatDoc(t)
	styled := func(evs ...fold.Event) string {
		return r.RenderFrame(mustDoc(t, js), fold.Fold(evs)).Styled()
	}
	if got := styled(toolEv(1, "read", "a.go", true, "Read 3 lines")); !strings.Contains(got, "«chat.tool.dot.ok:● ") {
		t.Errorf("a call that worked lacks the ok dot:\n%s", got)
	}
	if got := styled(toolEv(1, "read", ".env", false, "refused")); !strings.Contains(got, "«chat.tool.dot.fail:● ") {
		t.Errorf("a call that failed lacks the fail dot:\n%s", got)
	}
}
