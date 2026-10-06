package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// followFunc is the log-follow seam serveDriver uses to stream a created run's
// events. It is driver.LogFollow in production and a fake in the test, so the
// deferred-run.start sequencing is proven without a subprocess or a real file
// on disk -- the one thing only a live arxi serve confirms (the run.start wire
// round-trip and the <runsRoot>/<job_id>/events.ndjson layout appearing on
// disk) stays out of the unit test while everything up to it is pinned here.
type followFunc func(ctx context.Context, logPath string) (<-chan fold.Event, error)

// serveDriver adapts the NDJSON connection to `arxi serve` to the Driver the
// loop expects. It is the impure live half of Block M2: the four network-free
// pieces (SubmitRunStart, resolveRunStartParams, runLogPathForJob,
// requireRunStart) were joined by startRun (#127); this is the code that calls
// startRun against a real subprocess and points log-follow at the run it
// creates.
//
// The load-bearing structural fact is that run.start CREATES a run, and a run's
// event log does not exist until the run does. The old serveDriver armed
// log-follow at boot on a fixed path (ARXI_RUN_DIR or ~/.arxi/runs/last), which
// only worked when a run had already been started OUTSIDE the TUI (e.g.
// `arxi run start`) and the TUI merely attached to its log. On this build there
// is no such external run and no other verb to make one: run.prompt and
// run.steer are both declared-but-unimplemented (M1b), so run.start is the only
// way to begin a run, and it needs the prompt. So the run.start round-trip
// moves OFF boot and ONTO the first user line, and log-follow is armed on the
// path derived from the returned job_id rather than a path guessed before any
// run exists.
//
// Each SubmitPrompt starts a fresh run (M1c option a: one run.start per turn,
// each turn its own run, since this build cannot steer an existing one). The
// previous run's follow goroutine is cancelled before the next is armed, so two
// runs' logs never relay onto the channel at once, and every run's events reach
// the loop through the one relay channel openServeDriver returned as eventCh.
type serveDriver struct {
	// rs is the run.start round-trip and the hello to gate on. It is the
	// runStarter interface, not *driver.NDJSONDriver, so serveDriver's
	// sequencing is testable against a fake; the real driver satisfies it.
	rs runStarter
	// inbox is the decision-verb subset of the same NDJSON driver rs is, used to
	// answer inbox items (inboxDecider). It is a separate narrow seam so the
	// run-id plumbing is testable against a fake without the run.start machinery.
	inbox inboxSubmitter
	// closer tears down the subprocess (kill + reap). It is a func rather than
	// the *exec.Cmd so the test constructs a serveDriver with no process.
	closer func() error
	// getenv resolves the session config (actor/budget/sim/model) through
	// resolveRunStartParams; injected for the same reason run_config's is.
	getenv func(string) string
	// runsRoot is the directory each run's own subdirectory lives under
	// (<runsRoot>/<job_id>/events.ndjson); the follow path is built from it and
	// the returned job_id by runLogPathForJob.
	runsRoot string
	// follow streams a created run's log events (driver.LogFollow in production).
	follow followFunc

	// relay is the single channel the loop reads for the whole session. Each
	// run's follow goroutine copies onto it; it is never closed here, because a
	// closed eventCh tells the loop the session is over, and a session outlives
	// any one run under the one-run-per-turn mapping.
	relay chan fold.Event

	mu sync.Mutex
	// logPath is the current run's event-log path, updated on each SubmitPrompt.
	// runID reads the run id back out of it; keeping it here (rather than a bare
	// per-call local) is what lets a later attach/show/cancel verb (M3) address
	// the run the TUI is currently following.
	logPath string
	// runCancel cancels the current run's follow goroutine, so the next
	// SubmitPrompt's fresh run does not relay alongside the previous one.
	runCancel context.CancelFunc
	// actorLabel is the resolved actor name for the status bar (the label
	// resolveRunStartParams returns). It is held for the deferred status-bar
	// wiring: surfacing it needs a signed host view-state bind and a scene row,
	// which is its own increment -- so it is captured here at the moment it is
	// known rather than recomputed later.
	actorLabel string

	// chat answers plain chat lines with chat.send; hub is the provider/model
	// management seam. Both are nil on a connection that was built without them
	// (tests, the mock).
	chat *chatSession
	hub  hubCore
}

// Hub is the provider/model management seam, or nil when this connection has none.
func (d *serveDriver) Hub() hubCore { return d.hub }

