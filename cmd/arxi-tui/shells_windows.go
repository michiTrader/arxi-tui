//go:build windows

package main

import (
	"os/exec"
	"strconv"
	"syscall"
)

// shellCommand is the command line a background shell runs. CREATE_NO_WINDOW gives it
// no console window of its own (the output is read inside the TUI) and keeps cmd.exe
// from sharing the TUI's console and changing its input modes.
func shellCommand(command, dir string) *exec.Cmd {
	cmd := exec.Command("cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:       `cmd.exe /d /s /c "` + command + `"`,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	return cmd
}

// stopTree ends the command and everything it started; a console program ignores a
// polite close, so it is always forced.
func stopTree(cmd *exec.Cmd, _ bool) {
	if cmd.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
}
