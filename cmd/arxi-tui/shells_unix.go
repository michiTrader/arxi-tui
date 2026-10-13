//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// shellCommand is the command line a background shell runs, in a process group of its
// own so that stopping it stops everything it started.
func shellCommand(command, dir string) *exec.Cmd {
	cmd := exec.Command("sh", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

// stopTree asks the whole group to end (SIGTERM), or force-kills it.
func stopTree(cmd *exec.Cmd, force bool) {
	if cmd.Process == nil {
		return
	}
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	_ = syscall.Kill(-cmd.Process.Pid, sig)
}