// SubmitPrompt begins a run for the user's line and follows its event log.
//
// It runs the M2 sequence in the one correct, testable order (startRun): gate
// on the hello, resolve the session config, send run.start, derive the follow
// path from the returned job_id. Only then does it arm log-follow -- a run.start
// that was refused (a not_implemented kernel, a malformed ARXI_BUDGET) returns
// before any follow is attempted, so the failure is a named error at submit
// rather than a follow that waits forever on a log no run creates.
func (d *serveDriver) SubmitPrompt(ctx context.Context, text string) error {
	err := d.submitPrompt(ctx, text)
	if err != nil && d.relay != nil {
		// Callers on the typing path cannot show an error, and a swallowed one is
		// a silent dead end: the failure joins the conversation as a chat line, the
		// same place a failed request is shown.
		ev := fold.Event{Type: "chat.error", Actor: "assistant", Payload: map[string]any{"text": err.Error()}}
		select {
		case d.relay <- ev:
		default:
		}
	}
	return err
}

func (d *serveDriver) submitPrompt(ctx context.Context, text string) error {
	if d.chat != nil {
		return d.chat.send(ctx, text)
	}
	logPath, actorLabel, err := startRun(ctx, d.rs, d.getenv, d.runsRoot, text)
	if err != nil {
		return err
	}

	// Cancel the previous run's follow before arming the next, so the two do not
	// relay onto the channel at once. Done under the lock together with the
	// state update so a concurrent SubmitPrompt cannot interleave a half-swap.
	d.mu.Lock()
	prevCancel := d.runCancel
	d.mu.Unlock()
	if prevCancel != nil {
		prevCancel()
	}

	runCtx, cancel := context.WithCancel(ctx)
	runCh, err := d.follow(runCtx, logPath)
	if err != nil {
		cancel()
		return fmt.Errorf(
			"cmd/arxi-tui/serve_driver.go: run.start created run %q but log-follow of %s failed: %w",
			runIDFromLogPath(logPath), logPath, err)
	}

	d.mu.Lock()
	d.logPath = logPath
	d.actorLabel = actorLabel
	d.runCancel = cancel
	d.mu.Unlock()

	go d.relayEvents(runCtx, runCh)
	return nil
}

// relayEvents copies one run's events onto the shared relay until the run's log
// channel closes (LogFollow closes it on ctx cancel or a read error) or the
// context is cancelled. It never closes relay: the session's channel outlives
// any one run, and a closed relay would tell the loop the session ended.
func (d *serveDriver) relayEvents(ctx context.Context, runCh <-chan fold.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-runCh:
			if !ok {
				return
			}
			select {
			case <-ctx.Done():
				return
			case d.relay <- ev:
			}
		}
	}
}

// runID derives the run ID from the current run's log path directory name: a
// run lives at <runsRoot>/<run-id>/events.ndjson, so the parent dir's base is
// the id. It is the inverse of runLogPathForJob, and the round-trip
// runID(runLogPathForJob(root, id)) == id is what guarantees the TUI follows a
// run under the same id it would address it by (attach/show/cancel).
func (d *serveDriver) runID() string {
	d.mu.Lock()
	logPath := d.logPath
	d.mu.Unlock()
	return runIDFromLogPath(logPath)
}

// runIDFromLogPath is runID's pure body, split out so the round-trip with
// runLogPathForJob is pinned without constructing a driver.
func runIDFromLogPath(logPath string) string {
	dir := filepath.Base(filepath.Dir(logPath))
	if dir == "" || dir == "." {
		return "last"
	}
	return dir
}

// ActorLabel returns the resolved actor blueprint of the run the driver is
// currently following, for the status bar (host.run.actor, BINDS.md §4.3). It
// is captured on the SubmitPrompt that started the run -- the host resolves the
// actor from the run.start config it sends, so the label is known a round-trip
// before any run.started could echo it. Empty until the first prompt begins a
// run; read under the lock because SubmitPrompt writes it from its own path.
func (d *serveDriver) ActorLabel() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.actorLabel
}

// SetEffort chooses the thinking level the next chat turns ask for ("" sends
// nothing). It is a setting, so ClearSession leaves it alone.
func (d *serveDriver) SetEffort(level string) {
	if d.chat != nil {
		d.chat.setEffort(level)
	}
}

// SetMode applies an agent mode: what the model may do to files follows it.
func (d *serveDriver) SetMode(name string) {
	if m, ok := modeByName(name); ok && d.chat != nil {
		d.chat.setEdits(m.policy(classEdit))
		d.chat.setRuns(m.policy(classRun))
		d.chat.setWeb(m.policy(classWeb))
	}
}

// PendingApproval reports whether a change waits for the user's answer.
func (d *serveDriver) PendingApproval() bool { return d.chat != nil && d.chat.pendingNow() }

// Decide answers the change that waits and reports whether there was one.
func (d *serveDriver) Decide(allow bool) bool { return d.chat != nil && d.chat.decide(allow) }

// CancelTurn stops the chat turn in flight and reports whether there was one.
func (d *serveDriver) CancelTurn() bool {
	return d.chat != nil && d.chat.cancelTurn()
}

