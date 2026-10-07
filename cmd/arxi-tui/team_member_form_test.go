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

var twoMembers = []driver.BlueprintMember{
	{Name: "backend", Role: "implementer", Model: "p/big", Tools: []string{"read", "write"}},
	{Name: "reviewer", Tools: []string{"read"}, Advisory: true},
}

func TestTheMemberFormOpensOnWhatTheMemberIsToday(t *testing.T) {
	f := newMemberForm("crew", twoMembers, []string{"p/small"})
	for _, want := range []string{"backend — model: p/big; role: implementer; tools: read, write",
		"reviewer — model: none of its own (the run supplies one); role: none; tools: read; advisory"} {
		if !strings.Contains(f.help, want) {
			t.Errorf("the form does not say %q:\n%s", want, f.help)
		}
	}
	// A model the member has but is not enabled is still offered, and chosen.
	if m := fieldNamed(t, f, modelLabel); m.choices[m.idx] != "p/big" {
		t.Errorf("model shows %q out of %v", m.choices[m.idx], m.choices)
	}
	if fieldNamed(t, f, roleLabel).value != "implementer" || !fieldNamed(t, f, toolPrefix+"write").on ||
		fieldNamed(t, f, toolPrefix+"bash").on || fieldNamed(t, f, advisoryLabel).on {
		t.Errorf("fields do not match backend: %+v", f.fields)
	}
	if strings.Contains(f.help, "arxi ") {
		t.Errorf("the form sends the person to the command line:\n%s", f.help)
	}
	// Choosing another member loads that member.
	f.focus = 0
	f.flip(1)
	if m := fieldNamed(t, f, modelLabel); m.idx != 0 || !fieldNamed(t, f, advisoryLabel).on ||
		fieldNamed(t, f, roleLabel).value != "" || fieldNamed(t, f, toolPrefix+"write").on {
		t.Errorf("the fields did not follow the member: %+v", f.fields)
	}
}

func TestTheMemberFormAsksOnlyForWhatWasChanged(t *testing.T) {
	str := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	cases := []struct {
		name  string
		setup func(*testing.T, *teamForm)
		check func(*testing.T, driver.BlueprintMemberParams)
	}{
		{"another model", func(t *testing.T, f *teamForm) { choose(t, f, modelLabel, "p/small") },
			func(t *testing.T, p driver.BlueprintMemberParams) {
				if str(p.Model) != "p/small" || p.Role != nil || p.Tools != nil || p.Advisory != nil {
					t.Errorf("%+v", p)
				}
			}},
		{"no model of its own", func(t *testing.T, f *teamForm) { choose(t, f, modelLabel, noModel) },
			func(t *testing.T, p driver.BlueprintMemberParams) {
				if p.Model == nil || *p.Model != "" {
					t.Errorf("removing the model must reach the core as an empty string: %v", str(p.Model))
				}
			}},
		{"an emptied role", func(t *testing.T, f *teamForm) { fieldNamed(t, f, roleLabel).value = "" },
			func(t *testing.T, p driver.BlueprintMemberParams) {
				if p.Role == nil || *p.Role != "" || p.Model != nil {
					t.Errorf("%+v", p)
				}
			}},
		{"one more tool", func(t *testing.T, f *teamForm) { fieldNamed(t, f, toolPrefix+"grep").on = true },
			func(t *testing.T, p driver.BlueprintMemberParams) {
				if p.Tools == nil || strings.Join(*p.Tools, ",") != "grep,read,write" {
					t.Errorf("%+v", p.Tools)
				}
			}},
		{"every tool taken away", func(t *testing.T, f *teamForm) {
			fieldNamed(t, f, toolPrefix+"read").on = false
			fieldNamed(t, f, toolPrefix+"write").on = false
		}, func(t *testing.T, p driver.BlueprintMemberParams) {
			if p.Tools == nil || len(*p.Tools) != 0 {
				t.Errorf("no tools must reach the core as an empty list, not as nothing: %v", p.Tools)
			}
		}},
		{"advisory on", func(t *testing.T, f *teamForm) { fieldNamed(t, f, advisoryLabel).on = true },
			func(t *testing.T, p driver.BlueprintMemberParams) {
				if p.Advisory == nil || !*p.Advisory || p.Tools != nil {
					t.Errorf("%+v", p)
				}
			}},
		{"the other member", func(t *testing.T, f *teamForm) {
			choose(t, f, memberLabel, "reviewer")
			f.loadMember()
			fieldNamed(t, f, advisoryLabel).on = false
		}, func(t *testing.T, p driver.BlueprintMemberParams) {
			if p.Member != "reviewer" || p.Name != "crew" || p.Advisory == nil || *p.Advisory {
				t.Errorf("%+v", p)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newMemberForm("crew", twoMembers, []string{"p/small"})
			c.setup(t, f)
			task, msg := f.submitMember()
			if msg != "" || task == nil || task.member == nil {
				t.Fatalf("task=%v msg=%q", task, msg)
			}
			c.check(t, *task.member)
		})
	}
}

func TestTheMemberFormRefusesToSaveNothing(t *testing.T) {
	f := newMemberForm("crew", twoMembers, nil)
	if task, msg := f.submitMember(); task != nil || !strings.Contains(msg, "nothing to change") {
		t.Fatalf("task=%v msg=%q", task, msg)
	}
}

func screenWithMembers(can bool) *teamScreen {
	ts := &teamScreen{canMember: can, items: []teamItem{
		{Name: "crew", Info: &driver.BlueprintInfo{Members: twoMembers}},
	}}
	ts.sel = teamActions
	return ts
}

func mKey() term.Key { return term.Key{Type: term.KeyRunes, Runes: []rune{'m'}} }

func TestMOnATeamOpensTheMemberFormAndSavingAsksTheCore(t *testing.T) {
	ts := screenWithMembers(true)
	if closeIt, _ := ts.key(mKey()); closeIt || ts.form == nil || ts.form.kind != formEditMember {
		t.Fatalf("form = %+v", ts.form)
	}
	fieldNamed(t, ts.form, advisoryLabel).on = true
	ts.form.focus = len(ts.form.fields) - 1
	_, task := ts.key(tk(term.KeyEnter))
	if task == nil || task.member == nil || task.member.Member != "backend" || task.member.Advisory == nil {
		t.Fatalf("task = %+v", task)
	}
	if !strings.Contains(ts.working, "Saving backend in crew") {
		t.Errorf("working = %q", ts.working)
	}
}

func TestMExplainsWhyItCannotOpenTheForm(t *testing.T) {
	cases := []struct {
		name string
		ts   *teamScreen
		want string
	}{
		{"an old core", screenWithMembers(false), "cannot create files yet"},
		{"a file the core refuses", &teamScreen{canMember: true, sel: teamActions,
			items: []teamItem{{Name: "bad", Err: "nope"}}}, "refuses this file"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.ts.key(mKey())
			if c.ts.form != nil || !strings.Contains(c.ts.banner, c.want) {
				t.Errorf("form=%v banner=%q", c.ts.form, c.ts.banner)
			}
		})
	}
	ts := screenWithMembers(true)
	ts.sel = rowNewAgent
	ts.key(mKey())
	if ts.form != nil || ts.banner != "" {
		t.Errorf("m on an action row: form=%v banner=%q", ts.form, ts.banner)
	}
}

