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

func TestRunLaunchArgsCarryOnlyWhatWasChosen(t *testing.T) {
	got := strings.Join(runLaunch{Team: "crew", Task: "do it", Budget: 0.5}.args(), "|")
	if got != "run|start|crew|do it|--budget|0.5" {
		t.Errorf("args = %s", got)
	}
	got = strings.Join(runLaunch{Team: "crew", Task: "x", Budget: 2, Model: "p/m", Sim: true}.args(), "|")
	if got != "run|start|crew|x|--budget|2|--model|p/m|--sim" {
		t.Errorf("args = %s", got)
	}
}

func TestStartedRunIDReadsTheCoresAnnouncement(t *testing.T) {
	if id, ok := startedRunID("run abc-123 started (budget 0.50 USD, workspace auto→none)"); !ok || id != "abc-123" {
		t.Errorf("id=%q ok=%v", id, ok)
	}
	for _, l := range []string{"", "run abc succeeded (seq 1)", "blueprint is not valid."} {
		if _, ok := startedRunID(l); ok {
			t.Errorf("%q is not an announcement", l)
		}
	}
}

func TestARefusalLosesItsCommandLineTeaching(t *testing.T) {
	raw := "arxi run start: these members name no model and the run has no default: a, b\n" +
		"  fix: give each a `model:` in the blueprint, or start the run with --model <id>\n" +
		"  see: arxi model list, for the models this machine has enabled\n" +
		"  note: no default is invented here on purpose\n"
	got := plainRefusal(raw)
	if got != "these members name no model and the run has no default: a, b" {
		t.Errorf("refusal = %q", got)
	}
	if strings.Contains(plainRefusal("usage: arxi run start <actor>\n"), "arxi") {
		t.Error("usage text must not reach the person")
	}
}

func TestRunFormValidatesTaskAndBudgetBeforeAskingTheCore(t *testing.T) {
	f := newRunForm("crew", []string{"p/m"})
	f.focus = len(f.fields) - 1
	if _, task, msg := f.key(tk(term.KeyEnter)); task != nil || msg != "Task is required" {
		t.Fatalf("task=%v msg=%q", task, msg)
	}
	f = newRunForm("crew", nil)
	f.insert("build it")
	f.focus = 1
	f.fields[1].value = "zero"
	f.focus = len(f.fields) - 1
	if _, task, msg := f.key(tk(term.KeyEnter)); task != nil || !strings.Contains(msg, "Budget") {
		t.Fatalf("task=%v msg=%q", task, msg)
	}
}

func TestRunFormBuildsTheLaunch(t *testing.T) {
	f := newRunForm("crew", []string{"p/m"})
	typeForm(f, "build the login page")
	f.key(tk(term.KeyTab))
	f.fields[1].value = "0.25"
	f.key(tk(term.KeyTab))
	f.key(tk(term.KeyRight)) // p/m
	f.key(tk(term.KeyTab))
	f.key(rk(' ')) // rehearsal
	_, task, msg := f.key(tk(term.KeyEnter))
	if msg != "" || task == nil || task.run == nil {
		t.Fatalf("task=%v msg=%q", task, msg)
	}
	r := task.run
	if r.Team != "crew" || r.Task != "build the login page" || r.Budget != 0.25 || r.Model != "p/m" || !r.Sim {
		t.Fatalf("launch = %+v", r)
	}
}

func TestEnterOnAStoredFileOpensTheRunFormAndABrokenOneRefuses(t *testing.T) {
	ts := &teamScreen{sel: teamActions, items: []teamItem{
		{Name: "crew", Info: &driver.BlueprintInfo{}}, {Name: "bad", Err: "nope"}}}
	ts.key(tk(term.KeyEnter))
	if ts.form == nil || ts.form.title != "Run crew" {
		t.Fatalf("form = %+v", ts.form)
	}
	ts.form = nil
	ts.sel = teamActions + 1
	ts.key(tk(term.KeyEnter))
	if ts.form != nil || !strings.Contains(ts.banner, "cannot run") {
		t.Fatalf("form=%v banner=%q", ts.form, ts.banner)
	}
}

func TestAMissingModelRefusalPointsAtTheModelField(t *testing.T) {
	ts := &teamScreen{form: newRunForm("crew", nil)}
	ts.apply(teamOutcome{refused: "these members name no model and the run has no default: a, b"})
	if !strings.Contains(ts.banner, "choose a model in the Model field") || ts.form == nil {
		t.Fatalf("banner=%q form=%v", ts.banner, ts.form)
	}
}

type fakeLauncher struct {
	got runLaunch
	err error
}

func (f *fakeLauncher) LaunchRun(_ context.Context, r runLaunch) (string, error) {
	f.got = r
	return "run-1", f.err
}

func TestStartTeamRunReportsTheIDOrTheRefusal(t *testing.T) {
	ch := make(chan teamOutcome, 1)
	startTeamRun(context.Background(), &fakeLauncher{}, runLaunch{Team: "crew"}, ch)
	if o := <-ch; o.launched != "run-1" || o.refused != "" {
		t.Fatalf("outcome = %+v", o)
	}
	startTeamRun(context.Background(), &fakeLauncher{err: os.ErrInvalid}, runLaunch{}, ch)
	if o := <-ch; o.refused == "" {
		t.Fatalf("outcome = %+v", o)
	}
}

