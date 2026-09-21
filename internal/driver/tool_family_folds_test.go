package driver

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// The tool.* family is eight events of the measured 122 and the only ones that
// say what the agent DID. exec.* (91 events) is how much durable work is in
// flight; stage.* (7) is where the run sits in its blueprint. Both are
// position and plumbing. "backend ran read on README.md" is the line a person
// watching a run is trying to read, and the host showed none of it.
//
// Every assertion below was measured against testdata/serve/real_run.ndjson,
// or against an emission site in the arxi core, BEFORE it was written down.
// Three of them contradict what the payload's shape suggests, which is why
// this file exists rather than a hand-authored fixture: a fixture written by
// the author of the fold agrees with the fold by construction.

// TestTheToolCallsOfTheRealRunAreAllAttributed is the end-to-end measurement.
//
// Four tool calls, two agents, two calls each. The count and the attribution
// are asserted exactly rather than as "not zero", because the interesting
// failure here does not produce zero -- it produces a plausible smaller
// number. See TestDuplicateCallIDsDoNotCollapseDistinctCalls.
func TestTheToolCallsOfTheRealRunAreAllAttributed(t *testing.T) {
	state := fold.Fold(replayBytes(t, realRunLog(t)))

	if got := len(state.ToolCalls); got != 4 {
		t.Fatalf("folded %d tool calls, want 4: the recorded run contains four "+
			"tool.call events (seq 32, 39, 93, 100), two from backend and two "+
			"from frontend", got)
	}
	if state.ToolCallsTotal != 4 {
		t.Errorf("tool.calls_total = %d, want 4", state.ToolCallsTotal)
	}

	// Attribution per call, in log order. An empty Actor is the specific
	// failure this family was most likely to ship with: the agent's name is a
	// TOP-LEVEL `actor` field, the fold's other cases read payload.agent, and
	// the payload key is present in a --sim log and absent on the real
	// provider path.
	want := []struct {
		actor, tool string
		seq         int64
	}{
		{"backend", "read", 32},
		{"frontend", "read", 39},
		{"backend", "read", 93},
		{"frontend", "read", 100},
	}
	for i, w := range want {
		got := state.ToolCalls[i]
		if got.Actor != w.actor {
			t.Errorf("call %d: actor = %q, want %q. The name lives in the "+
				"event's top-level `actor` field; a fold that reads only "+
				"payload.agent attributes tool calls in simulation and leaves "+
				"them anonymous in production", i, got.Actor, w.actor)
		}
		if got.Tool != w.tool {
			t.Errorf("call %d: tool = %q, want %q", i, got.Tool, w.tool)
		}
		if got.Seq != w.seq {
			t.Errorf("call %d: seq = %d, want %d", i, got.Seq, w.seq)
		}
	}

	// All four completed, none pending, none denied: the simulated run allows
	// every tool.
	for i, c := range state.ToolCalls {
		if c.Outcome != "completed" {
			t.Errorf("call %d: outcome = %q, want \"completed\": every call in "+
				"the recorded run has a tool.call_completed one seq later",
				i, c.Outcome)
		}
	}
	if state.ToolsPending != 0 {
		t.Errorf("tool.pending = %d, want 0: every call in this log has a "+
			"terminal record, so a non-zero pending count means a completion "+
			"failed to pair with its call", state.ToolsPending)
	}
	if state.ToolsDenied != 0 || state.ToolsAwaitingApproval != 0 {
		t.Errorf("denied = %d, awaiting approval = %d, want 0 and 0: this run "+
			"contains no tool.call_denied at all",
			state.ToolsDenied, state.ToolsAwaitingApproval)
	}

	// tool.last is what a one-line status row shows.
	if state.ToolLast.Actor != "frontend" || state.ToolLast.Seq != 100 {
		t.Errorf("tool.last = %+v, want the seq-100 frontend call: the row "+
			"shows the most recent call, and the most recent is the last in "+
			"log order", state.ToolLast)
	}
}

