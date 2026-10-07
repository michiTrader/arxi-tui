package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// inAgentsDir points the agent and role stores at a disposable folder.
func inAgentsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	oldA, oldR := agentDir, roleDir
	agentDir, roleDir = filepath.Join(dir, "agents"), filepath.Join(dir, "roles")
	t.Cleanup(func() { agentDir, roleDir = oldA, oldR })
	return dir
}

func wireOK(t *testing.T, line string) map[string]any {
	t.Helper()
	got := one(t, line)
	if !got.OK {
		t.Fatalf("%s refused: %+v", line, got.Error)
	}
	raw, _ := json.Marshal(got.Result)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func wireRefused(t *testing.T, line string) string {
	t.Helper()
	got := one(t, line)
	if got.OK {
		t.Fatalf("%s succeeded, expected a refusal: %+v", line, got.Result)
	}
	return got.Error.Message
}

// The whole story a screen needs, over the protocol and with no command line:
// create two agents, compose them into a team with stages, and read it all back.
func TestAgentsAndTeamsCanBeCreatedAndListedOverTheProtocol(t *testing.T) {
	inAgentsDir(t)

	if l := wireOK(t, `{"id":"1","type":"agent.list"}`); len(l["agents"].([]any)) != 0 {
		t.Fatalf("a fresh folder lists %v", l)
	}

	a := wireOK(t, `{"id":"2","type":"agent.create","params":{"name":"backend","model":"prov/m","role":"implementer","tools":"read,write"}}`)
	if a["name"] != "backend" || !strings.HasSuffix(a["path"].(string), "backend.yaml") {
		t.Fatalf("created = %v", a)
	}
	if note, _ := a["role_note"].(string); !strings.Contains(note, "not defined") {
		t.Errorf("an undefined role must be said, role_note = %q", note)
	}
	wireOK(t, `{"id":"3","type":"agent.create","params":{"name":"security","role":"reviewer","tools":"read","advisory":true}}`)

	team := wireOK(t, `{"id":"4","type":"blueprint.create","params":{"name":"duo","members":"backend,security","stages":"build,review"}}`)
	if got := team["stages"].([]any); len(got) != 2 || got[0] != "build" {
		t.Fatalf("team = %v", team)
	}

	l := wireOK(t, `{"id":"5","type":"agent.list"}`)["agents"].([]any)
	if len(l) != 3 {
		t.Fatalf("listed %d, want 3: %v", len(l), l)
	}
	var duo map[string]any
	for _, e := range l {
		if m := e.(map[string]any); m["name"] == "duo" {
			duo = m
		}
	}
	if duo == nil || len(duo["members"].([]any)) != 2 || len(duo["stages"].([]any)) != 2 {
		t.Fatalf("duo = %v", duo)
	}

	// What was written is a file the validator accepts: the promise of the store.
	v := wireOK(t, `{"id":"6","type":"blueprint.validate","params":{"path":`+quoteJSON(filepath.Join(agentDir, "duo.yaml"))+`}}`)
	if ms := v["members"].([]any); len(ms) != 2 || ms[1].(map[string]any)["advisory"] != true {
		t.Fatalf("validate = %v", v)
	}
}

func quoteJSON(s string) string { b, _ := json.Marshal(s); return string(b) }

// Every refusal the CLI gives must arrive as an error on a connection that stays up.
func TestAgentAndTeamCreationRefuseWhatTheCLIRefuses(t *testing.T) {
	inAgentsDir(t)
	wireOK(t, `{"id":"1","type":"agent.create","params":{"name":"solo","tools":"read"}}`)

	cases := []struct{ name, line, want string }{
		{"a duplicate agent is never overwritten",
			`{"id":"2","type":"agent.create","params":{"name":"solo"}}`, "already exists"},
		{"an unknown tool names itself",
			`{"id":"3","type":"agent.create","params":{"name":"x","tools":"reed"}}`, "reed"},
		{"a path separator in a name",
			`{"id":"4","type":"agent.create","params":{"name":"a/b"}}`, "path separator"},
		{"a team over a missing agent",
			`{"id":"5","type":"blueprint.create","params":{"name":"t","members":"solo,ghost"}}`, "no such agent"},
		{"a team with no members",
			`{"id":"6","type":"blueprint.create","params":{"name":"t","members":" , "}}`, "at least one member"},
		{"a team over a name already taken by an agent",
			`{"id":"7","type":"blueprint.create","params":{"name":"solo","members":"solo"}}`, "already exists"},
		{"two stages with one name",
			`{"id":"8","type":"blueprint.create","params":{"name":"t","members":"solo","stages":"build,build"}}`, "both called"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msg := wireRefused(t, c.line)
			if !strings.Contains(msg, c.want) {
				t.Errorf("message = %q, want it to contain %q", msg, c.want)
			}
		})
	}
	if names, _ := readAgents().Names(); len(names) != 1 {
		t.Errorf("a refused request wrote a file: %v", names)
	}
}

