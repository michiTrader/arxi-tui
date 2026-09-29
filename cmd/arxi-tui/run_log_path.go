package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// defaultRunsRoot returns the directory arxi keeps each run's own subdirectory
// under. A run lives at <runsRoot>/<run-id>/ and its event log at
// <runsRoot>/<run-id>/events.ndjson -- the layout serveDriver.runID already
// reads a run id back out of (base(dir(logPath))). The default is
// ~/.arxi/runs, the parent of openServeDriver's existing ~/.arxi/runs/last
// default, so the run-follow path and the run-id derivation agree on where
// runs live rather than encoding the tree in two places that can drift.
func defaultRunsRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf(
			"cmd/arxi-tui/run_log_path.go: cannot resolve home dir for the runs root: %w", err)
	}
	return filepath.Join(home, ".arxi", "runs"), nil
}

// runLogPathForJob returns the event-log path a run.start job_id's log follows.
//
// It is the inverse of serveDriver.runID: a run's log lives at
// <runsRoot>/<job_id>/events.ndjson, so runID(runLogPathForJob(root, id)) == id
// for any id. Building the follow path here from the job_id run.start returns --
// rather than reading a run dir out of an ARXI_RUN_DIR the user pre-set -- is
// what lets the TUI follow the run it just created instead of one started
// outside it. The <runsRoot>/<id>/events.ndjson layout is a wire fact only M2's
// live round-trip can confirm against a real arxi serve; isolating it in one
// function keeps that confirmation to a single site, the same way runID keeps
// the forward derivation to one.
//
// An empty job_id is refused rather than yielding <runsRoot>/events.ndjson -- a
// path that would silently follow the runs root itself, which is no run's log.
// SubmitRunStart already fails loud on an ok response with no job_id, so this is
// the second line: a caller that skips that check still cannot build a log path
// that points at nothing.
func runLogPathForJob(runsRoot, jobID string) (string, error) {
	if strings.TrimSpace(jobID) == "" {
		return "", fmt.Errorf(
			"cmd/arxi-tui/run_log_path.go: empty job_id has no run log; " +
				"remedy: follow the job_id run.start returned (SubmitRunStart fails loud on an empty one)")
	}
	return filepath.Join(runsRoot, jobID, "events.ndjson"), nil
}
