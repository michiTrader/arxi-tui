package driver

import (
	"context"
	"strings"
	"testing"
)

// TestInboxApproveReturnsTheJobSnapshot pins the happy path: an inbox.approve
// answered ok with a Job must surface the run's id and the lifecycle state the
// decision left it in. Approving an item unblocks the run, so the ack shows the
// run running again -- the host reads status/terminal to see the decision took
// hold, not to re-derive the run's budget (that is run.show's job). The wire
// shape is arxi/host/v1 Job (id, status, terminal), the same struct run.cancel
// and run.show return.
func TestInboxApproveReturnsTheJobSnapshot(t *testing.T) {
	resp := `{"id":"approve","ok":true,"result":{"id":"run-7","status":"running","terminal":false}}`
	d := session(t, resp)

	res, err := d.SubmitInboxApprove(context.Background(), InboxApproveParams{RunID: "run-7", ItemID: "approval-1"})
	if err != nil {
		t.Fatalf("inbox.approve answered ok with a Job and SubmitInboxApprove still "+
			"reported an error: %v. The result is the run's state after the item was "+
			"approved, which the host reads to know the decision was recorded", err)
	}
	if res.JobID != "run-7" {
		t.Fatalf("SubmitInboxApprove returned id %q, want %q; the host answered one "+
			"run's item and must confirm the acknowledgement names it", res.JobID, "run-7")
	}
	if res.Status != "running" || res.Terminal {
		t.Fatalf("SubmitInboxApprove returned status=%q terminal=%v, want running and "+
			"false; an approved item unblocks the run, and dropping that state makes "+
			"a resumed run read as unchanged", res.Status, res.Terminal)
	}
}

// TestInboxRejectReturnsTheJobSnapshot is the reject counterpart: a rejection
// answered ok returns the same Job snapshot, here a run the rejection finished.
// Reject is the sibling of approve, so it must surface the run's resulting state
// the same way -- a rejected approval can terminate the run, and terminal=true
// is exactly the state the ack must not collapse into "still running".
func TestInboxRejectReturnsTheJobSnapshot(t *testing.T) {
	resp := `{"id":"reject","ok":true,"result":{"id":"run-7","status":"failed","terminal":true}}`
	d := session(t, resp)

	res, err := d.SubmitInboxReject(context.Background(), InboxRejectParams{RunID: "run-7", ItemID: "approval-1"})
	if err != nil {
		t.Fatalf("inbox.reject answered ok with a Job and SubmitInboxReject still "+
			"reported an error: %v", err)
	}
	if res.JobID != "run-7" {
		t.Fatalf("SubmitInboxReject returned id %q, want %q", res.JobID, "run-7")
	}
	if res.Status != "failed" || !res.Terminal {
		t.Fatalf("SubmitInboxReject returned status=%q terminal=%v, want failed and "+
			"true; a rejection can end the run, and losing that makes a finished run "+
			"read as still awaiting the decision", res.Status, res.Terminal)
	}
}

// TestInboxApproveRefusalIsReturnedAsAnError holds inbox.approve to the same
// contract every verb keeps: ok:false is an answered refusal, not a transport
// success. An approve against an item that no longer exists answers not_found,
// and that must reach the caller as a *Refusal carrying the verb -- a nil error
// here makes a rejected decision read as an accepted one, and the host would
// believe it unblocked a run it never touched.
func TestInboxApproveRefusalIsReturnedAsAnError(t *testing.T) {
	refusal := `{"id":"approve","ok":false,"error":{"code":"not_found","message":"no such item","fix":["arxi inbox"]}}`
	d := session(t, refusal)

	res, err := d.SubmitInboxApprove(context.Background(), InboxApproveParams{RunID: "run-7", ItemID: "gone"})
	if err == nil {
		t.Fatal("the core refused inbox.approve and SubmitInboxApprove reported " +
			"success. ok:false is an answer, not a transport success; a nil error " +
			"here makes a rejected decision read as an accepted one")
	}
	if res != nil {
		t.Fatalf("SubmitInboxApprove returned a non-nil result alongside a refusal: "+
			"%+v. A refused decision changed no run's state", res)
	}
	ref, ok := err.(*Refusal)
	if !ok {
		t.Fatalf("SubmitInboxApprove returned a %T, want *Refusal: the code is a "+
			"closed set the host branches on, and folding it into a plain error "+
			"throws that branch away", err)
	}
	if ref.Code != "not_found" || ref.Type != "inbox.approve" {
		t.Fatalf("the refusal carried code=%q type=%q, want not_found and "+
			"inbox.approve; a refusal with no verb in it is not addressable",
			ref.Code, ref.Type)
	}
}

