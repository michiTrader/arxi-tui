package driver

import (
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// The exec.* family is 91 of the 122 events in the measured real run and
// run.result is one more: 92 events, 75.4% of the log, that the fold threw
// away. The host was blank for the entire execution of a run.
//
// These tests measure what the fold now does with them. Every assertion below
// is a figure taken from the recorded core log or a rule read at the core's
// emission site -- not one of them is a round number picked to make a test
// pass, because that is the failure mode this file exists to avoid.

// TestControlWorkDoesNotDriveTheActiveCountNegative is the defect that would
// have shipped, caught by measuring instead of assuming.
//
// The obvious implementation of an in-flight counter is started++ / finished--.
// Against the real log that ends at -7, because 23 works are prepared and
// finish while only 16 ever start. The gap is not lost events: it is the
// core's design. exec.go's runDurableControl handles Emit, SetTimer,
// CancelTimer and Snapshot by calling finishWork directly, so control work
// never crosses the durable start boundary and never emits exec.work_started.
//
// A negative active count is not a cosmetic bug. ExecPhase is derived from it,
// so "working" would be decided by an arithmetic artefact.
func TestControlWorkDoesNotDriveTheActiveCountNegative(t *testing.T) {
	events := replayBytes(t, realRunLog(t))
	state := fold.Fold(events)

	// The run ends with nothing in flight: every started work finished.
	//
	// What this assertion actually discriminates, measured rather than
	// assumed: a counter that never subtracts ends at 16 and fails here. A
	// counter that subtracts for every finish does NOT fail here -- the
	// `ExecActive > 0` clamp absorbs the seven unpaired decrements, and in
	// this log no control work finishes while external work is in flight, so
	// the two implementations agree at all 122 prefixes. That case is
	// separated by TestAControlFinishDuringExternalWorkDoesNotStealTheSlot.
	//
	// The -7 figure below describes an unclamped counter. It is stated as
	// the motivation for the pairing check, not as something this test
	// proves.
	if state.ExecActive != 0 {
		t.Errorf("exec.active = %d at end of a completed run, want 0. The log "+
			"contains 16 exec.work_started and 23 exec.work_finished; a "+
			"counter that never subtracts ends at 16, and an unclamped "+
			"counter that subtracts for every finish ends at -7",
			state.ExecActive)
	}
	if state.ExecPhase != "idle" {
		t.Errorf("exec.phase = %q at end of a completed run, want \"idle\": "+
			"the phase is derived from the active count, so a wrong count "+
			"puts a spinner on screen for a run that finished",
			state.ExecPhase)
	}
}

// TestAControlFinishDuringExternalWorkDoesNotStealTheSlot is the test the
// sweep proved was missing, and the reason it was missing is worth more than
// the test.
//
// TestControlWorkDoesNotDriveTheActiveCountNegative above passes whether the
// decrement is paired with a start or not. The mutation sweep welded the
// pairing check out -- `if id != "" && s.started[id]` -> `if id != ""` -- and
// every test still passed. Replaying both versions over the recorded log
// produces byte-identical active counts at all 122 prefixes.
//
// Two things hid it. The `if s.ExecActive > 0` clamp absorbs the underflow,
// so the count never actually reaches -7; and in the recorded run no control
// work ever finishes while external work is in flight, so the unpaired
// decrement always subtracts from zero. The comment on that test claimed the
// pairing check was what landed the count on 0. That was an assertion about
// the implementation, not a measurement of it -- the clamp was doing the
// work.
//
// This is the separating input: control work (prepared, never started)
// finishing while one external work is genuinely in flight. Correct folding
// holds at 1; the unpaired decrement steals the external work's slot and
// reports 0 while real, possibly paid work is still running.
func TestAControlFinishDuringExternalWorkDoesNotStealTheSlot(t *testing.T) {
	events := []fold.Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{"simulated": false}},
		// External work: prepared, started, in flight.
		{Type: "exec.work_prepared", Seq: 2, Payload: map[string]any{
			"work_id": "ext1", "effect_class": "independent",
		}},
		{Type: "exec.work_started", Seq: 3, Payload: map[string]any{"work_id": "ext1"}},
		// Control work: prepared and finished without ever starting, which is
		// how runDurableControl handles Emit/SetTimer/CancelTimer/Snapshot.
		{Type: "exec.work_prepared", Seq: 4, Payload: map[string]any{
			"work_id": "ctl1", "effect_kind": "emit", "effect_class": "control",
		}},
		{Type: "exec.work_finished", Seq: 5, Payload: map[string]any{
			"work_id": "ctl1", "status": "completed",
		}},
	}

	state := fold.Fold(events)
	if state.ExecActive != 1 {
		t.Errorf("exec.active = %d after control work finished while one "+
			"external work is in flight, want 1. A decrement that is not "+
			"paired with a start it actually saw takes the external work's "+
			"slot, and the host reports an idle run while paid work is still "+
			"running", state.ExecActive)
	}
	if state.ExecPhase != "working" {
		t.Errorf("exec.phase = %q, want \"working\": ext1 has not finished",
			state.ExecPhase)
	}

	// And the external work finishing afterwards must bring it to zero --
	// not below, and not leave it stuck at 1.
	events = append(events, fold.Event{
		Type: "exec.work_finished", Seq: 6,
		Payload: map[string]any{"work_id": "ext1", "status": "completed"},
	})
	state = fold.Fold(events)
	if state.ExecActive != 0 {
		t.Errorf("exec.active = %d once both works finished, want 0",
			state.ExecActive)
	}
	if state.ExecCompleted != 2 {
		t.Errorf("exec.completed = %d, want 2: both the control and the "+
			"external work reached a terminal status", state.ExecCompleted)
	}
}

