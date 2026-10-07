package main

import (
	"fmt"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/driver"
)

// This file is the half of /team that CHANGES what wakes a member of an agent or
// team already stored: its watchers. A watcher says "when this kind of event
// happens, give that member a turn" (or tell it, or run a tool for it). It is the
// one setting that spends money without anybody asking for a turn, so the form
// says so, and it never offers the "react to its own events" switch the core
// refuses.
//
// Like the other forms it only gathers and shapes. A rule the core refuses (a
// member that is not in the team, a pattern with a wildcard in the middle) comes
// back as the core's own sentence and the file is left as it was.

const (
	watchRuleLabel   = "Rule"
	watchMemberLabel = "Member"
	watchEventsLabel = "Events"
	watchActionLabel = "What it does"
	watchToolLabel   = "Tool"
	watchRemoveLabel = "Remove this rule"

	newWatch  = "(a new rule)"
	noWatchTl = "(none)"
)

// The answers of the action field and what each one asks of the core.
const (
	actWake   = "give it a turn"
	actNotify = "tell it, without starting a turn if it is busy"
	actTool   = "run a tool for it"
)

var watchActionValue = map[string]string{actWake: "", actNotify: "notify", actTool: "run_tool"}

// watchActionWords says an action the way the form does.
func watchActionWords(a string) string {
	switch a {
	case "notify":
		return actNotify
	case "run_tool":
		return actTool
	}
	return actWake
}

// watchLine says one rule in plain words, for the list and for the choice of rule.
func watchLine(w driver.BlueprintWatcher) string {
	line := fmt.Sprintf("%s listens to %s: %s", w.Agent, w.Pattern, watchActionWords(w.Action))
	if w.Action == "run_tool" && w.Tool != "" {
		line += " (" + w.Tool + ")"
	}
	return line
}

// newWatchForm builds the form that adds, changes or removes one watcher of a
// stored blueprint.
func newWatchForm(name string, members []driver.BlueprintMember, watchers []driver.BlueprintWatcher) *teamForm {
	names := make([]string, len(members))
	for i, m := range members {
		names[i] = m.Name
	}
	rules := []string{newWatch}
	lines := []string{"Right now:"}
	for _, w := range watchers {
		rules = append(rules, watchLine(w))
		lines = append(lines, "  "+watchLine(w))
	}
	if len(watchers) == 0 {
		lines = append(lines, "  no rules: nothing wakes a member by itself")
	}
	f := &teamForm{
		kind:     formEditWatch,
		title:    "Watchers · " + name,
		target:   name,
		watchers: watchers,
		help: "A watcher gives a member a turn when a kind of event happens in a run, like a stage finishing " +
			"or the run getting stuck. Every turn it wakes is paid for, so keep the events narrow. " +
			"It applies to runs you start from now on.\n\n" + strings.Join(lines, "\n"),
	}
	f.fields = []teamField{
		{label: watchRuleLabel, kind: fieldChoice, choices: rules,
			help: "Add a new rule, or choose one that exists to change it or remove it. Use ←/→ to choose."},
		{label: watchMemberLabel, kind: fieldChoice, choices: names,
			help: "The member that is woken. It cannot be woken by what it does itself."},
		{label: watchEventsLabel, kind: fieldText, required: true,
			help: "The kind of event to listen to, like stage.advanced, run.quiescent (the run got stuck) or agent.failed. " +
				"End with .* to take the whole family: stage.* is every stage event."},
		{label: watchActionLabel, kind: fieldChoice, choices: []string{actWake, actNotify, actTool},
			help: "Give it a turn, or tell it, or run one tool on its behalf."},
		{label: watchToolLabel, kind: fieldChoice, choices: append([]string{noWatchTl}, agentTools...),
			help: "Only for “" + actTool + "”: which tool to run."},
		{label: watchRemoveLabel, kind: fieldToggle,
			help: "Only for a rule that exists: delete it."},
	}
	f.loadWatch()
	return f
}

// chosenWatch is the rule the Rule field points at, if it is an existing one.
func (f *teamForm) chosenWatch() (driver.BlueprintWatcher, bool) {
	for i := range f.fields {
		if f.fields[i].label == watchRuleLabel {
			if idx := f.fields[i].idx - 1; idx >= 0 && idx < len(f.watchers) {
				return f.watchers[idx], true
			}
		}
	}
	return driver.BlueprintWatcher{}, false
}

// loadWatch fills the fields with the chosen rule, or empties them for a new one.
func (f *teamForm) loadWatch() {
	w, existing := f.chosenWatch()
	for i := range f.fields {
		fl := &f.fields[i]
		switch fl.label {
		case watchMemberLabel:
			fl.idx = 0
			for j, c := range fl.choices {
				if existing && c == w.Agent {
					fl.idx = j
				}
			}
		case watchEventsLabel:
			fl.value = ""
			if existing {
				fl.value = w.Pattern
			}
		case watchActionLabel:
			fl.idx = 0
			if existing {
				for j, c := range fl.choices {
					if c == watchActionWords(w.Action) {
						fl.idx = j
					}
				}
			}
		case watchToolLabel:
			fl.idx = 0
			for j, c := range fl.choices {
				if existing && w.Tool != "" && c == w.Tool {
					fl.idx = j
				}
			}
		case watchRemoveLabel:
			fl.on = false
		}
	}
}

// submitWatch turns the form into the change to ask the core for.
func (f *teamForm) submitWatch() (*teamTask, string) {
	old, existing := f.chosenWatch()
	p := driver.BlueprintWatchParams{Name: f.target}
	var action string
	for _, fl := range f.fields {
		switch fl.label {
		case watchMemberLabel:
			p.Agent = fl.choices[fl.idx]
		case watchActionLabel:
			action = fl.choices[fl.idx]
		case watchToolLabel:
			if fl.idx > 0 {
				p.Tool = fl.choices[fl.idx]
			}
		case watchRemoveLabel:
			p.Remove = fl.on
		}
	}
	p.Pattern = f.value(watchEventsLabel)

	if p.Remove {
		if !existing {
			return nil, "there is no rule to remove: choose an existing rule in Rule first"
		}
		return &teamTask{watch: &driver.BlueprintWatchParams{Name: f.target, Agent: old.Agent, Pattern: old.Pattern, Remove: true}}, ""
	}
	if p.Agent == "" {
		return nil, "this blueprint has no members to wake"
	}
	if p.Pattern == "" {
		return nil, watchEventsLabel + " is required: say which events to listen to, like stage.advanced"
	}
	if existing && (old.Agent != p.Agent || old.Pattern != p.Pattern) {
		return nil, "a rule is its member and its events; to listen to something else add a new rule, and remove this one"
	}
	p.Action = watchActionValue[action]
	if p.Action == "run_tool" {
		if p.Tool == "" {
			return nil, watchToolLabel + " is required to run a tool: choose which one"
		}
	} else {
		p.Tool = ""
	}
	if existing && old.Action == p.Action && old.Tool == p.Tool {
		return nil, "nothing to change: choose another action or tool, or remove the rule"
	}
	return &teamTask{watch: &p}, ""
}