// TestInboxRejectRefusalIsReturnedAsAnError is the reject counterpart, and it
// pins the one thing the approve refusal test cannot: that the refusal carries
// `inbox.reject`, not `inbox.approve`. The shared submitDecision helper is told
// the verb by its caller, so a caller passing the wrong verb string would label
// every reject refusal as an approve -- this is the counterfactual for that
// argument.
func TestInboxRejectRefusalIsReturnedAsAnError(t *testing.T) {
	refusal := `{"id":"reject","ok":false,"error":{"code":"not_found","message":"no such item","fix":["arxi inbox"]}}`
	d := session(t, refusal)

	res, err := d.SubmitInboxReject(context.Background(), InboxRejectParams{RunID: "run-7", ItemID: "gone"})
	if err == nil {
		t.Fatal("the core refused inbox.reject and SubmitInboxReject reported success")
	}
	if res != nil {
		t.Fatalf("SubmitInboxReject returned a result alongside a refusal: %+v", res)
	}
	ref, ok := err.(*Refusal)
	if !ok {
		t.Fatalf("SubmitInboxReject returned a %T, want *Refusal", err)
	}
	if ref.Type != "inbox.reject" {
		t.Fatalf("the refusal carried type=%q, want inbox.reject; the shared decision "+
			"helper must echo the verb its caller sent, or a reject refusal is "+
			"misfiled as an approve", ref.Type)
	}
}

// TestInboxApproveEmptyRunIDIsRefusedLocally pins the run-id guard that never
// reaches the wire: a decision is authorized against the run it belongs to, so
// an empty run addresses no run. The core's decisionIdentity would refuse it,
// but refusing locally names the real cause at the call site. The response is a
// valid ok so a driver that skipped the guard would send the request.
func TestInboxApproveEmptyRunIDIsRefusedLocally(t *testing.T) {
	resp := `{"id":"approve","ok":true,"result":{"id":"run-1","status":"running","terminal":false}}`
	rec := &recordingWriter{}
	d := sessionWithWriter(t, rec, resp)

	res, err := d.SubmitInboxApprove(context.Background(), InboxApproveParams{RunID: "", ItemID: "approval-1"})
	if err == nil {
		t.Fatal("SubmitInboxApprove sent an inbox.approve with an empty run id. A " +
			"decision is job-scoped; letting it reach the core borrows a refusal that " +
			"misnames the cause")
	}
	if res != nil {
		t.Fatalf("SubmitInboxApprove returned a result for an unaddressed decision: %+v", res)
	}
	if strings.Contains(rec.String(), "inbox.approve") {
		t.Fatalf("SubmitInboxApprove encoded a request %q despite the empty run id; "+
			"the guard must refuse before the wire", rec.String())
	}
}

// TestInboxApproveEmptyItemIDIsRefusedLocally pins the second identity guard: an
// approval answers one item, so an empty item id answers nothing. This is a
// distinct guard from the run-id one -- a valid run with no item is still
// unaddressable -- so it needs its own counterfactual, or removing it passes on
// the run-id test alone.
func TestInboxApproveEmptyItemIDIsRefusedLocally(t *testing.T) {
	resp := `{"id":"approve","ok":true,"result":{"id":"run-1","status":"running","terminal":false}}`
	rec := &recordingWriter{}
	d := sessionWithWriter(t, rec, resp)

	res, err := d.SubmitInboxApprove(context.Background(), InboxApproveParams{RunID: "run-1", ItemID: ""})
	if err == nil {
		t.Fatal("SubmitInboxApprove sent an inbox.approve with an empty item id. An " +
			"approval with a run but no item answers no decision")
	}
	if res != nil {
		t.Fatalf("SubmitInboxApprove returned a result for an itemless decision: %+v", res)
	}
	if strings.Contains(rec.String(), "inbox.approve") {
		t.Fatalf("SubmitInboxApprove encoded a request %q despite the empty item id",
			rec.String())
	}
}

