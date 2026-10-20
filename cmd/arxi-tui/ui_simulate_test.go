package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const addGoButton = `{"change":{"commands":["/ui add node below_input {\"id\":\"go\",\"type\":\"button\",\"text\":\"Go\",\"on_press\":\"cmd:/help\"}"]},`

func TestKeysPressTheProposedInterfaceForReal(t *testing.T) {
	r := newBridgeRig(t, "ask")
	// Tab reaches the button the proposal adds and Enter presses it: its action opens the
	// command menu. A picture cannot say that; only pressing the keys can.
	got := r.preview(t, addGoButton+`"keys":["tab","enter"],"height":40}`)
	if !got.OK || !strings.Contains(got.Text, "Commands") || !strings.Contains(got.Text, "/shell") {
		t.Errorf("pressing tab, enter on the new button did not open the command menu:\n%s\nConsequence: the model says the button works without having pressed it.\nRemedy: run the keys through the real loop, on the proposal.", got.Text)
	}
	// The same keys on the interface on screen, which has no button, do something else.
	plain := r.preview(t, `{"keys":["tab","enter"],"height":40}`)
	if strings.Contains(plain.Text, "Commands") {
		t.Errorf("the keys opened the menu on an interface with no button:\n%s", plain.Text)
	}
}

func TestKeysShowWhatWouldBeSentToTheModel(t *testing.T) {
	r := newBridgeRig(t, "ask")
	got := r.preview(t, `{"keys":["type:hola mundo","enter"]}`).Text
	if !strings.Contains(got, `SENT TO THE MODEL (1): "hola mundo"`) {
		t.Errorf("the line typed and sent is not reported:\n%s\nConsequence: the model cannot tell a button that sends a line from one that does nothing.\nRemedy: list what the simulated driver was asked to submit.", got)
	}
	if got := r.preview(t, `{"keys":["type:/help","enter"]}`).Text; strings.Contains(got, "SENT TO THE MODEL") {
		t.Errorf("a slash command was reported as sent to the model:\n%s", got)
	}
}

func TestKeysNeverTouchTheUsersFolders(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(historyEnv, dir)
	t.Setenv(configDirEnv, dir)
	r := newBridgeRig(t, "ask")
	r.preview(t, `{"keys":["type:una frase para el historial","enter","type:/ui reset","enter"]}`)
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("pressing keys in the simulation wrote into the user's folder: %v.\nConsequence: a look at a proposal edits the history and the saved interface of the person using the program.\nRemedy: give the copy its own folders through its environment.", names)
	}
}

func TestKeysNeverRunARealCommand(t *testing.T) {
	dir := t.TempDir()
	mark := filepath.Join(dir, "marker")
	r := newBridgeRig(t, "ask")
	r.preview(t, `{"keys":["type:/shell start touch `+mark+`","enter"],"shells":1}`)
	if _, err := os.Stat(mark); err == nil {
		t.Errorf("a command typed in the simulation ran for real and created %s.\nConsequence: the model, testing a button that starts a shell, starts one on the user's machine.\nRemedy: the simulation's shells run nothing.", mark)
	}
}

func TestKeysShowTheRefusalOfALinkNoPackageAllows(t *testing.T) {
	r := newBridgeRig(t, "ask")
	add := `{"change":{"commands":["/ui add node below_input {\"id\":\"web\",\"type\":\"button\",\"text\":\"Docs\",\"on_press\":\"open:https://example.com/docs\"}"]},"keys":["tab","enter"]}`
	got := r.preview(t, add).Text
	// The real loop refuses an open: that no approved package permits, and says so on the
	// screen. The simulation has no packages, so the model sees that very refusal: a link
	// button needs a package, and now it learns it before the user does.
	if !strings.Contains(got, "is not a host any approved package may open") {
		t.Errorf("the refusal the real loop gives is not on the simulated screen:\n%s\nConsequence: the model builds a link button that will never open and says it works.\nRemedy: run the real loop, which holds the rule.", got)
	}
	if strings.Contains(got, "BROWSER WAS ASKED TO OPEN") {
		t.Errorf("the browser was asked to open a host no package allows:\n%s", got)
	}
}

