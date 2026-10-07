package main

import (
	"fmt"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// This file is the half of /team that CREATES: the "New agent" and "New team" forms.
// Everything the arxi command line can write into ./agents is written from here
// through the core, so nobody has to leave the TUI to build a team.
//
// The forms only gather and shape what the person typed. Every real refusal (a name
// already taken, an unknown tool, a team given where an agent belongs) is the core's
// own sentence, shown as it came: this file never second-guesses the store.

// agentTools are the tools an agent can be granted, in the order the core lists
// them (kernel.KnownTools). The core refuses a grant it does not know, so a drift
// between this list and the core shows up as the core's message, not a silent miss.
var agentTools = []string{"bash", "edit", "grep", "read", "write"}

type teamFieldKind int

const (
	fieldText   teamFieldKind = iota // typed text
	fieldChoice                      // one of a short list, cycled with ←/→ or space
	fieldToggle                      // on or off, flipped with space or ←/→
)

// teamField is one row of a form.
type teamField struct {
	label    string
	help     string
	kind     teamFieldKind
	value    string   // fieldText
	spaces   bool     // fieldText: a space is part of the text
	required bool     // fieldText
	choices  []string // fieldChoice: index 0 is the "nothing chosen" answer
	idx      int      // fieldChoice
	on       bool     // fieldToggle
}

type teamFormKind int

const (
	formNewAgent teamFormKind = iota
	formNewTeam
)

// teamForm is the open form; it lives only while it is on screen.
type teamForm struct {
	kind   teamFormKind
	title  string
	help   string
	fields []teamField
	focus  int
}

// teamTask is what the worker is asked to create.
type teamTask struct {
	agent *driver.AgentCreateParams
	team  *driver.BlueprintCreateParams
}

// shown is what the row's right-hand column says.
func (f teamField) shown() string {
	switch f.kind {
	case fieldChoice:
		return "‹ " + f.choices[f.idx] + " ›"
	case fieldToggle:
		if f.on {
			return "[x] yes"
		}
		return "[ ] no"
	}
	switch {
	case f.value != "":
		return f.value
	case f.required:
		return "(required)"
	}
	return "(optional)"
}

// noModel is the choice that leaves the model to the core's default.
const noModel = "(core default)"

// newAgentForm builds the "New agent" form. models are the enabled "provider/id"
// refs the chat can already use; none is a valid answer.
func newAgentForm(models []string) *teamForm {
	f := &teamForm{
		kind:  formNewAgent,
		title: "New agent",
		help: "An agent is one worker: a name, the model it thinks with and the tools it may touch. " +
			"Teams are built out of agents, so make the pieces first and compose them afterwards.",
	}
	f.fields = []teamField{
		{label: "Name", kind: fieldText, required: true,
			help: "A short name with no spaces or slashes, like backend or reviewer. It is the file name and how the team refers to it."},
		{label: "Model", kind: fieldChoice, choices: append([]string{noModel}, models...),
			help: "What the agent thinks with. Only models you already enabled in /provider are offered; use ←/→ to choose."},
		{label: "Role", kind: fieldText,
			help: "A free label such as implementer or reviewer. If a role of that name is defined, it fills in the tools and the advisory trait you leave blank."},
	}
	for _, t := range agentTools {
		f.fields = append(f.fields, teamField{label: "Tool · " + t, kind: fieldToggle,
			help: toolHelp[t]})
	}
	f.fields = append(f.fields, teamField{label: "Advisory", kind: fieldToggle,
		help: "An advisory agent gives its opinion but does not count toward moving a stage forward."})
	return f
}

var toolHelp = map[string]string{
	"bash":  "Run shell commands in the workspace. The most powerful tool: grant it on purpose.",
	"edit":  "Change part of an existing file.",
	"grep":  "Search the files of the workspace.",
	"read":  "Read files of the workspace.",
	"write": "Create or overwrite files in the workspace.",
}

// newTeamForm builds the "New team" form from the agents that can be members.
func newTeamForm(agents []string) *teamForm {
	f := &teamForm{
		kind:  formNewTeam,
		title: "New team",
		help: "A team is a group of agents that work together, one after another through stages. " +
			"Each member is copied from the agent as it is today.",
	}
	f.fields = []teamField{{label: "Name", kind: fieldText, required: true,
		help: "A short name with no spaces or slashes, like feature-team."}}
	for _, a := range agents {
		f.fields = append(f.fields, teamField{label: "Member · " + a, kind: fieldToggle,
			help: "Space adds or removes " + a + " from the team."})
	}
	f.fields = append(f.fields, teamField{label: "Stages", kind: fieldText, spaces: true,
		help: "The steps the work goes through, in order, separated by commas: build, review. " +
			"Leave it empty for a single stage called work in which everybody takes part."})
	return f
}

// typed is the text under the cursor (what the input line shows); only a text
// field has any.
func (f *teamForm) typed() string {
	if fl := f.fields[f.focus]; fl.kind == fieldText {
		return fl.value
	}
	return ""
}

// insert adds text to the focused field if it is a text field.
func (f *teamForm) insert(text string) {
	fl := &f.fields[f.focus]
	if fl.kind != fieldText {
		return
	}
	if fl.spaces {
		fl.value += cleanFilterText(text)
	} else {
		fl.value += cleanFieldText(text)
	}
}

// flip changes a choice or a toggle by one step; on a text field it does nothing.
func (f *teamForm) flip(delta int) {
	fl := &f.fields[f.focus]
	switch fl.kind {
	case fieldToggle:
		fl.on = !fl.on
	case fieldChoice:
		n := len(fl.choices)
		fl.idx = ((fl.idx+delta)%n + n) % n
	}
}

func (f *teamForm) move(delta int) {
	f.focus += delta
	if f.focus < 0 {
		f.focus = 0
	}
	if f.focus > len(f.fields)-1 {
		f.focus = len(f.fields) - 1
	}
}

// key applies one key. It returns true when the form should close (Esc) and a task
// when Enter on the last field produced something to create. A refusal that is the
// form's own (a missing name) comes back as msg, naming the FIELD.
func (f *teamForm) key(k term.Key) (closeIt bool, task *teamTask, msg string) {
	fl := &f.fields[f.focus]
	switch k.Type {
	case term.KeyEscape:
		return true, nil, ""
	case term.KeyUp:
		f.move(-1)
	case term.KeyDown:
		f.move(1)
	case term.KeyTab:
		if k.Mod&term.ModShift != 0 {
			f.move(-1)
		} else {
			f.move(1)
		}
	case term.KeyLeft:
		f.flip(-1)
	case term.KeyRight:
		f.flip(1)
	case term.KeyBackspace:
		if fl.kind == fieldText {
			fl.value = dropLastRune(fl.value)
		}
	case term.KeyEnter:
		if f.focus < len(f.fields)-1 {
			f.focus++
			return false, nil, ""
		}
		task, msg = f.submit()
	case term.KeyRunes:
		if k.Mod&term.ModCtrl != 0 {
			if len(k.Runes) == 1 && k.Runes[0] == 'u' && fl.kind == fieldText {
				fl.value = ""
			}
			return false, nil, ""
		}
		if k.Mod&term.ModAlt != 0 {
			return false, nil, ""
		}
		if len(k.Runes) == 1 && k.Runes[0] == ' ' && fl.kind != fieldText {
			f.flip(1)
			return false, nil, ""
		}
		f.insert(string(k.Runes))
	}
	return false, task, msg
}

// value reads a text field by label.
func (f *teamForm) value(label string) string {
	for _, fl := range f.fields {
		if fl.label == label {
			return strings.TrimSpace(fl.value)
		}
	}
	return ""
}

// submit validates only what the form itself can know and builds the task.
func (f *teamForm) submit() (*teamTask, string) {
	name := f.value("Name")
	if name == "" {
		return nil, "Name is required"
	}
	switch f.kind {
	case formNewAgent:
		p := driver.AgentCreateParams{Name: name, Role: f.value("Role")}
		for _, fl := range f.fields {
			switch {
			case fl.label == "Model" && fl.idx > 0:
				p.Model = fl.choices[fl.idx]
			case strings.HasPrefix(fl.label, "Tool · ") && fl.on:
				p.Tools = append(p.Tools, strings.TrimPrefix(fl.label, "Tool · "))
			case fl.label == "Advisory":
				p.Advisory = fl.on
			}
		}
		return &teamTask{agent: &p}, ""
	case formNewTeam:
		p := driver.BlueprintCreateParams{Name: name}
		for _, fl := range f.fields {
			if strings.HasPrefix(fl.label, "Member · ") && fl.on {
				p.Members = append(p.Members, strings.TrimPrefix(fl.label, "Member · "))
			}
		}
		if len(p.Members) == 0 {
			return nil, "pick at least one member (space adds one)"
		}
		for _, s := range strings.Split(f.value("Stages"), ",") {
			if s = strings.TrimSpace(s); s != "" {
				p.Stages = append(p.Stages, s)
			}
		}
		return &teamTask{team: &p}, ""
	}
	return nil, "unknown form"
}

// working is the line shown while the core writes the file.
func (t *teamTask) working() string {
	if t.agent != nil {
		return "Creating agent " + t.agent.Name + " …"
	}
	return "Creating team " + t.team.Name + " …"
}

// done is the line shown after the core wrote the file.
func (t *teamTask) done(agent *driver.AgentCreateResult, team *driver.BlueprintCreateResult) string {
	switch {
	case agent != nil:
		s := fmt.Sprintf("agent %s created in %s", agent.Name, shortPath(agent.Path))
		if agent.RoleNote != "" {
			s += " · " + agent.RoleNote
		}
		return s
	case team != nil:
		return fmt.Sprintf("team %s created with %s", team.Name, plural(len(team.Members), "member", "members"))
	}
	return ""
}

// shortPath is the part of a path a person recognises: agents/<file>.
func shortPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if i := strings.LastIndex(p, "/"+teamDir+"/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
