package driver

import (
	"reflect"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// The stage.* family is seven events of the measured 122 and the only ones
// that answer "where is this run". exec.* (91) counts durable work and
// tool.* (8) says what the agent did; neither says which phase of the plan is
// running, so a run that had finished building and moved to reviewing looked
// identical to one still building.
//
// Every assertion below was measured against testdata/serve/real_run.ndjson
// or read at an emission site in the arxi core BEFORE it was written down.
// Four of them contradict what the family's shape suggests:
//
//   - there is NO stage total anywhere in the log, so "stage 2 of 5" is not
//     representable and a denominator would have to be invented;
//   - agent.turn_done arrives one sequence after every stage.submitted and
//     was erasing it;
//   - the two emitters of stage.submitted disagree about the payload;
//   - stage.timeout is not a failure.
//
// That is also why this file measures the recorded log and hand-built
// adversarial lines rather than a fixture: a fixture written by the author of
// the fold agrees with the fold by construction.

// TestTheStagePositionOfTheRealRunIsFolded is the end-to-end measurement.
//
// The recorded run walks build -> review and ends there. The final state is
// asserted exactly rather than as "not empty", because the interesting
// failures here do not produce an empty value -- they produce a plausible
// wrong one: the stage the run had already left, or index 0 for a run in its
// second stage.
func TestTheStagePositionOfTheRealRunIsFolded(t *testing.T) {
	state := fold.Fold(replayBytes(t, realRunLog(t)))

	if state.StageName != "review" {
		t.Errorf("stage.name = %q, want \"review\": the recorded run enters "+
			"build (seq 3), advances at seq 62 and enters review (seq 64), "+
			"which is where it ends", state.StageName)
	}
	if state.StageIndex != 1 {
		t.Errorf("stage.index = %d, want 1: review is the second stage and "+
			"the payload says so (stage.entered seq 64 carries index 1)",
			state.StageIndex)
	}
	if state.StagePrev != "build" {
		t.Errorf("stage.prev = %q, want \"build\": stage.advanced seq 62 "+
			"carries {from: build, to: review}", state.StagePrev)
	}
	if state.StageAdvances != 1 {
		t.Errorf("stage.advances = %d, want 1: the log contains exactly one "+
			"stage.advanced", state.StageAdvances)
	}

	// Two submissions, and specifically the REVIEW ones. The log holds four
	// stage.submitted events, two per stage; a fold that never cleared the
	// list on stage entry would report four here and look merely generous.
	if got := len(state.StageSubmissions); got != 2 {
		t.Fatalf("stage.submissions = %v (%d), want 2: backend and frontend "+
			"both submitted to review (seq 96, 103). Four would mean the "+
			"build-stage submissions were never cleared, which is the "+
			"defect that lets a stale submit satisfy the next stage's "+
			"advance rule", state.StageSubmissions, got)
	}
	if state.StageSubmittedCount != 2 {
		t.Errorf("stage.submitted_count = %d, want 2 (must equal "+
			"len(stage.submissions) = %d)",
			state.StageSubmittedCount, len(state.StageSubmissions))
	}
}

// TestEnteringAStageClearsThePreviousStagesSubmissions isolates the clearing
// rule, because the end-to-end count above can be satisfied by accident.
//
// The core clears it in exactly this place (applyStageEntered sets
// m.Submitted = false for every member) and says why: "Submitted is cleared
// on stage entry, so this cannot leak into the next stage." A leaked submit
// satisfies an advance rule nobody met, so the run skips a stage.
//
// The input is built so that NOT clearing is distinguishable from clearing:
// the same two agents submit in both stages. If the list were never cleared
// the count would be 4 and the names would still all be plausible.
func TestEnteringAStageClearsThePreviousStagesSubmissions(t *testing.T) {
	log := strings.Join([]string{
		`{"seq":1,"type":"stage.entered","payload":{"stage":"build","index":0}}`,
		`{"seq":2,"type":"stage.submitted","actor":"backend","payload":{"agent":"backend","stage":"build"}}`,
		`{"seq":3,"type":"stage.submitted","actor":"frontend","payload":{"agent":"frontend","stage":"build"}}`,
		`{"seq":4,"type":"stage.advanced","payload":{"from":"build","to":"review","to_index":1}}`,
		`{"seq":5,"type":"stage.entered","payload":{"stage":"review","index":1}}`,
		`{"seq":6,"type":"stage.submitted","actor":"backend","payload":{"agent":"backend","stage":"review"}}`,
	}, "\n") + "\n"

	state := fold.Fold(replayBytes(t, []byte(log)))

	if len(state.StageSubmissions) != 1 || state.StageSubmissions[0] != "backend" {
		t.Errorf("stage.submissions = %v, want [backend]: entering review "+
			"must clear the two build submissions. Without the clear the "+
			"list reads [backend frontend backend] -- three plausible "+
			"names, no obviously wrong value, and a quorum of 2 reported "+
			"for a stage one agent has answered", state.StageSubmissions)
	}
	if state.StageSubmittedCount != 1 {
		t.Errorf("stage.submitted_count = %d, want 1", state.StageSubmittedCount)
	}
}

// TestSubmittedSurvivesTheTurnDoneThatFollowsIt is the defect this family
// found, isolated.
//
// The fold set the member to idle on every agent.turn_done, unconditionally.
// The core does not: applyTurnDone guards with `if !m.Submitted`, and the
// comment there calls the exception "load-bearing rather than tidy".
//
// The ordering makes the collision certain rather than occasional. A real
// agent submits by calling a tool DURING its turn, so stage.submitted always
// precedes the agent.turn_done closing that turn. Measured in the recorded
// log, every time, at the very next sequence number:
//
//	seq 35 stage.submitted backend  -> seq 36 agent.turn_done backend
//	seq 42 stage.submitted frontend -> seq 43 agent.turn_done frontend
//	seq 96 stage.submitted backend  -> seq 97 agent.turn_done backend
//	seq 103 stage.submitted frontend -> seq 104 agent.turn_done frontend
//
// So "submitted" -- a state docs/BINDS.md §4.1 has signed in the
// team.members enum since the beginning -- existed for exactly one event in
// the entire run and was gone before anybody could read it.
func TestSubmittedSurvivesTheTurnDoneThatFollowsIt(t *testing.T) {
	log := strings.Join([]string{
		`{"seq":1,"type":"stage.entered","payload":{"stage":"build","index":0}}`,
		`{"seq":2,"type":"agent.activated","actor":"backend","payload":{"agent":"backend"}}`,
		`{"seq":3,"type":"stage.submitted","actor":"backend","payload":{"agent":"backend","stage":"build"}}`,
		`{"seq":4,"type":"agent.turn_done","actor":"backend","payload":{"agent":"backend"}}`,
	}, "\n") + "\n"

	state := fold.Fold(replayBytes(t, []byte(log)))

	if got := memberState(t, state, "backend"); got != "submitted" {
		t.Errorf("backend state = %q, want \"submitted\": the turn_done at "+
			"seq 4 must not overwrite the submit at seq 3. The core guards "+
			"this exact case (applyTurnDone: `if !m.Submitted { m.State = "+
			"MemberIdle }`) because an idle member that has in fact "+
			"submitted is reported as runnable, so a staged run where "+
			"everyone submitted but the advance rule cannot be met looks "+
			"eternally healthy and quiescence never fires", got)
	}

	// The turn still ended. Preserving the state must not also suppress the
	// facts that are true either way, or the fix trades one wrong field for
	// another -- and a test that only checked State would not notice.
	m := memberOf(t, state, "backend")
	if m.Busy {
		t.Errorf("backend busy = true after agent.turn_done: the turn is " +
			"over whatever state the member holds")
	}
	if m.Turns != 1 {
		t.Errorf("backend turns = %d, want 1: a submitted member's turn "+
			"still counts", m.Turns)
	}
}

// TestATurnDoneWithoutASubmitStillReturnsTheMemberToIdle is the other side of
// the same guard, and it is what makes the guard measurable.
//
// Without this case, "preserve submitted" and "never set idle at all" produce
// identical results on every input above, so a weld that deletes the idle
// assignment entirely would escape. The two behaviours differ only on a turn
// that ended WITHOUT a submit -- which is the ordinary case, and the one
// where a member stuck on "thinking" forever would be the visible bug.
func TestATurnDoneWithoutASubmitStillReturnsTheMemberToIdle(t *testing.T) {
	log := strings.Join([]string{
		`{"seq":1,"type":"agent.activated","actor":"backend","payload":{"agent":"backend"}}`,
		`{"seq":2,"type":"agent.turn_done","actor":"backend","payload":{"agent":"backend"}}`,
	}, "\n") + "\n"

	state := fold.Fold(replayBytes(t, []byte(log)))

	if got := memberState(t, state, "backend"); got != "idle" {
		t.Errorf("backend state = %q, want \"idle\": a turn that ended with "+
			"no submit returns the member to idle. This is the input that "+
			"tells 'preserve the submitted state' apart from 'never write "+
			"idle at all'; without it a fold that dropped the assignment "+
			"outright would satisfy every other test in this file", got)
	}
}

// TestAWaitingMemberIsNotIdledByItsOwnTurnDone pins the branch the core puts
// FIRST, and for a reason it spells out: getting it wrong destroys a block
// rather than merely mislabelling it.
//
// A tool denied with policy "ask" moves the member to waiting DURING its
// turn, so the agent.turn_done for that same turn arrives afterwards --
// always. Clearing the state there yields a member that is simultaneously
// idle and carrying a block: the host would show an agent ready for work
// while its approval sits unanswered.
func TestAWaitingMemberIsNotIdledByItsOwnTurnDone(t *testing.T) {
	log := strings.Join([]string{
		`{"seq":1,"type":"agent.activated","actor":"backend","payload":{"agent":"backend"}}`,
		`{"seq":2,"type":"agent.blocked","actor":"backend","payload":{"agent":"backend","blocked_on":"approval","task":"run bash"}}`,
		`{"seq":3,"type":"agent.turn_done","actor":"backend","payload":{"agent":"backend"}}`,
	}, "\n") + "\n"

	state := fold.Fold(replayBytes(t, []byte(log)))

	if got := memberState(t, state, "backend"); got != "waiting" {
		t.Errorf("backend state = %q, want \"waiting\": the turn_done that "+
			"closes a turn blocked on a human must not report the member "+
			"idle. The block is still outstanding and nothing has answered "+
			"it", got)
	}
	// The todo must survive too: a block whose reason was erased is a block
	// nobody can act on, and the member state alone would not reveal that.
	if len(state.Todos) != 1 {
		t.Errorf("todos = %d, want 1: the outstanding approval is still "+
			"outstanding after the turn ended", len(state.Todos))
	}
}

// TestABlockedMemberIsResolvedByTheTopLevelActor is a defect the test above
// found, and it is not a stage defect at all.
//
// The agent.blocked case read ONLY payload.actor. The core reads the
// top-level field -- applyBlocked is `m := out.Member(e.Actor)` -- which is
// the same conclusion the tool.* family reached last turn, now in a second
// place.
//
// What kept it hidden is worth recording: the recorded 122-event log contains
// ZERO agent.blocked events. The whole blocked surface -- the todos list, the
// agent.blocked.* binds of BINDS.md §4.2, the waiting member state -- had
// never been folded from real data even once, so "measured against the real
// log" said nothing about it. With payload.actor absent the actor resolved to
// "", so every todo was attributed to nobody and the blocked member kept
// whatever state it already had: the host would show an approval request with
// no owner while the agent waiting on it still looked busy.
func TestABlockedMemberIsResolvedByTheTopLevelActor(t *testing.T) {
	// The core's shape: actor at the top level, no `actor` key in the
	// payload.
	log := strings.Join([]string{
		`{"seq":1,"type":"agent.activated","actor":"backend","payload":{"agent":"backend"}}`,
		`{"seq":2,"type":"agent.blocked","actor":"backend","payload":{"blocked_on":"approval","task":"run bash","blocked_ref":{"kind":"tool"}}}`,
	}, "\n") + "\n"

	state := fold.Fold(replayBytes(t, []byte(log)))

	if len(state.Todos) != 1 {
		t.Fatalf("todos = %d, want 1", len(state.Todos))
	}
	if state.Todos[0].Actor != "backend" {
		t.Errorf("todo actor = %q, want \"backend\": with no `actor` key in "+
			"the payload the name is only at the top level, and a todo "+
			"owned by \"\" is a request the user cannot act on",
			state.Todos[0].Actor)
	}
	if state.BlockedActor != "backend" {
		t.Errorf("agent.blocked.actor = %q, want \"backend\"", state.BlockedActor)
	}
	if got := memberState(t, state, "backend"); got != "waiting" {
		t.Errorf("backend state = %q, want \"waiting\": the member the block "+
			"belongs to could not be found, so its state was never updated "+
			"and the host shows an agent still working on a turn that is "+
			"parked on a human", got)
	}

	// The unblock must resolve the name the same way, or the todo can be
	// created and never cleared -- a worse outcome than never showing it.
	unblock := log + `{"seq":3,"type":"agent.unblocked","actor":"backend","payload":{"blocked_on":"approval"}}` + "\n"
	cleared := fold.Fold(replayBytes(t, []byte(unblock)))
	if len(cleared.Todos) != 0 {
		t.Errorf("todos = %v after agent.unblocked, want none: the two "+
			"events are matched by actor, so resolving the name differently "+
			"on each side leaves the todo in the list forever",
			cleared.Todos)
	}
	if got := memberState(t, cleared, "backend"); got != "idle" {
		t.Errorf("backend state = %q after unblock, want \"idle\"", got)
	}
}

// TestTheStageTotalIsNotInventedBecauseTheLogDoesNotCarryOne is a test about
// something the fold deliberately does NOT do, which is why it is written
// down rather than left as a comment.
//
// Measured: every stage.* payload in the recorded run carries only
// stage.entered {stage, index}, stage.advanced {from, to, to_index},
// stage.submitted {agent, stage, simulated}. No stage count, and none in
// run.started either -- its payload is actor, blueprint_sha, budget_usd,
// max_turns, prompt, run_id, simulated, workspace, effective_config_*. The
// total lives in Config.Stages, the frozen blueprint, which the fold does not
// read because the fold reads the log.
//
// The tempting denominator is "highest index seen + 1". It renders
// "stage 1 of 1" for the whole of a five-stage run's first stage: a progress
// bar that is always full, always advancing, and never wrong in a way anyone
// can see. So the test asserts the absence: no field on State may report a
// stage total.
func TestTheStageTotalIsNotInventedBecauseTheLogDoesNotCarryOne(t *testing.T) {
	// Every stage-carrying payload key in the recorded log, gathered from the
	// events themselves rather than from memory.
	events := replayBytes(t, realRunLog(t))
	keys := map[string]bool{}
	var stageEvents int
	for _, e := range events {
		if !strings.HasPrefix(e.Type, "stage.") && e.Type != "run.started" {
			continue
		}
		if strings.HasPrefix(e.Type, "stage.") {
			stageEvents++
		}
		for k := range e.Payload {
			keys[k] = true
		}
	}
	if stageEvents != 7 {
		t.Fatalf("found %d stage.* events, want 7: this test reasons about "+
			"the payloads of a family whose size it must first confirm",
			stageEvents)
	}
	for _, forbidden := range []string{"stages", "stage_count", "total", "of", "stage_total"} {
		if keys[forbidden] {
			t.Errorf("the log DOES carry %q, so the premise of this test is "+
				"stale: a real total is available and stage.index should be "+
				"rendered against it instead of alone", forbidden)
		}
	}

	// And the fold does not manufacture one. StageIndex is a position; there
	// is deliberately no StageTotal beside it.
	state := fold.Fold(events)
	if state.StageIndex != 1 {
		t.Fatalf("stage.index = %d, want 1", state.StageIndex)
	}
	if _, hasTotal := anyStageTotalField(state); hasTotal {
		t.Error("fold.State grew a stage-total field. The log carries no " +
			"stage count at any emission site, so such a field can only be " +
			"a running maximum of the indices seen -- which reports " +
			"\"stage 1 of 1\" throughout the first stage of a five-stage " +
			"run, and grows to stay correct. A position with no denominator " +
			"is honest; a denominator inferred from the positions so far is " +
			"a lie with a number on it")
	}
}

// TestStageIndexStartsAtMinusOneSoTheFirstStageIsNotAReEntry pins the
// sentinel, which is the core's own (applyRunStarted: "StageIndex = -1 means
// 'has not entered any stage yet'. Starting at 0 would make the first
// stage.entered look like a re-entry").
//
// Zero is a real position -- the first stage -- so a zero default makes "no
// stage yet" and "in the first stage" the same value, and the host would
// render a run that has not started as if it were building.
func TestStageIndexStartsAtMinusOneSoTheFirstStageIsNotAReEntry(t *testing.T) {
	empty := fold.Fold(nil)
	if empty.StageIndex != -1 {
		t.Errorf("stage.index on an empty fold = %d, want -1: zero is the "+
			"first stage, a real position, so a zero default cannot be told "+
			"apart from a run that has entered it", empty.StageIndex)
	}
	if empty.StageName != "" {
		t.Errorf("stage.name on an empty fold = %q, want empty", empty.StageName)
	}

	// And a run that has started but entered nothing keeps the sentinel: an
	// unstaged blueprint gets no stage.entered at all ("there is no stage to
	// enter"), so this is a shape the host must survive rather than a
	// hypothetical.
	started := fold.Fold(replayBytes(t, []byte(
		`{"seq":1,"type":"run.started","payload":{"run_id":"r1","simulated":true}}`+"\n")))
	if started.StageIndex != -1 {
		t.Errorf("stage.index after run.started with no stage.entered = %d, "+
			"want -1: an unstaged blueprint never enters a stage, and "+
			"reporting index 0 would invent a stage the run does not have",
			started.StageIndex)
	}
}

// TestAMissingIndexDoesNotSilentlyReadAsTheFirstStage is the guard for the
// `ok` check on the payload read, and it needs its own input because every
// well-formed event in the log carries the key.
//
// JSON numbers arrive as float64 and a failed type assertion yields 0. Since
// 0 is a legitimate stage index, a fold that assigned unconditionally would
// move a run in stage 3 back to stage 0 on one malformed event -- and then
// keep rendering confidently.
func TestAMissingIndexDoesNotSilentlyReadAsTheFirstStage(t *testing.T) {
	log := strings.Join([]string{
		`{"seq":1,"type":"stage.entered","payload":{"stage":"build","index":0}}`,
		`{"seq":2,"type":"stage.advanced","payload":{"from":"build","to":"review","to_index":1}}`,
		`{"seq":3,"type":"stage.entered","payload":{"stage":"review","index":1}}`,
		// No index key at all: the shape a newer or partial emitter produces.
		`{"seq":4,"type":"stage.entered","payload":{"stage":"ship"}}`,
	}, "\n") + "\n"

	state := fold.Fold(replayBytes(t, []byte(log)))

	if state.StageName != "ship" {
		t.Errorf("stage.name = %q, want \"ship\": the name was present and "+
			"must still be read", state.StageName)
	}
	if state.StageIndex != 1 {
		t.Errorf("stage.index = %d, want 1: the last event carried no index, "+
			"so the last known position stands. An unconditional assignment "+
			"reads the failed float64 assertion as 0 and walks the run back "+
			"to the first stage -- a value that is indistinguishable from a "+
			"correct one", state.StageIndex)
	}
}

// TestStageAdvancedAloneMovesThePositionEvenIfTheLogIsTruncated pins why
// stage.advanced writes the position at all, given that the stage.entered
// which always follows it would restate the same thing.
//
// The pair is emitted in that order deliberately -- orderEffects in the core
// notes "the order of the Emits among themselves is semantic (stage.advanced
// before stage.entered)" -- and a log read live can end between them. Reading
// only `from` here would leave the host displaying the stage the run has just
// left, which is worse than showing nothing: it is confidently stale.
func TestStageAdvancedAloneMovesThePositionEvenIfTheLogIsTruncated(t *testing.T) {
	log := strings.Join([]string{
		`{"seq":1,"type":"stage.entered","payload":{"stage":"build","index":0}}`,
		`{"seq":2,"type":"stage.advanced","payload":{"from":"build","to":"review","to_index":1}}`,
		// The stage.entered for review has not been written yet.
	}, "\n") + "\n"

	state := fold.Fold(replayBytes(t, []byte(log)))

	if state.StageName != "review" {
		t.Errorf("stage.name = %q, want \"review\": the advance names its "+
			"destination and the log ends before the matching "+
			"stage.entered. A fold that only recorded `from` would show "+
			"\"build\" -- the stage the run has already left", state.StageName)
	}
	if state.StageIndex != 1 {
		t.Errorf("stage.index = %d, want 1 (to_index)", state.StageIndex)
	}
	if state.StagePrev != "build" {
		t.Errorf("stage.prev = %q, want \"build\"", state.StagePrev)
	}
}

// TestTheSubmitterIsReadFromTheTopLevelActorNotThePayload pins the reading
// order for this family specifically, because its two emitters disagree about
// the payload in a way the recorded log cannot show.
//
//	internal/exec/fake.go:335   payload {agent, stage, simulated}   (--sim)
//	host/v1/text_executor.go:60 payload {agent, result}             (no stage)
//
// Both set Actor. The core's own reader resolves it in exactly this order
// (cmd/arxi/runresult.go's lastSubmission: `who := e.Actor; if who == "" {
// who = e.Str("agent") }`), so top-level-first is the core's rule and not a
// preference of this host.
//
// Every stage.submitted in the recorded log has actor and payload.agent set
// to the SAME name, so the log cannot distinguish the two orders. The
// conflicting line below is the only input that can.
func TestTheSubmitterIsReadFromTheTopLevelActorNotThePayload(t *testing.T) {
	conflict := `{"seq":1,"type":"stage.submitted","actor":"backend",` +
		`"payload":{"agent":"frontend","stage":"build"}}` + "\n"

	state := fold.Fold(replayBytes(t, []byte(conflict)))

	if len(state.StageSubmissions) != 1 {
		t.Fatalf("stage.submissions = %v, want one entry", state.StageSubmissions)
	}
	if state.StageSubmissions[0] != "backend" {
		t.Errorf("submitter = %q, want \"backend\": with actor and "+
			"payload.agent disagreeing, `actor` wins. This is the only "+
			"input that separates the two reading orders -- on every "+
			"stage.submitted in the recorded log the fields agree, so a "+
			"payload-first fold returns the same answer and the defect "+
			"stays invisible", state.StageSubmissions[0])
	}
}

// TestASubmitWithNoTopLevelActorStillFallsBackToThePayload is the other half,
// and it is why actorName has a fallback rather than one reading.
//
// host/v1's executor sets Actor, but the payload key is what an emitter that
// predates the top-level field writes, and the core's own reader keeps the
// fallback for that reason. Without this case a weld deleting the fallback
// would escape.
func TestASubmitWithNoTopLevelActorStillFallsBackToThePayload(t *testing.T) {
	line := `{"seq":1,"type":"stage.submitted","payload":{"agent":"frontend","stage":"build"}}` + "\n"

	state := fold.Fold(replayBytes(t, []byte(line)))

	if len(state.StageSubmissions) != 1 || state.StageSubmissions[0] != "frontend" {
		t.Errorf("stage.submissions = %v, want [frontend]: with no "+
			"top-level actor the payload key is the only attribution the "+
			"event carries, and dropping it loses the submit entirely",
			state.StageSubmissions)
	}
}

// TestARepeatedSubmitFromOneMemberIsCountedOnce pins the dedupe.
//
// The core tolerates repeat submits explicitly -- "A stage resolves ONCE,
// however many members go on submitting to it" -- so a second submit from the
// same agent is legal input, not a corrupt log. Counting both reports a
// quorum of two from one agent, which is exactly the shape of wrongness that
// looks right: the number is small, plausible, and the names are real.
func TestARepeatedSubmitFromOneMemberIsCountedOnce(t *testing.T) {
	log := strings.Join([]string{
		`{"seq":1,"type":"stage.entered","payload":{"stage":"build","index":0}}`,
		`{"seq":2,"type":"stage.submitted","actor":"backend","payload":{"agent":"backend","stage":"build"}}`,
		`{"seq":3,"type":"stage.submitted","actor":"backend","payload":{"agent":"backend","stage":"build"}}`,
	}, "\n") + "\n"

	state := fold.Fold(replayBytes(t, []byte(log)))

	if len(state.StageSubmissions) != 1 {
		t.Errorf("stage.submissions = %v, want one entry: backend submitted "+
			"twice to the same stage, which the core permits. Two entries "+
			"report a two-member quorum for a stage one member has answered",
			state.StageSubmissions)
	}
	if state.StageSubmittedCount != 1 {
		t.Errorf("stage.submitted_count = %d, want 1", state.StageSubmittedCount)
	}
}

// TestAStageTimeoutIsNotAFailureAndDoesNotEndTheStage pins the semantics of
// the one stage event the recorded log does not contain.
//
// The core's default action for it is `escalate`, whose comment reads: "A
// timeout almost never means 'impossible', it means 'something got stuck,
// take a look'." The stage stays open, its members keep working, and what the
// timeout actually causes arrives as its own later event.
//
// So the fold must record no verdict here. Treating it as terminal would be
// the run.result mistake in a new place: collapsing "no decision yet" into
// "it failed".
func TestAStageTimeoutIsNotAFailureAndDoesNotEndTheStage(t *testing.T) {
	log := strings.Join([]string{
		`{"seq":1,"type":"stage.entered","payload":{"stage":"build","index":0}}`,
		`{"seq":2,"type":"stage.submitted","actor":"backend","payload":{"agent":"backend","stage":"build"}}`,
		`{"seq":3,"type":"stage.timeout","payload":{"stage":"build","index":0,"timer_id":"stage:build"}}`,
	}, "\n") + "\n"

	state := fold.Fold(replayBytes(t, []byte(log)))

	if state.StageName != "build" {
		t.Errorf("stage.name = %q, want \"build\": a timeout does not end "+
			"the stage. The core's default is escalate, which leaves the "+
			"stage open precisely so the members still working can go on to "+
			"satisfy the rule normally", state.StageName)
	}
	if state.StageIndex != 0 {
		t.Errorf("stage.index = %d, want 0", state.StageIndex)
	}
	if len(state.StageSubmissions) != 1 {
		t.Errorf("stage.submissions = %v, want [backend]: the submit that "+
			"already happened is not undone by the deadline passing",
			state.StageSubmissions)
	}
	if state.RunOutcome != "" {
		t.Errorf("run.outcome = %q, want empty: a stage timeout is not a "+
			"run verdict. Whatever it causes -- an escalation to a human, "+
			"an advance, or a failed run -- is emitted as its own event, "+
			"and deciding here would be this host guessing at a decision "+
			"the core has not made", state.RunOutcome)
	}
	if state.StageAdvances != 0 {
		t.Errorf("stage.advances = %d, want 0: no stage.advanced was "+
			"emitted", state.StageAdvances)
	}

	// And it is handled rather than merely ignored. The distinction matters:
	// an unhandled type looks like an oversight, while a case whose correct
	// projection is "no state change" is a decision on the record.
	if !fold.Handles("stage.timeout") {
		t.Error("fold.Handles(\"stage.timeout\") = false: the type is read " +
			"and understood, and its projection is deliberately empty. " +
			"Leaving it unhandled would report it as a gap nobody had " +
			"looked at")
	}
}

// TestTeamMembersRenderInTheSameOrderEveryFold is the defect this turn found
// without looking for it, and it is not about stage.* at all.
//
// deriveTeamMembers ranged over a Go map, and Go randomises map iteration
// order BY DESIGN. team.members is a rendered list, so the subagent panel
// reordered itself between two folds of identical bytes. Measured before the
// fix: four agents, same input, order changed by the 4th of 200 repetitions.
//
// Nothing in the suite could see it. Every existing team.members test either
// folds a single member -- nothing to permute -- or looks the member up by id,
// which is the one access pattern that cannot observe order. `go test
// -count=5` across the whole repo stayed green.
//
// It is worth more than a tidy panel: fold.go's stated contract is "Two runs
// of the same log produce the same state, or the replay is worthless", and
// ADR-0002 re-decided log-follow BECAUSE Replay, the goldens and the Phase 2
// corpus all run through this path. A golden comparing a frame with two or
// more agents would have failed intermittently -- and been blamed on the
// harness.
//
// Four members, because two permute often enough to be caught by luck and
// four do not: with n=4 a wrong implementation has a 1/24 chance of matching
// per fold, so the repetitions below make an escape effectively impossible
// rather than unlikely.
func TestTeamMembersRenderInTheSameOrderEveryFold(t *testing.T) {
	log := strings.Join([]string{
		`{"seq":1,"type":"agent.activated","actor":"backend","payload":{"agent":"backend"}}`,
		`{"seq":2,"type":"agent.activated","actor":"frontend","payload":{"agent":"frontend"}}`,
		`{"seq":3,"type":"agent.activated","actor":"reviewer","payload":{"agent":"reviewer"}}`,
		`{"seq":4,"type":"agent.activated","actor":"docs","payload":{"agent":"docs"}}`,
	}, "\n") + "\n"

	events := replayBytes(t, []byte(log))
	want := orderOf(fold.Fold(events))

	if len(want) != 4 {
		t.Fatalf("folded %d members, want 4: this guard compares orderings "+
			"and needs every member present to mean anything", len(want))
	}

	// The order must also be the LOG's, not the map's and not the
	// alphabet's. Asserting the exact sequence is what stops "sorted"
	// passing as "deterministic": sorting would be stable too, and would
	// silently discard the appearance order the log carries for free.
	// (Alphabetical here would be [backend docs frontend reviewer].)
	expect := []string{"backend", "frontend", "reviewer", "docs"}
	for i := range expect {
		if want[i] != expect[i] {
			t.Fatalf("team.members order = %v, want %v (first-seen order in "+
				"the log)", want, expect)
			break
		}
	}

	for i := 0; i < 200; i++ {
		got := orderOf(fold.Fold(events))
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("fold %d produced team.members in a different "+
					"order: %v, first fold gave %v. The same bytes must "+
					"fold to the same state -- replay, the goldens and the "+
					"Phase 2 corpus all depend on it, and an intermittent "+
					"ordering failure is the kind that gets blamed on the "+
					"harness", i+2, got, want)
			}
		}
	}
}