// TestTheActorIsReadFromTheTopLevelFieldNotThePayload is the defect this
// family would have shipped with, isolated -- and it is the one a passing
// measurement against the recorded log could not have caught.
//
// The two emission sites disagree about where the agent's name lives:
//
//   - internal/exec/fake.go (the --sim path, which wrote the recorded log)
//     stamps BOTH the top-level Actor and payload["agent"].
//   - internal/provider/executor.go FinishTurn (the REAL provider path) builds
//     the tool payload as {tool, call_id, args, argument_digest} -- no `agent`
//     key -- and puts the name only in Actor.
//
// So a payload-first fold passes every test built on the recorded log and
// attributes nothing in production. This test supplies the production shape:
// an event with `actor` and no payload.agent.
//
// THE FIRST VERSION OF THIS TEST DID NOT MEASURE THAT. It asserted the actor
// on an event carrying `actor` and no `agent`, where both reading orders
// return "backend" -- so the weld "read payload.agent first" ESCAPED the
// sweep, on the very assertion written to catch it. The guard was present and
// the input did not discriminate: this repo's recurring defect, found again by
// the instrument built for it.
//
// What discriminates is an event where the two fields DISAGREE. Measuring the
// recorded log for that produced a better reason than the one assumed:
//
//	type                  n   actor  payload.agent
//	exec.work_prepared   23       0             12
//	tool.call             4       4              4
//
// exec.* events carry the name ONLY in payload.agent -- exec.go's
// progressEvent() never sets Actor at all -- while tool.* carries both. So
// the fold genuinely needs both readings, and the order is what decides which
// field wins when a single event has both. That is what is pinned here.
func TestTheActorIsReadFromTheTopLevelFieldNotThePayload(t *testing.T) {
	// Exactly what executor.go FinishTurn emits: actor at the top level, and
	// a payload with no `agent` key.
	line := `{"seq":5,"type":"tool.call","source":"agent","actor":"backend",` +
		`"payload":{"tool":"bash","call_id":"call_abc",` +
		`"args":{"cmd":"go test ./..."},"argument_digest":"deadbeef"}}`

	events := replayBytes(t, []byte(line+"\n"))
	if events[0].Actor != "backend" {
		t.Fatalf("the decoder dropped the top-level actor: Actor = %q. Both "+
			"event shapes (kernel.Event on disk, host/v1.Event on the wire) "+
			"spell it `actor` at the top level, and json.Unmarshal discards a "+
			"key with no struct field without reporting anything",
			events[0].Actor)
	}

	state := fold.Fold(events)
	if len(state.ToolCalls) != 1 {
		t.Fatalf("folded %d tool calls, want 1", len(state.ToolCalls))
	}
	if state.ToolCalls[0].Actor != "backend" {
		t.Errorf("actor = %q, want \"backend\": this is the real provider "+
			"payload shape, which carries no `agent` key at all. arxi's own "+
			"reducer keys off e.Actor (decide.go's ToolCall case is "+
			"out.Member(e.Actor)), so `actor` is not a preference -- it is "+
			"what the core considers the member's identity",
			state.ToolCalls[0].Actor)
	}
	if state.ToolCalls[0].Tool != "bash" {
		t.Errorf("tool = %q, want \"bash\"", state.ToolCalls[0].Tool)
	}

	// The discriminating case: both fields present and DISAGREEING.
	//
	// `actor` must win. It is the field arxi's reducer uses, and it is the
	// event's own attribution; payload.agent is a subject annotation that
	// some emitters add (exec.go stamps a turn CHILD's agent into the payload
	// of a record whose actor is the parent, turn.go:1473). Preferring the
	// payload would therefore attribute an event to a name the core did not
	// consider the member for it.
	//
	// Without this case, "actor first" and "payload first" are
	// indistinguishable and the weld escapes.
	conflict := `{"seq":6,"type":"tool.call","actor":"backend",` +
		`"payload":{"agent":"frontend","tool":"read","call_id":"c9"}}`
	s2 := fold.Fold(replayBytes(t, []byte(conflict+"\n")))
	if len(s2.ToolCalls) != 1 {
		t.Fatalf("folded %d tool calls, want 1", len(s2.ToolCalls))
	}
	if s2.ToolCalls[0].Actor != "backend" {
		t.Errorf("with actor=backend and payload.agent=frontend the fold "+
			"chose %q, want \"backend\". This is the only input that "+
			"distinguishes the two reading orders: on every event where the "+
			"fields agree -- which is every tool event in the recorded log -- "+
			"a payload-first fold returns the same answer and the defect is "+
			"invisible", s2.ToolCalls[0].Actor)
	}
}

