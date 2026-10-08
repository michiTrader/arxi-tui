package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/driver"
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
	// plan is the stages the team's blueprint declares, in order, so the stages
	// still ahead can be drawn. Empty when the blueprint could not be read (a
	// plain chat, a team file that moved): the screen then draws only what the
	// log proves, as it always did.
	plan []driver.BlueprintStage
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
	// plan is the blueprint's stages when this outcome carries them (and only then).
	plan []driver.BlueprintStage
}

// startFlowPlan reads the stages of ./agents/<team>.yaml on a worker, so /flow can
// show the ones still ahead. Any failure just leaves the screen without a plan:
// what the log proves is still drawn.
func startFlowPlan(ctx context.Context, core blueprintReader, root, team string, done chan<- flowOutcome) {
	if core == nil || team == "" || strings.ContainsAny(team, `/\`) {
		return
	}
	go func() {
		info, err := core.SubmitBlueprintValidate(ctx, filepath.Join(root, teamDir, team+".yaml"))
		if err != nil || info == nil || len(info.Stages) == 0 {
			return
		}
		select {
		case done <- flowOutcome{plan: info.Stages}:
		case <-ctx.Done():
		}
	}()
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
	if o.plan != nil {
		f.plan = o.plan
		return
	}
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
// the one it is in marked, then the stages the blueprint says are still ahead
// (marked ○). With no plan only what happened is drawn.
func stageLine(runs []fold.StageProgress, plan []driver.BlueprintStage) string {
	var parts []string
	for _, s := range runs {
		switch {
		case s.Left:
			parts = append(parts, s.Name+" ✓")
		default:
			p := s.Name + " ●"
			if len(s.Submitted) > 0 {
				p += " (submitted: " + strings.Join(s.Submitted, ", ") + ")"
			}
			parts = append(parts, p)
		}
	}
	for _, name := range aheadStages(runs, plan) {
		parts = append(parts, name+" ○")
	}
	return strings.Join(parts, " → ")
}

// aheadStages is the names of the stages the plan still has after the stage the
// run last entered, in the plan's order. A run that has entered nothing yet has the
// whole plan ahead of it; a last stage the plan does not know has nothing invented
// after it.
func aheadStages(runs []fold.StageProgress, plan []driver.BlueprintStage) []string {
	from := 0
	if n := len(runs); n > 0 {
		from = -1
		for i, st := range plan {
			if st.Name == runs[n-1].Name {
				from = i + 1
			}
		}
	}
	if from < 0 {
		return nil
	}
	var names []string
	for _, st := range plan[from:] {
		names = append(names, st.Name)
	}
	return names
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
	// A plan only exists for a team run, so a run that has not shown a member yet
	// is still a team's, not a chat.
	if flowIsChat(st) && len(f.plan) == 0 {
		return "This conversation is a plain chat with one agent, so there is no team flow to show.\n\n" +
			"When a team run is going (members working in stages), this screen draws who is doing what, " +
			"which stage the run is in and what is holding it up."
	}
	var parts []string
	if len(st.StageRun) > 0 || len(f.plan) > 0 {
		parts = append(parts, stageLine(st.StageRun, f.plan))
	}
	tree, drawn := f.flowTree(st)
	if tree != "" {
		parts = append(parts, tree)
	}
	// The tree draws the highlighted member under its stage. When it could not (the
	// log shows no open stage, or no stage at all) the member's story stands alone,
	// as it always did.
	if !drawn {
		if m := f.memberDetail(st); m != "" {
			parts = append(parts, m)
		}
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

// recentTools is how many of a member's latest tool calls the detail lists.
const recentTools = 5

// memberStory is what the log says about one member: a head line, then either a
// caption over the member's latest tool calls or, when it has used none, a single
// sentence saying so. It is the one source both the flat detail and the tree draw.
type memberStory struct {
	head    string   // "backend · implementer — … waiting for approval"
	caption string   // "tools used:", "last 5 of 8 tool calls:" or "has not used any tool yet"
	tools   []string // one toolLine per call, oldest first; empty with the "none yet" caption
}

// memberStoryOf reads the story of the member at index i of the fold's members.
func memberStoryOf(st *fold.State, i int) memberStory {
	m := st.TeamMembers[i]
	head := m.ID
	if m.Role != "" {
		head += " · " + m.Role
	}
	story := memberStory{head: head + " — " + memberStatus(m, st.WaitingOn)}
	var mine []fold.ToolActivity
	for _, c := range st.ToolCalls {
		if c.Actor == m.ID {
			mine = append(mine, c)
		}
	}
	if len(mine) == 0 {
		story.caption = "has not used any tool yet"
		return story
	}
	if len(mine) > recentTools {
		story.caption = fmt.Sprintf("last %d of %s:", recentTools, plural(len(mine), "tool call", "tool calls"))
		mine = mine[len(mine)-recentTools:]
	} else {
		story.caption = "tools used:"
	}
	for _, c := range mine {
		story.tools = append(story.tools, toolLine(c))
	}
	return story
}

// memberDetail says what the highlighted member has been doing: its state, its
// turns and spend, and the last tools it used with what came of each. It is read
// from the log like everything here, so a resumed session shows the same thing.
// Empty when the run has no members to highlight.
func (f *flowScreen) memberDetail(st *fold.State) string {
	if f.sel < 0 || f.sel >= len(st.TeamMembers) {
		return ""
	}
	story := memberStoryOf(st, f.sel)
	lines := []string{"▸ " + story.head, "  " + story.caption}
	for _, t := range story.tools {
		lines = append(lines, "  "+t)
	}
	return strings.Join(lines, "\n")
}

// flowTree draws the open stage as a nested tree: the stage at the root, its members
// as branches, and under the highlighted member the tools it used. Only the
// highlighted member is opened, so a big team still fits the screen and moving the
// highlight is what walks the tree.
//
//	build ●
//	├─ backend · implementer — … waiting for approval
//	│    last 5 of 8 tool calls:
//	│    ✓ read
//	└─ frontend · implementer — ✓ submitted
//
// The log does not say which stage a member belongs to, so every member hangs under
// the stage the run is in now; the stages already left are told by the line above.
// The second result is false when there was nothing to draw (no open stage, no
// members), and the caller then falls back to the flat member story.
func (f *flowScreen) flowTree(st *fold.State) (string, bool) {
	if len(st.TeamMembers) == 0 {
		return "", false
	}
	open := ""
	if n := len(st.StageRun); n > 0 && !st.StageRun[n-1].Left {
		open = st.StageRun[n-1].Name
	}
	if open == "" {
		return "", false
	}
	lines := []string{open + " ●"}
	last := len(st.TeamMembers) - 1
	for i := range st.TeamMembers {
		branch, trunk := "├─ ", "│    "
		if i == last {
			branch, trunk = "└─ ", "     "
		}
		story := memberStoryOf(st, i)
		if i != f.sel {
			lines = append(lines, branch+story.head)
			continue
		}
		lines = append(lines, branch+"▸ "+story.head, trunk+story.caption)
		for _, t := range story.tools {
			lines = append(lines, trunk+t)
		}
	}
	return strings.Join(lines, "\n"), true
}

// toolLine is one tool call in words: what ran and how it ended. A denial that is
// a question is said as a question, because that is what it is.
func toolLine(c fold.ToolActivity) string {
	name := c.Tool
	if name == "" {
		name = "a tool"
	}
	switch c.Outcome {
	case "completed":
		return "✓ " + name
	case "denied":
		if c.Policy == "ask" {
			return "… " + name + " — waits for your approval"
		}
		return "✗ " + name + " — not allowed"
	}
	return "● " + name + " — running"
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

// planReader is the part of the driver that can describe a stored team, or nil.
func planReader(drv Driver) blueprintReader {
	if hc, _ := drv.(interface{ Hub() hubCore }); hc != nil && hc.Hub() != nil {
		br, _ := hc.Hub().(blueprintReader)
		return br
	}
	return nil
}

// planTeam is the name of the team the followed run belongs to ("" when none).
func planTeam(drv Driver) string {
	if l, ok := drv.(actorLabeler); ok {
		return l.ActorLabel()
	}
	return ""
}
