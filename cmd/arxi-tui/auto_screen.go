package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// This file is /auto: the automations the core keeps in ./triggers — a team started
// on a schedule with a task written once and a spending ceiling per period. The
// person lists them, creates one with a form and pauses one with a key, and never
// types a command. They only fire while arxi-tui is open, because the scheduler is
// the TUI's own invisible child (auto_sched.go); the screen says so.

// factoryAuto is /auto's document: the flow screen's layout under its own title.
var factoryAuto = strings.Replace(factoryFlow, "· flow", "· auto", 1)

// loadAutoScene parses and validates the embedded screen.
func loadAutoScene() (*scene.Document, error) {
	doc, err := scene.ParseDocument([]byte(factoryAuto))
	if err != nil {
		return nil, fmt.Errorf("auto scene: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("auto scene: %w", err)
	}
	return doc, nil
}

// autoCore is what /auto needs from the core.
type autoCore interface {
	Hello() *driver.Hello
	SubmitTriggerList(ctx context.Context) (*driver.TriggerListResult, error)
	SubmitTriggerCreate(ctx context.Context, p driver.TriggerCreateParams) (*driver.TriggerRow, error)
	SubmitTriggerPause(ctx context.Context, name string) (*driver.TriggerRow, error)
	SubmitTriggerResume(ctx context.Context, name string) (*driver.TriggerRow, error)
	SubmitBlueprintValidate(ctx context.Context, path string) (*driver.BlueprintInfo, error)
	SubmitModelList(ctx context.Context) (*driver.ModelListResult, error)
}

var _ autoCore = (*driver.NDJSONDriver)(nil)

// autoOutcome is a worker's one answer to the loop.
type autoOutcome struct {
	rows   []driver.TriggerRow
	teams  []string // stored teams and agents that load, for the form
	models []string
	err    string // why the list could not be read

	created string // the sentence to show after a create
	paused  string // the sentence to show after a pause
	resumed string // the sentence to show after a resume
	name    string // the automation to land on
	refused string // the core's own sentence when a request was turned down
}

// readAuto lists the automations and what a new one could run.
func readAuto(ctx context.Context, core autoCore, root string) autoOutcome {
	res, err := core.SubmitTriggerList(ctx)
	if err != nil {
		return autoOutcome{err: err.Error()}
	}
	out := autoOutcome{rows: res.Triggers, models: readModels(ctx, core)}
	items, err := readTeams(ctx, core, root)
	if err != nil {
		out.err = err.Error()
	}
	for _, it := range items {
		if it.Info != nil {
			out.teams = append(out.teams, it.Name)
		}
	}
	return out
}

// startAutoRead reads on a worker so a slow core never freezes the loop.
func startAutoRead(ctx context.Context, core autoCore, root string, done chan<- autoOutcome) {
	go func() { done <- readAuto(ctx, core, root) }()
}

// startAutoCreate stores one automation and then re-reads, so the list is what is on
// disk and not what was hoped.
func startAutoCreate(ctx context.Context, core autoCore, root string, p driver.TriggerCreateParams, done chan<- autoOutcome) {
	go func() {
		row, err := core.SubmitTriggerCreate(ctx, p)
		if err != nil {
			done <- autoOutcome{refused: err.Error()}
			return
		}
		out := readAuto(ctx, core, root)
		out.name = row.Record.Name
		out.created = fmt.Sprintf("automation %s saved: %s", row.Record.Name, describeOn(row.Record.On))
		done <- out
	}()
}

// startAutoPause pauses one automation and re-reads.
func startAutoPause(ctx context.Context, core autoCore, root, name string, done chan<- autoOutcome) {
	go func() {
		if _, err := core.SubmitTriggerPause(ctx, name); err != nil {
			done <- autoOutcome{refused: err.Error()}
			return
		}
		out := readAuto(ctx, core, root)
		out.name = name
		out.paused = "automation " + name + " paused"
		done <- out
	}()
}

// startAutoResume lets one paused automation fire again and re-reads.
func startAutoResume(ctx context.Context, core autoCore, root, name string, done chan<- autoOutcome) {
	go func() {
		if _, err := core.SubmitTriggerResume(ctx, name); err != nil {
			done <- autoOutcome{refused: err.Error()}
			return
		}
		out := readAuto(ctx, core, root)
		out.name = name
		out.resumed = "automation " + name + " is on again"
		done <- out
	}()
}

// autoTask is what a key asks the loop to start.
type autoTask struct {
	create *driver.TriggerCreateParams
	pause  string // the automation to pause
	resume string // the automation to switch back on
	reload bool
}

// autoScreen is the open screen.
type autoScreen struct {
	sel     int // 0 is "New automation", then one row per stored automation
	loading bool
	rows    []driver.TriggerRow
	teams   []string
	models  []string
	note    string // why there is nothing to list

	canCreate, canPause, canResume bool
	form                           *teamForm
	working                        string
	banner                         string

	// scheduler is read each frame from the driver: whether automations can fire now.
	schedRunning bool
	schedWhy     string
	hasScheduler bool
}

const (
	autoHint     = "↑↓ choose · enter create · p pause or resume · r refresh · esc close"
	autoFormHint = "tab or ↑↓ field · ←/→ or space choose · enter next / save · esc back"
)

// newAutoScreen opens the screen. What the core can do is read off its hello.
func newAutoScreen(h *driver.Hello) *autoScreen {
	return &autoScreen{
		loading:   true,
		canCreate: helloImplements(h, "trigger.create"),
		canPause:  helloImplements(h, "trigger.pause"),
		canResume: helloImplements(h, "trigger.resume"),
	}
}

// apply stores a worker's answer.
func (a *autoScreen) apply(o autoOutcome) {
	a.working = ""
	if o.refused != "" {
		a.banner = "✗ " + o.refused
		return
	}
	a.loading = false
	a.rows, a.teams, a.models, a.note = o.rows, o.teams, o.models, o.err
	switch {
	case o.created != "":
		a.form = nil
		a.banner = "✓ " + o.created
	case o.paused != "":
		a.banner = "✓ " + o.paused
	case o.resumed != "":
		a.banner = "✓ " + o.resumed
	}
	for i, r := range a.rows {
		if r.Record.Name == o.name && o.name != "" {
			a.sel = 1 + i
		}
	}
}

// Rows in the list: the create action, then one per automation.
func (a *autoScreen) count() int { return 1 + len(a.rows) }

func (a *autoScreen) current() *driver.TriggerRow {
	if a.sel < 1 || a.sel > len(a.rows) {
		return nil
	}
	return &a.rows[a.sel-1]
}

// when formats an RFC 3339 instant on the person's clock.
func when(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.In(autoZone).Format("2006-01-02 15:04")
}

// whatItDoes reads the team and the task back out of the stored command; anything
// that is not in the form's own shape is shown as it was stored.
func whatItDoes(then string) string {
	f := strings.Fields(then)
	if len(f) >= 3 && f[0] == "run" && f[1] == "start" {
		team := f[2]
		for i := 3; i < len(f); i++ {
			if f[i] == "--" {
				return fmt.Sprintf("%s: %s", team, strings.Join(f[i+1:], " "))
			}
		}
		return team
	}
	return then
}

// nextText says when it fires next, or why it will not.
func nextText(r driver.TriggerRow) string {
	switch {
	case r.Next != "":
		return when(r.Next)
	case r.NextAbsent == "paused":
		return "paused"
	case r.NextAbsent == "external":
		return "when something outside asks for it"
	case r.NextAbsent != "":
		return r.NextAbsent
	}
	return "—"
}

// rowStatus is a row's right-hand column.
func autoRowStatus(r driver.TriggerRow) string {
	if r.Record.Status == "paused" {
		return "paused"
	}
	return describeOn(r.Record.On) + " · next " + nextText(r)
}

func autoDetail(r driver.TriggerRow) string {
	rec := r.Record
	lines := []string{
		"Does      " + whatItDoes(rec.Then),
		"Schedule  " + describeOn(rec.On),
		fmt.Sprintf("Budget    $%.2f per %s", rec.Budget, rec.BudgetPeriod),
		"Status    " + rec.Status,
		"Next      " + nextText(r),
	}
	if rec.LastFiredAt != "" {
		last := "Last      " + when(rec.LastFiredAt)
		if rec.LastStatus != "" {
			last += " · " + rec.LastStatus
		}
		lines = append(lines, last)
	} else {
		lines = append(lines, "Last      never")
	}
	if r.Missed > 0 {
		lines = append(lines, fmt.Sprintf("Missed    %s while arxi-tui was closed; they are skipped, not made up", plural(r.Missed, "run", "runs")))
	}
	if r.Note != "" {
		lines = append(lines, "Note      "+r.Note)
	}
	if rec.Status != "paused" {
		lines = append(lines, "", "Press p to pause it.")
	} else {
		lines = append(lines, "", "It is paused. Press p to switch it on again.")
	}
	return strings.Join(lines, "\n")
}

const newAutoDetail = "Create an automation: pick a team, write the task once, say when it runs and how much it may spend."

const noAutosYet = "Nothing is scheduled yet.\n\n" +
	"An automation starts a team on its own: every 30 minutes, every day at a time, or once. " +
	"Each one has a spending ceiling per period, so it can never run away with the bill."

const noTeamsForAuto = "An automation runs a team or an agent, and none is stored yet. Create one in /team first."

// schedulerLine says whether the automations can fire right now.
func (a *autoScreen) schedulerLine() string {
	switch {
	case !a.hasScheduler:
		return ""
	case a.schedRunning:
		return "● Automations fire while arxi-tui stays open."
	case a.schedWhy != "":
		return "○ Automations are not firing: " + a.schedWhy
	}
	return "○ Automations will start firing as soon as one is active."
}

// publish writes the screen's binds onto the state the renderer reads.
func (a *autoScreen) publish(st *fold.State) {
	if a.form != nil {
		a.publishForm(st)
		return
	}
	n := a.count()
	if a.sel >= n {
		a.sel = n - 1
	}
	if a.sel < 0 {
		a.sel = 0
	}
	st.UserInput, st.UserInputCaret = "", 0
	title := "Automations"
	if k := len(a.rows); k > 0 {
		title += fmt.Sprintf(" · %d scheduled", k)
	}
	var detail string
	switch {
	case a.loading:
		detail = "Reading the automations …"
	case a.sel == 0:
		detail = newAutoDetail
		if len(a.teams) == 0 {
			detail += "\n\n" + noTeamsForAuto
		}
		if len(a.rows) == 0 {
			detail += "\n\n" + noAutosYet
		}
	default:
		detail = autoDetail(a.rows[a.sel-1])
	}
	if a.note != "" {
		detail = a.note + "\n\n" + detail
	}
	if line := a.schedulerLine(); line != "" {
		detail += "\n\n" + line
	}
	if a.banner != "" {
		detail = a.banner + "\n\n" + detail
	}
	if a.working != "" {
		detail = "⏳ " + a.working + "\n\n" + detail
	}
	var rows []fold.HubRow
	lo, hi := window(a.sel, n, hubPageSize)
	for i := lo; i < hi; i++ {
		var r fold.HubRow
		if i == 0 {
			status := "a team, on a schedule"
			if !a.canCreate {
				status = "needs a newer core"
			}
			r = fold.HubRow{Label: "＋ New automation…", Status: status}
		} else {
			t := a.rows[i-1]
			r = fold.HubRow{Label: t.Record.Name, Status: autoRowStatus(t)}
		}
		r.Selected = i == a.sel
		rows = append(rows, r)
	}
	st.HubTitle, st.HubRows, st.HubHint, st.HubDetail = title, rows, autoHint, detail
}

func (a *autoScreen) publishForm(st *fold.State) {
	f := a.form
	var rows []fold.HubRow
	lo, hi := window(f.focus, len(f.fields), hubPageSize)
	for i := lo; i < hi; i++ {
		rows = append(rows, fold.HubRow{Label: f.fields[i].label, Status: f.fields[i].shown(), Selected: i == f.focus})
	}
	fl := f.fields[f.focus]
	help := fl.help
	if fl.dynHelp != nil {
		help = fl.dynHelp(f)
	}
	detail := f.help + "\n\n" + help
	if p := f.preview(); p != "" {
		detail += "\n\n" + p
	}
	if a.banner != "" {
		detail = a.banner + "\n\n" + detail
	}
	if a.working != "" {
		detail = "⏳ " + a.working + "\n\n" + detail
	}
	st.UserInput = f.typed()
	st.UserInputCaret = len([]rune(st.UserInput))
	st.HubTitle, st.HubRows, st.HubHint, st.HubDetail = f.title, rows, autoFormHint, detail
}

// key applies one key. closeIt asks the loop to leave the screen; task asks it to
// start a worker. Only Esc and q close the screen, so nothing typed here can leak
// into the chat.
func (a *autoScreen) key(k term.Key) (closeIt bool, task *autoTask) {
	if a.form != nil {
		return false, a.formKey(k)
	}
	a.banner = ""
	switch k.Type {
	case term.KeyEscape:
		return true, nil
	case term.KeyRunes:
		if k.Mod&term.ModCtrl != 0 || k.Mod&term.ModAlt != 0 || len(k.Runes) != 1 {
			return false, nil
		}
		switch k.Runes[0] {
		case 'q', 'Q':
			return true, nil
		case 'p', 'P':
			return false, a.pause()
		case 'r', 'R':
			if !a.loading && a.working == "" {
				a.loading = true
				return false, &autoTask{reload: true}
			}
		}
	case term.KeyUp:
		a.sel--
	case term.KeyDown:
		a.sel++
	case term.KeyPgUp:
		a.sel -= hubPageSize
	case term.KeyPgDn:
		a.sel += hubPageSize
	case term.KeyEnter:
		a.open()
	}
	return false, nil
}

func (a *autoScreen) pause() *autoTask {
	r := a.current()
	switch {
	case a.loading || a.working != "" || r == nil:
		return nil
	case !a.canPause:
		a.banner = "✗ " + oldCoreNotice
		return nil
	case r.Record.Status == "paused":
		if !a.canResume {
			a.banner = "✗ " + oldCoreNotice
			return nil
		}
		a.working = "Switching " + r.Record.Name + " on …"
		return &autoTask{resume: r.Record.Name}
	}
	a.working = "Pausing " + r.Record.Name + " …"
	return &autoTask{pause: r.Record.Name}
}

// open acts on Enter: only the first row does anything.
func (a *autoScreen) open() {
	if a.loading || a.working != "" || a.sel != 0 {
		return
	}
	switch {
	case !a.canCreate:
		a.banner = "✗ " + oldCoreNotice
	case len(a.teams) == 0:
		a.banner = "✗ " + noTeamsForAuto
	default:
		a.form = newAutoForm(a.teams, a.models)
	}
}

func (a *autoScreen) formKey(k term.Key) *autoTask {
	if a.working != "" && k.Type != term.KeyEscape {
		return nil
	}
	closeForm, task, msg := a.form.key(k)
	switch {
	case closeForm:
		a.form, a.banner, a.working = nil, "", ""
	case msg != "":
		a.banner = "✗ " + msg
	case task != nil && task.auto != nil:
		a.banner, a.working = "", task.working()
		return &autoTask{create: task.auto}
	default:
		a.banner = ""
	}
	return nil
}

// paste inserts clipboard text into the focused text field.
func (a *autoScreen) paste(text string) {
	if a.form != nil && a.working == "" {
		a.form.insert(text)
	}
}

// autoCommand reports whether Enter on this line (or on the highlighted menu row)
// is `/auto` with nothing after it.
func autoCommand(input string, sel int, cat string) bool {
	return menuCommand("auto", input, sel, cat)
}

const noAutoCoreNotice = "/auto needs the arxi core to keep the automations, and none is connected; " + rebuildRemedy
