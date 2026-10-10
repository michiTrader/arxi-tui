package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michiTrader/arxi/internal/chattools"
	"github.com/michiTrader/arxi/internal/provider"
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

func TestToolLoopCutsOffAModelThatNeverStops(t *testing.T) {
	f := setUpFake(t)
	for i := 0; i < maxToolRounds+4; i++ {
		f.script = append(f.script, scripted{name: "list", args: `{"path":"."}`})
	}
	ctx, got := collect(t, projectDir(t))
	if _, err := chatSendEffort(ctx, "loop", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if len(f.tools) != maxToolRounds+1 || len(*got) != maxToolRounds {
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
	hints := toolsHint("/p/project", editsAsk) + " " + runsHint(editsAsk) + " " + webHint(editsAsk)
	defs := append(append(chattools.Definitions(), chattools.EditDefinitions()...), chattools.RunDefinitions()...)
	raw, err := json.Marshal(defs)
	if err != nil {
		t.Fatal(err)
	}
	web, err := json.Marshal(append(webtools.Definitions(), webtools.SearchDefinition()))
	if err != nil {
		t.Fatal(err)
	}
	const maxHintBytes, maxDefBytes = 600, 1650 + 520
	raw = append(raw, web...)
	if len(hints) > maxHintBytes {
		t.Errorf("the hints are %d bytes, budget %d:\n%s", len(hints), maxHintBytes, hints)
	}
	if len(raw) > maxDefBytes {
		t.Errorf("the tool definitions are %d bytes, budget %d", len(raw), maxDefBytes)
	}
}

// A model lent tools answered "OK" to a request and did nothing, then answered "OK" to every
// line after it because its own earlier replies were the pattern it followed. The loop asks
// once more, and when it is still only an acknowledgment it says so instead of passing the
// word on as if it were an answer.
func TestABareOKFromAModelWithToolsIsAskedAgainAndThenSaidPlainly(t *testing.T) {
	f := setUpFake(t)
	f.script = []scripted{{text: "OK"}, {text: "Ok."}}
	ctx, _ := collect(t, projectDir(t))
	res, err := chatSendEffort(ctx, "add a border", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != bareAckGaveUp {
		t.Errorf("reply = %q; a bare acknowledgment given twice must be reported as doing nothing, not passed on as the answer", res.Text)
	}
	if len(f.messages) != 2 {
		t.Fatalf("the model was asked %d times, want 2 (the request and one more)", len(f.messages))
	}
	again := f.messages[1]
	if body, _ := again[len(again)-1]["content"].(string); body != bareAckNudge {
		t.Errorf("the second ask ends with %q, want the reminder", body)
	}
}

func TestAnAcknowledgmentThenARealAnswerIsKept(t *testing.T) {
	f := setUpFake(t)
	f.script = []scripted{{text: "OK"}, {text: "I added the border around the input bar."}}
	ctx, _ := collect(t, projectDir(t))
	res, err := chatSendEffort(ctx, "add a border", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "I added the border around the input bar." {
		t.Errorf("reply = %q; the answer after the reminder is the model's answer", res.Text)
	}
}

func TestAShortAnswerThatSaysSomethingIsNotAnAcknowledgment(t *testing.T) {
	for _, s := range []string{"OK, I read main.go: it is empty.", "Hecho: añadí la línea.", "No.", "42", ""} {
		if bareAck(s) {
			t.Errorf("%q is taken for a bare acknowledgment; the reminder would be sent to a model that answered", s)
		}
	}
	for _, s := range []string{"OK", "ok.", "  Okay! ", "Listo", "Entendido.", "Done", "de acuerdo"} {
		if !bareAck(s) {
			t.Errorf("%q is not taken for a bare acknowledgment", s)
		}
	}
}

func TestAnAcknowledgmentAfterToolsIsAFinalAnswerNotAFailure(t *testing.T) {
	f := setUpFake(t)
	f.script = []scripted{{name: "list", args: `{"path":"."}`}, {text: "Done"}}
	ctx, _ := collect(t, projectDir(t))
	res, err := chatSendEffort(ctx, "list the files", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Done" || len(f.messages) != 2 {
		t.Errorf("reply = %q after %d asks; a model that used its tools and then said Done answered", res.Text, len(f.messages))
	}
}

func TestAnAcknowledgmentTheUserAskedForIsTheAnswer(t *testing.T) {
	f := setUpFake(t)
	f.script = []scripted{{text: "OK"}}
	ctx, _ := collect(t, projectDir(t))
	res, err := chatSendEffort(ctx, "reply with just OK", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "OK" || len(f.messages) != 1 {
		t.Errorf("reply = %q after %d asks; a user who asked for OK was sent back to do more", res.Text, len(f.messages))
	}
}
