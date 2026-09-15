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
	// Payload holds the fields the fold needs for projection.
	Payload map[string]any `json:"payload,omitempty"`
}

// ChatLine is one entry in chat.history: the speaker and the text.
type ChatLine struct {
	Role string // "user" or "assistant"
	Text string
}
