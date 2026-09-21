package driver

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// Every event this repo has ever folded was written by this repo. The Phase 0
// mock emits eight hand-authored events; the ndjson_test.go fixtures are
// hand-authored too. That makes the fold's event types and payload keys a
// guess about the core dressed up as a contract.
//
// testdata/serve/real_run.ndjson is a log the arxi core actually wrote: 121
// events from
//
//	arxi run start "hola" --actor examples/feature-team.yaml --budget 1 --sim
//
// copied verbatim out of runs/<id>/events.ndjson. It is the first
// core-produced input this fold has ever seen, and it found three defects.

func realRunLog(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/serve/real_run.ndjson")
	if err != nil {
		t.Fatalf("read the recorded real run log: %v", err)
	}
	return raw
}

// replayBytes writes the given log content to a temp file and decodes it.
func replayBytes(t *testing.T, content []byte) []fold.Event {
	t.Helper()
	path := t.TempDir() + "/events.ndjson"
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	events, err := Replay(path)
	if err != nil {
		t.Fatalf("the core's own log was rejected by the host's decoder: %v", err)
	}
	return events
}

// TestTheRealLogDecodesWithEverySequenceNumberIntact is the field-name check
// the bridge never had.
//
// The core has two event shapes and they do not agree. On disk the log record
// is internal/kernel.Event, whose sequence field is `seq`. On the wire, the
// event inside a `run.attach` notification is host/v1.Event, whose sequence
// field is `sequence`. The decoder read `seq`, so it was right about the file
// and wrong about the socket -- and being wrong is not an error:
// json.Unmarshal leaves the field at zero, so every event folds with Seq 0 and
// the log looks like one indistinguishable batch.
//
// This test pins the half that works, so the half that did not cannot be
// fixed by breaking it.
func TestTheRealLogDecodesWithEverySequenceNumberIntact(t *testing.T) {
	events := replayBytes(t, realRunLog(t))
	if len(events) == 0 {
		t.Fatal("no events decoded from a 121-event log")
	}

	// A zero Seq on a real event means the field name is wrong, not that the
	// event is unordered: the log writer assigns seq starting at 1.
	for i, e := range events {
		if e.Seq == 0 {
			t.Fatalf("event %d (type %q) decoded with Seq 0. The core's log writer "+
				"assigns seq from 1, so a zero means this decoder is reading a "+
				"field the core does not write -- and the symptom is a log that "+
				"looks unordered rather than an error", i, e.Type)
		}
		if e.Type == "" {
			t.Fatalf("event %d decoded with no type", i)
		}
	}

	// Strictly increasing, which is what makes the fold's ordering meaningful.
	for i := 1; i < len(events); i++ {
		if events[i].Seq <= events[i-1].Seq {
			t.Errorf("seq went %d -> %d at event %d: the log is append-only and "+
				"seq-numbered, so this is the decoder losing order",
				events[i-1].Seq, events[i].Seq, i)
		}
	}
}

// TestTheWireEventShapeIsDecodedToo is the other half, and it failed.
//
// host/v1.Event serializes its sequence as `sequence` and its timestamp as
// `time`; kernel.Event uses `seq` and `ts`. A notification from run.attach
// carries the former. Feeding one to the old decoder produced an event with
// Seq 0 -- no error, no warning, just a fold that cannot order anything.
//
// The fix is not to pick a side: both shapes are real, and which one arrives
// depends on whether the host is reading the file or the socket. Accept either
// and refuse only a record that carries neither.
func TestTheWireEventShapeIsDecodedToo(t *testing.T) {
	// One event in host/v1.Event spelling, which is what `run.attach`
	// notifications contain (cmd/arxi/serve_stream.go:eventNotification).
	wire := `{"sequence":7,"id":"ev-1","time":"2026-09-21T16:21:41Z",` +
		`"type":"llm.response","source":"agent","payload":{"text":"hola"}}`

	events := replayBytes(t, []byte(wire+"\n"))
	if len(events) != 1 {
		t.Fatalf("decoded %d events, want 1", len(events))
	}
	if events[0].Seq != 7 {
		t.Errorf("Seq = %d, want 7. host/v1.Event spells the sequence "+
			"`sequence`; a decoder that only reads `seq` silently zeroes it, so "+
			"every event off the socket folds as if it were the first",
			events[0].Seq)
	}
	// The payload must survive the other spelling too, or the event is
	// ordered correctly and empty.
	if events[0].Payload["text"] != "hola" {
		t.Errorf("payload = %v: the wire shape lost its payload, so the event "+
			"folds in the right order with nothing in it", events[0].Payload)
	}
}

