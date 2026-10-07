package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// This file is /team: the architecture of the teams stored in ./agents, drawn
// WITHOUT running them. /flow shows what a running team is doing; /team shows what
// a team is — its stages, who takes part in each, what each member thinks with and
// may touch, and who watches for trouble. The core describes each file
// (blueprint.validate); this screen only lays the answer out.

// teamDir is where teams live, relative to the folder arxi-tui was opened in. The
// core keeps the same convention (agentstore.DefaultDir), so a file seen here is a
// file `arxi run start <name>` finds.
const teamDir = "agents"

// factoryTeam is /team's document: the flow screen's layout under its own title.
var factoryTeam = strings.Replace(factoryFlow, "· flow", "· team", 1)

// loadTeamScene parses and validates the embedded screen.
func loadTeamScene() (*scene.Document, error) {
	doc, err := scene.ParseDocument([]byte(factoryTeam))
	if err != nil {
		return nil, fmt.Errorf("team scene: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("team scene: %w", err)
	}
	return doc, nil
}

// teamCore is what /team needs from the core, kept apart from hubCore so the
// provider fakes do not have to know about blueprints.
type teamCore interface {
	Hello() *driver.Hello
	SubmitBlueprintValidate(ctx context.Context, path string) (*driver.BlueprintInfo, error)
	SubmitModelList(ctx context.Context) (*driver.ModelListResult, error)
	SubmitAgentCreate(ctx context.Context, p driver.AgentCreateParams) (*driver.AgentCreateResult, error)
	SubmitBlueprintCreate(ctx context.Context, p driver.BlueprintCreateParams) (*driver.BlueprintCreateResult, error)
}

// teamItem is one file in ./agents and what the core said about it.
type teamItem struct {
	Name string
	Path string
	Info *driver.BlueprintInfo
	Err  string
}

// teamOutcome is the worker's one answer to the loop.
type teamOutcome struct {
	items  []teamItem
	models []string // enabled "provider/id" refs, for the agent form
	err    string   // why the list could not be read
	// created is set after a create: the sentence to show and the name to land on.
	created string
	name    string
	// refused is the core's own sentence when a create was turned down.
	refused string
	// launched is the id of a run that has just started: the screen hands over to /flow.
	launched string
}

// runLauncher is a driver that can start a run of a stored agent or team and begin
// following it. Only the live connection to the core can.
type runLauncher interface {
	LaunchRun(ctx context.Context, r runLaunch) (string, error)
}

// startTeamRun starts a run on a worker; the form's refusal is the core's sentence.
func startTeamRun(ctx context.Context, l runLauncher, r runLaunch, done chan<- teamOutcome) {
	go func() {
		id, err := l.LaunchRun(ctx, r)
		if err != nil {
			done <- teamOutcome{refused: err.Error()}
			return
		}
		done <- teamOutcome{launched: id}
	}()
}

// blueprintReader is the one thing reading ./agents needs from the core.
type blueprintReader interface {
	SubmitBlueprintValidate(ctx context.Context, path string) (*driver.BlueprintInfo, error)
}

// modelLister is the one thing reading the enabled models needs from the core.
type modelLister interface {
	SubmitModelList(ctx context.Context) (*driver.ModelListResult, error)
}

// readTeams lists ./agents/*.yaml under root and asks the core to describe each.
// A file the core refuses stays in the list with its reason: hiding it would make
// a broken team look like a team that was never written.
func readTeams(ctx context.Context, core blueprintReader, root string) ([]teamItem, error) {
	dir := filepath.Join(root, teamDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	var items []teamItem
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		it := teamItem{Name: strings.TrimSuffix(e.Name(), ".yaml"), Path: filepath.Join(dir, e.Name())}
		info, err := core.SubmitBlueprintValidate(ctx, it.Path)
		if err != nil {
			it.Err = err.Error()
		} else {
			it.Info = info
		}
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

// readModels lists the models the chat can already use, as "provider/id". A core
// that cannot list them just offers none: an agent may be left to the default model.
func readModels(ctx context.Context, core modelLister) []string {
	res, err := core.SubmitModelList(ctx)
	if err != nil || res == nil {
		return nil
	}
	var out []string
	for _, m := range res.Models {
		if m.Enabled {
			out = append(out, modelRef(m))
		}
	}
	sort.Strings(out)
	return out
}

// startTeamRead reads the teams on a worker so a slow core never freezes the loop.
func startTeamRead(ctx context.Context, core teamCore, root string, done chan<- teamOutcome) {
	go func() {
		items, err := readTeams(ctx, core, root)
		if err != nil {
			done <- teamOutcome{err: err.Error()}
			return
		}
		done <- teamOutcome{items: items, models: readModels(ctx, core)}
	}()
}

// startTeamCreate writes one agent or team through the core and then re-reads the
// folder, so the list the person sees is what is on disk and not what was hoped.
func startTeamCreate(ctx context.Context, core teamCore, root string, task teamTask, done chan<- teamOutcome) {
	go func() {
		var out teamOutcome
		switch {
		case task.agent != nil:
			res, err := core.SubmitAgentCreate(ctx, *task.agent)
			if err != nil {
				done <- teamOutcome{refused: err.Error()}
				return
			}
			out.created, out.name = task.done(res, nil), res.Name
		case task.team != nil:
			res, err := core.SubmitBlueprintCreate(ctx, *task.team)
			if err != nil {
				done <- teamOutcome{refused: err.Error()}
				return
			}
			out.created, out.name = task.done(nil, res), res.Name
		}
		items, err := readTeams(ctx, core, root)
		if err != nil {
			out.err = err.Error()
		}
		out.items, out.models = items, readModels(ctx, core)
		done <- out
	}()
}

// Rows above the stored files: the two things the screen can create.
const (
	rowNewAgent = iota
	rowNewTeam
	teamActions
)

// teamScreen is the open screen: the list, the highlight and, when one is open, the
// form that creates an agent or a team.
type teamScreen struct {
	sel     int // 0..teamActions-1 are the actions, then one row per stored file
	loading bool
	items   []teamItem
	models  []string
	note    string // why there is nothing to list (no core, a failed read)

	canAgent, canTeam bool // the core implements the create verbs
	form              *teamForm
	working           string // set while the core writes a file
	banner            string // the last thing that happened, good or bad
	launched          string // set when a run started: the loop opens /flow
}

const (
	teamHint     = "↑↓ choose · enter run or open · esc close"
	teamFormHint = "tab or ↑↓ field · ←/→ or space choose · enter next / create · esc back"
)

// newTeamScreen opens the manager. What the core can create is read off its hello.
func newTeamScreen(h *driver.Hello) *teamScreen {
	return &teamScreen{
		loading:  true,
		canAgent: helloImplements(h, "agent.create"),
		canTeam:  helloImplements(h, "blueprint.create"),
	}
}

// apply stores a worker's answer.
func (t *teamScreen) apply(o teamOutcome) {
	t.working = ""
	if o.refused != "" {
		// The form stays open with what the person typed: a refusal is a reason to
		// fix one field, not to start over.
		t.banner = "✗ " + o.refused
		if strings.Contains(o.refused, "name no model") {
			t.banner += " — choose a model in the Model field"
		}
		return
	}
	if o.launched != "" {
		t.launched = o.launched
		return
	}
	t.loading = false
	t.items, t.models, t.note = o.items, o.models, o.err
	if o.created != "" {
		t.form = nil
		t.banner = "✓ " + o.created
		for i, it := range t.items {
			if it.Name == o.name {
				t.sel = teamActions + i
			}
		}
	}
}

// members are the stored agents a team can be made of: files that load with exactly
// one member (a team of several is not an agent).
func (t *teamScreen) members() []string {
	var out []string
	for _, it := range t.items {
		if it.Info != nil && len(it.Info.Members) == 1 {
			out = append(out, it.Name)
		}
	}
	return out
}

// duration spells a millisecond timeout the way a person would say it.
func duration(ms int64) string {
	switch {
	case ms >= 3600000 && ms%3600000 == 0:
		return fmt.Sprintf("%d h", ms/3600000)
	case ms >= 60000 && ms%60000 == 0:
		return fmt.Sprintf("%d min", ms/60000)
	case ms >= 1000 && ms%1000 == 0:
		return fmt.Sprintf("%d s", ms/1000)
	}
	return fmt.Sprintf("%d ms", ms)
}

// inStage reports whether member m takes part in the stage. A member that names no
// stages takes part in all of them, which is how the blueprint declares it.
func inStage(m driver.BlueprintMember, stage string) bool {
	if len(m.Stages) == 0 {
		return true
	}
	for _, s := range m.Stages {
		if s == stage {
			return true
		}
	}
	return false
}

// advanceText says in words when a stage moves on, given the members that count.
func advanceText(rule string, counted []string) string {
	who := strings.Join(counted, ", ")
	switch {
	case len(counted) == 0:
		return "nobody counts here, so it never advances by itself"
	case rule == "all":
		return "every one of " + who + " must submit"
	case rule == "any":
		return "any one of " + who + " submitting is enough"
	case strings.HasPrefix(rule, "quorum:"):
		return strings.TrimPrefix(rule, "quorum:") + " of " + who + " must submit"
	}
	return rule + " (" + who + ")"
}

// teamDetail draws one team: the stage rail, what each stage waits for, the
// members, the watchers and where the work happens.
func teamDetail(it teamItem) string {
	if it.Err != "" {
		return "✗ the core refuses this blueprint:\n\n" + it.Err +
			"\n\nThe file is " + filepath.ToSlash(filepath.Join(teamDir, it.Name+".yaml")) + "."
	}
	b := it.Info
	var parts []string

	if len(b.Stages) == 0 {
		who := make([]string, 0, len(b.Members))
		for _, m := range b.Members {
			who = append(who, m.Name)
		}
		parts = append(parts, "No stages: "+strings.Join(who, ", ")+" work together until the run ends.")
	} else {
		names := make([]string, len(b.Stages))
		for i, s := range b.Stages {
			names[i] = s.Name
		}
		lines := []string{"Stages  " + strings.Join(names, " → ")}
		for i, s := range b.Stages {
			var counted, advisory []string
			for _, m := range b.Members {
				switch {
				case !inStage(m, s.Name):
				case m.Advisory:
					advisory = append(advisory, m.Name)
				default:
					counted = append(counted, m.Name)
				}
			}
			line := fmt.Sprintf("  %d. %s — %s", i+1, s.Name, advanceText(s.AdvanceWhen, counted))
			if len(advisory) > 0 {
				line += "; " + strings.Join(advisory, ", ") + " only advise"
			}
			if s.TimeoutMs > 0 {
				line += fmt.Sprintf(". After %s: %s", duration(s.TimeoutMs), orWord(s.OnTimeout, "stop"))
			}
			lines = append(lines, line)
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}

	lines := []string{"Members"}
	for _, m := range b.Members {
		line := "  " + m.Name
		if m.Role != "" {
			line += " · " + m.Role
		}
		if m.Advisory {
			line += " · advisory"
		}
		if m.Model != "" {
			line += " · " + m.Model
		}
		if len(m.Tools) > 0 {
			line += " · tools: " + strings.Join(m.Tools, ", ")
		}
		if len(m.Stages) > 0 {
			line += " · only in " + strings.Join(m.Stages, ", ")
		}
		lines = append(lines, line)
	}
	parts = append(parts, strings.Join(lines, "\n"))

	if len(b.Watchers) > 0 {
		lines := []string{"Watchers"}
		for _, w := range b.Watchers {
			lines = append(lines, fmt.Sprintf("  %s watches %s → %s", w.Agent, w.Pattern, orWord(w.Action, "wake")))
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}
	if b.Workspace != "" {
		line := "Works in: " + b.Workspace
		if b.WorkspaceReason != "" {
			line += " (" + b.WorkspaceReason + ")"
		}
		parts = append(parts, line)
	}
	return strings.Join(parts, "\n\n")
}

func orWord(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// teamRowStatus is a row's right-hand column.
func teamRowStatus(it teamItem) string {
	if it.Err != "" {
		return "✗ not valid"
	}
	if isAgent(it.Info) {
		m := it.Info.Members[0]
		return "agent · " + orWord(m.Role, "no role") + " · " + orWord(m.Model, "default model")
	}
	n := len(it.Info.Members)
	word := "members"
	if n == 1 {
		word = "member"
	}
	s := fmt.Sprintf("%d %s", n, word)
	if k := len(it.Info.Stages); k > 0 {
		w := "stages"
		if k == 1 {
			w = "stage"
		}
		s += fmt.Sprintf(" · %d %s", k, w)
	}
	return s
}

// isAgent reports whether a described file is a single agent rather than a team.
func isAgent(info *driver.BlueprintInfo) bool {
	return info != nil && len(info.Members) == 1 && len(info.Stages) == 0
}

const newAgentDetail = "Create an agent: a name, the model it thinks with and the tools it may touch.\n\n" +
	"Agents are the pieces; a team is built out of them."

const newTeamDetail = "Compose a team out of the agents you have: pick the members and name the stages the work goes through."

const noAgentsYet = "A team is made of agents, and there are none yet. Create an agent first."

const oldCoreNotice = "this arxi core cannot create files yet; " + rebuildRemedy

// emptyTeams is what the screen says under the actions when ./agents holds nothing.
const emptyTeams = "Nothing is stored in " + teamDir + "/ yet.\n\n" +
	"Start with “New agent”: give it a name, a model and the tools it may use. " +
	"Then “New team” puts agents together, with the stages they go through."

// publish writes the screen's binds onto the state the renderer reads.
func (t *teamScreen) publish(st *fold.State) {
	if t.form != nil {
		t.publishForm(st)
		return
	}
	n := teamActions + len(t.items)
	if t.sel >= n {
		t.sel = n - 1
	}
	if t.sel < 0 {
		t.sel = 0
	}
	st.UserInput, st.UserInputCaret = "", 0
	title := "Agents & teams"
	if k := len(t.items); k > 0 {
		title += fmt.Sprintf(" · %d in %s/", k, teamDir)
	}
	var detail string
	switch {
	case t.loading:
		detail = "Reading " + teamDir + "/ …"
	case t.sel == rowNewAgent:
		detail = newAgentDetail
	case t.sel == rowNewTeam:
		detail = newTeamDetail
		if len(t.members()) == 0 {
			detail += "\n\n" + noAgentsYet
		}
	default:
		detail = teamDetail(t.items[t.sel-teamActions]) + "\n\nPress enter to run it."
	}
	if t.note != "" {
		detail = t.note + "\n\n" + detail
	}
	if t.sel < teamActions && len(t.items) == 0 && !t.loading {
		detail += "\n\n" + emptyTeams
	}
	if t.banner != "" {
		detail = t.banner + "\n\n" + detail
	}
	if t.working != "" {
		detail = "⏳ " + t.working + "\n\n" + detail
	}
	var rows []fold.HubRow
	lo, hi := window(t.sel, n, hubPageSize)
	for i := lo; i < hi; i++ {
		var r fold.HubRow
		switch {
		case i == rowNewAgent:
			r = fold.HubRow{Label: "＋ New agent…", Status: orWord(t.createGap(t.canAgent), "one worker")}
		case i == rowNewTeam:
			r = fold.HubRow{Label: "＋ New team…", Status: orWord(t.createGap(t.canTeam), "agents working together")}
		default:
			it := t.items[i-teamActions]
			r = fold.HubRow{Label: it.Name, Status: teamRowStatus(it)}
		}
		r.Selected = i == t.sel
		rows = append(rows, r)
	}
	st.HubTitle, st.HubRows, st.HubHint, st.HubDetail = title, rows, teamHint, detail
}

// createGap is the row's status when the core cannot do what the row promises.
func (t *teamScreen) createGap(can bool) string {
	if can {
		return ""
	}
	return "needs a newer core"
}

// publishForm draws the open form: one row per field, its help under it.
func (t *teamScreen) publishForm(st *fold.State) {
	f := t.form
	var rows []fold.HubRow
	lo, hi := window(f.focus, len(f.fields), hubPageSize)
	for i := lo; i < hi; i++ {
		rows = append(rows, fold.HubRow{Label: f.fields[i].label, Status: f.fields[i].shown(), Selected: i == f.focus})
	}
	detail := f.help + "\n\n" + f.fields[f.focus].help
	if t.banner != "" {
		detail = t.banner + "\n\n" + detail
	}
	if t.working != "" {
		detail = "⏳ " + t.working + "\n\n" + detail
	}
	st.UserInput = f.typed()
	st.UserInputCaret = len([]rune(st.UserInput))
	st.HubTitle, st.HubRows, st.HubHint, st.HubDetail = f.title, rows, teamFormHint, detail
}

// key applies one key. closeIt asks the loop to leave the screen; task asks it to
// start the worker that writes the file. Only Esc and q close the screen, so
// nothing typed here can leak into the chat.
func (t *teamScreen) key(k term.Key) (closeIt bool, task *teamTask) {
	if t.form != nil {
		return false, t.formKey(k)
	}
	t.banner = ""
	switch k.Type {
	case term.KeyEscape:
		return true, nil
	case term.KeyRunes:
		return k.Mod&term.ModCtrl == 0 && len(k.Runes) == 1 && (k.Runes[0] == 'q' || k.Runes[0] == 'Q'), nil
	case term.KeyUp:
		t.sel--
	case term.KeyDown:
		t.sel++
	case term.KeyPgUp:
		t.sel -= hubPageSize
	case term.KeyPgDn:
		t.sel += hubPageSize
	case term.KeyEnter:
		t.open()
	}
	return false, nil
}

// open acts on Enter over an action row.
func (t *teamScreen) open() {
	if t.loading || t.working != "" {
		return
	}
	if t.sel >= teamActions {
		it := t.items[t.sel-teamActions]
		if it.Err != "" {
			t.banner = "✗ the core refuses this file, so it cannot run"
			return
		}
		t.form = newRunForm(it.Name, t.models)
		return
	}
	switch t.sel {
	case rowNewAgent:
		if !t.canAgent {
			t.banner = "✗ " + oldCoreNotice
			return
		}
		t.form = newAgentForm(t.models)
	case rowNewTeam:
		switch {
		case !t.canTeam:
			t.banner = "✗ " + oldCoreNotice
		case len(t.members()) == 0:
			t.banner = "✗ " + noAgentsYet
		default:
			t.form = newTeamForm(t.members())
		}
	}
}

// formKey applies a key to the open form and returns the task Enter produced.
func (t *teamScreen) formKey(k term.Key) *teamTask {
	if t.working != "" && k.Type != term.KeyEscape {
		return nil
	}
	closeForm, task, msg := t.form.key(k)
	switch {
	case closeForm:
		t.form, t.banner, t.working = nil, "", ""
	case msg != "":
		t.banner = "✗ " + msg
	case task != nil:
		t.banner, t.working = "", task.working()
	default:
		t.banner = ""
	}
	return task
}

// paste inserts clipboard text into the focused text field.
func (t *teamScreen) paste(text string) {
	if t.form != nil && t.working == "" {
		t.form.insert(text)
	}
}

// teamCommand reports whether Enter on this line (or on the highlighted menu row)
// is `/team` with nothing after it.
func teamCommand(input string, sel int, cat string) bool {
	return menuCommand("team", input, sel, cat)
}

// noTeamCoreNotice is shown when /team has no core to describe the files.
const noTeamCoreNotice = "/team needs the arxi core to read the blueprints, and none is connected; " + rebuildRemedy

// helloImplements reports whether the core's hello lists the verb as implemented
// (not merely declared: a build can declare a verb it never wired).
func helloImplements(h *driver.Hello, verb string) bool {
	if h == nil {
		return false
	}
	for _, v := range h.Implemented {
		if v == verb {
			return true
		}
	}
	return false
}

var _ teamCore = (*driver.NDJSONDriver)(nil)
