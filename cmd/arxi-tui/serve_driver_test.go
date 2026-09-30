package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// fakeFollower is a followFunc that records the paths it was asked to follow
// and the per-run contexts it was handed, and returns a channel the test drives.
// It stands in for driver.LogFollow so serveDriver's deferred-run.start
// sequencing is proven without a subprocess or a real file on disk.
type fakeFollower struct {
	mu    sync.Mutex
	paths []string
	ctxs  []context.Context
	chans []chan fold.Event
	err   error
}

func (f *fakeFollower) follow(ctx context.Context, path string) (<-chan fold.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, path)
	f.ctxs = append(f.ctxs, ctx)
	if f.err != nil {
		return nil, f.err
	}
	ch := make(chan fold.Event, 4)
	f.chans = append(f.chans, ch)
	return ch, nil
}

func (f *fakeFollower) followCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.paths)
}

// newTestServeDriver builds a serveDriver with the given run starter and
// follower and a closer that records it was called, so a test never touches a
// real process or channel from openServeDriver.
func newTestServeDriver(rs runStarter, ff *fakeFollower, getenv func(string) string, closed *bool) *serveDriver {
	return &serveDriver{
		rs:       rs,
		getenv:   getenv,
		runsRoot: "/root/runs",
		follow:   ff.follow,
		relay:    make(chan fold.Event, 8),
		closer: func() error {
			*closed = true
			return nil
		},
	}
}

