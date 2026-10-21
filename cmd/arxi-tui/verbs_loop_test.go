//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// Scenario 3 finished: a row of the shells list that stops its own shell, through the real
// loop and the real press path.
func TestScenario3AStopButtonOnEachShellRowStopsThatShell(t *testing.T) {
	reg := simRegistry(2)
	doc := `{"root":{"type":"stack","children":[
	  {"id":"chat","type":"markdown","bind":"chat.history","grow":1},
	  {"id":"shell_list","type":"list","bind":"host.shell.rows",
	   "row_template":{"id":"shell_row","type":"text","text":"[stop {row.id}] {row.command}","on_press":"do:shell.stop {row.id}"}},
	  {"id":"prompt","type":"input","bind":"user.input","placeholder":"say something"}]}}`
	script := []scheduledEvent{
		{80 * time.Millisecond, arrowEvent(term.KeyTab)},
		{40 * time.Millisecond, arrowEvent(term.KeyTab)}, // the second row
		{40 * time.Millisecond, enterEvent()},
		{300 * time.Millisecond, ctrlCharEvent('c')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
	}
	out, drv := loopWithShells(t, doc, reg, script)
	if reg.running() != 1 {
		t.Fatalf("%d shells run after pressing the stop of row 2, want 1\nConsequence: the button does nothing, or stops the wrong shell.\nRemedy: do:shell.stop {row.id} must reach runQueued with the row expanded.", reg.running())
	}
	if all := reg.list(); all[0].State != shellRunning || all[1].State == shellRunning {
		t.Errorf("states %v %v: the shell of row 2 must be the one stopped", all[0].State, all[1].State)
	}
	if !strings.Contains(stripANSI(lastFrame(out)), "stopped shell 2") {
		t.Errorf("the conversation does not say shell 2 was stopped:\n%s", lastFrame(out))
	}
	if len(drv.submitted) != 0 {
		t.Errorf("pressing the button sent %q to the model", drv.submitted)
	}
}

// A button may not carry the verb that starts a program: the scene is refused at load.
func TestAButtonCannotCarryTheVerbThatStartsAProgram(t *testing.T) {
	for _, press := range []string{"do:shell.start touch X", "do:shell.start {row.command}"} {
		d, err := scene.ParseDocument([]byte(`{"root":{"type":"list","bind":"host.shell.rows","row_template":{"type":"text","text":"x","on_press":"` + press + `"}}}`))
		if err != nil {
			t.Fatal(err)
		}
		err = d.Validate()
		if err == nil || !strings.Contains(err.Error(), "only a line the user types") {
			t.Errorf("%s validates (err %v)\nConsequence: a button the model drew starts a program the user did not choose.\nRemedy: the validator refuses a typed verb in on_press.", press, err)
		}
	}
}

// The other door, cmd:/shell start, is a command line and so valid in a scene. Before this
// change a button with it started a real program (found by running it: the file appeared).
// Now the loop knows the line was not typed and refuses.
func TestAButtonWithShellStartInItStartsNothing(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "started")
	doc := `{"root":{"type":"stack","children":[
	 {"id":"chat","type":"markdown","bind":"chat.history","grow":1},
	 {"type":"button","id":"go","text":"[go]","on_press":"cmd:/shell start touch ` + mark + `"},
	 {"type":"input","bind":"user.input","prefix":"> "}]}}`
	reg := newShellRegistry(nil)
	defer reg.stopAll()
	script := []scheduledEvent{{80 * time.Millisecond, arrowEvent(term.KeyTab)}, {40 * time.Millisecond, enterEvent()}, {500 * time.Millisecond, ctrlCharEvent('c')}, {50 * time.Millisecond, ctrlCharEvent('c')}}
	out, _ := loopWithShells(t, doc, reg, script)
	if _, err := os.Stat(mark); err == nil || len(reg.list()) != 0 {
		t.Errorf("a button started a program\nConsequence: what a button says it does and what runs can differ; the model draws buttons.\nRemedy: runShellCommand takes !synth and refuses a start that was not typed.")
	}
	if !strings.Contains(stripANSI(lastFrame(out)), "type it yourself") {
		t.Errorf("the button said nothing about the refusal:\n%s", lastFrame(out))
	}
}

