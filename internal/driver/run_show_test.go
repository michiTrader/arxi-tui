package driver

import (
	"context"
	"strings"
	"testing"
)

// TestRunShowReturnsTheRunSnapshot pins the happy path AND the projection width
// that makes run.show a status query rather than a worse run.cancel: a run.show
// answered ok with a Job snapshot must surface not only the id/status/terminal
// run.cancel also carries, but the run-state fields an inspect exists for --
// turns against the cap and spend against the budget. The wire shape is
// arxi/host/v1/types.go Job, the same struct run.cancel returns; this test is
// the guard that RunShowResult does not silently drop the wider slice (a dropped
// field decodes to a zero and reads as "0 turns, $0 spent", a false state).
func TestRunShowReturnsTheRunSnapshot(t *testing.T) {
	resp := `{"id":"show","ok":true,"result":{"id":"run-2f","status":"running","terminal":false,"turns":3,"max_turns":8,"spent_usd":0.42,"budget_usd":1.5,"cancellation_requested":false,"result":""}}`
	d := session(t, resp)

	res, err := d.SubmitRunShow(context.Background(), RunShowParams{RunID: "run-2f"})
	if err != nil {
		t.Fatalf("run.show answered ok with a Job snapshot and SubmitRunShow still "+
			"reported an error: %v. The result is the run's live state the host "+
			"reads to show progress", err)
	}
	if res.JobID != "run-2f" {
		t.Fatalf("SubmitRunShow returned id %q, want %q; the host inspected one run "+
			"and must confirm the snapshot names it", res.JobID, "run-2f")
	}
	if res.Status != "running" || res.Terminal {
		t.Fatalf("SubmitRunShow returned status=%q terminal=%v, want running and "+
			"false; a live run misreported as terminal reads as finished", res.Status, res.Terminal)
	}
	if res.Turns != 3 || res.MaxTurns != 8 {
		t.Fatalf("SubmitRunShow returned turns=%d max_turns=%d, want 3 and 8; turns "+
			"against the cap are the progress an inspect exists to surface, and a "+
			"dropped field reads as no progress", res.Turns, res.MaxTurns)
	}
	if res.SpentUSD != 0.42 || res.BudgetUSD != 1.5 {
		t.Fatalf("SubmitRunShow returned spent_usd=%v budget_usd=%v, want 0.42 and "+
			"1.5; spend against the budget is the pair a budget-aware status line "+
			"shows, and a dropped one reads as $0 spent", res.SpentUSD, res.BudgetUSD)
	}
}

// TestRunShowSurfacesTheResultOfAFinishedRun is the terminal-side counterfactual
// of the running-side happy path: a finished run answers terminal=true and
// carries its final text in result, and an inspect must surface both. Without
// this the running-only fixture above leaves result and terminal=true untested,
// and either could be dropped while every running-state assertion still passes.
func TestRunShowSurfacesTheResultOfAFinishedRun(t *testing.T) {
	resp := `{"id":"show","ok":true,"result":{"id":"run-2f","status":"succeeded","terminal":true,"turns":5,"max_turns":8,"spent_usd":0.9,"budget_usd":1.5,"cancellation_requested":false,"result":"done: the patch applies cleanly"}}`
	d := session(t, resp)

	res, err := d.SubmitRunShow(context.Background(), RunShowParams{RunID: "run-2f"})
	if err != nil {
		t.Fatalf("run.show of a finished run errored: %v", err)
	}
	if !res.Terminal || res.Status != "succeeded" {
		t.Fatalf("SubmitRunShow returned terminal=%v status=%q, want true and "+
			"succeeded; a finished run reported as live never lets the caller stop "+
			"polling", res.Terminal, res.Status)
	}
	if res.Result != "done: the patch applies cleanly" {
		t.Fatalf("SubmitRunShow returned result %q, want the run's final text; "+
			"surfacing it here is what lets an inspect of a finished run show its "+
			"outcome without a separate run.result round-trip", res.Result)
	}
}

