//go:build !windows

package chattools

import (
	"context"
	"os/exec"
	"syscall"
)

// ShellName is the shell the run tool uses, for the model to know what syntax to write.
func ShellName() string { return "sh" }

// shellCommand runs the line with sh, in a process group of its own so that a timeout
// stops everything the command started, not only the shell.
func shellCommand(ctx context.Context, line string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "sh", "-c", line)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	return cmd
}
