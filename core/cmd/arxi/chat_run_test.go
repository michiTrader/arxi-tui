package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runRig is runEdits for the run tool: files may not change here, commands are the
// policy under test.
func runRig(t *testing.T, runs string, ask func(chatApprovalNotification) (bool, error), script ...scripted) *editRig {
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
	ctx, err := withTools(context.Background(), r.dir, "", wrapped, func(n chatToolNotification) { r.got = append(r.got, n) })
	if err != nil {
		t.Fatal(err)
	}
	if ctx, err = withRuns(ctx, runs); err != nil {
		t.Fatal(err)
	}
	r.res, r.err = chatSendEffort(ctx, "build it", "", "", "", "")
	return r
}

const echoCall = `{"command":"echo built"}`

func TestWithoutRunsTheToolIsNotOfferedAndRefusedIfCalled(t *testing.T) {
	for _, runs := range []string{"", editsDeny} {
		r := runRig(t, runs, nil, scripted{name: "run", args: echoCall})
		if r.err != nil {
			t.Fatal(r.err)
		}
		if got := strings.Join(r.f.tools[0], ","); got != "list,read,grep" {
			t.Errorf("runs=%q offered tools = %q", runs, got)
		}
		if len(r.got) != 1 || r.got[0].OK || !strings.Contains(r.got[0].Summary, "may not run commands") {
			t.Errorf("runs=%q notification = %+v", runs, r.got)
		}
	}
}

func TestAllowRunsTheCommandWithoutAsking(t *testing.T) {
	r := runRig(t, editsAllow, nil, scripted{name: "run", args: echoCall})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if got := strings.Join(r.f.tools[0], ","); got != "list,read,grep,run" {
		t.Errorf("offered tools = %q", got)
	}
	if len(r.got) != 1 || !r.got[0].OK || r.got[0].Arg != "echo built" || !strings.Contains(r.got[0].Output, "built") {
		t.Fatalf("notification = %+v", r.got)
	}
	if !strings.HasPrefix(r.got[0].Summary, "Exit 0") {
		t.Errorf("summary = %q", r.got[0].Summary)
	}
}

func TestAskPutsTheCommandToTheUserAndRunsItWhenAllowed(t *testing.T) {
	r := runRig(t, editsAsk, func(chatApprovalNotification) (bool, error) { return true, nil },
		scripted{name: "run", args: echoCall})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if len(r.asked) != 1 || r.asked[0].Name != "run" || r.asked[0].Arg != "echo built" ||
		r.asked[0].Diff != "" || !strings.Contains(r.asked[0].Summary, "Run in") {
		t.Fatalf("the user was shown %+v", r.asked)
	}
	if len(r.got) != 1 || !r.got[0].OK {
		t.Errorf("notification = %+v", r.got)
	}
}

func TestAskDoesNotRunACommandTheUserDeclined(t *testing.T) {
	marker := "made.txt"
	r := runRig(t, editsAsk, func(chatApprovalNotification) (bool, error) { return false, nil },
		scripted{name: "run", args: `{"command":"echo x > ` + marker + `"}`})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if _, err := os.Stat(filepath.Join(r.dir, marker)); err == nil {
		t.Fatal("a declined command was run")
	}
	if len(r.got) != 1 || r.got[0].OK || !strings.Contains(r.got[0].Summary, "did not allow") {
		t.Errorf("notification = %+v", r.got)
	}
	last := r.f.messages[len(r.f.messages)-1]
	if body, _ := last[len(last)-1]["content"].(string); !strings.Contains(body, "the user did not allow") {
		t.Errorf("the model was not told about the refusal: %v", last[len(last)-1])
	}
}

func TestACommandIsAskedAboutEvenWhenItWouldChangeNothing(t *testing.T) {
	r := runRig(t, editsAsk, func(chatApprovalNotification) (bool, error) { return false, nil },
		scripted{name: "run", args: `{"command":"true"}`})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if len(r.asked) != 1 {
		t.Errorf("every command is put to the user under ask, asked %d times", len(r.asked))
	}
}

func TestAskWithoutAWayToAskDoesNotRun(t *testing.T) {
	r := runRig(t, editsAsk, nil, scripted{name: "run", args: `{"command":"echo x > made.txt"}`})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "made.txt")); err == nil {
		t.Error("nothing may run when the user cannot be asked")
	}
	if len(r.got) != 1 || r.got[0].OK || !strings.Contains(r.got[0].Summary, "approval") {
		t.Errorf("notification = %+v", r.got)
	}
}

func TestLosingTheUserEndsTheTurnBeforeTheCommandRuns(t *testing.T) {
	r := runRig(t, editsAsk, func(chatApprovalNotification) (bool, error) { return false, errors.New("connection closed") },
		scripted{name: "run", args: `{"command":"echo x > made.txt"}`})
	if r.err == nil || !strings.Contains(r.err.Error(), "connection closed") {
		t.Fatalf("err = %v", r.err)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "made.txt")); err == nil {
		t.Error("the command ran although the user was gone")
	}
}

func TestAFailingCommandIsShownAsAFailureAndReadByTheModel(t *testing.T) {
	r := runRig(t, editsAllow, nil, scripted{name: "run", args: `{"command":"exit 4"}`})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if len(r.got) != 1 || r.got[0].OK || !strings.HasPrefix(r.got[0].Summary, "Exit 4") {
		t.Fatalf("notification = %+v", r.got)
	}
	last := r.f.messages[len(r.f.messages)-1]
	if body, _ := last[len(last)-1]["content"].(string); !strings.Contains(body, "exit code 4") {
		t.Errorf("the model must read the exit code: %v", last[len(last)-1])
	}
}

func TestRunsAreIndependentOfEdits(t *testing.T) {
	// Commands allowed, file changes denied: the model can build but not write.
	r := runRig(t, editsAllow, nil,
		scripted{name: "write", args: `{"path":"x.txt","content":"x\n"}`})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if len(r.got) != 1 || r.got[0].OK || !strings.Contains(r.got[0].Summary, "not available") {
		t.Errorf("notification = %+v", r.got)
	}
}

func TestAnInvalidRunsPolicyIsRefused(t *testing.T) {
	ctx, err := withTools(context.Background(), t.TempDir(), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := withRuns(ctx, "yolo"); err == nil {
		t.Error("runs must be deny, ask or allow")
	}
	if _, err := withRuns(context.Background(), editsAsk); err == nil {
		t.Error("runs without a workdir must be refused")
	}
}

func TestTheHintMentionsRunningOnlyWhenItIsOffered(t *testing.T) {
	if runsHint(editsDeny) != "" || runsHint("") != "" {
		t.Error("no run tool, no hint")
	}
	if !strings.Contains(runsHint(editsAsk), "run") {
		t.Error("the ask hint must mention the run tool")
	}
}