// TestRunShowRefusalIsReturnedAsAnError holds run.show to the same contract every
// other verb has: ok:false is an answered refusal, not a transport success. A
// show of a run that does not exist answers not_found, and that must reach the
// caller as a *Refusal -- a nil error here makes a rejected inspect read as a
// real (empty) run snapshot.
func TestRunShowRefusalIsReturnedAsAnError(t *testing.T) {
	refusal := `{"id":"show","ok":false,"error":{"code":"not_found","message":"no such run","fix":["arxi run list"]}}`
	d := session(t, refusal)

	res, err := d.SubmitRunShow(context.Background(), RunShowParams{RunID: "run-gone"})
	if err == nil {
		t.Fatal("the core refused run.show and SubmitRunShow reported success. " +
			"ok:false is an answer, not a transport success; a nil error here makes " +
			"a rejected inspect read as a real run snapshot")
	}
	if res != nil {
		t.Fatalf("SubmitRunShow returned a non-nil result alongside a refusal: %+v. "+
			"A refused inspect describes no run", res)
	}
	ref, ok := err.(*Refusal)
	if !ok {
		t.Fatalf("SubmitRunShow returned a %T, want *Refusal: the code is a closed "+
			"set the host branches on (not_found means the run is gone), and folding "+
			"it into a plain error throws that branch away", err)
	}
	if ref.Code != "not_found" || ref.Type != "run.show" {
		t.Fatalf("the refusal carried code=%q type=%q, want not_found and run.show; "+
			"a refusal with no verb in it is not addressable", ref.Code, ref.Type)
	}
}

// TestRunShowEmptyRunIDIsRefusedLocally pins the guard that never reaches the
// wire: an empty run id addresses no run. The core would answer not_found, which
// reads as "the run is gone" when the truth is "the client sent no run" -- so
// SubmitRunShow refuses locally and names the real cause. The response is a valid
// ok so a driver that skipped the guard would send the request and succeed
// against nothing.
func TestRunShowEmptyRunIDIsRefusedLocally(t *testing.T) {
	resp := `{"id":"show","ok":true,"result":{"id":"run-1","status":"running","terminal":false}}`
	rec := &recordingWriter{}
	d := sessionWithWriter(t, rec, resp)

	res, err := d.SubmitRunShow(context.Background(), RunShowParams{RunID: ""})
	if err == nil {
		t.Fatal("SubmitRunShow sent a run.show with an empty run id. An empty id " +
			"addresses no run; letting it reach the core borrows a not_found that " +
			"misnames the cause as a missing run rather than a missing id")
	}
	if res != nil {
		t.Fatalf("SubmitRunShow returned a result for an unaddressed inspect: %+v", res)
	}
	if strings.Contains(rec.String(), "run.show") {
		t.Fatalf("SubmitRunShow encoded a request %q despite the empty run id; the "+
			"guard must refuse before the wire, not after the core answers", rec.String())
	}
}

// TestRunShowOkWithNoJobIDFailsLoud pins the one ok shape that must not be
// accepted: ok:true with an empty id. The host asked about one run and got a
// snapshot that names none, so it cannot tell the snapshot describes the run it
// inspected. An unverifiable snapshot is worse than a refusal -- the same reason
// SubmitRunStart rejects an ok with no job_id.
func TestRunShowOkWithNoJobIDFailsLoud(t *testing.T) {
	resp := `{"id":"show","ok":true,"result":{"id":"","status":"running","terminal":false}}`
	d := session(t, resp)

	res, err := d.SubmitRunShow(context.Background(), RunShowParams{RunID: "run-2f"})
	if err == nil {
		t.Fatal("run.show answered ok with an empty id and SubmitRunShow accepted " +
			"it. A snapshot that names no run cannot be trusted to describe the run " +
			"that was inspected; an unverifiable snapshot is worse than a refusal")
	}
	if res != nil {
		t.Fatalf("SubmitRunShow returned a snapshot it could not verify: %+v", res)
	}
}

// TestRunShowSendsTheRunID pins the request wire: the run id is sent as the wire
// param `run`, the name the core's inspect dispatch reads, under a run.show type.
// run.show carries no optionals (an inspect mutates nothing and takes no reason),
// so the whole request is the type and the addressed run.
func TestRunShowSendsTheRunID(t *testing.T) {
	resp := `{"id":"show","ok":true,"result":{"id":"run-1","status":"running","terminal":false}}`
	rec := &recordingWriter{}
	d := sessionWithWriter(t, rec, resp)

	if _, err := d.SubmitRunShow(context.Background(), RunShowParams{RunID: "run-1"}); err != nil {
		t.Fatalf("run.show happy path errored: %v", err)
	}

	sent := rec.String()
	for _, want := range []string{`"run":"run-1"`, `"type":"run.show"`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the run.show request %q is missing required field %q; the run id "+
				"is the param the core's inspect dispatch reads to address the run",
				sent, want)
		}
	}
}
