package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const addGoCommand = `{"commands":["/ui add node below_input {\"id\":\"go\",\"type\":\"button\",\"text\":\"Go\",\"on_press\":\"cmd:/help\"}"]}`

const goTest = `{"save":"go opens the menu","keys":["tab","enter"],"height":40,"expect":["Commands","/shell"]}`

// scenarioRig is a bridge whose interface has the Go button, with its own config folder.
func scenarioRig(t *testing.T) *bridgeRig {
	t.Helper()
	t.Setenv(configDirEnv, t.TempDir())
	r := newBridgeRig(t, "ask")
	if res := r.edit(t, addGoCommand, true); !res.OK {
		t.Fatalf("the button was not added: %+v", res)
	}
	r.b.publish(r.doc, r.b.below, nil, nil, behaviour{})
	return r
}

func TestASavedTestSaysAtOnceWhetherItPassesNow(t *testing.T) {
	r := scenarioRig(t)
	got := r.preview(t, goTest)
	if !got.OK || !strings.Contains(got.Text, "PASSES on the interface on screen now") {
		t.Errorf("a test of a working button did not say it passes:\n%+v\nConsequence: the model saves a check without knowing it ever worked.\nRemedy: run the test once when it is saved and say how it went.", got)
	}
	bad := r.preview(t, `{"save":"wrong","keys":["tab","enter"],"expect":["no such words on screen"]}`)
	if !bad.OK || !strings.Contains(bad.Text, "FAILS on the interface on screen now") || !strings.Contains(bad.Text, "no such words on screen") {
		t.Errorf("a test that cannot pass was not reported as failing:\n%+v\nConsequence: a check that never passed is trusted as one that does.\nRemedy: report the failure with what it expected, and what the screen showed.", bad)
	}
}

func TestAnEditThatBreaksASavedTestSaysSoAndKeepsTheChange(t *testing.T) {
	r := scenarioRig(t)
	r.preview(t, goTest)
	// Removing the button: the edit is valid and gets applied, and the button no longer works.
	res := r.edit(t, `{"commands":["/ui remove go"]}`, true)
	if !res.OK || !strings.Contains(res.Text, "applied and saved") {
		t.Fatalf("the edit that removes the button was not applied: %+v", res)
	}
	for _, want := range []string{"TESTS: 1 of 1 FAIL", `"go opens the menu"`, "The change IS applied", "expected \"Commands\""} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("the answer of the edit lacks %q:\n%s\nConsequence: the model breaks a button it checked earlier and says done.\nRemedy: run the saved tests on the new interface and put the failures in the answer.", want, res.Text)
		}
	}
}

func TestAnEditThatKeepsASavedTestPassingSaysSo(t *testing.T) {
	r := scenarioRig(t)
	r.preview(t, goTest)
	res := r.edit(t, `{"colors":{"markdown.code":"fg=magenta"}}`, true)
	if !strings.Contains(res.Text, `TESTS: all 1 pass after this change ("go opens the menu")`) {
		t.Errorf("a harmless edit did not report the tests passing:\n%s\nConsequence: the model cannot tell that nothing broke from not having looked.\nRemedy: say that the tests ran and passed.", res.Text)
	}
}

func TestWithNoSavedTestsAnEditAnswersAsBefore(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	r := newBridgeRig(t, "ask")
	res := r.edit(t, addGoCommand, true)
	if strings.Contains(res.Text, "TESTS") {
		t.Errorf("an edit with no tests mentions tests:\n%s\nConsequence: every answer pays tokens for a feature nobody uses.\nRemedy: say nothing when there is nothing saved.", res.Text)
	}
}

func TestATestIsRunOnTheChangeNotOnWhatWasOnScreenBefore(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	r := newBridgeRig(t, "ask")
	// Saved before the button exists: it fails now, and passes once the edit adds the button.
	if got := r.preview(t, goTest); !strings.Contains(got.Text, "FAILS") {
		t.Fatalf("the test should fail with no button yet: %+v", got)
	}
	res := r.edit(t, addGoCommand, true)
	if !strings.Contains(res.Text, "TESTS: all 1 pass") {
		t.Errorf("the test was run on the interface before the edit:\n%s\nConsequence: the model never sees a test turn green after the fix it wrote.\nRemedy: run the tests on the document the edit produced.", res.Text)
	}
}

