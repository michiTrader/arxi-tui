package fold

// Event is one entry in the arxi log. The fold reads a log (from a driver,
// which may be a file, a subprocess stdout, or a mock for tests) and projects
// events into view-state binds.
type Event struct {
	// Type is the event kind: "run.prompt", "llm.response", "agent.working", etc.
	Type string `json:"type"`
	// Seq is the monotonic sequence number from the log. The fold never
	// reorders; a CAS check (ADR-0006 of arxi) lives in the driver layer.
	Seq int64 `json:"seq"`
	// Actor is the member the event is about, and it is a TOP-LEVEL field of
	// the log record -- not a payload key.
	//
	// It was missing from this struct, and the omission was invisible for the
	// same reason every other bridge defect here was: json.Unmarshal drops a
	// key with no field and reports nothing. It matters because the two event
	// shapes disagree about where the agent's name lives:
	//
	//   - kernel.Event (on disk) and host/v1.Event (on the wire) BOTH spell it
	//     `actor` at the top level.
	//   - `payload.agent` is stamped by SOME emitters and not others.
	//
	// internal/exec/fake.go writes both (so a --sim log carries the name
	// twice), but internal/provider/executor.go's FinishTurn -- the REAL
	// provider path -- builds the tool payload as
	// {tool, call_id, args, argument_digest} with no `agent` key at all, and
	// puts the name only in Actor. A fold that reads payload.agent therefore
	// attributes tool calls correctly in simulation and not at all in
	// production, which is the worst possible split: the measurement passes
	// and the real run is anonymous.
	//
	// arxi's own reducer keys off this field (kernel/decide.go's ToolCall case
	// is `out.Member(e.Actor)`), so reading `actor` is not a preference, it is
	// what the core considers the member's identity.
	Actor string `json:"actor,omitempty"`
	// Payload holds the fields the fold needs for projection.
	Payload map[string]any `json:"payload,omitempty"`
}

// ChatLine is one entry in chat.history: the speaker and the text.
type ChatLine struct {
	Role string // "user" or "assistant"
	Text string
}

// ToolActivity is one tool invocation as the log records it: who called what,
// and how it ended.
//
// Outcome is a three-valued string and not a bool for the same reason
// State.RunOutcome is: "" means no terminal record has arrived yet, which is
// not the same as failure and must not be rendered as one.
//
// There is deliberately no map keyed on call_id pairing a call to its
// completion. The measured log makes that impossible: all four tool.call
// events in testdata/serve/real_run.ndjson carry call_id
// "sim-provider-call-1", because internal/exec/fake.go:245 hardcodes the
// string. Two agents (backend, frontend) each call twice, so one id
// identifies four distinct calls. A map keyed on call_id would have collapsed
// them to one entry and shown a single tool call for a run that made four --
// and it would have looked right, because the count would be plausible.
//
// The real provider path issues genuinely unique provider call ids, so this
// is a simulation artefact. It is still the shape the host must survive: the
// fold pairs positionally (the newest call by that actor with no terminal
// record yet), which is correct under both, because the spec guarantees that
// "calls from one response and their results preserve provider order".
type ToolActivity struct {
	// Actor is the member that made the call, read from the event's top-level
	// `actor` field rather than payload.agent -- the real provider path omits
	// the payload key. See Event.Actor.
	Actor string `json:"actor"`
	// Tool is the tool name, e.g. "read". The spec fixes this key as present
	// on all three tool.* events.
	Tool string `json:"tool"`
	// CallID is the provider-issued id, carried for display and correlation
	// but NOT used as a map key. See the type comment.
	CallID string `json:"call_id"`
	// Outcome is "" (no terminal record yet) | "completed" | "denied".
	Outcome string `json:"outcome"`
	// Policy is the denial's policy: "deny" (a decision) or "ask" (a
	// question awaiting a human). Empty unless Outcome is "denied".
	Policy string `json:"policy"`
	// Result is tool.call_completed.result, verbatim. The core notes that a
	// non-zero exit is "an ANSWER and not an error", so this is not
	// interpreted. Empty is legal: the spec marks `result?` optional and the
	// simulated run writes "".
	Result string `json:"result"`
	// Seq is the sequence of the tool.call, or of the terminal event when the
	// call was never recorded (the CallTool effect path emits no tool.call).
	Seq int64 `json:"seq"`
}

// TodoItem is one entry in agent.todos: a task the agent is blocked on, why,
// and which actor owns it. Rendered by a `list` node bound to agent.todos.
type TodoItem struct {
	Task      string // the human-readable task description
	BlockedOn string // why it is blocked: approval, lock, peer, budget, timer, tool, workspace
	Actor     string // which agent owns the todo
}

// TeamMember is one entry in team.members: projected from the run's active
// agents per BINDS.md §4.1. Each member has id, state, role, busy flag, turn
// count, and spend tracking.
type TeamMember struct {
	ID       string  `json:"id"`        // agent name
	State    string  `json:"state"`     // idle/thinking/tool/submitted/waiting/inactive/failed
	Role     string  `json:"role"`      // backend/frontend/... from agent.activated
	Busy     bool    `json:"busy"`      // true while agent.working
	Turns    uint    `json:"turns"`     // completed turn count
	SpentUSD float64 `json:"spent_usd"` // cumulative cost for this agent
}
