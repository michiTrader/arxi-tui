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

// A fixed clock in a fixed zone (UTC-3), so schedules read the same everywhere.
func fixedClock(t *testing.T) {
	t.Helper()
	oz, on := autoZone, autoNow
	autoZone = time.FixedZone("test", -3*3600)
	autoNow = func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { autoZone, autoNow = oz, on })
}

func TestParseEveryAcceptsPlainUnitsAndRefusesTheRest(t *testing.T) {
	for in, want := range map[string]string{
		"30m": "every:30m", "30 minutes": "every:30m", "2h": "every:2h", "2 hours": "every:2h",
		"1d": "every:24h", "2 days": "every:48h", " 15 MIN ": "every:15m",
	} {
		got, _, err := parseEvery(in)
		if err != nil || got != want {
			t.Errorf("parseEvery(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "soon", "m", "0m", "1.5h", "-3m", "every 3m"} {
		if _, _, err := parseEvery(bad); err == nil {
			t.Errorf("parseEvery(%q) was accepted", bad)
		}
	}
}

func TestDailyAndOnceAreWrittenInUTCFromThePersonsClock(t *testing.T) {
	fixedClock(t)
	spec, words, err := parseDaily("09:30", autoZone, autoNow())
	if err != nil || spec != "cron:30 12 * * *" {
		t.Fatalf("daily = %q, %v", spec, err)
	}
	if !strings.Contains(words, "09:30") {
		t.Errorf("words = %q", words)
	}
	if _, _, err := parseDaily("9h", autoZone, autoNow()); err == nil {
		t.Error("a bad time of day was accepted")
	}
	spec, _, err = parseOnce("2026-12-24 18:00", autoZone, autoNow())
	if err != nil || spec != "at:2026-12-24T21:00:00Z" {
		t.Fatalf("once = %q, %v", spec, err)
	}
	if _, _, err := parseOnce("2026-10-07 08:00", autoZone, autoNow()); err == nil || !strings.Contains(err.Error(), "passed") {
		t.Errorf("a moment in the past: %v", err)
	}
}

func TestDescribeOnReadsBackWhatTheFormWrote(t *testing.T) {
	fixedClock(t)
	for on, want := range map[string]string{
		"every:1m":                "every 1 minute",
		"every:90m":               "every 90 minutes",
		"every:2h":                "every 2 hours",
		"every:48h":               "every 2 days",
		"cron:30 12 * * *":        "every day at 09:30",
		"at:2026-12-24T21:00:00Z": "once, on 2026-12-24 at 18:00",
		"cron:0 3 1 * 1":          "cron:0 3 1 * 1",
	} {
		if got := describeOn(on); got != want {
			t.Errorf("describeOn(%q) = %q, want %q", on, got, want)
		}
	}
}

func fillForm(t *testing.T, f *teamForm, vals map[string]string) {
	t.Helper()
	for label, v := range vals {
		found := false
		for i := range f.fields {
			if f.fields[i].label == label {
				f.fields[i].value, found = v, true
			}
		}
		if !found {
			t.Fatalf("no field %q", label)
		}
	}
}

func TestAutoFormBuildsTheCoresRequest(t *testing.T) {
	fixedClock(t)
	f := newAutoForm([]string{"duo", "solo"}, []string{"p/m"})
	fillForm(t, f, map[string]string{"Name": "nightly", "Task": "audit the deps", "Timing": "30m", "Budget (USD)": "2.5"})
	f.focus = 1
	f.flip(1) // Team: solo
	f.focus = 6
	f.flip(1) // Per: week
	f.focus = 7
	f.flip(1) // Model: p/m
	f.focus = 8
	f.flip(1) // Rehearsal on
	task, msg := f.submit()
	if msg != "" || task == nil || task.auto == nil {
		t.Fatalf("submit: %v %q", task, msg)
	}
	p := task.auto
	if p.Name != "nightly" || p.On != "every:30m" || p.Budget != 2.5 || p.BudgetPeriod != "week" {
		t.Errorf("params = %+v", p)
	}
	if want := "run start solo --model p/m --sim -- audit the deps"; p.Then != want {
		t.Errorf("then = %q, want %q", p.Then, want)
	}
}

func TestAutoFormRefusalsNameTheField(t *testing.T) {
	fixedClock(t)
	ok := map[string]string{"Name": "n", "Task": "t", "Timing": "1h"}
	for name, tc := range map[string]struct {
		mod  map[string]string
		want string
	}{
		"no name":    {map[string]string{"Name": ""}, "Name is required"},
		"no task":    {map[string]string{"Task": ""}, "Task is required"},
		"no timing":  {map[string]string{"Timing": ""}, "Timing is required"},
		"bad timing": {map[string]string{"Timing": "whenever"}, "Timing must be"},
		"zero money": {map[string]string{"Budget (USD)": "0"}, "Budget (USD) must be"},
		"no money":   {map[string]string{"Budget (USD)": "lots"}, "Budget (USD) must be"},
	} {
		f := newAutoForm([]string{"duo"}, nil)
		fillForm(t, f, ok)
		fillForm(t, f, tc.mod)
		if _, msg := f.submit(); !strings.Contains(msg, tc.want) {
			t.Errorf("%s: msg = %q, want %q", name, msg, tc.want)
		}
	}
	f := newAutoForm(nil, nil)
	fillForm(t, f, ok)
	if _, msg := f.submit(); !strings.Contains(msg, "create an agent or a team") {
		t.Errorf("no team: %q", msg)
	}
}

func TestAutoFormExplainsTimingForTheChosenRepeat(t *testing.T) {
	fixedClock(t)
	a := &autoScreen{form: newAutoForm([]string{"duo"}, nil)}
	a.form.focus = 4
	var st fold.State
	a.publish(&st)
	if !strings.Contains(st.HubDetail, "a number and a unit") {
		t.Errorf("detail:\n%s", st.HubDetail)
	}
	a.form.focus = 3
	a.form.flip(1)
	a.form.focus = 4
	fillForm(t, a.form, map[string]string{"Timing": "09:30"})
	a.publish(&st)
	if !strings.Contains(st.HubDetail, "HH:MM") || !strings.Contains(st.HubDetail, "→ Runs every day at 09:30 (12:30 UTC)") {
		t.Errorf("detail:\n%s", st.HubDetail)
	}
	fillForm(t, a.form, map[string]string{"Timing": "9"})
	a.publish(&st)
	if !strings.Contains(st.HubDetail, "→ Timing must be a time of day") {
		t.Errorf("a wrong value is said before Enter:\n%s", st.HubDetail)
	}
}

func rowOf(name, on, status string) driver.TriggerRow {
	return driver.TriggerRow{Record: driver.TriggerRecord{
		Name: name, On: on, Then: "run start duo -- audit the deps", Budget: 1, BudgetPeriod: "day", Status: status}}
}

func TestAutoScreenListsDetailsAndPausesWithOneKey(t *testing.T) {
	fixedClock(t)
	a := newAutoScreen(&driver.Hello{Implemented: []string{"trigger.list", "trigger.create", "trigger.pause"}})
	a.apply(autoOutcome{rows: []driver.TriggerRow{
		func() driver.TriggerRow {
			r := rowOf("nightly", "every:30m", "active")
			r.Next = "2026-10-07T12:30:00Z"
			r.Record.LastFiredAt, r.Record.LastStatus = "2026-10-07T12:00:00Z", "started"
			r.Missed = 2
			return r
		}(),
		func() driver.TriggerRow { r := rowOf("old", "every:1h", "paused"); r.NextAbsent = "paused"; return r }(),
	}, teams: []string{"duo"}})

	var st fold.State
	a.publish(&st)
	if len(st.HubRows) != 3 || st.HubRows[0].Label != "＋ New automation…" {
		t.Fatalf("rows = %+v", st.HubRows)
	}
	if st.HubRows[1].Status != "every 30 minutes · next 2026-10-07 09:30" || st.HubRows[2].Status != "paused" {
		t.Errorf("statuses = %q / %q", st.HubRows[1].Status, st.HubRows[2].Status)
	}

	if _, task := a.key(term.Key{Type: term.KeyDown}); task != nil {
		t.Fatal("moving started work")
	}
	a.publish(&st)
	for _, want := range []string{"duo: audit the deps", "every 30 minutes", "$1.00 per day", "2 runs while arxi-tui was closed", "Press p to pause it"} {
		if !strings.Contains(st.HubDetail, want) {
			t.Errorf("detail lacks %q:\n%s", want, st.HubDetail)
		}
	}
	if strings.Contains(st.HubDetail, "arxi trigger") || strings.Contains(st.HubDetail, "arxi ") {
		t.Errorf("the detail teaches a command:\n%s", st.HubDetail)
	}
	_, task := a.key(keyP())
	if task == nil || task.pause != "nightly" || a.working == "" {
		t.Fatalf("p: task=%+v working=%q", task, a.working)
	}
	if _, again := a.key(keyP()); again != nil {
		t.Error("a second press while the first is in flight started another pause")
	}
	a.apply(autoOutcome{rows: []driver.TriggerRow{rowOf("nightly", "every:30m", "paused")}, name: "nightly", paused: "automation nightly paused"})
	a.publish(&st)
	if !strings.Contains(st.HubDetail, "✓ automation nightly paused") || a.sel != 1 {
		t.Errorf("after pausing: sel=%d\n%s", a.sel, st.HubDetail)
	}
	if _, task := a.key(keyP()); task != nil || !strings.Contains(a.banner, "already paused") {
		t.Errorf("pausing a paused one: task=%v banner=%q", task, a.banner)
	}
}

func keyP() term.Key { return term.Key{Type: term.KeyRunes, Runes: []rune{'p'}} }

func TestAutoScreenOnlyEscAndQCloseIt(t *testing.T) {
	a := newAutoScreen(&driver.Hello{})
	for _, r := range "zxcv pP" {
		if closeIt, _ := a.key(term.Key{Type: term.KeyRunes, Runes: []rune{r}}); closeIt {
			t.Fatalf("%q closed the screen", r)
		}
	}
	if closeIt, _ := a.key(term.Key{Type: term.KeyEscape}); !closeIt {
		t.Error("Esc did not close")
	}
	if closeIt, _ := a.key(term.Key{Type: term.KeyRunes, Runes: []rune{'q'}}); !closeIt {
		t.Error("q did not close")
	}
}

func TestAutoScreenSaysWhyItCannotCreate(t *testing.T) {
	a := newAutoScreen(&driver.Hello{Implemented: []string{"trigger.list"}})
	a.apply(autoOutcome{})
	a.key(term.Key{Type: term.KeyEnter})
	if !strings.Contains(a.banner, "cannot create files yet") || a.form != nil {
		t.Errorf("old core: banner=%q", a.banner)
	}
	a = newAutoScreen(&driver.Hello{Implemented: []string{"trigger.create"}})
	a.apply(autoOutcome{})
	a.key(term.Key{Type: term.KeyEnter})
	if !strings.Contains(a.banner, "Create one in /team first") || a.form != nil {
		t.Errorf("no teams: banner=%q", a.banner)
	}
}

func TestAutoScreenKeepsTheFormOpenOnARefusal(t *testing.T) {
	a := newAutoScreen(&driver.Hello{Implemented: []string{"trigger.create"}})
	a.apply(autoOutcome{teams: []string{"duo"}})
	a.key(term.Key{Type: term.KeyEnter})
	if a.form == nil {
		t.Fatal("the form did not open")
	}
	a.working = "Saving …"
	a.apply(autoOutcome{refused: "trigger \"n\" already exists"})
	if a.form == nil || a.working != "" || !strings.Contains(a.banner, "already exists") {
		t.Errorf("form=%v working=%q banner=%q", a.form, a.working, a.banner)
	}
}

func TestSchedulerLineIsHonestAboutWhetherAutomationsFire(t *testing.T) {
	a := &autoScreen{}
	if a.schedulerLine() != "" {
		t.Error("a session with no scheduler said something")
	}
	a.hasScheduler = true
	if got := a.schedulerLine(); !strings.Contains(got, "will start firing") {
		t.Errorf("idle: %q", got)
	}
	a.schedRunning = true
	if got := a.schedulerLine(); !strings.Contains(got, "fire while arxi-tui stays open") {
		t.Errorf("running: %q", got)
	}
	a.schedRunning, a.schedWhy = false, "the scheduler stopped"
	if got := a.schedulerLine(); !strings.Contains(got, "not firing: the scheduler stopped") {
		t.Errorf("stopped: %q", got)
	}
}

func TestActiveTriggersCountsOnlyTheActiveOnes(t *testing.T) {
	root := t.TempDir()
	if activeTriggers(root) != 0 {
		t.Fatal("no folder, no automations")
	}
	dir := filepath.Join(root, triggersDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"a.json": `{"status":"active"}`, "b.json": `{"status":"paused"}`, "c.json": `not json`, "d.txt": `{"status":"active"}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := activeTriggers(root); got != 1 {
		t.Errorf("active = %d, want 1", got)
	}
}

// ---- workers ---------------------------------------------------------------

type fakeAutoCore struct {
	*fakeTeamCore
	rows    []driver.TriggerRow
	created []driver.TriggerCreateParams
	paused  []string
	refuse  string
}

func (f *fakeAutoCore) Hello() *driver.Hello {
	return &driver.Hello{Implemented: []string{"blueprint.validate", "trigger.list", "trigger.create", "trigger.pause"}}
}

func (f *fakeAutoCore) SubmitTriggerList(context.Context) (*driver.TriggerListResult, error) {
	return &driver.TriggerListResult{Triggers: f.rows}, nil
}

func (f *fakeAutoCore) SubmitTriggerCreate(_ context.Context, p driver.TriggerCreateParams) (*driver.TriggerRow, error) {
	if f.refuse != "" {
		return nil, errors.New(f.refuse)
	}
	f.created = append(f.created, p)
	r := rowOf(p.Name, p.On, "active")
	f.rows = append(f.rows, r)
	return &r, nil
}

func (f *fakeAutoCore) SubmitTriggerPause(_ context.Context, name string) (*driver.TriggerRow, error) {
	if f.refuse != "" {
		return nil, errors.New(f.refuse)
	}
	f.paused = append(f.paused, name)
	for i := range f.rows {
		if f.rows[i].Record.Name == name {
			f.rows[i].Record.Status = "paused"
		}
	}
	return &f.rows[0], nil
}

func newFakeAutoCore(t *testing.T) (*fakeAutoCore, string) {
	root := t.TempDir()
	writeAgents(t, root, "duo.yaml", "broken.yaml")
	return &fakeAutoCore{fakeTeamCore: &fakeTeamCore{infos: map[string]*driver.BlueprintInfo{"duo.yaml": featureTeamInfo()}}}, root
}

func waitAuto(t *testing.T, ch <-chan autoOutcome) autoOutcome {
	t.Helper()
	select {
	case o := <-ch:
		return o
	case <-time.After(5 * time.Second):
		t.Fatal("the worker never answered")
		return autoOutcome{}
	}
}

func TestReadAutoOffersOnlyTeamsTheCoreAccepts(t *testing.T) {
	core, root := newFakeAutoCore(t)
	core.rows = []driver.TriggerRow{rowOf("n", "every:1h", "active")}
	ch := make(chan autoOutcome, 1)
	startAutoRead(context.Background(), core, root, ch)
	o := waitAuto(t, ch)
	if len(o.rows) != 1 || len(o.teams) != 1 || o.teams[0] != "duo" {
		t.Fatalf("outcome = %+v", o)
	}
}

func TestCreateAndPauseWorkersReReadAndLandOnTheRow(t *testing.T) {
	core, root := newFakeAutoCore(t)
	ch := make(chan autoOutcome, 1)
	startAutoCreate(context.Background(), core, root, driver.TriggerCreateParams{Name: "n", On: "every:1h", Then: "run start duo -- x", Budget: 1, BudgetPeriod: "day"}, ch)
	o := waitAuto(t, ch)
	if o.created == "" || o.name != "n" || len(o.rows) != 1 || !strings.Contains(o.created, "every 1 hour") {
		t.Fatalf("create outcome = %+v", o)
	}
	startAutoPause(context.Background(), core, root, "n", ch)
	o = waitAuto(t, ch)
	if o.paused == "" || o.rows[0].Record.Status != "paused" {
		t.Fatalf("pause outcome = %+v", o)
	}
	core.refuse = "no such trigger"
	startAutoPause(context.Background(), core, root, "ghost", ch)
	if o = waitAuto(t, ch); o.refused != "no such trigger" {
		t.Fatalf("refusal outcome = %+v", o)
	}
}

// ---- the real loop ---------------------------------------------------------

type autoHubCore struct {
	hubCore
	*fakeAutoCore
}

func (c autoHubCore) Hello() *driver.Hello { return c.fakeAutoCore.Hello() }
func (c autoHubCore) SubmitModelList(ctx context.Context) (*driver.ModelListResult, error) {
	return c.fakeAutoCore.SubmitModelList(ctx)
}

type autoTestDriver struct {
	testDriver
	core    hubCore
	started int
	running bool
}

func (d *autoTestDriver) Hub() hubCore { return d.core }
func (d *autoTestDriver) EnsureScheduler(context.Context) error {
	d.started++
	d.running = true
	return nil
}
func (d *autoTestDriver) SchedulerState() (bool, string) { return d.running, "" }

func typeKeys(s string) []scheduledEvent {
	var out []scheduledEvent
	for _, r := range s {
		out = append(out, scheduledEvent{5 * time.Millisecond, keyEvent(r)})
	}
	return out
}

// TestLoopAutoCreatesAnAutomationAndStartsTheScheduler drives the real loop with no
// command typed anywhere: /auto, Enter on "New automation", fill the form, Enter.
func TestLoopAutoCreatesAnAutomationAndStartsTheScheduler(t *testing.T) {
	fixedClock(t)
	core, root := newFakeAutoCore(t)
	t.Chdir(root)
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatal(err)
	}
	drv := &autoTestDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}, core: autoHubCore{fakeAutoCore: core}}

	key := func(k term.KeyType) scheduledEvent {
		return scheduledEvent{15 * time.Millisecond, term.Event{Kind: term.EventKey, Key: term.Key{Type: k}}}
	}
	script := []scheduledEvent{{120 * time.Millisecond, keyEvent('/')}}
	script = append(script, typeKeys("auto")...)
	script = append(script, scheduledEvent{40 * time.Millisecond, enterEvent()}, scheduledEvent{200 * time.Millisecond, enterEvent()})
	script = append(script, typeKeys("nightly")...)
	script = append(script, key(term.KeyEnter)) // Team
	script = append(script, key(term.KeyEnter)) // Task
	script = append(script, typeKeys("audit the deps")...)
	script = append(script, key(term.KeyEnter)) // Repeat
	script = append(script, key(term.KeyEnter)) // Timing
	script = append(script, typeKeys("45m")...)
	for i := 0; i < 5; i++ { // Budget, Per, Model, Rehearsal, then save
		script = append(script, key(term.KeyEnter))
	}
	script = append(script,
		scheduledEvent{400 * time.Millisecond, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyEscape}}},
		scheduledEvent{100 * time.Millisecond, ctrlCharEvent('c')},
		scheduledEvent{50 * time.Millisecond, ctrlCharEvent('c')})

	tty := newFakeTTY(110, 40, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	if len(core.created) != 1 {
		t.Fatalf("created %d automations; frames:\n%s", len(core.created), stripANSI(tty.output()))
	}
	p := core.created[0]
	if p.Name != "nightly" || p.On != "every:45m" || p.Then != "run start duo -- audit the deps" || p.Budget != 1 || p.BudgetPeriod != "day" {
		t.Errorf("created = %+v", p)
	}
	if drv.started == 0 {
		t.Error("saving an automation did not start the scheduler")
	}
	out := stripANSI(tty.output())
	for _, want := range []string{"Automations", "New automation", "every 45 minutes", "automation nightly saved", "fire while arxi-tui stays open"} {
		if !strings.Contains(out, want) {
			t.Errorf("no frame shows %q", want)
		}
	}
	if strings.Contains(out, "arxi trigger") {
		t.Error("the screen taught a command")
	}
	if len(drv.submitted) != 0 {
		t.Errorf("/auto reached the driver as %q", drv.submitted)
	}
	frames := strings.Split(tty.output(), "\x1b[?2026h")
	if last := stripANSI(frames[len(frames)-1]); strings.Contains(last, "Automations ·") || strings.Contains(last, "esc close") {
		t.Errorf("Esc did not close the screen:\n%s", last)
	}
}

// ---- the scheduler child ---------------------------------------------------

func TestSchedulerNeedsACoreAndSaysWhyWhenItCannotStart(t *testing.T) {
	var s schedulerProc
	if err := s.ensure(context.Background(), "", t.TempDir()); err == nil {
		t.Error("starting with no core succeeded")
	}
	if err := s.ensure(context.Background(), filepath.Join(t.TempDir(), "missing"), t.TempDir()); err == nil {
		t.Error("starting a missing binary succeeded")
	}
	if running, why := s.state(); running || why == "" {
		t.Errorf("state = %v, %q", running, why)
	}
	d := &serveDriver{}
	if err := d.EnsureScheduler(context.Background()); err == nil || !strings.Contains(err.Error(), "no arxi core") {
		t.Errorf("a driver with no core: %v", err)
	}
}

func TestRealCoreAutomationsRoundTripAndTheSchedulerFiresOne(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the real core")
	}
	bin := buildCore(t)
	work := t.TempDir()
	t.Setenv("ARXI_BIN", bin)
	t.Setenv("HOME", work)
	t.Setenv("USERPROFILE", work)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(work, "cfg"))
	t.Chdir(work)

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
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
	ac, ok := hc.Hub().(autoCore)
	if !ok || !helloImplements(ac.Hello(), "trigger.create") || !helloImplements(ac.Hello(), "trigger.pause") {
		t.Fatal("the real core must implement the trigger verbs")
	}

	tc := hc.Hub().(teamCore)
	if _, err := tc.SubmitAgentCreate(ctx, driver.AgentCreateParams{Name: "solo", Role: "implementer", Tools: []string{"read"}}); err != nil {
		t.Fatal(err)
	}
	if o := readAuto(ctx, ac, work); o.err != "" || len(o.rows) != 0 || len(o.teams) != 1 || o.teams[0] != "solo" {
		t.Fatalf("fresh read = %+v", o)
	}

	f := newAutoForm([]string{"solo"}, nil)
	fillForm(t, f, map[string]string{"Name": "tick", "Task": "say hello", "Timing": "1m"})
	f.focus = 8
	f.flip(1) // Rehearsal: nothing is spent
	task, msg := f.submit()
	if msg != "" {
		t.Fatal(msg)
	}
	row, err := ac.SubmitTriggerCreate(ctx, *task.auto)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if row.Record.Status != "active" || row.Next == "" || describeOn(row.Record.On) != "every 1 minute" {
		t.Fatalf("created = %+v", row)
	}
	if _, err := ac.SubmitTriggerCreate(ctx, *task.auto); err == nil {
		t.Error("a taken name was accepted")
	}
	bad := *task.auto
	bad.Name, bad.On = "bad", "every:10s"
	if _, err := ac.SubmitTriggerCreate(ctx, bad); err == nil {
		t.Error("a schedule below the floor was accepted")
	}

	// The invisible scheduler fires it: wait for a run folder to appear. It is
	// stopped and waited for before the test ends, so that on Windows nothing still
	// holds the working folder when it is removed.
	schedCtx, stopSched := context.WithCancel(ctx)
	defer func() {
		stopSched()
		for i := 0; i < 100; i++ {
			if running, _ := drv.(autoScheduler).SchedulerState(); !running {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
	if err := drv.(autoScheduler).EnsureScheduler(schedCtx); err != nil {
		t.Fatalf("EnsureScheduler: %v", err)
	}
	if err := drv.(autoScheduler).EnsureScheduler(ctx); err != nil {
		t.Fatalf("a second EnsureScheduler: %v", err)
	}
	if running, _ := drv.(autoScheduler).SchedulerState(); !running {
		t.Fatal("the scheduler is not running")
	}
	deadline := time.Now().Add(100 * time.Second)
	for {
		if es, _ := os.ReadDir(filepath.Join(work, "runs")); len(es) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the scheduler never fired the automation")
		}
		time.Sleep(time.Second)
	}

	if _, err := ac.SubmitTriggerPause(ctx, "tick"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	o := readAuto(ctx, ac, work)
	if len(o.rows) != 1 || o.rows[0].Record.Status != "paused" || o.rows[0].NextAbsent != "paused" || o.rows[0].Record.LastFiredAt == "" {
		t.Fatalf("after pause = %+v", o.rows)
	}
	if activeTriggers(work) != 0 {
		t.Error("a paused automation still counts as active")
	}
}
