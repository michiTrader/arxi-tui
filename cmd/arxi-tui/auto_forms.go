package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
)

// This file is the form of /auto: it turns what a person says in plain words ("every
// 30 minutes", "every day at 09:30") into the schedule text the core stores, and
// builds the command the automation runs. The core still validates everything; this
// only keeps the person from having to know the schedule syntax.

// autoZone and autoNow are where the person's clock comes from; tests replace them.
var (
	autoZone = time.Local
	autoNow  = time.Now
)

// The ways an automation can repeat, as the form's Repeat field shows them.
const (
	repeatEvery = "every …"
	repeatDaily = "every day at …"
	repeatOnce  = "once, at …"
)

var autoRepeats = []string{repeatEvery, repeatDaily, repeatOnce}

var autoPeriods = []string{"day", "week", "month"}

// timingHelp explains the Timing field for the chosen Repeat.
func timingHelp(f *teamForm) string {
	switch f.choice("Repeat") {
	case repeatDaily:
		return "The time of day, as HH:MM on your clock, like 09:30. It runs at that time every day."
	case repeatOnce:
		return "The moment, as YYYY-MM-DD HH:MM on your clock, like 2026-12-24 18:00. It runs once."
	}
	return "How often: a number and a unit, like 30m, 2h or 1d. The shortest is 1 minute."
}

// newAutoForm builds the "New automation" form from the stored teams and agents
// and the enabled models.
func newAutoForm(teams, models []string) *teamForm {
	return &teamForm{
		kind:  formNewAuto,
		title: "New automation",
		help: "An automation starts a team on its own, on a schedule, with a task you write once. " +
			"It spends at most the ceiling you set for each period, and it only runs while arxi-tui is open.",
		fields: []teamField{
			{label: "Name", kind: fieldText, required: true,
				help: "A short name with no spaces or slashes, like nightly-audit."},
			{label: "Team", kind: fieldChoice, choices: teams,
				help: "The team or agent that does the work. Use ←/→ to choose."},
			{label: "Task", kind: fieldText, spaces: true, required: true,
				help: "What it should do each time, in your own words."},
			{label: "Repeat", kind: fieldChoice, choices: autoRepeats,
				help: "How the schedule is written: at a fixed interval, every day at a time, or once."},
			{label: "Timing", kind: fieldText, spaces: true, required: true, dynHelp: timingHelp},
			{label: "Budget (USD)", kind: fieldText, value: runFormBudget,
				help: "The most it may spend in one period, in dollars; it must be above zero. Each run sets the whole amount aside while it works, " +
					"so a new run starts only when that much is still unspent in the period: with 1 per day, expect one run a day."},
			{label: "Per", kind: fieldChoice, choices: autoPeriods,
				help: "The period the budget applies to. When it is used up, the automation waits for the next period."},
			{label: "Model", kind: fieldChoice, choices: append([]string{eachMember}, models...),
				help: "One model for every member, or each member's own."},
			{label: "Rehearsal", kind: fieldToggle,
				help: "A practice run each time: no model is called and nothing is spent."},
		},
	}
}

// choice reads the chosen value of a choice field by label.
func (f *teamForm) choice(label string) string {
	for _, fl := range f.fields {
		if fl.label == label && fl.kind == fieldChoice && len(fl.choices) > 0 {
			return fl.choices[fl.idx]
		}
	}
	return ""
}

var everyRe = regexp.MustCompile(`^(\d+)\s*(m|min|mins|minute|minutes|h|hr|hrs|hour|hours|d|day|days)$`)

// parseEvery turns "30m", "2 hours" or "1d" into the core's every:<n><unit> and the
// words that describe it.
func parseEvery(s string) (spec, words string, err error) {
	m := everyRe.FindStringSubmatch(strings.ToLower(strings.TrimSpace(s)))
	if m == nil {
		return "", "", fmt.Errorf("Timing must be a number and a unit, like 30m, 2h or 1d")
	}
	n, _ := strconv.Atoi(m[1])
	if n < 1 {
		return "", "", fmt.Errorf("Timing must be at least 1 minute")
	}
	switch m[2][0] {
	case 'm':
		return fmt.Sprintf("every:%dm", n), "every " + unitWords(n, "minute", "minutes"), nil
	case 'h':
		return fmt.Sprintf("every:%dh", n), "every " + unitWords(n, "hour", "hours"), nil
	}
	return fmt.Sprintf("every:%dh", n*24), "every " + unitWords(n, "day", "days"), nil
}

// unitWords is "1 hour" or "3 hours".
func unitWords(n int, one, many string) string { return plural(n, one, many) }

