package engine

import (
	"strings"

	"github.com/michiTrader/arxi_tui/internal/ui"
)

// ChatLook is how the conversation is laid out: the glyphs, the words, the limits and
// the spacing of every chat line. Before this existed they were constants in render.go
// and only the colours of the chat were data, so a user who asked their agent for
// "the result on the same row as the tool call" was told it could not be done: the
// format was in Go, out of reach of /ui and ui_edit. The product's thesis is that the
// factory interface is written with the tools the user has, so the host now fills this
// from the user's texts (cmd/arxi-tui/chat_look.go) and the engine only draws it.
//
// A nil Renderer.Look is DefaultChatLook(), so every golden and every test that does not
// care is unchanged. A field the host leaves empty (or non-positive where a number is
// needed) falls back to the factory value in the same way, through resolved().
type ChatLook struct {
	// Tool lines.
	ToolDot      string            // "● " before a tool call
	ToolElbow    string            // "└ " before a result that has its own row
	ToolInline   bool              // a one-row result goes on the call's own row
	ToolSep      string            // " - " between the call and an inline result
	ToolTitles   map[string]string // tool name -> the name a person reads
	ExpandHint   string            // " (ctrl+o to expand)"
	OutputRows   int               // rows of a command's output shown before counting the rest
	DiffRows     int               // rows of a change shown
	ApprovalRows int               // rows of a change shown while it waits for an answer
	// Asks maps a tool name to the question under a change that waits for the user;
	// the key "" is the question for every tool without its own. AskKeys is the legend
	// that follows the question.
	Asks    map[string]string
	AskKeys string

	// The user's own messages.
	UserMarker   string // "┃ "
	UserMaxLines int    // rows of a message's words shown in the transcript; 0 = all of it
	UserMore     string // what a cut message says, e.g. "… +4 lines (ctrl+o to expand)"; {n} is the hidden rows

	// The other voices.
	ErrorMarker     string // "✗ "
	WarnMarker      string // "! "
	AssistantIndent int    // columns an answer is drawn in

	// Spacing and the usage line.
	TurnGap     int    // blank rows between two turns
	UsageGap    int    // blank rows between an answer and its usage line
	UsageFormat string // "{time} {tokens}"; blank hides the line
	UsageTokens string // "(↑{in} ↓{out})"
}

// DefaultChatLook is the factory look.
func DefaultChatLook() ChatLook {
	return ChatLook{
		ToolDot:      toolDot,
		ToolElbow:    toolElbow,
		ToolInline:   true,
		ToolSep:      " - ",
		ExpandHint:   expandHint,
		OutputRows:   maxOutputRows,
		DiffRows:     maxDiffRows,
		ApprovalRows: maxApprovalRows,
		Asks: map[string]string{
			"":           "Allow this change?",
			"run":        "Allow this command?",
			"web_fetch":  "Allow reading this page?",
			"web_search": "Allow this search?",
			"ui_edit":    "Allow this change to the interface?",
		},
		AskKeys:         "y yes · n no (Esc)",
		UserMarker:      userTurnMarker,
		UserMaxLines:    3,
		UserMore:        "… +{n} lines{hint}",
		ErrorMarker:     errorTurnMarker,
		WarnMarker:      warnTurnMarker,
		AssistantIndent: assistantIndent,
		TurnGap:         1,
		UsageGap:        1,
		UsageFormat:     "{time} {tokens}",
		UsageTokens:     "(↑{in} ↓{out})",
	}
}

// look is the Renderer's chat look, never nil-dependent.
func (r *Renderer) look() ChatLook {
	if r.Look == nil {
		return DefaultChatLook()
	}
	return r.Look.resolved()
}

// resolved fills what the host left empty with the factory value. A blank that the user
// meant (a hidden glyph) arrives as spaces, which are not empty, so it survives.
func (l ChatLook) resolved() ChatLook {
	d := DefaultChatLook()
	pick := func(v, def string) string {
		if v == "" {
			return def
		}
		return v
	}
	l.ToolDot = pick(l.ToolDot, d.ToolDot)
	l.ToolElbow = pick(l.ToolElbow, d.ToolElbow)
	l.ToolSep = pick(l.ToolSep, d.ToolSep)
	l.ExpandHint = pick(l.ExpandHint, d.ExpandHint)
	l.AskKeys = pick(l.AskKeys, d.AskKeys)
	l.UserMarker = pick(l.UserMarker, d.UserMarker)
	l.UserMore = pick(l.UserMore, d.UserMore)
	l.ErrorMarker = pick(l.ErrorMarker, d.ErrorMarker)
	l.WarnMarker = pick(l.WarnMarker, d.WarnMarker)
	l.UsageFormat = pick(l.UsageFormat, d.UsageFormat)
	l.UsageTokens = pick(l.UsageTokens, d.UsageTokens)
	if l.OutputRows < 1 {
		l.OutputRows = d.OutputRows
	}
	if l.DiffRows < 1 {
		l.DiffRows = d.DiffRows
	}
	if l.ApprovalRows < 1 {
		l.ApprovalRows = d.ApprovalRows
	}
	if l.UserMaxLines < 0 {
		l.UserMaxLines = 0
	}
	if l.AssistantIndent < 0 {
		l.AssistantIndent = 0
	}
	if l.TurnGap < 0 {
		l.TurnGap = 0
	}
	if l.UsageGap < 0 {
		l.UsageGap = 0
	}
	if l.Asks == nil {
		l.Asks = d.Asks
	}
	return l
}

// Title is the name a person reads for a tool: the user's, else the factory's, else the
// tool's own name.
func (l ChatLook) Title(tool string) string {
	if t := l.ToolTitles[tool]; t != "" {
		return t
	}
	if t := toolTitles[tool]; t != "" {
		return t
	}
	return tool
}

// ask is the question under a change waiting for the user.
func (l ChatLook) ask(tool string) string {
	if q, ok := l.Asks[tool]; ok && q != "" {
		return q
	}
	if q := l.Asks[""]; q != "" {
		return q
	}
	return DefaultChatLook().Asks[""]
}

// trimBlankRows drops the empty rows at both ends of a block and folds a run of them in
// the middle into one. A model's reply often begins with a line break or ends with two,
// and drawn faithfully each one is a whole row of nothing between the turns; markdown
// itself only ever means "a paragraph break" by it.
func trimBlankRows(rows []ui.Line) []ui.Line {
	out := make([]ui.Line, 0, len(rows))
	for _, r := range rows {
		if len(r) == 0 && (len(out) == 0 || len(out[len(out)-1]) == 0) {
			continue
		}
		out = append(out, r)
	}
	for len(out) > 0 && len(out[len(out)-1]) == 0 {
		out = out[:len(out)-1]
	}
	return out
}

// trimBlankText is the same for what a person typed: no blank rows before or after the
// words. Blank rows inside a message are theirs and stay.
func trimBlankText(s string) string {
	return strings.Trim(strings.ReplaceAll(s, "\r", ""), "\n")
}
