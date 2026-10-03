//go:build windows

package main

import (
	"errors"
	"os"
)

// echoOff is unavailable here: hiding console input needs the Windows console
// API, and the core takes no dependencies. stdinKey answers by asking for the
// key to be piped in; the TUI's /login screen is the normal way to enter one.
func echoOff(*os.File) (func(), error) {
	return nil, errors.New("echo cannot be switched off")
}
