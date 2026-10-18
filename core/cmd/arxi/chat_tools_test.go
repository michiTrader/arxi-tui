package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/michiTrader/arxi/internal/chattools"
	"github.com/michiTrader/arxi/internal/provider"
	"github.com/michiTrader/arxi/internal/turn"
	"github.com/michiTrader/arxi/internal/webtools"
)

func projectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func collect(t *testing.T, dir string) (context.Context, *[]chatToolNotification) {
	t.Helper()
	var got []chatToolNotification
	ctx, err := withTools(context.Background(), dir, "", nil, func(n chatToolNotification) { got = append(got, n) })
	if err != nil {
		t.Fatal(err)
	}
	return ctx, &got
}

func TestToolLoopRunsToolsThenAnswersOnBothWires(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "plain"
		if stream {
			name = "streamed"
		}
		t.Run(name, func(t *testing.T) {
			f := setUpFake(t)
			f.script = []scripted{{name: "list", args: `{"path":"."}`}, {name: "read", args: `{"path":"main.go"}`}}
			ctx, got := collect(t, projectDir(t))
			if stream {
				ctx = provider.WithThinking(ctx, func(string) {})
			}
			res, err := chatSendEffort(ctx, "what is in here?", "", "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(res.Text, "echo:") {
				t.Errorf("the final answer should come after the tools: %q", res.Text)
			}
			if res.InputTokens != 23 || res.OutputTokens != 12 {
				t.Errorf("tokens must be summed over all asks: in=%d out=%d", res.InputTokens, res.OutputTokens)
			}
			if len(*got) != 2 || (*got)[0].Name != "list" || (*got)[1].Name != "read" || !(*got)[0].OK || !(*got)[1].OK {
				t.Fatalf("notifications = %+v", *got)
			}
			if (*got)[1].Arg != "main.go" || (*got)[1].CallID != "call_read" || !strings.Contains((*got)[1].Output, "func main()") {
				t.Errorf("read notification = %+v", (*got)[1])
			}
			for i, offered := range f.tools {
				if strings.Join(offered, ",") != "list,read,grep" {
					t.Errorf("ask %d offered tools %v", i, offered)
				}
			}
			last := f.messages[len(f.messages)-1]
			asst, tool := last[len(last)-2], last[len(last)-1]
			if asst["role"] != "assistant" || tool["role"] != "tool" || tool["tool_call_id"] != "call_read" {
				t.Errorf("the model must be shown its call and the result: %v / %v", asst["role"], tool)
			}
			sys, _ := last[0]["content"].(string)
			if !strings.Contains(sys, "You can use tools on the user's project") {
				t.Errorf("system prompt lacks the tools hint: %q", sys)
			}
		})
	}
}

