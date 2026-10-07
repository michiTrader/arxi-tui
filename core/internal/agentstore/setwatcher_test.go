package agentstore

import (
	"os"
	"strings"
	"testing"

	"github.com/michiTrader/arxi/internal/kernel"
)

func watchersOf(t *testing.T, st *Store, team string) []kernel.Watcher {
	t.Helper()
	bp, err := st.Load(team)
	if err != nil {
		t.Fatal(err)
	}
	return bp.Config.Watchers
}

func TestSetWatcherAddsReplacesAndRemovesOneRule(t *testing.T) {
	st := teamStore(t)
	if err := st.SetWatcher("duo", WatchEdit{Agent: "b", Pattern: "stage.*"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetWatcher("duo", WatchEdit{Agent: "a", Pattern: "run.quiescent", Action: "notify"}); err != nil {
		t.Fatal(err)
	}
	w := watchersOf(t, st, "duo")
	if len(w) != 2 || w[0].Agent != "b" || w[1].Action != "notify" {
		t.Fatalf("watchers = %+v", w)
	}

	// The same member and pattern again replaces the rule instead of adding one.
	if err := st.SetWatcher("duo", WatchEdit{Agent: "b", Pattern: "stage.*", Action: "run_tool", Tool: "read"}); err != nil {
		t.Fatal(err)
	}
	w = watchersOf(t, st, "duo")
	if len(w) != 2 || w[0].Action != "run_tool" || w[0].Tool != "read" || w[1].Action != "notify" {
		t.Fatalf("after replacing: %+v", w)
	}

	if err := st.SetWatcher("duo", WatchEdit{Agent: "b", Pattern: "stage.*", Remove: true}); err != nil {
		t.Fatal(err)
	}
	if w = watchersOf(t, st, "duo"); len(w) != 1 || w[0].Agent != "a" {
		t.Fatalf("after removing: %+v", w)
	}
	// The last one goes too and the file still loads.
	if err := st.SetWatcher("duo", WatchEdit{Agent: "a", Pattern: "run.quiescent", Remove: true}); err != nil {
		t.Fatal(err)
	}
	if w = watchersOf(t, st, "duo"); len(w) != 0 {
		t.Fatalf("after removing the last: %+v", w)
	}
}

func TestSetWatcherLeavesEverythingOutsideTheBlockAlone(t *testing.T) {
	st := teamStore(t)
	raw := func() string {
		b, err := os.ReadFile(st.Path("duo"))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	before := raw()
	if err := st.SetWatcher("duo", WatchEdit{Agent: "a", Pattern: "stage.advanced"}); err != nil {
		t.Fatal(err)
	}
	after := raw()
	if !strings.HasPrefix(after, before) || !strings.Contains(after, "watchers:\n  - {agent: a, pattern: stage.advanced}") {
		t.Fatalf("a file with no watchers must only grow at its end:\n%s", after)
	}
	mid := raw()
	if err := st.SetWatcher("duo", WatchEdit{Agent: "b", Pattern: "stage.*", Action: "notify"}); err != nil {
		t.Fatal(err)
	}
	if got := raw(); !strings.HasPrefix(got, strings.TrimSuffix(mid, "\n")) {
		t.Errorf("an existing watcher was rewritten:\n%s\n--- was ---\n%s", got, mid)
	}
}

func TestSetWatcherRefusesWhatTheLoaderRefusesAndKeepsTheFile(t *testing.T) {
	cases := []struct {
		name string
		e    WatchEdit
		want string
	}{
		{"not a member", WatchEdit{Agent: "ghost", Pattern: "stage.*"}, "not declared in members"},
		{"a wildcard in the middle", WatchEdit{Agent: "a", Pattern: "st*age"}, "trailing wildcard"},
		{"an unknown tool", WatchEdit{Agent: "a", Pattern: "stage.*", Action: "run_tool", Tool: "nuke"}, "nuke"},
		{"run_tool with no tool", WatchEdit{Agent: "a", Pattern: "stage.*", Action: "run_tool"}, "no tool"},
		{"an unknown action", WatchEdit{Agent: "a", Pattern: "stage.*", Action: "notfiy"}, "notfiy"},
		{"a tool on a plain wake", WatchEdit{Agent: "a", Pattern: "stage.*", Tool: "read"}, "dead config"},
		{"nobody named", WatchEdit{Pattern: "stage.*"}, "which member"},
		{"nothing listened to", WatchEdit{Agent: "a"}, "which member"},
		{"removing one that is not there", WatchEdit{Agent: "a", Pattern: "stage.*", Remove: true}, "no watcher"},
		{"removing with an action", WatchEdit{Agent: "a", Pattern: "stage.*", Remove: true, Action: "notify"}, "only its member"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := teamStore(t)
			before, _ := os.ReadFile(st.Path("duo"))
			err := st.SetWatcher("duo", c.e)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want one containing %q", err, c.want)
			}
			if after, _ := os.ReadFile(st.Path("duo")); string(after) != string(before) {
				t.Errorf("a refusal changed the file:\n%s", after)
			}
		})
	}
}