// TestTheActiveCountIsNeverNegativeAtAnyPointInTheRun is the stronger form.
//
// Checking only the final state would pass for an implementation that swung
// to -7 mid-run and happened to come back. The count is read live by a status
// row, so every intermediate value has to be sane.
func TestTheActiveCountIsNeverNegativeAtAnyPointInTheRun(t *testing.T) {
	events := replayBytes(t, realRunLog(t))

	// ExecActive is uint, so a "negative" shows up as a huge number after
	// wraparound. Both failures are caught by folding each prefix.
	const absurd = 1 << 20
	peak := uint(0)
	for i := 1; i <= len(events); i++ {
		s := fold.Fold(events[:i])
		if s.ExecActive > absurd {
			t.Fatalf("after %d events exec.active = %d: the counter wrapped, "+
				"which is what an unguarded decrement does to a uint when "+
				"control work finishes without starting", i, s.ExecActive)
		}
		if s.ExecActive > peak {
			peak = s.ExecActive
		}
	}

	// The measured peak. Pinned exactly: a counter that never decremented
	// would climb to 16, and one that counted prepared work would climb
	// higher still. 4 is the real concurrency of this run.
	if peak != 4 {
		t.Errorf("peak exec.active = %d, want 4: measured from the recorded "+
			"log by walking every prefix. A different peak means the fold is "+
			"counting something other than work in flight", peak)
	}
}

