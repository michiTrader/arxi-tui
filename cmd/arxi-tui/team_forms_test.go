package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

func rk(r rune) term.Key         { return term.Key{Type: term.KeyRunes, Runes: []rune{r}} }
func tk(t term.KeyType) term.Key { return term.Key{Type: t} }

// typeInto sends each rune of s to the form as a key press.
func typeForm(f *teamForm, s string) {
	for _, r := range s {
		f.key(rk(r))
	}
}

func TestNewAgentFormBuildsOnlyWhatWasChosen(t *testing.T) {
	f := newAgentForm([]string{"openai/gpt-5", "z/m"})
	typeForm(f, "backend")
	f.key(tk(term.KeyTab))   // Model
	f.key(tk(term.KeyRight)) // first real model
	f.key(tk(term.KeyTab))   // Role
	typeForm(f, "implementer")
	f.key(tk(term.KeyTab)) // bash
	f.key(tk(term.KeyTab)) // edit
	f.key(tk(term.KeyTab)) // grep
	f.key(tk(term.KeyTab)) // read
	f.key(rk(' '))         // read on
	f.key(tk(term.KeyTab)) // write
	f.key(rk(' '))         // write on
	f.key(tk(term.KeyTab)) // advisory
	f.key(rk(' '))
	_, task, msg := f.key(tk(term.KeyEnter))
	if msg != "" || task == nil || task.agent == nil {
		t.Fatalf("task=%v msg=%q", task, msg)
	}
	p := task.agent
	if p.Name != "backend" || p.Model != "openai/gpt-5" || p.Role != "implementer" || !p.Advisory ||
		strings.Join(p.Tools, ",") != "read,write" {
		t.Fatalf("params = %+v", p)
	}
}

func TestNewAgentFormLeavesTheModelToTheCoreWhenNoneIsPicked(t *testing.T) {
	f := newAgentForm(nil)
	typeForm(f, "solo")
	for range f.fields[1:] {
		f.key(tk(term.KeyTab))
	}
	_, task, _ := f.key(tk(term.KeyEnter))
	if task == nil || task.agent.Model != "" || len(task.agent.Tools) != 0 {
		t.Fatalf("task = %+v", task)
	}
}

func TestFormsNameTheFieldThatIsMissing(t *testing.T) {
	f := newAgentForm(nil)
	f.focus = len(f.fields) - 1
	if _, task, msg := f.key(tk(term.KeyEnter)); task != nil || msg != "Name is required" {
		t.Fatalf("task=%v msg=%q", task, msg)
	}
	g := newTeamForm([]string{"a", "b"})
	typeForm(g, "crew")
	g.focus = len(g.fields) - 1
	if _, task, msg := g.key(tk(term.KeyEnter)); task != nil || !strings.Contains(msg, "at least one member") {
		t.Fatalf("task=%v msg=%q", task, msg)
	}
}

func TestNewTeamFormPicksMembersAndSplitsStages(t *testing.T) {
	f := newTeamForm([]string{"backend", "reviewer"})
	typeForm(f, "crew")
	f.key(tk(term.KeyTab))
	f.key(rk(' ')) // backend
	f.key(tk(term.KeyTab))
	f.key(tk(term.KeyTab)) // Stages
	typeForm(f, "build, review ,")
	_, task, msg := f.key(tk(term.KeyEnter))
	if msg != "" || task == nil || task.team == nil {
		t.Fatalf("task=%v msg=%q", task, msg)
	}
	p := task.team
	if p.Name != "crew" || strings.Join(p.Members, ",") != "backend" || strings.Join(p.Stages, ",") != "build,review" {
		t.Fatalf("params = %+v", p)
	}
}

func TestATextFieldTakesNoWhitespaceExceptStages(t *testing.T) {
	f := newTeamForm([]string{"a"})
	f.insert("my crew\n")
	if got := f.value("Name"); got != "mycrew" {
		t.Errorf("name = %q", got)
	}
	f.focus = len(f.fields) - 1
	f.insert("build review")
	if got := f.value("Stages"); got != "build review" {
		t.Errorf("stages = %q", got)
	}
}

func TestTheAgentsAreTheMembersAndATeamIsNot(t *testing.T) {
	ts := &teamScreen{items: []teamItem{
		{Name: "backend", Info: &driver.BlueprintInfo{Members: []driver.BlueprintMember{{Name: "backend"}}}},
		{Name: "crew", Info: &driver.BlueprintInfo{Members: []driver.BlueprintMember{{Name: "a"}, {Name: "b"}}}},
		{Name: "broken", Err: "nope"},
	}}
	if got := strings.Join(ts.members(), ","); got != "backend" {
		t.Fatalf("members = %q", got)
	}
}

