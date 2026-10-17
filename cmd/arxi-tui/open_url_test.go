//go:build !windows

package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
)

func urlCall(addr string) driver.ClientToolCall {
	return shellCall(toolOpenURL, map[string]any{"url": addr})
}

// opened is a registry whose browser is a recorder: nothing real is opened.
func openRig(t *testing.T) (*shellRegistry, *[]string) {
	t.Helper()
	r := newTestShells(t)
	var got []string
	r.open = func(addr string) error { got = append(got, addr); return nil }
	return r, &got
}

func TestOpenURLTakesOnlyWebAddresses(t *testing.T) {
	good := []string{
		"http://localhost:8000/report.html",
		"https://example.com/a?b=1&c=2#frag",
		"  https://example.com  ",
	}
	for _, g := range good {
		if _, err := checkURL(g); err != nil {
			t.Errorf("%q refused: %v", g, err)
		}
	}
	bad := map[string]string{
		"empty":        "",
		"file":         "file:///etc/passwd",
		"custom":       "vscode://settings",
		"javascript":   "javascript:alert(1)",
		"no scheme":    "example.com/page",
		"no host":      "https://",
		"a space":      "https://example.com/a b",
		"a newline":    "https://example.com/\nrm -rf /",
		"an escape":    "https://example.com/\x1b[2J",
		"a nul":        "https://example.com/a\x00b",
		"too long":     "https://example.com/" + strings.Repeat("a", maxURLLen),
		"a path":       "/mnt/c/Users/x/report.html",
		"bare windows": `C:\Users\x\report.html`,
	}
	for name, b := range bad {
		if _, err := checkURL(b); err == nil {
			t.Errorf("%s (%q) was accepted", name, b)
		}
	}
}

// The command that reaches the user's browser is different on each system, and the
// address always travels as ONE argument, so an "&" in it never ends a command.
func TestOpenerCommandPerSystem(t *testing.T) {
	addr := "https://example.com/a?b=1&c=2"
	none := func(string) bool { return false }
	only := func(names ...string) func(string) bool {
		return func(n string) bool {
			for _, x := range names {
				if x == n {
					return true
				}
			}
			return false
		}
	}
	cases := []struct {
		name string
		goos string
		wsl  bool
		have func(string) bool
		prog string
		args []string
	}{
		{"windows", "windows", false, none, "rundll32", []string{"url.dll,FileProtocolHandler", addr}},
		{"mac", "darwin", false, none, "open", []string{addr}},
		{"linux", "linux", false, none, "xdg-open", []string{addr}},
		{"wsl with wslview", "linux", true, only("wslview", "rundll32.exe"), "wslview", []string{addr}},
		{"wsl without it", "linux", true, only("rundll32.exe"), "rundll32.exe", []string{"url.dll,FileProtocolHandler", addr}},
		{"wsl with nothing", "linux", true, none, "xdg-open", []string{addr}},
	}
	for _, c := range cases {
		prog, args := openerCommand(c.goos, c.wsl, c.have, addr)
		if prog != c.prog || !reflect.DeepEqual(args, c.args) {
			t.Errorf("%s: got %s %q, want %s %q", c.name, prog, args, c.prog, c.args)
		}
	}
}

func TestTheModelOpensAPageAfterTheUserAllowsIt(t *testing.T) {
	r, got := openRig(t)
	ctx, dir := context.Background(), t.TempDir()
	var asked []driver.Approval
	yes := func(a driver.Approval) bool { asked = append(asked, a); return true }
	res := r.call(ctx, urlCall("http://localhost:8000/r.html"), dir, policyAsk, yes)
	if !res.OK || !reflect.DeepEqual(*got, []string{"http://localhost:8000/r.html"}) {
		t.Fatalf("res=%+v opened=%v", res, *got)
	}
	if len(asked) != 1 || asked[0].Name != toolOpenURL || asked[0].Arg != "http://localhost:8000/r.html" {
		t.Errorf("the user was not asked about this address: %+v", asked)
	}
	if !strings.Contains(res.Text, "do not open it again") {
		t.Errorf("the model must be told not to open it twice: %q", res.Text)
	}
}

func TestOpenURLFollowsThePolicyOfRun(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	addr := "https://example.com/"
	no := func(driver.Approval) bool { return false }

	r, got := openRig(t)
	if res := r.call(ctx, urlCall(addr), dir, policyDeny, nil); res.OK || len(*got) != 0 {
		t.Errorf("deny opened a page: %+v %v", res, *got)
	}
	res := r.call(ctx, urlCall(addr), dir, policyAsk, no)
	if res.OK || len(*got) != 0 || !strings.Contains(res.Text, "do not retry") {
		t.Errorf("a no must open nothing and say not to retry: %+v %v", res, *got)
	}
	if res := r.call(ctx, urlCall(addr), dir, policyAsk, nil); res.OK || len(*got) != 0 {
		t.Errorf("ask with no way to ask opened a page: %+v", res)
	}
	asked := 0
	count := func(driver.Approval) bool { asked++; return false }
	if res := r.call(ctx, urlCall(addr), dir, policyAllow, count); !res.OK || asked != 0 || len(*got) != 1 {
		t.Errorf("allow must open without asking: %+v asked=%d opened=%v", res, asked, *got)
	}
}

