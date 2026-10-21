//go:build !windows

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/defaultscene"
	"github.com/michiTrader/arxi_tui/internal/driver"
	extv2 "github.com/michiTrader/arxi_tui/internal/ext/v2"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// The seven events that came after the first eight: what happened during a turn (a tool
// used, a file changed, an approval asked, the provider failing), the window nearly full,
// the model switched, a conversation resumed.

type evSpec struct{ id, on, is string }

// eventsPackage writes and approves a package "pn" with one event per spec. Each event
// appends a line to counters/<id> and says NOTE-<id>, so a test can count how many times
// each really ran.
func eventsPackage(t *testing.T, specs []evSpec) (counters string) {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	t.Setenv(historyEnv, t.TempDir())
	t.Setenv(sessionsEnv, "")
	counters = t.TempDir()
	var evs []string
	files := map[string]string{}
	for _, s := range specs {
		is := ""
		if s.is != "" {
			is = `,"is":"` + s.is + `"`
		}
		evs = append(evs, `{"id":"`+s.id+`","on":"`+s.on+`"`+is+`,"op":{"kind":"exec","argv":["./`+s.id+`.sh"]}}`)
		files["pn/"+s.id+".sh"] = "#!/bin/sh\necho x >> " + filepath.Join(counters, s.id) + "\nprintf 'NOTE-" + s.id + "'\n"
	}
	files["pn/package.json"] = execPkg("pn", "", `"events":[`+strings.Join(evs, ",")+`]`)
	for name, body := range files {
		mode := os.FileMode(0o644)
		if strings.HasSuffix(name, ".sh") {
			mode = 0o755
		}
		write(t, filepath.Join(cfg, packagesDirName, filepath.FromSlash(name)), body, mode)
	}
	cwd, _ := os.Getwd()
	p, ok := loadAllPackages(cwd, behaviour{}).find("pn")
	if !ok {
		t.Fatal("the package did not load")
	}
	if err := grantPackage(p, cwd); err != nil {
		t.Fatal(err)
	}
	return counters
}

// autoOver is a packageAuto loaded with the packages eventsPackage wrote.
func autoOver(t *testing.T) *packageAuto {
	t.Helper()
	cwd, _ := os.Getwd()
	set := loadAllPackages(cwd, behaviour{})
	a := newPackageAuto(context.Background(), make(chan autoMsg, 256))
	a.reload(set, newPackageRunner(set))
	return a
}

func ran(counters, id string) int {
	b, _ := os.ReadFile(filepath.Join(counters, id))
	return strings.Count(string(b), "x")
}

// Each new event fires only its own packages' events, and none of the older ones.
func TestEachOfTheSevenNewEventsFiresOnlyItsOwn(t *testing.T) {
	fire := map[string]func(a *packageAuto){
		"tool.used":       func(a *packageAuto) { a.toolUsed("read") },
		"provider.error":  (*packageAuto).providerError,
		"approval.asked":  func(a *packageAuto) { a.approvalAsked("run") },
		"context.high":    (*packageAuto).contextHigh,
		"model.changed":   func(a *packageAuto) { a.modelChanged("p/m") },
		"file.changed":    (*packageAuto).fileChanged,
		"session.resumed": (*packageAuto).sessionResumed,
		// the older ones must not run them either
		"turn.done": func(a *packageAuto) { a.turnDone("") }, "turn.failed": (*packageAuto).turnFailed,
		"turn.started": (*packageAuto).turnStarted, "turn.cancelled": (*packageAuto).turnCancelled,
		"shell.ended": (*packageAuto).shellEnded,
	}
	var specs []evSpec
	for on := range fire {
		specs = append(specs, evSpec{id: strings.ReplaceAll(on, ".", "-"), on: on})
	}
	for _, own := range []string{"tool.used", "provider.error", "approval.asked", "context.high", "model.changed", "file.changed", "session.resumed"} {
		counters := eventsPackage(t, specs)
		a := autoOver(t)
		fire[own](a)
		id := strings.ReplaceAll(own, ".", "-")
		waitUntil(t, own+" ran", func() bool { return ran(counters, id) == 1 })
		time.Sleep(150 * time.Millisecond)
		for _, s := range specs {
			if s.id != id && ran(counters, s.id) != 0 {
				t.Errorf("%s also ran the event of %s", own, s.on)
			}
		}
		a.stopAll()
	}
}

