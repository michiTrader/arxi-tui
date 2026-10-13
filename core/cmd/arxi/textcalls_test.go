package main

import (
	"context"
	"encoding/json"
	"github.com/michiTrader/arxi/internal/chattools"
	"github.com/michiTrader/arxi/internal/turn"
	"strings"
	"testing"

	"github.com/michiTrader/arxi/internal/provider"
)

// hermesCall is how a model that cannot use the tool channel writes a call: as text.
const hermesCall = "<tool_call>\n<function=read>\n<parameter=path>\nmain.go\n</parameter>\n<parameter=limit>\n5\n</parameter>\n</function>\n</tool_call>"

// The reported failure: the model's "final answer" was its own tool call, written as
// text, and the turn ended there with the call on screen and nothing done.
func TestAToolCallWrittenAsTextIsRunNotShownAsTheAnswer(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "plain"
		if stream {
			name = "streamed"
		}
		t.Run(name, func(t *testing.T) {
			f := setUpFake(t)
			f.script = []scripted{{text: "Let me look.\n" + hermesCall}}
			ctx, got := collect(t, projectDir(t))
			if stream {
				ctx = provider.WithThinking(ctx, func(string) {})
			}
			res, err := chatSendEffort(ctx, "what is in main.go?", "", "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(res.Text, "<tool_call>") || strings.Contains(res.Text, "<function=") {
				t.Fatalf("the model's tool call came back as the answer: %q", res.Text)
			}
			if len(*got) != 1 || (*got)[0].Name != "read" || !(*got)[0].OK {
				t.Fatalf("the call written as text was not run: %+v", *got)
			}
			if !strings.HasPrefix(res.Text, "echo:") {
				t.Errorf("the model was never asked again after the tool ran: %q", res.Text)
			}
		})
	}
}