// A refused address is refused BEFORE the user is asked: nobody is put a question about
// a file:// address that could never open.
func TestOpenURLRefusesABadAddressWithoutAskingOrOpening(t *testing.T) {
	r, got := openRig(t)
	asked := 0
	count := func(driver.Approval) bool { asked++; return true }
	for _, bad := range []string{"file:///etc/passwd", "", "javascript:alert(1)"} {
		res := r.call(context.Background(), urlCall(bad), t.TempDir(), policyAsk, count)
		if res.OK {
			t.Errorf("%q accepted", bad)
		}
	}
	if asked != 0 || len(*got) != 0 {
		t.Errorf("asked=%d opened=%v", asked, *got)
	}
}

func TestAnOpenerThatFailsIsAnErrorTheModelCanRead(t *testing.T) {
	r := newTestShells(t)
	r.open = func(string) error { return errors.New("xdg-open failed: no method available") }
	res := r.call(context.Background(), urlCall("https://example.com/"), t.TempDir(), policyAllow, nil)
	if res.OK || !strings.Contains(res.Text, "no method available") {
		t.Errorf("%+v", res)
	}
}

// Through the real turn: offered with the shell tools (so only where commands may run),
// asked about under ask, and not asked again once the user moved to full access.
func TestOpenURLIsLentWithTheShellToolsAndFollowsTheLiveMode(t *testing.T) {
	r := shellChat(t, policyAsk)
	var opened []string
	r.c.shells.open = func(addr string) error { opened = append(opened, addr); return nil }
	p := r.turn(t, "muestrame el reporte")
	found := false
	for _, n := range names(p.ClientTools) {
		if n == toolOpenURL {
			found = true
		}
	}
	if !found {
		t.Fatalf("open_url was not lent: %v", names(p.ClientTools))
	}
	if !strings.Contains(p.System, "open_url") {
		t.Errorf("the hint does not name the tool: %q", p.System)
	}
	go func() {
		wait(t, r.c.pendingNow)
		r.c.decide(false)
	}()
	if res := p.OnClientTool(context.Background(), urlCall("https://example.com/")); res.OK || len(opened) != 0 {
		t.Fatalf("under ask a no must open nothing: %+v %v", res, opened)
	}
	r.c.setRuns(policyAllow)
	if res := p.OnClientTool(context.Background(), urlCall("https://example.com/")); !res.OK || len(opened) != 1 {
		t.Errorf("full access must open without asking: %+v %v", res, opened)
	}
}

func TestNoOpenURLWhereCommandsAreDenied(t *testing.T) {
	p := shellChat(t, policyDeny).turn(t, "muestrame el reporte")
	for _, n := range names(p.ClientTools) {
		if n == toolOpenURL {
			t.Errorf("open_url lent under deny")
		}
	}
}

// The model used to be told to open a page with run and a command per system. That
// sentence is the thing that made it open nothing on WSL; the tool replaces it.
func TestTheHintsPointAtOpenURLNotAtRun(t *testing.T) {
	if !strings.Contains(shellsHint, "with open_url") {
		t.Errorf("the shells hint does not say to use open_url: %s", shellsHint)
	}
	if strings.Contains(shellsHint, "in their browser with run") {
		t.Errorf("the shells hint still says to use run: %s", shellsHint)
	}
}

// A question about a page is waiting and the user moves to full access: it is answered
// yes by the mode, as a command's would be, and nobody has to press a key for it.
func TestAWaitingOpenURLQuestionIsSettledByFullAccess(t *testing.T) {
	r := shellChat(t, policyAsk)
	var opened []string
	r.c.shells.open = func(addr string) error { opened = append(opened, addr); return nil }
	p := r.turn(t, "muestrame el reporte")
	done := make(chan driver.ClientToolResult, 1)
	go func() { done <- p.OnClientTool(context.Background(), urlCall("https://example.com/")) }()
	wait(t, r.c.pendingNow)
	r.c.setRuns(policyAllow)
	r.c.modeChanged()
	select {
	case res := <-done:
		if !res.OK || len(opened) != 1 {
			t.Errorf("the waiting question was not answered yes: %+v %v", res, opened)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the question is still waiting after the mode allowed it")
	}
}
