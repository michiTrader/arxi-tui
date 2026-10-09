//go:build !windows

package term

// read fills buf from the terminal. Off Windows the file read is already the raw bytes,
// and ctrl+z is just 0x1a in them.
func (t *TTY) readInput(buf []byte) (int, error) { return t.in.Read(buf) }
