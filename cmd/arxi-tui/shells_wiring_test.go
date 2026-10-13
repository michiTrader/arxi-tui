//go:build !windows

package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	extv2 "github.com/michiTrader/arxi_tui/internal/ext/v2"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

type shellRig struct {
	c    *chatSession
	core *seenChat
	out  chan fold.Event
}

func shellChat(t *testing.T, runs string) *shellRig {
	t.Helper()
	core := &seenChat{}
	c, out := pkgToolChat(t, core, newPackageToolbox())
	c.setRuns(runs)
	t.Cleanup(func() { c.shells.stopAll() })
	return &shellRig{c, core, out}
}

func (r *shellRig) turn(t *testing.T, text string) driver.ChatSendParams {
	t.Helper()
	sendAndWait(t, r.c, r.out, text, "agent.turn_done")
	return r.core.last()
}

// The shell tools follow the policy of run: not offered when commands are denied.
func TestShellToolsAreOfferedOnlyWhereCommandsMayRun(t *testing.T) {
	for _, tc := range []struct {
		runs string
		want bool
	}{{policyAllow, true}, {policyAsk, true}, {policyDeny, false}} {
		p := shellChat(t, tc.runs).turn(t, "arranca el servidor")
		got := false
		for _, n := range names(p.ClientTools) {
			if isShellTool(n) {
				got = true
			}
		}
		if got != tc.want {
			t.Errorf("runs=%s: offered=%v, want %v (%v)", tc.runs, got, tc.want, names(p.ClientTools))
		}
		if has := strings.Contains(p.System, shellsHint); has != tc.want {
			t.Errorf("runs=%s: the hint is present=%v, want %v", tc.runs, has, tc.want)
		}
	}
}

// Without a project folder there is nowhere to start one and the core refuses tools.
func TestNoShellToolsWithoutAProjectFolder(t *testing.T) {
	r := shellChat(t, policyAllow)
	r.c.setWorkdir("")
	if n := len(r.turn(t, "arregla el banner").ClientTools); n != 0 {
		t.Errorf("tools lent with no workdir: %d", n)
	}
}

// The three share the eight with the others; when they do not fit, none is half-lent.
func TestShellToolsNeverExceedTheBudgetOrComeInPart(t *testing.T) {
	r := shellChat(t, policyAllow)
	var ts []extv2.Tool
	for _, n := range []string{"a", "b", "c", "d", "e", "f"} {
		ts = append(ts, echoT(n))
	}
	r.c.tools = boxOf(toolPkg(t, "p", true, ts...))
	p := r.turn(t, "arregla el banner")
	if len(p.ClientTools) > clientToolBudget {
		t.Fatalf("%d tools lent, budget %d", len(p.ClientTools), clientToolBudget)
	}
	n := 0
	for _, name := range names(p.ClientTools) {
		if isShellTool(name) {
			n++
		}
	}
	if n != 0 && n != 3 {
		t.Errorf("%d of the 3 shell tools lent", n)
	}
	if n == 0 && strings.Contains(p.System, shellsHint) {
		t.Error("the hint names tools that were not lent")
	}
}

// A core that cannot carry client tools answers anyway, without the hint.
func TestACoreWithoutClientToolsGetsNoShellHint(t *testing.T) {
	core := &seenChat{fail: func(p driver.ChatSendParams) error {
		if len(p.ClientTools) > 0 {
			return driver.ErrClientToolsUnsupported
		}
		return nil
	}}
	c, out := pkgToolChat(t, core, newPackageToolbox())
	t.Cleanup(func() { c.shells.stopAll() })
	sendAndWait(t, c, out, "arregla el banner", "agent.turn_done")
	p := core.last()
	if len(p.ClientTools) != 0 || strings.Contains(p.System, shellsHint) {
		t.Errorf("the retry still names the shell tools: %v", names(p.ClientTools))
	}
}

// The model's call reaches the registry through the turn's own callback, and the
// command it started outlives the turn.
func TestAShellStartedInATurnOutlivesTheTurn(t *testing.T) {
	r := shellChat(t, policyAllow)
	p := r.turn(t, "arranca")
	res := p.OnClientTool(context.Background(), driver.ClientToolCall{CallID: "1", Name: toolShellStart, Arguments: []byte(`{"command":"sleep 30"}`)})
	if !res.OK {
		t.Fatalf("%+v", res)
	}
	r.turn(t, "y ahora?")
	if n := r.c.shells.running(); n != 1 {
		t.Fatalf("the shell did not survive the next turn: %d running", n)
	}
	if got := shellsLabel(runningShells(&serveDriver{chat: r.c})); got != "1 shell" {
		t.Errorf("the bar would say %q", got)
	}
}

// /clear is a new conversation: it does not inherit servers.
func TestClearStopsTheShells(t *testing.T) {
	r := shellChat(t, policyAllow)
	if _, err := r.c.shells.start("sleep 30", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	r.c.reset()
	waitFor(t, "shells stopped", func() bool { return r.c.shells.running() == 0 })
}

// Closing the program must not leave them behind.
func TestClosingTheDriverStopsTheShells(t *testing.T) {
	r := shellChat(t, policyAllow)
	if _, err := r.c.shells.start("sleep 30", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	d := &serveDriver{chat: r.c, closer: func() error { return nil }}
	_ = d.Close()
	if n := r.c.shells.running(); n != 0 {
		t.Errorf("%d still running after Close", n)
	}
}

// A shell that ends by itself is told to the user as a line of the conversation.
func TestAShellThatEndsByItselfIsReported(t *testing.T) {
	r := shellChat(t, policyAllow)
	if _, err := r.c.shells.start("exit 3", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-r.out:
			if ev.Type == "chat.warn" {
				txt := fmt.Sprint(ev.Payload["text"])
				if !strings.Contains(txt, "code 3") || !strings.Contains(txt, "exit 3") {
					t.Errorf("the line does not say how it ended: %q", txt)
				}
				return
			}
		case <-deadline:
			t.Fatal("no word that the shell ended")
		}
	}
}
