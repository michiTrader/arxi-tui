//go:build windows

package extv2

import "os/exec"

// prepareChild has nothing to arrange on Windows: the process tree is stopped through
// the process itself. A helper the program started may outlive a timeout here; the
// supervisor package uses a job object for plugins that run for a long time, and an
// operation bounded to a few seconds does not justify the same machinery.
func prepareChild(cmd *exec.Cmd) {}

func killChild(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
