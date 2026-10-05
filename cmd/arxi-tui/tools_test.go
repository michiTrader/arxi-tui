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

// toolChat looks at two things through the callback the session hands it, then
// answers. With old set it behaves like a core that does not know workdir.
type toolChat struct {
	old bool

	mu       sync.Mutex
	workdirs []string
}

func (*toolChat) Hello() *driver.Hello {
	return &driver.Hello{Implemented: []string{"chat.send"}}
}

func (c *toolChat) SubmitChatSend(_ context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error) {
	c.mu.Lock()
	c.workdirs = append(c.workdirs, p.Workdir)
	c.mu.Unlock()
	if c.old && p.Workdir != "" {
		return nil, driver.ErrToolsUnsupported
	}
	if p.OnTool != nil {
		p.OnTool(driver.ToolCall{ID: "c1", Name: "list", Arg: ".", OK: true, Summary: "Listed 2 entries", Output: "a\nb"})
		p.OnTool(driver.ToolCall{ID: "c2", Name: "read", Arg: ".env", OK: false, Summary: "looks like it holds secrets"})
	}
	return &driver.ChatSendResult{Text: "done", Model: "m", Provider: "p"}, nil
}

func types(events []fold.Event) string {
	var out []string
	for _, ev := range events {
		out = append(out, ev.Type)
	}
	return strings.Join(out, ",")
}

// What the model looks at reaches the conversation as chat.tool events, between
// the question and the answer, and the folder rides along with the turn.
func TestChatSessionRelaysToolCalls(t *testing.T) {
	out := make(chan fold.Event, 32)
	core := &toolChat{}
	c := newChatSession(core, out)
	c.setWorkdir("/proj")
	if err := c.send(context.Background(), "what is here"); err != nil {
		t.Fatal(err)
	}
	events := drain(out, "agent.turn_done", 2*time.Second)
	if got, want := types(events), "run.prompt,agent.activated,chat.tool,chat.tool,llm.response,agent.turn_done"; got != want {
		t.Fatalf("events = %s, want %s", got, want)
	}
	if core.workdirs[0] != "/proj" {
		t.Errorf("workdir sent = %q", core.workdirs[0])
	}
	st := fold.Fold(events)
	var roles []string
	for _, h := range st.History {
		roles = append(roles, h.Role)
	}
	if strings.Join(roles, ",") != "user,tool,tool,assistant" {
		t.Errorf("history roles = %v", roles)
	}
	if h := st.History[2]; h.Tool != "read" || h.ToolOK || h.ToolSummary != "looks like it holds secrets" {
		t.Errorf("failed call lost its outcome: %+v", h)
	}
}

func TestChatSessionWithoutAFolderSendsNone(t *testing.T) {
	out := make(chan fold.Event, 32)
	core := &toolChat{}
	c := newChatSession(core, out)
	if err := c.send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	drain(out, "agent.turn_done", 2*time.Second)
	if core.workdirs[0] != "" {
		t.Errorf("workdir sent = %q, want none", core.workdirs[0])
	}
}

// An old core cannot give tools: the user is told, and the question is still
// answered the plain way.
func TestChatSessionWarnsAndAnswersAgainstACoreWithoutTools(t *testing.T) {
	out := make(chan fold.Event, 32)
	core := &toolChat{old: true}
	c := newChatSession(core, out)
	c.setWorkdir("/proj")
	if err := c.send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	events := drain(out, "agent.turn_done", 2*time.Second)
	if got, want := types(events), "run.prompt,agent.activated,chat.warn,llm.response,agent.turn_done"; got != want {
		t.Fatalf("events = %s, want %s", got, want)
	}
	warn, _ := events[2].Payload["text"].(string)
	if !strings.Contains(warn, "cannot give the model access to your files") {
		t.Errorf("warning = %q", warn)
	}
	if len(core.workdirs) != 2 || core.workdirs[0] != "/proj" || core.workdirs[1] != "" {
		t.Errorf("asks = %q; want the folder first, then none", core.workdirs)
	}
}
