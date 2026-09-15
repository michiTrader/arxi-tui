//go:build !windows

package term

import (
	"os"
	"os/signal"
	"syscall"
)

// watchResize turns SIGWINCH into an event.
//
// This is the whole reason the resize path is two files. On a Unix terminal — which
// on a phone means Termux — a window nobody is resizing costs exactly nothing: the
// process sleeps in a select and wakes when the kernel says the size changed. A
// 250 ms poll would have been one file and would have quietly broken the promise
// that an idle frame with no animated widget wakes up zero times per second, on the
// platform where that promise is measured in battery.
func (t *TTY) watchResize() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		defer signal.Stop(ch)
		for {
			select {
			case <-t.done:
				return
			case <-ch:
				w, h := t.Size()
				if !t.emit(Event{Kind: EventResize, Width: w, Height: h}) {
					return
				}
			}
		}
	}()
}
