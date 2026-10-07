package main

import (
	"context"
	"errors"
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

// featureTeamInfo is the shape of agents/feature-team.yaml as the core describes it.
func featureTeamInfo() *driver.BlueprintInfo {
	return &driver.BlueprintInfo{
		Name: "feature-team", Workspace: "worktree", WorkspaceReason: "backend and frontend write files",
		Stages: []driver.BlueprintStage{
			{Name: "build", AdvanceWhen: "all", OnTimeout: "escalate", TimeoutMs: 1800000},
			{Name: "review", AdvanceWhen: "any"},
		},
		Members: []driver.BlueprintMember{
			{Name: "backend", Role: "implementer", Tools: []string{"read", "write", "bash"}},
			{Name: "frontend", Role: "implementer", Model: "openai/gpt-4o", Tools: []string{"read", "write"}},
			{Name: "security", Role: "reviewer", Tools: []string{"read"}, Advisory: true, Stages: []string{"review"}},
		},
		Watchers: []driver.BlueprintWatcher{{Agent: "security", Pattern: "run.quiescent", Action: "notify"}},
	}
}

func TestTeamDetailDrawsStagesMembersWatchersAndWorkspace(t *testing.T) {
	d := teamDetail(teamItem{Name: "feature-team", Info: featureTeamInfo()})
	for _, want := range []string{
		"Stages  build → review",
		"1. build — every one of backend, frontend must submit. After 30 min: escalate",
		"2. review — any one of backend, frontend submitting is enough; security only advise",
		"backend · implementer · tools: read, write, bash",
		"frontend · implementer · openai/gpt-4o · tools: read, write",
		"security · reviewer · advisory · tools: read · only in review",
		"security watches run.quiescent → notify",
		"Works in: worktree (backend and frontend write files)",
	} {
		if !strings.Contains(d, want) {
			t.Errorf("detail lacks %q:\n%s", want, d)
		}
	}
}

// A member that names no stages is in every stage; reading the empty list as "none"
// would draw a team nobody can advance.
func TestTeamDetailTreatsNoStagesAsEveryStage(t *testing.T) {
	info := &driver.BlueprintInfo{
		Stages:  []driver.BlueprintStage{{Name: "a", AdvanceWhen: "all"}, {Name: "b", AdvanceWhen: "quorum:2"}},
		Members: []driver.BlueprintMember{{Name: "x"}, {Name: "y"}, {Name: "z"}},
	}
	d := teamDetail(teamItem{Info: info})
	if !strings.Contains(d, "1. a — every one of x, y, z must submit") || !strings.Contains(d, "2. b — 2 of x, y, z must submit") {
		t.Fatalf("detail:\n%s", d)
	}
}

func TestTeamDetailWithoutStagesSaysTheyWorkTogether(t *testing.T) {
	info := &driver.BlueprintInfo{Members: []driver.BlueprintMember{{Name: "solo", Role: "agent"}}}
	d := teamDetail(teamItem{Info: info})
	if !strings.Contains(d, "No stages: solo work together") || strings.Contains(d, "Watchers") || strings.Contains(d, "Works in") {
		t.Fatalf("detail:\n%s", d)
	}
}

func TestTeamDetailShowsTheCoresReasonForARefusedFile(t *testing.T) {
	d := teamDetail(teamItem{Name: "broken", Err: "the blueprint is not valid: stage review has no members"})
	if !strings.Contains(d, "✗ the core refuses this blueprint") || !strings.Contains(d, "stage review has no members") || !strings.Contains(d, "agents/broken.yaml") {
		t.Fatalf("detail:\n%s", d)
	}
}

type fakeTeamCore struct {
	infos  map[string]*driver.BlueprintInfo
	asked  []string
	models []driver.ModelRow
	refuse string // when set, every create is refused with this sentence

	agents []driver.AgentCreateParams
	teams  []driver.BlueprintCreateParams
}

func (f *fakeTeamCore) Hello() *driver.Hello {
	return &driver.Hello{Implemented: []string{"blueprint.validate", "agent.create", "blueprint.create"}}
}

func (f *fakeTeamCore) SubmitModelList(context.Context) (*driver.ModelListResult, error) {
	return &driver.ModelListResult{Models: f.models}, nil
}

func (f *fakeTeamCore) SubmitAgentCreate(_ context.Context, p driver.AgentCreateParams) (*driver.AgentCreateResult, error) {
	if f.refuse != "" {
		return nil, errors.New(f.refuse)
	}
	f.agents = append(f.agents, p)
	return &driver.AgentCreateResult{Name: p.Name, Path: "/w/agents/" + p.Name + ".yaml", Tools: p.Tools}, nil
}

func (f *fakeTeamCore) SubmitBlueprintCreate(_ context.Context, p driver.BlueprintCreateParams) (*driver.BlueprintCreateResult, error) {
	if f.refuse != "" {
		return nil, errors.New(f.refuse)
	}
	f.teams = append(f.teams, p)
	return &driver.BlueprintCreateResult{Name: p.Name, Members: p.Members, Stages: p.Stages}, nil
}

func (f *fakeTeamCore) SubmitBlueprintValidate(_ context.Context, path string) (*driver.BlueprintInfo, error) {
	f.asked = append(f.asked, filepath.Base(path))
	if info, ok := f.infos[filepath.Base(path)]; ok {
		return info, nil
	}
	return nil, errors.New("the blueprint is not valid: nope")
}

func writeAgents(t *testing.T, root string, names ...string) {
	t.Helper()
	dir := filepath.Join(root, teamDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("name: x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadTeamsListsYamlFilesSortedAndKeepsTheBrokenOnes(t *testing.T) {
	root := t.TempDir()
	writeAgents(t, root, "zeta.yaml", "alpha.yaml", "notes.txt", "broken.yaml")
	if err := os.Mkdir(filepath.Join(root, teamDir, "sub.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	core := &fakeTeamCore{infos: map[string]*driver.BlueprintInfo{"alpha.yaml": featureTeamInfo(), "zeta.yaml": featureTeamInfo()}}
	items, err := readTeams(context.Background(), core, root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, it := range items {
		names = append(names, it.Name)
	}
	if strings.Join(names, ",") != "alpha,broken,zeta" {
		t.Fatalf("names = %v (a text file and a folder are not teams; a broken one must stay)", names)
	}
	if items[1].Err == "" || items[0].Err != "" {
		t.Fatalf("the broken file must carry the core's reason and the good one must not: %+v", items)
	}
	if got := teamRowStatus(items[1]); got != "✗ not valid" {
		t.Errorf("broken row status = %q", got)
	}
	if got := teamRowStatus(items[0]); got != "3 members · 2 stages" {
		t.Errorf("row status = %q", got)
	}
}

func TestReadTeamsWithNoAgentsFolderIsEmptyNotAnError(t *testing.T) {
	items, err := readTeams(context.Background(), &fakeTeamCore{}, t.TempDir())
	if err != nil || len(items) != 0 {
		t.Fatalf("items = %v, err = %v", items, err)
	}
}

func TestTeamScreenPublishesTheHighlightedTeamAndMoves(t *testing.T) {
	a, b := featureTeamInfo(), featureTeamInfo()
	b.Stages = nil
	ts := &teamScreen{sel: teamActions, items: []teamItem{{Name: "one", Info: a}, {Name: "two", Info: b}}}
	var st fold.State
	ts.publish(&st)
	if len(st.HubRows) != 4 || !st.HubRows[2].Selected || !strings.Contains(st.HubDetail, "Stages  build → review") {
		t.Fatalf("first publish: %+v / %s", st.HubRows, st.HubDetail)
	}
	if !strings.Contains(st.HubTitle, "2 in agents/") {
		t.Errorf("title = %q", st.HubTitle)
	}
	ts.key(term.Key{Type: term.KeyDown})
	ts.key(term.Key{Type: term.KeyDown}) // past the end
	ts.publish(&st)
	if ts.sel != teamActions+1 || !st.HubRows[3].Selected || !strings.Contains(st.HubDetail, "No stages") {
		t.Fatalf("after moving: sel=%d %s", ts.sel, st.HubDetail)
	}
}

func TestTeamScreenExplainsAnEmptyFolderAndALoadingRead(t *testing.T) {
	var st fold.State
	(&teamScreen{loading: true}).publish(&st)
	if !strings.Contains(st.HubDetail, "Reading agents/") {
		t.Errorf("loading: %q", st.HubDetail)
	}
	(&teamScreen{}).publish(&st)
	for _, want := range []string{"Nothing is stored in agents/", "New agent", "New team"} {
		if !strings.Contains(st.HubDetail, want) && !strings.Contains(st.HubRows[0].Label+st.HubRows[1].Label, want) {
			t.Errorf("empty detail lacks %q:\n%s", want, st.HubDetail)
		}
	}
}

func TestTeamScreenKeysOnlyEscAndQClose(t *testing.T) {
	ts := &teamScreen{}
	closes := func(k term.Key) bool { c, _ := ts.key(k); return c }
	if closes(term.Key{Type: term.KeyRunes, Runes: []rune{'x'}}) || closes(term.Key{Type: term.KeyEnter}) {
		t.Error("an ordinary key must not close the screen")
	}
	if !closes(term.Key{Type: term.KeyEscape}) || !closes(term.Key{Type: term.KeyRunes, Runes: []rune{'q'}}) {
		t.Error("Esc and q close it")
	}
}

func TestTheEmbeddedTeamSceneIsValid(t *testing.T) {
	if _, err := loadTeamScene(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(factoryTeam, "· team") || strings.Contains(factoryTeam, "· flow") {
		t.Error("the team screen must carry its own title")
	}
}

type teamTestDriver struct {
	testDriver
	core hubCore
}

func (d *teamTestDriver) Hub() hubCore { return d.core }

// teamHubCore is a hubCore that also describes blueprints: the loop reaches /team
// through the same Hub() seam /provider uses.
type teamHubCore struct {
	hubCore
	*fakeTeamCore
}

func (c teamHubCore) Hello() *driver.Hello { return c.fakeTeamCore.Hello() }

// TestLoopTeamOpensListsAndEscCloses drives the real loop: /team reads ./agents
// from the working folder through the core, draws the first team, and Esc returns
// to the chat.
func TestLoopTeamOpensListsAndEscCloses(t *testing.T) {
	root := t.TempDir()
	writeAgents(t, root, "feature-team.yaml")
	t.Chdir(root)

	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatal(err)
	}
	core := teamHubCore{fakeTeamCore: &fakeTeamCore{infos: map[string]*driver.BlueprintInfo{"feature-team.yaml": featureTeamInfo()}}}
	drv := &teamTestDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}, core: core}

	keys := func(s string) []scheduledEvent {
		var out []scheduledEvent
		for _, r := range s {
			out = append(out, scheduledEvent{5 * time.Millisecond, keyEvent(r)})
		}
		return out
	}
	script := []scheduledEvent{{120 * time.Millisecond, keyEvent('/')}}
	script = append(script, keys("team")...)
	script = append(script,
		scheduledEvent{40 * time.Millisecond, enterEvent()},
		scheduledEvent{60 * time.Millisecond, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyDown}}},
		scheduledEvent{20 * time.Millisecond, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyDown}}},
		scheduledEvent{60 * time.Millisecond, keyEvent('z')},
		scheduledEvent{250 * time.Millisecond, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyEscape}}},
		scheduledEvent{150 * time.Millisecond, ctrlCharEvent('c')},
		scheduledEvent{50 * time.Millisecond, ctrlCharEvent('c')})

	tty := newFakeTTY(110, 40, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	frames := strings.Split(tty.output(), "\x1b[?2026h")
	var open string
	for _, f := range frames {
		if s := stripANSI(f); strings.Contains(s, "Stages  build → review") {
			open = s
		}
	}
	if open == "" {
		t.Fatalf("no frame drew the team:\n%s", stripANSI(tty.output()))
	}
	for _, want := range []string{"Agents & teams", "feature-team", "3 members · 2 stages", "security watches run.quiescent", "esc close"} {
		if !strings.Contains(open, want) {
			t.Errorf("team screen lacks %q:\n%s", want, open)
		}
	}
	last := stripANSI(frames[len(frames)-1])
	if strings.Contains(last, "esc close") {
		t.Errorf("Esc did not close the screen:\n%s", last)
	}
	if strings.Contains(last, "› z") || strings.Contains(last, "┃ z") {
		t.Errorf("a key typed on the team screen leaked into the chat:\n%s", last)
	}
	if len(drv.submitted) != 0 {
		t.Errorf("/team reached the driver as %q", drv.submitted)
	}
	if len(core.fakeTeamCore.asked) != 1 || core.fakeTeamCore.asked[0] != "feature-team.yaml" {
		t.Errorf("the core was asked %v", core.fakeTeamCore.asked)
	}
}

