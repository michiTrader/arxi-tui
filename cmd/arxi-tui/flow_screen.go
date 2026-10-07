package main

import (
	"context"
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

// flowScreen is the open screen's state: the highlight and the answer being
// given to an approval, because everything else is read from the fold each frame.
type flowScreen struct {
	sel int

	// item is the inbox item the run is blocked on, refreshed every frame by
	// publish; "" when nothing is waiting for an answer.
	item string
	// rejecting is true while the person types the reason for a rejection.
	rejecting bool
	reason    string
	// working is what is shown while the core records the answer.
	working string
	// banner is the last result: "✓ ..." or "✗ ...".
	banner string
}

// flowHint is the key legend.
const flowHint = "↑↓ move · esc close"

// flowAskHint is the legend while an approval waits for the person.
const flowAskHint = "a approve · r reject · ↑↓ move · esc close"

// flowReasonHint is the legend while the reason for a rejection is typed.
const flowReasonHint = "type the reason · enter send · esc back"

// flowTask is an answer to give the core: approve or reject one inbox item.
type flowTask struct {
	kind   string // "approve" or "reject"
	item   string
	reason string
}

// working is the line shown while the task runs.
func (t flowTask) workingLine() string {
	if t.kind == "reject" {
		return "Rejecting …"
	}
	return "Approving …"
}

// flowOutcome is what an answer's worker reports back to the loop.
type flowOutcome struct {
	answered string // the success sentence once the answer is recorded
	err      string // a plain sentence when it was not
}

// inboxResumer answers an inbox item and keeps the run going. Only the live
// connection to the core can: the answer and the continuation are one act, so a
// person never has to know they are two.
type inboxResumer interface {
	// AnswerAndResume records the answer, calls recorded once it is durable, and
	// returns when the continuation ends (nil) or fails (a plain sentence).
	AnswerAndResume(ctx context.Context, kind, item, reason string, recorded func()) error
}

// startFlowAnswer gives the answer on a worker so a slow core never freezes the
// loop. The first outcome is the recorded answer; a later one, if any, is the
// continuation failing.
func startFlowAnswer(ctx context.Context, a inboxResumer, t flowTask, done chan<- flowOutcome) {
	go func() {
		recorded := false
		err := a.AnswerAndResume(ctx, t.kind, t.item, t.reason, func() {
			recorded = true
			word := "approved"
			if t.kind == "reject" {
				word = "rejected"
			}
			done <- flowOutcome{answered: "✓ " + word + " — the run goes on"}
		})
		if err != nil {
			msg := err.Error()
			if recorded {
				msg = "the answer is saved, but the run could not go on: " + msg
			}
			done <- flowOutcome{err: msg}
		}
	}()
}

// apply shows a worker's result.
func (f *flowScreen) apply(o flowOutcome) {
	f.working = ""
	switch {
	case o.err != "":
		f.banner = "✗ " + o.err
	case o.answered != "":
		f.banner = o.answered
		f.rejecting, f.reason = false, ""
	}
}

// paste inserts clipboard text into the rejection reason being typed.
func (f *flowScreen) paste(text string) {
	if f.rejecting && f.working == "" {
		// Line breaks and tabs become spaces; a space at either end stays, so
		// pasting " now" after "a" does not glue the words together.
		f.reason += strings.Map(func(r rune) rune {
			if r == '\n' || r == '\r' || r == '\t' {
				return ' '
			}
			return r
		}, text)
	}
}

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
	return (&flowScreen{}).detail(st)
}

// detail is flowDetail plus what this screen is doing about the blocker.
func (f *flowScreen) detail(st *fold.State) string {
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
				switch {
				case f.working != "":
					line += "\n\n" + f.working
				case f.rejecting:
					line += "\n\nWhy reject it? " + f.reason + "▌"
				default:
					line += "\n\nPress a to approve it, or r to reject it."
				}
			}
		}
		parts = append(parts, line)
	} else if st.RunOutcome == "succeeded" {
		parts = append(parts, "✓ the run finished")
	} else {
		parts = append(parts, "Nothing is holding the run up.")
	}
	if f.banner != "" {
		parts = append(parts, f.banner)
	}
	return strings.Join(parts, "\n\n")
}

// approvalItem is the inbox item an approval attention waits on, or "".
func approvalItem(st *fold.State) string {
	if a := st.Attention; a.Kind == "approval" {
		id, _ := a.Ref["inbox_id"].(string)
		return id
	}
	return ""
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
	f.item = approvalItem(st)
	if f.item == "" {
		f.rejecting, f.reason = false, ""
	}
	hint := flowHint
	switch {
	case f.rejecting:
		hint = flowReasonHint
	case f.item != "":
		hint = flowAskHint
	}
	st.HubTitle, st.HubRows, st.HubHint, st.HubDetail = flowTitle(st), rows, hint, f.detail(st)
}

// key applies one key and reports whether the screen should close, and the answer
// to give the core when the key chose one. Esc, Enter and q close it; a approves
// and r rejects while an approval waits; the arrows and the page keys move the
// highlight; everything else is ignored, so nothing typed here can leak into the
// chat.
func (f *flowScreen) key(k term.Key) (closeIt bool, task *flowTask) {
	if f.working != "" {
		return false, nil // the core is busy with the last answer
	}
	if f.rejecting {
		return f.reasonKey(k)
	}
	switch k.Type {
	case term.KeyEscape, term.KeyEnter:
		return true, nil
	case term.KeyRunes:
		if k.Mod&term.ModCtrl != 0 || len(k.Runes) != 1 {
			return false, nil
		}
		switch k.Runes[0] {
		case 'q', 'Q':
			return true, nil
		case 'a', 'A':
			if f.item != "" {
				t := flowTask{kind: "approve", item: f.item}
				f.banner, f.working = "", t.workingLine()
				return false, &t
			}
		case 'r', 'R':
			if f.item != "" {
				f.banner, f.rejecting, f.reason = "", true, ""
			}
		}
	case term.KeyUp:
		f.sel--
	case term.KeyDown:
		f.sel++
	case term.KeyPgUp:
		f.sel -= hubPageSize
	case term.KeyPgDn:
		f.sel += hubPageSize
	}
	return false, nil
}

// reasonKey is the keyboard while the reason for a rejection is typed.
func (f *flowScreen) reasonKey(k term.Key) (bool, *flowTask) {
	switch k.Type {
	case term.KeyEscape:
		f.rejecting, f.reason = false, ""
	case term.KeyBackspace:
		f.reason = dropLastRune(f.reason)
	case term.KeyEnter:
		reason := strings.TrimSpace(f.reason)
		if reason == "" {
			f.banner = "✗ Say why you reject it: the agent reads the reason"
			return false, nil
		}
		t := flowTask{kind: "reject", item: f.item, reason: reason}
		f.banner, f.working, f.rejecting = "", t.workingLine(), false
		return false, &t
	case term.KeyRunes:
		if k.Mod&term.ModCtrl != 0 {
			if len(k.Runes) == 1 && k.Runes[0] == 'u' {
				f.reason = ""
			}
			return false, nil
		}
		if k.Mod&term.ModAlt == 0 {
			f.reason += string(k.Runes)
		}
	}
	return false, nil
}

// flowCommand reports whether Enter on this line (or on the highlighted menu row)
// is `/flow` with nothing after it.
func flowCommand(input string, sel int, cat string) bool {
	return menuCommand("flow", input, sel, cat)
}
