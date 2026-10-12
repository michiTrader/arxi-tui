//go:build !windows

package main

import (
	"context"
	"strings"
	"sync"
	"testing"

	extv2 "github.com/michiTrader/arxi_tui/internal/ext/v2"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// stubBrowser replaces the program that opens an address and records what it was asked
// to open, so no test ever starts a browser.
func stubBrowser(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var opened []string
	old := openBrowser
	openBrowser = func(u string) error {
		mu.Lock()
		opened = append(opened, u)
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { openBrowser = old })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), opened...)
	}
}

const docsPkg = `,"permissions":{"open":["docs.example.com"]},"commands":[{"name":"hi","op":{"kind":"builtin","name":"echo"}}]`

func behRun(name string, acts ...string) string {
	q := make([]string, len(acts))
	for i, a := range acts {
		q[i] = `"` + a + `"`
	}
	return `{"commands":[{"name":"` + name + `","description":"d","run":[` + strings.Join(q, ",") + `]}]}`
}

func TestLoopSayActionShowsANoteAndSendsNothing(t *testing.T) {
	script := append(enterLine("/hello"), exitKeys()...)
	out, sent := runPackageWithBehaviour(t, behRun("hello", "say:All good here"), nil, nil, nil, script)
	if len(sent) != 0 {
		t.Fatalf("the model was sent %q; say: is the host talking to the user", sent)
	}
	if !strings.Contains(screen(out), "· All good here") {
		t.Errorf("the text is not on screen as a note; last frame:\n%s", lastFrame(out))
	}
}

func TestLoopOpenActionOpensAHostAnApprovedPackageNamed(t *testing.T) {
	opened := stubBrowser(t)
	script := append(enterLine("/docs"), exitKeys()...)
	out, sent := runPackageWithBehaviour(t, behRun("docs", "open:https://docs.example.com/start"),
		map[string]string{"dp": pkgDoc("dp", docsPkg)}, nil, []string{"dp"}, script)
	if got := opened(); len(got) != 1 || got[0] != "https://docs.example.com/start" {
		t.Fatalf("opened %q, want exactly the address written", got)
	}
	if len(sent) != 0 {
		t.Errorf("sent %q to the model", sent)
	}
	if !strings.Contains(screen(out), "Opening https://docs.example.com/start") {
		t.Errorf("the user was not told where the browser went; last frame:\n%s", lastFrame(out))
	}
}

func TestLoopOpenActionRefusedForAHostNoPackageNamed(t *testing.T) {
	opened := stubBrowser(t)
	script := append(enterLine("/docs"), exitKeys()...)
	out, _ := runPackageWithBehaviour(t, behRun("docs", "open:https://evil.example.org/"),
		map[string]string{"dp": pkgDoc("dp", docsPkg)}, nil, []string{"dp"}, script)
	if got := opened(); len(got) != 0 {
		t.Fatalf("opened %q for a host no approved package named", got)
	}
	if !strings.Contains(screen(out), "evil.example.org is not a host any approved package may open") {
		t.Errorf("no reason shown; last frame:\n%s", lastFrame(out))
	}
}

func TestLoopOpenActionRefusedWhenThePackageIsNotApproved(t *testing.T) {
	opened := stubBrowser(t)
	script := append(enterLine("/docs"), exitKeys()...)
	runPackageWithBehaviour(t, behRun("docs", "open:https://docs.example.com/"),
		map[string]string{"dp": pkgDoc("dp", docsPkg)}, nil, nil, script)
	if got := opened(); len(got) != 0 {
		t.Fatalf("opened %q although the package that names the host was never approved", got)
	}
}

func TestLoopOpenActionWithNoPackageAtAll(t *testing.T) {
	opened := stubBrowser(t)
	script := append(enterLine("/docs"), exitKeys()...)
	runPackageWithBehaviour(t, behRun("docs", "open:https://docs.example.com/"), nil, nil, nil, script)
	if got := opened(); len(got) != 0 {
		t.Fatalf("opened %q with no package granting anything", got)
	}
}

// The host refuses an open: that reaches it from a hook even if validation was skipped.
func TestOpenRefusalNeverAllowsAHook(t *testing.T) {
	act, err := scene.ParseAction("open:https://docs.example.com/")
	if err != nil {
		t.Fatal(err)
	}
	set := packageSet{pkgs: []installedPackage{grantedWithOpen("dp", "docs.example.com")}}
	if why := openRefusal(act, false, set); why != "" {
		t.Fatalf("a key or command was refused: %s", why)
	}
	if why := openRefusal(act, true, set); why == "" {
		t.Fatal("a hook was allowed to open the browser")
	}
}

func TestOpenCommandIsArgvAndNeverAShellLine(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows", "freebsd"} {
		argv := openCommand(goos, "https://a.example.com/?x=1&y=2")
		if argv[len(argv)-1] != "https://a.example.com/?x=1&y=2" {
			t.Errorf("%s: the address is not the last, untouched argument: %q", goos, argv)
		}
		for _, a := range argv[:len(argv)-1] {
			if strings.ContainsAny(a, " ;&|$") && goos != "windows" {
				t.Errorf("%s: %q looks like a shell fragment", goos, a)
			}
		}
	}
}

