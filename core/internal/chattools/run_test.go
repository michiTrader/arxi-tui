package chattools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func runBox(t *testing.T) *Toolbox {
	t.Helper()
	tb, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return tb.WithRuns()
}

func runArg(command string, seconds int) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"command": command, "timeout_seconds": seconds})
	return b
}

func TestRunNeedsToBeTurnedOn(t *testing.T) {
	tb, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = tb.Run(ToolRun, runArg("echo hi", 0))
	if err == nil || !strings.Contains(err.Error(), "may not run commands") {
		t.Fatalf("a toolbox that was not given runs must refuse, got %v", err)
	}
}

func TestRunReturnsTheOutputAndTheExitCode(t *testing.T) {
	tb := runBox(t)
	res, err := tb.Run(ToolRun, runArg("echo hello", 0))
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !strings.Contains(res.Text, "exit code 0") || !strings.Contains(res.Text, "hello") {
		t.Errorf("result = %+v", res)
	}
	if !strings.HasPrefix(res.Summary, "Exit 0") || res.Arg != "echo hello" {
		t.Errorf("summary/arg = %q / %q", res.Summary, res.Arg)
	}
}

func TestAFailingCommandIsAResultNotAnError(t *testing.T) {
	tb := runBox(t)
	res, err := tb.Run(ToolRun, runArg("echo oops 1>&2 && exit 3", 0))
	if err != nil {
		t.Fatalf("a command that ran and failed must not be an error: %v", err)
	}
	if !res.Failed || !strings.Contains(res.Text, "exit code 3") || !strings.Contains(res.Text, "oops") {
		t.Errorf("result = %+v", res)
	}
	if !strings.HasPrefix(res.Summary, "Exit 3") {
		t.Errorf("summary = %q", res.Summary)
	}
}

func TestACommandStartsInTheProjectFolder(t *testing.T) {
	tb := runBox(t)
	if err := os.WriteFile(filepath.Join(tb.Root(), "marker.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := "ls"
	if runtime.GOOS == "windows" {
		cmd = "dir /b"
	}
	res, err := tb.Run(ToolRun, runArg(cmd, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "marker.txt") {
		t.Errorf("the command did not run in the project folder:\n%s", res.Text)
	}
}

func TestACommandCannotWaitForTheKeyboard(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cat is not a Windows command")
	}
	tb := runBox(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		res, err := tb.Run(ToolRun, runArg("cat", 0))
		if err != nil || res.Failed {
			t.Errorf("cat on an empty input should just end: %+v %v", res, err)
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a command that reads standard input hung the turn")
	}
}

func TestATimeoutStopsTheCommandAndWhatItStarted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep is not a Windows command")
	}
	tb := runBox(t)
	started := time.Now()
	// The inner sleep is a child of the shell: stopping only the shell would leave it
	// holding the output pipe and the call would wait for it.
	res, err := tb.Run(ToolRun, runArg("sleep 30 & sleep 30", 1))
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 10*time.Second {
		t.Errorf("the timeout took %v to bite", time.Since(started))
	}
	if !res.Failed || !strings.Contains(res.Summary, "Stopped after 1s") {
		t.Errorf("result = %+v", res)
	}
}

func TestTheEnvironmentDoesNotCarrySecrets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("env is not a Windows command")
	}
	t.Setenv("OPENAI_API_KEY", "sk-should-not-leak")
	t.Setenv("ARXI_SECRETS_DIR", "/should/not/leak")
	t.Setenv("MY_PLAIN_SETTING", "visible")
	tb := runBox(t)
	res, err := tb.Run(ToolRun, runArg("env", 0))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Text, "should-not-leak") || strings.Contains(res.Text, "/should/not/leak") {
		t.Errorf("a secret reached the command's environment:\n%s", res.Text)
	}
	if !strings.Contains(res.Text, "MY_PLAIN_SETTING=visible") {
		t.Error("an ordinary variable must still be there")
	}
}

func TestLongOutputKeepsTheStartAndTheEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("seq is not a Windows command")
	}
	tb := runBox(t)
	res, err := tb.Run(ToolRun, runArg("seq 1 20000", 0))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "\n1\n") || !strings.Contains(res.Text, "\n20000") {
		t.Error("the start and the end of the output must both survive")
	}
	if !strings.Contains(res.Text, "bytes of output left out") {
		t.Error("the cut must be announced")
	}
	if len(res.Text) > maxRunHead+maxRunTail+200 {
		t.Errorf("the text for the model is %d bytes", len(res.Text))
	}
}

func TestPreviewStartsNothing(t *testing.T) {
	tb := runBox(t)
	marker := filepath.Join(tb.Root(), "made")
	res, err := tb.PreviewRun(runArg("echo x > made", 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a preview ran the command")
	}
	if res.Arg != "echo x > made" || !strings.Contains(res.Summary, tb.Root()) {
		t.Errorf("preview = %+v", res)
	}
}

func TestACallWithoutACommandIsRefused(t *testing.T) {
	tb := runBox(t)
	for _, raw := range []string{`{}`, `{"command":"   "}`} {
		if _, err := tb.Run(ToolRun, json.RawMessage(raw)); err == nil {
			t.Errorf("%s should be refused", raw)
		}
	}
}

func TestTheRunDefinitionIsOfferedByItself(t *testing.T) {
	defs := RunDefinitions()
	if len(defs) != 1 || defs[0].Name != ToolRun || !json.Valid(defs[0].Schema) {
		t.Fatalf("definitions = %+v", defs)
	}
	for _, d := range Definitions() {
		if d.Name == ToolRun {
			t.Error("run must not be among the always-offered tools")
		}
	}
}