func TestATestCanDemandThatSomethingIsNotThere(t *testing.T) {
	r := scenarioRig(t)
	got := r.preview(t, `{"save":"no stray text","keys":["tab","enter"],"expect":["!Commands"]}`)
	if !strings.Contains(got.Text, "FAILS") || !strings.Contains(got.Text, `"Commands" must not be there and it is`) {
		t.Errorf("a ! expectation did not fail when the text is on screen:\n%+v", got)
	}
	ok := r.preview(t, `{"save":"no stray text","keys":["down"],"expect":["!Commands"]}`)
	if !strings.Contains(ok.Text, "replaced") || !strings.Contains(ok.Text, "PASSES") {
		t.Errorf("the same name did not replace the test, or its ! expectation did not pass:\n%+v", ok)
	}
}

func TestATestSeesWhatWasSentToTheModel(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	r := newBridgeRig(t, "ask")
	got := r.preview(t, `{"save":"a line goes out","keys":["type:hola","enter"],"expect":["SENT TO THE MODEL (1): \"hola\""]}`)
	if !strings.Contains(got.Text, "PASSES") {
		t.Errorf("a test cannot expect what was sent to the model:\n%+v\nConsequence: a button that should send a line cannot be kept as a test.\nRemedy: check expect against the same text ui_preview answers, not the screen alone.", got)
	}
}

func TestAnUnreasonableTestIsRefusedWithTheRemedy(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	r := newBridgeRig(t, "ask")
	for name, c := range map[string]struct{ args, want string }{
		"nothing to expect": {`{"save":"x","keys":["down"]}`, "cannot fail"},
		"empty piece":       {`{"save":"x","keys":["down"],"expect":["!"]}`, "is empty"},
		"two rows":          {`{"save":"x","keys":["down"],"expect":["a\nb"]}`, "one row"},
		"bad name":          {`{"save":"../x","keys":["down"],"expect":["a"]}`, "test name"},
		"bad key":           {`{"save":"x","keys":["bogus"],"expect":["a"]}`, "is not a key"},
		"bad size":          {`{"save":"x","width":3,"expect":["a"]}`, "width must be"},
		"with a change":     {`{"save":"x","expect":["a"],"change":` + addGoCommand + `}`, "apply the change with ui_edit first"},
		"save and forget":   {`{"save":"x","forget":"y","expect":["a"]}`, "not both"},
		"unknown forget":    {`{"forget":"nothing"}`, `no test named "nothing"`},
		"expect alone":      {`{"expect":["a"]}`, "pass save"},
	} {
		res := r.preview(t, c.args)
		if res.OK || !strings.Contains(res.Text, c.want) {
			t.Errorf("%s: got %+v; want a refusal naming %q.\nConsequence: a test that is saved wrong passes or fails for a reason the model did not mean.\nRemedy: refuse it and say what to write instead.", name, res, c.want)
		}
	}
	if list, _ := loadScenarios(); len(list) != 0 {
		t.Errorf("a refused test was saved: %+v", list)
	}
}

func TestThereAreAtMostTwelveTestsAndTheThirteenthSaysWhatToDo(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	r := newBridgeRig(t, "ask")
	var list []scenario
	for i := 0; i < maxScenarios; i++ {
		list = append(list, scenario{Name: "t" + string(rune('a'+i)), Keys: []string{"down"}, Expect: []string{"x"}})
	}
	if err := saveScenarios(list); err != nil {
		t.Fatal(err)
	}
	res := r.preview(t, `{"save":"one more","keys":["down"],"expect":["x"]}`)
	if res.OK || !strings.Contains(res.Text, "forget one first") {
		t.Errorf("the 13th test was accepted or the refusal gives no way out: %+v\nConsequence: every ui_edit would run an unbounded number of copies of the program.\nRemedy: cap the tests and say how to make room.", res)
	}
}

func TestPlanModeCannotChangeTheTests(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	r := newBridgeRig(t, "plan")
	res := r.preview(t, `{"save":"x","keys":["down"],"expect":["a"]}`)
	if res.OK || !strings.Contains(res.Text, "plan mode") {
		t.Errorf("a test was saved in plan mode: %+v\nConsequence: plan mode, which promises that nothing changes, writes a file.\nRemedy: refuse it like ui_edit does.", res)
	}
	if _, err := os.Stat(scenariosPath()); err == nil {
		t.Errorf("a file was written in plan mode")
	}
}