// TestEveryMemberInTheMapIsInTheRenderedOrder guards the failure the fix
// could introduce, which is worse than the one it repaired.
//
// deriveTeamMembers now walks an insertion-order slice and looks each id up
// in the map. If a member were ever added to the map at a site that does not
// append to the slice, it would vanish from the panel entirely -- where the
// old map-ranging code at least showed everybody, in some order. The two are
// written at a single site so this cannot drift, and this asserts it rather
// than trusting it.
func TestEveryMemberInTheMapIsInTheRenderedOrder(t *testing.T) {
	// A log that reaches the member map through as many different event
	// types as possible: activation, a tool call, a block, a submit and a
	// turn ending. Any of these touching the map without registering the
	// member would show up as a missing row.
	log := strings.Join([]string{
		`{"seq":1,"type":"stage.entered","payload":{"stage":"build","index":0}}`,
		`{"seq":2,"type":"agent.activated","actor":"backend","payload":{"agent":"backend","role":"backend"}}`,
		`{"seq":3,"type":"agent.activated","actor":"frontend","payload":{"agent":"frontend","role":"frontend"}}`,
		`{"seq":4,"type":"tool.call","actor":"backend","payload":{"tool":"read","call_id":"c1"}}`,
		`{"seq":5,"type":"tool.call_completed","actor":"backend","payload":{"tool":"read","call_id":"c1","result":"ok"}}`,
		`{"seq":6,"type":"agent.blocked","actor":"frontend","payload":{"agent":"frontend","blocked_on":"approval","task":"write"}}`,
		`{"seq":7,"type":"stage.submitted","actor":"backend","payload":{"agent":"backend","stage":"build"}}`,
		`{"seq":8,"type":"agent.turn_done","actor":"backend","payload":{"agent":"backend"}}`,
	}, "\n") + "\n"

	state := fold.Fold(replayBytes(t, []byte(log)))

	seen := map[string]bool{}
	for _, m := range state.TeamMembers {
		if seen[m.ID] {
			t.Errorf("member %q appears twice in team.members: the order "+
				"list has a duplicate entry, so one agent renders as two",
				m.ID)
		}
		seen[m.ID] = true
	}
	for _, want := range []string{"backend", "frontend"} {
		if !seen[want] {
			t.Errorf("member %q is missing from team.members entirely. "+
				"Rendering from an insertion-order list means a member "+
				"added to the map without being appended to the list "+
				"disappears from the panel -- a quieter failure than the "+
				"random ordering it replaced, because a missing row looks "+
				"like an agent that never ran", want)
		}
	}
	if len(state.TeamMembers) != 2 {
		t.Errorf("team.members = %d rows, want 2", len(state.TeamMembers))
	}
}

