package fold

import (
	"strconv"
	"strings"
)

// State is the projected view-state from a log of events. It is the only thing
// a scene reads: the scene says form, the fold says content. The fold never
// waits on the scene, never imports UI packages, and never writes back — it is
// a pure function of events already received.
type State struct {
	// Run-state binds (mapped from arxi core's event catalog, docs/BINDS.md §4.1)
	History           []ChatLine   `json:"chat.history"`
	ThinkingText      string       `json:"thinking.text"`
	AgentWorking      bool         `json:"agent.working"`
	AgentMode         string       `json:"agent.mode"`
	ModelName         string       `json:"model.name"`
	UsageIn           uint64       `json:"usage.in"`
	UsageOut          uint64       `json:"usage.out"`
	UsageDelta        string       `json:"usage.delta"`
	SessionTokensUsed uint64       `json:"session.tokens_used"`
	TodosCount        uint         `json:"todos.count"`
	TeamMembers       []TeamMember `json:"team.members"`
	QuiescentDiag     string       `json:"run.quiescent.diagnosis"`

	// Run verdict (run.result). The core emits run.result ONLY on success:
	// kernel/decide.go's `case RunResult` sets Status = StatusSucceeded
	// unconditionally, and the two emission sites (decide.go:554 "all stages
	// completed" and decide.go:720 "last stage expired, advancing") are both
	// advance paths. Failure never produces a run.result -- it produces
	// run.cancelled, run.expired, or a status transition with no event of its
	// own (quiescent-with-no-observer, inbox timeout with on_timeout:fail).
	//
	// So reading run.result answers "did the run succeed", and its ABSENCE is
	// not "it failed" -- it is "no verdict yet". Collapsing those two into one
	// boolean is exactly the class of error this repo keeps finding, so the
	// bind is a three-valued string and not a bool.
	RunOutcome string `json:"run.outcome"` // "" (no verdict yet) | "succeeded"
	// RunSummary is run.result.summary: the run's own sentence about itself.
	RunSummary string `json:"run.summary"`
	// RunResultFrom is run.result.result_from: which blueprint rule produced
	// the verdict. Present on the stage-completion path, absent on the
	// expiry path, so an empty string here is meaningful rather than missing.
	RunResultFrom string `json:"run.result_from"`

	// Durable execution progress (exec.*). This family is 91 of the 122
	// events in the measured real run -- 74.6% of the log -- and the fold
	// ignored all of it, so the host was blank for the entire execution of a
	// run and only twitched on the four llm.response events.
	//
	// These are operational facts, not reducer inputs: kernel/event.go says
	// "They never wake watchers or cause quiescence decisions". The host
	// treats them the same way -- they drive a progress indicator, never a
	// verdict.
	ExecActive    uint   `json:"exec.active"`    // started and not yet finished
	ExecCompleted uint   `json:"exec.completed"` // terminal status "completed"
	ExecFailed    uint   `json:"exec.failed"`    // terminal status "failed"
	ExecUnknown   uint   `json:"exec.unknown"`   // terminal status "unknown"
	ExecCursor    int64  `json:"exec.cursor"`    // highest exec.step_completed source_seq
	ExecPhase     string `json:"exec.phase"`     // "idle" | "working"

	// Tool activity (tool.*). Eight events in the measured run, and the only
	// ones in the whole log that say WHAT THE AGENT DID. exec.* is how much
	// durable work is in flight and stage.* is where the run sits in its
	// blueprint; both are position and plumbing. "read README.md" is the thing
	// a person watching the run is actually trying to see, and the host showed
	// none of it.
	//
	// The count is deliberately not the reason this family was chosen over
	// stage.* (7 events). A progress bar that moves without saying what moved
	// it is the blank screen with extra steps.
	ToolCalls []ToolActivity `json:"tool.calls"`
	// ToolLast is the most recent call, which is what a one-line status row
	// shows. Derived from the list rather than maintained separately: two
	// places writing the same fact is two places to get it wrong, the same
	// reason ExecPhase is derived.
	ToolLast ToolActivity `json:"tool.last"`
	// ToolCallsTotal counts tool.call events seen. Equal to len(ToolCalls)
	// today; asserted so that a future change which trims the list is a
	// deliberate re-measurement rather than a drift.
	ToolCallsTotal uint `json:"tool.calls_total"`
	// ToolsPending is calls with no terminal record yet.
	//
	// It is derived from the call list and NOT from a "started minus
	// finished" counter, because that arithmetic is exactly what drove
	// ExecActive to -7: tool.call_completed can arrive with no preceding
	// tool.call at all. executor.go's CallTool -- the effect-runner path,
	// used when a tool is invoked as a blueprint effect rather than from
	// inside the canonical turn loop -- emits ONLY the completion or the
	// denial. FinishTurn (the turn-loop path) emits the pair. So a log may
	// legally contain terminal records that never had an opening, and a
	// decrementing counter would underflow on precisely the path that runs in
	// production.
	ToolsPending uint `json:"tool.pending"`
	// ToolsDenied counts tool.call_denied. It is NOT an error count: the spec
	// is explicit that policy:"ask" is "not an error: it is a question". See
	// the tool.call_denied case in apply().
	ToolsDenied uint `json:"tool.denied"`
	// ToolsAwaitingApproval counts denials with policy "ask" -- the ones a
	// human can unblock by answering. Separated from ToolsDenied because the
	// remedies differ: "deny" needs a policy change, "ask" needs a reply, and
	// the core keeps them apart for that reason (executor.go: "Losing it would
	// collapse 'not allowed' and 'not yet approved' into one outcome, and
	// those have different remedies").
	ToolsAwaitingApproval uint `json:"tool.awaiting_approval"`

	// Blueprint position (stage.*). Seven events in the measured run, and the
	// only ones that answer "where is this run". exec.* says how much durable
	// work is in flight and tool.* says what the agent did; neither says which
	// phase of the plan is running, so a run that finished building and is now
	// reviewing looked identical to one still building.
	//
	// THE TOTAL IS NOT IN THE LOG, and that is the fact that shapes this whole
	// surface. Every stage.* payload was read: stage.entered carries
	// {stage, index}, stage.advanced {from, to, to_index}, stage.submitted
	// {agent, stage}. None carries a stage count, and neither does run.started
	// (measured: its payload is actor/blueprint_sha/budget_usd/max_turns/
	// prompt/run_id/simulated/workspace/effective_config_*). The count lives in
	// Config.Stages, which is the frozen blueprint, and the fold does not read
	// the blueprint -- it reads the log.
	//
	// So there is no "stage 2 of 5" here, and inventing a denominator is the
	// error this field exists to refuse. The tempting one is
	// "highest index seen + 1", which renders "stage 1 of 1" for the whole
	// first stage of a five-stage run and then silently grows -- a progress
	// bar that is always full and always right by construction. An index with
	// no total is honest; a total that is a running maximum is a lie with a
	// number on it.
	StageName string `json:"stage.name"` // current stage, "" before the first entry
	// StageIndex is the zero-based position, or -1 before any stage is
	// entered. It mirrors the core's own sentinel (applyRunStarted sets
	// StageIndex = -1 with the comment "Starting at 0 would make the first
	// stage.entered look like a re-entry"), so a fold of the same log agrees
	// with the core's reducer about whether a stage has been entered at all.
	// Zero would be indistinguishable from "in the first stage".
	StageIndex int `json:"stage.index"`
	// StagePrev is stage.advanced.from: the stage just left. Empty until the
	// first advance, which is what distinguishes "first stage" from "advanced
	// into this one".
	StagePrev string `json:"stage.prev"`
	// StageAdvances counts stage.advanced events -- transitions actually
	// taken, not stages seen. It is NOT a denominator (see StageName) and not
	// derivable from StageIndex: the expiry path at decide.go:711 emits
	// advanced+entered as a pair just like the quorum path, so both routes
	// move the index, but only counting the event says how many transitions
	// the run made.
	StageAdvances uint `json:"stage.advances"`
	// StageSubmissions is the members that have submitted to the CURRENT
	// stage, in log order. Cleared on stage.entered, because the core clears
	// it there too (applyStageEntered: `m.Submitted = false` for every member)
	// and for the same reason -- it scopes the set to one stage so a submit
	// cannot leak into the next.
	//
	// A slice and not a set: it is displayed, so its order must be the log's
	// and not a map's. See deriveTeamMembers for what map order already cost
	// this package.
	StageSubmissions []string `json:"stage.submissions"`
	// StageSubmittedCount is len(StageSubmissions), for a header badge that
	// must not re-walk the list. Derived, never maintained alongside.
	StageSubmittedCount uint `json:"stage.submitted_count"`

	// agent.todos is the list of pending agent tasks (BINDS.md §4.1). Each
	// entry carries the task text, what it is blocked on, and the actor that
	// owns it. A list node bound to agent.todos renders one row per entry.
	Todos []TodoItem `json:"agent.todos"`

	// Agent blocked surface (BINDS.md §4.2)
	BlockedRef   map[string]any `json:"agent.blocked.blocked_ref"`
	BlockedOn    string         `json:"agent.blocked.blocked_on"`
	BlockedActor string         `json:"agent.blocked.actor"`

	// View-state binds (arxi-tui's own contract, docs/BINDS.md §4.3)
	UserInput    string       `json:"user.input"`
	SlashActive  bool         `json:"slash.active"`
	SlashTyped   string       `json:"slash.typed"`
	SlashMatches []SlashMatch `json:"slash.matches"`
	// SlashHint is the footer line shown while the slash menu is open (BINDS.md
	// §4.3). The host owns it: it carries the navigation hint when the menu is
	// active and is empty otherwise, so the scene gates the row with `when:
	// slash.hint`. The menu's help is a single line of info, and it replaces the
	// status row rather than stacking on top of it.
	SlashHint string `json:"slash.hint"`
	// StatusActive gates the live status row (BINDS.md §4.3): "true" while the
	// menu is closed so the row renders, "false" while it is open so the single
	// bottom line carries only the navigation hint. The evalWhen contract treats
	// "false" as falsy, so a `when: status.active` child hides itself.
	StatusActive string `json:"status.active"`
	// SlashSelected is the index into SlashMatches of the highlighted row.
	// The host owns it (↑/↓ while the menu is open), clamps it to the match
	// list on every filter keystroke, and resets it to 0 when the menu
	// reopens. A list bound to slash.matches renders this row bright and every
	// other row dim (docs/BINDS.md §4.3).
	SlashSelected int    `json:"slash.selected"`
	UIFocus       string `json:"ui.focus"`
	UIMax         string `json:"ui.max"`
	UISurface     string `json:"ui.surface"`
	EscapeArmed   bool   `json:"host.escape.armed"`
	SceneError    string `json:"host.scene.error"`

	// BudgetMicrounits is run.started.budget_usd × 1000, captured when the run
	// starts. Combined with CostMicrounits it produces session.tokens_used.
	BudgetMicrounits uint64
	// CostMicrounits is the running sum of llm.response.cost_usd × 1000.
	CostMicrounits uint64

	// Internal state for tracking team members across events
	members map[string]*TeamMember
	// memberOrder is the order members were first seen in the log.
	//
	// It exists because deriveTeamMembers ranged over the map, and Go
	// randomises map iteration ORDER BY DESIGN. team.members is a rendered
	// list, so the subagent panel reordered itself between two folds of the
	// SAME bytes -- measured: four agents, identical input, order changed by
	// the 4th of 200 repetitions.
	//
	// This was invisible to every existing test for one reason worth
	// recording: they either fold a single member (nothing to permute) or
	// look the member up by id (`for _, m := range s.TeamMembers { if m.ID
	// == want }`), which is exactly the access pattern that cannot observe
	// order. `go test -count=5` over the whole repo stayed green.
	//
	// It matters beyond a jittery panel. fold.go's own contract is "Two runs
	// of the same log produce the same state, or the replay is worthless",
	// and ADR-0002 re-decided log-follow BECAUSE Replay, the goldens and the
	// Phase 2 corpus all go through this path. A golden that compares a
	// rendered frame containing two or more agents would have failed
	// intermittently -- the worst failure mode, and one that would have been
	// blamed on the harness.
	//
	// Insertion order and not sorted order: the log is the sequence that
	// happened, and the panel should read in the order the agents appeared
	// rather than alphabetically. Sorting would also be deterministic, but it
	// would discard information the log carries for free.
	memberOrder []string

	// started is the set of work_ids that emitted exec.work_started.
	//
	// It exists because counting `started - finished` underflows. Control
	// work (Emit, SetTimer, CancelTimer, Snapshot) never crosses the durable
	// start boundary: exec.go's runDurableControl calls finishWork directly,
	// so those works are prepared and finished with no start in between. In
	// the measured log 23 works are prepared, 23 finish, and only 16 ever
	// start -- a naive counter ends the run at -7 active.
	//
	// Decrementing only for work that was actually seen starting is what
	// keeps ExecActive a count of things in flight rather than an arithmetic
	// artefact.
	started map[string]bool
	// finishedWork guards against double-counting a terminal status. The
	// core's Recover() tolerates a repeated exec.work_finished as long as the
	// status agrees, so a replay that sees one twice must not count it twice.
	finishedWork map[string]bool
}

