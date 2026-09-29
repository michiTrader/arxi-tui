package driver

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

// recordingWriter captures every byte the driver encodes so a test can assert
// on the exact request wire. The driver encodes under its own mutex and the
// test reads after the call returns, so a plain buffer needs no locking.
type recordingWriter struct{ buf bytes.Buffer }

func (w *recordingWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }
func (w *recordingWriter) String() string              { return w.buf.String() }

// sessionWithWriter is session()'s sibling for the tests that assert on the
// request bytes: it wires the recorded hello and the given responses as the
// reader and the caller's writer as the sink, with the handshake done.
func sessionWithWriter(t *testing.T, w io.Writer, responses ...string) *NDJSONDriver {
	t.Helper()
	wire := helloLine(t) + "\n" + strings.Join(responses, "\n") + "\n"
	d := NewNDJSON(readWriter{r: strings.NewReader(wire), w: w})
	if err := d.Handshake(context.Background()); err != nil {
		t.Fatalf("handshake against the recorded core: %v", err)
	}
	return d
}

// TestRunStartReturnsTheJobID pins the happy path: a run.start answered ok with
// a SubmitResult must surface the job_id, because that id is the run id every
// later verb (attach/show/result/cancel) addresses. The wire shape is the one
// arxi's own serve_lifecycle_test.go pins (M1a): result carries job_id,
// accepted_seq and status.
func TestRunStartReturnsTheJobID(t *testing.T) {
	resp := `{"id":"start","ok":true,"result":{"job_id":"run-2f","accepted_seq":7,"status":"queued"}}`
	d := session(t, resp)

	res, err := d.SubmitRunStart(context.Background(), RunStartParams{
		Actor:  "reviewer",
		Prompt: "hola",
		Budget: 0.5,
	})
	if err != nil {
		t.Fatalf("run.start answered ok with a job_id and SubmitRunStart still "+
			"reported an error: %v. The result is a started run the host must be "+
			"able to drive", err)
	}
	if res.JobID != "run-2f" {
		t.Fatalf("SubmitRunStart returned job_id %q, want %q; the job_id is the "+
			"run id every later verb addresses, so a dropped one leaves the run "+
			"unreachable", res.JobID, "run-2f")
	}
	if res.AcceptedSeq != 7 || res.Status != "queued" {
		t.Fatalf("SubmitRunStart returned accepted_seq=%d status=%q, want 7 and "+
			"%q; the whole SubmitResult reaches the caller, not just the id",
			res.AcceptedSeq, res.Status, "queued")
	}
}

// TestRunStartRefusalIsReturnedAsAnError holds run.start to the same contract
// SubmitPrompt has: ok:false is an answered refusal, not a transport success,
// so it must reach the caller as an error rather than a zero-valued result that
// reads as a started run. The refusal used is the budget one arxi's serve_test.go
// pins -- a non-positive budget is bad_params, the refusal a run.start actually
// receives (unlike run.prompt, run.start IS implemented, so not_implemented is
// not its failure mode).
func TestRunStartRefusalIsReturnedAsAnError(t *testing.T) {
	refusal := `{"id":"start","ok":false,"error":{"code":"bad_params","message":"budget must be positive","fix":["arxi schema"]}}`
	d := session(t, refusal)

	res, err := d.SubmitRunStart(context.Background(), RunStartParams{
		Actor:  "reviewer",
		Prompt: "hola",
		Budget: 0,
	})
	if err == nil {
		t.Fatal("the core refused run.start and SubmitRunStart reported success. " +
			"ok:false is an answer, not a transport success; a nil error here makes " +
			"a rejected submission read as a started run")
	}
	if res != nil {
		t.Fatalf("SubmitRunStart returned a non-nil result alongside a refusal: %+v. "+
			"A refused start has no run to drive", res)
	}
	ref, ok := err.(*Refusal)
	if !ok {
		t.Fatalf("SubmitRunStart returned a %T, want *Refusal: the code is a closed "+
			"set the host branches on (bad_params means retrying will not help), and "+
			"folding it into a plain error throws that branch away", err)
	}
	if ref.Code != "bad_params" || ref.Type != "run.start" {
		t.Fatalf("the refusal carried code=%q type=%q, want bad_params and run.start; "+
			"a refusal with no verb in it is not addressable", ref.Code, ref.Type)
	}
}