func TestOpeningAFormNeedsTheVerbAndTheAgents(t *testing.T) {
	ts := &teamScreen{}
	ts.key(tk(term.KeyEnter))
	if ts.form != nil || !strings.Contains(ts.banner, "cannot create files yet") {
		t.Fatalf("old core: form=%v banner=%q", ts.form, ts.banner)
	}
	ts = &teamScreen{canAgent: true, canTeam: true, sel: rowNewTeam}
	ts.key(tk(term.KeyEnter))
	if ts.form != nil || !strings.Contains(ts.banner, "Create an agent first") {
		t.Fatalf("no agents: form=%v banner=%q", ts.form, ts.banner)
	}
}

func TestARefusalKeepsTheFormAndShowsTheCoresSentence(t *testing.T) {
	ts := &teamScreen{canAgent: true, canTeam: true}
	ts.key(tk(term.KeyEnter)) // opens New agent
	typeForm(ts.form, "dup")
	ts.apply(teamOutcome{refused: `"dup" already exists; nothing was written.`})
	var st fold.State
	ts.publish(&st)
	if ts.form == nil || ts.form.value("Name") != "dup" {
		t.Fatal("the form must survive a refusal with what was typed")
	}
	if !strings.Contains(st.HubDetail, `✗ "dup" already exists`) {
		t.Fatalf("detail:\n%s", st.HubDetail)
	}
}

func TestACreatedFileClosesTheFormAndLandsOnIt(t *testing.T) {
	ts := &teamScreen{canAgent: true}
	ts.key(tk(term.KeyEnter))
	ts.apply(teamOutcome{created: "agent zed created", name: "zed", items: []teamItem{
		{Name: "alpha", Info: &driver.BlueprintInfo{}}, {Name: "zed", Info: &driver.BlueprintInfo{Members: []driver.BlueprintMember{{Name: "zed"}}}}}})
	if ts.form != nil || ts.sel != teamActions+1 || !strings.HasPrefix(ts.banner, "✓ agent zed created") {
		t.Fatalf("form=%v sel=%d banner=%q", ts.form, ts.sel, ts.banner)
	}
}

func TestNothingTheManagerSaysSendsThePersonToACommand(t *testing.T) {
	ts := &teamScreen{canAgent: true, canTeam: true}
	var st fold.State
	for _, s := range []*teamScreen{ts, {canAgent: true, canTeam: true, sel: rowNewTeam}, {loading: true}} {
		s.publish(&st)
		all := st.HubDetail + st.HubHint
		for _, r := range st.HubRows {
			all += r.Label + r.Status
		}
		if strings.Contains(all, "arxi ") {
			t.Errorf("the screen tells the person to use the command line:\n%s", all)
		}
	}
	ts.key(tk(term.KeyEnter))
	ts.publish(&st)
	if strings.Contains(st.HubDetail, "arxi ") {
		t.Errorf("form text mentions the CLI: %s", st.HubDetail)
	}
}

func TestStartTeamCreateWritesThroughTheCoreAndRereads(t *testing.T) {
	root := t.TempDir()
	writeAgents(t, root, "zed.yaml")
	core := &fakeTeamCore{infos: map[string]*driver.BlueprintInfo{"zed.yaml": {Members: []driver.BlueprintMember{{Name: "zed"}}}},
		models: []driver.ModelRow{{Provider: "p", ID: "on", Enabled: true}, {Provider: "p", ID: "off"}}}
	ch := make(chan teamOutcome, 1)
	startTeamCreate(context.Background(), core, root, teamTask{agent: &driver.AgentCreateParams{Name: "zed"}}, ch)
	out := <-ch
	if out.refused != "" || out.name != "zed" || len(out.items) != 1 || !strings.Contains(out.created, "agent zed created") {
		t.Fatalf("outcome = %+v", out)
	}
	if len(out.models) != 1 || out.models[0] != "p/on" {
		t.Errorf("only enabled models are offered: %v", out.models)
	}

	core.refuse = "no way"
	startTeamCreate(context.Background(), core, root, teamTask{team: &driver.BlueprintCreateParams{Name: "t", Members: []string{"zed"}}}, ch)
	if out := <-ch; out.refused != "no way" || out.items != nil {
		t.Fatalf("a refusal carries only the reason: %+v", out)
	}
}