// Fold is the pure reducer: events in, view-state out. It is deterministic.
// Two runs of the same log produce the same state, or the replay is worthless.
//
// Source events come from internal/kernel/event.go's EventType constants and
// the payload field names are confirmed against the emission sites in
// internal/provider/executor.go and internal/app/acceptance.go. The fold never
// imports the core; it reads these events from the log file the core writes.
func Fold(events []Event) State {
	s := State{
		AgentMode:    "idle",
		ModelName:    "",
		UISurface:    "chat", // default surface per BINDS.md §4.3
		StatusActive: "true", // status row visible unless the slash menu is open
		ExecPhase:    "idle", // nothing in flight before the first work starts
		// -1 is "no stage entered yet", the core's own sentinel
		// (applyRunStarted). Zero would read as "in the first stage" before
		// run.started has even landed.
		StageIndex:   -1,
		members:      make(map[string]*TeamMember),
		started:      make(map[string]bool),
		finishedWork: make(map[string]bool),
	}
	for _, e := range events {
		s.apply(e)
	}
	// Derive computed binds after all events are applied
	s.deriveUsageDelta()
	s.deriveSessionTokensUsed()
	s.deriveTodosCount()
	s.deriveTeamMembers()
	s.deriveExecPhase()
	s.deriveToolSurface()
	s.deriveStageSurface()
	return s
}

