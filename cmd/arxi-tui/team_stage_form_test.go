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

var twoStages = []driver.BlueprintStage{
	{Name: "build", AdvanceWhen: "all", OnTimeout: "escalate"},
	{Name: "review", AdvanceWhen: "quorum:2", OnTimeout: "advance", TimeoutMs: 600000},
}

// fieldNamed finds a field of the form by its label.
func fieldNamed(t *testing.T, f *teamForm, label string) *teamField {
	t.Helper()
	for i := range f.fields {
		if f.fields[i].label == label {
			return &f.fields[i]
		}
	}
	t.Fatalf("no field %q", label)
	return nil
}

func choose(t *testing.T, f *teamForm, label, answer string) {
	t.Helper()
	fl := fieldNamed(t, f, label)
	for i, c := range fl.choices {
		if c == answer {
			fl.idx = i
			return
		}
	}
	t.Fatalf("field %q has no answer %q (has %v)", label, answer, fl.choices)
}

func TestTheStagesFormOpensWithWhatTheTeamDoesToday(t *testing.T) {
	f := newStagesForm("crew", twoStages)
	for _, want := range []string{"1. build — everyone must finish; no time limit",
		"2. review — 2 of them must finish; after 10 min: move on to the next stage"} {
		if !strings.Contains(f.help, want) {
			t.Errorf("the form does not say %q:\n%s", want, f.help)
		}
	}
	if st := fieldNamed(t, f, "Stage"); len(st.choices) != 2 || st.choices[1] != "review" {
		t.Errorf("the stages offered: %v", st.choices)
	}
	if strings.Contains(f.help, "arxi ") {
		t.Errorf("the form sends the person to the command line:\n%s", f.help)
	}
}

func TestTheStagesFormAsksOnlyForWhatWasChanged(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*testing.T, *teamForm)
		check func(*testing.T, driver.BlueprintStageParams)
	}{
		{"the first to finish", func(t *testing.T, f *teamForm) { choose(t, f, "Finished when", ruleAny) },
			func(t *testing.T, p driver.BlueprintStageParams) {
				if p.AdvanceWhen != "any" || p.TimeoutMs != nil || p.OnTimeout != "" {
					t.Errorf("%+v", p)
				}
			}},
		{"a number of them", func(t *testing.T, f *teamForm) {
			choose(t, f, "Finished when", ruleQuorum)
			fieldNamed(t, f, "How many").value = "2"
		}, func(t *testing.T, p driver.BlueprintStageParams) {
			if p.AdvanceWhen != "quorum:2" {
				t.Errorf("%+v", p)
			}
		}},
		{"a limit in minutes", func(t *testing.T, f *teamForm) { fieldNamed(t, f, stageLimitLabel).value = "10" },
			func(t *testing.T, p driver.BlueprintStageParams) {
				if p.TimeoutMs == nil || *p.TimeoutMs != 600000 || p.AdvanceWhen != "" {
					t.Errorf("%+v", p)
				}
			}},
		{"half a minute, with a comma", func(t *testing.T, f *teamForm) { fieldNamed(t, f, stageLimitLabel).value = "0,5" },
			func(t *testing.T, p driver.BlueprintStageParams) {
				if p.TimeoutMs == nil || *p.TimeoutMs != 30000 {
					t.Errorf("%+v", p.TimeoutMs)
				}
			}},
		{"no limit at all", func(t *testing.T, f *teamForm) { fieldNamed(t, f, stageLimitLabel).value = "0" },
			func(t *testing.T, p driver.BlueprintStageParams) {
				if p.TimeoutMs == nil || *p.TimeoutMs != 0 {
					t.Errorf("0 must reach the core as 0, not as nothing: %v", p.TimeoutMs)
				}
			}},
		{"what happens at the limit", func(t *testing.T, f *teamForm) { choose(t, f, "When time runs out", onTimeoutStop) },
			func(t *testing.T, p driver.BlueprintStageParams) {
				if p.OnTimeout != "fail" || p.AdvanceWhen != "" {
					t.Errorf("%+v", p)
				}
			}},
		{"another stage", func(t *testing.T, f *teamForm) {
			choose(t, f, "Stage", "review")
			choose(t, f, "Finished when", ruleAll)
		}, func(t *testing.T, p driver.BlueprintStageParams) {
			if p.Stage != "review" || p.Name != "crew" || p.AdvanceWhen != "all" {
				t.Errorf("%+v", p)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newStagesForm("crew", twoStages)
			c.setup(t, f)
			task, msg := f.submitStages()
			if msg != "" || task == nil || task.stage == nil {
				t.Fatalf("task=%v msg=%q", task, msg)
			}
			c.check(t, *task.stage)
		})
	}
}

