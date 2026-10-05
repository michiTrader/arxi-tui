package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michiTrader/arxi/internal/provider"
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
	ctx, err := withTools(context.Background(), dir, func(n chatToolNotification) { got = append(got, n) })
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
			if !strings.Contains(sys, "list, read and grep") {
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
	if _, err := withTools(context.Background(), filepath.Join(t.TempDir(), "nope"), nil); err == nil {
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
