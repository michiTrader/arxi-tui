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
	return s
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
// This commit closes the largest part of that gap: the exec.* family (91
// events, 74.6% of the log) and run.result (the run's own verdict). Coverage
// goes 13/122 -> 105/122 (10.7% -> 86.1%).
//
// It does NOT close all of it, and the arithmetic is worth stating plainly
// because the previous turn's note got it wrong: 89% (109/122) was the total
// unhandled figure, not the exec.* share. Seventeen events remain invisible
// after this change -- tool.call x4, tool.call_completed x4, stage.entered x2,
// stage.submitted x4, stage.advanced x1, timer.scheduled x1,
// timer.cancelled x1 -- and they are the next piece of work, not a rounding
// error.
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
