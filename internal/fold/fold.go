package fold

import "strings"

// State is the projected view-state from a log of events. It is the only thing
// a scene reads: the scene says form, the fold says content. The fold never
// waits on the scene, never imports UI packages, and never writes back — it is
// a pure function of events already received.
type State struct {
	// Run-state binds (mapped from arxi core's event catalog)
	History      []ChatLine `json:"chat.history"`
	ThinkingText string     `json:"thinking.text"`
	AgentWorking bool       `json:"agent.working"`

	// View-state binds (arxi-tui's own contract)
	UserInput       string `json:"user.input"`
	EscapeArmed     bool   `json:"host.escape.armed"`
	SceneError      string `json:"host.scene.error"`
}

// Fold is the pure reducer: events in, view-state out. It is deterministic.
// Two runs of the same log produce the same state, or the replay is worthless.
func Fold(events []Event) State {
	s := State{}
	for _, e := range events {
		s.apply(e)
	}
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
		// response may be streaming: accumulate text field
		text := ""
		if t, ok := e.Payload["text"]; ok {
			text, _ = t.(string)
		}
		if len(s.History) > 0 && s.History[len(s.History)-1].Role == "assistant" {
			// Append to last assistant message (streaming delta)
			s.History[len(s.History)-1].Text += text
		} else {
			s.History = append(s.History, ChatLine{Role: "assistant", Text: text})
		}

	case "thinking.text":
		if t, ok := e.Payload["text"]; ok {
			s.ThinkingText, _ = t.(string)
		}

	case "agent.working":
		working, _ := e.Payload["working"].(bool)
		s.AgentWorking = working

	case "thinking.start":
		s.AgentWorking = true
	case "thinking.stop":
		s.AgentWorking = false
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
		if h.Role == "user" {
			b.WriteString(h.Text)
		} else {
			b.WriteString(h.Text)
		}
	}
	return strings.TrimSpace(b.String())
}