func TestForgettingATestStopsItRunning(t *testing.T) {
	r := scenarioRig(t)
	r.preview(t, goTest)
	if res := r.preview(t, `{"forget":"go opens the menu"}`); !res.OK || !strings.Contains(res.Text, "0 left") {
		t.Fatalf("the test was not deleted: %+v", res)
	}
	if _, err := os.Stat(scenariosPath()); err == nil {
		t.Errorf("deleting the last test left an empty file")
	}
	res := r.edit(t, `{"colors":{"markdown.code":"fg=magenta"}}`, true)
	if strings.Contains(res.Text, "TESTS") {
		t.Errorf("a deleted test still ran:\n%s", res.Text)
	}
}

func TestTheTestsAreNotPartOfTheInterfaceSoUndoAndResetKeepThem(t *testing.T) {
	r := scenarioRig(t)
	// A saved interface, so that reset has something to keep as the one undo returns to.
	if err := saveInterface(r.doc, nil, nil, behaviour{}); err != nil {
		t.Fatal(err)
	}
	if cur, _ := currentInterface(); cur.Scene == "" {
		t.Fatal("the interface with the button was not saved, so this test would prove nothing")
	}
	r.preview(t, goTest)
	if _, _, _, _, err := resetInterface(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := undoInterface(); err != nil {
		t.Fatal(err)
	}
	if list, err := loadScenarios(); err != nil || len(list) != 1 {
		t.Errorf("/ui reset took the tests with it: %v %v\nConsequence: undoing a change erases the very checks that say the change broke something.\nRemedy: keep the tests in their own file, outside the interface that undo swaps.", list, err)
	}
	cur, _ := currentInterface()
	if strings.Contains(cur.Scene+cur.Tokens+cur.Texts+cur.Behaviour, "go opens the menu") {
		t.Errorf("the tests are inside the saved interface")
	}
}

func TestAProposalLookedAtWithPreviewDoesNotRunTheTests(t *testing.T) {
	r := scenarioRig(t)
	r.preview(t, goTest)
	got := r.preview(t, `{"change":`+addGoCommand+`}`).Text
	if strings.Contains(got, "TESTS") {
		t.Errorf("looking at a proposal ran the saved tests:\n%s\nConsequence: every look costs a run of every test.\nRemedy: run them only when an edit is applied.", got)
	}
}

func TestAnUnreadableTestsFileIsSaidNotSkipped(t *testing.T) {
	r := scenarioRig(t)
	if err := os.WriteFile(scenariosPath(), []byte(`{"tests": [{"name":"x","bogus":1}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	res := r.edit(t, `{"colors":{"markdown.code":"fg=magenta"}}`, true)
	if !strings.Contains(res.Text, "TESTS were not run") {
		t.Errorf("a broken tests file was ignored:\n%s\nConsequence: the model believes its checks passed when none ran.\nRemedy: say the tests were not run and why.", res.Text)
	}
	save := r.preview(t, goTest)
	if save.OK || !strings.Contains(save.Text, "no test was changed") {
		t.Errorf("a test was saved over a file that could not be read: %+v", save)
	}
}

func TestTheGuideListsTheSavedTestsAndHowToKeepOne(t *testing.T) {
	r := scenarioRig(t)
	if g := r.b.guideText(); !strings.Contains(g, "Saved tests now: none") {
		t.Errorf("the guide does not say there are no tests")
	}
	r.preview(t, goTest)
	g := r.b.guideText()
	for _, want := range []string{"TESTS.", `"go opens the menu"`, "forget:", "NOT rerun after what the user types"} {
		if !strings.Contains(g, want) {
			t.Errorf("the guide lacks %q.\nConsequence: the model neither saves tests nor knows which exist, and one breaks unseen.\nRemedy: list them in ui_guide next to how to save them.", want)
		}
	}
}

func TestTestsFileIsPlainJSONInTheConfigFolder(t *testing.T) {
	r := scenarioRig(t)
	r.preview(t, goTest)
	b, err := os.ReadFile(filepath.Join(configDir(), "ui_tests.json"))
	if err != nil || !strings.Contains(string(b), `"keys": [`) || !strings.Contains(string(b), `"expect": [`) {
		t.Errorf("the saved tests are not readable JSON in the config folder: %v\n%s", err, b)
	}
}