// TestPreparedWorkIsNotCountedAsInFlight pins the deliberate choice in
// exec.work_prepared's empty case.
//
// Preparation binds an effect; it does not dispatch it. Seven of the 23
// prepared works in the real log are never dispatched at all. Counting
// preparation as activity would claim work in flight that the executor never
// started.
func TestPreparedWorkIsNotCountedAsInFlight(t *testing.T) {
	events := []fold.Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{"simulated": true}},
		{Type: "exec.work_prepared", Seq: 2, Payload: map[string]any{
			"work_id": "w1", "effect_kind": "emit", "effect_class": "control",
		}},
	}
	state := fold.Fold(events)

	if state.ExecActive != 0 {
		t.Errorf("exec.active = %d after a prepare with no start, want 0: "+
			"preparation is not dispatch, and control work is prepared and "+
			"finished without ever starting", state.ExecActive)
	}
	if state.ExecPhase != "idle" {
		t.Errorf("exec.phase = %q, want \"idle\": a prepared-but-not-started "+
			"work is not the run doing something", state.ExecPhase)
	}

	// And the control work finishing must not underflow.
	events = append(events, fold.Event{
		Type: "exec.work_finished", Seq: 3,
		Payload: map[string]any{"work_id": "w1", "status": "completed"},
	})
	state = fold.Fold(events)
	if state.ExecActive != 0 {
		t.Errorf("exec.active = %d after control work finished without ever "+
			"starting, want 0: this is the -7 defect in miniature",
			state.ExecActive)
	}
	if state.ExecCompleted != 1 {
		t.Errorf("exec.completed = %d, want 1: the work did finish, and not "+
			"counting its outcome is the opposite overcorrection",
			state.ExecCompleted)
	}
}

// TestUnknownOutcomeIsNotFoldedIntoFailed is a correctness claim about
// meaning, not about counting.
//
// The core's status vocabulary is exactly {completed, failed, unknown} --
// progress.go refuses anything else. "unknown" is not a soft "failed":
// exec.go's ErrUnknownWork means an external dispatch crossed its durable
// start boundary with no committed terminal outcome, so retrying could
// duplicate paid or mutating work. The core stops the run rather than guess.
//
// A host that rendered that as a failure would assert something the core
// explicitly refuses to assert -- the same class of error as labelling a
// simulated run live.
func TestUnknownOutcomeIsNotFoldedIntoFailed(t *testing.T) {
	events := []fold.Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{"simulated": false}},
		{Type: "exec.work_started", Seq: 2, Payload: map[string]any{"work_id": "w1"}},
		{Type: "exec.work_finished", Seq: 3, Payload: map[string]any{
			"work_id": "w1", "status": "unknown",
		}},
		{Type: "exec.work_started", Seq: 4, Payload: map[string]any{"work_id": "w2"}},
		{Type: "exec.work_finished", Seq: 5, Payload: map[string]any{
			"work_id": "w2", "status": "failed",
		}},
	}
	state := fold.Fold(events)

	if state.ExecUnknown != 1 {
		t.Errorf("exec.unknown = %d, want 1: the core distinguishes an "+
			"ambiguous outcome from a failed one because retrying the first "+
			"could double-charge, and a host that collapses them is claiming "+
			"knowledge the core withheld", state.ExecUnknown)
	}
	if state.ExecFailed != 1 {
		t.Errorf("exec.failed = %d, want 1: the genuinely failed work must "+
			"still be counted, or the fix removed the distinction instead of "+
			"honouring it", state.ExecFailed)
	}
	if state.ExecCompleted != 0 {
		t.Errorf("exec.completed = %d, want 0: neither outcome above "+
			"succeeded", state.ExecCompleted)
	}
}

// TestTheRealLogTerminalStatusesAreCountedExactly pins the measured figures.
//
// All 23 finishes in the recorded run carry status "completed". Asserting the
// exact number rather than "> 0" is deliberate: a fold that counted every
// exec.* event as a completion would pass a non-zero check and put 91 on
// screen.
func TestTheRealLogTerminalStatusesAreCountedExactly(t *testing.T) {
	events := replayBytes(t, realRunLog(t))
	state := fold.Fold(events)

	if state.ExecCompleted != 23 {
		t.Errorf("exec.completed = %d, want 23: the recorded log contains "+
			"exactly 23 exec.work_finished events and every one of them "+
			"carries status \"completed\"", state.ExecCompleted)
	}
	if state.ExecFailed != 0 || state.ExecUnknown != 0 {
		t.Errorf("exec.failed = %d, exec.unknown = %d, want 0 and 0: this run "+
			"completed cleanly, so any non-zero here means the fold is "+
			"assigning statuses the core did not write",
			state.ExecFailed, state.ExecUnknown)
	}
}

