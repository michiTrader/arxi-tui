package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Automations only fire while something is watching the clock. The core's scheduler
// is a command that loops forever, so the TUI runs it itself as an invisible child:
// nothing is printed, nothing has to be typed, and it ends with the TUI. That is also
// the honest limit of the feature, and /auto says so.

// schedulerInterval is how often the scheduler looks for a due automation. The core
// refuses anything more frequent than a minute, so half of that never misses one.
const schedulerInterval = "30s"

// triggersDir is where the core keeps automations, relative to the working folder.
const triggersDir = "triggers"

// schedulerProc is the child process, if any. The zero value is ready to use.
type schedulerProc struct {
	mu      sync.Mutex
	running bool
	why     string // why it last stopped on its own; "" when it never did
}

// ensure starts the scheduler unless it is already going. The child is bound to ctx:
// it stops when the TUI does.
func (s *schedulerProc) ensure(ctx context.Context, bin, dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	if bin == "" {
		return errors.New("this session has no arxi core to run automations with")
	}
	cmd := exec.CommandContext(ctx, bin, "trigger", "run", "--interval", schedulerInterval)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stdout = nil // the scheduler's report is not for this screen
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		s.why = "the scheduler could not start: " + err.Error()
		return errors.New(s.why)
	}
	s.running, s.why = true, ""
	go func() {
		err := cmd.Wait()
		s.mu.Lock()
		defer s.mu.Unlock()
		s.running = false
		if ctx.Err() != nil {
			return
		}
		switch {
		case strings.TrimSpace(stderr.String()) != "":
			s.why = plainRefusal(stderr.String())
		case err != nil:
			s.why = "the scheduler stopped: " + err.Error()
		default:
			s.why = "the scheduler stopped"
		}
	}()
	return nil
}

// state reports whether the scheduler is going and, if it stopped by itself, why.
func (s *schedulerProc) state() (running bool, why string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running, s.why
}

// autoScheduler is a driver that can keep the scheduler going. Only the live
// connection to the core can.
type autoScheduler interface {
	EnsureScheduler(ctx context.Context) error
	SchedulerState() (running bool, why string)
}

// EnsureScheduler starts the scheduler in the folder runs are started in.
func (d *serveDriver) EnsureScheduler(ctx context.Context) error {
	return d.sched.ensure(ctx, d.bin, d.dir)
}

// SchedulerState reports the scheduler's state.
func (d *serveDriver) SchedulerState() (bool, string) { return d.sched.state() }

// activeTriggers counts the automations stored under root that are not paused. A
// file that does not read is not counted: the core will say what is wrong with it.
func activeTriggers(root string) int {
	entries, err := os.ReadDir(filepath.Join(root, triggersDir))
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, triggersDir, e.Name()))
		if err != nil {
			continue
		}
		var r struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(b, &r) == nil && r.Status == "active" {
			n++
		}
	}
	return n
}
