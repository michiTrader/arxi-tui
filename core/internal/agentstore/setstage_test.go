package agentstore

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/michiTrader/arxi/internal/blueprint"
	"github.com/michiTrader/arxi/internal/kernel"
)

func i64(n int64) *int64 { return &n }

func teamStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateTeam(Team{Name: "duo", Stages: []string{"build", "review"},
		Members: []kernel.MemberConfig{{Name: "a"}, {Name: "b"}}}); err != nil {
		t.Fatal(err)
	}
	return st
}

func stageOf(t *testing.T, st *Store, team, stage string) kernel.StageConfig {
	t.Helper()
	bp, err := st.Load(team)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range bp.Config.Stages {
		if s.Name == stage {
			return s
		}
	}
	t.Fatalf("no stage %q", stage)
	return kernel.StageConfig{}
}

func TestSetStageChangesOnlyTheNamedStageAndOnlyTheNamedFields(t *testing.T) {
	st := teamStore(t)
	if err := st.SetStage("duo", StageEdit{Stage: "review", AdvanceWhen: "quorum:2", TimeoutMs: i64(60000)}); err != nil {
		t.Fatal(err)
	}
	r := stageOf(t, st, "duo", "review")
	if r.AdvanceWhen != "quorum:2" || r.TimeoutMs != 60000 || r.OnTimeout != "escalate" {
		t.Errorf("review = %+v", r)
	}
	if b := stageOf(t, st, "duo", "build"); b.AdvanceWhen != "all" || b.TimeoutMs != 0 {
		t.Errorf("an untouched stage changed: %+v", b)
	}

	// A second edit that names one field keeps the others.
	if err := st.SetStage("duo", StageEdit{Stage: "review", OnTimeout: "advance"}); err != nil {
		t.Fatal(err)
	}
	r = stageOf(t, st, "duo", "review")
	if r.AdvanceWhen != "quorum:2" || r.TimeoutMs != 60000 || r.OnTimeout != "advance" {
		t.Errorf("an edit naming one field clobbered the rest: %+v", r)
	}

	// Zero removes the timeout; nil would have left it.
	if err := st.SetStage("duo", StageEdit{Stage: "review", TimeoutMs: i64(0)}); err != nil {
		t.Fatal(err)
	}
	if r = stageOf(t, st, "duo", "review"); r.TimeoutMs != 0 || r.AdvanceWhen != "quorum:2" {
		t.Errorf("removing the timeout: %+v", r)
	}
}

func TestSetStageKeepsEverythingOutsideTheStagesBlock(t *testing.T) {
	dir := t.TempDir()
	st, _ := Open(dir)
	src := "# my notes stay\nname: hand\n\nmembers:\n  - name: a\n  - name: b\n    role: reviewer\n\nstages:\n  - name: one\n    advance_when: any\n    workspace: shared\n  - {name: two, advance_when: all}\n\nwatchers:\n  - {agent: a, pattern: \"stage.*\"}\n# trailing note\n"
	if err := os.WriteFile(st.Path("hand"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.SetStage("hand", StageEdit{Stage: "two", TimeoutMs: i64(5000)}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(st.Path("hand"))
	for _, keep := range []string{"# my notes stay\nname: hand\n\nmembers:\n  - name: a\n  - name: b\n    role: reviewer\n\nstages:\n",
		"\nwatchers:\n  - {agent: a, pattern: \"stage.*\"}\n# trailing note\n"} {
		if !strings.Contains(string(got), keep) {
			t.Errorf("lost %q in:\n%s", keep, got)
		}
	}
	if one := stageOf(t, st, "hand", "one"); one.AdvanceWhen != "any" || one.Workspace != "shared" {
		t.Errorf("a field of an untouched stage was lost: %+v", one)
	}
	if two := stageOf(t, st, "hand", "two"); two.TimeoutMs != 5000 {
		t.Errorf("two = %+v", two)
	}
	bp, _ := st.Load("hand")
	if len(bp.Config.Watchers) != 1 {
		t.Errorf("watchers lost: %+v", bp.Config.Watchers)
	}
}

func TestSetStageRefusesWhatWouldBreakTheFileAndLeavesItAlone(t *testing.T) {
	st := teamStore(t)
	before, _ := os.ReadFile(st.Path("duo"))

	cases := []struct {
		name string
		e    StageEdit
		want string
	}{
		{"a quorum bigger than the team", StageEdit{Stage: "build", AdvanceWhen: "quorum:5"}, "quorum"},
		{"a rule that does not exist", StageEdit{Stage: "build", AdvanceWhen: "sometimes"}, "not a rule"},
		{"an unknown timeout behaviour", StageEdit{Stage: "build", OnTimeout: "explode"}, "on_timeout"},
		{"a negative timeout", StageEdit{Stage: "build", TimeoutMs: i64(-1)}, "negative"},
		{"a stage that is not there", StageEdit{Stage: "ship", AdvanceWhen: "any"}, "no stage called"},
		{"no stage named", StageEdit{AdvanceWhen: "any"}, "which stage"},
		{"nothing to change", StageEdit{Stage: "build"}, "nothing to change"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := st.SetStage("duo", c.e)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to contain %q", err, c.want)
			}
			if now, _ := os.ReadFile(st.Path("duo")); string(now) != string(before) {
				t.Errorf("a refused edit changed the file:\n%s", now)
			}
		})
	}
}

func TestSetStageRefusesAMissingOrBrokenBlueprint(t *testing.T) {
	st := teamStore(t)
	if err := st.SetStage("ghost", StageEdit{Stage: "x", AdvanceWhen: "any"}); !errors.Is(err, ErrNotExist) {
		t.Errorf("a missing team: %v", err)
	}
	if err := st.SetStage("../duo", StageEdit{Stage: "build", AdvanceWhen: "any"}); err == nil {
		t.Error("a path in the name was accepted")
	}
	bad := "name: bad\nstages:\n  - {name: s, advance_when: sometimes}\n"
	if err := os.WriteFile(st.Path("bad"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.SetStage("bad", StageEdit{Stage: "s", AdvanceWhen: "any"}); err == nil {
		t.Error("a file that does not load was edited")
	}
	if now, _ := os.ReadFile(st.Path("bad")); string(now) != bad {
		t.Error("the broken file was touched")
	}
}

func TestAnEditedTeamStillLoadsAndRuns(t *testing.T) {
	st := teamStore(t)
	if err := st.SetStage("duo", StageEdit{Stage: "build", AdvanceWhen: "any", TimeoutMs: i64(1000), OnTimeout: "fail"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(st.Path("duo"))
	if _, err := blueprint.Load(raw); err != nil {
		t.Fatalf("the edited file does not load: %v\n%s", err, raw)
	}
	names, _ := st.Names()
	if len(names) != 1 {
		t.Errorf("an edit left stray files: %v", names)
	}
}
