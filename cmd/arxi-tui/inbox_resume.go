package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// This file answers an approval for a person who never types a command. The core
// can record an answer and carry the run on in one act (`inbox approve|reject
// --resume`); the TUI runs that itself, as its own child, and turns what it
// prints into the two facts the screen needs: the answer is saved, and the run
// went on (or why it could not).

// answerArgs is the argument list of the core's answer command for one item of
// one run.
func answerArgs(kind, item, runID, reason string) []string {
	a := []string{"inbox", kind, item, "--run", runID, "--resume"}
	if kind == "reject" {
		a = append(a, "--reason", reason)
	}
	return a
}

// answerRecorded reports whether a line the core printed says the answer is now
// in the run's log ("approved. backend unblocked (r1 seq 7)").
func answerRecorded(line string) bool {
	for _, w := range []string{"approved.", "rejected.", "replied."} {
		if strings.HasPrefix(line, w) {
			return true
		}
	}
	return false
}

// plainAnswerRefusal turns the core's complaint into one sentence without the
// command-line teaching around it.
func plainAnswerRefusal(stderr string) string {
	var keep []string
	for _, l := range strings.Split(stderr, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case t == "", strings.HasPrefix(t, "usage:"), strings.HasPrefix(t, "see "),
			strings.HasPrefix(t, "see:"), strings.HasPrefix(t, "fix:"), strings.HasPrefix(t, "what ended it"):
			continue
		}
		for _, p := range []string{"arxi inbox approve: ", "arxi inbox reject: ", "arxi inbox: "} {
			t = strings.TrimPrefix(t, p)
		}
		keep = append(keep, t)
	}
	if len(keep) == 0 {
		return "the core did not accept the answer"
	}
	return strings.Join(keep, " ")
}

// runAnswer runs the core's answer command in dir. recorded is called once, as
// soon as the core says the answer is saved; the call returns when the run's
// continuation ends. An error is a plain sentence.
func runAnswer(ctx context.Context, bin, dir string, args []string, recorded func()) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not send the answer: %w", err)
	}
	sc := bufio.NewScanner(stdout)
	saved := false
	for sc.Scan() {
		if !saved && answerRecorded(sc.Text()) {
			saved = true
			if recorded != nil {
				recorded()
			}
		}
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) || stderr.Len() > 0 {
			return errors.New(plainAnswerRefusal(stderr.String()))
		}
		return err
	}
	return nil
}

// AnswerAndResume is serveDriver's half of inboxResumer: it names the run being
// followed, so the person only ever says "approve" or "reject".
func (d *serveDriver) AnswerAndResume(ctx context.Context, kind, item, reason string, recorded func()) error {
	if d.bin == "" {
		return errors.New("this session has no arxi core to send the answer to")
	}
	runID, ok := d.currentRunID()
	if !ok {
		return errors.New("no run is being followed, so there is nothing to answer")
	}
	return runAnswer(ctx, d.bin, d.dir, answerArgs(kind, item, runID, reason), recorded)
}

var _ inboxResumer = (*serveDriver)(nil)