// TestRunStartOkWithNoJobIDFailsLoud pins the one result shape that must not be
// accepted: ok:true with an empty job_id. The id is how every later verb reaches
// the run, so returning a result whose JobID is "" is a silent dead end -- a run
// the host believes it started but can never attach to. Fail loud instead.
func TestRunStartOkWithNoJobIDFailsLoud(t *testing.T) {
	resp := `{"id":"start","ok":true,"result":{"job_id":"","accepted_seq":1,"status":"queued"}}`
	d := session(t, resp)

	res, err := d.SubmitRunStart(context.Background(), RunStartParams{
		Actor:  "reviewer",
		Prompt: "hola",
		Budget: 0.5,
	})
	if err == nil {
		t.Fatal("run.start answered ok with an empty job_id and SubmitRunStart " +
			"accepted it. An empty id leaves the run unreachable by every later " +
			"verb; a started run nobody can address is worse than a refusal, which " +
			"at least says so")
	}
	if res != nil {
		t.Fatalf("SubmitRunStart returned a result with an unusable id: %+v", res)
	}
}

// TestRunStartOmitsUnsetOptionals pins the request wire, not the response: the
// three required fields are always sent and the optionals are omitted when
// unset, so the core applies its own defaults rather than the client sending a
// zero it never chose. workspace must never appear -- its empty string is an
// illegal fourth enum member. The request is captured by a writer that records
// every encoded line.
func TestRunStartOmitsUnsetOptionals(t *testing.T) {
	resp := `{"id":"start","ok":true,"result":{"job_id":"run-1","accepted_seq":1,"status":"queued"}}`
	rec := &recordingWriter{}
	d := sessionWithWriter(t, rec, resp)

	if _, err := d.SubmitRunStart(context.Background(), RunStartParams{
		Actor:  "reviewer",
		Prompt: "hola",
		Budget: 1.0,
	}); err != nil {
		t.Fatalf("run.start happy path errored: %v", err)
	}

	sent := rec.String()
	for _, want := range []string{`"actor":"reviewer"`, `"prompt":"hola"`, `"budget":1`, `"type":"run.start"`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the run.start request %q is missing required field %q; actor, "+
				"prompt and budget are the params the core reads unconditionally",
				sent, want)
		}
	}
	for _, unwanted := range []string{`"max_turns"`, `"model"`, `"sim"`, `"workspace"`} {
		if strings.Contains(sent, unwanted) {
			t.Fatalf("the run.start request %q sent unset optional %q; an omitted "+
				"optional lets the core apply its own default, while a zero value "+
				"the caller never chose (or an empty workspace, an illegal enum "+
				"member) is a request the client did not mean to make", sent, unwanted)
		}
	}
}

// TestRunStartSendsSetOptionals is the counterfactual of the omission test: when
// the caller DOES choose an optional, it must reach the wire. Without this the
// omit-when-unset logic could pass by never sending the optionals at all.
func TestRunStartSendsSetOptionals(t *testing.T) {
	resp := `{"id":"start","ok":true,"result":{"job_id":"run-1","accepted_seq":1,"status":"queued"}}`
	rec := &recordingWriter{}
	d := sessionWithWriter(t, rec, resp)

	if _, err := d.SubmitRunStart(context.Background(), RunStartParams{
		Actor:    "reviewer",
		Prompt:   "hola",
		Budget:   1.0,
		MaxTurns: 4,
		Model:    "deepseek-v4.1-flash",
		Sim:      true,
	}); err != nil {
		t.Fatalf("run.start with optionals errored: %v", err)
	}

	sent := rec.String()
	for _, want := range []string{`"max_turns":4`, `"model":"deepseek-v4.1-flash"`, `"sim":true`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the run.start request %q dropped a chosen optional %q; an "+
				"optional the caller set must reach the core, or the setting is a "+
				"no-op that lies to the caller", sent, want)
		}
	}
}
