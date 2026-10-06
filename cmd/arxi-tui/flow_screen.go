package main

import (
	"fmt"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// This file is /flow: one screen that answers "what is my team doing, and why is
// nothing happening?". Everything on it is read from the folded event log, so a
// resumed session draws the same screen a live one does. It owns the keyboard
// while it is open (Esc closes it) and has no input line: nothing is typed here.

// factoryFlow is the flow screen's document. It reuses the provider hub's binds
// (title, detail, rows, hint) because they already mean exactly this layout: an
// explanation where the chat sits and a list of rows hanging below it.
const factoryFlow = `{ "root": { "type": "stack", "children": [
  { "type": "text", "style": {"style": "header"},
    "text": "Δr×i v0.1.0 · flow" },

  { "id": "title", "type": "text", "bind": "hub.title" },

  { "id": "detail", "type": "markdown", "bind": "hub.detail", "grow": 1 },

  { "id": "choices", "type": "overlay", "anchor": "bottom",
    "children": [
      { "type": "rule" },
      { "id": "options", "type": "list", "bind": "hub.rows",
        "row_template": { "type": "row", "children": [
          { "type": "text", "bind": "row.line", "weight": 9 },
          { "type": "text", "bind": "row.status", "weight": 7 }
        ]}},
      { "id": "hint", "type": "text", "bind": "hub.hint", "style": {"style": "dim"} }
    ]}
]}}`

// loadFlowScene parses and validates the embedded screen.
func loadFlowScene() (*scene.Document, error) {
	doc, err := scene.ParseDocument([]byte(factoryFlow))
	if err != nil {
		return nil, fmt.Errorf("flow scene: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("flow scene: %w", err)
	}
	return doc, nil
}

// flowScreen is the open screen's state: only the highlight, because everything
// else is read from the fold each frame.
type flowScreen struct{ sel int }

// flowHint is the key legend.
const flowHint = "↑↓ move · esc close"

// memberGlyph is the one-cell mark for a member's state. The word follows it, so
// the glyph never has to carry the meaning alone.
func memberGlyph(state string) string {
	switch state {
	case "thinking", "tool":
		return "●"
	case "submitted":
		return "✓"
	case "waiting":
		return "…"
	case "failed":
		return "✗"
	}
	return "○"
}

// memberStatus is a row's right-hand column: glyph and state, who it waits for,
// turns and spend.
func memberStatus(m fold.TeamMember, waitingOn map[string]string) string {
	state := m.State
	if state == "" {
		state = "idle"
	}
	parts := []string{memberGlyph(state) + " " + state}
	if p := waitingOn[m.ID]; p != "" {
		parts[0] += " for " + p
	}
	if m.Turns > 0 {
		word := "turns"
		if m.Turns == 1 {
			word = "turn"
		}
		parts = append(parts, fmt.Sprintf("%d %s", m.Turns, word))
	}
	if m.SpentUSD > 0 {
		parts = append(parts, fmt.Sprintf("$%.2f", m.SpentUSD))
	}
	return strings.Join(parts, " · ")
}

// stageLine draws the stages the log proves the run went through, oldest first,
// the one it is in marked. Stages that are still ahead are not shown: no event
// names them yet.
func stageLine(runs []fold.StageProgress) string {
	var b strings.Builder
	for i, s := range runs {
		if i > 0 {
			b.WriteString(" → ")
		}
		switch {
		case s.Left:
			b.WriteString(s.Name + " ✓")
		default:
			b.WriteString(s.Name + " ●")
			if len(s.Submitted) > 0 {
				b.WriteString(" (submitted: " + strings.Join(s.Submitted, ", ") + ")")
			}
		}
	}
	return b.String()
}

// flowIsChat reports whether the run has no team structure to draw: one agent
// (or none) and no stages.
func flowIsChat(st *fold.State) bool {
	return len(st.StageRun) == 0 && len(st.TeamMembers) <= 1 && st.Attention.Kind == ""
}

// flowTitle is the line above the detail.
func flowTitle(st *fold.State) string {
	if flowIsChat(st) {
		return "Flow"
	}
	title := "Flow"
	if st.StageName != "" {
		title += " · stage " + st.StageName
	}
	busy := 0
	var spent float64
	for _, m := range st.TeamMembers {
		if m.Busy {
			busy++
		}
		spent += m.SpentUSD
	}
	if n := len(st.TeamMembers); n > 0 {
		title += fmt.Sprintf(" · %d of %d working", busy, n)
	}
	if spent > 0 {
		title += fmt.Sprintf(" · $%.2f", spent)
	}
	return title
}

// flowDetail is the markdown shown where the chat sits.
func flowDetail(st *fold.State) string {
	if flowIsChat(st) {
		return "This conversation is a plain chat with one agent, so there is no team flow to show.\n\n" +
			"When a team run is going (members working in stages), this screen draws who is doing what, " +
			"which stage the run is in and what is holding it up."
	}
	var parts []string
	if len(st.StageRun) > 0 {
		parts = append(parts, stageLine(st.StageRun))
	}
	if a := st.Attention; a.Kind != "" {
		line := "⚠ " + a.Text
		if a.Kind == "approval" {
			if id, _ := a.Ref["inbox_id"].(string); id != "" {
				line += "\n   answer it with: arxi inbox approve " + id
			}
		}
		parts = append(parts, line)
	} else if st.RunOutcome == "succeeded" {
		parts = append(parts, "✓ the run finished")
	} else {
		parts = append(parts, "Nothing is holding the run up.")
	}
	return strings.Join(parts, "\n\n")
}

// publish writes the screen's binds onto the state the renderer reads.
func (f *flowScreen) publish(st *fold.State) {
	n := len(st.TeamMembers)
	if f.sel >= n {
		f.sel = n - 1
	}
	if f.sel < 0 {
		f.sel = 0
	}
	var rows []fold.HubRow
	lo, hi := window(f.sel, n, hubPageSize)
	for i := lo; i < hi; i++ {
		m := st.TeamMembers[i]
		label := m.ID
		if m.Role != "" {
			label += " · " + m.Role
		}
		rows = append(rows, fold.HubRow{Label: label, Status: memberStatus(m, st.WaitingOn), Selected: i == f.sel})
	}
	st.UserInput, st.UserInputCaret = "", 0
	st.HubTitle, st.HubRows, st.HubHint, st.HubDetail = flowTitle(st), rows, flowHint, flowDetail(st)
}

// key applies one key and reports whether the screen should close. Esc, Enter and
// q close it; the arrows and the page keys move the highlight; everything else is
// ignored, so nothing typed here can leak into the chat.
func (f *flowScreen) key(k term.Key) (closeIt bool) {
	switch k.Type {
	case term.KeyEscape, term.KeyEnter:
		return true
	case term.KeyRunes:
		return k.Mod&term.ModCtrl == 0 && len(k.Runes) == 1 && (k.Runes[0] == 'q' || k.Runes[0] == 'Q')
	case term.KeyUp:
		f.sel--
	case term.KeyDown:
		f.sel++
	case term.KeyPgUp:
		f.sel -= hubPageSize
	case term.KeyPgDn:
		f.sel += hubPageSize
	}
	return false
}

// flowCommand reports whether Enter on this line (or on the highlighted menu row)
// is `/flow` with nothing after it.
func flowCommand(input string, sel int, cat string) bool {
	return menuCommand("flow", input, sel, cat)
}
