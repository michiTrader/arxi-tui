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
// 105/122.
//
// This one adds the tool.* family: 113/122 (92.6%). It is eight events, not
// the biggest remaining count -- stage.* is seven and timer.* two -- and it
// was chosen anyway because it is the only family in the log that says WHAT
// THE AGENT DID. exec.* counts durable work and stage.* names a position in
// the blueprint; both are plumbing. A host that shows a moving progress
// indicator and never the words "read README.md" has replaced a blank screen
// with a busy one.
//
// Nine events remain invisible: stage.entered x2, stage.submitted x4,
// stage.advanced x1, timer.scheduled x1, timer.cancelled x1. They are
// position and plumbing, they are tracked by exact count in
// TestTheRealLogCoverageIsMeasuredNotAssumed, and they are the next piece of
// work rather than a rounding error.
//
// tool.call_denied is in the handled set and contributes ZERO to that figure:
// the recorded run allows every tool, so the log contains none. That is
// stated rather than hidden, because a coverage number that counted handled
// types instead of handled events would claim credit for it.
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

		if agent, ok := e.Payload["agent"].(string); ok {
			if m, exists := s.members[agent]; exists {
				m.State = "idle"
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
		actor := ""
		if a, ok := e.Payload["actor"].(string); ok {
			actor = a
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
		actor := ""
		if a, ok := e.Payload["actor"].(string); ok {
			actor = a
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

// deriveTeamMembers exports the internal members map as the team.members array.
func (s *State) deriveTeamMembers() {
	s.TeamMembers = make([]TeamMember, 0, len(s.members))
	for _, m := range s.members {
		s.TeamMembers = append(s.TeamMembers, *m)
	}
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
	{"ui", "General", "Mutate the scene (set, style)"},
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
