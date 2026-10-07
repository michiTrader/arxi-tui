package agentstore

import (
	"os"
	"strings"
	"testing"

	"github.com/michiTrader/arxi/internal/kernel"
)

func sp(s string) *string { return &s }

func memberOf(t *testing.T, st *Store, team, member string) kernel.MemberConfig {
	t.Helper()
	bp, err := st.Load(team)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range bp.Config.Members {
		if m.Name == member {
			return m
		}
	}
	t.Fatalf("no member %q", member)
	return kernel.MemberConfig{}
}

func TestSetMemberChangesOnlyTheNamedFieldsOfTheNamedMember(t *testing.T) {
	st := teamStore(t)
	tools := []string{"read", "grep"}
	adv := true
	if err := st.SetMember("duo", MemberEdit{Member: "b", Model: sp("p/m"), Role: sp("reviewer"), Tools: &tools, Advisory: &adv}); err != nil {
		t.Fatal(err)
	}
	b := memberOf(t, st, "duo", "b")
	if b.Model != "p/m" || b.Role != "reviewer" || len(b.Tools) != 2 || !b.Advisory {
		t.Errorf("b = %+v", b)
	}
	if a := memberOf(t, st, "duo", "a"); a.Model != "" || a.Role != "" || a.Advisory {
		t.Errorf("another member changed: %+v", a)
	}

	// An edit naming one field keeps the rest.
	if err := st.SetMember("duo", MemberEdit{Member: "b", Model: sp("other")}); err != nil {
		t.Fatal(err)
	}
	if b = memberOf(t, st, "duo", "b"); b.Model != "other" || b.Role != "reviewer" || len(b.Tools) != 2 || !b.Advisory {
		t.Errorf("an edit naming one field clobbered the rest: %+v", b)
	}

	// An empty value removes the field; false is a value, not an absence.
	none := []string{}
	no := false
	if err := st.SetMember("duo", MemberEdit{Member: "b", Model: sp(""), Role: sp(""), Tools: &none, Advisory: &no}); err != nil {
		t.Fatal(err)
	}
	if b = memberOf(t, st, "duo", "b"); b.Model != "" || b.Role != "" || len(b.Tools) != 0 || b.Advisory {
		t.Errorf("removing: %+v", b)
	}
}

func TestSetMemberKeepsWhatItDoesNotKnowAboutAMember(t *testing.T) {
	st, _ := Open(t.TempDir())
	src := "# notes stay\nname: hand\n\nmembers:\n  - name: a\n    activation: queue\n    stages: [one]\n  - name: b\n    role: x\n\nstages:\n  - {name: one, advance_when: any}\n  - {name: two, advance_when: all}\n\nwatchers:\n  - {agent: a, pattern: \"stage.*\"}\n# end\n"
	if err := os.WriteFile(st.Path("hand"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.SetMember("hand", MemberEdit{Member: "a", Model: sp("claude")}); err != nil {
		t.Fatal(err)
	}
	a := memberOf(t, st, "hand", "a")
	if a.Activation != "queue" || len(a.Stages) != 1 || a.Model != "claude" {
		t.Errorf("a = %+v", a)
	}
	got, _ := os.ReadFile(st.Path("hand"))
	for _, keep := range []string{"# notes stay\nname: hand\n\nmembers:\n", "\nstages:\n  - {name: one, advance_when: any}\n  - {name: two, advance_when: all}\n\nwatchers:\n  - {agent: a, pattern: \"stage.*\"}\n# end\n"} {
		if !strings.Contains(string(got), keep) {
			t.Errorf("lost %q in:\n%s", keep, got)
		}
	}
}

func TestSetMemberRefusesWhatWouldBreakTheFileAndLeavesItAlone(t *testing.T) {
	st := teamStore(t)
	before, _ := os.ReadFile(st.Path("duo"))
	bad := []string{"bahs"}
	cases := []struct {
		name string
		e    MemberEdit
		want string
	}{
		{"a tool that does not exist", MemberEdit{Member: "a", Tools: &bad}, "bahs"},
		{"a model with two slashes", MemberEdit{Member: "a", Model: sp("a/b/c")}, "provider/id"},
		{"a model with a stray space", MemberEdit{Member: "a", Model: sp(" m")}, "provider/id"},
		{"a member that is not there", MemberEdit{Member: "zed", Role: sp("x")}, "no member called"},
		{"no member named", MemberEdit{Role: sp("x")}, "which member"},
		{"nothing to change", MemberEdit{Member: "a"}, "nothing to change"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := st.SetMember("duo", c.e)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want it to contain %q", err, c.want)
			}
			if now, _ := os.ReadFile(st.Path("duo")); string(now) != string(before) {
				t.Errorf("a refused edit changed the file:\n%s", now)
			}
		})
	}
}

func TestSetMemberOnAnAgentEditsTheAgentItself(t *testing.T) {
	st, _ := Open(t.TempDir())
	if _, err := st.Create(Record{Name: "solo", Tools: []string{"read"}}); err != nil {
		t.Fatal(err)
	}
	tools := []string{"read", "write"}
	if err := st.SetMember("solo", MemberEdit{Member: "solo", Tools: &tools, Model: sp("m")}); err != nil {
		t.Fatal(err)
	}
	if m := memberOf(t, st, "solo", "solo"); len(m.Tools) != 2 || m.Model != "m" {
		t.Errorf("solo = %+v", m)
	}
	// Its stage is untouched, so it still runs.
	if s := stageOf(t, st, "solo", "work"); s.AdvanceWhen != "all" {
		t.Errorf("work = %+v", s)
	}
}

func TestAStagesLineWrittenOnOneLineIsRefusedNotDoubled(t *testing.T) {
	st, _ := Open(t.TempDir())
	src := "name: x\nmembers:\n  - name: a\nstages: [one]\n"
	_ = os.WriteFile(st.Path("x"), []byte(src), 0o644)
	// "stages: [one]" is not a list of mappings; the loader refuses the file first,
	// and either way the bytes stay.
	_ = st.SetStage("x", StageEdit{Stage: "one", AdvanceWhen: "any"})
	if now, _ := os.ReadFile(st.Path("x")); string(now) != src {
		t.Errorf("the file changed: %s", now)
	}
}