// TestServeDriverStartsRunAndRelaysItsLog is the whole live half in one pass:
// SubmitPrompt begins a run, follows the log path derived from the returned
// job_id, and relays that run's events onto the channel the loop reads. This is
// what M2 delivers -- the TUI follows the run it created -- so it is pinned end
// to end against fakes.
func TestServeDriverStartsRunAndRelaysItsLog(t *testing.T) {
	rs := &fakeRunStarter{hello: implementingHello(), jobID: "run-abc"}
	ff := &fakeFollower{}
	var closed bool
	sd := newTestServeDriver(rs, ff, emptyEnv, &closed)

	if err := sd.SubmitPrompt(context.Background(), "hello there"); err != nil {
		t.Fatalf("SubmitPrompt refused a clean sequence: %v; an implementing kernel with a returned "+
			"job_id is exactly the case that must begin a run and follow its log", err)
	}

	// The path followed must be the one derived from the created run's job_id --
	// not a boot-time guess -- so the TUI follows the run it just started.
	if ff.followCount() != 1 {
		t.Fatalf("SubmitPrompt followed %d logs, want exactly 1 (the run it created)", ff.followCount())
	}
	if !strings.Contains(ff.paths[0], "run-abc") {
		t.Fatalf("followed %q, which does not name the created run-abc; the TUI would follow the "+
			"wrong run's log or none", ff.paths[0])
	}
	if sd.actorLabel != defaultActor {
		t.Fatalf("actorLabel = %q, want the resolved default %q; it is captured for the status bar "+
			"so a plug-and-play default is never invisible", sd.actorLabel, defaultActor)
	}

	// An event written to the run's log must reach the loop through the relay.
	ff.chans[0] <- fold.Event{Type: "run.started", Seq: 1}
	select {
	case ev := <-sd.relay:
		if ev.Type != "run.started" {
			t.Fatalf("relayed event type = %q, want run.started; the run's log events must reach the "+
				"loop unchanged", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("an event written to the run's log never reached the relay; the follow goroutine is " +
			"not copying onto the channel the loop reads")
	}
}

// TestServeDriverActorLabelIsEmptyUntilARunStarts pins the getter the repaint
// loop reads to publish host.run.actor: it is empty before any run (so the
// status label does not draw at boot), and after SubmitPrompt it returns the
// actor the run was started with. The empty half is the counterfactual -- if
// ActorLabel returned the resolved default before a prompt, the status bar
// would name a run that does not exist yet.
func TestServeDriverActorLabelIsEmptyUntilARunStarts(t *testing.T) {
	rs := &fakeRunStarter{hello: implementingHello(), jobID: "run-abc"}
	ff := &fakeFollower{}
	var closed bool
	sd := newTestServeDriver(rs, ff, emptyEnv, &closed)

	if got := sd.ActorLabel(); got != "" {
		t.Fatalf("ActorLabel() = %q before any run, want empty; the status bar would name a run that has not started", got)
	}

	if err := sd.SubmitPrompt(context.Background(), "hello there"); err != nil {
		t.Fatalf("SubmitPrompt refused a clean sequence: %v", err)
	}

	if got := sd.ActorLabel(); got != defaultActor {
		t.Fatalf("ActorLabel() = %q after a run started, want the resolved default %q; the loop publishes "+
			"this into host.run.actor, so a wrong value mislabels the run on screen", got, defaultActor)
	}
}

// the live side: if startRun refuses (a not_implemented kernel, a malformed
// config), SubmitPrompt must return the error and NOT arm log-follow -- following
// a run that was never created would wait forever on a file no run writes.
func TestServeDriverDoesNotFollowWhenRunStartRefused(t *testing.T) {
	rs := &fakeRunStarter{
		hello: &driver.Hello{
			Type:        "hello",
			Types:       []string{"run.start", "schema"},
			Implemented: []string{"schema"}, // declared, not implemented
		},
		jobID: "run-should-not-be-used",
	}
	ff := &fakeFollower{}
	var closed bool
	sd := newTestServeDriver(rs, ff, emptyEnv, &closed)

	if err := sd.SubmitPrompt(context.Background(), "hello"); err == nil {
		t.Fatal("SubmitPrompt accepted a kernel that declares run.start but does not implement it; " +
			"the follow would wait forever on a log no run creates")
	}
	if ff.followCount() != 0 {
		t.Fatalf("SubmitPrompt armed log-follow (%d paths) after startRun refused; the gate must run "+
			"before any follow, or the TUI follows a run it never started", ff.followCount())
	}
}

// TestServeDriverReportsAFollowFailure is the follow-error path: run.start
// created the run, but arming log-follow failed. The error must name the log
// path so the failure points at the file, not just "something went wrong".
func TestServeDriverReportsAFollowFailure(t *testing.T) {
	rs := &fakeRunStarter{hello: implementingHello(), jobID: "run-abc"}
	ff := &fakeFollower{err: context.DeadlineExceeded}
	var closed bool
	sd := newTestServeDriver(rs, ff, emptyEnv, &closed)

	err := sd.SubmitPrompt(context.Background(), "hello")
	if err == nil {
		t.Fatal("SubmitPrompt hid a log-follow failure and reported success; a run whose log cannot " +
			"be followed shows the user nothing, so the failure must surface")
	}
	if !strings.Contains(err.Error(), "run-abc") {
		t.Fatalf("the follow-failure error did not name the run/log, so the user cannot locate it: %v", err)
	}
}

// TestServeDriverSecondPromptCancelsTheFirstFollow pins the one-run-per-turn
// swap (M1c option a): each SubmitPrompt starts a fresh run, and the previous
// run's follow is cancelled first so two runs' logs never relay at once. The
// proof is that the first run's context is cancelled by the second prompt.
func TestServeDriverSecondPromptCancelsTheFirstFollow(t *testing.T) {
	rs := &fakeRunStarter{hello: implementingHello(), jobID: "run-1"}
	ff := &fakeFollower{}
	var closed bool
	sd := newTestServeDriver(rs, ff, emptyEnv, &closed)

	if err := sd.SubmitPrompt(context.Background(), "first"); err != nil {
		t.Fatalf("first SubmitPrompt failed: %v", err)
	}
	firstCtx := ff.ctxs[0]

	if err := sd.SubmitPrompt(context.Background(), "second"); err != nil {
		t.Fatalf("second SubmitPrompt failed: %v", err)
	}

	select {
	case <-firstCtx.Done():
		// The first run's follow was cancelled, as required.
	case <-time.After(2 * time.Second):
		t.Fatal("a second prompt did not cancel the first run's follow; both runs' logs would relay " +
			"onto the channel at once, interleaving two transcripts")
	}
	if ff.followCount() != 2 {
		t.Fatalf("two prompts followed %d logs, want 2 (a fresh run per turn)", ff.followCount())
	}
}

// TestServeDriverCloseCancelsFollowAndCloser pins shutdown: Close cancels the
// current run's follow (so its goroutine exits) and tears down the subprocess
// through the closer. A Close that skipped either would leak a goroutine or a
// process.
func TestServeDriverCloseCancelsFollowAndCloser(t *testing.T) {
	rs := &fakeRunStarter{hello: implementingHello(), jobID: "run-1"}
	ff := &fakeFollower{}
	var closed bool
	sd := newTestServeDriver(rs, ff, emptyEnv, &closed)

	if err := sd.SubmitPrompt(context.Background(), "hello"); err != nil {
		t.Fatalf("SubmitPrompt failed: %v", err)
	}
	runCtx := ff.ctxs[0]

	if err := sd.Close(); err != nil {
		t.Fatalf("Close errored: %v", err)
	}
	if !closed {
		t.Fatal("Close did not call the closer; the arxi serve subprocess would be left running")
	}
	select {
	case <-runCtx.Done():
		// The run's follow context was cancelled on Close.
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not cancel the run's follow; the relay goroutine would leak past shutdown")
	}
}

// fakeInboxSubmitter records the decision-verb calls serveDriver made, so a test
// can assert the run id serveDriver supplied and the item/reason/text it passed
// through, without a live arxi serve.
type fakeInboxSubmitter struct {
	verb   string
	runID  string
	itemID string
	reason string
	text   string
	err    error
}

func (f *fakeInboxSubmitter) SubmitInboxApprove(_ context.Context, p driver.InboxApproveParams) (*driver.DecisionResult, error) {
	f.verb, f.runID, f.itemID = "approve", p.RunID, p.ItemID
	return &driver.DecisionResult{JobID: p.RunID}, f.err
}

func (f *fakeInboxSubmitter) SubmitInboxReject(_ context.Context, p driver.InboxRejectParams) (*driver.DecisionResult, error) {
	f.verb, f.runID, f.itemID, f.reason = "reject", p.RunID, p.ItemID, p.Reason
	return &driver.DecisionResult{JobID: p.RunID}, f.err
}

func (f *fakeInboxSubmitter) SubmitInboxReply(_ context.Context, p driver.InboxReplyParams) (*driver.DecisionResult, error) {
	f.verb, f.runID, f.itemID, f.text = "reply", p.RunID, p.ItemID, p.Text
	return &driver.DecisionResult{JobID: p.RunID}, f.err
}

// TestServeDriverAnswersAgainstTheFollowedRun pins that serveDriver supplies the
// run it is following as the decision's run id, so the host names only the item.
// If this fails, an approve/reject/reply would address the wrong run (or none),
// answering an item on a run other than the one the operator is watching.
func TestServeDriverAnswersAgainstTheFollowedRun(t *testing.T) {
	var closed bool
	rs := &fakeRunStarter{hello: implementingHello(), jobID: "run-xyz"}
	ff := &fakeFollower{}
	sd := newTestServeDriver(rs, ff, emptyEnv, &closed)
	inbox := &fakeInboxSubmitter{}
	sd.inbox = inbox

	// A run must exist first: run.start on the first prompt sets the followed run.
	if err := sd.SubmitPrompt(context.Background(), "do the thing"); err != nil {
		t.Fatalf("SubmitPrompt failed: %v", err)
	}

	if err := sd.RejectInboxItem(context.Background(), "item-9", "not safe"); err != nil {
		t.Fatalf("RejectInboxItem errored: %v", err)
	}
	if inbox.verb != "reject" {
		t.Errorf("serveDriver routed to verb %q, want \"reject\"", inbox.verb)
	}
	if inbox.runID != "run-xyz" {
		t.Errorf("serveDriver answered against run %q, want \"run-xyz\" (the followed run); a decision must address the run its item belongs to", inbox.runID)
	}
	if inbox.itemID != "item-9" {
		t.Errorf("serveDriver answered item %q, want \"item-9\"", inbox.itemID)
	}
	if inbox.reason != "not safe" {
		t.Errorf("serveDriver carried reason %q, want \"not safe\"", inbox.reason)
	}
}

// TestServeDriverRefusesADecisionWithNoActiveRun pins that answering before any
// run is followed is refused, not routed to the "last" fallback runID() would
// return. If this fails, a decision made with no run would send inbox.approve to
// whatever run "last" resolves to on disk — a decision against a run the operator
// never addressed.
func TestServeDriverRefusesADecisionWithNoActiveRun(t *testing.T) {
	var closed bool
	rs := &fakeRunStarter{hello: implementingHello(), jobID: "run-xyz"}
	ff := &fakeFollower{}
	sd := newTestServeDriver(rs, ff, emptyEnv, &closed)
	inbox := &fakeInboxSubmitter{}
	sd.inbox = inbox

	// No SubmitPrompt: no run is followed yet.
	err := sd.ApproveInboxItem(context.Background(), "item-9")
	if err == nil {
		t.Fatal("ApproveInboxItem: got nil with no active run, want a refusal; a decision with no run must not fall back to the \"last\" run id")
	}
	if inbox.verb != "" {
		t.Errorf("ApproveInboxItem reached the driver (verb %q) with no active run; it must refuse before submitting", inbox.verb)
	}
}