// TestInboxRejectEmptyRunIDIsRefusedLocally and its item sibling pin that
// reject carries its OWN copy of both guards: the guards are duplicated in each
// caller (not in the shared helper), so an approve test proves nothing about
// reject. Removing either reject guard passes every approve test and fails here.
func TestInboxRejectEmptyRunIDIsRefusedLocally(t *testing.T) {
	resp := `{"id":"reject","ok":true,"result":{"id":"run-1","status":"running","terminal":false}}`
	rec := &recordingWriter{}
	d := sessionWithWriter(t, rec, resp)

	if _, err := d.SubmitInboxReject(context.Background(), InboxRejectParams{RunID: "", ItemID: "approval-1"}); err == nil {
		t.Fatal("SubmitInboxReject sent an inbox.reject with an empty run id")
	}
	if strings.Contains(rec.String(), "inbox.reject") {
		t.Fatalf("SubmitInboxReject encoded a request %q despite the empty run id",
			rec.String())
	}
}

func TestInboxRejectEmptyItemIDIsRefusedLocally(t *testing.T) {
	resp := `{"id":"reject","ok":true,"result":{"id":"run-1","status":"running","terminal":false}}`
	rec := &recordingWriter{}
	d := sessionWithWriter(t, rec, resp)

	if _, err := d.SubmitInboxReject(context.Background(), InboxRejectParams{RunID: "run-1", ItemID: ""}); err == nil {
		t.Fatal("SubmitInboxReject sent an inbox.reject with an empty item id")
	}
	if strings.Contains(rec.String(), "inbox.reject") {
		t.Fatalf("SubmitInboxReject encoded a request %q despite the empty item id",
			rec.String())
	}
}

// TestInboxApproveOkWithNoJobIDFailsLoud pins the one ok shape that must not be
// accepted: ok:true with an empty id. The host answered one run's item and got
// an acknowledgement that names no run, so it cannot confirm the decision landed
// on the intended run. This lives in the shared submitDecision helper, so the
// approve path exercises it for both verbs.
func TestInboxApproveOkWithNoJobIDFailsLoud(t *testing.T) {
	resp := `{"id":"approve","ok":true,"result":{"id":"","status":"running","terminal":false}}`
	d := session(t, resp)

	res, err := d.SubmitInboxApprove(context.Background(), InboxApproveParams{RunID: "run-7", ItemID: "approval-1"})
	if err == nil {
		t.Fatal("inbox.approve answered ok with an empty id and SubmitInboxApprove " +
			"accepted it. An acknowledgement that names no run cannot confirm the " +
			"intended run's item was the one decided; a decision the host cannot " +
			"verify is worse than a refusal, which at least says so")
	}
	if res != nil {
		t.Fatalf("SubmitInboxApprove returned a result it could not verify: %+v", res)
	}
}

// TestInboxApproveSendsRunAndItem pins the request wire: the run id is sent as
// the param `run` and the item id as `item` (the name the core's itemParam reads
// first), under type inbox.approve. Getting either name wrong sends a request
// the core reads as unaddressed even though the caller supplied both.
func TestInboxApproveSendsRunAndItem(t *testing.T) {
	resp := `{"id":"approve","ok":true,"result":{"id":"run-1","status":"running","terminal":false}}`
	rec := &recordingWriter{}
	d := sessionWithWriter(t, rec, resp)

	if _, err := d.SubmitInboxApprove(context.Background(), InboxApproveParams{RunID: "run-1", ItemID: "q1"}); err != nil {
		t.Fatalf("inbox.approve happy path errored: %v", err)
	}

	sent := rec.String()
	for _, want := range []string{`"run":"run-1"`, `"item":"q1"`, `"type":"inbox.approve"`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the inbox.approve request %q is missing required field %q; the "+
				"run and item are the identities the core's decision dispatch reads to "+
				"address the item", sent, want)
		}
	}
}