// orderOf lists the member ids in the order team.members renders them.
func orderOf(s fold.State) []string {
	out := make([]string, 0, len(s.TeamMembers))
	for _, m := range s.TeamMembers {
		out = append(out, m.ID)
	}
	return out
}

// memberOf returns the member row, failing the test when it is absent. The
// lookup is by id on purpose -- the ORDER of team.members is the subject of
// its own test, and a helper that depended on position would couple every
// caller to it.
func memberOf(t *testing.T, s fold.State, id string) fold.TeamMember {
	t.Helper()
	for _, m := range s.TeamMembers {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("no member %q in team.members (%+v)", id, s.TeamMembers)
	return fold.TeamMember{}
}

// anyStageTotalField reports whether fold.State has grown a field that claims
// to be a stage total.
//
// It matches on the json bind tag rather than the Go field name because the
// bind name is what a scene author writes and what would appear on screen.
// The check is deliberately by name: the point is to make ADDING such a field
// a deliberate act that fails a test explaining why the log cannot support
// one, not to detect it behaviourally after the fact.
func anyStageTotalField(s fold.State) (string, bool) {
	st := reflect.TypeOf(s)
	for i := 0; i < st.NumField(); i++ {
		switch st.Field(i).Tag.Get("json") {
		case "stage.total", "stage.count", "stage.of", "stage.stages":
			return st.Field(i).Tag.Get("json"), true
		}
	}
	return "", false
}
