package main

import (
	"fmt"
	"strings"

	"arxi-tui/internal/driver"
)

// runStartType is the wire type SubmitRunStart sends. It is named once here so
// the gate below and the request builder cannot drift on the spelling of the
// one verb the whole run.start integration turns on.
const runStartType = "run.start"

// requireRunStart is the gate M2's live round-trip stands behind: it reads the
// hello the core sent at connect and decides whether this build can begin a run
// at all, before openServeDriver commits to resolving params and following a
// log that no run will ever write.
//
// The check is on the hello's `implemented` list, not its `types` list, and the
// distinction is the whole point (M1b paid for learning it): run.prompt and
// run.steer are both DECLARED in surface v1 and both answer not_implemented on
// this build, so a host that trusted `types` would send run.start to a kernel
// that never executes it and then wait forever on a log that is never created.
// run.start is the only verb that can begin a run from the TUI, so a kernel
// missing its executor is one the TUI cannot drive -- and not_implemented is
// permanent for a given binary, so retrying is not a fix and the refusal says so.
//
// The three refused states are kept distinct because their remedies differ: a
// nil hello means the handshake has not run (a caller ordering bug, not a
// kernel gap); an undeclared run.start means the connected surface is not the
// v1 vocabulary this host speaks (the wrong kernel, caught earlier by the
// surface-version gate but named here too); a declared-but-unimplemented
// run.start means the right surface on a build that has not wired the executor.
func requireRunStart(hello *driver.Hello) error {
	if hello == nil {
		return fmt.Errorf(
			"cmd/arxi-tui/run_start_gate.go: no hello to gate on; the handshake must " +
				"complete before requireRunStart, since the implemented list is what " +
				"tells run.start apart from a declared-but-unimplemented verb")
	}

	declared := false
	for _, t := range hello.Types {
		if t == runStartType {
			declared = true
			break
		}
	}
	if !declared {
		return fmt.Errorf(
			"cmd/arxi-tui/run_start_gate.go: the connected core does not declare %q; "+
				"its surface is not the v1 run vocabulary this host speaks, so it is the "+
				"wrong kernel. remedy: connect to an arxi build whose surface declares "+
				"run.start (declared types: %s)",
			runStartType, strings.Join(hello.Types, ", "))
	}

	for _, t := range hello.Implemented {
		if t == runStartType {
			return nil
		}
	}
	return fmt.Errorf(
		"cmd/arxi-tui/run_start_gate.go: the connected core declares %q but does not "+
			"implement it (it answers not_implemented), and run.start is the only verb "+
			"that begins a run from the TUI -- so this build cannot start or drive a run, "+
			"and following a log for one would wait forever on a file no run creates. "+
			"not_implemented is permanent for a binary, so retrying will not help. "+
			"remedy: connect to an arxi build that implements run.start (implemented "+
			"verbs on this build: %s)",
		runStartType, strings.Join(hello.Implemented, ", "))
}