// TestTheDurableCursorAdvancesToTheLastCompletedStep measures the field most
// likely to be silently zero.
//
// source_seq arrives as a JSON number, which unmarshals into any as float64.
// A type assertion to int64 fails, leaves the field at zero, and reports no
// error -- exactly the shape of the `seq`/`sequence` decoder defect. Pinning
// the real value is the only thing that separates "read correctly" from
// "read as nothing".
func TestTheDurableCursorAdvancesToTheLastCompletedStep(t *testing.T) {
	events := replayBytes(t, realRunLog(t))
	state := fold.Fold(events)

	if state.ExecCursor == 0 {
		t.Fatal("exec.cursor = 0 after a 122-event run: source_seq decodes " +
			"from JSON as float64, and an int64 type assertion on it fails " +
			"silently. A zero here is the field never having been read, not " +
			"a run that executed nothing")
	}
	// The highest source_seq in the recorded log's exec.step_completed
	// events. This is the run's durable cursor at the end.
	if state.ExecCursor != 112 {
		t.Errorf("exec.cursor = %d, want 112: measured as the maximum "+
			"source_seq across the log's 29 exec.step_completed events",
			state.ExecCursor)
	}
}

// TestTheCursorNeverGoesBackwards guards the ordering property the cursor
// exists for.
//
// The core's Recover() uses this value to decide where a resumed run may
// safely continue. A cursor that moved backwards would name an already-
// executed event as the next one to run.
//
// Note what this test can and cannot prove. The recorded log's
// exec.step_completed events are already monotonically non-decreasing in
// source_seq, so a plain assignment (`s.ExecCursor = v`) passes it just as a
// max does. The sweep demonstrated exactly that. Monotonicity over the real
// log is still worth pinning -- it is a claim about the core's output -- but
// the guard itself is measured by TestAnOutOfOrderStepDoesNotRewindTheCursor.
func TestTheCursorNeverGoesBackwards(t *testing.T) {
	events := replayBytes(t, realRunLog(t))

	var prev int64
	for i := 1; i <= len(events); i++ {
		c := fold.Fold(events[:i]).ExecCursor
		if c < prev {
			t.Fatalf("after %d events the cursor went %d -> %d: it is a "+
				"high-water mark, and moving it backwards would name an "+
				"already-executed event as the next one to run", i, prev, c)
		}
		prev = c
	}
}

// TestAnOutOfOrderStepDoesNotRewindTheCursor is the separating input the
// sweep asked for.
//
// Welding the max into a plain assignment escaped every test, because the
// recorded log never delivers an out-of-order step -- so nothing ever
// distinguished "takes the highest" from "takes the latest". The guard was
// present, commented, and unmeasured.
//
// The cursor is a high-water mark by definition: the core's Recover() treats
// it as the boundary past which work is known to be committed. Reading a
// lower source_seq must not move it back, because a resumed run would then
// re-execute an event whose effects were already applied -- duplicating paid
// or mutating work, which is the precise harm exec.* exists to prevent.
func TestAnOutOfOrderStepDoesNotRewindTheCursor(t *testing.T) {
	events := []fold.Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{"simulated": false}},
		{Type: "exec.step_completed", Seq: 2, Payload: map[string]any{
			"source_seq": float64(50), "work_ids": []any{"w1"},
		}},
		// A lower source_seq arriving afterwards. A plain assignment rewinds
		// the cursor from 50 to 12; a max holds it at 50.
		{Type: "exec.step_completed", Seq: 3, Payload: map[string]any{
			"source_seq": float64(12), "work_ids": []any{"w2"},
		}},
	}

	state := fold.Fold(events)
	if state.ExecCursor != 50 {
		t.Errorf("exec.cursor = %d after seeing source_seq 50 then 12, want "+
			"50: the cursor is a high-water mark, and rewinding it would "+
			"tell a resumed run to re-execute events whose effects are "+
			"already committed", state.ExecCursor)
	}

	// A genuinely higher step must still advance it, or the fix has frozen
	// the cursor instead of flooring it.
	events = append(events, fold.Event{
		Type: "exec.step_completed", Seq: 4,
		Payload: map[string]any{"source_seq": float64(73)},
	})
	if got := fold.Fold(events).ExecCursor; got != 73 {
		t.Errorf("exec.cursor = %d after a later source_seq 73, want 73: "+
			"guarding against rewind must not stop the cursor advancing",
			got)
	}
}

