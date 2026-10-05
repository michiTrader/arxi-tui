//go:build windows

package chattools

import (
	"context"
	"os/exec"
	"strconv"
	"syscall"
)

// ShellName is the shell the run tool uses, for the model to know what syntax to write.
func ShellName() string { return "cmd.exe" }

// shellCommand runs the line with cmd.exe. The line is handed over verbatim (Go's
// usual argument quoting is not what cmd.exe parses), and a timeout stops the whole
// tree with taskkill rather than only cmd.exe.
func shellCommand(ctx context.Context, line string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /d /s /c "` + line + `"`}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	}
	return cmd
}
