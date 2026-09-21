package driver

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

// The core answers every request, and an answer can be a refusal. serve.go
// makes `ok` an explicit boolean rather than something inferred from the
// presence of `error`, and says why: the two encodings "disagree the first
// time a server omits an empty error object or a client checks the wrong one,
// and the disagreement reads as success."
//
// That is exactly what shipped. SubmitPrompt returned (*protoResponse, nil)
// for a refusal, because at the transport layer nothing had gone wrong -- the
// line was sent, a line came back. And the host's only call site is
//
//	_ = drv.SubmitPrompt(ctx, text)
//
// which drops even that. A refused prompt was swallowed twice over, and the
// user saw their text vanish from the input line with no frame explaining why.
//
// Every refusal below is a line the real `arxi serve` produced; see
// testdata/serve/session.ndjson and the header of
// handshake_against_real_arxi_test.go for how it was captured.

// responseLines returns the recorded refusals, skipping the hello.
func responseLines(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/serve/session.ndjson")
	if err != nil {
		t.Fatalf("read the recorded arxi serve session: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("the recorded session has %d lines; it should have a hello "+
			"plus one response per request", len(lines))
	}
	return lines[1:]
}

// session builds a driver whose reader is the recorded hello followed by the
// given response lines, with the handshake already performed.
func session(t *testing.T, responses ...string) *NDJSONDriver {
	t.Helper()
	wire := helloLine(t) + "\n" + strings.Join(responses, "\n") + "\n"
	d := NewNDJSON(readWriter{r: strings.NewReader(wire), w: io.Discard})
	if err := d.Handshake(context.Background()); err != nil {
		t.Fatalf("handshake against the recorded core: %v", err)
	}
	return d
}

// TestARefusalIsReturnedAsAnError is the defect stated as a requirement.
//
// The refusal used is the one the host will actually receive for the one
// request it sends: `run.prompt` answered `not_implemented`, measured live
// against arxi 0.0.1-spec.
func TestARefusalIsReturnedAsAnError(t *testing.T) {
	refusals := responseLines(t)
	d := session(t, refusals[0])

	_, err := d.SubmitPrompt(context.Background(), "last", "hola")
	if err == nil {
		t.Fatal("the core refused the request and SubmitPrompt reported success. " +
			"`ok` is an explicit boolean precisely so this cannot be ambiguous; " +
			"a client that returns a nil error for ok:false makes the refusal " +
			"read as a delivered prompt, and the host's call site discards the " +
			"response value entirely")
	}
}

// TestTheRefusalCarriesTheCoreCodeAndSentence pins what the error must
// contain, because a refusal reduced to "request failed" is the thing the
// protocol went out of its way to prevent.
//
// serve.go sends a machine `code` AND a human `message` because "the code is
// what a client branches on; the message is what ends up in somebody's
// terminal at 2am." The host needs both: the code to decide whether retrying
// could ever help, the message to put in the frame. It also sends `fix`, the
// same shape of remedy `run why` prints -- and this repo already holds that a
// diagnosis without an address is a diagnosis nobody can act on, which is the
// entire reason scene errors carry a location.
func TestTheRefusalCarriesTheCoreCodeAndSentence(t *testing.T) {
	refusals := responseLines(t)
	d := session(t, refusals[0])

	_, err := d.SubmitPrompt(context.Background(), "last", "hola")
	if err == nil {
		t.Fatal("no error for a refused request")
	}

	got := err.Error()
	for _, want := range []string{
		"not_implemented", // the code the client branches on
		"no executor",     // the core's own sentence, not a paraphrase
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the refusal does not carry %q, so the host cannot tell the "+
				"user what the core actually said:\n%s", want, got)
		}
	}
}

