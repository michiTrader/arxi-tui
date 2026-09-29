package driver

import (
	"context"
	"strings"
	"testing"
)

// TestRunCancelReturnsTheJobSnapshot pins the happy path: a run.cancel answered
// ok with a Job snapshot must surface the run's id and cancellation state. Cancel
// is a REQUEST, not an instant kill, so a running job answers non-terminal with
// cancellation_requested=true -- the host reads these fields rather than
// assuming the run stopped. The wire shape is arxi/host/v1/types.go Job (id,
// status, terminal, cancellation_requested), the same struct run.show returns.
func TestRunCancelReturnsTheJobSnapshot(t *testing.T) {
	resp := `{"id":"cancel","ok":true,"result":{"id":"run-2f","status":"running","terminal":false,"cancellation_requested":true}}`
	d := session(t, resp)

	res, err := d.SubmitRunCancel(context.Background(), RunCancelParams{RunID: "run-2f"})
	if err != nil {
		t.Fatalf("run.cancel answered ok with a Job snapshot and SubmitRunCancel "+
			"still reported an error: %v. The result is the run's state the host "+
			"reads to know the cancel was accepted", err)
	}
	if res.JobID != "run-2f" {
		t.Fatalf("SubmitRunCancel returned id %q, want %q; the host addressed one "+
			"run and must confirm the acknowledgement names it", res.JobID, "run-2f")
	}
	if !res.CancellationRequested {
		t.Fatalf("SubmitRunCancel dropped cancellation_requested; the core reports " +
			"cancel as a request the run has yet to honour, and losing that flag " +
			"makes a pending cancel read as no cancel at all")
	}
	if res.Status != "running" || res.Terminal {
		t.Fatalf("SubmitRunCancel returned status=%q terminal=%v, want running and "+
			"false; a still-running job is exactly the non-terminal case cancel "+
			"must not collapse into 'already stopped'", res.Status, res.Terminal)
	}
}

// TestRunCancelRefusalIsReturnedAsAnError holds run.cancel to the same contract
// SubmitRunStart has: ok:false is an answered refusal, not a transport success.
// A cancel against a run that does not exist answers not_found, and that must
// reach the caller as a *Refusal -- a nil error here makes a rejected cancel
// read as an accepted one, and the host would drop a run that is still alive.
func TestRunCancelRefusalIsReturnedAsAnError(t *testing.T) {
	refusal := `{"id":"cancel","ok":false,"error":{"code":"not_found","message":"no such run","fix":["arxi run list"]}}`
	d := session(t, refusal)

	res, err := d.SubmitRunCancel(context.Background(), RunCancelParams{RunID: "run-gone"})
	if err == nil {
		t.Fatal("the core refused run.cancel and SubmitRunCancel reported success. " +
			"ok:false is an answer, not a transport success; a nil error here makes " +
			"a rejected cancel read as an accepted one")
	}
	if res != nil {
		t.Fatalf("SubmitRunCancel returned a non-nil result alongside a refusal: %+v. "+
			"A refused cancel changed no run's state", res)
	}
	ref, ok := err.(*Refusal)
	if !ok {
		t.Fatalf("SubmitRunCancel returned a %T, want *Refusal: the code is a closed "+
			"set the host branches on (not_found means the run is already gone, a "+
			"different action than a retryable failure), and folding it into a plain "+
			"error throws that branch away", err)
	}
	if ref.Code != "not_found" || ref.Type != "run.cancel" {
		t.Fatalf("the refusal carried code=%q type=%q, want not_found and run.cancel; "+
			"a refusal with no verb in it is not addressable", ref.Code, ref.Type)
	}
}