// actorName is the member this event is about.
//
// Top-level `actor` first, payload.agent second. Both spellings exist and the
// order matters: internal/exec/fake.go writes both, so a simulated log cannot
// tell the two apart, while internal/provider/executor.go's FinishTurn writes
// the tool payload as {tool, call_id, args, argument_digest} and puts the name
// only in Actor. Reading the payload first would therefore pass every test
// built on the recorded --sim log and attribute nothing on the real provider
// path.
func (e Event) actorName() string {
	if e.Actor != "" {
		return e.Actor
	}
	return str(e.Payload, "agent")
}

// str reads a string key, returning "" when absent or of another type. The
// fold never errors on a missing key -- the log belongs to the core -- but it
// must not panic on one either.
func str(p map[string]any, key string) string {
	v, _ := p[key].(string)
	return v
}

// closeToolCall attaches a terminal outcome to the most recent unfinished call
// by the same actor, or records a standalone terminal event when there is no
// open call to attach it to.
//
// Two things force this shape, and both were measured rather than assumed:
//
//  1. call_id cannot be a key. All four tool.call events in the recorded run
//     carry "sim-provider-call-1" (fake.go:245 hardcodes it), so keying on it
//     would merge four calls by two agents into one entry.
//
//  2. A terminal record may have no opening. executor.go's CallTool -- the
//     effect-runner path -- emits tool.call_completed or tool.call_denied and
//     never a preceding tool.call. So "find the open call and close it" must
//     have an else branch, or the production path loses the only record that
//     the tool ran at all. A subtract-on-finish counter would underflow here
//     for the same reason ExecActive went to -7.
//
// Scanning backwards is what makes the pairing correct under duplicate ids:
// the spec guarantees "calls from one response and their results preserve
// provider order", so the newest unfinished call by that actor is the one this
// result belongs to.
func (s *State) closeToolCall(e Event, outcome, result, policy string) {
	actor := e.actorName()
	for i := len(s.ToolCalls) - 1; i >= 0; i-- {
		c := &s.ToolCalls[i]
		if c.Outcome != "" || c.Actor != actor {
			continue
		}
		c.Outcome = outcome
		c.Result = result
		c.Policy = policy
		// The terminal record is authoritative for the tool name: the effect
		// path's payload is the one place the two could disagree, and an empty
		// name would blank a row that had one.
		if t := str(e.Payload, "tool"); t != "" {
			c.Tool = t
		}
		s.markToolIdle(actor)
		return
	}

	// No open call: a terminal record from the effect-runner path. Recorded as
	// a complete entry rather than dropped -- it is the only evidence the tool
	// ran, and discarding it would make the production path look idle.
	s.ToolCalls = append(s.ToolCalls, ToolActivity{
		Actor:   actor,
		Tool:    str(e.Payload, "tool"),
		CallID:  str(e.Payload, "call_id"),
		Outcome: outcome,
		Result:  result,
		Policy:  policy,
		Seq:     e.Seq,
	})
	s.markToolIdle(actor)
}

// markToolIdle takes a member out of the "tool" state once its call ended.
//
// It moves to "thinking" and not "idle", matching arxi's own reducer
// (decide.go ToolCallCompleted: `if m.State == MemberTool { m.State =
// MemberThinking }`). The turn is not over -- the result gets reinjected and
// the model is called again -- so reporting the member idle would say the
// agent stopped when it is mid-turn. The State == "tool" guard is the core's
// too: a completion for a member that moved on must not drag it backwards.
func (s *State) markToolIdle(actor string) {
	if actor == "" {
		return
	}
	if m, ok := s.members[actor]; ok && m.State == "tool" {
		m.State = "thinking"
	}
}

// deriveToolSurface reduces the call list to the summary binds a status row
// reads. Derived rather than maintained in apply() for the same reason
// ExecPhase is: a count written at event time and a list written at event time
// are two places to get one fact wrong.
func (s *State) deriveToolSurface() {
	s.ToolsPending = 0
	for _, c := range s.ToolCalls {
		if c.Outcome == "" {
			s.ToolsPending++
		}
	}
	if len(s.ToolCalls) > 0 {
		s.ToolLast = s.ToolCalls[len(s.ToolCalls)-1]
	} else {
		s.ToolLast = ToolActivity{}
	}
}

// deriveStageSurface reduces the submission list to the count a header badge
// reads. Derived for the same reason ExecPhase and ToolsPending are: a count
// written at event time and a list written at event time are two places to
// get one fact wrong, and the one that drifts is always the cheap one.
func (s *State) deriveStageSurface() {
	s.StageSubmittedCount = uint(len(s.StageSubmissions))
}