// ClearSession ends the conversation the driver is following and leaves it ready
// for a fresh one: the chat history is forgotten, a run being followed is no
// longer relayed, and anything already queued for the loop is dropped. Nothing
// the core owns is touched: providers, keys and the selected model survive.
func (d *serveDriver) ClearSession() {
	if d.chat != nil {
		d.chat.reset()
	}
	d.mu.Lock()
	cancel := d.runCancel
	d.runCancel = nil
	d.logPath = ""
	d.actorLabel = ""
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	for d.relay != nil {
		select {
		case <-d.relay:
		default:
			return
		}
	}
}

func (d *serveDriver) Close() error {
	d.mu.Lock()
	cancel := d.runCancel
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return d.closer()
}

// inboxSubmitter is the subset of the NDJSON driver serveDriver needs to answer
// inbox items: the three decision verbs (M4, PRs #132/#133). It is a narrow seam
// rather than *driver.NDJSONDriver so serveDriver's run-id plumbing is testable
// against a fake, the same reason rs is a runStarter and not the concrete driver.
type inboxSubmitter interface {
	SubmitInboxApprove(ctx context.Context, p driver.InboxApproveParams) (*driver.DecisionResult, error)
	SubmitInboxReject(ctx context.Context, p driver.InboxRejectParams) (*driver.DecisionResult, error)
	SubmitInboxReply(ctx context.Context, p driver.InboxReplyParams) (*driver.DecisionResult, error)
}

// currentRunID returns the id of the run the driver is following and false when
// no run is active. It is distinct from runID(), which falls back to "last" for
// a log-path derivation: a decision must address the run its item belongs to, and
// "last" is a guess that would send inbox.approve at whatever run that name
// resolves to. An answer with no active run is "nothing to answer", not a
// decision routed to a fallback run — the wrong-run failure this project holds
// worse than a loud refusal (the same reasoning inboxItemID applies to an empty
// item id).
func (d *serveDriver) currentRunID() (string, bool) {
	d.mu.Lock()
	logPath := d.logPath
	d.mu.Unlock()
	if logPath == "" {
		return "", false
	}
	return runIDFromLogPath(logPath), true
}

// ApproveInboxItem answers a pending approval item on the run the driver is
// following. It is serveDriver's half of the inboxDecider capability: the host
// names only the item (inboxItemID sourced it from the blocked_ref), and the
// driver supplies the run — the run is the one it is following, never in question
// at the press. A press with no active run is refused here rather than routed to
// the "last" fallback.
func (d *serveDriver) ApproveInboxItem(ctx context.Context, itemID string) error {
	runID, ok := d.currentRunID()
	if !ok {
		return fmt.Errorf("cmd/arxi-tui/serve_driver.go: cannot approve item %q: no run is being followed, so there is no run the decision belongs to", itemID)
	}
	_, err := d.inbox.SubmitInboxApprove(ctx, driver.InboxApproveParams{RunID: runID, ItemID: itemID})
	return err
}

// RejectInboxItem answers a pending approval item with a rejection, optionally
// carrying the operator's reason. The reason is passed through to the driver,
// which omits it on the wire when empty (an unset reason records nothing).
func (d *serveDriver) RejectInboxItem(ctx context.Context, itemID, reason string) error {
	runID, ok := d.currentRunID()
	if !ok {
		return fmt.Errorf("cmd/arxi-tui/serve_driver.go: cannot reject item %q: no run is being followed, so there is no run the decision belongs to", itemID)
	}
	_, err := d.inbox.SubmitInboxReject(ctx, driver.InboxRejectParams{RunID: runID, ItemID: itemID, Reason: reason})
	return err
}

// ReplyInboxItem answers a pending question item with the operator's free text.
// Unlike a reject's reason the text is always sent (it is the substance of the
// answer, not metadata about the act), a distinction the driver's InboxReplyParams
// carries on the wire.
func (d *serveDriver) ReplyInboxItem(ctx context.Context, itemID, text string) error {
	runID, ok := d.currentRunID()
	if !ok {
		return fmt.Errorf("cmd/arxi-tui/serve_driver.go: cannot reply to item %q: no run is being followed, so there is no run the decision belongs to", itemID)
	}
	_, err := d.inbox.SubmitInboxReply(ctx, driver.InboxReplyParams{RunID: runID, ItemID: itemID, Text: text})
	return err
}

// compile-time assertions: *driver.NDJSONDriver satisfies runStarter (so
// openServeDriver passes the real driver through unchanged), and serveDriver
// satisfies Driver (so the loop never knows which path it is on).
var (
	_ runStarter     = (*driver.NDJSONDriver)(nil)
	_ inboxSubmitter = (*driver.NDJSONDriver)(nil)
	_ chatSender     = (*driver.NDJSONDriver)(nil)
	_ Driver         = (*serveDriver)(nil)
	_ turnCanceller  = (*serveDriver)(nil)
	_ inboxDecider   = (*serveDriver)(nil)
)
