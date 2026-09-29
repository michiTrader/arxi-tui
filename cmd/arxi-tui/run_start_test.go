package main

import (
	"context"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/driver"
)

// fakeRunStarter is a runStarter that returns a chosen hello and job_id without
// a subprocess. It records the params SubmitRunStart was called with so a test
// can assert startRun forwarded the resolved config and the prompt, and it can
// refuse the round-trip to stand in for a core's bad_params refusal.
type fakeRunStarter struct {
	hello     *driver.Hello
	jobID     string
	submitErr error

	gotParams driver.RunStartParams
	submitted bool
}

func (f *fakeRunStarter) Hello() *driver.Hello { return f.hello }

func (f *fakeRunStarter) SubmitRunStart(ctx context.Context, p driver.RunStartParams) (*driver.RunStartResult, error) {
	f.submitted = true
	f.gotParams = p
	if f.submitErr != nil {
		return nil, f.submitErr
	}
	return &driver.RunStartResult{JobID: f.jobID, Status: "running"}, nil
}

// implementingHello is a hello whose implemented list carries run.start, the
// accepting state requireRunStart passes. Kept as a helper so each test that
// needs a good hello does not re-spell the two lists and drift on them.
func implementingHello() *driver.Hello {
	return &driver.Hello{
		Type:        "hello",
		Types:       []string{"run.prompt", "run.steer", "run.start", "schema"},
		Implemented: []string{"run.start", "run.attach", "run.cancel", "schema"},
	}
}

// emptyEnv is the getenv a plug-and-play session sees: nothing set, so every
// default applies. resolveRunStartParams' own tests cover the override and
// refusal paths; startRun's tests only need the default path to work and one
// refusal to propagate.
func emptyEnv(string) string { return "" }

// TestStartRunFollowsTheJobItCreated is the whole join in one pass: an
// implementing kernel, default config, a job_id back, and the log path derived
// from THAT job_id. This is the property M2 exists to deliver -- the TUI
// follows the run it started, not one started outside it -- so it is pinned
// end to end.
func TestStartRunFollowsTheJobItCreated(t *testing.T) {
	rs := &fakeRunStarter{hello: implementingHello(), jobID: "run-abc"}

	logPath, actorLabel, err := startRun(context.Background(), rs, emptyEnv, "/root/runs", "hello there")
	if err != nil {
		t.Fatalf("startRun refused a clean sequence: %v; an implementing kernel with default "+
			"config and a returned job_id is exactly the case that must succeed", err)
	}

	// runLogPathForJob is the inverse of serveDriver.runID, so the path must be
	// <runsRoot>/<job_id>/events.ndjson -- the run the TUI creates and the run it
	// follows are one id.
	if !strings.Contains(logPath, "run-abc") {
		t.Fatalf("log path %q does not name the created run-abc; the TUI would follow the wrong "+
			"run's log (or none), the exact drift runLogPathForJob exists to prevent", logPath)
	}
	if actorLabel != defaultActor {
		t.Fatalf("actorLabel = %q, want the resolved default %q; the status bar shows this so a "+
			"plug-and-play default is never invisible", actorLabel, defaultActor)
	}
	if !rs.submitted {
		t.Fatal("startRun returned a log path without calling SubmitRunStart; the run was never " +
			"created and the log-follow would wait forever on a file no run writes")
	}
	if rs.gotParams.Prompt != "hello there" {
		t.Fatalf("SubmitRunStart got prompt %q, want the session's first line; run.start takes the "+
			"first prompt positionally and a lost prompt starts an empty run", rs.gotParams.Prompt)
	}
	if rs.gotParams.Actor != defaultActor {
		t.Fatalf("SubmitRunStart got actor %q, want the resolved default %q; startRun must forward "+
			"the config resolveRunStartParams built, not a zero value", rs.gotParams.Actor, defaultActor)
	}
}

