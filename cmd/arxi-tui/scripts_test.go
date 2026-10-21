//go:build !windows

package main

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
)

const counterSrc = `
let n = 0
function run(text, facts) {
  n = n + (text === '' ? 1 : Number(text))
  return 'count ' + String(n) + ' in ' + facts.mode
}`

func scriptBeh(name, src string) behaviour {
	return behaviour{Scripts: map[string]string{name: src}}
}

func wantErr(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want one with %q", parts)
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("the error lacks %q: %v", p, err)
		}
	}
}

func TestCheckScriptRefusesWhatCannotRunAndSaysWhatToWrite(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"Bad Name", "function run() {}", "lowercase letters"},
		{"x", "   ", "is empty"},
		{"x", "function go() { return null }", "has no `run`"},
		{"x", "function run() { return new Date() }", "new"},
		{"x", "function run() { return nope }", "nope"},
		{"x", "function run( {", "syntax error"},
		{"x", strings.Repeat("let a = 1\n", maxScriptBytes/8), "split it in two"},
	}
	for _, c := range cases {
		err := checkScript(c.name, c.src)
		wantErr(t, err, c.want)
	}
	if err := checkScript("ok-1", "function run(text, facts) { return null }"); err != nil {
		t.Errorf("a good script was refused: %v", err)
	}
}

func TestABehaviourWithABadScriptDoesNotLoad(t *testing.T) {
	// Consequence: a script that only fails when a key is pressed would break at the worst time.
	// Remedy: the document is refused when it is saved, with the script's own message.
	b := scriptBeh("x", "function run() { return 1 +")
	wantErr(t, b.validate(), "scripts:", "x.js")
	if err := scriptBeh("x", counterSrc).validate(); err != nil {
		t.Errorf("a good script was refused: %v", err)
	}
	many := behaviour{Scripts: map[string]string{}}
	for i := 0; i <= maxUserScripts; i++ {
		many.Scripts["s"+strings.Repeat("a", i)] = counterSrc
	}
	wantErr(t, many.validate(), "the most is")
}

func TestAScriptKeepsItsStateBetweenCallsAndStartsOverWhenItsSourceChanges(t *testing.T) {
	h := newScriptHost()
	b := scriptBeh("counter", counterSrc)
	facts := map[string]any{"mode": "ask"}
	for i, want := range []string{"count 1 in ask", "count 2 in ask", "count 5 in ask"} {
		text := ""
		if i == 2 {
			text = "3"
		}
		r, err := h.call(b, "counter", text, facts, false)
		if err != nil || r.note != want {
			t.Fatalf("call %d: note %q err %v, want %q. Consequence: a counter or a state machine cannot work if its state is lost.", i+1, r.note, err, want)
		}
	}
	// The same name with other source is another program: it starts from its top level.
	b = scriptBeh("counter", strings.Replace(counterSrc, "'count '", "'now '", 1))
	r, err := h.call(b, "counter", "", facts, false)
	if err != nil || r.note != "now 1 in ask" {
		t.Errorf("after the source changed: %q %v, want the new text and a fresh count. Consequence: an edited script keeps running its old state or its old code.", r.note, err)
	}
}

func TestAScriptThatIsNotThereNamesTheOnesThatAre(t *testing.T) {
	h := newScriptHost()
	_, err := h.call(behaviour{}, "x", "", nil, false)
	wantErr(t, err, "none is defined")
	_, err = h.call(scriptBeh("counter", counterSrc), "countr", "", nil, false)
	wantErr(t, err, "the scripts are counter")
}

func TestARunErrorCarriesTheScriptNameLineAndRemedy(t *testing.T) {
	h := newScriptHost()
	b := scriptBeh("bad", "function run(text, facts) {\n  if (text) { return 'x' }\n  return null\n}")
	_, err := h.call(b, "bad", "hi", map[string]any{}, false)
	wantErr(t, err, "script.run bad", "bad.js:2", "Remedy")
}

func TestWhatAScriptReturnsIsCheckedLikeAShortcut(t *testing.T) {
	// Consequence if the returned actions were not checked: a script could start a typed
	// verb, answer an approval, or loop through itself, which no shortcut can.
	// Remedy: the same validateActions runs on the list, and a script cannot start a script.
	h := newScriptHost()
	run := func(ret string, fromHook bool) (scriptResult, error) {
		return h.call(scriptBeh("s", "function run(t, f) { return "+ret+" }"), "s", "", map[string]any{}, fromHook)
	}
	if r, err := run(`['cmd:/effort max', 'say:hi']`, false); err != nil || len(r.actions) != 2 {
		t.Fatalf("good actions refused: %v", err)
	}
	for _, c := range []struct {
		ret, want string
		hook      bool
	}{
		{`['do:shell.start rm -rf x']`, "only a line you type yourself", false},
		{`['answer:approve']`, "on your behalf", false},
		{`['do:script.run s']`, "cannot start a script", false},
		{`['ext:a:b']`, "without a keypress", true},
		{`['nonsense']`, "script s", false},
		{`[1]`, "not a text", false},
		{`7`, "neither a text nor a list", false},
	} {
		_, err := run(c.ret, c.hook)
		wantErr(t, err, c.want)
	}
	if r, err := run(`null`, false); err != nil || r.note != "" || len(r.actions) != 0 {
		t.Errorf("null should do nothing: %+v %v", r, err)
	}
}

