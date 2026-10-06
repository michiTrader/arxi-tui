//go:build !windows

package term

// Only the Windows console is shared with child processes in a way that can undo raw
// mode behind our back; a Unix tty keeps its termios until we change it.
func (t *TTY) wantConsole() {}
func (t *TTY) reassert()    {}
