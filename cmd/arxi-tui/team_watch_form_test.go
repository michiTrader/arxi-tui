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
	"github.com/michiTrader/arxi_tui/internal/term"
)

var oneRule = []driver.BlueprintWatcher{
	{Agent: "reviewer", Pattern: "stage.advanced", Action: "notify"},
	{Agent: "backend", Pattern: "run.quiescent", Action: "run_tool", Tool: "read"},
}

func TestTheWatchFormOpensOnTheRulesThereAreToday(t *testing.T) {
	f := newWatchForm("crew", twoMembers, oneRule)
	for _, want := range []string{
		"reviewer listens to stage.advanced: " + actNotify,
		"backend listens to run.quiescent: " + actTool + " (read)",
	} {
		if !strings.Contains(f.help, want) {
			t.Errorf("the form does not say %q:\n%s", want, f.help)
		}
	}
	if r := fieldNamed(t, f, watchRuleLabel); len(r.choices) != 3 || r.choices[0] != newWatch {
		t.Errorf("rules offered: %v", r.choices)
	}
	if strings.Contains(f.help, "arxi ") {
		t.Errorf("the form sends the person to the command line:\n%s", f.help)
	}
	if none := newWatchForm("crew", twoMembers, nil); !strings.Contains(none.help, "nothing wakes a member by itself") {
		t.Errorf("a team with no rules says nothing about it:\n%s", none.help)
	}
	// Choosing an existing rule loads it; going back to a new one empties the form.
	f.focus = 0
	f.flip(2)
	if fieldNamed(t, f, watchEventsLabel).value != "run.quiescent" ||
		fieldNamed(t, f, watchToolLabel).choices[fieldNamed(t, f, watchToolLabel).idx] != "read" ||
		fieldNamed(t, f, watchMemberLabel).choices[fieldNamed(t, f, watchMemberLabel).idx] != "backend" {
		t.Errorf("the rule was not loaded: %+v", f.fields)
	}
	f.flip(1) // wraps to "a new rule"
	if fieldNamed(t, f, watchEventsLabel).value != "" || fieldNamed(t, f, watchToolLabel).idx != 0 {
		t.Errorf("a new rule must start empty: %+v", f.fields)
	}
}

func TestTheWatchFormBuildsTheRuleTheCoreIsAskedFor(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*testing.T, *teamForm)
		check func(*testing.T, driver.BlueprintWatchParams)
	}{
		{"a new rule that wakes", func(t *testing.T, f *teamForm) {
			choose(t, f, watchMemberLabel, "reviewer")
			fieldNamed(t, f, watchEventsLabel).value = "stage.*"
		}, func(t *testing.T, p driver.BlueprintWatchParams) {
			if p.Agent != "reviewer" || p.Pattern != "stage.*" || p.Action != "" || p.Tool != "" || p.Remove {
				t.Errorf("%+v", p)
			}
		}},
		{"a new rule that tells", func(t *testing.T, f *teamForm) {
			fieldNamed(t, f, watchEventsLabel).value = "agent.failed"
			choose(t, f, watchActionLabel, actNotify)
		}, func(t *testing.T, p driver.BlueprintWatchParams) {
			if p.Action != "notify" || p.Pattern != "agent.failed" {
				t.Errorf("%+v", p)
			}
		}},
		{"a new rule that runs a tool", func(t *testing.T, f *teamForm) {
			fieldNamed(t, f, watchEventsLabel).value = "stage.entered"
			choose(t, f, watchActionLabel, actTool)
			choose(t, f, watchToolLabel, "grep")
		}, func(t *testing.T, p driver.BlueprintWatchParams) {
			if p.Action != "run_tool" || p.Tool != "grep" {
				t.Errorf("%+v", p)
			}
		}},
		{"a tool left over from a wake is not sent", func(t *testing.T, f *teamForm) {
			fieldNamed(t, f, watchEventsLabel).value = "stage.entered"
			choose(t, f, watchToolLabel, "grep")
		}, func(t *testing.T, p driver.BlueprintWatchParams) {
			if p.Action != "" || p.Tool != "" {
				t.Errorf("the core refuses a tool on a plain wake: %+v", p)
			}
		}},
		{"an existing rule given another action", func(t *testing.T, f *teamForm) {
			choose(t, f, watchRuleLabel, watchLine(oneRule[0]))
			f.loadWatch()
			choose(t, f, watchActionLabel, actWake)
		}, func(t *testing.T, p driver.BlueprintWatchParams) {
			if p.Agent != "reviewer" || p.Pattern != "stage.advanced" || p.Action != "" {
				t.Errorf("%+v", p)
			}
		}},
		{"removing an existing rule sends the pair and the flag only", func(t *testing.T, f *teamForm) {
			choose(t, f, watchRuleLabel, watchLine(oneRule[1]))
			f.loadWatch()
			fieldNamed(t, f, watchRemoveLabel).on = true
		}, func(t *testing.T, p driver.BlueprintWatchParams) {
			if !p.Remove || p.Agent != "backend" || p.Pattern != "run.quiescent" || p.Action != "" || p.Tool != "" {
				t.Errorf("%+v", p)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newWatchForm("crew", twoMembers, oneRule)
			c.setup(t, f)
			task, msg := f.submitWatch()
			if msg != "" || task == nil || task.watch == nil {
				t.Fatalf("task=%v msg=%q", task, msg)
			}
			if task.watch.Name != "crew" {
				t.Errorf("name = %q", task.watch.Name)
			}
			c.check(t, *task.watch)
		})
	}
}