func TestTheRowSaysHowToEditItsMembers(t *testing.T) {
	var st fold.State
	ts := screenWithMembers(true)
	ts.publish(&st)
	if !strings.Contains(st.HubDetail, "Press m to change") || !strings.Contains(st.HubHint, "m edit a member") {
		t.Errorf("detail=%q hint=%q", st.HubDetail, st.HubHint)
	}
}

func TestStartTeamCreateSavesAMemberAndRereads(t *testing.T) {
	root := t.TempDir()
	writeAgents(t, root, "crew.yaml")
	core := &fakeTeamCore{infos: map[string]*driver.BlueprintInfo{"crew.yaml": {Members: twoMembers}}}
	ch := make(chan teamOutcome, 1)
	yes := true
	startTeamCreate(context.Background(), core, root,
		teamTask{member: &driver.BlueprintMemberParams{Name: "crew", Member: "reviewer", Advisory: &yes}}, ch)
	out := <-ch
	if out.refused != "" || out.name != "crew" || len(out.items) != 1 || !strings.Contains(out.created, "member reviewer of crew saved") {
		t.Fatalf("outcome = %+v", out)
	}
	if len(core.members) != 1 || core.members[0].Advisory == nil {
		t.Errorf("the core was asked %+v", core.members)
	}
	core.refuse = "tool \"nuke\" is not known"
	startTeamCreate(context.Background(), core, root,
		teamTask{member: &driver.BlueprintMemberParams{Name: "crew", Member: "reviewer", Advisory: &yes}}, ch)
	if out := <-ch; !strings.Contains(out.refused, "nuke") || out.items != nil {
		t.Fatalf("a refusal carries only the reason: %+v", out)
	}
}

// TestAMemberIsChangedByTheRealCoreFromTheScreen takes the screen's path with the
// real core: compose a team, change a member through the worker, read it back the
// way the list does, and see a refusal come back whole.
func TestAMemberIsChangedByTheRealCoreFromTheScreen(t *testing.T) {
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
	if !ok || !helloImplements(drv.Hello(), "blueprint.member") {
		t.Fatal("the real core must implement blueprint.member")
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
	var members []driver.BlueprintMember
	for _, it := range out.items {
		if it.Name == "crew" {
			members = it.Info.Members
		}
	}
	if len(members) != 2 {
		t.Fatalf("the list shows %+v", members)
	}

	// Through the form, as a person would fill it in.
	f := newMemberForm("crew", members, nil)
	choose(t, f, memberLabel, "reviewer")
	f.loadMember()
	fieldNamed(t, f, roleLabel).value = "checker"
	fieldNamed(t, f, toolPrefix+"grep").on = true
	fieldNamed(t, f, advisoryLabel).on = true
	task, msg := f.submitMember()
	if msg != "" {
		t.Fatal(msg)
	}
	startTeamCreate(ctx, drv, work, *task, ch)
	out = <-ch
	if out.refused != "" {
		t.Fatalf("the core refused: %+v", out)
	}
	var got driver.BlueprintMember
	for _, it := range out.items {
		if it.Name == "crew" {
			for _, m := range it.Info.Members {
				if m.Name == "reviewer" {
					got = m
				}
			}
		}
	}
	if got.Role != "checker" || !got.Advisory || strings.Join(got.Tools, ",") != "grep,read" {
		t.Fatalf("the list now shows %+v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(work, teamDir, "crew.yaml")); !strings.Contains(string(b), "checker") {
		t.Errorf("the file does not hold the change:\n%s", b)
	}

	// A tool the core does not know is its own refusal, shown whole.
	bad := []string{"nuke"}
	startTeamCreate(ctx, drv, work, teamTask{member: &driver.BlueprintMemberParams{
		Name: "crew", Member: "backend", Tools: &bad}}, ch)
	if out := <-ch; !strings.Contains(out.refused, "nuke") {
		t.Fatalf("an unknown tool: %+v", out)
	}
}