// deriveExecPhase reduces the work counters to the one word a status row can
// show. It is derived rather than set in apply() so that it cannot disagree
// with ExecActive: a phase written at event time and a count written at event
// time are two places to get the same fact wrong.
func (s *State) deriveExecPhase() {
	if s.ExecActive > 0 {
		s.ExecPhase = "working"
		return
	}
	s.ExecPhase = "idle"
}

// handled is the set of event types apply() has a case for.
//
// It exists because the coverage question could not be asked before: the only
// events this fold had ever seen were the eight the Phase 0 mock emits, and
// every one of them is handled by construction. Measured against a real
// 122-event run log written by the arxi core, ten types were handled and
// twelve were not -- and the unhandled twelve were the bulk of the file.
//
// The previous commit closed the largest part of that gap: the exec.* family
// (91 events, 74.6% of the log) and run.result. Coverage went 13/122 ->
// 105/122. The commit after added the tool.* family (the only family in the
// log that says WHAT THE AGENT DID) for 113/122, and the stage.* family
// (blueprint position) for 120/122.
//
// This one closes the remainder: timer.scheduled and timer.cancelled, the two
// events the log still left invisible, for 122/122. They are handled as a
// read-and-understood no-op -- see the case and the handled-map note below for
// why a stage deadline arming and cancelling without firing has no host-facing
// projection. The figure is pinned in
// TestTheRealLogCoverageIsMeasuredNotAssumed so that a change in what the fold
// handles, or in what the core emits, is a deliberate re-measurement.
//
// exec.* counts durable work and stage.* names a position in the blueprint;
// both are plumbing. A host that shows a moving progress indicator and never
// the words "read README.md" has replaced a blank screen with a busy one --
// which is why tool.* was chosen before either even though it is not the
// biggest count.
//
// tool.call_denied and stage.timeout are in the handled set and contribute
// ZERO to the figure: the recorded run allows every tool and never exceeds a
// stage deadline, so the log contains neither. That is stated rather than
// hidden, because a coverage number that counted handled TYPES instead of
// handled EVENTS would claim credit for them.
//
// The list must be kept beside the switch. A type added to one and not the
// other makes Handles lie, which is why there is a test that walks the real
// log and compares the two.
var handled = map[string]bool{
	"run.prompt":      true,
	"llm.response":    true,
	"agent.activated": true,
	"agent.turn_done": true,
	"agent.failed":    true,
	"run.started":     true,
	"agent.blocked":   true,
	"agent.unblocked": true,
	"run.quiescent":   true,
	"ui.state":        true,

	// Durable execution progress, per kernel/event.go's "durable execution
	// progress" block. Operational facts: they drive a progress indicator and
	// never a verdict.
	"exec.work_prepared":  true,
	"exec.work_started":   true,
	"exec.work_finished":  true,
	"exec.step_completed": true,

	// The run's verdict. Success only -- see State.RunOutcome.
	"run.result": true,

	// Tool activity: what the agent actually did. tool.call_denied is handled
	// although the measured log contains none -- the simulated run allows
	// every tool -- because the denial is the half with a human in the loop,
	// and a host that only understands the happy path goes blank at exactly
	// the moment someone is waiting to be asked something.
	"tool.call":           true,
	"tool.call_completed": true,
	"tool.call_denied":    true,

	// Blueprint position. stage.timeout is handled although the measured log
	// contains none -- the recorded run never exceeds a stage deadline -- for
	// the same reason tool.call_denied is: it is the half where something went
	// wrong, and the core's default for it is `escalate`, meaning a human is
	// about to be asked. A host blind to it goes quiet at the moment someone
	// is waiting.
	"stage.entered":   true,
	"stage.submitted": true,
	"stage.advanced":  true,
	"stage.timeout":   true,

	// Stage-deadline scheduling. Handled although neither moves any host state,
	// for the same reason stage.timeout is: "handled" means read and
	// understood, and the understanding here is that these two are the core's
	// internal bookkeeping, not a fact the user needs. A deadline the user must
	// know about surfaces as agent.blocked with blocked_on=timer (BINDS.md
	// §4.2), which already has a handler and a todo; the scheduling of the
	// deadline itself, and its cancellation when the stage resolved in time,
	// are the timer doing its job invisibly. Unlike stage.timeout, these two DO
	// appear in the measured log, so recognising them moves coverage -- that is
	// the difference between an event read and understood as a no-op and an
	// unhandled type that looks like an oversight.
	"timer.scheduled": true,
	"timer.cancelled": true,
}

// Handles reports whether the fold does anything with this event type.
//
// An unhandled event is not an error -- the log is the core's, not the host's,
// and a host that refused unknown types could not read a log written by a
// newer core -- but it is invisible, and invisible is worth measuring.
func Handles(eventType string) bool { return handled[eventType] }