func TestTheWatchFormRefusesWhatItCanTellItself(t *testing.T) {
	cases := []struct {
		name, want string
		setup      func(*testing.T, *teamForm)
	}{
		{"no events", "Events is required", func(*testing.T, *teamForm) {}},
		{"a tool run with no tool", "Tool is required", func(t *testing.T, f *teamForm) {
			fieldNamed(t, f, watchEventsLabel).value = "stage.*"
			choose(t, f, watchActionLabel, actTool)
		}},
		{"removing a rule that is not there", "no rule to remove", func(t *testing.T, f *teamForm) {
			fieldNamed(t, f, watchRemoveLabel).on = true
		}},
		{"an existing rule changed into another", "add a new rule", func(t *testing.T, f *teamForm) {
			choose(t, f, watchRuleLabel, watchLine(oneRule[0]))
			f.loadWatch()
			fieldNamed(t, f, watchEventsLabel).value = "stage.*"
		}},
		{"an existing rule left as it is", "nothing to change", func(t *testing.T, f *teamForm) {
			choose(t, f, watchRuleLabel, watchLine(oneRule[0]))
			f.loadWatch()
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newWatchForm("crew", twoMembers, oneRule)
			c.setup(t, f)
			if task, msg := f.submitWatch(); task != nil || !strings.Contains(msg, c.want) {
				t.Fatalf("task=%v msg=%q, want a message containing %q", task, msg, c.want)
			}
		})
	}
}

func screenWithRules(can bool) *teamScreen {
	ts := &teamScreen{canWatch: can, items: []teamItem{
		{Name: "crew", Info: &driver.BlueprintInfo{Members: twoMembers, Watchers: oneRule}},
	}}
	ts.sel = teamActions
	return ts
}

func wKey() term.Key { return term.Key{Type: term.KeyRunes, Runes: []rune{'w'}} }