// tool.used, approval.asked and model.changed carry a value, and "is" narrows to it.
func TestAnEventThatCarriesAValueRunsOnlyForThatValue(t *testing.T) {
	counters := eventsPackage(t, []evSpec{
		{"anytool", "tool.used", ""}, {"onlyedit", "tool.used", "edit"},
		{"askrun", "approval.asked", "run"}, {"askwrite", "approval.asked", "write"},
		{"modela", "model.changed", "p/a"}, {"modelb", "model.changed", "p/b"},
	})
	a := autoOver(t)
	defer a.stopAll()
	a.toolUsed("read")
	a.approvalAsked("run")
	a.modelChanged("p/a")
	waitUntil(t, "the matching events", func() bool {
		return ran(counters, "anytool") == 1 && ran(counters, "askrun") == 1 && ran(counters, "modela") == 1
	})
	time.Sleep(150 * time.Millisecond)
	for _, id := range []string{"onlyedit", "askwrite", "modelb"} {
		if n := ran(counters, id); n != 0 {
			t.Errorf("%s ran %d times for a value it is not narrowed to", id, n)
		}
	}
	a.toolUsed("edit")
	waitUntil(t, "tool.used for edit", func() bool { return ran(counters, "onlyedit") == 1 })
}

// "is" is checked against what the host knows, so a typo is refused when the package is
// written, not silently never run.
func TestTheValuesOfTheNewEventsAreCheckedWhenThePackageIsWritten(t *testing.T) {
	check := func(on, is string) error {
		doc := execPkg("pn", "", `"events":[{"id":"e","on":"`+on+`","is":"`+is+`","op":{"kind":"exec","argv":["./e.sh"]}}]`)
		pkg, err := extv2.Parse([]byte(doc))
		if err != nil {
			return err
		}
		return pkg.Validate(packageNames(behaviour{}).options())
	}
	for _, ok := range [][2]string{{"tool.used", "read"}, {"tool.used", "shell_start"}, {"approval.asked", "folder"},
		{"tool.used", "pkg_weather_now"}, {"model.changed", "anything/at-all"}, {"effort", "high"}} {
		if err := check(ok[0], ok[1]); err != nil {
			t.Errorf("%s is %s was refused: %v", ok[0], ok[1], err)
		}
	}
	err := check("tool.used", "raed")
	if err == nil || !strings.Contains(err.Error(), "raed") || !strings.Contains(err.Error(), "read") {
		t.Errorf("a misspelled tool must be refused and the real names listed, got %v", err)
	}
	for _, on := range []string{"provider.error", "context.high", "file.changed", "session.resumed", "shell.ended", "turn.done"} {
		if err := check(on, "x"); err == nil {
			t.Errorf("%s carries no value, yet \"is\" was accepted", on)
		}
	}
}

func TestContextHighFiresOnceWhenTheWindowFillsAndAgainOnlyAfterItDrops(t *testing.T) {
	steps := []struct {
		was     bool
		percent int
		high    bool
		fire    bool
	}{
		{false, 0, false, false}, // unknown window or empty
		{false, contextHighPercent - 1, false, false},
		{false, contextHighPercent, true, true},
		{true, contextHighPercent + 5, true, false}, // still high: not news
		{true, 30, false, false},                    // compacted
		{false, 99, true, true},                     // filled again
	}
	for i, s := range steps {
		high, fire := contextHighCrossed(s.was, s.percent)
		if high != s.high || fire != s.fire {
			t.Errorf("step %d (was %v, %d%%): got high=%v fire=%v, want %v %v", i, s.was, s.percent, high, fire, s.high, s.fire)
		}
	}
}

func TestOnlyTheModelsOwnSuccessfulEditsCountAsAFileChanged(t *testing.T) {
	for _, c := range []struct {
		tool string
		ok   bool
		want bool
	}{
		{"edit", true, true}, {"write", true, true}, {"edit", false, false}, {"write", false, false},
		{"read", true, false}, {"grep", true, false}, {"run", true, false}, {"shell_start", true, false},
	} {
		if got := changedFile(c.tool, c.ok); got != c.want {
			t.Errorf("changedFile(%s, %v) = %v, want %v", c.tool, c.ok, got, c.want)
		}
	}
}