// TestARefusalIsDistinguishableFromATransportFailure is the branch the codes
// exist for.
//
// serve.go calls its error codes "a closed set on purpose: a client has to be
// able to tell 'you asked wrongly' (its own bug, retrying will not help) from
// 'this build cannot do that yet' (not its bug, and retrying after an upgrade
// will help). Collapsing them into one generic failure makes every client
// either retry forever or give up permanently, and both are wrong half the
// time."
//
// A host that wraps every refusal in fmt.Errorf has collapsed them: the code
// survives only as a substring of English. Expose it.
func TestARefusalIsDistinguishableFromATransportFailure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lineIdx  int
		wantCode string
	}{
		// run.prompt: declared in surface v1, no executor in this build.
		{"declared but unimplemented", 0, "not_implemented"},
		// run.nonsense: not a message type at all -- the client is wrong.
		{"not a message type", 1, "unknown_type"},
		// `ifseq` instead of `if_seq`: the CAS guard misspelled. The core
		// refuses unknown parameters rather than ignoring them, which is the
		// only reason a broken compare-and-swap is loud instead of a silent
		// last-write-wins.
		{"misspelled parameter", 2, "bad_params"},
		// run.attach against a job that does not exist. This refusal comes
		// from the core's HOST layer, not its protocol layer, and its code is
		// not in the closed set serve.go documents -- `not_found` is a real
		// answer the wire carries and the documented list does not mention.
		{"host-layer not_found", 3, "not_found"},
		// A line that is not JSON at all.
		{"not a JSON object", 4, "malformed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refusals := responseLines(t)
			if tc.lineIdx >= len(refusals) {
				t.Fatalf("the recorded session has %d responses, wanted index %d",
					len(refusals), tc.lineIdx)
			}
			d := session(t, refusals[tc.lineIdx])

			_, err := d.SubmitPrompt(context.Background(), "last", "hola")
			if err == nil {
				t.Fatalf("ok:false reported as success for %s", tc.name)
			}

			var refusal *Refusal
			if !AsRefusal(err, &refusal) {
				t.Fatalf("the refusal is not recoverable as a Refusal, so the host "+
					"can only match on English: %v", err)
			}
			if refusal.Code != tc.wantCode {
				t.Errorf("code = %q, want %q -- the host branches on this to decide "+
					"whether retrying could ever help", refusal.Code, tc.wantCode)
			}
			if refusal.Message == "" {
				t.Error("no message: the code is for the host, the sentence is for " +
					"the person reading the frame")
			}
			// The remedy. This assertion was missing in the first draft of
			// this file, and an injection sweep caught the omission: welding
			// `Fix: nil` into the refusal constructor broke nothing, because
			// the comments claimed `fix` was carried and no test asserted it.
			// That is the same defect class as a guard that passes without
			// measuring -- a claim in prose with nothing holding it.
			//
			// Every refusal the real core sent carries a fix; serve.go sends
			// it for the reason `run why` prints remedies, and this repo
			// already refuses to ship a diagnosis with no address.
			//
			// The host-layer refusal is the exception, and it is a measurement
			// rather than a guess: `not_found` arrives with an `operation` and
			// no `fix` at all. A test that demanded a remedy from every refusal
			// would be demanding the core change.
			if tc.wantCode != "not_found" && len(refusal.Fix) == 0 {
				t.Errorf("the refusal carries no remedy. The core answered %q "+
					"with a `fix`, and dropping it leaves the user a diagnosis "+
					"with nothing to do about it", tc.wantCode)
			}
			// `operation` names which host operation failed, and it is the only
			// thing distinguishing "the run you named does not exist" from any
			// other not_found. Dropping it survived an injection sweep until
			// this case existed, because no recorded refusal carried the field.
			if tc.wantCode == "not_found" && refusal.Operation == "" {
				t.Error("the host-layer refusal lost its `operation`: the core said " +
					"event.subscribe failed, and without it the message is just " +
					"\"job not found\" with no indication of what was being attempted")
			}
			// not_found describes this attempt, not this build: the run may
			// exist on a later request, so retrying is not provably useless.
			if tc.wantCode == "not_found" && refusal.Permanent() {
				t.Error("not_found reported as permanent: a job that does not exist " +
					"now may exist later, and calling that permanent makes the host " +
					"give up on a run that is merely not started yet")
			}
			// Permanence is the branch that matters to a TUI: a prompt the
			// core will never accept must not look like one that failed to
			// send.
			if tc.wantCode == "not_implemented" && !refusal.Permanent() {
				t.Error("not_implemented is not reported as permanent, so a host " +
					"could retry a request this binary can never answer")
			}
		})
	}
}

// TestAMalformedLineIsRefusedWithAnEmptyID records a detail that would
// otherwise look like a bug in the client.
//
// The core answers an unparseable line with id:"" -- serve.go allows an empty
// id rather than rejecting it, because "errors about a line we could not even
// parse have to carry SOMETHING." A client that required the response id to
// equal the request id would treat that answer as unmatched and wait forever
// for a reply it already has.
func TestAMalformedLineIsRefusedWithAnEmptyID(t *testing.T) {
	refusals := responseLines(t)
	d := session(t, refusals[4])

	resp, err := d.SubmitPrompt(context.Background(), "last", "hola")
	if err == nil {
		t.Fatal("a malformed-line refusal reported as success")
	}
	if resp == nil {
		t.Fatal("the response was not returned alongside the error; the caller " +
			"cannot inspect what the core said")
	}
	if resp.ID != "" {
		t.Errorf("id = %q, want \"\": the refusal for a line the core could not "+
			"parse carries no id, and a client that insists on a match would "+
			"wait forever for an answer it already has", resp.ID)
	}
}

// TestAnOkFalseWithNoErrorObjectIsStillARefusal closes the exact ambiguity
// serve.go names.
//
// `ok` is the field of record. A client that treats a missing error object as
// success has inferred the outcome from the wrong field -- "the disagreement
// reads as success" -- and the refusal becomes a delivered prompt.
func TestAnOkFalseWithNoErrorObjectIsStillARefusal(t *testing.T) {
	d := session(t, `{"id":"p1","ok":false}`)

	_, err := d.SubmitPrompt(context.Background(), "last", "hola")
	if err == nil {
		t.Fatal("ok:false with no error object was read as success. `ok` is the " +
			"field of record; inferring the outcome from the presence of " +
			"`error` is the encoding serve.go refuses to rely on")
	}
	var refusal *Refusal
	if !AsRefusal(err, &refusal) {
		t.Fatalf("not a Refusal: %v", err)
	}
	if refusal.Permanent() {
		t.Error("a refusal with no stated reason was reported as permanent: " +
			"nothing said this build can never do it, so a retry is not " +
			"provably useless")
	}
}
