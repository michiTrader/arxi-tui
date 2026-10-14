//go:build !windows

package main

import (
	"context"
	"syscall"
	"testing"
	"time"
)

func TestTerminalSignalsEndTheProgramContext(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT} {
		ctx, cancel := shutdownContext(context.Background())
		if err := syscall.Kill(syscall.Getpid(), sig); err != nil {
			cancel()
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
			cancel()
			t.Fatalf("%v did not end the context: the program would be killed without stopping its shells", sig)
		}
		cancel()
	}
}

func TestShutdownContextStaysOpenWithoutASignal(t *testing.T) {
	ctx, cancel := shutdownContext(context.Background())
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatal("the context ended with no signal")
	case <-time.After(50 * time.Millisecond):
	}
}
