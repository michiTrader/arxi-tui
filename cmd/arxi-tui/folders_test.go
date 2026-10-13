package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// folderChat is a core that, on its first turn, asks to open a folder, as a core does
// when the model names a path outside the project.
type folderChat struct {
	mu       sync.Mutex
	outside  []string
	folders  [][]string
	asked    bool
	approved chan bool
}

func (*folderChat) Hello() *driver.Hello { return &driver.Hello{Implemented: []string{"chat.send"}} }

func (c *folderChat) SubmitChatSend(ctx context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error) {
	c.mu.Lock()
	c.outside = append(c.outside, p.Outside)
	c.folders = append(c.folders, append([]string(nil), p.Folders...))
	first := !c.asked
	c.asked = true
	c.mu.Unlock()
	if first && p.OnApproval != nil && p.Outside == "ask" {
		c.approved <- p.OnApproval(ctx, driver.Approval{CallID: "c1", Name: "folder", Arg: "/other/proj", Summary: "open"})
	}
	return &driver.ChatSendResult{Text: "done"}, nil
}

func TestEveryModeSaysWhatToDoAboutAFolderBesideTheProject(t *testing.T) {
	want := map[string]string{"ask": policyAsk, "auto": policyAsk, "plan": policyAsk, "full access": policyAllow}
	for _, m := range agentModes {
		if got := m.policy(classFolder); got != want[m.name] {
			t.Errorf("mode %q: folder policy %q, want %q", m.name, got, want[m.name])
		}
	}
}

func TestAFolderTheUserOpensRidesWithTheNextTurnAndClearClosesIt(t *testing.T) {
	out := make(chan fold.Event, 64)
	core := &folderChat{approved: make(chan bool, 1)}
	c := newChatSession(core, out)
	c.setWorkdir("/proj")
	c.setOutside("ask")
	if err := c.send(context.Background(), "look at the other project"); err != nil {
		t.Fatal(err)
	}
	wait(t, c.pendingNow)
	c.decide(true)
	if !<-core.approved {
		t.Fatal("the core was told no")
	}
	wait(t, func() bool { return !c.busyNow() })
	if got := c.openFolders(); len(got) != 1 || got[0] != "/other/proj" {
		t.Fatalf("folders after agreeing = %v", got)
	}
	if err := c.send(context.Background(), "and again"); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return !c.busyNow() })
	core.mu.Lock()
	second := core.folders[1]
	core.mu.Unlock()
	if len(second) != 1 || second[0] != "/other/proj" {
		t.Errorf("the next turn carried %v", second)
	}
	c.reset()
	if len(c.openFolders()) != 0 {
		t.Error("/clear left a folder open")
	}
}

func TestDecliningAFolderDoesNotOpenIt(t *testing.T) {
	core := &folderChat{approved: make(chan bool, 1)}
	c := newChatSession(core, make(chan fold.Event, 64))
	c.setWorkdir("/proj")
	c.setOutside("ask")
	if err := c.send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	wait(t, c.pendingNow)
	c.decide(false)
	<-core.approved
	wait(t, func() bool { return !c.busyNow() })
	if len(c.openFolders()) != 0 {
		t.Errorf("a declined folder was kept: %v", c.openFolders())
	}
}

func TestADeniedPolicyIsNotSentToTheCore(t *testing.T) {
	core := &folderChat{approved: make(chan bool, 1)}
	c := newChatSession(core, make(chan fold.Event, 64))
	c.setWorkdir("/proj")
	c.setOutside(policyDeny)
	_ = c.send(context.Background(), "go")
	wait(t, func() bool { return !c.busyNow() })
	if core.outside[0] != "" {
		t.Errorf("deny was sent as %q: an older core would refuse the whole request", core.outside[0])
	}
}

type fakeFolders struct{ dirs []string }

func (f *fakeFolders) OpenFolder(d string) (string, error) { f.dirs = append(f.dirs, d); return d, nil }
func (f *fakeFolders) CloseFolder(d string) int {
	if d == "" {
		n := len(f.dirs)
		f.dirs = nil
		return n
	}
	n := 0
	keep := f.dirs[:0:0]
	for _, x := range f.dirs {
		if x == d {
			n++
		} else {
			keep = append(keep, x)
		}
	}
	f.dirs = keep
	return n
}
func (f *fakeFolders) Folders() []string { return f.dirs }

func TestFolderCommandListsAddsAndRemoves(t *testing.T) {
	base := t.TempDir()
	other := filepath.Join(base, "other")
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(other)
	fm := &fakeFolders{}
	if s := runFolderCommand("", fm, base); !strings.Contains(s, "only the project") {
		t.Errorf("empty list: %q", s)
	}
	if s := runFolderCommand("add other", fm, base); !strings.Contains(s, real) || len(fm.dirs) != 1 || fm.dirs[0] != real {
		t.Errorf("add relative: %q %v", s, fm.dirs)
	}
	if s := runFolderCommand("", fm, base); !strings.Contains(s, real) {
		t.Errorf("list: %q", s)
	}
	for _, bad := range []string{"add", "add nope", "add " + string(filepath.Separator)} {
		if s := runFolderCommand(bad, fm, base); !strings.Contains(s, "/folder:") || len(fm.dirs) != 1 {
			t.Errorf("%q should be refused: %q %v", bad, s, fm.dirs)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if s := runFolderCommand("add "+home, fm, base); len(fm.dirs) != 1 || !strings.Contains(s, "too wide") {
			t.Errorf("home was opened: %q", s)
		}
	}
	if s := runFolderCommand("remove other", fm, base); len(fm.dirs) != 0 || !strings.Contains(s, "closed") {
		t.Errorf("remove: %q %v", s, fm.dirs)
	}
	if s := runFolderCommand("remove all", fm, base); !strings.Contains(s, "no folder") {
		t.Errorf("remove all on empty: %q", s)
	}
	if s := runFolderCommand("frobnicate", fm, base); !strings.Contains(s, "unknown") {
		t.Errorf("unknown verb: %q", s)
	}
}

func TestFolderLineRecognisesTheCommand(t *testing.T) {
	if a, ok := folderLine("/folder add x", 0, ""); !ok || a != "add x" {
		t.Errorf("got %q %v", a, ok)
	}
	if _, ok := folderLine("/foldery", 0, ""); ok {
		t.Error("/foldery is not /folder")
	}
	if _, ok := folderLine("hello", 0, ""); ok {
		t.Error("plain text is not /folder")
	}
}