func TestValidateActionsForSayOpenSet(t *testing.T) {
	ok := []struct {
		list     []string
		allowExt bool
	}{
		{[]string{"say:hi"}, true},
		{[]string{"say:hi"}, false},
		{[]string{"set:effort=high"}, false},
		{[]string{"set:mode=plan"}, true},
		{[]string{"set:style=bar"}, true},
		{[]string{"open:https://docs.example.com/"}, true},
	}
	for _, c := range ok {
		if err := validateActions("x", c.list, c.allowExt, ""); err != nil {
			t.Errorf("%v (allowExt=%v) refused: %v", c.list, c.allowExt, err)
		}
	}
	bad := [][]string{
		{"open:https://docs.example.com/"}, // from a hook
	}
	if err := validateActions("hook", bad[0], false, ""); err == nil || !strings.Contains(err.Error(), "hook") {
		t.Errorf("a hook's open: was accepted: %v", err)
	}
	for _, a := range []string{"set:effort=ludicrous", "set:mode=nope", "set:style=nope", "say:" + strings.Repeat("a", 401)} {
		if err := validateActions("x", []string{a}, true, ""); err == nil {
			t.Errorf("%q accepted", a)
		}
	}
}

func TestLoopSetActionChangesEffort(t *testing.T) {
	seedBehaviour(t, behRun("hard", "set:effort=high"))
	drv := &effortDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}
	script := append(enterLine("/hard"), quitScript...)
	runLoop(t, drv, drv.evCh, script)
	drv.mu.Lock()
	defer drv.mu.Unlock()
	if len(drv.levels) != 1 || drv.levels[0] != "high" {
		t.Errorf("levels = %v, want [high]", drv.levels)
	}
	if len(drv.submitted) != 0 {
		t.Errorf("the command reached the model as %q", drv.submitted)
	}
}

// A set: action in a hook is allowed (it is one of the settings a hook watches) but
// a hook's own change fires no further hook, so two hooks cannot chase each other.
func TestLoopSetInAHookDoesNotFireHooksAgain(t *testing.T) {
	seedBehaviour(t, `{"hooks":[{"on":"effort","is":"high","do":["set:effort=low"]},{"on":"effort","is":"low","do":["say:low reached"]}],`+
		`"commands":[{"name":"hard","description":"d","run":["set:effort=high"]}]}`)
	drv := &effortDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}
	script := append(enterLine("/hard"), quitScript...)
	tty := runLoop(t, drv, drv.evCh, script)
	drv.mu.Lock()
	defer drv.mu.Unlock()
	if strings.Join(drv.levels, ",") != "high,low" {
		t.Errorf("levels = %v, want [high low]", drv.levels)
	}
	if frameWith(tty.output(), "low reached") != "" {
		t.Error("a hook fired another hook")
	}
}

// A press on a scene button carrying say:/open:/set: reaches the loop's queue as the
// action, never as a typed line.
func TestPressQueuesSayOpenSetAsActions(t *testing.T) {
	for _, a := range []string{"say:hi there", "open:https://docs.example.com/", "set:effort=high"} {
		doc := pressDoc(t, `{"root":{"type":"stack","children":[{"id":"b","type":"text","text":"B","on_press":"`+a+`"}]}}`)
		notice := ""
		var queued []string
		handled, _, _ := focusKey(term.Key{Type: term.KeyEnter}, "", "b", &doc, fold.State{}, &notice, map[string]bool{}, nil, nil, nil, nil, func(l string) { queued = append(queued, l) }, context.Background(), &testDriver{})
		if !handled || len(queued) != 1 || queued[0] != rawActionMark+a {
			t.Errorf("%s: handled=%v queued=%q notice=%q", a, handled, queued, notice)
		}
	}
}

// A cmd: line that starts with the raw-action mark cannot pose as an action.
func TestPressCmdCannotCarryTheRawActionMark(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"stack","children":[{"id":"b","type":"text","text":"B","on_press":"cmd:`+strings.Replace(rawActionMark, "\x00", `\u0000`, 1)+`say:x"}]}}`)
	notice := ""
	var queued []string
	focusKey(term.Key{Type: term.KeyEnter}, "", "b", &doc, fold.State{}, &notice, map[string]bool{}, nil, nil, nil, nil, func(l string) { queued = append(queued, l) }, context.Background(), &testDriver{})
	if len(queued) != 0 {
		t.Errorf("queued %q", queued)
	}
}

func grantedWithOpen(id string, hosts ...string) installedPackage {
	return installedPackage{
		Package: &extv2.Package{ID: id, Permissions: extv2.Permissions{Open: hosts}},
		Granted: true,
	}
}