// TestInboxRejectOmitsUnsetReason pins that reject omits reason when the caller
// did not set one: an omitted reason records nothing, while an empty string is
// operator text the caller never wrote. Same contract run.cancel keeps.
func TestInboxRejectOmitsUnsetReason(t *testing.T) {
	resp := `{"id":"reject","ok":true,"result":{"id":"run-1","status":"failed","terminal":true}}`
	rec := &recordingWriter{}
	d := sessionWithWriter(t, rec, resp)

	if _, err := d.SubmitInboxReject(context.Background(), InboxRejectParams{RunID: "run-1", ItemID: "q1"}); err != nil {
		t.Fatalf("inbox.reject happy path errored: %v", err)
	}

	sent := rec.String()
	for _, want := range []string{`"run":"run-1"`, `"item":"q1"`, `"type":"inbox.reject"`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the inbox.reject request %q is missing required field %q", sent, want)
		}
	}
	if strings.Contains(sent, `"reason"`) {
		t.Fatalf("the inbox.reject request %q sent an unset reason; an omitted reason "+
			"records nothing, while an empty string is operator text the caller never "+
			"wrote", sent)
	}
}

// TestInboxRejectSendsSetReason is the counterfactual of the omission test: when
// the caller DOES supply a reason it must reach the wire. Without this the
// omit-when-unset logic could pass by never sending reason at all.
func TestInboxRejectSendsSetReason(t *testing.T) {
	resp := `{"id":"reject","ok":true,"result":{"id":"run-1","status":"failed","terminal":true}}`
	rec := &recordingWriter{}
	d := sessionWithWriter(t, rec, resp)

	if _, err := d.SubmitInboxReject(context.Background(), InboxRejectParams{
		RunID:  "run-1",
		ItemID: "q1",
		Reason: "the diff touches production config",
	}); err != nil {
		t.Fatalf("inbox.reject with a reason errored: %v", err)
	}

	sent := rec.String()
	if !strings.Contains(sent, `"reason":"the diff touches production config"`) {
		t.Fatalf("the inbox.reject request %q dropped the supplied reason; a reason the "+
			"caller set must reach the core, or the setting is a no-op that lies to "+
			"the caller", sent)
	}
}

// TestInboxReplyReturnsTheJobSnapshot pins the reply happy path: answering a
// question item ok returns the same narrow Job snapshot approve and reject do.
// Answering a question unblocks the run, so the ack shows it running again --
// reply is the third caller of the shared submitDecision helper and must surface
// status/terminal the same way its siblings do.
func TestInboxReplyReturnsTheJobSnapshot(t *testing.T) {
	resp := `{"id":"reply","ok":true,"result":{"id":"run-7","status":"running","terminal":false}}`
	d := session(t, resp)

	res, err := d.SubmitInboxReply(context.Background(), InboxReplyParams{RunID: "run-7", ItemID: "q1", Text: "use the staging bucket"})
	if err != nil {
		t.Fatalf("inbox.reply answered ok with a Job and SubmitInboxReply still "+
			"reported an error: %v", err)
	}
	if res.JobID != "run-7" {
		t.Fatalf("SubmitInboxReply returned id %q, want %q", res.JobID, "run-7")
	}
	if res.Status != "running" || res.Terminal {
		t.Fatalf("SubmitInboxReply returned status=%q terminal=%v, want running and "+
			"false; an answered question unblocks the run, and dropping that state "+
			"makes a resumed run read as unchanged", res.Status, res.Terminal)
	}
}

// TestInboxReplyRefusalIsReturnedAsAnError pins the one thing reply's own tests
// must prove and the approve/reject ones cannot: the shared helper echoes
// `inbox.reply` back on a refusal, not a sibling verb. Told the wrong verb
// string, the helper would misfile every reply refusal -- this is the
// counterfactual for reply's caller passing the right one.
func TestInboxReplyRefusalIsReturnedAsAnError(t *testing.T) {
	refusal := `{"id":"reply","ok":false,"error":{"code":"not_found","message":"no such item","fix":["arxi inbox"]}}`
	d := session(t, refusal)

	res, err := d.SubmitInboxReply(context.Background(), InboxReplyParams{RunID: "run-7", ItemID: "gone", Text: "hi"})
	if err == nil {
		t.Fatal("the core refused inbox.reply and SubmitInboxReply reported success")
	}
	if res != nil {
		t.Fatalf("SubmitInboxReply returned a result alongside a refusal: %+v", res)
	}
	ref, ok := err.(*Refusal)
	if !ok {
		t.Fatalf("SubmitInboxReply returned a %T, want *Refusal", err)
	}
	if ref.Type != "inbox.reply" {
		t.Fatalf("the refusal carried type=%q, want inbox.reply; the shared decision "+
			"helper must echo the verb its caller sent, or a reply refusal is "+
			"misfiled as an approve or reject", ref.Type)
	}
}

