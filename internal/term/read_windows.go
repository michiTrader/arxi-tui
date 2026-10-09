//go:build windows

package term

import (
	"io"

	"golang.org/x/sys/windows"
)

// readInput reads the console without the standard library's rule that 0x1a is the end
// of the file (see utf16Carry): with it, ctrl+z made the read report io.EOF and the
// program left. When stdin is not a console (a pipe in a test) the plain read is right.
func (t *TTY) readInput(buf []byte) (int, error) {
	h := windows.Handle(t.in.Fd())
	var mode uint32
	if windows.GetConsoleMode(h, &mode) != nil {
		return t.in.Read(buf)
	}
	for {
		units := make([]uint16, 1024)
		var n uint32
		if err := windows.ReadConsole(h, &units[0], uint32(len(units)), &n, nil); err != nil {
			return 0, err
		}
		if n == 0 {
			return 0, io.EOF
		}
		b := t.carry.bytes(units[:n])
		if len(b) == 0 {
			continue // a lone high surrogate: wait for its partner
		}
		return copy(buf, b), nil
	}
}
