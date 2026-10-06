package chattools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// This file is the part of the toolbox that runs a command. It is the most powerful
// tool the model can be given: whatever the user can do in a terminal, it can do. So
// it is separate from the file tools, off unless the caller turns it on, and meant to
// be put to the user first: Preview says what would run, and in what folder, without
// starting anything.
//
// What the guard does and does not do. The command starts in the project folder, gets
// no standard input (a program that waits for a keyboard would otherwise hang the
// turn), is stopped after a time limit together with everything it started, and its
// environment is stripped of anything that looks like a secret, because what a command
// prints goes to a remote provider. It does NOT confine the command to the project: a
// command may name any path, so the user's approval is the boundary, not this code.

// ToolRun is the name of the tool, as the model calls it.
const ToolRun = "run"

// Limits for a command.
const (
	defaultRunSeconds = 120
	maxRunSeconds     = 600
	// maxRunCapture is the most output kept from one command; the rest is dropped.
	maxRunCapture = 1 << 20
	// maxRunText is how much of it goes back to the model: the start and the end,
	// because a failure is usually at the end and the cause at the start.
	maxRunHead = 8 << 10
	maxRunTail = 16 << 10
)

// Runs reports whether a tool starts a command.
func Runs(name string) bool { return name == ToolRun }

// RunDefinitions lists the tool that runs commands.
func RunDefinitions() []Definition {
	return []Definition{
		{ToolRun, "Run a " + ShellName() + " command in the project folder; returns its output and exit code. It has no keyboard. The user approves each command.",
			[]byte(`{"type":"object","properties":{"command":{"type":"string"},"timeout_seconds":{"type":"integer","description":"Default 120, max 600."}},"required":["command"]}`)},
	}
}

// WithRuns returns a toolbox on the same folder whose run tool works. Without it the
// tool refuses, whatever the model asks.
func (t *Toolbox) WithRuns() *Toolbox {
	c := *t
	c.runs = true
	return &c
}

// PreviewRun says what a call of run would do, without starting anything. The error is
// the same one RunCommand would give for a call that cannot even start.
func (t *Toolbox) PreviewRun(rawArgs []byte) (Result, error) {
	cmd, _, err := runArgs(rawArgs)
	if err != nil {
		return Result{}, err
	}
	return Result{Arg: cmd, Summary: "in " + t.root}, nil
}

func runArgs(rawArgs []byte) (command string, seconds int, err error) {
	args, err := decodeArgs(rawArgs)
	if err != nil {
		return "", 0, err
	}
	command = strings.TrimSpace(str(args, "command"))
	if command == "" {
		return "", 0, errors.New("run needs a command")
	}
	seconds = num(args, "timeout_seconds")
	switch {
	case seconds <= 0:
		seconds = defaultRunSeconds
	case seconds > maxRunSeconds:
		seconds = maxRunSeconds
	}
	return command, seconds, nil
}

// runCommand starts the command and waits for it. A command that ran and failed is not
// an error here: its output and exit code are the result, and Failed says it did not
// succeed. An error is a command that could not be started.
func (t *Toolbox) runCommand(rawArgs []byte) (Result, error) {
	command, seconds, err := runArgs(rawArgs)
	if err != nil {
		return Result{}, err
	}
	res := Result{Arg: command}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds)*time.Second)
	defer cancel()

	cmd := shellCommand(ctx, command)
	cmd.Dir = t.root
	cmd.Env = scrubEnv(os.Environ())
	cmd.WaitDelay = 3 * time.Second
	out := &capture{max: maxRunCapture}
	cmd.Stdout, cmd.Stderr = out, out
	started := time.Now()
	err = cmd.Run()
	took := time.Since(started).Round(100 * time.Millisecond)

	body := trimOutput(out.buf.Bytes(), out.dropped)
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		res.Failed = true
		res.Summary = fmt.Sprintf("Stopped after %ds", seconds)
		res.Text = fmt.Sprintf("The command did not finish within %d seconds and was stopped.\n%s", seconds, body)
	case err == nil:
		res.Summary = "Exit 0 in " + took.String()
		res.Text = withBody("exit code 0", body)
	default:
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			return res, fmt.Errorf("the command could not be started: %v", err)
		}
		res.Failed = true
		res.Summary = fmt.Sprintf("Exit %d in %s", ee.ExitCode(), took)
		res.Text = withBody(fmt.Sprintf("exit code %d", ee.ExitCode()), body)
	}
	return res, nil
}

func withBody(head, body string) string {
	if body == "" {
		return head + "\n(no output)"
	}
	return head + "\n" + body
}

// capture keeps the first max bytes written to it and counts the rest.
type capture struct {
	buf     bytes.Buffer
	max     int
	dropped int
}

func (c *capture) Write(p []byte) (int, error) {
	room := c.max - c.buf.Len()
	if room > len(p) {
		room = len(p)
	}
	if room > 0 {
		c.buf.Write(p[:room])
	}
	c.dropped += len(p) - room
	return len(p), nil
}

// trimOutput shortens output to what the model should read: the start and the end,
// with a line saying how much was left out in between.
func trimOutput(b []byte, dropped int) string {
	s := strings.ToValidUTF8(string(b), "")
	s = strings.TrimRight(s, "\r\n ")
	if len(s) > maxRunHead+maxRunTail {
		cut := len(s) - maxRunHead - maxRunTail
		s = strings.ToValidUTF8(s[:maxRunHead], "") +
			fmt.Sprintf("\n[... %d bytes of output left out ...]\n", cut+dropped) +
			strings.ToValidUTF8(s[len(s)-maxRunTail:], "")
	} else if dropped > 0 {
		s += fmt.Sprintf("\n[... %d bytes of output left out ...]", dropped)
	}
	return s
}

// scrubEnv removes from a command's environment anything that looks like a secret: the
// command's output goes to a remote provider, and `env` is the first thing a curious
// model runs.
func scrubEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			name = kv[:i]
		}
		up := strings.ToUpper(name)
		secret := strings.HasPrefix(up, "ARXI_")
		for _, w := range []string{"KEY", "TOKEN", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL"} {
			if strings.Contains(up, w) {
				secret = true
			}
		}
		if !secret {
			out = append(out, kv)
		}
	}
	return out
}