func TestTheStagesFormRefusesWhatItCanTellItself(t *testing.T) {
	cases := []struct {
		name, want string
		setup      func(*testing.T, *teamForm)
	}{
		{"nothing chosen", "nothing to change", func(*testing.T, *teamForm) {}},
		{"a number of them with no number", "How many", func(t *testing.T, f *teamForm) { choose(t, f, "Finished when", ruleQuorum) }},
		{"a number of them with zero", "How many", func(t *testing.T, f *teamForm) {
			choose(t, f, "Finished when", ruleQuorum)
			fieldNamed(t, f, "How many").value = "0"
		}},
		{"a limit that is not a number", "minutes", func(t *testing.T, f *teamForm) { fieldNamed(t, f, stageLimitLabel).value = "soon" }},
		{"a negative limit", "minutes", func(t *testing.T, f *teamForm) { fieldNamed(t, f, stageLimitLabel).value = "-3" }},
		{"a limit too small to exist", "too small", func(t *testing.T, f *teamForm) { fieldNamed(t, f, stageLimitLabel).value = "0.0000001" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newStagesForm("crew", twoStages)
			c.setup(t, f)
			if task, msg := f.submitStages(); task != nil || !strings.Contains(msg, c.want) {
				t.Fatalf("task=%v msg=%q, want a message containing %q", task, msg, c.want)
			}
		})
	}
}

func screenWithTeam(stages []driver.BlueprintStage, can bool) *teamScreen {
	ts := &teamScreen{canStage: can, items: []teamItem{
		{Name: "crew", Info: &driver.BlueprintInfo{Stages: stages, Members: []driver.BlueprintMember{{Name: "a"}, {Name: "b"}}}},
	}}
	ts.sel = teamActions
	return ts
}

func eKey() term.Key { return term.Key{Type: term.KeyRunes, Runes: []rune{'e'}} }

func TestEOnATeamOpensTheStagesFormAndSavingAsksTheCore(t *testing.T) {
	ts := screenWithTeam(twoStages, true)
	if closeIt, _ := ts.key(eKey()); closeIt || ts.form == nil || ts.form.kind != formEditStages {
		t.Fatalf("form = %+v", ts.form)
	}
	choose(t, ts.form, "Finished when", ruleAny)
	ts.form.focus = len(ts.form.fields) - 1
	_, task := ts.key(tk(term.KeyEnter))
	if task == nil || task.stage == nil || task.stage.Name != "crew" || task.stage.AdvanceWhen != "any" {
		t.Fatalf("task = %+v", task)
	}
	if !strings.Contains(ts.working, "Saving the rules") {
		t.Errorf("working = %q", ts.working)
	}
}

func TestEExplainsWhyItCannotOpenTheForm(t *testing.T) {
	cases := []struct {
		name string
		ts   *teamScreen
		want string
	}{
		{"an agent has no stages", screenWithTeam(nil, true), "no stages to edit"},
		{"an old core", screenWithTeam(twoStages, false), "cannot create files yet"},
		{"a file the core refuses", &teamScreen{canStage: true, sel: teamActions,
			items: []teamItem{{Name: "bad", Err: "nope"}}}, "refuses this file"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.ts.key(eKey())
			if c.ts.form != nil || !strings.Contains(c.ts.banner, c.want) {
				t.Errorf("form=%v banner=%q", c.ts.form, c.ts.banner)
			}
		})
	}
	// On the action rows E does nothing at all.
	ts := screenWithTeam(twoStages, true)
	ts.sel = rowNewAgent
	ts.key(eKey())
	if ts.form != nil || ts.banner != "" {
		t.Errorf("e on an action row: form=%v banner=%q", ts.form, ts.banner)
	}
}

