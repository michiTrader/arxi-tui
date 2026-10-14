package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// shutdownSignals are the ways a terminal program is asked to end that it can still
// answer: SIGTERM (kill, a service manager) and SIGHUP (the terminal window closed or
// the ssh session dropped). Ctrl-C is not here: the screen is in raw mode, so it
// arrives as a key and has its own double-press gesture. SIGKILL cannot be caught.
// os.Interrupt covers a SIGINT sent from outside, and is what Windows delivers for a
// console Ctrl-Break.
var shutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}

// shutdownContext is the context the program runs under. It ends when the program is
// asked to end by one of shutdownSignals, which lets run return normally: the deferred
// cleanups then restore the terminal and stop the background shells (and everything
// they started). Without it those signals killed the process outright, and a dev server
// started with /shell or by the model kept running with its port taken.
func shutdownContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, shutdownSignals...)
}
