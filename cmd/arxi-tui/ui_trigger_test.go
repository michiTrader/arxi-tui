package main

import (
	"context"
	"strings"
	"sync"
	"testing"

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

	turn(t, c, out, "thanks, now explain the project")
	if tools, system, _ := core.at(2); len(tools) != 0 || strings.Contains(system, "ui_edit") {
		t.Fatal("the interface stayed on for the next question")
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