// TestRunResultMeansSucceededAndNothingElse is the finding that contradicts
// the obvious reading of "the host cannot distinguish a successful run from a
// failed one".
//
// It cannot -- but not because run.result is unread. It is because run.result
// is emitted ONLY on success. kernel/decide.go's `case RunResult` sets
// Status = StatusSucceeded with no branch on the payload, and both emission
// sites (decide.go:554 "all stages completed", decide.go:720 "last stage
// expired, advancing") are stage-advance paths. There is no failing run that
// emits one.
//
// So reading run.result answers "did it succeed". Its absence means "no
// verdict yet", NOT "it failed": a failed run goes to run.cancelled,
// run.expired, or to a status change carried by no event at all (quiescent
// with no observer, inbox timeout with on_timeout:fail). Encoding this as a
// bool would make those two cases indistinguishable, which is why the bind is
// a three-valued string.
func TestRunResultMeansSucceededAndNothingElse(t *testing.T) {
	events := replayBytes(t, realRunLog(t))
	state := fold.Fold(events)

	if state.RunOutcome != "succeeded" {
		t.Errorf("run.outcome = %q, want \"succeeded\": the recorded run "+
			"emitted run.result at seq 112 and the core treats that event as "+
			"success unconditionally", state.RunOutcome)
	}
	if state.RunSummary != "all stages completed" {
		t.Errorf("run.summary = %q, want \"all stages completed\": the "+
			"summary is the run's own sentence about itself and the host "+
			"must not paraphrase it", state.RunSummary)
	}
	if state.RunResultFrom != "last_submit" {
		t.Errorf("run.result_from = %q, want \"last_submit\": this names "+
			"which blueprint rule produced the verdict", state.RunResultFrom)
	}
}

// TestNoVerdictIsDistinctFromFailure is the half a boolean would have lost.
//
// A run still in flight and a run that failed are different states, and the
// difference matters: one says "wait", the other says "look at what went
// wrong". Neither emits run.result.
func TestNoVerdictIsDistinctFromFailure(t *testing.T) {
	// A run in progress: work happening, no verdict.
	inFlight := fold.Fold([]fold.Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{"simulated": false}},
		{Type: "exec.work_started", Seq: 2, Payload: map[string]any{"work_id": "w1"}},
	})
	if inFlight.RunOutcome != "" {
		t.Errorf("run.outcome = %q for a run with no run.result, want \"\": "+
			"absence of a verdict is not a verdict", inFlight.RunOutcome)
	}
	if inFlight.ExecPhase != "working" {
		t.Errorf("exec.phase = %q with one work in flight, want \"working\"",
			inFlight.ExecPhase)
	}

	// A cancelled run: also no run.result, and it is NOT the same state.
	// The fold does not yet read run.cancelled -- that is named honestly
	// here rather than asserted as if it worked.
	cancelled := fold.Fold([]fold.Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{"simulated": false}},
		{Type: "run.cancelled", Seq: 2, Payload: map[string]any{}},
	})
	if cancelled.RunOutcome != "" {
		t.Errorf("run.outcome = %q after run.cancelled, want \"\": the core "+
			"never emits run.result for a cancelled run, so claiming success "+
			"here would be inventing one", cancelled.RunOutcome)
	}
	if fold.Handles("run.cancelled") {
		t.Error("the fold now handles run.cancelled: this test asserts that a " +
			"cancelled run is merely verdict-less, which stops being the " +
			"whole truth once cancellation is folded. Re-measure")
	}
}