// TestARealTeamRunStartsFollowsAndFinishes uses the real core: a team made through
// the manager's own calls is rehearsed with launchRun, the log appears where /flow
// follows it, and a run the core refuses comes back as a plain sentence.
func TestARealTeamRunStartsFollowsAndFinishes(t *testing.T) {
	bin := buildCore(t)
	work := t.TempDir()
	t.Setenv("ARXI_BIN", bin)
	t.Setenv("HOME", work)
	t.Setenv("USERPROFILE", work)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(work, "cfg"))
	t.Chdir(work)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	sd, _, err := openServeDriver(ctx, bin)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if c, ok := sd.(interface{ Close() error }); ok {
			c.Close()
		}
	}()
	hc, _ := sd.(interface{ Hub() hubCore })
	tc := hc.Hub().(teamCore)
	ch := make(chan teamOutcome, 1)
	for _, n := range []string{"a", "b"} {
		startTeamCreate(ctx, tc, work, teamTask{agent: &driver.AgentCreateParams{Name: n, Tools: []string{"read"}}}, ch)
		if o := <-ch; o.refused != "" {
			t.Fatal(o.refused)
		}
	}
	startTeamCreate(ctx, tc, work, teamTask{team: &driver.BlueprintCreateParams{
		Name: "crew", Members: []string{"a", "b"}, Stages: []string{"build", "review"}}}, ch)
	if o := <-ch; o.refused != "" {
		t.Fatal(o.refused)
	}

	// A live run with no model is refused in plain words.
	_, _, err = launchRun(ctx, bin, work, runLaunch{Team: "crew", Task: "x", Budget: 1})
	if err == nil || !strings.Contains(err.Error(), "name no model") || strings.Contains(err.Error(), "arxi ") {
		t.Fatalf("refusal = %v", err)
	}
	if _, _, err = launchRun(ctx, bin, work, runLaunch{Team: "ghost", Task: "x", Budget: 1, Sim: true}); err == nil ||
		!strings.Contains(err.Error(), "no such") {
		t.Fatalf("missing team = %v", err)
	}

	// The same team in rehearsal runs to the end and leaves its log.
	l, ok := sd.(runLauncher)
	if !ok {
		t.Fatal("the live driver must be a runLauncher")
	}
	id, err := l.LaunchRun(ctx, runLaunch{Team: "crew", Task: "make a thing", Budget: 0.5, Sim: true})
	if err != nil || id == "" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	log := filepath.Join(work, "runs", id, "events.ndjson")
	deadline := time.Now().Add(20 * time.Second)
	for {
		b, _ := os.ReadFile(log)
		if strings.Contains(string(b), `"type":"run.result"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the run never finished:\n%s", b)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

var _ = fold.Event{}

type launchingDriver struct {
	teamTestDriver
	l *fakeLauncher
}

func (d *launchingDriver) LaunchRun(ctx context.Context, r runLaunch) (string, error) {
	return d.l.LaunchRun(ctx, r)
}

// TestLoopRunsATeamFromTheScreenAndOpensFlow drives the real loop with the keyboard
// alone: /team, Enter on a stored team, a task, Enter through the fields. The
// launcher gets the task and the screen is handed over to /flow.
func TestLoopRunsATeamFromTheScreenAndOpensFlow(t *testing.T) {
	root := t.TempDir()
	writeAgents(t, root, "feature-team.yaml")
	t.Chdir(root)
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatal(err)
	}
	core := teamHubCore{fakeTeamCore: &fakeTeamCore{infos: map[string]*driver.BlueprintInfo{"feature-team.yaml": featureTeamInfo()}}}
	l := &fakeLauncher{}
	drv := &launchingDriver{teamTestDriver: teamTestDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}, core: core}, l: l}

	at := func(ms int, ev term.Event) scheduledEvent {
		return scheduledEvent{time.Duration(ms) * time.Millisecond, ev}
	}
	key := func(k term.Key) term.Event { return term.Event{Kind: term.EventKey, Key: k} }
	script := []scheduledEvent{at(120, keyEvent('/'))}
	for _, r := range "team" {
		script = append(script, at(5, keyEvent(r)))
	}
	script = append(script, at(40, enterEvent()),
		at(150, key(tk(term.KeyDown))), at(10, key(tk(term.KeyDown))), // onto feature-team
		at(10, enterEvent())) // opens the Run form
	for _, r := range "ship it" {
		script = append(script, at(5, keyEvent(r)))
	}
	for i := 0; i < 4; i++ { // Task → Budget → Model → Rehearsal → submit
		script = append(script, at(10, enterEvent()))
	}
	script = append(script, at(300, key(tk(term.KeyEscape))),
		at(150, ctrlCharEvent('c')), at(50, ctrlCharEvent('c')))

	tty := newFakeTTY(110, 40, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	if l.got.Team != "feature-team" || l.got.Task != "ship it" || l.got.Budget != 1 {
		t.Fatalf("the launcher got %+v", l.got)
	}
	out := stripANSI(tty.output())
	for _, want := range []string{"Run feature-team", "Starting feature-team", "Flow"} {
		if !strings.Contains(out, want) {
			t.Errorf("no frame showed %q", want)
		}
	}
	if len(drv.submitted) != 0 {
		t.Errorf("keys typed in the form reached the chat as %q", drv.submitted)
	}
}
