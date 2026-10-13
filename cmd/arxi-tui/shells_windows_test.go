//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The Linux tests of the shells need `sh`; these are their Windows counterparts, so the
// code in shells_windows.go (cmd.exe, CREATE_NO_WINDOW, taskkill /T) runs in CI and is not
// only compiled.

func winWaitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 1000; i++ {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func winAlive(pid int) bool {
	out, err := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/NH").Output()
	return err == nil && strings.Contains(string(out), strconv.Itoa(pid))
}

func TestWindowsAShellRunsInTheBackgroundAndItsOutputIsKept(t *testing.T) {
	r := newShellRegistry(nil)
	t.Cleanup(func() { r.stopAll() })
	t0 := time.Now()
	info, err := r.start("echo hello-from-shell & ping -n 30 127.0.0.1 >nul", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(t0); d > 5*time.Second {
		t.Errorf("start took %v: it waited for the command", d)
	}
	if info.ID != 1 || r.running() != 1 {
		t.Fatalf("info=%+v running=%d", info, r.running())
	}
	s, _ := r.find(1)
	winWaitFor(t, "output", func() bool { return s.hasNew() })
	if out, _ := s.output(false, maxShellRead); !strings.Contains(out, "hello-from-shell") {
		t.Errorf("output = %q", out)
	}
	if s.snapshot().State != shellRunning {
		t.Error("a waiting command must still be running")
	}
}

// Stopping the shell must end what it started too, or a dev server would outlive /shell stop.
func TestWindowsStopEndsTheWholeProcessTree(t *testing.T) {
	r := newShellRegistry(nil)
	t.Cleanup(func() { r.stopAll() })
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	// powershell starts a grandchild (a long ping) and writes its pid.
	cmd := `powershell -NoProfile -Command "$p = Start-Process -PassThru -WindowStyle Hidden ping -ArgumentList '-n','60','127.0.0.1'; $p.Id | Out-File -Encoding ascii child.pid; Wait-Process -Id $p.Id"`
	if _, err := r.start(cmd, dir); err != nil {
		t.Fatal(err)
	}
	var child int
	winWaitFor(t, "child pid", func() bool {
		b, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		n, err := strconv.Atoi(strings.TrimSpace(string(b)))
		child = n
		return err == nil && n > 0
	})
	if !winAlive(child) {
		t.Fatal("the grandchild is not running before the stop")
	}
	if !r.stop(1) {
		t.Fatal("stop reported nothing to stop")
	}
	winWaitFor(t, "grandchild gone", func() bool { return !winAlive(child) })
	if r.running() != 0 || r.list()[0].State != shellStopped {
		t.Errorf("state after stop: %+v", r.list())
	}
}

func TestWindowsAShellThatExitsOnItsOwnIsReportedWithItsCode(t *testing.T) {
	got := make(chan shellInfo, 1)
	r := newShellRegistry(func(i shellInfo) { got <- i })
	t.Cleanup(func() { r.stopAll() })
	if _, err := r.start("exit 3", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	select {
	case i := <-got:
		if i.Code != 3 || i.State != shellExited {
			t.Errorf("info = %+v, want exit code 3", i)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the end of the shell was not reported")
	}
}