// TestRunResultIsNotTheEndOfTheLog is the ordering assumption that would have
// truncated the tail.
//
// run.result reads like a terminator, and in the recorded log it is not: it
// lands at seq 112 with ten events after it (one exec.work_finished and nine
// exec.step_completed, seq 113-122) as the executor drains its queue. A fold
// that stopped on the verdict would drop them and report a cursor and a
// completion count that are both short.
func TestRunResultIsNotTheEndOfTheLog(t *testing.T) {
	events := replayBytes(t, realRunLog(t))

	var verdictAt = -1
	for i, e := range events {
		if e.Type == "run.result" {
			verdictAt = i
			break
		}
	}
	if verdictAt < 0 {
		t.Fatal("the recorded log has no run.result")
	}
	after := len(events) - verdictAt - 1
	if after != 10 {
		t.Errorf("%d events follow run.result, want 10: measured from the "+
			"recorded log. If the core changed, the truncation risk this "+
			"test guards has to be re-measured", after)
	}

	// The state at the verdict versus the state at the end of the log must
	// differ, or the ten trailing events carried nothing and the test is
	// guarding an empty property.
	atVerdict := fold.Fold(events[:verdictAt+1])
	atEnd := fold.Fold(events)

	if atVerdict.ExecCursor == atEnd.ExecCursor {
		t.Errorf("the cursor is %d both at run.result and at end of log: the "+
			"trailing exec.step_completed events must advance it, or this "+
			"test is not measuring truncation", atEnd.ExecCursor)
	}
	if atEnd.ExecCursor < atVerdict.ExecCursor {
		t.Errorf("cursor went backwards across the verdict: %d -> %d",
			atVerdict.ExecCursor, atEnd.ExecCursor)
	}
	// Both must report success: the verdict does not un-happen.
	if atVerdict.RunOutcome != "succeeded" || atEnd.RunOutcome != "succeeded" {
		t.Errorf("outcome at verdict = %q, at end = %q: both must be "+
			"\"succeeded\"", atVerdict.RunOutcome, atEnd.RunOutcome)
	}
}

// TestTheFoldStaysDeterministicOverTheRealLog restates the property the whole
// design rests on, now that the fold carries three internal maps.
//
// Maps introduced for work tracking are an obvious place for iteration order
// to leak into output. Folding the same log twice must produce the same
// counters, or replay is worthless.
func TestTheFoldStaysDeterministicOverTheRealLog(t *testing.T) {
	events := replayBytes(t, realRunLog(t))

	first := fold.Fold(events)
	for i := 0; i < 8; i++ {
		again := fold.Fold(events)
		if again.ExecActive != first.ExecActive ||
			again.ExecCompleted != first.ExecCompleted ||
			again.ExecFailed != first.ExecFailed ||
			again.ExecUnknown != first.ExecUnknown ||
			again.ExecCursor != first.ExecCursor ||
			again.ExecPhase != first.ExecPhase ||
			again.RunOutcome != first.RunOutcome ||
			again.RunSummary != first.RunSummary {
			t.Fatalf("fold %d disagreed with fold 0: the work-tracking maps "+
				"have leaked iteration order into the counters, and a "+
				"non-deterministic fold makes replay worthless", i+1)
		}
	}
}