func TestTheTeamRowSaysHowToEditItsStages(t *testing.T) {
	var st fold.State
	ts := screenWithTeam(twoStages, true)
	ts.publish(&st)
	if !strings.Contains(st.HubDetail, "Press e to change") || !strings.Contains(st.HubHint, "e edit stages") {
		t.Errorf("detail=%q hint=%q", st.HubDetail, st.HubHint)
	}
	ag := screenWithTeam(nil, true)
	ag.publish(&st)
	if strings.Contains(st.HubDetail, "Press e to change") {
		t.Errorf("an agent is offered stages to edit: %q", st.HubDetail)
	}
}

func TestStartTeamCreateSavesAStageAndRereads(t *testing.T) {
	root := t.TempDir()
	writeAgents(t, root, "crew.yaml")
	core := &fakeTeamCore{infos: map[string]*driver.BlueprintInfo{"crew.yaml": {Stages: twoStages}}}
	ch := make(chan teamOutcome, 1)
	zero := int64(0)
	startTeamCreate(context.Background(), core, root,
		teamTask{stage: &driver.BlueprintStageParams{Name: "crew", Stage: "review", TimeoutMs: &zero}}, ch)
	out := <-ch
	if out.refused != "" || out.name != "crew" || len(out.items) != 1 || !strings.Contains(out.created, "stage review of crew saved") {
		t.Fatalf("outcome = %+v", out)
	}
	if len(core.stages) != 1 || core.stages[0].TimeoutMs == nil {
		t.Errorf("the core was asked %+v", core.stages)
	}

	core.refuse = "quorum:5 but the blueprint declares 2 members"
	startTeamCreate(context.Background(), core, root, teamTask{stage: &driver.BlueprintStageParams{Name: "crew", Stage: "review", AdvanceWhen: "quorum:5"}}, ch)
	if out := <-ch; !strings.Contains(out.refused, "quorum") || out.items != nil {
		t.Fatalf("a refusal carries only the reason: %+v", out)
	}
}

// TestTheStagesAreChangedByTheRealCoreFromTheScreen takes the screen's path with the
// real core: compose a team, change a stage through the worker, read it back the way
// the list does, and see the refusal of a rule the team cannot satisfy.
func TestTheStagesAreChangedByTheRealCoreFromTheScreen(t *testing.T) {
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
	if !ok || !helloImplements(drv.Hello(), "blueprint.stage") {
		t.Fatal("the real core must implement blueprint.stage")
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
	if out := <-ch; out.refused != "" {
		t.Fatalf("team: %+v", out)
	}

	// Through the form, as a person would fill it in.
	f := newStagesForm("crew", twoStages)
	choose(t, f, "Stage", "review")
	choose(t, f, "Finished when", ruleQuorum)
	fieldNamed(t, f, "How many").value = "2"
	fieldNamed(t, f, stageLimitLabel).value = "10"
	choose(t, f, "When time runs out", onTimeoutAsk)
	task, msg := f.submitStages()
	if msg != "" {
		t.Fatal(msg)
	}
	startTeamCreate(ctx, drv, work, *task, ch)
	out := <-ch
	if out.refused != "" {
		t.Fatalf("the core refused: %+v", out)
	}
	var review driver.BlueprintStage
	for _, it := range out.items {
		if it.Name == "crew" {
			for _, s := range it.Info.Stages {
				if s.Name == "review" {
					review = s
				}
			}
		}
	}
	if review.AdvanceWhen != "quorum:2" || review.TimeoutMs != 600000 || review.OnTimeout != "escalate" {
		t.Fatalf("the list now shows %+v", review)
	}
	if b, _ := os.ReadFile(filepath.Join(work, teamDir, "crew.yaml")); !strings.Contains(string(b), "quorum:2") {
		t.Errorf("the file does not hold the rule:\n%s", b)
	}

	// A rule the team cannot satisfy is the core's refusal, shown whole.
	f = newStagesForm("crew", twoStages)
	choose(t, f, "Finished when", ruleQuorum)
	fieldNamed(t, f, "How many").value = "9"
	task, _ = f.submitStages()
	startTeamCreate(ctx, drv, work, *task, ch)
	if out := <-ch; !strings.Contains(out.refused, "quorum") {
		t.Fatalf("a quorum of 9 in a team of 2: %+v", out)
	}
}