func TestAKeyACommandAndAHookCannotCarryTheTypedVerbEither(t *testing.T) {
	act := "do:shell.start touch X"
	for who, b := range map[string]behaviour{
		"key":     {Keys: map[string]actionList{"f5": {act}}},
		"command": {Commands: []userCommand{{Name: "go", Category: "Custom", Description: "d", Run: actionList{act}}}},
		"hook":    {Hooks: []userHook{{On: "effort", Do: actionList{act}}}},
	} {
		if err := b.validate(); err == nil {
			t.Errorf("a %s carrying %s is accepted\nConsequence: a shortcut or a hook starts a program with nobody typing.\nRemedy: validateActions refuses a typed verb.", who, act)
		}
	}
	// the free verbs are fine anywhere
	b := behaviour{Keys: map[string]actionList{"f5": {"do:shell.stop all"}}}
	if err := b.validate(); err != nil {
		t.Errorf("a key that stops the shells is refused: %v", err)
	}
}

// The last line of defence: even if a typed verb gets past every check, runVerb itself
// refuses it when the line was not typed, and starts nothing.
func TestRunVerbRefusesATypedVerbThatWasNotTyped(t *testing.T) {
	dir := t.TempDir()
	mark := filepath.Join(dir, "started")
	reg := newShellRegistry(nil)
	defer reg.stopAll()
	got := runVerbLine("shell.start touch "+mark, verbEnv{shells: reg, dir: dir, typed: false})
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(mark); err == nil || len(reg.list()) != 0 {
		t.Errorf("a program started from a line nobody typed (%q)\nConsequence: the label of a button decides what runs.\nRemedy: runVerb checks the protection before the implementation.", got)
	}
	if !strings.Contains(got, "type it yourself") {
		t.Errorf("the refusal does not tell the user what to do: %q", got)
	}
	got = runVerbLine("shell.start touch "+mark, verbEnv{shells: reg, dir: dir, typed: true})
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(mark); err != nil {
		t.Errorf("the same verb typed by the user did not run (%q): %v", got, err)
	}
}

// The user typing /shell start still works, and /shell start from a line the loop made
// (a button through cmd:, a shortcut, a command) does not.
func TestTypedShellStartRunsAndASyntheticOneDoesNot(t *testing.T) {
	dir := t.TempDir()
	reg := newShellRegistry(nil)
	defer reg.stopAll()
	if got := runShellCommand("start sleep 5", reg, dir, true); !strings.Contains(got, "started shell 1") {
		t.Errorf("typed: %q", got)
	}
	if got := runShellCommand("start sleep 5", reg, dir, false); !strings.Contains(got, "type it yourself") || len(reg.list()) != 1 {
		t.Errorf("synthetic /shell start was not refused: %q (%d shells)\nConsequence: a shortcut or hook of the user's behaviour starts a program.\nRemedy: pass !synth to runShellCommand.", got, len(reg.list()))
	}
}

// What comes out of the action queue is never a typed line. The validators refuse a typed
// verb before it gets there, so only the environment itself can show the last wall holds.
func TestWhatTheActionQueueRunsIsNeverTreatedAsTyped(t *testing.T) {
	dir := t.TempDir()
	mark := filepath.Join(dir, "started")
	drv := &shellTestDriver{reg: newShellRegistry(nil)}
	defer drv.reg.stopAll()
	env := queuedVerbEnv(drv, dir)
	if env.typed {
		t.Fatal("the queue's environment says the line was typed\nConsequence: a typed verb that slipped past the validators starts a program from a button.\nRemedy: queuedVerbEnv leaves typed false.")
	}
	if env.shells != drv.reg {
		t.Error("the queue's environment does not reach the shells of the driver, so no button can stop one")
	}
	got := runVerbLine("shell.start touch "+mark, env)
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(mark); err == nil || len(drv.reg.list()) != 0 {
		t.Errorf("a program started through the queue's environment (%q)", got)
	}
}