// TestStartRunGatesBeforeSubmitting is the ordering counterfactual for step 1:
// a declared-but-unimplemented run.start (the M1b not_implemented trap) must be
// refused BEFORE SubmitRunStart is called. If the gate ran after the submit --
// or not at all -- the host would send run.start into a kernel that never
// executes it and hang on a log no run creates. The test proves both halves:
// the refusal, and that nothing was submitted.
func TestStartRunGatesBeforeSubmitting(t *testing.T) {
	rs := &fakeRunStarter{
		hello: &driver.Hello{
			Type:        "hello",
			Types:       []string{"run.prompt", "run.steer", "run.start", "schema"},
			Implemented: []string{"run.attach", "run.cancel", "schema"},
		},
		jobID: "run-should-not-be-used",
	}

	_, _, err := startRun(context.Background(), rs, emptyEnv, "/root/runs", "hello")
	if err == nil {
		t.Fatal("startRun accepted a kernel that declares run.start but does not implement it; " +
			"the send would succeed and no run would exist -- the not_implemented hang M1b documented")
	}
	if !strings.Contains(err.Error(), "not_implemented") {
		t.Fatalf("refusal did not name not_implemented, so it reads as transient not permanent: %v", err)
	}
	if rs.submitted {
		t.Fatal("startRun called SubmitRunStart after the hello failed the gate; the gate must " +
			"run first, or a kernel that cannot start a run still receives the send")
	}
}

// TestStartRunRefusesBadConfigBeforeSubmitting is the ordering counterfactual
// for step 2: a malformed ARXI_BUDGET must be refused by name, and again before
// any run is created -- spending a run.start against a budget the user did not
// choose is exactly what resolveRunStartParams refuses, and startRun must honour
// that refusal rather than submit with a defaulted value.
func TestStartRunRefusesBadConfigBeforeSubmitting(t *testing.T) {
	rs := &fakeRunStarter{hello: implementingHello(), jobID: "run-xyz"}
	getenv := func(k string) string {
		if k == "ARXI_BUDGET" {
			return "not-a-number"
		}
		return ""
	}

	_, _, err := startRun(context.Background(), rs, getenv, "/root/runs", "hello")
	if err == nil {
		t.Fatal("startRun submitted with a malformed ARXI_BUDGET; a run.start starts a live model " +
			"by default, so a budget the user never wrote must refuse, not silently default")
	}
	if !strings.Contains(err.Error(), "ARXI_BUDGET") {
		t.Fatalf("the config refusal did not name ARXI_BUDGET, so the user cannot see which knob to fix: %v", err)
	}
	if rs.submitted {
		t.Fatal("startRun created a run despite a bad config; params must resolve before the submit")
	}
}

// TestStartRunPropagatesTheSubmitRefusal is step 3's failure path: an ok:false
// from the core (a budget or actor refusal, already turned into a *Refusal by
// SubmitRunStart) must reach the caller as an error, not a zero-valued log path
// that reads as a started run.
func TestStartRunPropagatesTheSubmitRefusal(t *testing.T) {
	rs := &fakeRunStarter{
		hello:     implementingHello(),
		submitErr: context.Canceled, // any error stands in for the core's refusal
	}

	logPath, _, err := startRun(context.Background(), rs, emptyEnv, "/root/runs", "hello")
	if err == nil {
		t.Fatal("startRun swallowed SubmitRunStart's error and returned success; a refused run.start " +
			"must not read as a started run with a followable log")
	}
	if logPath != "" {
		t.Fatalf("startRun returned a log path %q on a failed submit; there is no run to follow", logPath)
	}
}

// TestStartRunRefusesAnEmptyJobID is step 4's failure path: an ok:true with an
// empty job_id (SubmitRunStart is meant to catch this, so this is the second
// line) must not yield <runsRoot>/events.ndjson -- the runs root itself, no
// run's log. runLogPathForJob refuses it and startRun must surface that.
func TestStartRunRefusesAnEmptyJobID(t *testing.T) {
	rs := &fakeRunStarter{hello: implementingHello(), jobID: ""}

	logPath, _, err := startRun(context.Background(), rs, emptyEnv, "/root/runs", "hello")
	if err == nil {
		t.Fatalf("startRun accepted an empty job_id and returned log path %q; that path follows the "+
			"runs root itself, which is no run's log -- an unreachable follow is worse than a refusal", logPath)
	}
	if !strings.Contains(err.Error(), "log path") {
		t.Fatalf("the empty-job_id refusal did not point at the missing log path: %v", err)
	}
}
