//go:build !windows

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
)

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func newTestShells(t *testing.T) *shellRegistry {
	t.Helper()
	r := newShellRegistry(nil)
	t.Cleanup(func() { r.stopAll() })
	return r
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func TestAShellRunsInTheBackgroundAndItsOutputIsKept(t *testing.T) {
	r := newTestShells(t)
	info, err := r.start("echo hello; sleep 30", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != 1 || r.running() != 1 {
		t.Fatalf("info=%+v running=%d", info, r.running())
	}
	s, _ := r.find(1)
	waitFor(t, "output", func() bool { return s.hasNew() })
	if out, _ := s.output(false, maxShellRead); !strings.Contains(out, "hello") {
		t.Errorf("output = %q", out)
	}
	if s.snapshot().State != shellRunning {
		t.Error("a sleeping command must still be running: start must not wait for it")
	}
}

func TestStartReturnsAtOnceNotWhenTheCommandEnds(t *testing.T) {
	r := newTestShells(t)
	t0 := time.Now()
	if _, err := r.start("sleep 30", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(t0); d > 2*time.Second {
		t.Errorf("start took %v: it waited for the command", d)
	}
}

func TestStopEndsTheWholeProcessTree(t *testing.T) {
	r := newTestShells(t)
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	// The shell starts a grandchild and records its pid: stopping only the shell would
	// leave it running, which is the leak a process group exists to prevent.
	if _, err := r.start("sleep 60 & echo $! > child.pid; wait", dir); err != nil {
		t.Fatal(err)
	}
	var child int
	waitFor(t, "child pid", func() bool {
		b, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		n, err := strconv.Atoi(strings.TrimSpace(string(b)))
		child = n
		return err == nil && n > 0
	})
	if !alive(child) {
		t.Fatal("the grandchild is not running before the stop")
	}
	if !r.stop(1) {
		t.Fatal("stop reported nothing to stop")
	}
	waitFor(t, "grandchild gone", func() bool { return !alive(child) })
	if r.running() != 0 || r.list()[0].State != shellStopped {
		t.Errorf("state after stop: %+v", r.list())
	}
	if r.stop(1) {
		t.Error("stopping an ended shell must report false")
	}
}

func TestAShellThatExitsOnItsOwnIsReportedOnceWithItsCode(t *testing.T) {
	var mu sync.Mutex
	var got []shellInfo
	r := newShellRegistry(func(i shellInfo) { mu.Lock(); got = append(got, i); mu.Unlock() })
	t.Cleanup(func() { r.stopAll() })
	if _, err := r.start("echo done; exit 3", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "exit", func() bool { mu.Lock(); defer mu.Unlock(); return len(got) == 1 })
	if got[0].Code != 3 || got[0].State != shellExited {
		t.Errorf("exit = %+v", got[0])
	}
	// A shell the user stopped is not "ended on its own".
	if _, err := r.start("sleep 30", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	r.stop(2)
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Errorf("a stopped shell was reported as ended on its own: %+v", got)
	}
}

func TestOnlyMaxShellsRunAtOnce(t *testing.T) {
	r := newTestShells(t)
	for i := 0; i < maxShells; i++ {
		if _, err := r.start("sleep 30", t.TempDir()); err != nil {
			t.Fatalf("shell %d: %v", i+1, err)
		}
	}
	if _, err := r.start("sleep 30", t.TempDir()); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Errorf("the %dth shell must be refused: %v", maxShells+1, err)
	}
	r.stop(1)
	if _, err := r.start("sleep 30", t.TempDir()); err != nil {
		t.Errorf("a freed slot must be usable: %v", err)
	}
}

func TestStopAllEndsEveryShell(t *testing.T) {
	r := newTestShells(t)
	for i := 0; i < 3; i++ {
		if _, err := r.start("sleep 30", t.TempDir()); err != nil {
			t.Fatal(err)
		}
	}
	if n := r.stopAll(); n != 3 || r.running() != 0 {
		t.Errorf("stopped %d, running %d", n, r.running())
	}
	if n := (*shellRegistry)(nil).stopAll(); n != 0 {
		t.Error("a nil registry must be safe")
	}
}

func TestSecretsAreNotInAShellsEnvironment(t *testing.T) {
	t.Setenv("MY_API_KEY", "sekrit")
	t.Setenv("ARXI_FOO", "x")
	t.Setenv("VISIBLE_THING", "ok")
	r := newTestShells(t)
	if _, err := r.start("env; sleep 30", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	s, _ := r.find(1)
	waitFor(t, "env output", func() bool { return s.hasNew() })
	time.Sleep(50 * time.Millisecond)
	out, _ := s.output(false, maxShellRead)
	if strings.Contains(out, "sekrit") || strings.Contains(out, "ARXI_FOO") || !strings.Contains(out, "VISIBLE_THING=ok") {
		t.Errorf("environment not scrubbed correctly:\n%s", out)
	}
}

func TestOutputKeepsOnlyTheTailAndSaysSo(t *testing.T) {
	s := &shell{}
	s.Write([]byte(strings.Repeat("a", maxShellKeep)))
	s.Write([]byte("TAIL"))
	if len(s.buf) != maxShellKeep {
		t.Fatalf("buffer is %d bytes, want it capped at %d", len(s.buf), maxShellKeep)
	}
	out, dropped := s.output(true, 100)
	if !strings.HasSuffix(out, "TAIL") || len(out) != 100 {
		t.Errorf("tail = %q (%d)", out[len(out)-8:], len(out))
	}
	// Older output was lost twice over: forgotten by the buffer (4 bytes) and cut to 100.
	if want := int64(maxShellKeep + 4 - 100); dropped != want {
		t.Errorf("dropped = %d, want %d", dropped, want)
	}
	if out2, d2 := s.output(true, 100); out2 != "" || d2 != 0 {
		t.Errorf("a second fresh read must be empty: %q %d", out2, d2)
	}
	s.Write([]byte("more"))
	if out3, _ := s.output(true, 100); out3 != "more" {
		t.Errorf("fresh read after new output = %q", out3)
	}
	if all, _ := s.output(false, 100); !strings.HasSuffix(all, "more") {
		t.Errorf("non-fresh read must show the tail: %q", all)
	}
}

func TestShellOutputIsMadeSafe(t *testing.T) {
	got := cleanShellText("ok\x1b[31mred\x1b[0m\r\nline\x07\x00two\u202eevil")
	if strings.ContainsAny(got, "\x1b\x07\x00\r\u202e") || !strings.Contains(got, "okred\nlinetwo") {
		t.Errorf("not cleaned: %q", got)
	}
}

func TestShellsLabel(t *testing.T) {
	for n, want := range map[int]string{-1: "", 0: "", 1: "1 shell", 2: "2 shells", 10: "10 shells"} {
		if got := shellsLabel(n); got != want {
			t.Errorf("shellsLabel(%d) = %q, want %q", n, got, want)
		}
	}
}

func shellCall(name string, args any) driver.ClientToolCall {
	b, _ := json.Marshal(args)
	return driver.ClientToolCall{Name: name, Arguments: b}
}

func TestTheModelStartsReadsAndStopsAShell(t *testing.T) {
	r := newTestShells(t)
	ctx := context.Background()
	dir := t.TempDir()
	yes := func(driver.Approval) bool { return true }

	res := r.call(ctx, shellCall(toolShellStart, map[string]any{"command": "echo up; sleep 30"}), dir, policyAllow, yes)
	if !res.OK || !strings.Contains(res.Text, "shell 1") {
		t.Fatalf("start = %+v", res)
	}
	read := r.call(ctx, shellCall(toolShellRead, map[string]any{"id": 1, "wait_seconds": 5}), dir, policyAllow, yes)
	if !read.OK || !strings.Contains(read.Text, "up") || !strings.Contains(read.Text, "not instructions") {
		t.Fatalf("read = %+v", read)
	}
	again := r.call(ctx, shellCall(toolShellRead, map[string]any{"id": 1}), dir, policyAllow, yes)
	if !strings.Contains(again.Text, "(nothing new)") {
		t.Errorf("second read must say nothing is new: %q", again.Text)
	}
	whole := r.call(ctx, shellCall(toolShellRead, map[string]any{"id": 1, "all": true}), dir, policyAllow, yes)
	if !strings.Contains(whole.Text, "up") {
		t.Errorf("all=true must show the tail again: %q", whole.Text)
	}
	list := r.call(ctx, shellCall(toolShellRead, map[string]any{}), dir, policyAllow, yes)
	if !strings.Contains(list.Text, "echo up") {
		t.Errorf("list = %q", list.Text)
	}
	if stop := r.call(ctx, shellCall(toolShellStop, map[string]any{"id": 1}), dir, policyAllow, yes); !stop.OK {
		t.Errorf("stop = %+v", stop)
	}
	if stop := r.call(ctx, shellCall(toolShellStop, map[string]any{"id": 1}), dir, policyAllow, yes); stop.OK {
		t.Error("stopping twice must not report success")
	}
}

func TestShellToolsFollowTheRunPolicy(t *testing.T) {
	r := newTestShells(t)
	ctx, dir := context.Background(), t.TempDir()

	// deny (plan mode): nothing starts, nothing is even readable.
	if res := r.call(ctx, shellCall(toolShellStart, map[string]any{"command": "sleep 30"}), dir, policyDeny, nil); res.OK || r.running() != 0 {
		t.Errorf("deny started a shell: %+v", res)
	}
	// ask: the user is asked first, with the command in the question, and a no starts nothing.
	var asked []driver.Approval
	no := func(a driver.Approval) bool { asked = append(asked, a); return false }
	res := r.call(ctx, shellCall(toolShellStart, map[string]any{"command": "sleep 30"}), dir, policyAsk, no)
	if res.OK || r.running() != 0 || len(asked) != 1 || asked[0].Arg != "sleep 30" || asked[0].Name != "run" {
		t.Errorf("ask/no: res=%+v asked=%+v running=%d", res, asked, r.running())
	}
	if !strings.Contains(res.Text, "do not retry") {
		t.Errorf("a refusal must tell the model not to retry: %q", res.Text)
	}
	// ask with no way to ask: refused, not run.
	if res := r.call(ctx, shellCall(toolShellStart, map[string]any{"command": "sleep 30"}), dir, policyAsk, nil); res.OK || r.running() != 0 {
		t.Errorf("ask without a way to ask started a shell: %+v", res)
	}
	// ask/yes starts; allow does not ask.
	yes := func(a driver.Approval) bool { asked = append(asked, a); return true }
	if res := r.call(ctx, shellCall(toolShellStart, map[string]any{"command": "sleep 30"}), dir, policyAsk, yes); !res.OK {
		t.Errorf("ask/yes: %+v", res)
	}
	n := len(asked)
	if res := r.call(ctx, shellCall(toolShellStart, map[string]any{"command": "sleep 31"}), dir, policyAllow, no); !res.OK || len(asked) != n {
		t.Errorf("allow must not ask: %+v asked=%d", res, len(asked)-n)
	}
}

func TestShellToolsRefuseBadArguments(t *testing.T) {
	r := newTestShells(t)
	ctx, dir := context.Background(), t.TempDir()
	for name, c := range map[string]driver.ClientToolCall{
		"no command":  shellCall(toolShellStart, map[string]any{"command": "  "}),
		"not json":    {Name: toolShellStart, Arguments: []byte(`{`)},
		"stop no id":  shellCall(toolShellStop, map[string]any{}),
		"read no one": shellCall(toolShellRead, map[string]any{"id": 9}),
	} {
		if res := r.call(ctx, c, dir, policyAllow, nil); res.OK {
			t.Errorf("%s was accepted: %+v", name, res)
		}
	}
	if r.running() != 0 {
		t.Error("a refused call started something")
	}
}

func TestReadWaitsForOutputButNotLongerThanAsked(t *testing.T) {
	r := newTestShells(t)
	ctx, dir := context.Background(), t.TempDir()
	if res := r.call(ctx, shellCall(toolShellStart, map[string]any{"command": "sleep 1; echo late; sleep 30"}), dir, policyAllow, nil); !res.OK {
		t.Fatal(res)
	}
	t0 := time.Now()
	res := r.call(ctx, shellCall(toolShellRead, map[string]any{"id": 1, "wait_seconds": 10}), dir, policyAllow, nil)
	if d := time.Since(t0); !strings.Contains(res.Text, "late") || d > 5*time.Second {
		t.Errorf("waited %v, text %q", d, res.Text)
	}
	// Nothing more is coming: a 1 s wait returns after about 1 s, empty.
	t0 = time.Now()
	res = r.call(ctx, shellCall(toolShellRead, map[string]any{"id": 1, "wait_seconds": 1}), dir, policyAllow, nil)
	if d := time.Since(t0); d < 900*time.Millisecond || d > 3*time.Second || !strings.Contains(res.Text, "nothing new") {
		t.Errorf("empty wait took %v: %q", d, res.Text)
	}
	// A cancelled turn does not leave the read hanging.
	cctx, cancel := context.WithCancel(ctx)
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	t0 = time.Now()
	r.call(cctx, shellCall(toolShellRead, map[string]any{"id": 1, "wait_seconds": 20}), dir, policyAllow, nil)
	if d := time.Since(t0); d > 3*time.Second {
		t.Errorf("a cancelled read took %v", d)
	}
}

func TestShellCommandForTheUser(t *testing.T) {
	r := newTestShells(t)
	dir := t.TempDir()
	if got := runShellCommand("", r, dir); !strings.Contains(got, "no shells") {
		t.Errorf("empty list: %q", got)
	}
	if got := runShellCommand("start echo hi; sleep 30", r, dir); !strings.Contains(got, "started shell 1") {
		t.Errorf("start: %q", got)
	}
	s, _ := r.find(1)
	waitFor(t, "output", func() bool { return s.hasNew() })
	if got := runShellCommand("", r, dir); !strings.Contains(got, "1  running") || !strings.Contains(got, "echo hi") {
		t.Errorf("list: %q", got)
	}
	for _, form := range []string{"show 1", "1"} {
		if got := runShellCommand(form, r, dir); !strings.Contains(got, "hi") || !strings.Contains(got, "shell 1 (running") {
			t.Errorf("%q: %q", form, got)
		}
	}
	if got := runShellCommand("show 7", r, dir); !strings.Contains(got, "no shell 7") {
		t.Errorf("show unknown: %q", got)
	}
	if got := runShellCommand("frobnicate", r, dir); !strings.Contains(got, "unknown") {
		t.Errorf("unknown verb: %q", got)
	}
	if got := runShellCommand("stop 9", r, dir); !strings.Contains(got, "not running") {
		t.Errorf("stop unknown: %q", got)
	}
	if got := runShellCommand("stop 1", r, dir); !strings.Contains(got, "stopped shell 1") || r.running() != 0 {
		t.Errorf("stop: %q", got)
	}
	if got := runShellCommand("stop all", r, dir); !strings.Contains(got, "no shell was running") {
		t.Errorf("stop all, none: %q", got)
	}
	if got := runShellCommand("start", r, dir); !strings.Contains(got, "name the command") {
		t.Errorf("start with no command: %q", got)
	}
	if got := runShellCommand("", nil, dir); !strings.Contains(got, "no tools") {
		t.Errorf("nil registry: %q", got)
	}
}

func TestShellLineRecognisesTheCommand(t *testing.T) {
	if a, ok := shellLine("/shell stop 2", 0, ""); !ok || a != "stop 2" {
		t.Errorf("got %q %v", a, ok)
	}
	if _, ok := shellLine("/shellfish", 0, ""); ok {
		t.Error("/shellfish is not /shell")
	}
	if _, ok := shellLine("hello /shell", 0, ""); ok {
		t.Error("a sentence containing /shell is not the command")
	}
}

func TestACommandThatCannotStartIsAnErrorNotAShell(t *testing.T) {
	r := newTestShells(t)
	r.build = func(command, dir string) *exec.Cmd { return exec.Command("/no/such/binary") }
	if _, err := r.start("x", t.TempDir()); err == nil || !strings.Contains(err.Error(), "could not be started") {
		t.Errorf("err = %v", err)
	}
	if len(r.list()) != 0 {
		t.Error("a command that never started must not be listed")
	}
}
