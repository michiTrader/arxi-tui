//go:build windows

package term

import "time"

// resizePoll is how often Windows is asked how big it is. Windows has no SIGWINCH:
// the console reports resizes through a window event we would have to read from the
// input handle, in the same queue as the keys, which would mean two ways of reading
// the terminal instead of one. A quarter of a second is under the threshold where a
// human notices a redraw lagging behind their mouse, and this file is the only place
// in the program that ticks when nothing is happening.
const resizePoll = 250 * time.Millisecond

func (t *TTY) watchResize() {
	go func() {
		tick := time.NewTicker(resizePoll)
		defer tick.Stop()
		lastW, lastH := t.Size()
		for {
			select {
			case <-t.done:
				return
			case <-tick.C:
				w, h := t.Size()
				if w == lastW && h == lastH {
					continue
				}
				lastW, lastH = w, h
				if !t.emit(Event{Kind: EventResize, Width: w, Height: h}) {
					return
				}
			}
		}
	}()
}
