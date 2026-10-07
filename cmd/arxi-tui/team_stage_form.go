package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/driver"
)

// This file is the half of /team that CHANGES a team already stored: how each of
// its stages decides it is finished, and when it gives up. Creating a team writes
// every stage as "everybody must finish" with no time limit, because no member can
// say otherwise; this form is where the person says it, without opening a file.
//
// Like the other forms it only gathers and shapes. A rule the core refuses (a
// quorum larger than the team) comes back as the core's own sentence and the file
// is left as it was.

// The answers of the Advance field and what each one asks of the core.
const (
	keepRule   = "(keep as it is)"
	ruleAll    = "everyone must finish"
	ruleAny    = "the first to finish is enough"
	ruleQuorum = "a number of them must finish"
)

// The answers of the field about running out of time.
const (
	keepOnTimeout   = "(keep as it is)"
	onTimeoutAsk    = "ask me what to do"
	onTimeoutMove   = "move on to the next stage"
	onTimeoutStop   = "stop the run"
	stageLimitLabel = "Time limit (minutes)"
)

var onTimeoutValue = map[string]string{
	onTimeoutAsk:  "escalate",
	onTimeoutMove: "advance",
	onTimeoutStop: "fail",
}

// ruleWords says a stage's rule the way the form does, for the "now" summary.
func ruleWords(rule string) string {
	switch {
	case rule == "all":
		return ruleAll
	case rule == "any":
		return ruleAny
	case strings.HasPrefix(rule, "quorum:"):
		return strings.TrimPrefix(rule, "quorum:") + " of them must finish"
	}
	return rule
}

// onTimeoutWords is what happens at the limit, in words.
func onTimeoutWords(v string) string {
	switch v {
	case "advance":
		return onTimeoutMove
	case "fail":
		return onTimeoutStop
	}
	return onTimeoutAsk
}

// stagesNow is the summary of the current rules the form opens with, so nobody has
// to remember what the team does today in order to change it.
func stagesNow(stages []driver.BlueprintStage) string {
	lines := []string{"Right now:"}
	for i, s := range stages {
		line := fmt.Sprintf("  %d. %s — %s", i+1, s.Name, ruleWords(s.AdvanceWhen))
		if s.TimeoutMs > 0 {
			line += fmt.Sprintf("; after %s: %s", duration(s.TimeoutMs), onTimeoutWords(s.OnTimeout))
		} else {
			line += "; no time limit"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// newStagesForm builds the form that changes the rules of one stage of a team.
func newStagesForm(team string, stages []driver.BlueprintStage) *teamForm {
	names := make([]string, len(stages))
	for i, s := range stages {
		names[i] = s.Name
	}
	return &teamForm{
		kind:   formEditStages,
		title:  "Edit stages · " + team,
		target: team,
		help: "Change how one stage of " + team + " is finished, or how long it may take. " +
			"Leave a field as it is to keep it. It applies to runs you start from now on; " +
			"a run already going keeps the rules it began with.\n\n" + stagesNow(stages),
		fields: []teamField{
			{label: "Stage", kind: fieldChoice, choices: names,
				help: "Which stage to change. Use ←/→ to choose."},
			{label: "Finished when", kind: fieldChoice,
				choices: []string{keepRule, ruleAll, ruleAny, ruleQuorum},
				help:    "When the stage may move on. Members marked advisory never count: they only give an opinion."},
			{label: "How many", kind: fieldText,
				help: "Only for “" + ruleQuorum + "”: the number of members that must finish, like 2. It cannot be more than the members that take part."},
			{label: stageLimitLabel, kind: fieldText,
				help: "How long the stage may take before something happens, like 10 or 0.5. Type 0 to remove the limit; leave it empty to keep what is there."},
			{label: "When time runs out", kind: fieldChoice,
				choices: []string{keepOnTimeout, onTimeoutAsk, onTimeoutMove, onTimeoutStop},
				help:    "Asking you is the safe answer: the run waits for your decision. Moving on or stopping happens without asking."},
		},
	}
}

// submitStages turns the form into the change to ask the core for.
func (f *teamForm) submitStages() (*teamTask, string) {
	p := driver.BlueprintStageParams{Name: f.target}
	var rule string
	for _, fl := range f.fields {
		switch fl.label {
		case "Stage":
			p.Stage = fl.choices[fl.idx]
		case "Finished when":
			rule = fl.choices[fl.idx]
		case "When time runs out":
			p.OnTimeout = onTimeoutValue[fl.choices[fl.idx]]
		}
	}
	switch rule {
	case ruleAll:
		p.AdvanceWhen = "all"
	case ruleAny:
		p.AdvanceWhen = "any"
	case ruleQuorum:
		n, err := strconv.Atoi(f.value("How many"))
		if err != nil || n < 1 {
			return nil, "How many must be a whole number of 1 or more, like 2"
		}
		p.AdvanceWhen = fmt.Sprintf("quorum:%d", n)
	}
	if raw := f.value(stageLimitLabel); raw != "" {
		min, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
		if err != nil || min < 0 || math.IsNaN(min) || math.IsInf(min, 0) {
			return nil, stageLimitLabel + " must be a number of minutes, like 10 or 0.5 (0 removes the limit)"
		}
		ms := int64(math.Round(min * 60000))
		if min > 0 && ms == 0 {
			return nil, stageLimitLabel + " is too small: the least is a thousandth of a minute"
		}
		p.TimeoutMs = &ms
	}
	if p.AdvanceWhen == "" && p.TimeoutMs == nil && p.OnTimeout == "" {
		return nil, "nothing to change: choose a new rule, a time limit or what happens when it runs out"
	}
	return &teamTask{stage: &p}, ""
}
