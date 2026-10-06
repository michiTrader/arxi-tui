//go:build windows

package term

import "golang.org/x/sys/windows"

// The Windows console is shared with every process attached to it. A child that the
// model's run tool starts (cmd.exe) can change the console modes and put back what it
// found rather than what we had set. The result the user sees is the terminal echoing
// keys as caret notation (^[[A, ^[[<0;10;5M) into the input line and the status bar,
// because virtual-terminal input is off and echo is on again. The run tool now gives
// its child a console of its own, and this is the belt to that pair of braces.
//
// So the modes are not set once. Raw records the ones it wants and reassert, called
// from the resize poll that already ticks four times a second on this platform, puts
// them back whenever they have drifted. Reading a console mode is a cheap system call
// and the common case is "unchanged".

// consoleWant is what this process needs from the two console handles.
type consoleWant struct {
	in, out     windows.Handle
	inOn, inOff uint32
	outOn       uint32
}

func (t *TTY) wantConsole() {
	w := &consoleWant{
		in:    windows.Handle(t.in.Fd()),
		out:   windows.Handle(t.out.Fd()),
		inOn:  windows.ENABLE_VIRTUAL_TERMINAL_INPUT,
		inOff: windows.ENABLE_ECHO_INPUT | windows.ENABLE_LINE_INPUT | windows.ENABLE_PROCESSED_INPUT,
		outOn: windows.ENABLE_PROCESSED_OUTPUT | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING,
	}
	t.console = w
	t.reassert()
}

// reassert returns the console to the raw modes if anything changed them.
func (t *TTY) reassert() {
	w, _ := t.console.(*consoleWant)
	if w == nil || t.state == nil {
		return
	}
	var m uint32
	if windows.GetConsoleMode(w.in, &m) == nil {
		if want := (m | w.inOn) &^ w.inOff; want != m {
			_ = windows.SetConsoleMode(w.in, want)
		}
	}
	if windows.GetConsoleMode(w.out, &m) == nil {
		if want := m | w.outOn; want != m {
			_ = windows.SetConsoleMode(w.out, want)
		}
	}
}