// TestLoopCreatesAnAgentFromTheForm drives the real loop end to end with the keyboard
// alone: /team → New agent → type a name → Enter through the fields → created.
func TestLoopCreatesAnAgentFromTheForm(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatal(err)
	}
	core := teamHubCore{fakeTeamCore: &fakeTeamCore{
		models: []driver.ModelRow{{Provider: "p", ID: "m", Enabled: true}}}}
	drv := &teamTestDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}, core: core}

	at := func(ms int, ev term.Event) scheduledEvent {
		return scheduledEvent{time.Duration(ms) * time.Millisecond, ev}
	}
	key := func(k term.Key) term.Event { return term.Event{Kind: term.EventKey, Key: k} }
	script := []scheduledEvent{at(120, keyEvent('/'))}
	for _, r := range "team" {
		script = append(script, at(5, keyEvent(r)))
	}
	script = append(script, at(40, enterEvent()), at(150, enterEvent())) // open New agent
	for _, r := range "backend" {
		script = append(script, at(5, keyEvent(r)))
	}
	for i := 0; i < 8; i++ { // through Model, Role, five tools, Advisory
		script = append(script, at(5, key(tk(term.KeyDown))))
	}
	script = append(script, at(20, enterEvent()),
		at(300, key(tk(term.KeyEscape))),
		at(150, ctrlCharEvent('c')), at(50, ctrlCharEvent('c')))

	tty := newFakeTTY(110, 40, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	if len(core.fakeTeamCore.agents) != 1 || core.fakeTeamCore.agents[0].Name != "backend" {
		t.Fatalf("the core was asked to create %+v", core.fakeTeamCore.agents)
	}
	out := stripANSI(tty.output())
	for _, want := range []string{"New agent", "Tool · bash", "✓ agent backend created"} {
		if !strings.Contains(out, want) {
			t.Errorf("no frame showed %q", want)
		}
	}
	if len(drv.submitted) != 0 {
		t.Errorf("keys typed in the form reached the chat as %q", drv.submitted)
	}
}

// TestAgentsAndTeamsAreCreatedByTheRealCoreFromTheScreen creates an agent and a team
// through the real core, with the same calls the screen's worker makes, and reads
// them back the way the list does.
func TestAgentsAndTeamsAreCreatedByTheRealCoreFromTheScreen(t *testing.T) {
	bin := buildCore(t)
	work := t.TempDir()
	t.Setenv("ARXI_BIN", bin)
	t.Setenv("HOME", work)
	t.Setenv("USERPROFILE", work)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(work, "cfg"))
	t.Chdir(work)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	sd, _, err := openServeDriver(ctx, bin)
	if err != nil {
		t.Fatalf("openServeDriver: %v", err)
	}
	defer func() {
		if c, ok := sd.(interface{ Close() error }); ok {
			c.Close()
		}
	}()
	hc, _ := sd.(interface{ Hub() hubCore })
	drv, ok := hc.Hub().(teamCore)
	if !ok || !helloImplements(drv.Hello(), "blueprint.create") {
		t.Fatal("the real core must implement blueprint.create")
	}
	ch := make(chan teamOutcome, 1)

	for _, name := range []string{"backend", "reviewer"} {
		task := teamTask{agent: &driver.AgentCreateParams{Name: name, Tools: []string{"read"}}}
		startTeamCreate(ctx, drv, work, task, ch)
		if out := <-ch; out.refused != "" || out.name != name {
			t.Fatalf("create %s: %+v", name, out)
		}
	}
	// A second agent with the same name is the core's refusal, shown whole.
	startTeamCreate(ctx, drv, work, teamTask{agent: &driver.AgentCreateParams{Name: "backend"}}, ch)
	if out := <-ch; !strings.Contains(out.refused, "already exists") {
		t.Fatalf("duplicate: %+v", out)
	}
	startTeamCreate(ctx, drv, work, teamTask{team: &driver.BlueprintCreateParams{
		Name: "crew", Members: []string{"backend", "reviewer"}, Stages: []string{"build", "review"}}}, ch)
	out := <-ch
	if out.refused != "" || len(out.items) != 3 {
		t.Fatalf("team: %+v", out)
	}
	var crew teamItem
	for _, it := range out.items {
		if it.Name == "crew" {
			crew = it
		}
	}
	if crew.Info == nil || len(crew.Info.Members) != 2 || len(crew.Info.Stages) != 2 {
		t.Fatalf("crew as the core describes it: %+v", crew)
	}
	if _, err := os.Stat(filepath.Join(work, teamDir, "crew.yaml")); err != nil {
		t.Fatal(err)
	}
}