func TestATextCallIsGivenTheTypesTheToolWants(t *testing.T) {
	f := setUpFake(t)
	// limit is an integer in read's schema: "1" written as text must arrive as 1.
	f.script = []scripted{{text: "<tool_call>\n<function=read>\n<parameter=path>\nmain.go\n</parameter>\n<parameter=limit>\n1\n</parameter>\n</function>\n</tool_call>"}}
	ctx, got := collect(t, projectDir(t))
	if _, err := chatSendEffort(ctx, "go", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 1 || !(*got)[0].OK {
		t.Fatalf("calls = %+v", *got)
	}
	// limit=1 only means something if it arrived as the integer 1: the reply says it
	// showed lines 1-1, and not the whole three-line file.
	if out := (*got)[0].Output; !strings.Contains(out, "lines 1-1 of 3") {
		t.Errorf("limit did not arrive as an integer: %q", out)
	}
}

func TestAJSONTextCallIsRunToo(t *testing.T) {
	f := setUpFake(t)
	f.script = []scripted{{text: `<tool_call>{"name":"list","arguments":{"path":"."}}</tool_call>`}}
	ctx, got := collect(t, projectDir(t))
	res, err := chatSendEffort(ctx, "go", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(*got) != 1 || (*got)[0].Name != "list" || strings.Contains(res.Text, "<tool_call>") {
		t.Fatalf("calls=%+v text=%q", *got, res.Text)
	}
}

// A name that is not one of the tools offered is not a call: it stays text, and
// nothing runs. Guessing would run what nobody offered.
func TestATextCallToAnUnknownToolIsLeftAlone(t *testing.T) {
	f := setUpFake(t)
	f.script = []scripted{{text: "<tool_call>\n<function=format_disk>\n<parameter=x>\n1\n</parameter>\n</function>\n</tool_call>"}}
	ctx, got := collect(t, projectDir(t))
	res, err := chatSendEffort(ctx, "go", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(*got) != 0 {
		t.Fatalf("an unknown tool ran: %+v", *got)
	}
	if !strings.Contains(res.Text, "format_disk") {
		t.Errorf("the text was changed: %q", res.Text)
	}
}

// Without tools there is nothing to run: a turn with no folder shows the text as is.
func TestATextCallWithNoToolsIsJustText(t *testing.T) {
	f := setUpFake(t)
	f.script = []scripted{{text: hermesCall}}
	res, err := chatSendEffort(context.Background(), "go", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "<tool_call>") {
		t.Errorf("text with no tools to run was rewritten: %q", res.Text)
	}
}

// A policy still applies: a call written as text to run a command is put to the user
// exactly like a native one. Nothing is run because the model chose another syntax.
func TestATextCallStillGoesThroughThePolicy(t *testing.T) {
	f := setUpFake(t)
	f.script = []scripted{{text: "<tool_call>\n<function=run>\n<parameter=command>\necho hi\n</parameter>\n</function>\n</tool_call>"}}
	var asked []string
	ctx, err := withTools(context.Background(), projectDir(t), "", func(n chatApprovalNotification) (bool, error) {
		asked = append(asked, n.Name+":"+n.Arg)
		return false, nil
	}, func(chatToolNotification) {})
	if err != nil {
		t.Fatal(err)
	}
	if ctx, err = withRuns(ctx, editsAsk); err != nil {
		t.Fatal(err)
	}
	if _, err := chatSendEffort(ctx, "go", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || asked[0] != "run:echo hi" {
		t.Fatalf("the command was not put to the user: %v", asked)
	}
}

func TestAJSONTextCallToAnUnknownToolIsLeftAlone(t *testing.T) {
	f := setUpFake(t)
	f.script = []scripted{{text: `<tool_call>{"name":"format_disk","arguments":{}}</tool_call>`}}
	ctx, got := collect(t, projectDir(t))
	res, err := chatSendEffort(ctx, "go", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(*got) != 0 || !strings.Contains(res.Text, "format_disk") {
		t.Fatalf("calls=%+v text=%q", *got, res.Text)
	}
}

// What the model reads next is its call as a call, once. If the raw block stayed in
// the text too, every later request would carry the call twice, and the model would
// learn from its own history that writing calls as text is what works.
func TestTheRawBlockIsNotKeptInWhatTheModelReadsNext(t *testing.T) {
	f := setUpFake(t)
	f.script = []scripted{{text: "Let me look.\n" + hermesCall}}
	ctx, _ := collect(t, projectDir(t))
	if _, err := chatSendEffort(ctx, "go", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if len(f.messages) != 2 {
		t.Fatalf("asks = %d, want 2", len(f.messages))
	}
	var assistant map[string]any
	for _, m := range f.messages[1] {
		if m["role"] == "assistant" {
			assistant = m
		}
	}
	if assistant == nil {
		t.Fatalf("no assistant message in the second ask: %v", f.messages[1])
	}
	if c, _ := assistant["content"].(string); strings.Contains(c, "<tool_call>") || strings.Contains(c, "<function=") {
		t.Errorf("the raw call stayed in the history: %q", c)
	}
	if calls, _ := assistant["tool_calls"].([]any); len(calls) != 1 {
		t.Errorf("the call is not in the history as a call: %v", assistant)
	}
}

// The shapes from the user's own sessions: two calls in one reply, a Windows path with
// backslashes, and a multi-line value that holds quotes and a "$".
func TestTheUsersOwnTextCallsAreRun(t *testing.T) {
	twoReads := "<tool_call>\n<function=read>\n<parameter=limit>\n30\n</parameter>\n<parameter=offset>\n0\n</parameter>\n<parameter=path>\nmain.go\n</parameter>\n</function>\n</tool_call>\n" +
		"<tool_call>\n<function=read>\n<parameter=limit>\n2\n</parameter>\n<parameter=path>\nmain.go\n</parameter>\n</function>\n</tool_call>"
	f := setUpFake(t)
	f.script = []scripted{{text: twoReads}}
	ctx, got := collect(t, projectDir(t))
	res, err := chatSendEffort(ctx, "go", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(*got) != 2 || !(*got)[0].OK || !(*got)[1].OK {
		t.Fatalf("both calls of one reply must run: %+v", *got)
	}
	if strings.Contains(res.Text, "<tool_call>") {
		t.Errorf("a call came back as the answer: %q", res.Text)
	}
}

func TestATextRunCallWithBackslashesAndQuotesKeepsThemExactly(t *testing.T) {
	cmd := "powershell -NoProfile -Command \"$c = Get-Content 'D:\\projects\\x\\_tools.py' -Raw\""
	text := "<tool_call>\n<function=run>\n<parameter=command>\n" + cmd + "\n</parameter>\n<parameter=timeout_seconds>\n60\n</parameter>\n</function>\n</tool_call>"
	calls, rest := textCalls(text, chattoolDefs(), "t")
	if len(calls) != 1 || strings.TrimSpace(rest) != "" {
		t.Fatalf("calls=%d rest=%q", len(calls), rest)
	}
	var args map[string]any
	if err := json.Unmarshal(calls[0].Arguments, &args); err != nil {
		t.Fatal(err)
	}
	if args["command"] != cmd {
		t.Errorf("command = %q, want %q", args["command"], cmd)
	}
	if n, ok := args["timeout_seconds"].(float64); !ok || n != 60 {
		t.Errorf("timeout_seconds = %#v, want the number 60", args["timeout_seconds"])
	}
}

func chattoolDefs() []turn.ToolDefinition {
	var out []turn.ToolDefinition
	for _, d := range append(chattools.Definitions(), chattools.RunDefinitions()...) {
		out = append(out, turn.ToolDefinition{Name: d.Name, Description: d.Description, InputSchema: d.Schema})
	}
	return out
}