// A model call that failed is marked, and only that: the loop fires provider.error for it.
func TestAFailedModelCallIsMarkedAsAProviderError(t *testing.T) {
	core := &seenChat{fail: func(driver.ChatSendParams) error { return errors.New("the provider said 402") }}
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	c.setWorkdir(t.TempDir())
	var errEv *fold.Event
	for _, e := range sendAndDrain(t, c, out, "hola") {
		e := e
		if e.Type == "chat.error" {
			errEv = &e
		}
	}
	if errEv == nil {
		t.Fatal("no error line for a failed call")
	}
	if marked, _ := errEv.Payload[providerErrorKey].(bool); !marked {
		t.Errorf("the failure of the model call is not marked: %+v", errEv.Payload)
	}
	if !strings.Contains(errEv.Payload["text"].(string), "402") {
		t.Errorf("the error text lost the provider's words: %v", errEv.Payload["text"])
	}
}

// A core that could not even be reached is not the provider failing.
func TestACoreThatCannotBeReachedIsNotAProviderError(t *testing.T) {
	out := make(chan fold.Event, 64)
	c := newChatSession(&seenChat{}, out)
	c.setWorkdir(t.TempDir())
	c.dial = func(context.Context) (chatSender, func(), error) { return nil, nil, errors.New("no core") }
	var errEv *fold.Event
	for _, e := range sendAndDrain(t, c, out, "hola") {
		e := e
		if e.Type == "chat.error" {
			errEv = &e
		}
	}
	if errEv == nil {
		t.Fatal("no error line")
	}
	if marked, _ := errEv.Payload[providerErrorKey].(bool); marked {
		t.Error("an unreachable core was marked as the provider failing")
	}
}

// sendAndDrain sends one message and returns the events until the turn ends.
func sendAndDrain(t *testing.T, c *chatSession, out chan fold.Event, text string) []fold.Event {
	t.Helper()
	if err := c.send(context.Background(), text); err != nil {
		t.Fatal(err)
	}
	var got []fold.Event
	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-out:
			got = append(got, e)
			if e.Type == "agent.failed" || e.Type == "agent.turn_done" {
				// the error line follows agent.failed
				time.Sleep(100 * time.Millisecond)
				for {
					select {
					case e := <-out:
						got = append(got, e)
					default:
						return got
					}
				}
			}
		case <-deadline:
			t.Fatal("the turn never ended")
		}
	}
}

func loopWithPackage(t *testing.T, drv Driver, evCh chan fold.Event, script []scheduledEvent) string {
	t.Helper()
	doc, err := scene.ParseNamed(defaultscene.Name, defaultscene.JSON)
	if err != nil {
		t.Fatal(err)
	}
	tty := newFakeTTY(110, 34, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	return screen(tty.output())
}

// The loop turns what the chat really emits into the new package events, with the right
// value, and not for what only looks alike (a read is not a file change, an error that is
// not the model's is not a provider error, a window that stays full is not news).
func TestLoopTurnsChatEventsIntoTheNewPackageEvents(t *testing.T) {
	counters := eventsPackage(t, []evSpec{
		{"tool", "tool.used", ""}, {"toolsedit", "tool.used", "edit"}, {"file", "file.changed", ""},
		{"askrun", "approval.asked", "run"}, {"perr", "provider.error", ""}, {"ctxhigh", "context.high", ""},
	})
	core := &modelCore{def: "openai/mimo-pro", models: []driver.ModelRow{{Provider: "openai", ID: "mimo-pro", Enabled: true, ContextWindow: 1_000_000}}}
	drv := &compactDriver{hubTestDriver: hubTestDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64), seq: 1000}, hub: core}}
	send := func(typ string, p map[string]any) {
		drv.evCh <- fold.Event{Type: typ, Seq: int64(drv.seq + 1), Payload: p}
		drv.seq++
		time.Sleep(160 * time.Millisecond)
	}
	go func() {
		time.Sleep(700 * time.Millisecond) // the startup read of the model's window
		send("chat.tool", map[string]any{"name": "read", "arg": "a.go", "ok": true})
		send("chat.tool", map[string]any{"name": "edit", "arg": "a.go", "ok": true})
		send("chat.tool", map[string]any{"name": "write", "arg": "b.go", "ok": false})
		send("chat.error", map[string]any{"text": "plain error"})
		send("chat.error", map[string]any{"text": "the provider said no", providerErrorKey: true})
		answered(&compactDriver{hubTestDriver: drv.hubTestDriver}, 900_000)
		time.Sleep(160 * time.Millisecond)
		answered(&compactDriver{hubTestDriver: drv.hubTestDriver}, 950_000) // still full: not news
		time.Sleep(160 * time.Millisecond)
		send("chat.approval", map[string]any{"name": "run", "arg": "ls"})
	}()
	s := loopWithPackage(t, drv, drv.evCh, exitAfter(3200))
	want := map[string]int{"tool": 3, "toolsedit": 1, "file": 1, "askrun": 1, "perr": 1, "ctxhigh": 1}
	for id, n := range want {
		if got := ran(counters, id); got != n {
			t.Errorf("event %s ran %d times, want %d", id, got, n)
		}
	}
	if !strings.Contains(s, "NOTE-perr") {
		t.Errorf("the provider.error note is not on screen; last frame:\n%s", s)
	}
}

