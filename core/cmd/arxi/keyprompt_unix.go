//go:build !windows

package main

import (
	"os"
	"syscall"
)

// echoOff switches terminal echo off and returns the function that restores it.
// It reuses the termios helpers the design screen already has, rather than
// pulling in a terminal library: the core takes no third-party dependencies.
func echoOff(f *os.File) (func(), error) {
	fd := int(f.Fd())
	before, err := ioctlTermios(fd, tcGetAttr)
	if err != nil {
		return nil, err
	}
	quiet := *before
	quiet.Lflag &^= syscall.ECHO
	if err := setTermios(fd, &quiet); err != nil {
		return nil, err
	}
	return func() { setTermios(fd, before) }, nil
}
