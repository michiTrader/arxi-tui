package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestRunLogPathRoundTripsWithRunID is the load-bearing property: the log path
// runLogPathForJob builds for a job_id must be one serveDriver.runID reads that
// exact id back out of. If the two derivations disagree, the TUI follows a run
// under one id and addresses it (attach/show/cancel) under another -- a run it
// created but cannot drive, the silent dead end SubmitRunStart's empty-job_id
// guard exists to prevent one layer up.
func TestRunLogPathRoundTripsWithRunID(t *testing.T) {
	root := filepath.Join("var", "runs")
	for _, jobID := range []string{"run-2f", "last", "abc123", "a"} {
		logPath, err := runLogPathForJob(root, jobID)
		if err != nil {
			t.Fatalf("runLogPathForJob(%q,%q) errored: %v; a non-empty job_id has a log path",
				root, jobID, err)
		}
		sd := &serveDriver{logPath: logPath}
		if got := sd.runID(); got != jobID {
			t.Fatalf("runID(runLogPathForJob(%q)) = %q, want %q; the follow path and the "+
				"run-id derivation disagree, so the TUI would address the run it created "+
				"under the wrong id", jobID, got, jobID)
		}
	}
}

// TestRunLogPathIsUnderTheJobDir pins the layout the round-trip does not fully
// nail down on its own: the log is <runsRoot>/<job_id>/events.ndjson, so the
// job's own directory sits between the root and the leaf. runID reads only the
// parent dir's base, so a path that put the id elsewhere (or dropped the
// per-run directory) could still round-trip while pointing at the wrong file.
func TestRunLogPathIsUnderTheJobDir(t *testing.T) {
	root := filepath.Join("var", "runs")
	got, err := runLogPathForJob(root, "run-9")
	if err != nil {
		t.Fatalf("runLogPathForJob errored on a valid job_id: %v", err)
	}
	want := filepath.Join(root, "run-9", "events.ndjson")
	if got != want {
		t.Fatalf("runLogPathForJob = %q, want %q; the log must live at "+
			"<runsRoot>/<job_id>/events.ndjson so runID can read the id back out",
			got, want)
	}
}

// TestRunLogPathRefusesEmptyJobID holds the guard the doc comment argues: an
// empty job_id must not yield <runsRoot>/events.ndjson, a path that follows the
// runs root itself and belongs to no run. The refusal names job_id so the fix
// (follow the id run.start returned) is legible.
func TestRunLogPathRefusesEmptyJobID(t *testing.T) {
	for _, jobID := range []string{"", "   "} {
		_, err := runLogPathForJob(filepath.Join("var", "runs"), jobID)
		if err == nil {
			t.Fatalf("runLogPathForJob accepted job_id %q; an empty id would follow the "+
				"runs root itself, which is no run's log", jobID)
		}
		if !strings.Contains(err.Error(), "job_id") {
			t.Fatalf("the empty-job_id refusal must name job_id so the fix is obvious; got %q",
				err.Error())
		}
	}
}