// TestTheAgentPayloadKeyIsStillReadWhenThereIsNoActor is the other half, and
// it is why actorName() has a fallback at all rather than reading one field.
//
// Measured across the recorded log:
//
//	exec.work_prepared   23 events: actor on 0, payload.agent on 12
//	exec.work_finished   23 events: actor on 0, payload.agent on 12
//
// exec.go's progressEvent() builds the event with Source and Payload and
// never sets Actor, so for the entire exec.* family the agent's name exists
// ONLY in the payload. A fold that read `actor` alone would attribute none of
// the 91 exec events, which is why removing the fallback is its own weld.
func TestTheAgentPayloadKeyIsStillReadWhenThereIsNoActor(t *testing.T) {
	// The exec.* shape: no top-level actor, name in the payload.
	line := `{"seq":7,"type":"tool.call",` +
		`"payload":{"agent":"backend","tool":"read","call_id":"c1"}}`

	state := fold.Fold(replayBytes(t, []byte(line+"\n")))
	if len(state.ToolCalls) != 1 {
		t.Fatalf("folded %d tool calls, want 1", len(state.ToolCalls))
	}
	if state.ToolCalls[0].Actor != "backend" {
		t.Errorf("actor = %q, want \"backend\": this event carries no "+
			"top-level actor, which is the shape every exec.* event has "+
			"(progressEvent never sets Actor). Dropping the payload fallback "+
			"leaves the whole exec.* family unattributed",
			state.ToolCalls[0].Actor)
	}
}

