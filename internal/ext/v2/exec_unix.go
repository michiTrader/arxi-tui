//go:build !windows

package extv2

import (
	"os/exec"
	"syscall"
)

// prepareChild puts the child in its own process group, so a timeout can stop the
// program and every helper it started. Stopping only the program would leave an
// orphan running after the host reported that it was stopped.
func prepareChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killChild(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
