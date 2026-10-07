package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// This file starts a team run for a person who never types a command. The core's
// protocol cannot start a team (it has no workspace to give it), but its run
// command can, and writes the run's log into ./runs/<id>/events.ndjson. So the TUI
// runs that command itself, reads the run id off its first line, and follows the
// log: the run is then drawn by /flow exactly like any other.
//
// The process is the TUI's own child: it ends with the TUI, and an error it prints
// comes back as a sentence on the screen, never as a command to run.

// runLaunch is what the "Run" form asks for.
type runLaunch struct {
	Team   string
	Task   string
	Budget float64 // USD ceiling; must be above zero
	Model  string  // "" keeps each member's own
	Sim    bool    // a rehearsal: no model is called and nothing is spent
}

// args is the argument list of the core's run command.
func (r runLaunch) args() []string {
	a := []string{"run", "start", r.Team, r.Task, "--budget", strconv.FormatFloat(r.Budget, 'f', -1, 64)}
	if r.Model != "" {
		a = append(a, "--model", r.Model)
	}
	if r.Sim {
		a = append(a, "--sim")
	}
	return a
}

// startedRunID reads the id out of the line the core prints when a run begins:
// "run <id> started (budget ...)".
func startedRunID(line string) (string, bool) {
	f := strings.Fields(line)
	if len(f) >= 3 && f[0] == "run" && f[2] == "started" {
		return f[1], true
	}
	return "", false
}

// plainRefusal turns what the core's run command printed on a refusal into one
// plain sentence: the lines that only teach the command line (usage, "fix: ...",
// "see: ...") are dropped, since the person is not using it.
func plainRefusal(stderr string) string {
	var keep []string
	for _, l := range strings.Split(stderr, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case t == "",
			strings.HasPrefix(t, "usage:"), strings.HasPrefix(t, "short:"),
			strings.HasPrefix(t, "fix:"), strings.HasPrefix(t, "see:"), strings.HasPrefix(t, "note:"),
			strings.HasPrefix(t, "looked for"), strings.HasPrefix(t, "and a stored agent"),
			strings.HasPrefix(t, "what is stored"), strings.HasPrefix(t, "or write it"):
			continue
		}
		keep = append(keep, strings.TrimPrefix(t, "arxi run start: "))
	}
	if len(keep) == 0 {
		return "the core refused to start the run"
	}
	return strings.Join(keep, " ")
}

// launchRun starts the run in dir and returns its id and the path of its event log
// once the log exists. The child is bound to ctx and keeps going after this
// returns: the run lasts as long as the work does. A refusal comes back as an error
// holding a plain sentence.
func launchRun(ctx context.Context, bin, dir string, r runLaunch) (id, logPath string, err error) {
	cmd := exec.CommandContext(ctx, bin, r.args()...)
	cmd.Dir = dir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", "", fmt.Errorf("could not start the run: %w", err)
	}

	// One goroutine owns the pipe: it reports the id when the core announces it,
	// drains the rest, and reaps the process when the run ends. ended closes
	// only after stderr is complete.
	idCh := make(chan string, 1)
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		sc := bufio.NewScanner(stdout)
		sent := false
		for sc.Scan() {
			if sent {
				continue
			}
			if got, ok := startedRunID(sc.Text()); ok {
				idCh <- got
				sent = true
			}
		}
		_ = cmd.Wait()
	}()

	select {
	case id = <-idCh:
	case <-ended:
		return "", "", errors.New(plainRefusal(stderr.String()))
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		return "", "", errors.New("the core did not start the run in time")
	case <-ctx.Done():
		return "", "", ctx.Err()
	}

	logPath = filepath.Join(dir, "runs", id, "events.ndjson")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, statErr := os.Stat(logPath); statErr == nil {
			return id, logPath, nil
		}
		if time.Now().After(deadline) {
			return "", "", fmt.Errorf("run %s started but its log never appeared", id)
		}
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