func (s *State) apply(e Event) {
	switch e.Type {
	case "run.prompt":
		text := ""
		if t, ok := e.Payload["text"]; ok {
			text, _ = t.(string)
		}
		s.History = append(s.History, ChatLine{Role: "user", Text: text})

	case "llm.response":
		// A response event accumulates text (streaming deltas arrive as
		// consecutive llm.response events with partial text, per the executor).
		text := ""
		if t, ok := e.Payload["text"]; ok {
			text, _ = t.(string)
		}
		if len(s.History) > 0 && s.History[len(s.History)-1].Role == "assistant" {
			// Append to last assistant message (streaming delta).
			s.History[len(s.History)-1].Text += text
		} else {
			s.History = append(s.History, ChatLine{Role: "assistant", Text: text})
		}

		// Accumulate usage counters from this response.
		if in, ok := e.Payload["tokens_in"].(float64); ok {
			s.UsageIn += uint64(in)
		}
		if out, ok := e.Payload["tokens_out"].(float64); ok {
			s.UsageOut += uint64(out)
		}
		// Accumulate cost in microunits (USD × 1000) for session.tokens_used
		if cost, ok := e.Payload["cost_usd"].(float64); ok {
			s.CostMicrounits += uint64(cost * 1000)
		}
		// model name: "provider/model" from the final response.
		if model, ok := e.Payload["model"].(string); ok && model != "" {
			s.ModelName = model
		}

		// Update the current agent's spend if tracked
		if agent, ok := e.Payload["agent"].(string); ok {
			if m, exists := s.members[agent]; exists {
				if cost, ok := e.Payload["cost_usd"].(float64); ok {
					m.SpentUSD += cost
				}
			}
		}

	case "agent.activated":
		// A member began a turn. That is all this event knows.
		//
		// It used to also set AgentMode = "live", with a comment reading "the
		// run is live and not simulated-idle". That conflated two different
		// questions: who is working, which is AgentWorking on the line below,
		// and whose money is being spent, which only run.started answers via
		// `simulated`. A simulated run activates agents exactly like a real
		// one, so the overwrite relabelled every sim run as live the instant
		// it did any work -- and for a host that displays cost, claiming a
		// simulation is live is the one error that cannot be tolerated.
		//
		// The mock could never catch it: its run.started carries
		// simulated:false, so the overwrite wrote the value already there.
		// A real 121-event core log caught it on the first run.
		s.AgentWorking = true

		// Track the activated agent in team.members
		agent := ""
		if a, ok := e.Payload["agent"].(string); ok {
			agent = a
		}
		if agent != "" {
			if _, exists := s.members[agent]; !exists {
				s.members[agent] = &TeamMember{ID: agent, State: "thinking", Busy: true}
				// Recorded here and nowhere else: this is the only site that
				// creates a member, so the order list cannot fall out of step
				// with the map it indexes.
				s.memberOrder = append(s.memberOrder, agent)
			} else {
				s.members[agent].State = "thinking"
				s.members[agent].Busy = true
			}
			if role, ok := e.Payload["role"].(string); ok {
				s.members[agent].Role = role
			}
		}

	case "agent.turn_done":
		// The member's turn finished cleanly: no longer busy.
		s.AgentWorking = false

		// Read via actorName() rather than payload.agent alone: `actor` is
		// the top-level field both event shapes agree on, and the payload key
		// is stamped by some emitters and not others. The old code read only
		// the payload, so on any emitter that omits it the turn was never
		// counted and the member never left "thinking".
		if agent := e.actorName(); agent != "" {
			if m, exists := s.members[agent]; exists {
				// A member that already submitted for this stage does NOT go
				// back to idle, and this guard is load-bearing rather than
				// tidy. It is the core's own condition (applyTurnDone:
				// `if !m.Submitted { m.State = MemberIdle }`).
				//
				// The ordering makes it unavoidable, not occasional: a real
				// agent submits by calling a tool DURING its turn, so
				// stage.submitted always precedes the agent.turn_done that
				// closes that turn. Measured in the recorded log, every
				// single time: seq 35→36, 42→43, 96→97, 103→104 -- submit,
				// then turn_done at the very next sequence. Overwriting here
				// would erase "submitted" a single event after it was set,
				// so the state BINDS.md §4.1 signs would exist for exactly
				// one event in the whole run and never be observable at the
				// end of a fold.
				//
				// "waiting" and "failed" are excluded for the core's reasons
				// too, and they are checked first there because getting them
				// wrong destroys a block rather than mislabelling it: a
				// member blocked on a human (tool denied with policy=ask)
				// receives its turn_done afterwards -- always -- and
				// clearing the state would leave the host showing an idle
				// agent that is in fact waiting on an unanswered question.
				switch m.State {
				case "waiting", "failed", "submitted":
					// State preserved. The turn still ended, so the turn
					// count and the busy flag below still apply.
				default:
					m.State = "idle"
				}
				m.Busy = false
				m.Turns++
			}
		}

	case "agent.failed":
		// The turn failed: no longer busy.
		s.AgentWorking = false

		if agent, ok := e.Payload["agent"].(string); ok {
			if m, exists := s.members[agent]; exists {
				m.State = "failed"
				m.Busy = false
			}
		}

	case "run.started":
		// Determines live vs sim from run.started.simulated. Before this
		// event lands the mode stays the pre-run default ("idle").
		if sim, ok := e.Payload["simulated"].(bool); ok && sim {
			s.AgentMode = "sim"
		} else {
			s.AgentMode = "live"
		}
		// Capture budget in microunits for session.tokens_used computation
		if budget, ok := e.Payload["budget_usd"].(float64); ok {
			s.BudgetMicrounits = uint64(budget * 1000)
		}

	case "agent.blocked":
		// A member is blocked on something: add a todo, resolved later by
		// agent.unblocked. The blocked_on field is one of: approval, lock,
		// peer, budget, timer, tool, workspace (BINDs.md §4.2).
		task := "blocked"
		if t, ok := e.Payload["task"].(string); ok && t != "" {
			task = t
		}
		blockedOn := ""
		if b, ok := e.Payload["blocked_on"].(string); ok {
			blockedOn = b
		}
		// The blocked member, read the way the core resolves it.
		//
		// This used to read ONLY payload.actor, which is not where the name
		// lives. arxi's reducer is `m := out.Member(e.Actor)` (applyBlocked),
		// i.e. the TOP-LEVEL actor field -- the same conclusion the tool.*
		// family reached last turn, in a second place.
		//
		// Nothing could have caught it from the recorded log: it contains
		// ZERO agent.blocked events, so this surface -- the todos list, the
		// agent.blocked.* binds, the waiting member state -- had never once
		// been folded from real data. With payload.actor absent, every todo
		// was attributed to "" and every blocked member kept whatever state
		// it already had, so the host would have shown an approval request
		// belonging to nobody while the agent waiting on it looked busy.
		//
		// payload.actor is kept as the last fallback rather than dropped: it
		// costs nothing, and an emitter that writes it is not wrong, merely
		// unusual.
		actor := e.actorName()
		if actor == "" {
			actor = str(e.Payload, "actor")
		}
		s.Todos = append(s.Todos, TodoItem{Task: task, BlockedOn: blockedOn, Actor: actor})

		// Update agent.blocked.* surface binds (BINDS.md §4.2) — these are
		// the most recent blocked event's fields, not a list.
		s.BlockedOn = blockedOn
		s.BlockedActor = actor
		if ref, ok := e.Payload["blocked_ref"].(map[string]any); ok {
			s.BlockedRef = ref
		}

		// Update team member state
		if m, exists := s.members[actor]; exists {
			m.State = "waiting"
			m.Busy = false
		}

	case "agent.unblocked":
		// A member's blocking condition cleared: remove the first todo
		// matching that actor and blocked_on.
		//
		// Resolved identically to agent.blocked, and that symmetry is the
		// whole point: the two are matched against each other by actor, so
		// reading the name differently on the two sides would leave the todo
		// in the list forever. A block that can be created but never cleared
		// is worse than one that is never shown.
		actor := e.actorName()
		if actor == "" {
			actor = str(e.Payload, "actor")
		}
		blockedOn := ""
		if b, ok := e.Payload["blocked_on"].(string); ok {
			blockedOn = b
		}
		for i := 0; i < len(s.Todos); i++ {
			if s.Todos[i].Actor == actor && s.Todos[i].BlockedOn == blockedOn {
				s.Todos = append(s.Todos[:i], s.Todos[i+1:]...)
				break
			}
		}

		// Clear blocked surface if this was the most recent block
		if s.BlockedActor == actor && s.BlockedOn == blockedOn {
			s.BlockedRef = nil
			s.BlockedOn = ""
			s.BlockedActor = ""
		}

		// Update team member state
		if m, exists := s.members[actor]; exists {
			m.State = "idle"
		}

	case "run.quiescent":
		// Quiescence diagnosis: why the run is stuck (BINDS.md §4.1)
		if diag, ok := e.Payload["diagnosis"].(string); ok {
			s.QuiescentDiag = diag
		}

	case "exec.work_prepared":
		// A unit of work was bound and is about to be attempted. The fold
		// deliberately does NOT count this as active: preparation is not
		// dispatch, and control work is prepared and finished without ever
		// starting. Counting it here is what would make the indicator claim
		// work in flight that the executor never dispatched.
		//
		// The event is handled (not ignored) because its absence is what
		// makes a later start or finish invalid -- the core's Recover()
		// refuses work that "starts before it is prepared". The host does not
		// refuse, it is not the kernel, but it does acknowledge the record.

	case "exec.work_started":
		// The durable start boundary was crossed: real, possibly paid,
		// possibly mutating work is now in flight.
		id, ok := e.Payload["work_id"].(string)
		if !ok || id == "" {
			// A start with no work_id cannot be paired with its finish, so
			// counting it would leak an active forever.
			break
		}
		if s.started[id] {
			// Replay saw the same start twice; it is still one unit of work.
			break
		}
		s.started[id] = true
		s.ExecActive++

	case "exec.work_finished":
		// Terminal outcome for one unit of work. The status vocabulary is
		// fixed by the core: progress.go refuses anything that is not
		// "completed", "failed", or "unknown".
		//
		// "unknown" is not a synonym for "failed" and must not be folded into
		// it. exec.go: ErrUnknownWork means "external work has an unknown
		// outcome" -- the dispatch crossed its start boundary and no terminal
		// truth was committed, so retrying could duplicate paid work. The run
		// stops rather than guess. A host that displayed that as a failure
		// would be asserting something the core explicitly refuses to assert.
		id, _ := e.Payload["work_id"].(string)
		if id != "" && s.finishedWork[id] {
			break // already counted; Recover() tolerates a repeated record
		}
		if id != "" {
			s.finishedWork[id] = true
		}
		// Only decrement for work that was seen starting. Control work
		// (Emit/SetTimer/CancelTimer/Snapshot) finishes without a start, and
		// subtracting for it drove the counter to -7 on the measured log.
		if id != "" && s.started[id] {
			delete(s.started, id)
			if s.ExecActive > 0 {
				s.ExecActive--
			}
		}
		switch status, _ := e.Payload["status"].(string); status {
		case "completed":
			s.ExecCompleted++
		case "failed":
			s.ExecFailed++
		case "unknown":
			s.ExecUnknown++
		}

	case "exec.step_completed":
		// One domain event has been fully executed. source_seq is the durable
		// cursor: the core's Recover() uses it to know where a resumed run
		// may safely continue, and it advances one domain event at a time.
		//
		// JSON numbers decode as float64. Reading it as int64 directly would
		// leave the cursor at zero -- the same silent-zero failure the
		// `seq`/`sequence` decoder bug produced, so it is spelled out rather
		// than assumed.
		if v, ok := e.Payload["source_seq"].(float64); ok {
			if c := int64(v); c > s.ExecCursor {
				s.ExecCursor = c
			}
		}

	case "run.result":
		// The run's verdict, and it only ever means success.
		//
		// kernel/decide.go `case RunResult` sets Status = StatusSucceeded
		// with no branch on the payload. Both emission sites are advance
		// paths. A failing run never emits this event at all -- it goes to
		// run.cancelled, run.expired, or to a status change carried by no
		// event (quiescent with no observer; inbox timeout with
		// on_timeout:fail).
		//
		// Therefore: presence => succeeded, absence => no verdict yet. NOT
		// failure. The bind is a string with an empty zero value so that the
		// scene can distinguish "still running" from "done", which a bool
		// cannot express.
		//
		// This event is also NOT the end of the log. In the measured run it
		// lands at seq 112 and ten more events follow (one work_finished and
		// nine step_completed, seq 113-122) as the executor drains. A fold
		// that stopped here would truncate the tail.
		s.RunOutcome = "succeeded"
		if v, ok := e.Payload["summary"].(string); ok {
			s.RunSummary = v
		}
		if v, ok := e.Payload["result_from"].(string); ok {
			s.RunResultFrom = v
		}

	case "tool.call":
		// The agent asked for a tool. Recorded as pending: the call has been
		// issued and no terminal record has arrived, which is a third state
		// distinct from completed and denied.
		//
		// The actor comes from e.Actor (top-level), with payload.agent as a
		// fallback only. That order is deliberate and is the opposite of what
		// the rest of this fold does: the simulated path writes both, so
		// either would have passed the measurement, while the real provider
		// path (executor.go FinishTurn) writes ONLY the top-level field. A
		// payload-first reading works in every test and attributes nothing in
		// production.
		a := ToolActivity{
			Actor:  e.actorName(),
			Tool:   str(e.Payload, "tool"),
			CallID: str(e.Payload, "call_id"),
			Seq:    e.Seq,
		}
		s.ToolCalls = append(s.ToolCalls, a)
		s.ToolCallsTotal++

		// A member running a tool is in the "tool" state, which is in the
		// documented vocabulary (event.go: idle/thinking/tool/...) and was
		// the one value nothing ever set. arxi's reducer does the same thing
		// on this event (decide.go: m.State = MemberTool, m.Detail = tool).
		if m, ok := s.members[a.Actor]; ok && a.Actor != "" {
			m.State = "tool"
			m.Busy = true
		}

	case "tool.call_completed":
		// The tool ran and returned. `result` is carried verbatim and not
		// inspected: the core is explicit that a non-zero exit is an answer,
		// not an error ("the tests fail" is what the agent asked to find
		// out), so a host that scanned it for failure strings would invent a
		// verdict the core never gave.
		s.closeToolCall(e, "completed", str(e.Payload, "result"), "")

	case "tool.call_denied":
		// Policy refused the call. This is NOT an error branch.
		//
		// spec/events.md: `tool.call_denied` with policy:"ask" "is **not an
		// error**: it is a question." The core turns it into an inbox item and
		// a blocked_ref, and the run continues waiting for a human. "deny" is
		// the other meaning: a decision, final, nothing ran.
		//
		// Both are counted in ToolsDenied and only "ask" in
		// ToolsAwaitingApproval, because the remedies differ -- a policy
		// change versus an answer.
		//
		// No todo is appended here. The core emits its own agent.blocked for
		// the ask path (applyToolDenied returns an AskHuman effect and sets
		// the member to MemberWaiting), and this fold already turns
		// agent.blocked into a todo. Adding one here too would double-count
		// every approval request in the todo list.
		policy := str(e.Payload, "policy")
		s.closeToolCall(e, "denied", "", policy)
		s.ToolsDenied++
		if policy == "ask" {
			s.ToolsAwaitingApproval++
		}

	case "stage.entered":
		// The run moved into a stage. Both the name and the index are read
		// from the payload rather than one being derived from the other: the
		// core writes both (decide.go emits {"stage": name, "index": idx} at
		// all three emission sites), and computing the index by counting
		// entries would be wrong on a resumed run, whose log starts mid-plan.
		s.StageName = str(e.Payload, "stage")
		// JSON numbers arrive as float64. A missing index must not silently
		// read as 0 -- that is "the first stage", a real position -- so the
		// index only moves when the key is actually present.
		if idx, ok := e.Payload["index"].(float64); ok {
			s.StageIndex = int(idx)
		}
		// Entering a stage clears the submissions, and this is the line that
		// scopes them. The core does exactly this (applyStageEntered sets
		// m.Submitted = false for every member) and states why: without it a
		// submit leaks into the next stage, where it satisfies an advance
		// rule nobody met. Set to an empty non-nil slice so "entered a stage,
		// nobody submitted yet" and "no stage yet" stay distinguishable.
		s.StageSubmissions = []string{}

	case "stage.submitted":
		// A member declared its work for this stage done.
		//
		// The actor is read via actorName(), top-level `actor` first. The two
		// emitters disagree about the payload and the measured log cannot
		// show it: internal/exec/fake.go writes {agent, stage, simulated},
		// but host/v1/text_executor.go writes {agent, result} -- no `stage`
		// key at all. Both set Actor. The core's own reader does the same
		// thing in the same order (cmd/arxi/runresult.go's lastSubmission:
		// `who := e.Actor; if who == "" { who = e.Str("agent") }`), so this
		// is the core's rule rather than a preference.
		actor := e.actorName()
		if actor != "" {
			// Recorded once per member per stage. A member may legally emit
			// two submits for one stage -- the core tolerates it explicitly
			// ("A stage resolves ONCE, however many members go on submitting
			// to it") -- and counting both would report a quorum of 2 from
			// one agent.
			seen := false
			for _, a := range s.StageSubmissions {
				if a == actor {
					seen = true
					break
				}
			}
			if !seen {
				s.StageSubmissions = append(s.StageSubmissions, actor)
			}
		}
		// The member is submitted, not idle. This is the state BINDS.md §4.1
		// has always listed in the team.members enum and the fold could
		// never produce, because nothing handled this event.
		if m, ok := s.members[actor]; ok {
			m.State = "submitted"
			// Busy stays as it is. "Submitted" is a mid-turn state: the
			// agent submits by calling a tool DURING its turn, so its
			// agent.turn_done is still to come and the turn is still open.
			// The core makes the same distinction -- MemberSubmitted is
			// "neither Busy nor Runnable, yet its turn is still running".
		}

	case "stage.advanced":
		// The plan moved on. The core's reducer does exactly two things with
		// this event (decide.go:94 `out.Stage = e.Str("to")`,
		// `out.StageIndex = int(e.Num("to_index"))`) and so does this: it is
		// the transition record, and the stage.entered that always follows
		// it is what sets up the new stage.
		//
		// Both are applied even though stage.entered will immediately
		// restate them, because the pair is emitted in that order on purpose
		// (orderEffects: "the order of the Emits among themselves is
		// semantic (stage.advanced before stage.entered)") and a log may be
		// truncated between the two. Reading only `from` here would leave a
		// tail-truncated log showing the stage it had already left.
		s.StagePrev = str(e.Payload, "from")
		if to := str(e.Payload, "to"); to != "" {
			s.StageName = to
		}
		if idx, ok := e.Payload["to_index"].(float64); ok {
			s.StageIndex = int(idx)
		}
		s.StageAdvances++

	case "stage.timeout":
		// The stage deadline fired. Deliberately NOT a failure and not a
		// terminal state: the core's default action is `escalate`, whose
		// comment reads "A timeout almost never means 'impossible', it means
		// 'something got stuck, take a look'". The stage stays open and its
		// members keep working.
		//
		// So nothing here clears StageName or the submissions, and no
		// outcome is recorded. What the timeout actually causes -- an
		// AskHuman, a stage advance, or a failed run -- arrives as its own
		// subsequent event (agent.blocked, stage.advanced, or a status
		// transition), and those already have handlers. Folding a verdict
		// here would be this host guessing at a decision the core has not
		// made yet, which is the run.result mistake in a new place.
		//
		// The case exists rather than being left to the default because
		// "handled" must mean "read and understood": an event whose correct
		// projection is 'no state change' is a decision, and the alternative
		// is an unhandled type that looks like an oversight.

	case "timer.scheduled", "timer.cancelled":
		// A stage deadline was armed (timer.scheduled, payload after_ms /
		// deadline_ms / timer_id) or disarmed because the stage resolved in
		// time (timer.cancelled, payload timer_id). Neither moves host state,
		// and the case exists rather than falling to the default for the same
		// reason stage.timeout does: to record that the no-op is a decision.
		//
		// The user-facing half of "a timer exists" is not the scheduling, it
		// is an agent waiting on one -- agent.blocked with blocked_on=timer
		// (BINDS.md §4.2) -- and that already folds into a todo. Projecting a
		// countdown here would invent host state the scene vocabulary has no
		// bind for and the core does not ask the host to show; the deadlines
		// are long (the measured log arms 1_800_000 ms) and the product's
		// signal is escalate-on-timeout, not a ticking clock. So a deadline
		// that arms and cancels without firing is exactly the timer doing its
		// job invisibly, and the honest projection of it is nothing.

	case "ui.state":
		// Generic UI state updates for view-state binds (BINDS.md §4.3).
		// The payload is a map of bind name → value; each field present is
		// applied to the corresponding State field.
		if val, ok := e.Payload["user.input"].(string); ok {
			s.UserInput = val
		}
		if val, ok := e.Payload["slash.active"].(bool); ok {
			s.SlashActive = val
		}
		if val, ok := e.Payload["slash.typed"].(string); ok {
			s.SlashTyped = val
		}
		// JSON numbers unmarshal as float64; the bind is an int index and a
		// fractional selection is not a thing the host ever writes.
		if val, ok := e.Payload["slash.selected"].(float64); ok {
			s.SlashSelected = int(val)
		}
		if val, ok := e.Payload["ui.surface"].(string); ok {
			s.UISurface = val
		}
		if val, ok := e.Payload["host.escape.armed"].(bool); ok {
			s.EscapeArmed = val
		}
		if val, ok := e.Payload["host.scene.error"].(string); ok {
			s.SceneError = val
		}
	}
}