func TestToolFailureIsReadByTheModelNotFatal(t *testing.T) {
	f := setUpFake(t)
	f.script = []scripted{{name: "read", args: `{"path":"../secret.txt"}`}}
	ctx, got := collect(t, projectDir(t))
	if _, err := chatSendEffort(ctx, "read it", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 1 || (*got)[0].OK || !strings.Contains((*got)[0].Summary, "outside the project") {
		t.Fatalf("notifications = %+v", *got)
	}
	last := f.messages[len(f.messages)-1]
	body, _ := last[len(last)-1]["content"].(string)
	if !strings.HasPrefix(body, "error: ") {
		t.Errorf("the model should read the failure as the result: %q", body)
	}
}

func TestToolLoopCutsOffAModelThatNeverStopsWhenALimitIsSet(t *testing.T) {
	const limit = 12
	t.Setenv(toolRoundsEnv, strconv.Itoa(limit))
	f := setUpFake(t)
	for i := 0; i < limit+4; i++ {
		f.script = append(f.script, scripted{name: "list", args: `{"path":"."}`})
	}
	ctx, got := collect(t, projectDir(t))
	if _, err := chatSendEffort(ctx, "loop", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if len(f.tools) != limit+1 || len(*got) != limit {
		t.Fatalf("asks = %d, tool calls = %d", len(f.tools), len(*got))
	}
	if len(f.tools[len(f.tools)-1]) != 0 {
		t.Errorf("the last ask must offer no tools: %v", f.tools[len(f.tools)-1])
	}
}

func TestTurnWithoutAFolderOffersNoTools(t *testing.T) {
	f := setUpFake(t)
	if _, err := chatSendEffort(context.Background(), "hi", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if len(f.tools) != 1 || len(f.tools[0]) != 0 {
		t.Errorf("tools offered without a folder: %v", f.tools)
	}
}

func TestAnInvalidFolderIsRefusedUpFront(t *testing.T) {
	f := setUpFake(t)
	if _, err := withTools(context.Background(), filepath.Join(t.TempDir(), "nope"), "", nil, nil); err == nil {
		t.Error("a folder that does not exist must be refused")
	}
	_, err := handleChatSend(map[string]any{"prompt": "hi", "workdir": t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "live connection") {
		t.Errorf("plain chat.send with workdir: err = %v", err)
	}
	if f.calls != 0 {
		t.Errorf("the provider was called %d times for a refused request", f.calls)
	}
}

// editRig runs one turn in which the model asks for the given tool calls, under an
// edits policy. asked collects what the user was shown.
type editRig struct {
	f     *fakeLLM
	dir   string
	got   []chatToolNotification
	asked []chatApprovalNotification
	res   chatResult
	err   error
}

func runEdits(t *testing.T, edits string, ask func(chatApprovalNotification) (bool, error), script ...scripted) *editRig {
	t.Helper()
	r := &editRig{f: setUpFake(t), dir: projectDir(t)}
	r.f.script = script
	wrapped := ask
	if ask != nil {
		wrapped = func(n chatApprovalNotification) (bool, error) {
			r.asked = append(r.asked, n)
			return ask(n)
		}
	}
	ctx, err := withTools(context.Background(), r.dir, edits, wrapped, func(n chatToolNotification) { r.got = append(r.got, n) })
	if err != nil {
		t.Fatal(err)
	}
	r.res, r.err = chatSendEffort(ctx, "change it", "", "", "", "")
	return r
}

func (r *editRig) main(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const editMain = `{"path":"main.go","old_string":"func main() {}","new_string":"func main() { println(1) }"}`

func TestDenyOffersOnlyTheReadingTools(t *testing.T) {
	r := runEdits(t, "", nil, scripted{name: "edit", args: editMain})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if got := strings.Join(r.f.tools[0], ","); got != "list,read,grep" {
		t.Errorf("offered tools = %q", got)
	}
	if len(r.got) != 1 || r.got[0].OK || !strings.Contains(r.got[0].Summary, "not available") {
		t.Errorf("a refused edit must be reported as a failure: %+v", r.got)
	}
	if strings.Contains(r.main(t), "println") {
		t.Error("deny must never change a file")
	}
}

func TestAllowEditsWithoutAsking(t *testing.T) {
	r := runEdits(t, editsAllow, nil, scripted{name: "edit", args: editMain})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if got := strings.Join(r.f.tools[0], ","); got != "list,read,grep,edit,write" {
		t.Errorf("offered tools = %q", got)
	}
	if !strings.Contains(r.main(t), "println(1)") {
		t.Error("the file should have been edited")
	}
	if len(r.got) != 1 || !r.got[0].OK || r.got[0].Diff == "" || r.got[0].Arg != "main.go" {
		t.Errorf("notification = %+v", r.got)
	}
}

func TestAskPutsTheDiffToTheUserAndEditsWhenAllowed(t *testing.T) {
	r := runEdits(t, editsAsk, func(chatApprovalNotification) (bool, error) { return true, nil },
		scripted{name: "edit", args: editMain})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if len(r.asked) != 1 || r.asked[0].Name != "edit" || r.asked[0].Arg != "main.go" ||
		!strings.Contains(r.asked[0].Diff, "+ func main() { println(1) }") || r.asked[0].CallID != "call_edit" {
		t.Fatalf("the user was shown %+v", r.asked)
	}
	if !strings.Contains(r.main(t), "println(1)") {
		t.Error("an allowed change must be made")
	}
	if len(r.got) != 1 || !r.got[0].OK {
		t.Errorf("notification = %+v", r.got)
	}
}

func TestAskLeavesTheFileAloneWhenDeclined(t *testing.T) {
	r := runEdits(t, editsAsk, func(chatApprovalNotification) (bool, error) { return false, nil },
		scripted{name: "edit", args: editMain})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if strings.Contains(r.main(t), "println") {
		t.Fatal("a declined change must not be made")
	}
	if len(r.got) != 1 || r.got[0].OK || !strings.Contains(r.got[0].Summary, "did not allow") {
		t.Errorf("notification = %+v", r.got)
	}
	// The model must be told, so it does not retry blindly.
	last := r.f.messages[len(r.f.messages)-1]
	if body, _ := last[len(last)-1]["content"].(string); !strings.Contains(body, "the user did not allow") {
		t.Errorf("the model was not told about the refusal: %v", last[len(last)-1])
	}
}

func TestAskWithoutAWayToAskRefuses(t *testing.T) {
	r := runEdits(t, editsAsk, nil, scripted{name: "write", args: `{"path":"new.txt","content":"x\n"}`})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "new.txt")); err == nil {
		t.Error("nothing may be written when the user cannot be asked")
	}
	if len(r.got) != 1 || r.got[0].OK || !strings.Contains(r.got[0].Summary, "approval") {
		t.Errorf("notification = %+v", r.got)
	}
}

func TestLosingTheUserEndsTheTurn(t *testing.T) {
	r := runEdits(t, editsAsk, func(chatApprovalNotification) (bool, error) { return false, errors.New("connection closed") },
		scripted{name: "edit", args: editMain})
	if r.err == nil || !strings.Contains(r.err.Error(), "connection closed") {
		t.Fatalf("err = %v", r.err)
	}
	if strings.Contains(r.main(t), "println") {
		t.Error("no change may be made when the user is gone")
	}
}

func TestAChangeThatChangesNothingIsNotAsked(t *testing.T) {
	r := runEdits(t, editsAsk, func(chatApprovalNotification) (bool, error) { return false, nil },
		scripted{name: "write", args: `{"path":"main.go","content":"package main\n\nfunc main() {}\n"}`})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if len(r.asked) != 0 {
		t.Errorf("asked about a no-op: %+v", r.asked)
	}
}

func TestAnInvalidEditsPolicyIsRefused(t *testing.T) {
	if _, err := withTools(context.Background(), t.TempDir(), "yolo", nil, nil); err == nil {
		t.Error("edits must be deny, ask or allow")
	}
}

func TestTheHintMentionsEditingOnlyWhenItIsOffered(t *testing.T) {
	if strings.Contains(toolsHint("/p/x", editsDeny), "edit") {
		t.Error("the deny hint must not mention editing")
	}
	if !strings.Contains(toolsHint("/p/x", editsAsk), "edit") {
		t.Error("the ask hint must mention editing")
	}
}

// What every request carries before the user has said a word: the hints plus the
// tool schemas. It is a cost on every turn of every session, so it has a budget; a
// change that grows it past that has to say so here, on purpose.
func TestStandingPromptStaysWithinItsBudget(t *testing.T) {
	// The hint names the user's home folder; a real one can be any length, so the test fixes it.
	t.Setenv("HOME", "/h")
	t.Setenv("USERPROFILE", "/h")
	hints := toolsHint("/p/project", editsAsk) + " " + envHint("/p/project", editsAsk) + " " + runsHint(editsAsk) + " " + webHint(editsAsk)
	defs := append(append(chattools.Definitions(), chattools.EditDefinitions()...), chattools.RunDefinitions()...)
	raw, err := json.Marshal(defs)
	if err != nil {
		t.Fatal(err)
	}
	web, err := json.Marshal(append(webtools.Definitions(), webtools.SearchDefinition()))
	if err != nil {
		t.Fatal(err)
	}
	const maxDefBytes = 1650 + 520
	// 1160 bytes on Linux. Windows adds one sentence of its own (the cmd.exe commands) and
	// longer names (windows/amd64, cmd.exe, a drive letter), which is measured here rather
	// than hidden in a bigger number: the budget for everything else is the same.
	// 1100: the four sentences added after sessions in which a model asked permission
	// to list a folder in full access mode, answered "I am running it" without calling
	// run, took one wrong path for an empty project, and read "open it in the browser" as
	// "the file is already open" and asked "say yes and I will run it" (about 90 tokens a turn).
	maxHintBytes := 1160
	if runtime.GOOS == "windows" {
		maxHintBytes += len(windowsShellHint) + 20
	}
	raw = append(raw, web...)
	if len(hints) > maxHintBytes {
		t.Errorf("the hints are %d bytes, budget %d:\n%s", len(hints), maxHintBytes, hints)
	}
	if len(raw) > maxDefBytes {
		t.Errorf("the tool definitions are %d bytes, budget %d", len(raw), maxDefBytes)
	}
}

// The model is told where it is running. Counterfactual: without it a Windows user's
// "rename the file on my Desktop" was answered with ls, $HOME and "that is outside my
// scope", over a dozen calls.
func TestTheModelIsToldTheSystemTheProjectAndTheShell(t *testing.T) {
	h := envHint(t.TempDir(), editsAsk)
	for _, want := range []string{runtime.GOOS, "Project:", chattools.ShellName(), "any path"} {
		if !strings.Contains(h, want) {
			t.Errorf("the environment hint lacks %q: %s", want, h)
		}
	}
	if strings.Contains(envHint(t.TempDir(), editsDeny), "run uses") {
		t.Error("no run tool, no word about its shell")
	}
	if runtime.GOOS == "windows" && !strings.Contains(h, "dir") {
		t.Error("on Windows the model must be told the commands are cmd.exe's")
	}
}

// vyceai/deepseek-v4.1 answered once with a tool call that had no name and the whole
// turn died with "tool name is required", so the user had to type "continua". It is a
// broken reply, not an answer: the call is asked again and the turn goes on.
func TestANamelessToolCallIsAskedAgainAndTheTurnGoesOn(t *testing.T) {
	f := setUpFake(t)
	f.script = []scripted{{name: namelessCall}, {name: "list", args: `{"path":"."}`}, {text: "all done"}}
	ctx, got := collect(t, projectDir(t))
	res, err := chatSendEffort(ctx, "go", "", "", "", "")
	if err != nil {
		t.Fatalf("a nameless call must not end the turn: %v", err)
	}
	if res.Text != "all done" || len(*got) != 1 || f.calls != 3 {
		t.Errorf("text %q, tool calls %d, asks %d; want the answer after one retry and one real call", res.Text, len(*got), f.calls)
	}
}

// A turn cut at the round cap used to end like any other: no sign that the work was
// unfinished, so it looked like the model "stopped suddenly".
func TestACutTurnSaysItWasCutAndAFinishedOneDoesNot(t *testing.T) {
	t.Setenv(toolRoundsEnv, "40")
	f := setUpFake(t)
	for i := 0; i < 44; i++ {
		f.script = append(f.script, scripted{name: "list", args: `{"path":"."}`})
	}
	ctx, _ := collect(t, projectDir(t))
	res, err := chatSendEffort(ctx, "loop", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "limit of 40 tool rounds") || !strings.Contains(res.Text, "continue") {
		t.Errorf("a cut turn does not say so: %q", res.Text)
	}

	f2 := setUpFake(t)
	f2.script = []scripted{{name: "list", args: `{"path":"."}`}, {text: "done"}}
	ctx2, _ := collect(t, projectDir(t))
	res2, err := chatSendEffort(ctx2, "ok", "", "", "", "")
	if err != nil || strings.Contains(res2.Text, "tool rounds") {
		t.Errorf("a finished turn must not carry the note: %q %v", res2.Text, err)
	}
}

func TestToolRoundsDefaultAndEnv(t *testing.T) {
	if n, err := toolRounds(""); err != nil || n != defaultToolRounds {
		t.Fatalf("default = %d, %v", n, err)
	}
	if n, err := toolRounds(" 7 "); err != nil || n != 7 {
		t.Fatalf("7 = %d, %v", n, err)
	}
	for _, bad := range []string{"-3", "abc", "2.5", "1001", "99999"} {
		if _, err := toolRounds(bad); err == nil {
			t.Errorf("%q was accepted; a limit nobody chose must be refused", bad)
		}
	}
}

// The environment variable really moves the cut-off of a turn, and the note says how.
func TestToolRoundsEnvMovesTheCut(t *testing.T) {
	t.Setenv(toolRoundsEnv, "3")
	f := setUpFake(t)
	for i := 0; i < 8; i++ {
		f.script = append(f.script, scripted{name: "list", args: `{"path":"."}`})
	}
	ctx, got := collect(t, projectDir(t))
	res, err := chatSendEffort(ctx, "loop", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(*got) != 3 || len(f.tools) != 4 {
		t.Fatalf("tool calls = %d, asks = %d; want 3 and 4", len(*got), len(f.tools))
	}
	if !strings.Contains(res.Text, "limit of 3 tool rounds") || !strings.Contains(res.Text, toolRoundsEnv+" raises the limit") {
		t.Errorf("the note does not say the limit or how to change it: %q", res.Text)
	}
}

func TestABadToolRoundsValueFailsTheTurnInsteadOfGuessing(t *testing.T) {
	t.Setenv(toolRoundsEnv, "lots")
	f := setUpFake(t)
	f.script = append(f.script, scripted{name: "list", args: `{"path":"."}`})
	ctx, _ := collect(t, projectDir(t))
	if _, err := chatSendEffort(ctx, "loop", "", "", "", ""); err == nil || !strings.Contains(err.Error(), toolRoundsEnv) {
		t.Fatalf("err = %v, want one naming %s", err, toolRoundsEnv)
	}
}

// Every ask of a turn is reported as it finishes, so a client can show how full the
// context is DURING a long tool loop, and keeps the last figure when the turn later
// fails (a timeout at round 20 used to leave the bar at the size of round 0).
func TestEveryAskOfATurnReportsTheContextItHeld(t *testing.T) {
	f := setUpFake(t)
	f.script = []scripted{{name: "list", args: `{"path":"."}`}, {name: "list", args: `{"path":"."}`}, {text: "done"}}
	ctx, _ := collect(t, projectDir(t))
	var notes []chatUsageNotification
	ctx = withUsageNotice(ctx, func(n chatUsageNotification) { notes = append(notes, n) })
	res, err := chatSendEffort(ctx, "go", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 3 {
		t.Fatalf("%d usage notices, want one per ask (3)", len(notes))
	}
	for i, n := range notes {
		if n.Type != "chat.usage" || n.ContextTokens != 15 || n.InputTokens != 10 || n.OutputTokens != 5 {
			t.Errorf("notice %d = %+v, want chat.usage with 15 held (10 in + 5 out)", i, n)
		}
	}
	if res.ContextTokens != 15 {
		t.Errorf("final context = %d", res.ContextTokens)
	}
}

// A turn that fails after some asks has still told the client what the model held.
func TestAFailedTurnStillReportedTheAsksThatDidAnswer(t *testing.T) {
	f := setUpFake(t)
	f.script = []scripted{{name: "list", args: `{"path":"."}`}}
	f.failAfter = 1 // the second ask is refused for good
	ctx, _ := collect(t, projectDir(t))
	var notes []chatUsageNotification
	ctx = withUsageNotice(ctx, func(n chatUsageNotification) { notes = append(notes, n) })
	if _, err := chatSendEffort(ctx, "go", "", "", "", ""); err == nil {
		t.Fatal("the second ask is refused, the turn must fail")
	}
	if len(notes) != 1 || notes[0].ContextTokens != 15 {
		t.Errorf("notices = %+v, want the one ask that answered", notes)
	}
}

// Three failures seen in one user's sessions, each answered by one sentence the model
// is given: asking leave to look, announcing a run it never made, and calling the project
// empty because a guessed path was not there.
func TestTheHintsStopTheModelAskingLeaveAnnouncingRunsAndDeclaringTheProjectEmpty(t *testing.T) {
	h := toolsHint("/p/x", editsAsk)
	for _, want := range []string{"Never ask leave to look or run", `list "." before saying the project is empty`} {
		if !strings.Contains(h, want) {
			t.Errorf("the tools hint lost %q: %q", want, h)
		}
	}
	if r := runsHint(editsAsk); !strings.Contains(r, "without calling run in that reply") {
		t.Errorf("the run hint does not forbid announcing a run that was not made: %q", r)
	}
}

// "Open it in the browser" was read as "show me the file", and the model answered that it
// was already open. It has to be told what the user means and what to run, per system.
func TestTheRunHintSaysOpenInTheBrowserMeansTheUsersOwnBrowser(t *testing.T) {
	h := runsHint(editsAsk)
	if !strings.Contains(h, "own: run ") || !strings.Contains(h, openerHint()) || !strings.Contains(h, "serve a page first") {
		t.Errorf("the run hint does not say how to open the user's browser: %q", h)
	}
	for goos, want := range map[string]string{"windows": `start ""`, "darwin": "open ", "linux": "xdg-open"} {
		if goos == runtime.GOOS && !strings.Contains(openerHint(), want) {
			t.Errorf("on %s the opener should be %q, got %q", goos, want, openerHint())
		}
	}
}

// deepseek-v4.1 sent a tool call whose arguments stopped in the middle and the turn died
// with "decode tool arguments: unexpected EOF". A stream that ended early is asked again
// as it was, and the turn goes on.
func TestAToolCallCutByAnEarlyEndIsAskedAgainAndTheTurnGoesOn(t *testing.T) {
	f := setUpFake(t)
	f.script = []scripted{{name: cutCall}, {name: "list", args: `{"path":"."}`}, {text: "all done"}}
	ctx, got := collect(t, projectDir(t))
	res, err := chatSendEffort(ctx, "go", "", "", "", "")
	if err != nil {
		t.Fatalf("a cut call must not end the turn: %v", err)
	}
	if res.Text != "all done" || len(*got) != 1 || f.calls != 3 {
		t.Errorf("text %q, tool calls %d, asks %d; want the answer after one retry and one real call", res.Text, len(*got), f.calls)
	}
}

// Cut at the output limit: not asked again as it was (it would be cut at the same place);
// the model is told to work in smaller steps, and the turn goes on. Nothing half-written ran.
func TestAToolCallCutByTheOutputLimitMakesTheModelWorkInSmallerSteps(t *testing.T) {
	f := setUpFake(t)
	f.script = []scripted{{name: cutCallLength}, {name: "list", args: `{"path":"."}`}, {text: "all done"}}
	ctx, got := collect(t, projectDir(t))
	res, err := chatSendEffort(ctx, "go", "", "", "", "")
	if err != nil {
		t.Fatalf("a call cut at the limit must not end the turn: %v", err)
	}
	if res.Text != "all done" || len(*got) != 1 || f.calls != 3 {
		t.Errorf("text %q, tool calls %d, asks %d", res.Text, len(*got), f.calls)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	last := f.messages[1]
	if c, _ := last[len(last)-1]["content"].(string); !strings.Contains(c, "smaller steps") {
		t.Errorf("the second ask did not tell the model to work in smaller steps: %v", last[len(last)-1])
	}
}

// A model that is cut every time does not loop for ever: after maxCutAsks the turn ends
// with the plain sentence, not "unexpected EOF".
func TestACallCutAtTheLimitEveryTimeEndsWithAPlainSentence(t *testing.T) {
	f := setUpFake(t)
	for i := 0; i < maxCutAsks+3; i++ {
		f.script = append(f.script, scripted{name: cutCallLength})
	}
	ctx, got := collect(t, projectDir(t))
	_, err := chatSendEffort(ctx, "go", "", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "output allowance") || strings.Contains(err.Error(), "EOF") {
		t.Fatalf("want the plain output-limit sentence, got %v", err)
	}
	if f.calls != 1+maxCutAsks || len(*got) != 0 {
		t.Errorf("asked %d times (want %d), ran %d tools (want 0)", f.calls, 1+maxCutAsks, len(*got))
	}
}

// There is no round limit unless one is asked for: a long task is not cut in the middle.
func TestThereIsNoRoundLimitByDefault(t *testing.T) {
	if defaultToolRounds != 0 {
		t.Fatalf("defaultToolRounds = %d, want 0 (no limit)", defaultToolRounds)
	}
	const many = 60 // more than the old limit of 40
	f := setUpFake(t)
	for i := 0; i < many; i++ {
		f.script = append(f.script, scripted{name: "list", args: `{"path":"."}`})
	}
	f.script = append(f.script, scripted{text: "all done"})
	ctx, got := collect(t, projectDir(t))
	res, err := chatSendEffort(ctx, "loop", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(*got) != many || res.Text != "all done" {
		t.Fatalf("tool calls = %d, text = %q; want %d calls and the model's own answer", len(*got), res.Text, many)
	}
	for i, offered := range f.tools {
		if len(offered) == 0 {
			t.Fatalf("ask %d offered no tools: the loop was cut", i)
		}
	}
	if strings.Contains(res.Text, "tool rounds") {
		t.Errorf("an unlimited turn must not carry the cut note: %q", res.Text)
	}
}

// The words that mean "no limit" are accepted; so is an explicit number.
func TestTheWordsForNoRoundLimitAreAccepted(t *testing.T) {
	for _, w := range []string{"", "0", "off", "OFF", " unlimited ", "none"} {
		if n, err := toolRounds(w); err != nil || n != 0 {
			t.Errorf("%q = %d, %v; want 0 and no error", w, n, err)
		}
	}
	if n, err := toolRounds("1000"); err != nil || n != 1000 {
		t.Errorf("1000 = %d, %v", n, err)
	}
}

func readResultsOf(f *fakeLLM, ask int) []string {
	var out []string
	for _, m := range f.messages[ask] {
		if m["role"] == "tool" {
			s, _ := m["content"].(string)
			out = append(out, s)
		}
	}
	return out
}

// Reading the same file again, with the file unchanged, costs the model the whole file
// again in every later ask. The second copy is replaced by a short note.
func TestAnIdenticalRepeatedReadIsNotSentTwice(t *testing.T) {
	dir := projectDir(t)
	body := strings.Repeat("line of a long file\n", 60)
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	f := setUpFake(t)
	f.script = []scripted{
		{name: "read", args: `{"path":"big.txt"}`},
		{name: "read", args: `{"path":"big.txt"}`},
		{text: "ok"},
	}
	ctx, _ := collect(t, dir)
	if _, err := chatSendEffort(ctx, "go", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	got := readResultsOf(f, 2)
	if len(got) != 2 {
		t.Fatalf("tool results in the last ask = %d, want 2: %v", len(got), got)
	}
	if !strings.Contains(got[0], "line of a long file") {
		t.Errorf("the first read must be whole: %q", got[0])
	}
	if got[1] != repeatNote {
		t.Errorf("the identical second read must be the note, got %q", got[1])
	}
}

// A file that changed between two reads is sent whole the second time: the model is
// never told "unchanged" about something that changed.
func TestACollapsedReadNeverHidesAFileThatChanged(t *testing.T) {
	dir := projectDir(t)
	path := filepath.Join(dir, "big.txt")
	one := strings.Repeat("old line of a long file\n", 60)
	two := strings.Repeat("new line of a long file\n", 60)
	if err := os.WriteFile(path, []byte(one), 0o644); err != nil {
		t.Fatal(err)
	}
	f := setUpFake(t)
	f.script = []scripted{
		{name: "read", args: `{"path":"big.txt"}`},
		{name: "write", args: `{"path":"big.txt","content":` + strconv.Quote(two) + `}`},
		{name: "read", args: `{"path":"big.txt"}`},
		{text: "ok"},
	}
	ctx, err := withTools(context.Background(), dir, editsAllow, nil, func(chatToolNotification) {})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chatSendEffort(ctx, "go", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != two {
		t.Fatalf("the write did not happen, so this test proves nothing: %q", b[:30])
	}
	got := readResultsOf(f, 3)
	last := got[len(got)-1]
	if last == repeatNote || !strings.Contains(last, "new line of a long file") {
		t.Errorf("the changed file must be sent whole, got %q", last)
	}
}

func TestOnlyBigIdenticalReadsListsAndSearchesAreCollapsed(t *testing.T) {
	big := strings.Repeat("x", minRepeatBytes+10)
	mk := func(name, text string, isErr bool) (*turn.ToolCall, *turn.ToolResult) {
		return &turn.ToolCall{Name: name, Arguments: json.RawMessage(`{"a":1}`)},
			&turn.ToolResult{IsError: isErr, Content: []turn.ContentBlock{{Type: turn.BlockText, Text: text}}}
	}
	cases := []struct {
		name, text string
		isErr      bool
		collapsed  bool
	}{
		{"read", big, false, true},
		{"list", big, false, true},
		{"grep", big, false, true},
		{"read", big, true, false},                      // an error is not "unchanged"
		{"read", strings.Repeat("x", 20), false, false}, // too short to be worth a note
		{"run", big, false, false},                      // a command can give another result each time
		{"write", big, false, false},
	}
	for _, c := range cases {
		seen := map[string]string{}
		call, first := mk(c.name, c.text, c.isErr)
		collapseRepeat(seen, call, first)
		call2, second := mk(c.name, c.text, c.isErr)
		collapseRepeat(seen, call2, second)
		if got := second.Content[0].Text == repeatNote; got != c.collapsed {
			t.Errorf("%s err=%v len=%d: collapsed = %v, want %v", c.name, c.isErr, len(c.text), got, c.collapsed)
		}
		if first.Content[0].Text == repeatNote {
			t.Errorf("%s: the first result must never be collapsed", c.name)
		}
	}
}

// "The same call" means the same tool on the same path: two different files that happen
// to hold the same text are each sent whole, and a file read, then another, then the
// first again is still recognised as repeated.
func TestARepeatIsJudgedPerCallNotPerTool(t *testing.T) {
	dir := projectDir(t)
	body := strings.Repeat("shared text of a long file\n", 40)
	for _, n := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f := setUpFake(t)
	f.script = []scripted{
		{name: "read", args: `{"path":"a.txt"}`},
		{name: "read", args: `{"path":"b.txt"}`},
		{name: "read", args: `{"path":"a.txt"}`},
		{text: "ok"},
	}
	ctx, _ := collect(t, dir)
	if _, err := chatSendEffort(ctx, "go", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	got := readResultsOf(f, 3)
	if len(got) != 3 {
		t.Fatalf("results = %d", len(got))
	}
	if got[0] == repeatNote || got[1] == repeatNote {
		t.Errorf("a first read of a path must be whole: %q / %q", got[0], got[1])
	}
	if got[2] != repeatNote {
		t.Errorf("a.txt read again must be the note, got %q", got[2])
	}
}

// The app keeps what the model read to spare it reading again, and it cannot keep a file
// it only got half of: a file of ordinary size reaches it whole, a huge one says it was cut.
func TestAToolResultOfOrdinarySizeReachesTheClientWhole(t *testing.T) {
	dir := projectDir(t)
	body := strings.Repeat("x := 1 // a line of code\n", 800) // about 20 KB
	if err := os.WriteFile(filepath.Join(dir, "big.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	huge := strings.Repeat(strings.Repeat("y", 99)+"\n", maxToolOutputEvent/100+60)
	if err := os.WriteFile(filepath.Join(dir, "huge.txt"), []byte(huge), 0o644); err != nil {
		t.Fatal(err)
	}
	f := setUpFake(t)
	f.script = []scripted{{name: "read", args: `{"path":"big.go"}`}, {name: "read", args: `{"path":"huge.txt"}`}, {text: "ok"}}
	ctx, got := collect(t, dir)
	if _, err := chatSendEffort(ctx, "go", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 2 {
		t.Fatalf("notifications = %d", len(*got))
	}
	if !strings.Contains((*got)[0].Output, "x := 1") || strings.HasSuffix((*got)[0].Output, "[output cut]") || len((*got)[0].Output) < 20000 {
		t.Errorf("a 20 KB file must reach the client whole, got %d bytes", len((*got)[0].Output))
	}
	if !strings.HasSuffix((*got)[1].Output, "[output cut]") {
		t.Errorf("a result over the event cap must say it was cut; got %d bytes ending %q", len((*got)[1].Output), (*got)[1].Output[max(0, len((*got)[1].Output)-60):])
	}
}