// Switching the model through /model fires model.changed with the ref picked. (A pick that
// fails goes down another branch of the loop, where nothing fires; not tested separately.)
func TestLoopSwitchingTheModelFiresModelChanged(t *testing.T) {
	counters := eventsPackage(t, []evSpec{
		{"any", "model.changed", ""}, {"deep", "model.changed", "tokenharbor/deepseek-v4.1-flash:free"}, {"other", "model.changed", "openai/gpt-4o"},
	})
	core := &modelCore{def: "openai/gpt-4o"}
	core.models = sampleModels().models
	drv := &hubTestDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}, hub: core}
	script := append([]scheduledEvent{}, typed("/model ")...)
	script = append(script, scheduledEvent{300 * time.Millisecond, keyEvent('x')})
	script = append(script, scheduledEvent{5 * time.Millisecond, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyBackspace}}})
	script = append(script, typed("deeps flash")...)
	script = append(script, scheduledEvent{50 * time.Millisecond, enterEvent()})
	script = append(script, exitAfter(700)...)
	loopWithPackage(t, drv, drv.evCh, script)
	if core.current() != "tokenharbor/deepseek-v4.1-flash:free" {
		t.Fatalf("the model is %q: the pick did not happen, so the test proves nothing", core.current())
	}
	if ran(counters, "any") != 1 || ran(counters, "deep") != 1 {
		t.Errorf("model.changed ran any=%d deep=%d times, want 1 and 1", ran(counters, "any"), ran(counters, "deep"))
	}
	if ran(counters, "other") != 0 {
		t.Error("an event narrowed to another model ran")
	}
}

// /resume brings a conversation back and fires session.resumed once.
func TestLoopResumingAConversationFiresSessionResumed(t *testing.T) {
	counters := eventsPackage(t, []evSpec{{"back", "session.resumed", ""}})
	t.Setenv(sessionsEnv, t.TempDir())
	drv := &resumeDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}
	drv.evCh <- fold.Event{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "remember zebra"}}
	drv.evCh <- fold.Event{Type: "llm.response", Seq: 3, Payload: map[string]any{"text": "noted: zebra", "model": "p/m"}}
	script := []scheduledEvent{{200 * time.Millisecond, keyEvent(' ')}, {10 * time.Millisecond, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyBackspace}}}}
	script = append(script, typed("/clear")...)
	script = append(script, scheduledEvent{40 * time.Millisecond, enterEvent()})
	script = append(script, typed("/resume ")...)
	script = append(script, scheduledEvent{80 * time.Millisecond, enterEvent()})
	script = append(script, exitAfter(500)...)
	loopWithPackage(t, drv, drv.evCh, script)
	drv.mu.Lock()
	resumed := len(drv.resumed)
	drv.mu.Unlock()
	if resumed != 2 {
		t.Fatalf("the conversation was not resumed (history %d): the test proves nothing", resumed)
	}
	if n := ran(counters, "back"); n != 1 {
		t.Errorf("session.resumed ran %d times, want 1 (and /clear must not count as a resume)", n)
	}
}