// deriveUsageDelta computes the per-turn token delta as a short string for
// usage.delta bind: the input+output tokens of the most recent llm.response.
// Empty string when no completed response has landed yet.
// DeriveUsageDelta recomputes usage.delta after manual State construction.
// In normal use, Fold calls this internally; this method exists for tests and
// golden generation that build State directly.
func (s *State) DeriveUsageDelta() {
	s.deriveUsageDelta()
}

func (s *State) deriveUsageDelta() {
	if len(s.History) == 0 {
		s.UsageDelta = ""
		return
	}
	// usage.in/out are cumulative; the last response's contribution is the
	// difference from the previous cumulative total. Since the fold does not
	// retain per-response deltas, we show the cumulative totals as a compact
	// summary only when both counters are nonzero.
	if s.UsageIn == 0 && s.UsageOut == 0 {
		s.UsageDelta = ""
		return
	}
	s.UsageDelta = formatDelta(s.UsageIn, s.UsageOut)
}

func formatDelta(in, out uint64) string {
	if in == 0 && out == 0 {
		return ""
	}
	var parts []string
	if in > 0 {
		parts = append(parts, "+i"+shortNum(in))
	}
	if out > 0 {
		parts = append(parts, "+o"+shortNum(out))
	}
	return strings.Join(parts, " ")
}