// TestDuplicateCallIDsDoNotCollapseDistinctCalls pins the log shape that makes
// the obvious implementation wrong.
//
// call_id is the natural key: the spec says tool.call_completed carries "the
// same call_id" as its call, so a map keyed on it is the first thing anyone
// would write. The measured log forbids it. All four tool.call events in
// testdata/serve/real_run.ndjson carry call_id "sim-provider-call-1", because
// internal/exec/fake.go:245 hardcodes that string -- one id for four calls by
// two different agents.
//
// A call_id-keyed fold reports ONE tool call for a run that made four, and it
// looks right: one plausible entry rather than an error. That is the exact
// class of failure this repo keeps finding, so the collision is a test and not
// a comment.
func TestDuplicateCallIDsDoNotCollapseDistinctCalls(t *testing.T) {
	// First, the premise: confirm the recorded log really does reuse one id.
	// If a future core fixes fake.go, this stops being the hazard it is, and
	// the reader should learn that here rather than trust the comment.
	ids := map[string]int{}
	for _, l := range strings.Split(strings.TrimRight(string(realRunLog(t)), "\n"), "\n") {
		var rec struct {
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal([]byte(l), &rec); err != nil {
			t.Fatalf("unparsable line in the recorded log: %v", err)
		}
		if rec.Type == "tool.call" {
			id, _ := rec.Payload["call_id"].(string)
			ids[id]++
		}
	}
	if len(ids) != 1 || ids["sim-provider-call-1"] != 4 {
		t.Skipf("the recorded log no longer reuses one call_id across four "+
			"calls (%v): the collision hazard was measured against a core "+
			"that did, so re-measure rather than assuming it still holds", ids)
	}

	state := fold.Fold(replayBytes(t, realRunLog(t)))
	if len(state.ToolCalls) != 4 {
		t.Fatalf("folded %d tool calls from four tool.call events sharing one "+
			"call_id, want 4: keying on call_id merges them, and the result "+
			"is a plausible-looking single entry rather than an error",
			len(state.ToolCalls))
	}

	// The two agents must still be distinguishable, which is the part a merge
	// destroys most visibly.
	byActor := map[string]int{}
	for _, c := range state.ToolCalls {
		byActor[c.Actor]++
	}
	if byActor["backend"] != 2 || byActor["frontend"] != 2 {
		t.Errorf("calls by actor = %v, want two each for backend and "+
			"frontend: one call_id identifies four calls here, so the fold "+
			"must pair positionally and not by id", byActor)
	}
}

// TestInterleavedCallsPairWithTheirOwnActor is the third shape the recorded
// log cannot measure.
//
// In testdata/serve/real_run.ndjson the eight tool events are strictly
// sequential -- call, completion, call, completion -- so there is never more
// than ONE open call at a time:
//
//	seq  32 call     backend   open->1
//	seq  33 terminal backend   open->0
//	seq  39 call     frontend  open->1
//	seq  40 terminal frontend  open->0
//
// With at most one candidate, "close the newest open call by THIS actor" and
// "close the newest open call by anyone" always pick the same entry. The weld
// that drops the actor check therefore ESCAPED the sweep: every assertion in
// this file passed with one agent's result credited to whichever call
// happened to be open.
//
// Two agents working concurrently is the normal case for a feature team -- the
// recorded run is just serialized by the simulator -- so this interleaves them
// deliberately: two calls open, then two completions in the opposite order.
// Only actor-aware pairing gets both right.
func TestInterleavedCallsPairWithTheirOwnActor(t *testing.T) {
	state := fold.Fold([]fold.Event{
		{Type: "tool.call", Seq: 1, Actor: "backend", Payload: map[string]any{
			"tool": "read", "call_id": "c1",
		}},
		{Type: "tool.call", Seq: 2, Actor: "frontend", Payload: map[string]any{
			"tool": "bash", "call_id": "c2",
		}},
		// Completions arrive in the OPPOSITE order to the calls, which is
		// what makes "newest open call by anyone" pick the wrong one.
		{Type: "tool.call_completed", Seq: 3, Actor: "backend", Payload: map[string]any{
			"tool": "read", "call_id": "c1", "result": "readme text",
		}},
		{Type: "tool.call_completed", Seq: 4, Actor: "frontend", Payload: map[string]any{
			"tool": "bash", "call_id": "c2", "result": "tests failed",
		}},
	})

	if len(state.ToolCalls) != 2 {
		t.Fatalf("folded %d tool calls, want 2", len(state.ToolCalls))
	}

	// Each result must land on its own agent's call. Asserting the RESULT and
	// not merely the outcome is the point: with the actor check removed both
	// calls still end up "completed", and only the text reveals that
	// backend's call was credited with frontend's output.
	byActor := map[string]fold.ToolActivity{}
	for _, c := range state.ToolCalls {
		byActor[c.Actor] = c
	}
	if got := byActor["backend"]; got.Tool != "read" || got.Result != "readme text" {
		t.Errorf("backend's call folded to tool=%q result=%q, want read / "+
			"\"readme text\": with two calls open, pairing that ignores the "+
			"actor closes whichever call is newest and credits one agent's "+
			"output to another", got.Tool, got.Result)
	}
	if got := byActor["frontend"]; got.Tool != "bash" || got.Result != "tests failed" {
		t.Errorf("frontend's call folded to tool=%q result=%q, want bash / "+
			"\"tests failed\"", got.Tool, got.Result)
	}
	if state.ToolsPending != 0 {
		t.Errorf("tool.pending = %d, want 0: both calls have a terminal "+
			"record", state.ToolsPending)
	}
}

// TestATerminalRecordWithNoPrecedingCallIsStillRecorded is the fourth log
// shape the recorded run does not contain, and it is one that runs in
// production.
//
// There are two emission paths and they are not symmetric:
//
//   - executor.go FinishTurn (canonical turn loop) emits tool.call THEN
//     tool.call_completed.
//   - executor.go CallTool (the effect-runner path, used when a tool is
//     invoked as a blueprint effect) emits ONLY tool.call_completed or
//     tool.call_denied. There is no preceding tool.call at all -- the file's
//     `kernel.ToolCall` appears exactly once, in FinishTurn.
//
// So "find the open call and close it" needs an else branch. Without one, the
// only evidence that an effect-dispatched tool ran is discarded and that path
// folds to an empty tool list. A subtract-on-finish counter would underflow
// here for the same reason the exec.* active count reached -7.
func TestATerminalRecordWithNoPrecedingCallIsStillRecorded(t *testing.T) {
	// Exactly what CallTool emits on the allow path: {tool, result}, with no
	// call_id and no tool.call before it.
	line := `{"seq":9,"type":"tool.call_completed","source":"agent",` +
		`"actor":"backend","payload":{"tool":"bash","result":"ok\n"}}`

	state := fold.Fold(replayBytes(t, []byte(line+"\n")))

	if len(state.ToolCalls) != 1 {
		t.Fatalf("folded %d tool calls, want 1: a tool.call_completed with no "+
			"preceding tool.call is what the effect-runner path emits, and it "+
			"is the only record that the tool ran. Dropping it makes that "+
			"path look idle", len(state.ToolCalls))
	}
	got := state.ToolCalls[0]
	if got.Outcome != "completed" || got.Tool != "bash" || got.Actor != "backend" {
		t.Errorf("standalone completion folded to %+v, want backend/bash/"+
			"completed", got)
	}
	if got.Result != "ok\n" {
		t.Errorf("result = %q, want \"ok\\n\" verbatim: the core carries the "+
			"command's own output, and a non-zero exit is an ANSWER rather "+
			"than an error, so the host must not interpret it", got.Result)
	}
	// It must NOT be counted as pending: the outcome is known.
	if state.ToolsPending != 0 {
		t.Errorf("tool.pending = %d, want 0: the call has a terminal record, "+
			"so nothing is in flight", state.ToolsPending)
	}
}

// TestAnAskDenialIsAQuestionAndNotAFailure pins the distinction the spec is
// explicit about and that a host is most tempted to collapse.
//
// spec/events.md: `tool.call_denied` with policy:"ask" "is **not an error**:
// it is a question." The core keeps the two policies apart deliberately --
// executor.go: "Losing it would collapse 'not allowed' and 'not yet approved'
// into one outcome, and those have different remedies -- one needs a policy
// change, the other an approval."
//
// So the fold counts both as denied and only "ask" as awaiting approval. A
// host that showed "ask" as a failure would tell the user a tool was refused
// at the exact moment the run is waiting for them to permit it.
func TestAnAskDenialIsAQuestionAndNotAFailure(t *testing.T) {
	events := []fold.Event{
		{Type: "tool.call", Seq: 1, Actor: "backend", Payload: map[string]any{
			"tool": "write", "call_id": "c1",
		}},
		{Type: "tool.call_denied", Seq: 2, Actor: "backend", Payload: map[string]any{
			"tool": "write", "call_id": "c1", "policy": "ask",
		}},
		{Type: "tool.call", Seq: 3, Actor: "frontend", Payload: map[string]any{
			"tool": "bash", "call_id": "c2",
		}},
		{Type: "tool.call_denied", Seq: 4, Actor: "frontend", Payload: map[string]any{
			"tool": "bash", "call_id": "c2", "policy": "deny",
		}},
	}

	state := fold.Fold(events)

	if state.ToolsDenied != 2 {
		t.Errorf("tool.denied = %d, want 2: both policies are denials",
			state.ToolsDenied)
	}
	if state.ToolsAwaitingApproval != 1 {
		t.Errorf("tool.awaiting_approval = %d, want 1: only the \"ask\" denial "+
			"is a question a human can answer. \"deny\" is a decision and "+
			"needs a policy change instead, which is why the core carries the "+
			"policy rather than a boolean", state.ToolsAwaitingApproval)
	}
	// The count above is NOT enough on its own, and the sweep proved it: with
	// one ask and one deny, counting the wrong policy also yields 1. The weld
	// "count deny instead of ask" escaped on that assertion.
	//
	// What discriminates is an asymmetric population, so the two policies
	// cannot produce the same total. Two asks and one deny: awaiting must be
	// 2, and a fold counting denials reports 1.
	asym := fold.Fold([]fold.Event{
		{Type: "tool.call_denied", Seq: 1, Actor: "a", Payload: map[string]any{
			"tool": "write", "policy": "ask",
		}},
		{Type: "tool.call_denied", Seq: 2, Actor: "b", Payload: map[string]any{
			"tool": "write", "policy": "ask",
		}},
		{Type: "tool.call_denied", Seq: 3, Actor: "c", Payload: map[string]any{
			"tool": "bash", "policy": "deny",
		}},
	})
	if asym.ToolsAwaitingApproval != 2 {
		t.Errorf("with two asks and one deny, awaiting_approval = %d, want 2: "+
			"a symmetric one-and-one population cannot tell \"counts asks\" "+
			"from \"counts denies\" -- both give 1 -- so the asymmetry is "+
			"what makes this assertion measure the policy and not the total",
			asym.ToolsAwaitingApproval)
	}
	if asym.ToolsDenied != 3 {
		t.Errorf("denied = %d, want 3: all three are denials regardless of "+
			"policy", asym.ToolsDenied)
	}
	// Neither is pending: both have a terminal record. A denial is an
	// outcome, not the absence of one.
	if state.ToolsPending != 0 {
		t.Errorf("tool.pending = %d, want 0: a denied call is not in flight",
			state.ToolsPending)
	}
	for i, c := range state.ToolCalls {
		if c.Outcome != "denied" {
			t.Errorf("call %d: outcome = %q, want \"denied\"", i, c.Outcome)
		}
	}
	if state.ToolCalls[0].Policy != "ask" || state.ToolCalls[1].Policy != "deny" {
		t.Errorf("policies = %q, %q; want \"ask\", \"deny\": collapsing them "+
			"loses the remedy", state.ToolCalls[0].Policy,
			state.ToolCalls[1].Policy)
	}
}

// TestAnAskDenialDoesNotDoubleCountAsATodo guards the fix above from
// introducing a second bug.
//
// The ask path already produces a todo: the core's reducer turns
// tool.call_denied(policy:ask) into an inbox item and emits agent.blocked
// (decide.go applyToolDenied returns an AskHuman effect), and this fold
// already appends a TodoItem on agent.blocked. Appending one in the
// tool.call_denied case as well would list every approval request twice, and
// the count feeds a header badge.
func TestAnAskDenialDoesNotDoubleCountAsATodo(t *testing.T) {
	events := []fold.Event{
		{Type: "tool.call", Seq: 1, Actor: "backend", Payload: map[string]any{
			"tool": "write", "call_id": "c1",
		}},
		{Type: "tool.call_denied", Seq: 2, Actor: "backend", Payload: map[string]any{
			"tool": "write", "call_id": "c1", "policy": "ask",
		}},
		// What the core emits next for the same block.
		{Type: "agent.blocked", Seq: 3, Actor: "backend", Payload: map[string]any{
			"actor": "backend", "blocked_on": "approval", "task": "approve write",
		}},
	}

	state := fold.Fold(events)
	if len(state.Todos) != 1 {
		t.Errorf("todos = %d, want 1: the denial and the agent.blocked that "+
			"follows it describe ONE pending approval. A fold that appends a "+
			"todo on both lists every approval request twice", len(state.Todos))
	}
	if state.TodosCount != 1 {
		t.Errorf("todos.count = %d, want 1", state.TodosCount)
	}
}

// TestAMemberRunningAToolReportsTheToolState closes the one documented member
// state nothing ever set.
//
// event.go documents the vocabulary as idle/thinking/tool/submitted/waiting/
// inactive/failed. Before this change the fold set idle, thinking, waiting and
// failed -- "tool" was declared and unreachable, so a member mid-tool-call
// rendered as "thinking", indistinguishable from waiting on the model.
//
// The transition back is to "thinking" and NOT "idle", matching arxi's reducer
// (decide.go ToolCallCompleted: `if m.State == MemberTool { m.State =
// MemberThinking }`). The turn is not over: the result is reinjected and the
// model is called again. Reporting idle would say the agent stopped while it
// is mid-turn.
func TestAMemberRunningAToolReportsTheToolState(t *testing.T) {
	base := []fold.Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{"simulated": true}},
		{Type: "agent.activated", Seq: 2, Actor: "backend", Payload: map[string]any{
			"agent": "backend", "role": "backend",
		}},
		{Type: "tool.call", Seq: 3, Actor: "backend", Payload: map[string]any{
			"tool": "read", "call_id": "c1",
		}},
	}

	// Mid-call: the member is in the tool state.
	mid := fold.Fold(base)
	if got := memberState(t, mid, "backend"); got != "tool" {
		t.Errorf("member state during a tool call = %q, want \"tool\": the "+
			"value is in the documented vocabulary and was unreachable, so a "+
			"member mid-call rendered as \"thinking\"", got)
	}

	// After the completion: back to thinking, because the turn continues.
	done := fold.Fold(append(base, fold.Event{
		Type: "tool.call_completed", Seq: 4, Actor: "backend",
		Payload: map[string]any{"tool": "read", "call_id": "c1", "result": "..."},
	}))
	if got := memberState(t, done, "backend"); got != "thinking" {
		t.Errorf("member state after a tool completed = %q, want \"thinking\": "+
			"the result is reinjected and the model is called again, so the "+
			"turn is not over. arxi's reducer makes the same transition", got)
	}
}