func TestATeamCannotBeComposedIntoATeam(t *testing.T) {
	inAgentsDir(t)
	wireOK(t, `{"id":"1","type":"agent.create","params":{"name":"a"}}`)
	wireOK(t, `{"id":"2","type":"agent.create","params":{"name":"b"}}`)
	wireOK(t, `{"id":"3","type":"blueprint.create","params":{"name":"ab","members":"a,b"}}`)
	if msg := wireRefused(t, `{"id":"4","type":"blueprint.create","params":{"name":"outer","members":"ab"}}`); !strings.Contains(msg, "itself a team") {
		t.Errorf("message = %q", msg)
	}
}

func TestARoleFillsTheBlanksOfAnAgentCreatedOverTheProtocol(t *testing.T) {
	dir := inAgentsDir(t)
	if err := os.MkdirAll(roleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(roleDir, "auditor.json"),
		[]byte(`{"name":"auditor","advisory":true,"tools":["read"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a := wireOK(t, `{"id":"1","type":"agent.create","params":{"name":"audit","role":"auditor"}}`)
	if a["advisory"] != true || len(a["tools"].([]any)) != 1 {
		t.Fatalf("the role's defaults did not reach the agent: %v", a)
	}
	if note, _ := a["role_note"].(string); !strings.Contains(note, "supplied the tools and the advisory trait") {
		t.Errorf("role_note = %q", note)
	}
	_ = dir
}

// A file that does not load stays in the list with its reason, so one broken file
// never hides the others.
func TestAgentListKeepsABrokenFileWithItsReason(t *testing.T) {
	inAgentsDir(t)
	wireOK(t, `{"id":"1","type":"agent.create","params":{"name":"good"}}`)
	if err := os.WriteFile(filepath.Join(agentDir, "bad.yaml"), []byte("name: bad\nstages:\n  - {name: s, advance_when: sometimes}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := wireOK(t, `{"id":"2","type":"agent.list"}`)["agents"].([]any)
	if len(l) != 2 {
		t.Fatalf("listed %v", l)
	}
	bad := l[0].(map[string]any)
	if bad["name"] != "bad" || bad["error"] == "" || bad["error"] == nil {
		t.Fatalf("the broken file lost its reason: %v", bad)
	}
}

func TestBlueprintStageChangesTheRulesAndAnswersWithWhatIsSaved(t *testing.T) {
	inAgentsDir(t)
	wireOK(t, `{"id":"1","type":"agent.create","params":{"name":"a","tools":"read"}}`)
	wireOK(t, `{"id":"2","type":"agent.create","params":{"name":"b","tools":"read"}}`)
	wireOK(t, `{"id":"3","type":"blueprint.create","params":{"name":"duo","members":"a,b","stages":"build,review"}}`)

	got := wireOK(t, `{"id":"4","type":"blueprint.stage","params":{"name":"duo","stage":"review","advance_when":"quorum:2","timeout_ms":60000,"on_timeout":"ask"}}`)
	stages := got["stages"].([]any)
	if len(stages) != 2 {
		t.Fatalf("stages = %v", stages)
	}
	review := stages[1].(map[string]any)
	if review["advance_when"] != "quorum:2" || review["timeout_ms"] != float64(60000) || review["on_timeout"] != "ask" {
		t.Errorf("review = %v", review)
	}
	if build := stages[0].(map[string]any); build["advance_when"] != "all" {
		t.Errorf("an untouched stage changed: %v", build)
	}

	// What was saved is what the validator reads back.
	v := wireOK(t, `{"id":"5","type":"blueprint.validate","params":{"path":`+quoteJSON(filepath.Join(agentDir, "duo.yaml"))+`}}`)
	if vs := v["stages"].([]any); vs[1].(map[string]any)["advance_when"] != "quorum:2" {
		t.Errorf("validate = %v", v)
	}

	// A timeout of 0 removes it, and leaves the rest.
	got = wireOK(t, `{"id":"6","type":"blueprint.stage","params":{"name":"duo","stage":"review","timeout_ms":0}}`)
	review = got["stages"].([]any)[1].(map[string]any)
	if _, has := review["timeout_ms"]; has || review["advance_when"] != "quorum:2" {
		t.Errorf("review after removing the timeout = %v", review)
	}
}

func TestBlueprintStageRefusesWhatTheLoaderRefusesAndKeepsTheFile(t *testing.T) {
	dir := inAgentsDir(t)
	wireOK(t, `{"id":"1","type":"agent.create","params":{"name":"solo","tools":"read"}}`)
	before, _ := os.ReadFile(filepath.Join(dir, "solo.yaml"))

	cases := []struct{ name, line, want string }{
		{"a quorum bigger than the team",
			`{"id":"2","type":"blueprint.stage","params":{"name":"solo","stage":"work","advance_when":"quorum:3"}}`, "quorum"},
		{"a stage that is not there",
			`{"id":"3","type":"blueprint.stage","params":{"name":"solo","stage":"ship","advance_when":"any"}}`, "no stage called"},
		{"a team that does not exist",
			`{"id":"4","type":"blueprint.stage","params":{"name":"ghost","stage":"work","advance_when":"any"}}`, "no such agent"},
		{"nothing to change",
			`{"id":"5","type":"blueprint.stage","params":{"name":"solo","stage":"work"}}`, "nothing to change"},
		{"a timeout with a fraction",
			`{"id":"6","type":"blueprint.stage","params":{"name":"solo","stage":"work","timeout_ms":1.5}}`, "whole number"},
		{"a path in the name",
			`{"id":"7","type":"blueprint.stage","params":{"name":"../solo","stage":"work","advance_when":"any"}}`, "path separator"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if msg := wireRefused(t, c.line); !strings.Contains(msg, c.want) {
				t.Errorf("message = %q, want it to contain %q", msg, c.want)
			}
		})
	}
	if after, _ := os.ReadFile(filepath.Join(dir, "solo.yaml")); string(after) != string(before) {
		t.Errorf("a refused edit changed the file:\n%s", after)
	}
}

func TestBlueprintMemberChangesOneMemberAndAnswersWithWhatIsSaved(t *testing.T) {
	inAgentsDir(t)
	wireOK(t, `{"id":"1","type":"agent.create","params":{"name":"a","tools":"read"}}`)
	wireOK(t, `{"id":"2","type":"agent.create","params":{"name":"b","tools":"read"}}`)
	wireOK(t, `{"id":"3","type":"blueprint.create","params":{"name":"duo","members":"a,b"}}`)

	got := wireOK(t, `{"id":"4","type":"blueprint.member","params":{"name":"duo","member":"b","model":"p/m","role":"reviewer","tools":"read,grep","advisory":true}}`)
	ms := got["members"].([]any)
	b := ms[1].(map[string]any)
	if b["model"] != "p/m" || b["role"] != "reviewer" || b["advisory"] != true || len(b["tools"].([]any)) != 2 {
		t.Errorf("b = %v", b)
	}
	if a := ms[0].(map[string]any); a["model"] != nil || a["advisory"] == true {
		t.Errorf("another member changed: %v", a)
	}

	// An empty string removes; an absent one leaves.
	got = wireOK(t, `{"id":"5","type":"blueprint.member","params":{"name":"duo","member":"b","tools":"","advisory":false}}`)
	b = got["members"].([]any)[1].(map[string]any)
	if b["tools"] != nil || b["model"] != "p/m" || b["advisory"] == true {
		t.Errorf("b after removing = %v", b)
	}
}

func TestBlueprintMemberRefusesWhatTheLoaderRefusesAndKeepsTheFile(t *testing.T) {
	dir := inAgentsDir(t)
	wireOK(t, `{"id":"1","type":"agent.create","params":{"name":"solo","tools":"read"}}`)
	before, _ := os.ReadFile(filepath.Join(dir, "solo.yaml"))
	cases := []struct{ name, line, want string }{
		{"a tool that does not exist", `{"id":"2","type":"blueprint.member","params":{"name":"solo","member":"solo","tools":"bahs"}}`, "bahs"},
		{"a member that is not there", `{"id":"3","type":"blueprint.member","params":{"name":"solo","member":"zed","role":"x"}}`, "no member called"},
		{"nothing to change", `{"id":"4","type":"blueprint.member","params":{"name":"solo","member":"solo"}}`, "nothing to change"},
		{"a team that does not exist", `{"id":"5","type":"blueprint.member","params":{"name":"ghost","member":"x","role":"r"}}`, "no such agent"},
		{"advisory that is not a boolean", `{"id":"6","type":"blueprint.member","params":{"name":"solo","member":"solo","advisory":"yes"}}`, "must be a bool"},
		{"a model that is not text", `{"id":"7","type":"blueprint.member","params":{"name":"solo","member":"solo","model":3}}`, "must be a string"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if msg := wireRefused(t, c.line); !strings.Contains(msg, c.want) {
				t.Errorf("message = %q, want it to contain %q", msg, c.want)
			}
		})
	}
	if after, _ := os.ReadFile(filepath.Join(dir, "solo.yaml")); string(after) != string(before) {
		t.Errorf("a refused edit changed the file:\n%s", after)
	}
}