func TestWOnATeamOpensTheWatchFormAndSavingAsksTheCore(t *testing.T) {
	ts := screenWithRules(true)
	if closeIt, _ := ts.key(wKey()); closeIt || ts.form == nil || ts.form.kind != formEditWatch {
		t.Fatalf("form = %+v", ts.form)
	}
	fieldNamed(t, ts.form, watchEventsLabel).value = "stage.*"
	ts.form.focus = len(ts.form.fields) - 1
	_, task := ts.key(tk(term.KeyEnter))
	if task == nil || task.watch == nil || task.watch.Name != "crew" || task.watch.Pattern != "stage.*" {
		t.Fatalf("task = %+v", task)
	}
	if !strings.Contains(ts.working, "Saving the watchers of crew") {
		t.Errorf("working = %q", ts.working)
	}
}

func TestWExplainsWhyItCannotOpenTheForm(t *testing.T) {
	cases := []struct {
		name string
		ts   *teamScreen
		want string
	}{
		{"an old core", screenWithRules(false), "cannot create files yet"},
		{"a file the core refuses", &teamScreen{canWatch: true, sel: teamActions,
			items: []teamItem{{Name: "bad", Err: "nope"}}}, "refuses this file"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.ts.key(wKey())
			if c.ts.form != nil || !strings.Contains(c.ts.banner, c.want) {
				t.Errorf("form=%v banner=%q", c.ts.form, c.ts.banner)
			}
		})
	}
	ts := screenWithRules(true)
	ts.sel = rowNewAgent
	ts.key(wKey())
	if ts.form != nil || ts.banner != "" {
		t.Errorf("w on an action row: form=%v banner=%q", ts.form, ts.banner)
	}
}

func TestTheRowSaysHowToEditItsWatchers(t *testing.T) {
	var st fold.State
	ts := screenWithRules(true)
	ts.publish(&st)
	if !strings.Contains(st.HubDetail, "w to change what wakes a member") || !strings.Contains(st.HubHint, "w watchers") {
		t.Errorf("detail=%q hint=%q", st.HubDetail, st.HubHint)
	}
}

func TestStartTeamCreateSavesAWatcherAndRereads(t *testing.T) {
	root := t.TempDir()
	writeAgents(t, root, "crew.yaml")
	core := &fakeTeamCore{infos: map[string]*driver.BlueprintInfo{"crew.yaml": {Members: twoMembers}}}
	ch := make(chan teamOutcome, 1)
	startTeamCreate(context.Background(), core, root,
		teamTask{watch: &driver.BlueprintWatchParams{Name: "crew", Agent: "reviewer", Pattern: "stage.*"}}, ch)
	out := <-ch
	if out.refused != "" || out.name != "crew" || len(out.items) != 1 || !strings.Contains(out.created, "watcher of reviewer on stage.* saved in crew") {
		t.Fatalf("outcome = %+v", out)
	}
	if len(core.watches) != 1 {
		t.Errorf("the core was asked %+v", core.watches)
	}
	startTeamCreate(context.Background(), core, root,
		teamTask{watch: &driver.BlueprintWatchParams{Name: "crew", Agent: "reviewer", Pattern: "stage.*", Remove: true}}, ch)
	if out := <-ch; !strings.Contains(out.created, "removed from crew") {
		t.Errorf("removal said %q", out.created)
	}
	core.refuse = "agent \"ghost\" is not declared in members"
	startTeamCreate(context.Background(), core, root,
		teamTask{watch: &driver.BlueprintWatchParams{Name: "crew", Agent: "ghost", Pattern: "stage.*"}}, ch)
	if out := <-ch; !strings.Contains(out.refused, "ghost") || out.items != nil {
		t.Fatalf("a refusal carries only the reason: %+v", out)
	}
}