// TestInboxReplyEmptyRunIDIsRefusedLocally and its item sibling pin that reply
// carries its OWN copy of both identity guards, duplicated in the caller like
// approve and reject: an approve or reject test proves nothing about reply.
func TestInboxReplyEmptyRunIDIsRefusedLocally(t *testing.T) {
	resp := `{"id":"reply","ok":true,"result":{"id":"run-1","status":"running","terminal":false}}`
	rec := &recordingWriter{}
	d := sessionWithWriter(t, rec, resp)

	if _, err := d.SubmitInboxReply(context.Background(), InboxReplyParams{RunID: "", ItemID: "q1", Text: "hi"}); err == nil {
		t.Fatal("SubmitInboxReply sent an inbox.reply with an empty run id")
	}
	if strings.Contains(rec.String(), "inbox.reply") {
		t.Fatalf("SubmitInboxReply encoded a request %q despite the empty run id",
			rec.String())
	}
}

func TestInboxReplyEmptyItemIDIsRefusedLocally(t *testing.T) {
	resp := `{"id":"reply","ok":true,"result":{"id":"run-1","status":"running","terminal":false}}`
	rec := &recordingWriter{}
	d := sessionWithWriter(t, rec, resp)

	if _, err := d.SubmitInboxReply(context.Background(), InboxReplyParams{RunID: "run-1", ItemID: "", Text: "hi"}); err == nil {
		t.Fatal("SubmitInboxReply sent an inbox.reply with an empty item id")
	}
	if strings.Contains(rec.String(), "inbox.reply") {
		t.Fatalf("SubmitInboxReply encoded a request %q despite the empty item id",
			rec.String())
	}
}

// TestInboxReplySendsRunItemAndText pins the request wire: run as `run`, item as
// `item`, and the answer as `text`, under type inbox.reply. The text is the
// param that distinguishes reply from its siblings, so a dropped or misnamed
// text sends an empty answer even though the caller supplied one.
func TestInboxReplySendsRunItemAndText(t *testing.T) {
	resp := `{"id":"reply","ok":true,"result":{"id":"run-1","status":"running","terminal":false}}`
	rec := &recordingWriter{}
	d := sessionWithWriter(t, rec, resp)

	if _, err := d.SubmitInboxReply(context.Background(), InboxReplyParams{RunID: "run-1", ItemID: "q1", Text: "use the staging bucket"}); err != nil {
		t.Fatalf("inbox.reply happy path errored: %v", err)
	}

	sent := rec.String()
	for _, want := range []string{`"run":"run-1"`, `"item":"q1"`, `"text":"use the staging bucket"`, `"type":"inbox.reply"`} {
		if !strings.Contains(sent, want) {
			t.Fatalf("the inbox.reply request %q is missing required field %q; run and "+
				"item address the item and text is the answer the core records", sent, want)
		}
	}
}

// TestInboxReplySendsEmptyTextUnconditionally is the load-bearing distinction
// between reply's text and reject's reason: text is ALWAYS sent, even empty,
// where reject omits an unset reason. The two look interchangeable until this
// probe -- reason is metadata about the act of rejecting, so its absence records
// nothing; text is the substance of the answer, so "the operator submitted an
// empty reply" is a fact the wire must carry, not an omission the core reads as
// no text field. Reusing reject's omit-when-empty logic here would silently drop
// an empty answer, and the counterfactual is to send Text: "" and require
// `"text":""` on the wire.
func TestInboxReplySendsEmptyTextUnconditionally(t *testing.T) {
	resp := `{"id":"reply","ok":true,"result":{"id":"run-1","status":"running","terminal":false}}`
	rec := &recordingWriter{}
	d := sessionWithWriter(t, rec, resp)

	if _, err := d.SubmitInboxReply(context.Background(), InboxReplyParams{RunID: "run-1", ItemID: "q1", Text: ""}); err != nil {
		t.Fatalf("inbox.reply with an empty text errored: %v; empty text is a valid "+
			"answer the core accepts, so it must not be refused locally", err)
	}

	sent := rec.String()
	if !strings.Contains(sent, `"text":""`) {
		t.Fatalf("the inbox.reply request %q dropped an empty text; text is the answer, "+
			"not optional metadata like reject's reason, so an empty reply must reach "+
			"the wire as \"text\":\"\" rather than be omitted", sent)
	}
}
