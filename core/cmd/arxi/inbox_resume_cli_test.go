package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInboxApproveResumeContinuesTheRun pins the one-step answer a front end
// without its own scheduler needs: the approval is written to the log and the
// same process drives the run on, so a person never has to know that two
// separate acts (answer, then resume) exist.
func TestInboxApproveResumeContinuesTheRun(t *testing.T) {
	dir := workdir(t)
	blockedRun(t, dir, "r1")
	upgradeFixtureRun(t, dir, "r1")

	got := arxi(t, dir, "inbox", "approve", "inbox-1", "--resume")
	t.Logf("exit %d\n%s", got.code, got.out)
	if !strings.Contains(got.out, "approved.") {
		t.Fatalf("the answer was not recorded:\n%s", got.out)
	}
	log, err := os.ReadFile(filepath.Join(dir, "runs", "r1", "events.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), `"inbox.replied"`) {
		t.Fatalf("no inbox.replied in the log")
	}
	if _, err := os.Stat(filepath.Join(dir, "runs", "r1", "writer.lock")); !os.IsNotExist(err) {
		t.Errorf("the run's writer lock was left behind: %v", err)
	}
}

// TestInboxRunFlagNarrowsAnAmbiguousID: inbox-1 exists in every blocked run, so a
// caller that knows its run says which one, and the other run is left alone.
func TestInboxRunFlagNarrowsAnAmbiguousID(t *testing.T) {
	dir := workdir(t)
	blockedRun(t, dir, "r1")
	blockedRun(t, dir, "r2")

	if got := arxi(t, dir, "inbox", "approve", "inbox-1"); got.code == 0 {
		t.Fatalf("an id pending in two runs must be refused without --run:\n%s", got.out)
	}
	got := arxi(t, dir, "inbox", "approve", "inbox-1", "--run", "r2")
	if got.code != 0 || !strings.Contains(got.out, "r2") {
		t.Fatalf("--run r2 should answer r2 only: exit %d\n%s", got.code, got.out)
	}
	if left := arxi(t, dir, "inbox"); !strings.Contains(left.out, "r1") {
		t.Errorf("r1 must still be waiting:\n%s", left.out)
	}
}