// TestTeamsAreDescribedByTheRealCore reads a team file through the real core, so
// the wire shape this screen decodes is the one the core actually sends.
func TestTeamsAreDescribedByTheRealCore(t *testing.T) {
	bin := buildCore(t)
	work := t.TempDir()
	t.Setenv("ARXI_BIN", bin)
	t.Setenv("HOME", work)
	t.Setenv("USERPROFILE", work)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(work, "cfg"))
	t.Chdir(work)
	if err := os.MkdirAll(teamDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const yaml = `name: duo
members:
  - {name: backend, role: implementer, tools: [read, write]}
  - {name: security, role: reviewer, tools: [read], advisory: true}
stages:
  - {name: build, advance_when: all, timeout_ms: 600000, on_timeout: escalate}
  - {name: review, advance_when: any}
watchers:
  - {agent: security, pattern: run.quiescent, action: notify}
`
	if err := os.WriteFile(filepath.Join(teamDir, "duo.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(teamDir, "bad.yaml"), []byte("name: bad\nmembers:\n  - {name: a}\nstages:\n  - {name: s, advance_when: sometimes}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	drv, _, err := openServeDriver(ctx, bin)
	if err != nil {
		t.Fatalf("openServeDriver: %v", err)
	}
	defer func() {
		if c, ok := drv.(interface{ Close() error }); ok {
			c.Close()
		}
	}()
	hc, _ := drv.(interface{ Hub() hubCore })
	tc, ok := hc.Hub().(teamCore)
	if !ok || !helloImplements(tc.Hello(), "blueprint.validate") {
		t.Fatal("the real core must implement blueprint.validate")
	}
	items, err := readTeams(ctx, tc, work)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Name != "bad" || items[1].Name != "duo" {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Err == "" {
		t.Error("a blueprint with an unknown advance rule must be refused with a reason")
	}
	d := teamDetail(items[1])
	for _, want := range []string{
		"Stages  build → review",
		"1. build — every one of backend must submit; security only advise. After 10 min: escalate",
		"2. review — any one of backend submitting is enough; security only advise",
		"backend · implementer · tools: read, write",
		"security · reviewer · advisory · tools: read",
		"security watches run.quiescent → notify",
	} {
		if !strings.Contains(d, want) {
			t.Errorf("detail lacks %q:\n%s", want, d)
		}
	}
}

// Both embedded cores answer model.list; the one under test is the team fake.
func (c teamHubCore) SubmitModelList(ctx context.Context) (*driver.ModelListResult, error) {
	return c.fakeTeamCore.SubmitModelList(ctx)
}