// TestRunCancelEmptyRunIDIsRefusedLocally pins the guard that never reaches the
// wire: an empty run id addresses no run. The core would answer not_found, which
// reads as "the run is gone" when the truth is "the client sent no run" -- so
// SubmitRunCancel refuses locally and names the real cause. The response is a
// valid ok so a driver that skipped the guard would send the request and succeed
// against nothing.
func TestRunCancelEmptyRunIDIsRefusedLocally(t *testing.T) {
	resp := `{"id":"cancel","ok":true,"result":{"id":"run-1","status":"cancelled","terminal":true,"cancellation_requested":true}}`
	rec := &recordingWriter{}
	d := sessionWithWriter(t, rec, resp)

	res, err := d.SubmitRunCancel(context.Background(), RunCancelParams{RunID: ""})
	if err == nil {
		t.Fatal("SubmitRunCancel sent a run.cancel with an empty run id. An empty " +
			"id addresses no run; letting it reach the core borrows a not_found that " +
			"misnames the cause as a missing run rather than a missing id")
	}
	if res != nil {
		t.Fatalf("SubmitRunCancel returned a result for an unaddressed cancel: %+v", res)
	}
	if strings.Contains(rec.String(), "run.cancel") {
		t.Fatalf("SubmitRunCancel encoded a request %q despite the empty run id; the "+
			"guard must refuse before the wire, not after the core answers", rec.String())
	}
}

// TestRunCancelOkWithNoJobIDFailsLoud pins the one ok shape that must not be
// accepted: ok:true with an empty id. The host asked to cancel one run and got
// an acknowledgement that names none, so it cannot confirm the intended run was
// the one addressed. An unverifiable success is worse than a refusal, which at
// least says so -- the same reason SubmitRunStart rejects an ok with no job_id.
func TestRunCancelOkWithNoJobIDFailsLoud(t *testing.T) {
	resp := `{"id":"cancel","ok":true,"result":{"id":"","status":"cancelled","terminal":true,"cancellation_requested":true}}`
	d := session(t, resp)

	res, err := d.SubmitRunCancel(context.Background(), RunCancelParams{RunID: "run-2f"})
	if err == nil {
		t.Fatal("run.cancel answered ok with an empty id and SubmitRunCancel " +
			"accepted it. An acknowledgement that names no run cannot confirm the " +
			"intended run was cancelled; a cancel the host cannot verify is worse " +
			"than a refusal, which at least says so")
	}
	if res != nil {
		t.Fatalf("SubmitRunCancel returned a result it could not verify: %+v", res)
	}
}

// TestRunCancelOmitsUnsetReason pins the request wire: the required run id is
// always sent as the wire param `run`, and reason is omitted when unset so the
// core records no operator text the caller never supplied. The request is
// captured by a writer that records every encoded line.
func TestRunCancelOmitsUnsetReason(t *testing.T) {
	resp := `{"id":"cancel","ok":true,"result":{"id":"run-1","status":"running","terminal":false,"cancellation_requested":true}}`
	rec := &recordingWriter{}
	d := sessionWithWriter(t, rec, resp)

	if _, err := d.SubmitRunCancel(context.Background(), RunCancelParams{RunID: "run-1"}); err != nil {
		t.Fatalf("run.cancel happy path errored: %v", err)
	}

	sent := rec.String()
	for _, want := range []string{`"run":"run-1"`, `"type":"run.cancel"`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the run.cancel request %q is missing required field %q; the run "+
				"id is the param the core's cancel dispatch reads to address the run",
				sent, want)
		}
	}
	if strings.Contains(sent, `"reason"`) {
		t.Fatalf("the run.cancel request %q sent an unset reason; an omitted reason "+
			"records nothing, while an empty string is operator text the caller "+
			"never wrote", sent)
	}
}

// TestRunCancelSendsSetReason is the counterfactual of the omission test: when
// the caller DOES supply a reason, it must reach the wire. Without this the
// omit-when-unset logic could pass by never sending reason at all.
func TestRunCancelSendsSetReason(t *testing.T) {
	resp := `{"id":"cancel","ok":true,"result":{"id":"run-1","status":"running","terminal":false,"cancellation_requested":true}}`
	rec := &recordingWriter{}
	d := sessionWithWriter(t, rec, resp)

	if _, err := d.SubmitRunCancel(context.Background(), RunCancelParams{
		RunID:  "run-1",
		Reason: "user quit the session",
	}); err != nil {
		t.Fatalf("run.cancel with a reason errored: %v", err)
	}

	sent := rec.String()
	if !strings.Contains(sent, `"reason":"user quit the session"`) {
		t.Fatalf("the run.cancel request %q dropped the supplied reason; a reason the "+
			"caller set must reach the core, or the setting is a no-op that lies to "+
			"the caller", sent)
	}
}
