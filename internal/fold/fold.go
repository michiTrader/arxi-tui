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
		members:      make(map[string]*TeamMember),
	}
	for _, e := range events {
		s.apply(e)
	}
	// Derive computed binds after all events are applied
	s.deriveUsageDelta()
	s.deriveSessionTokensUsed()
	s.deriveTodosCount()
	s.deriveTeamMembers()
	return s
}

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
		// A member began a turn: the run is live and not simulated-idle.
		s.AgentWorking = true
		s.AgentMode = "live"

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