// TestAWatcherIsChangedByTheRealCoreFromTheScreen takes the screen's path with the
// real core: compose a team, add, change and remove a watcher through the worker,
// read it back the way the list does, and see a refusal come back whole.
func TestAWatcherIsChangedByTheRealCoreFromTheScreen(t *testing.T) {
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
	if !ok || !helloImplements(drv.Hello(), "blueprint.watch") {
		t.Fatal("the real core must implement blueprint.watch")
	}
	ch := make(chan teamOutcome, 1)
	for _, n := range []string{"backend", "reviewer"} {
		startTeamCreate(ctx, drv, work, teamTask{agent: &driver.AgentCreateParams{Name: n, Tools: []string{"read"}}}, ch)
		if out := <-ch; out.refused != "" {
			t.Fatalf("create %s: %+v", n, out)
		}
	}
	startTeamCreate(ctx, drv, work, teamTask{team: &driver.BlueprintCreateParams{
		Name: "crew", Members: []string{"backend", "reviewer"}, Stages: []string{"build", "review"}}}, ch)
	out := <-ch
	if out.refused != "" {
		t.Fatalf("team: %+v", out)
	}
	infoOf := func(out teamOutcome) *driver.BlueprintInfo {
		for _, it := range out.items {
			if it.Name == "crew" {
				return it.Info
			}
		}
		t.Fatalf("crew is not listed: %+v", out.items)
		return nil
	}
	info := infoOf(out)

	// A new rule, through the form as a person would fill it in.
	f := newWatchForm("crew", info.Members, info.Watchers)
	choose(t, f, watchMemberLabel, "reviewer")
	fieldNamed(t, f, watchEventsLabel).value = "stage.advanced"
	choose(t, f, watchActionLabel, actNotify)
	task, msg := f.submitWatch()
	if msg != "" {
		t.Fatal(msg)
	}
	startTeamCreate(ctx, drv, work, *task, ch)
	out = <-ch
	if out.refused != "" {
		t.Fatalf("the core refused: %+v", out)
	}
	info = infoOf(out)
	if len(info.Watchers) != 1 || info.Watchers[0].Agent != "reviewer" || info.Watchers[0].Action != "notify" {
		t.Fatalf("the list now shows %+v", info.Watchers)
	}

	// The same rule given a tool to run replaces it.
	f = newWatchForm("crew", info.Members, info.Watchers)
	choose(t, f, watchRuleLabel, watchLine(info.Watchers[0]))
	f.loadWatch()
	choose(t, f, watchActionLabel, actTool)
	choose(t, f, watchToolLabel, "read")
	task, msg = f.submitWatch()
	if msg != "" {
		t.Fatal(msg)
	}
	startTeamCreate(ctx, drv, work, *task, ch)
	out = <-ch
	if out.refused != "" {
		t.Fatalf("the core refused: %+v", out)
	}
	info = infoOf(out)
	if len(info.Watchers) != 1 || info.Watchers[0].Action != "run_tool" || info.Watchers[0].Tool != "read" {
		t.Fatalf("after replacing the list shows %+v", info.Watchers)
	}
	if b, _ := os.ReadFile(filepath.Join(work, teamDir, "crew.yaml")); !strings.Contains(string(b), "run_tool") {
		t.Errorf("the file does not hold the change:\n%s", b)
	}

	// A member that is not in the team is the core's own refusal, shown whole.
	startTeamCreate(ctx, drv, work, teamTask{watch: &driver.BlueprintWatchParams{
		Name: "crew", Agent: "ghost", Pattern: "stage.*"}}, ch)
	if out := <-ch; !strings.Contains(out.refused, "ghost") {
		t.Fatalf("a watcher on nobody: %+v", out)
	}

	// And the rule goes away.
	f = newWatchForm("crew", info.Members, info.Watchers)
	choose(t, f, watchRuleLabel, watchLine(info.Watchers[0]))
	f.loadWatch()
	fieldNamed(t, f, watchRemoveLabel).on = true
	task, msg = f.submitWatch()
	if msg != "" {
		t.Fatal(msg)
	}
	startTeamCreate(ctx, drv, work, *task, ch)
	out = <-ch
	if out.refused != "" {
		t.Fatalf("the core refused: %+v", out)
	}
	if info = infoOf(out); len(info.Watchers) != 0 {
		t.Fatalf("after removing the list shows %+v", info.Watchers)
	}
}
