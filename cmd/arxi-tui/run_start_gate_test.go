package main

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/driver"
)

// requireRunStart is the gate that decides, from the hello the core sent,
// whether this build can begin a run. These tests pin the three refused states
// apart and the one accepting state, because M2's live round-trip commits to
// resolving params and following a log only after this gate passes -- a gate
// that accepted a kernel without a run.start executor would hang the TUI on a
// log no run ever writes.

// TestRequireRunStartAcceptsAnImplementingKernel is the accepting state: a
// hello whose implemented list carries run.start passes, so openServeDriver may
// go on to start and follow the run.
func TestRequireRunStartAcceptsAnImplementingKernel(t *testing.T) {
	hello := &driver.Hello{
		Type:        "hello",
		Types:       []string{"run.prompt", "run.steer", "run.start", "schema"},
		Implemented: []string{"run.start", "run.attach", "run.cancel", "schema"},
	}
	if err := requireRunStart(hello); err != nil {
		t.Fatalf("requireRunStart refused a kernel that implements run.start: %v; "+
			"a run.start in the implemented list is exactly what the TUI needs to begin a run", err)
	}
}

// TestRequireRunStartRefusesDeclaredButUnimplemented is the M1b state made a
// gate: run.start is in `types` but not in `implemented` (as run.prompt and
// run.steer really are on this build). Trusting `types` here is the exact bug
// M1b paid for -- the send would succeed and the run would never exist -- so
// the gate must refuse and name that the verb answers not_implemented.
func TestRequireRunStartRefusesDeclaredButUnimplemented(t *testing.T) {
	hello := &driver.Hello{
		Type:        "hello",
		Types:       []string{"run.prompt", "run.steer", "run.start", "schema"},
		Implemented: []string{"run.attach", "run.cancel", "schema"},
	}
	err := requireRunStart(hello)
	if err == nil {
		t.Fatal("requireRunStart accepted a kernel that declares run.start but does not " +
			"implement it; the send would succeed and no run would be created, hanging the " +
			"log-follow forever -- the not_implemented trap M1b documented")
	}
	if !strings.Contains(err.Error(), "not_implemented") {
		t.Fatalf("refusal did not name not_implemented, so it reads as a transient failure "+
			"rather than a permanent gap in this binary: %v", err)
	}
}

// TestRequireRunStartRefusesUndeclared is the wrong-surface state: run.start is
// absent from `types` entirely, so the connected core is not the v1 run
// vocabulary this host speaks. The refusal must be distinct from the
// unimplemented one because the remedy differs -- a different kernel surface,
// not a newer build of the same one.
func TestRequireRunStartRefusesUndeclared(t *testing.T) {
	hello := &driver.Hello{
		Type:        "hello",
		Types:       []string{"blueprint.validate", "schema"},
		Implemented: []string{"blueprint.validate", "schema"},
	}
	err := requireRunStart(hello)
	if err == nil {
		t.Fatal("requireRunStart accepted a core that does not declare run.start at all; " +
			"that surface cannot begin a run and the host must say so, not send into the void")
	}
	if strings.Contains(err.Error(), "not_implemented") {
		t.Fatalf("an undeclared run.start was reported as not_implemented; the two states have "+
			"different remedies (wrong surface vs. unwired executor) and must not collapse: %v", err)
	}
}

// TestRequireRunStartRefusesNilHello guards the ordering contract: the gate is
// meaningless before the handshake fills the hello, so a nil hello is a caller
// bug (gating before connecting), refused rather than silently treated as
// "run.start unavailable".
func TestRequireRunStartRefusesNilHello(t *testing.T) {
	err := requireRunStart(nil)
	if err == nil {
		t.Fatal("requireRunStart accepted a nil hello; with no hello there is no implemented " +
			"list to gate on, so this must refuse rather than pass or panic")
	}
	if !strings.Contains(err.Error(), "handshake") {
		t.Fatalf("the nil-hello refusal did not point at the missing handshake, so it does not "+
			"tell the caller the gate ran too early: %v", err)
	}
}
