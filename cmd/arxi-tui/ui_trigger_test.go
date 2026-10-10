package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// The interface tools and guide are lent only to a question that asks for them
// (/ui <request> or @ui). Always on, a user building a TUI of their own could not tell
// whether "the TUI" meant this app or theirs.

func TestUITriggerIsReadOnlyWhereTheUserWroteIt(t *testing.T) {
	cases := []struct {
		in   string
		want string
		on   bool
	}{
		{"/ui give the input bar a rainbow border", "give the input bar a rainbow border", true},
		{"  /ui   make it purple ", "make it purple", true},
		{"@ui make it purple", "make it purple", true},
		{"make the input bar round @ui", "make the input bar round", true},
		{"make it purple @UI please", "make it purple please", true},
		// typed commands are the host's, never a question for the model
		{"/ui add node below_input {}", "/ui add node below_input {}", false},
		{"/ui undo", "/ui undo", false},
		{"/ui color markdown.code fg=magenta", "/ui color markdown.code fg=magenta", false},
		// look-alikes
		{"/uize the thing", "/uize the thing", false},
		{"mail me@ui.com", "mail me@ui.com", false},
		{"fix the tui of my project", "fix the tui of my project", false},
		{"/ui add a blank line under the input bar", "add a blank line under the input bar", true},
		{"@uiux review", "@uiux review", false},
	}
	for _, c := range cases {
		got, on := uiTrigger(c.in)
		if got != c.want || on != c.on {
			t.Errorf("uiTrigger(%q) = %q, %v; want %q, %v", c.in, got, on, c.want, c.on)
		}
	}
}

type toolsSeen struct {
	mu      sync.Mutex
	tools   [][]driver.ClientToolDef
	systems []string
	prompts []string
}

func (*toolsSeen) Hello() *driver.Hello {
	return &driver.Hello{Implemented: []string{"chat.send"}}
}

func (c *toolsSeen) SubmitChatSend(_ context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error) {
	c.mu.Lock()
	c.tools = append(c.tools, p.ClientTools)
	c.systems = append(c.systems, p.System)
	c.prompts = append(c.prompts, p.Prompt)
	c.mu.Unlock()
	return &driver.ChatSendResult{Text: "ok", Model: "m", Provider: "p"}, nil
}

func (c *toolsSeen) at(i int) ([]driver.ClientToolDef, string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tools[i], c.systems[i], c.prompts[i]
}

func TestTheInterfaceToolsRideOnlyWithAQuestionThatAskedForThem(t *testing.T) {
	out := make(chan fold.Event, 128)
	core := &toolsSeen{}
	c := newChatSession(core, out)
	c.setWorkdir(t.TempDir())
	c.ui = newUIBridge()
	c.ui.publish(builtinDoc(t), nil, nil, nil, behaviour{})

	turn(t, c, out, "change the colours of the tui in my project")
	tools, system, _ := core.at(0)
	if len(tools) != 0 || strings.Contains(system, "arxi-tui") || strings.Contains(system, "ui_edit") {
		t.Fatalf("a plain question carried the interface: %d tools, system %q", len(tools), system)
	}

	turn(t, c, out, "/ui make the border of the input bar a rainbow")
	tools, system, prompt := core.at(1)
	if len(tools) != 2 {
		t.Fatalf("the request carried %d tools, want ui_guide and ui_edit", len(tools))
	}
	if !strings.Contains(system, uiSystemHint) || !strings.Contains(system, "Current document") || !strings.Contains(system, "input_frame") {
		t.Fatalf("the guide did not travel with the request (system is %d bytes)", len(system))
	}
	if prompt != "make the border of the input bar a rainbow" {
		t.Fatalf("the model saw %q; the switch is not part of the request", prompt)
	}

	// One /ui is enough: the interface conversation stays on, so the next message is
	// still about the interface and carries the guide and the tools.
	turn(t, c, out, "and make it a bit slower")
	tools, system, prompt = core.at(2)
	if len(tools) != 2 || !strings.Contains(system, "ui_edit") || !strings.Contains(system, uiSystemHint) {
		t.Fatalf("the interface did not stay on for the follow-up: %d tools", len(tools))
	}
	if prompt != "and make it a bit slower" {
		t.Fatalf("the follow-up reached the model as %q", prompt)
	}

	// /ui off leaves it: an ordinary chat again.
	c.setUIMode(false)
	turn(t, c, out, "thanks, now explain the project")
	if tools, system, _ := core.at(3); len(tools) != 0 || strings.Contains(system, "ui_edit") {
		t.Fatal("the interface stayed on after /ui off")
	}
}

func TestTheUICommandWordIsOnTheRecord(t *testing.T) {
	out := make(chan fold.Event, 128)
	c := newChatSession(&toolsSeen{}, out)
	c.setWorkdir(t.TempDir())
	c.ui = newUIBridge()
	c.ui.publish(builtinDoc(t), nil, nil, nil, behaviour{})
	var got []fold.Event
	for _, line := range []string{"/ui make the border red", "now blue"} {
		if err := c.send(context.Background(), line); err != nil {
			t.Fatal(err)
		}
		got = append(got, drain(out, "agent.turn_done", 2*time.Second)...)
	}
	var cmds, texts []string
	for _, ev := range got {
		if ev.Type == "run.prompt" {
			cmd, _ := ev.Payload["command"].(string)
			txt, _ := ev.Payload["text"].(string)
			cmds, texts = append(cmds, cmd), append(texts, txt)
		}
	}
	if len(cmds) != 2 || cmds[0] != "/ui" || cmds[1] != "" || texts[0] != "make the border red" || texts[1] != "now blue" {
		t.Fatalf("run.prompt carried command %q text %q; the first line must keep /ui apart from its words and the follow-up must not repeat it", cmds, texts)
	}
}

func TestResetEndsTheInterfaceConversation(t *testing.T) {
	c := newChatSession(&toolsSeen{}, make(chan fold.Event, 8))
	c.setUIMode(true)
	c.reset()
	if c.inUIMode() {
		t.Fatal("/clear left the interface conversation on")
	}
}

func TestUIModeCommands(t *testing.T) {
	for line, want := range map[string][2]bool{
		"/ui": {true, true}, "/ui on": {true, true}, "/ui off": {false, true}, "/ui exit": {false, true},
		"/ui undo": {false, false}, "/ui make it red": {false, false}, "hello": {false, false},
	} {
		if on, ok := uiModeCommand(line); on != want[0] || ok != want[1] {
			t.Errorf("uiModeCommand(%q) = %v,%v want %v,%v", line, on, ok, want[0], want[1])
		}
	}
	if _, ok := uiPromptAfterSlash("/ui off"); ok {
		t.Error("\"/ui off\" must not be read as a request for the model")
	}
}

func TestABareAtUIAsksForTheRequest(t *testing.T) {
	out := make(chan fold.Event, 16)
	c := newChatSession(&toolsSeen{}, out)
	c.setWorkdir(t.TempDir())
	if err := c.send(context.Background(), "@ui"); err == nil || !strings.Contains(err.Error(), "needs a request") {
		t.Fatalf("err = %v", err)
	}
	// and the session is not left busy by the refusal
	if err := c.send(context.Background(), "hello"); err != nil {
		t.Fatalf("the session stayed busy: %v", err)
	}
}