func TestTheCopyIsGivenNothingOfTheUsersEnvironment(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-secret-value")
	t.Setenv("GITHUB_TOKEN", "ghp-secret-value")
	for _, kv := range simEnv(t.TempDir(), t.TempDir()) {
		if strings.Contains(kv, "secret-value") || strings.HasPrefix(kv, "OPENAI") || strings.HasPrefix(kv, "GITHUB") {
			t.Errorf("the copy receives %q.\nConsequence: an interface that runs a command could read the user's keys.\nRemedy: build the copy's environment from a short list, never from os.Environ.", kv)
		}
	}
	var path string
	for _, kv := range simEnv(t.TempDir(), t.TempDir()) {
		if strings.HasPrefix(kv, "PATH=") {
			path = kv
		}
	}
	if path == "" || strings.Contains(path, "/usr") {
		t.Errorf("the copy has a PATH that finds programs: %q", path)
	}
}

func TestKeysRefuseWhatTheyCannotPressAndSayWhy(t *testing.T) {
	r := newBridgeRig(t, "ask")
	many := `"` + strings.Repeat(`down","`, simMaxSteps) + `down"`
	for name, c := range map[string]struct{ args, want string }{
		"name":    {`{"keys":["bogus"]}`, `"bogus" is not a key`},
		"count":   {`{"keys":[` + many + `]}`, "at most"},
		"typed":   {`{"keys":["type:` + strings.Repeat("x", simMaxTyped+1) + `"]}`, "type: takes at most"},
		"shape":   {`{"keys":{"a":1}}`, "keys is a list"},
		"inspect": {`{"keys":["down"],"inspect":true}`, "inspect describes"},
	} {
		res := r.preview(t, c.args)
		if res.OK || !strings.Contains(res.Text, c.want) {
			t.Errorf("%s: got %+v; want a refusal naming %q.\nConsequence: a key that is silently skipped makes the model trust a result it did not get.\nRemedy: refuse the whole call and name the key.", name, res, c.want)
		}
	}
}

func TestKeysAcceptASpaceSeparatedString(t *testing.T) {
	r := newBridgeRig(t, "ask")
	if got := r.preview(t, `{"keys":"type:ok enter"}`); !got.OK || !strings.Contains(got.Text, `"ok"`) {
		t.Errorf(`"keys" as one string was not understood: %+v`, got)
	}
}

func TestKeysSayWhenTheProgramEnded(t *testing.T) {
	r := newBridgeRig(t, "ask")
	got := r.preview(t, `{"keys":["ctrl+c","ctrl+c","down"]}`).Text
	if !strings.Contains(got, "THE PROGRAM ENDED at key 2") {
		t.Errorf("the exit was not reported:\n%s\nConsequence: the model reads the screen as if the later keys had been pressed.\nRemedy: say at which key the program ended.", got)
	}
}

func TestParseSimKeysReadsNamesTypedTextAndPaste(t *testing.T) {
	steps, err := parseSimKeys([]string{"Down", "shift+tab", "type:ab", "paste:x\ny", "f2"})
	if err != nil || len(steps) != 5 {
		t.Fatalf("steps %v err %v", steps, err)
	}
	if steps[0].label != "down" || steps[1].label != "shift+tab" || len(steps[2].events) != 2 || len(steps[3].events) != 1 {
		t.Errorf("keys were not read as named: %+v", steps)
	}
}

func TestTheGuideTellsTheModelToPressKeysToCheckThatSomethingWorks(t *testing.T) {
	r := newBridgeRig(t, "ask")
	g := r.b.guideText()
	for _, want := range []string{"pass keys", "shift+tab", "Nothing in the copy is real"} {
		if !strings.Contains(g, want) {
			t.Errorf("the guide lacks %q.\nConsequence: the model never presses the keys of what it builds, and a button that cannot be reached ships as \"done\".\nRemedy: say in the guide when to use keys and that it is safe.", want)
		}
	}
}