// memberState finds one member in the derived team.members list. The list is
// built from a map, so it has no stable order and must be searched by id.
func memberState(t *testing.T, s fold.State, id string) string {
	t.Helper()
	for _, m := range s.TeamMembers {
		if m.ID == id {
			return m.State
		}
	}
	t.Fatalf("no member %q in team.members (%+v)", id, s.TeamMembers)
	return ""
}

// TestToolCallsSurviveTheReplayPathToo is the guard against fixing the decoder
// in one place.
//
// decodeEvent is one function precisely so the live stream and the replay
// cannot disagree about the bytes -- but both call sites then REBUILT the
// event field by field (Type, Seq, Payload), so a field learned by the decoder
// would still not reach the fold. Actor was exactly that field. Both sites now
// pass the decoded value through whole.
//
// This reads a line through the replay path and asserts the actor survived,
// which is the half a unit test of decodeEvent alone would not cover.
func TestToolCallsSurviveTheReplayPathToo(t *testing.T) {
	line := `{"seq":11,"type":"tool.call","actor":"backend",` +
		`"payload":{"tool":"read","call_id":"c1"}}`

	events := replayBytes(t, []byte(line+"\n"))
	if len(events) != 1 {
		t.Fatalf("decoded %d events, want 1", len(events))
	}
	if events[0].Actor != "backend" {
		t.Errorf("Actor = %q, want \"backend\": the decoder's callers used to "+
			"rebuild the event field by field and dropped everything the "+
			"literal did not name, which is the same \"a decoder duplicated "+
			"will disagree with itself\" failure decodeEvent was consolidated "+
			"to prevent", events[0].Actor)
	}
}