func TestAScriptCannotReachThePiecesOfTheProgram(t *testing.T) {
	h := newScriptHost()
	for _, src := range []string{
		"function run() { return require('fs') }",
		"function run() { return process.env }",
		"function run() { return eval('1') }",
	} {
		_, err := h.call(scriptBeh("s", src), "s", "", map[string]any{}, false)
		wantErr(t, err, "script.run s")
	}
}

func TestAScriptFromAProjectLayerWinsByNameAndAddsTheRest(t *testing.T) {
	user := behaviour{Scripts: map[string]string{"a": "function run() { return 'user' }", "b": "function run() { return 'b' }"}}
	proj := behaviour{Scripts: map[string]string{"a": "function run() { return 'project' }"}}
	m := mergeBehaviour(user, proj)
	if !strings.Contains(m.Scripts["a"], "project") || !strings.Contains(m.Scripts["b"], "'b'") {
		t.Errorf("merged scripts = %v. Consequence: a project cannot ship its own script, or it wipes the user's others.", m.Scripts)
	}
}

func TestAPatchAddsReplacesAndRemovesScripts(t *testing.T) {
	src := "function run() { return null }"
	base := behaviour{}
	p, err := parseBehaviourPatch([]byte(`{"scripts":{"one":"` + src + `"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !p.touchesWhatRuns() {
		t.Error("a script must be put to the user: it decides what their keyboard does")
	}
	next, err := applyBehaviourPatch(base, p)
	if err != nil || next.Scripts["one"] != src {
		t.Fatalf("add: %v %v", next.Scripts, err)
	}
	p, _ = parseBehaviourPatch([]byte(`{"scripts":{"one":null}}`))
	next, err = applyBehaviourPatch(next, p)
	if err != nil || len(next.Scripts) != 0 {
		t.Errorf("remove: %v %v", next.Scripts, err)
	}
	p, _ = parseBehaviourPatch([]byte(`{"scripts":{"bad":"function run( {"}}`))
	if _, err := applyBehaviourPatch(base, p); err == nil {
		t.Error("a script that does not parse was accepted by a patch; Consequence: the agent saves a script that breaks on the first key. Remedy: validate on every save.")
	}
}

// ---- through the real loop

func scriptLoopBehaviour(t *testing.T, keys map[string]string, scripts map[string]string) {
	t.Helper()
	doc, err := json.Marshal(map[string]any{"keys": keys, "scripts": scripts})
	if err != nil {
		t.Fatal(err)
	}
	seedBehaviour(t, string(doc))
}

func TestLoopAKeyCallsAScriptAndTheScriptKeepsItsCountBetweenPresses(t *testing.T) {
	// Consequence if the state were lost between presses (or the verb never reached the script):
	// a counter or a state machine written by the user would always say "count 1" or nothing.
	// Remedy: one scriptHost for the whole loop, called through do:script.run.
	scriptLoopBehaviour(t, map[string]string{"f5": "do:script.run counter"}, map[string]string{"counter": counterSrc})
	drv := &modeEffortDriver{effortDriver: effortDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}}
	script := append([]scheduledEvent{fkey(term.KeyF5), fkey(term.KeyF5), fkey(term.KeyF5)}, quitScript...)
	tty := runLoop(t, drv, drv.evCh, script)
	if frameWith(tty.output(), "count 3 in") == "" {
		t.Errorf("the third press did not show `count 3`; last frame:\n%s", lastFrame(tty.output()))
	}
}

func TestLoopAScriptThatReturnsActionsRunsThemLikeAShortcut(t *testing.T) {
	// The script picks the effort from the mode: here it asks for `high`, as `set:` would.
	src := `function run(text, facts) { return facts.effort === 'high' ? ['say:already high'] : ['set:effort=high', 'say:made it high'] }`
	scriptLoopBehaviour(t, map[string]string{"f5": "do:script.run pick"}, map[string]string{"pick": src})
	drv := &modeEffortDriver{effortDriver: effortDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}}
	script := append([]scheduledEvent{fkey(term.KeyF5), fkey(term.KeyF5)}, quitScript...)
	tty := runLoop(t, drv, drv.evCh, script)
	out := tty.output()
	if frameWith(out, "made it high") == "" || frameWith(out, "already high") == "" {
		t.Errorf("the first press should set the effort and the second should see it (facts.effort); last frame:\n%s", lastFrame(out))
	}
}

func TestLoopAScriptThatFailsSaysSoOnScreenAndTheKeyStillWorks(t *testing.T) {
	// Consequence: a script with a mistake would freeze or silently do nothing.
	// Remedy: the error (script, line, remedy) is a note, and the next press runs again.
	src := "let n = 0\nfunction run(text, facts) {\n  n = n + 1\n  if (n === 1) { return facts.nothing.deep }\n  return 'second try ' + String(n)\n}"
	scriptLoopBehaviour(t, map[string]string{"f5": "do:script.run flaky"}, map[string]string{"flaky": src})
	drv := &modeEffortDriver{effortDriver: effortDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}}
	script := append([]scheduledEvent{fkey(term.KeyF5), fkey(term.KeyF5)}, quitScript...)
	tty := runLoop(t, drv, drv.evCh, script)
	out := tty.output()
	if frameWith(out, "flaky.js:4") == "" {
		t.Errorf("the failure is not on screen with its line; last frame:\n%s", lastFrame(out))
	}
	if frameWith(out, "second try 2") == "" {
		t.Errorf("the key did not work again after a failed call (and the failed call's own change must stay: n is 2); last frame:\n%s", lastFrame(out))
	}
}

func TestLoopAScriptCannotTurnAHookIntoAnEndlessLoop(t *testing.T) {
	// A hook calls a script; the script returns the setting change that fires the same hook.
	// Consequence if that looped: the interface would never come back. Remedy: what a hook's
	// action does never starts another hook (the rule hooks already follow).
	src := `function run(text, facts) { return ['set:effort=low', 'say:RAN'] }`
	seedBehaviour(t, `{"keys":{"f5":"set:effort=high"},"hooks":[{"on":"effort","is":"high","do":"do:script.run bounce"},{"on":"effort","is":"low","do":"say:LOOP"}],"scripts":{"bounce":`+strconv.Quote(src)+`}}`)
	drv := &modeEffortDriver{effortDriver: effortDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}}
	script := append([]scheduledEvent{fkey(term.KeyF5)}, quitScript...)
	tty := runLoop(t, drv, drv.evCh, script)
	if frameWith(tty.output(), "RAN") == "" {
		t.Errorf("the hook's script did not run; last frame:\n%s", lastFrame(tty.output()))
	}
	if got := frameWith(tty.output(), "LOOP"); got != "" {
		t.Errorf("the script's `set:` fired another hook: %s", got)
	}
}

func TestLoopAHooksScriptCannotReachAPluginThatAKeyCould(t *testing.T) {
	// Consequence if the loop did not tell the script it was called by a hook: a hook, which
	// fires with no key pressed, could send a press to a plugin through a script. A hook never
	// could, when written as plain actions. Remedy: the loop passes q.hook on, and the list a
	// script returns is checked as a hook's.
	src := `function run(text, facts) { return ['ext:any:thing'] }`
	seedBehaviour(t, `{"keys":{"f5":"set:effort=high"},"hooks":[{"on":"effort","is":"high","do":"do:script.run sneaky"}],"scripts":{"sneaky":`+strconv.Quote(src)+`}}`)
	drv := &modeEffortDriver{effortDriver: effortDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}}
	script := append([]scheduledEvent{fkey(term.KeyF5)}, quitScript...)
	tty := runLoop(t, drv, drv.evCh, script)
	if frameWith(tty.output(), "without a keypress") == "" {
		t.Errorf("the hook's script was not refused for returning ext:; last frame:\n%s", lastFrame(tty.output()))
	}
}

func TestTheGuideTeachesScriptsAndItsOwnExampleRuns(t *testing.T) {
	// Consequence if the guide had no scripts, or an example that fails: the model never offers
	// a script, or copies a broken one and fails the same way on every try.
	// Remedy: the example is taken out of the guide text and run here.
	g := behaviourGuide(behaviour{})
	for _, must := range []string{"do:script.run", "function run(text, facts)", "ONLY thing the script does", "Always put to the user"} {
		if !strings.Contains(g, must) {
			t.Errorf("the guide lacks %q", must)
		}
	}
	i := strings.Index(g, "  scripts     {\"counter\": ")
	if i < 0 {
		t.Fatal("the guide has no example script")
	}
	line := g[i+len("  scripts     "):]
	line = line[:strings.Index(line, "\n")]
	var ex map[string]string
	if err := json.Unmarshal([]byte(line), &ex); err != nil {
		t.Fatalf("the example in the guide is not JSON: %v", err)
	}
	h := newScriptHost()
	b := behaviour{Scripts: ex}
	if err := b.validate(); err != nil {
		t.Fatalf("the guide's own example is refused: %v", err)
	}
	r, err := h.call(b, "counter", "", map[string]any{"effort": "high"}, false)
	if err != nil || r.note != "pressed 1 times, effort high" {
		t.Errorf("the guide's example gave %q, %v", r.note, err)
	}
}