// TestAnEventWithNoSequenceAtAllIsRefused is the guard that keeps the fix
// above from becoming a second silent failure.
//
// Accepting both spellings must not become accepting anything: a record with
// neither field has an unknown position in the log, and folding it at Seq 0
// puts it before every real event. That is a wrong frame, which this repo
// holds to be worse than an error.
func TestAnEventWithNoSequenceAtAllIsRefused(t *testing.T) {
	path := t.TempDir() + "/noseq.ndjson"
	if err := os.WriteFile(path, []byte(`{"type":"llm.response","payload":{}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Replay(path)
	if err == nil {
		t.Fatal("an event with neither `seq` nor `sequence` was accepted. Its " +
			"position in the log is unknown, and folding it at zero orders it " +
			"before every real event")
	}
	if !strings.Contains(err.Error(), "seq") {
		t.Errorf("the refusal does not name the missing field: %v", err)
	}
}

// TestTheRealLogFoldsWithoutLosingTheRunState is the end-to-end measurement:
// 121 core-written events through the host's fold.
func TestTheRealLogFoldsWithoutLosingTheRunState(t *testing.T) {
	events := replayBytes(t, realRunLog(t))
	state := fold.Fold(events)

	// run.started was emitted with simulated:true, so the fold must have read
	// that key. This is the cheapest end-to-end proof that a payload key
	// matches: a fold reading a key the core does not write leaves the
	// pre-run default in place with no error, and the frame then claims a
	// live run for a simulated one.
	//
	// It failed. The fold ended at "live", because `agent.activated` set
	// AgentMode = "live" unconditionally and four of those arrive after
	// run.started. The mock never caught it: the mock's run.started carries
	// simulated:false, so both paths agree there and the overwrite is
	// invisible. See TestSimulatedIsNotOverwrittenByAgentActivity.
	if state.AgentMode != "sim" {
		t.Errorf("agent mode = %q, want \"sim\": the run was started with --sim "+
			"and the core wrote simulated:true on run.started. The frame labels "+
			"a simulation as a live run, which is the one thing a cost-bearing "+
			"host must not get wrong", state.AgentMode)
	}

	// budget_usd was 1, so the captured budget must be exactly 1000
	// microunits. Asserting the value rather than "not zero" is deliberate:
	// a scale error (×1 or ×1_000_000) passes a non-zero check and puts the
	// wrong ceiling on screen.
	if state.BudgetMicrounits != 1000 {
		t.Errorf("budget = %d microunits, want 1000: run.started carried "+
			"budget_usd:1 and microunits are USD×1000. session.tokens_used is "+
			"derived from this, so a wrong scale misreports every run's ceiling",
			state.BudgetMicrounits)
	}

	// Four llm.response events carry cost_usd 0.01 each, and the core's own
	// run summary reported 0.0400 USD spent. 0.01 × 1000 = 10 microunits per
	// response, so the fold must land on exactly 40.
	//
	// The exact figure matters: an injection sweep first "caught" a broken
	// accumulator only because the weld failed to compile, which is not a
	// measurement. Re-welded compilably (cost × 0), this assertion is what
	// fails.
	if state.CostMicrounits != 40 {
		t.Errorf("cost = %d microunits, want 40: the core's run summary reported "+
			"0.0400 USD across four llm.response events at cost_usd 0.01. A "+
			"fold that disagrees with the core about spend is worse than one "+
			"that shows nothing", state.CostMicrounits)
	}
}

// TestTheRealLogCoverageIsMeasuredNotAssumed states the gap as a number under
// test rather than a note in a comment.
//
// It has now been re-measured once, deliberately, which is what the pin was
// for. The previous figure was 13/122: the fold knew ten event types and the
// real log contains sixteen, so the host was blank for the entire execution
// of a run and only twitched on the four llm.response events. Teaching it the
// exec.* family and run.result moves the figure to 105/122.
//
// The arithmetic is spelled out because the note that motivated this change
// had it wrong. "exec.* and run.result, which is 89% of the real log" was
// two numbers welded together: 109/122 = 89.3% is EVERYTHING unhandled, while
// exec.* (91) + run.result (1) = 92 = 75.4%. Acting on the 89% figure would
// have meant reporting full coverage at the end of a change that leaves 17
// events invisible.
func TestTheRealLogCoverageIsMeasuredNotAssumed(t *testing.T) {
	events := replayBytes(t, realRunLog(t))

	var known int
	unhandled := map[string]int{}
	for _, e := range events {
		if fold.Handles(e.Type) {
			known++
		} else {
			unhandled[e.Type]++
		}
	}

	if known == 0 {
		t.Fatal("the fold recognised none of the core's event types, which " +
			"means the type vocabulary is wrong end to end")
	}
	// The re-measured figure, pinned again. A Handles() that claimed
	// everything (or nothing) would move this and say so.
	if known != 105 {
		t.Errorf("coverage = %d/%d events folded, want 105: the figure is pinned "+
			"so that a change in what the fold handles -- or in what the core "+
			"emits -- is a deliberate re-measurement and not a drift",
			known, len(events))
	}
	t.Logf("real-log coverage: %d/%d events folded; %d ignored across %d "+
		"unhandled types %v", known, len(events), len(events)-known,
		len(unhandled), sortedKeys(unhandled))

	// The families that are now handled. Pinned as PRESENT so that a
	// regression which quietly drops one of them fails here rather than
	// showing up as a blank progress indicator.
	for _, ty := range []string{
		"exec.step_completed", "exec.work_started", "exec.work_prepared",
		"exec.work_finished", "run.result",
	} {
		if unhandled[ty] != 0 {
			t.Errorf("%s went back to being unhandled: the fold learned this "+
				"type deliberately and losing it makes the host silent for "+
				"%d events", ty, unhandled[ty])
		}
	}

	// What is STILL invisible, named and counted rather than left as a
	// rounding error. These seventeen are the next piece of work: tool.call
	// and tool.call_completed are what the agent actually did, and stage.*
	// is where the run is in its blueprint.
	stillBlind := map[string]int{
		"tool.call": 4, "tool.call_completed": 4,
		"stage.entered": 2, "stage.submitted": 4, "stage.advanced": 1,
		"timer.scheduled": 1, "timer.cancelled": 1,
	}
	var blindTotal int
	for ty, want := range stillBlind {
		if unhandled[ty] != want {
			t.Errorf("%s: %d unhandled, expected %d. The remaining blind spot "+
				"is tracked as an exact figure so that closing part of it is a "+
				"re-measurement and not a drift", ty, unhandled[ty], want)
		}
		blindTotal += want
	}
	if blindTotal+known != len(events) {
		t.Errorf("the accounting does not close: %d folded + %d blind != %d "+
			"events. Every event in the log must be either handled or named "+
			"in the blind list, or the coverage figure is a guess",
			known, blindTotal, len(events))
	}
}

// TestSimulatedIsNotOverwrittenByAgentActivity is the defect the real log
// found, isolated.
//
// `agent.activated` set AgentMode = "live" with a comment reading "the run is
// live and not simulated-idle". That conflated two different questions: who is
// working (AgentWorking, which the same case already sets) and whose money is
// being spent (simulated vs live, which only run.started answers). A simulated
// run activates agents exactly like a real one, so every sim run was
// relabelled live the moment it did any work.
//
// The mock could not catch this. Its run.started carries simulated:false, so
// the overwrite wrote the value that was already there.
func TestSimulatedIsNotOverwrittenByAgentActivity(t *testing.T) {
	events := []fold.Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{
			"run_id": "r1", "simulated": true, "budget_usd": 1.0,
		}},
		{Type: "agent.activated", Seq: 2, Payload: map[string]any{"agent": "backend"}},
	}

	state := fold.Fold(events)
	if state.AgentMode != "sim" {
		t.Errorf("agent mode = %q, want \"sim\": agent.activated answers who is "+
			"working, not whose money is at stake. Only run.started knows "+
			"whether the run is simulated, and a simulated run activates "+
			"agents exactly like a real one", state.AgentMode)
	}
	// The thing agent.activated legitimately owns must still be set.
	if !state.AgentWorking {
		t.Error("agent.activated no longer marks the agent as working: the fix " +
			"removed the wrong half")
	}
}

// TestALiveRunIsStillReportedLive is the guard against fixing the above by
// deleting the distinction.
//
// run.started with simulated:false must still produce "live". A fold that
// simply never says "live" would pass the sim test and be just as wrong in the
// other direction.
func TestALiveRunIsStillReportedLive(t *testing.T) {
	events := []fold.Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{
			"run_id": "r1", "simulated": false, "budget_usd": 1.0,
		}},
		{Type: "agent.activated", Seq: 2, Payload: map[string]any{"agent": "backend"}},
	}

	state := fold.Fold(events)
	if state.AgentMode != "live" {
		t.Errorf("agent mode = %q, want \"live\": a real run must be labelled "+
			"live, or the sim fix has merely removed the distinction instead of "+
			"assigning it to the event that knows", state.AgentMode)
	}
}

// TestPayloadKeysTheFoldReadsExistInTheRealLog is the inverse check, and it is
// the one a mock can never do.
//
// The fold reaches into payloads by key. Every one of those keys was written
// against the mock, so a key the core spells differently reads as a missing
// value -- an empty string in a frame, not an error. This walks the real log
// and asserts that the keys the fold cares about are keys the core actually
// writes.
func TestPayloadKeysTheFoldReadsExistInTheRealLog(t *testing.T) {
	// Keys the fold reads and the core writes UNCONDITIONALLY, confirmed at
	// the emission site (arxi internal/provider/executor.go).
	//
	// `text` and `model` are deliberately absent from this list, and that is
	// the finding. executor.go emits them conditionally:
	//
	//	if text := responseText(final); text != "" { llm["text"] = text }
	//
	// so a turn with no text has no key at all -- and in the measured
	// simulated run, NONE of the four llm.response events carries `text` or
	// `model`. The fold treats both as always-present, which is why a real
	// sim run folds to a transcript of empty assistant lines. Demanding them
	// here would be demanding the core change; the correct assertion is on
	// the keys it does promise, with the conditional ones pinned separately
	// in TestASimRunFoldsToEmptyAssistantLines.
	want := map[string][]string{
		"run.started":  {"run_id", "budget_usd", "simulated"},
		"llm.response": {"agent", "cost_usd"},
	}

	found := map[string]map[string]bool{}
	for _, l := range strings.Split(strings.TrimRight(string(realRunLog(t)), "\n"), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		var rec struct {
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal([]byte(l), &rec); err != nil {
			t.Fatalf("the recorded log has a line the test cannot parse: %v", err)
		}
		if _, ok := want[rec.Type]; !ok {
			continue
		}
		if found[rec.Type] == nil {
			found[rec.Type] = map[string]bool{}
		}
		for k := range rec.Payload {
			found[rec.Type][k] = true
		}
	}

	for evType, keys := range want {
		if found[evType] == nil {
			t.Errorf("the real log contains no %s event, so the fold's handling of "+
				"it has still never been measured", evType)
			continue
		}
		for _, k := range keys {
			if !found[evType][k] {
				t.Errorf("the fold reads %s.payload[%q] and the core does not write "+
					"that key. The value reads as empty, so the frame is wrong and "+
					"nothing reports it", evType, k)
			}
		}
	}

	// The conditional keys, pinned as absent. This is the measurement behind
	// the comment above: if a future core starts emitting `text` on a
	// simulated turn, this fails and the blank-transcript finding below has
	// to be re-measured rather than quietly becoming untrue.
	for _, k := range []string{"text", "model"} {
		if found["llm.response"][k] {
			t.Errorf("llm.response now carries %q in a simulated run: the "+
				"blank-transcript finding was measured against a core that "+
				"omitted it, so re-measure rather than assuming it still holds", k)
		}
	}
}

// TestASimRunFoldsToEmptyAssistantLines records the third defect the real log
// found, as a measurement rather than a fix.
//
// The core omits `text` from llm.response when the turn produced none, which
// is every turn of a simulated run. The fold appends an assistant ChatLine
// regardless, so a sim run's transcript is blank bubbles. Whether the right
// answer is to skip the line or to render "(simulated)" is a design question
// about the chat scene and not a decision to smuggle into a driver test -- so
// this pins the current behaviour and names the choice instead of guessing.
func TestASimRunFoldsToEmptyAssistantLines(t *testing.T) {
	events := replayBytes(t, realRunLog(t))
	state := fold.Fold(events)

	var blank int
	for _, line := range state.History {
		if line.Role == "assistant" && line.Text == "" {
			blank++
		}
	}

	if blank == 0 {
		t.Skip("the fold no longer produces blank assistant lines for a sim " +
			"run: the design choice was made, so re-measure this")
	}
	t.Logf("a simulated run folds to %d blank assistant line(s) out of %d "+
		"transcript lines: the core omits llm.response.text when the turn "+
		"produced none, and the fold appends a bubble anyway. The chat scene "+
		"needs a deliberate answer here (skip the line, or label it), which "+
		"is why this is measured and not silently patched",
		blank, len(state.History))
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Small n; insertion sort keeps the log line stable without importing
	// sort for one call.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