// parseDaily turns "09:30" on the person's clock into a daily cron in UTC, which is
// the only zone the core's schedules speak.
func parseDaily(s string, zone *time.Location, now time.Time) (spec, words string, err error) {
	t, err := time.ParseInLocation("15:04", strings.TrimSpace(s), zone)
	if err != nil {
		return "", "", fmt.Errorf("Timing must be a time of day like 09:30")
	}
	local := now.In(zone)
	at := time.Date(local.Year(), local.Month(), local.Day(), t.Hour(), t.Minute(), 0, 0, zone).UTC()
	return fmt.Sprintf("cron:%d %d * * *", at.Minute(), at.Hour()),
		fmt.Sprintf("every day at %s (%s UTC)", t.Format("15:04"), at.Format("15:04")), nil
}

// parseOnce turns "2026-12-24 18:00" on the person's clock into at:<UTC instant>.
func parseOnce(s string, zone *time.Location, now time.Time) (spec, words string, err error) {
	t, err := time.ParseInLocation("2006-01-02 15:04", strings.TrimSpace(s), zone)
	if err != nil {
		return "", "", fmt.Errorf("Timing must be a date and time like 2026-12-24 18:00")
	}
	if !t.After(now) {
		return "", "", fmt.Errorf("that moment has already passed")
	}
	return "at:" + t.UTC().Format(time.RFC3339), "once, on " + t.Format("2006-01-02 at 15:04"), nil
}

// schedule reads the Repeat and Timing fields.
func (f *teamForm) schedule() (spec, words string, err error) {
	v := f.value("Timing")
	if v == "" {
		return "", "", fmt.Errorf("Timing is required")
	}
	switch f.choice("Repeat") {
	case repeatDaily:
		return parseDaily(v, autoZone, autoNow())
	case repeatOnce:
		return parseOnce(v, autoZone, autoNow())
	}
	return parseEvery(v)
}

// thenCommand is what the automation runs when it fires. Everything after the
// double dash is the task, whatever it says.
func thenCommand(team, model string, sim bool, task string) string {
	c := "run start " + team
	if model != "" {
		c += " --model " + model
	}
	if sim {
		c += " --sim"
	}
	return c + " -- " + task
}

// submitAuto validates what the form itself can know and builds the creation.
func (f *teamForm) submitAuto() (*teamTask, string) {
	name := f.value("Name")
	if name == "" {
		return nil, "Name is required"
	}
	team := f.choice("Team")
	if team == "" {
		return nil, "there is no team to run: create an agent or a team in /team first"
	}
	task := f.value("Task")
	if task == "" {
		return nil, "Task is required"
	}
	spec, _, err := f.schedule()
	if err != nil {
		return nil, err.Error()
	}
	b, err := strconv.ParseFloat(f.value("Budget (USD)"), 64)
	if err != nil || b <= 0 {
		return nil, "Budget (USD) must be a number above 0, like 1 or 0.50"
	}
	model := ""
	for _, fl := range f.fields {
		if fl.label == "Model" && fl.idx > 0 {
			model = fl.choices[fl.idx]
		}
	}
	sim := false
	for _, fl := range f.fields {
		if fl.label == "Rehearsal" {
			sim = fl.on
		}
	}
	return &teamTask{auto: &driver.TriggerCreateParams{
		Name: name, On: spec, Then: thenCommand(team, model, sim, task),
		Budget: b, BudgetPeriod: f.choice("Per"),
	}}, ""
}

// preview says in words what the schedule typed so far means, or why it does not
// yet; it is shown under the form so a mistake is seen before Enter.
func (f *teamForm) preview() string {
	if f.value("Timing") == "" {
		return ""
	}
	_, words, err := f.schedule()
	if err != nil {
		return "→ " + err.Error()
	}
	return "→ Runs " + words + "."
}

// describeOn spells a stored schedule the way a person would say it.
func describeOn(on string) string {
	switch {
	case strings.HasPrefix(on, "every:"):
		v := strings.TrimPrefix(on, "every:")
		if m := everyRe.FindStringSubmatch(v); m != nil {
			n, _ := strconv.Atoi(m[1])
			switch m[2][0] {
			case 'm':
				return "every " + unitWords(n, "minute", "minutes")
			case 'h':
				if n%24 == 0 {
					return "every " + unitWords(n/24, "day", "days")
				}
				return "every " + unitWords(n, "hour", "hours")
			}
		}
	case strings.HasPrefix(on, "at:"):
		if t, err := time.Parse(time.RFC3339, strings.TrimPrefix(on, "at:")); err == nil {
			return "once, on " + t.In(autoZone).Format("2006-01-02 at 15:04")
		}
	case strings.HasPrefix(on, "cron:"):
		f := strings.Fields(strings.TrimPrefix(on, "cron:"))
		if len(f) == 5 && f[2] == "*" && f[3] == "*" && f[4] == "*" {
			m, e1 := strconv.Atoi(f[0])
			h, e2 := strconv.Atoi(f[1])
			if e1 == nil && e2 == nil {
				at := time.Date(2000, 1, 1, h, m, 0, 0, time.UTC).In(autoZone)
				return "every day at " + at.Format("15:04")
			}
		}
	}
	return on
}
