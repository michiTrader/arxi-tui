package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/driver"
)

// This file is the half of /team that CHANGES one member of an agent or team that
// is already stored: the model it thinks with, its role, the tools it may touch and
// whether it only advises. The form opens showing what the member is today, and only
// what the person changed is sent, so a member's other settings are never rewritten
// by accident.
//
// Like the other forms it only gathers and shapes. A change the core refuses (a tool
// it does not know, a model spelled wrongly) comes back as the core's own sentence
// and the file is left as it was.

const (
	memberLabel   = "Member"
	modelLabel    = "Model"
	roleLabel     = "Role"
	advisoryLabel = "Advisory"
	toolPrefix    = "Tool · "
)

// memberNow says what a member is today, for the form's help.
func memberNow(m driver.BlueprintMember) string {
	model := m.Model
	if model == "" {
		model = "none of its own (the run supplies one)"
	}
	role := m.Role
	if role == "" {
		role = "none"
	}
	tools := "none"
	if len(m.Tools) > 0 {
		tools = strings.Join(m.Tools, ", ")
	}
	line := fmt.Sprintf("  %s — model: %s; role: %s; tools: %s", m.Name, model, role, tools)
	if m.Advisory {
		line += "; advisory"
	}
	return line
}

// newMemberForm builds the form that changes one member of a stored blueprint.
// models are the enabled "provider/id" refs the chat can already use.
func newMemberForm(name string, members []driver.BlueprintMember, models []string) *teamForm {
	names := make([]string, len(members))
	lines := []string{"Right now:"}
	known := map[string]bool{}
	for _, m := range models {
		known[m] = true
	}
	choices := append([]string{noModel}, models...)
	for i, m := range members {
		names[i] = m.Name
		lines = append(lines, memberNow(m))
		// A model the member already has but that is not enabled must still be
		// offered, or opening the form would silently look like a change.
		if m.Model != "" && !known[m.Model] {
			known[m.Model] = true
			choices = append(choices, m.Model)
		}
	}
	f := &teamForm{
		kind:    formEditMember,
		title:   "Edit member · " + name,
		target:  name,
		members: members,
		help: "Change what one member of " + name + " thinks with, its role or the tools it may use. " +
			"The fields start as the member is today; change only what you want. " +
			"It applies to runs you start from now on; a run already going keeps what it began with.\n\n" +
			strings.Join(lines, "\n"),
	}
	f.fields = []teamField{
		{label: memberLabel, kind: fieldChoice, choices: names,
			help: "Which member to change. Use ←/→ to choose; the fields below show that member."},
		{label: modelLabel, kind: fieldChoice, choices: choices,
			help: "What the member thinks with. “" + noModel + "” removes its own model, and then every run must supply one. Only models you enabled in /provider are offered."},
		{label: roleLabel, kind: fieldText,
			help: "A free label such as implementer or reviewer. Empty it to remove the role."},
	}
	for _, t := range agentTools {
		f.fields = append(f.fields, teamField{label: toolPrefix + t, kind: fieldToggle, help: toolHelp[t]})
	}
	f.fields = append(f.fields, teamField{label: advisoryLabel, kind: fieldToggle,
		help: "An advisory member gives its opinion but does not count toward moving a stage forward."})
	f.loadMember()
	return f
}

// chosenMember is the member the Member field points at.
func (f *teamForm) chosenMember() driver.BlueprintMember {
	for i := range f.fields {
		if f.fields[i].label == memberLabel {
			if idx := f.fields[i].idx; idx >= 0 && idx < len(f.members) {
				return f.members[idx]
			}
		}
	}
	return driver.BlueprintMember{}
}

// loadMember fills the fields with what the chosen member is today.
func (f *teamForm) loadMember() {
	m := f.chosenMember()
	has := map[string]bool{}
	for _, t := range m.Tools {
		has[t] = true
	}
	for i := range f.fields {
		fl := &f.fields[i]
		switch {
		case fl.label == modelLabel:
			fl.idx = 0
			for j, c := range fl.choices {
				if m.Model != "" && c == m.Model {
					fl.idx = j
				}
			}
		case fl.label == roleLabel:
			fl.value = m.Role
		case strings.HasPrefix(fl.label, toolPrefix):
			fl.on = has[strings.TrimPrefix(fl.label, toolPrefix)]
		case fl.label == advisoryLabel:
			fl.on = m.Advisory
		}
	}
}

// sameTools says whether two tool lists grant the same tools.
func sameTools(a, b []string) bool {
	x, y := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	return strings.Join(x, ",") == strings.Join(y, ",")
}

// submitMember turns the form into the change to ask the core for: only what
// differs from the member as it is now.
func (f *teamForm) submitMember() (*teamTask, string) {
	cur := f.chosenMember()
	p := driver.BlueprintMemberParams{Name: f.target, Member: cur.Name}
	var tools []string
	for _, fl := range f.fields {
		switch {
		case fl.label == modelLabel:
			model := fl.choices[fl.idx]
			if fl.idx == 0 {
				model = ""
			}
			if model != cur.Model {
				p.Model = &model
			}
		case strings.HasPrefix(fl.label, toolPrefix) && fl.on:
			tools = append(tools, strings.TrimPrefix(fl.label, toolPrefix))
		case fl.label == advisoryLabel && fl.on != cur.Advisory:
			on := fl.on
			p.Advisory = &on
		}
	}
	if role := f.value(roleLabel); role != cur.Role {
		p.Role = &role
	}
	if !sameTools(tools, cur.Tools) {
		if tools == nil {
			tools = []string{}
		}
		p.Tools = &tools
	}
	if p.Model == nil && p.Role == nil && p.Tools == nil && p.Advisory == nil {
		return nil, "nothing to change: edit the model, the role, the tools or the advisory switch"
	}
	return &teamTask{member: &p}, ""
}
