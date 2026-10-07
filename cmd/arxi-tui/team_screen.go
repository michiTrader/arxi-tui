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
	items []teamItem
	err   string
}

// readTeams lists ./agents/*.yaml under root and asks the core to describe each.
// A file the core refuses stays in the list with its reason: hiding it would make
// a broken team look like a team that was never written.
func readTeams(ctx context.Context, core teamCore, root string) ([]teamItem, error) {
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

// startTeamRead reads the teams on a worker so a slow core never freezes the loop.
func startTeamRead(ctx context.Context, core teamCore, root string, done chan<- teamOutcome) {
	go func() {
		items, err := readTeams(ctx, core, root)
		if err != nil {
			done <- teamOutcome{err: err.Error()}
			return
		}
		done <- teamOutcome{items: items}
	}()
}

// teamScreen is the open screen: the list it was given and the highlight.
type teamScreen struct {
	sel     int
	loading bool
	items   []teamItem
	note    string // why there is nothing to list (no core, a failed read)
}

const teamHint = "↑↓ choose · esc close"

// apply stores a worker's answer.
func (t *teamScreen) apply(o teamOutcome) {
	t.loading = false
	t.items, t.note = o.items, o.err
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
			"\n\nFix " + filepath.ToSlash(filepath.Join(teamDir, it.Name+".yaml")) + " and open /team again."
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

// emptyTeams is what the screen says when ./agents holds nothing.
const emptyTeams = "There are no teams in this folder yet.\n\n" +
	"A team is a file named " + teamDir + "/<name>.yaml that lists its members, the stages they go through " +
	"and who watches for trouble. Create one with:\n\n" +
	"  arxi agent create <name> --model <provider/model> --role <role>\n" +
	"  arxi blueprint create <team> --members <a>,<b>\n\n" +
	"Then open /team again to see its architecture."

// publish writes the screen's binds onto the state the renderer reads.
func (t *teamScreen) publish(st *fold.State) {
	n := len(t.items)
	if t.sel >= n {
		t.sel = n - 1
	}
	if t.sel < 0 {
		t.sel = 0
	}
	st.UserInput, st.UserInputCaret = "", 0
	title := "Teams"
	if n > 0 {
		title += fmt.Sprintf(" · %d in %s/", n, teamDir)
	}
	var detail string
	switch {
	case t.loading:
		detail = "Reading the teams …"
	case t.note != "":
		detail = t.note
	case n == 0:
		detail = emptyTeams
	default:
		detail = teamDetail(t.items[t.sel])
	}
	var rows []fold.HubRow
	lo, hi := window(t.sel, n, hubPageSize)
	for i := lo; i < hi; i++ {
		rows = append(rows, fold.HubRow{Label: t.items[i].Name, Status: teamRowStatus(t.items[i]), Selected: i == t.sel})
	}
	st.HubTitle, st.HubRows, st.HubHint, st.HubDetail = title, rows, teamHint, detail
}

// key applies one key and reports whether the screen should close. Only Esc and q
// close it, so nothing typed here can leak into the chat.
func (t *teamScreen) key(k term.Key) (closeIt bool) {
	switch k.Type {
	case term.KeyEscape:
		return true
	case term.KeyRunes:
		return k.Mod&term.ModCtrl == 0 && len(k.Runes) == 1 && (k.Runes[0] == 'q' || k.Runes[0] == 'Q')
	case term.KeyUp:
		t.sel--
	case term.KeyDown:
		t.sel++
	case term.KeyPgUp:
		t.sel -= hubPageSize
	case term.KeyPgDn:
		t.sel += hubPageSize
	}
	return false
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
