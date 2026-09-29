package main

import (
	"context"
	"fmt"

	"github.com/michiTrader/arxi_tui/internal/driver"
)

// runStarter is the subset of *driver.NDJSONDriver that startRun needs: the
// hello to gate on and the run.start round-trip. It is an interface, not the
// concrete driver, so startRun -- the sequencing of the four M2 pieces -- is
// pinned against a fake that returns a chosen hello and job_id, keeping the one
// thing only a real arxi serve can confirm (the live round-trip) out of the
// test while everything up to it is proven here. *driver.NDJSONDriver satisfies
// it (Hello, SubmitRunStart), so openServeDriver passes the real driver through
// unchanged.
type runStarter interface {
	Hello() *driver.Hello
	SubmitRunStart(ctx context.Context, p driver.RunStartParams) (*driver.RunStartResult, error)
}

// startRun performs the M2 run.start sequence and returns the event-log path the
// created run will write, plus the actor label for the status bar.
//
// It is the join between the four network-free pieces M1c/M2 landed alone
// (SubmitRunStart, resolveRunStartParams, runLogPathForJob, requireRunStart),
// composed in the one order that is correct and testable without a subprocess:
//
//  1. requireRunStart gates on the hello FIRST. A kernel that declares but does
//     not implement run.start (the M1b not_implemented trap: run.prompt and
//     run.steer are both declared-but-unimplemented on this build) would accept
//     the send and never create a run, hanging the log-follow forever. Gating
//     before resolving params or spending a prompt is what makes the failure a
//     named refusal at connect rather than a silent hang.
//  2. resolveRunStartParams reads the session config (actor/budget/sim/model),
//     refusing a malformed ARXI_BUDGET/ARXI_SIM by name rather than guessing.
//  3. SubmitRunStart creates the run and returns its job_id (an ok:false is
//     already turned into a *Refusal, and an ok:true with an empty job_id fails
//     loud, so a nil error here means a real run id).
//  4. runLogPathForJob derives the log path from that job_id, so the TUI follows
//     the run it just created rather than one started outside it.
//
// The prompt is the run's first prompt: under the one-run.start-per-turn mapping
// M1c established (run.prompt/run.steer have no executor on this build), a
// session's first user line is what begins the run.
func startRun(ctx context.Context, rs runStarter, getenv func(string) string, runsRoot, prompt string) (logPath, actorLabel string, err error) {
	if err := requireRunStart(rs.Hello()); err != nil {
		return "", "", err
	}

	params, actorLabel, err := resolveRunStartParams(getenv)
	if err != nil {
		return "", "", err
	}
	params.Prompt = prompt

	res, err := rs.SubmitRunStart(ctx, params)
	if err != nil {
		return "", "", err
	}

	logPath, err = runLogPathForJob(runsRoot, res.JobID)
	if err != nil {
		// SubmitRunStart already fails loud on an ok:true with an empty job_id,
		// so reaching this is a caller that bypassed that guard or a future
		// SubmitRunStart that stopped enforcing it; either way an unreachable
		// log is worse than a refusal, so name it rather than follow a path that
		// points at the runs root itself.
		return "", "", fmt.Errorf("cmd/arxi-tui/run_start.go: run.start returned no usable log path: %w", err)
	}

	return logPath, actorLabel, nil
}
