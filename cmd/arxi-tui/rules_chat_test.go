package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// systemChat records the system prompt each turn is sent with.
type systemChat struct {
	mu      sync.Mutex
	systems []string
}

func (*systemChat) Hello() *driver.Hello {
	return &driver.Hello{Implemented: []string{"chat.send"}}
}

func (c *systemChat) SubmitChatSend(_ context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error) {
	c.mu.Lock()
	c.systems = append(c.systems, p.System)
	c.mu.Unlock()
	return &driver.ChatSendResult{Text: "ok", Model: "m", Provider: "p"}, nil
}

func (c *systemChat) last() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.systems) == 0 {
		return ""
	}
	return c.systems[len(c.systems)-1]
}

func turn(t *testing.T, c *chatSession, out chan fold.Event, text string) {
	t.Helper()
	if err := c.send(context.Background(), text); err != nil {
		t.Fatal(err)
	}
	drain(out, "agent.turn_done", 2*time.Second)
}

// The rules written in the project folder are what the model is told, every turn,
// and an edit to the file counts from the next question.
func TestProjectRulesReachTheModelInTheSystemPrompt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("Always answer in haiku."), 0o644); err != nil {
		t.Fatal(err)
	}
	out := make(chan fold.Event, 64)
	core := &systemChat{}
	c := newChatSession(core, out)
	c.setWorkdir(dir)

	turn(t, c, out, "what is in this folder")
	if got := core.last(); !strings.Contains(got, "Always answer in haiku.") || !strings.HasPrefix(got, chatSystem()) {
		t.Fatalf("the rules did not reach the model:\n%s", got)
	}

	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("Reply only in French."), 0o644); err != nil {
		t.Fatal(err)
	}
	turn(t, c, out, "and in the parent folder")
	if got := core.last(); !strings.Contains(got, "Reply only in French.") || strings.Contains(got, "haiku") {
		t.Fatalf("an edit to the rules did not count from the next turn:\n%s", got)
	}
}

func TestNoRulesMeansTheBareSystemPrompt(t *testing.T) {
	out := make(chan fold.Event, 64)
	core := &systemChat{}
	c := newChatSession(core, out)
	c.setWorkdir(t.TempDir())
	turn(t, c, out, "what is in this folder")
	if got := core.last(); got != chatSystem() {
		t.Fatalf("with no rules file the system prompt must be untouched:\n%s", got)
	}
}

func TestSwitchingTheRulesOffKeepsThemFromTheModel(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ARXI.md"), []byte("secret rule"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(rulesOffEnv, "off")
	out := make(chan fold.Event, 64)
	core := &systemChat{}
	c := newChatSession(core, out)
	c.setWorkdir(dir)
	turn(t, c, out, "what is in this folder")
	if got := core.last(); strings.Contains(got, "secret rule") {
		t.Fatalf("rules were sent although they are switched off:\n%s", got)
	}
}
