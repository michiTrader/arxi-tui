//go:build !windows

package supervisor

import (
	"os/exec"
	"syscall"
)

// The process-group mechanics are ported by copy from arxi-sim (ADR-0001),
// never imported: the mechanics are the part worth inheriting, and importing a
// sibling project's internal package would pull its whole dependency surface
// across an arch seam this project keeps closed.
//
// Setpgid puts the child in its own process group whose id equals the child's
// pid. A plugin that spawns helpers of its own therefore has them all in that
// one group, so signalling the negative pid (-pid, "the whole group") reaches
// the grandchildren too. Signalling only cmd.Process.Pid would leave an
// orphaned grandchild running after the host thinks the plugin is gone — the
// exact leak the escape hatch cannot afford, because a plugin the user unmounted
// must actually stop.

func prepareProcess(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return nil
}

// attachProcess and releaseProcess are the Windows Job Object hooks; on unix the
// process group is established by prepareProcess before Start, so there is
// nothing to attach after and nothing to release.
func attachProcess(*exec.Cmd) error { return nil }
func releaseProcess(*exec.Cmd)      {}

// terminateProcess asks the whole group to exit (SIGTERM), the graceful step
// before the deadline kill. A plugin that installs no handler still dies; one
// that does gets a chance to flush.
func terminateProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
}

// killProcessTree is the unconditional kill of the whole group (SIGKILL to
// -pid). It is what Close falls back to when the graceful deadline passes, so an
// unmount can never hang on a plugin that ignores SIGTERM.
func killProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
