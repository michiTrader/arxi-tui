package term

import (
	"os"
	"strings"
)

// IsTermux reports whether the terminal drawing this session is Termux's own view on
// Android. Its answer sets two phone defaults, and they are one trade. The conversation is
// drawn on the main screen, where a swipe is Termux's own scrollback moving the transcript
// instead of the shell's history; and the mouse is left to the terminal, because tracking
// turns the tap that shows Android's keyboard back into a mouse report, and no sequence asks
// for the keyboard after that. Flags and the config file can still choose either differently.
//
// TERMUX_VERSION is the app's own signal, exported by the process that draws the view. PREFIX
// under com.termux is the bootstrap's path, which nothing else has a reason to name, and it is
// the fallback for a launcher or a version that does not export the first.
//
// An ssh session is not this terminal, in either direction, and the guard is for the direction
// that would otherwise be wrong: Termux's sshd inherits TERMUX_VERSION from the session it was
// started in and passes it to every login, so a laptop connected to a phone would have
// drag-to-select taken off a mouse that has one. The other direction — ssh out of Termux into
// a machine that runs this — is a miss rather than a mistake, and so is a proot distro with an
// environment of its own. -mouse answers both, which is what the flag has always been for.
func IsTermux() bool {
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" {
		return false
	}
	if os.Getenv("TERMUX_VERSION") != "" {
		return true
	}
	return strings.Contains(os.Getenv("PREFIX"), "com.termux")
}