func shortNum(n uint64) string {
	if n >= 1000 {
		return strconv.FormatFloat(float64(n)/1000, 'f', 1, 64)
	}
	return strconv.FormatUint(n, 10)
}

// deriveSessionTokensUsed computes session.tokens_used from the budget and
// cumulative cost. Per BINDS.md §4.1, session.tokens_used = budget_usd minus
// the running sum of llm.response.cost_usd, reported in microunits (USD × 1000)
// for integer bind compatibility.
func (s *State) deriveSessionTokensUsed() {
	if s.BudgetMicrounits >= s.CostMicrounits {
		s.SessionTokensUsed = s.BudgetMicrounits - s.CostMicrounits
	} else {
		// Budget exceeded: report zero remaining
		s.SessionTokensUsed = 0
	}
}

// deriveTodosCount computes todos.count: the count of pending todos.
func (s *State) deriveTodosCount() {
	s.TodosCount = uint(len(s.Todos))
}

// deriveTeamMembers exports the internal members map as the team.members
// array, in the order the members first appeared in the log.
//
// It walks memberOrder and not the map. Ranging a Go map yields a randomised
// order by design, so the previous version returned the same members in a
// different sequence for the same input -- see State.memberOrder for the
// measurement and for why no test could see it.
func (s *State) deriveTeamMembers() {
	s.TeamMembers = make([]TeamMember, 0, len(s.members))
	for _, id := range s.memberOrder {
		if m, ok := s.members[id]; ok {
			s.TeamMembers = append(s.TeamMembers, *m)
		}
	}
	// A member in the map but not in the order list would silently vanish
	// from the panel, which is a worse failure than the one just fixed: the
	// old code at least showed everybody. The two are written together at a
	// single site so this cannot happen, and the invariant is asserted by
	// TestEveryMemberInTheMapIsInTheRenderedOrder rather than trusted.
}