// TestARepeatedTerminalRecordIsCountedOnce guards the replay tolerance the
// core itself has.
//
// progress.go's Recover() accepts a repeated exec.work_finished as long as the
// status agrees -- it only rejects CONFLICTING statuses. So a duplicate is a
// legal log, and a host that counted it twice would report more completed work
// than the run performed.
func TestARepeatedTerminalRecordIsCountedOnce(t *testing.T) {
	events := []fold.Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{"simulated": false}},
		{Type: "exec.work_prepared", Seq: 2, Payload: map[string]any{"work_id": "w1"}},
		{Type: "exec.work_started", Seq: 3, Payload: map[string]any{"work_id": "w1"}},
		{Type: "exec.work_finished", Seq: 4, Payload: map[string]any{
			"work_id": "w1", "status": "completed",
		}},
		{Type: "exec.work_finished", Seq: 5, Payload: map[string]any{
			"work_id": "w1", "status": "completed",
		}},
	}
	state := fold.Fold(events)

	if state.ExecCompleted != 1 {
		t.Errorf("exec.completed = %d, want 1: the core's Recover() tolerates "+
			"a repeated terminal record with a matching status, so a "+
			"duplicate is a legal log and counting it twice overstates the "+
			"work done", state.ExecCompleted)
	}
	if state.ExecActive != 0 {
		t.Errorf("exec.active = %d, want 0: the second finish must not "+
			"decrement a counter the first already brought to zero",
			state.ExecActive)
	}
}

// TestARepeatedStartIsCountedOnce is the third weld the sweep caught
// escaping, and it is the same shape as the other two: a guard defending
// against a log the recorded run does not contain.
//
// No work_id in the recorded log ever starts twice, so the dedup check had
// never been exercised. It is not hypothetical, though: exec.work_started is
// appended to the log before the dispatch goroutine runs, and a crash between
// the append and the terminal record leaves a run that resumes and re-emits.
// The core's Recover() accepts that shape -- it only rejects a start with no
// preceding prepare.
//
// Counted twice, one unit of work occupies two slots, and since only one
// finish arrives the count never returns to zero: the host spins forever on
// a run that ended.
func TestARepeatedStartIsCountedOnce(t *testing.T) {
	events := []fold.Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{"simulated": false}},
		{Type: "exec.work_prepared", Seq: 2, Payload: map[string]any{"work_id": "w1"}},
		{Type: "exec.work_started", Seq: 3, Payload: map[string]any{"work_id": "w1"}},
		{Type: "exec.work_started", Seq: 4, Payload: map[string]any{"work_id": "w1"}},
	}

	state := fold.Fold(events)
	if state.ExecActive != 1 {
		t.Errorf("exec.active = %d after the same work_id started twice, "+
			"want 1: it is one unit of work and there will be one finish, so "+
			"counting the replayed start again leaves the indicator stuck "+
			"above zero for the rest of the run", state.ExecActive)
	}

	// The single finish must bring it back to zero.
	events = append(events, fold.Event{
		Type: "exec.work_finished", Seq: 5,
		Payload: map[string]any{"work_id": "w1", "status": "completed"},
	})
	state = fold.Fold(events)
	if state.ExecActive != 0 {
		t.Errorf("exec.active = %d after the one finish, want 0: this is the "+
			"stuck-spinner symptom the dedup exists to prevent",
			state.ExecActive)
	}
	if state.ExecPhase != "idle" {
		t.Errorf("exec.phase = %q, want \"idle\"", state.ExecPhase)
	}
}

// TestWorkWithNoIdentifierDoesNotLeakAnActiveSlot is the malformed-input
// guard.
//
// A start whose work_id is missing can never be paired with its finish. If the
// fold counted it, the active count would stay above zero for the rest of the
// run and the status row would show a permanent spinner.
func TestWorkWithNoIdentifierDoesNotLeakAnActiveSlot(t *testing.T) {
	state := fold.Fold([]fold.Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{"simulated": false}},
		{Type: "exec.work_started", Seq: 2, Payload: map[string]any{}},
		{Type: "exec.work_started", Seq: 3, Payload: map[string]any{"work_id": ""}},
	})

	if state.ExecActive != 0 {
		t.Errorf("exec.active = %d after two starts with no work_id, want 0: "+
			"unidentifiable work can never be matched to a finish, so "+
			"counting it pins the indicator to \"working\" for the rest of "+
			"the run", state.ExecActive)
	}
	if state.ExecPhase != "idle" {
		t.Errorf("exec.phase = %q, want \"idle\"", state.ExecPhase)
	}
}