// ChatHistoryMarkdown renders the chat history as a markdown stream.
// This feeds chat.history bind: append-only view of the transcript.
func (s State) ChatHistoryMarkdown() string {
	var b strings.Builder
	for i, h := range s.History {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(h.Text)
	}
	return strings.TrimSpace(b.String())
}

// ThinkingTextFromHistory derives the streaming text for the thinking marquee
// from the last assistant message in chat.history. The thinking.text bind is
// the in-flight portion of the current llm.response that has not yet landed
// as a completed message.
func (s State) ThinkingTextFromHistory() string {
	if len(s.History) == 0 {
		return ""
	}
	last := s.History[len(s.History)-1]
	if last.Role != "assistant" {
		return ""
	}
	return last.Text
}

// SlashMatch is one row in slash.matches: a command the host knows, filtered
// by the typed substring after "/". Scenes render it through a `list` node
// with `filter_by: "typed"`.
type SlashMatch struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
}

// Commands is the host's command registry, the source for slash.matches.
// Each entry is {name, category, description} per docs/BINDS.md §4.3.
var Commands = []SlashMatch{
	{"help", "General", "Show available commands"},
	{"max", "General", "Maximize a pane by id"},
	{"focus", "General", "Focus a node by id"},
	{"surface", "General", "Switch active surface"},
	// The description names the verbs that exist, not the verbs the plan
	// sketches. It read "add, move, style, plugin" while the surface
	// implements set and style: the menu is the only place a user learns what
	// they may type, so advertising `add` there sends them to a refusal the
	// interface itself invited. That is the accepted-but-not-drawn class one
	// layer out — a capability the chrome claims and the engine does not have
	// — and it is worse here than in a document, because the menu is read at
	// the moment of use.
	//
	// It is held to patch.Verbs() by a test rather than by a comment; the
	// import would be a cycle, and a fold that imported the patch surface
	// would stop being the pure host-owned fold ADR-0002 requires.
	{"ui", "General", "Mutate the scene (add, set, style)"},
}

// FilterSlashMatches returns the commands matching the typed substring after
// "/". An empty typed string returns all commands (the menu is open but
// unfiltered).
func FilterSlashMatches(typed string) []SlashMatch {
	if typed == "" {
		return Commands
	}
	var out []SlashMatch
	for _, c := range Commands {
		if strings.Contains(strings.ToLower(c.Name), strings.ToLower(typed)) {
			out = append(out, c)
		}
	}
	return out
}
