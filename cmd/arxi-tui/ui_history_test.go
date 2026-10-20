package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/ui"
)

// cols is a colour layer with one colour, written the way the user writes it.
func cols(t *testing.T, style string) userTokens {
	t.Helper()
	st, err := ui.ParseStyle(style)
	if err != nil {
		t.Fatal(err)
	}
	return userTokens{"markdown.code": st}
}

// histRig gives a test its own config folder and a clock that moves one minute per call.
func histRig(t *testing.T) {
	t.Helper()
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	tick := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	old := historyNow
	historyNow = func() time.Time { tick = tick.Add(time.Minute); return tick }
	t.Cleanup(func() { historyNow = old })
}

// saveColour saves the interface with one colour and records it, as the loop does.
func saveColour(t *testing.T, style, label string) {
	t.Helper()
	doc := builtinDoc(t)
	if err := saveInterface(doc, cols(t, style), nil, behaviour{}); err != nil {
		t.Fatal(err)
	}
	if err := recordVersion(label); err != nil {
		t.Fatal(err)
	}
}

func TestEveryChangeIsAVersionAndTheListSaysWhichOneIsNow(t *testing.T) {
	histRig(t)
	saveColour(t, "fg=red", "make code red")
	saveColour(t, "fg=blue", "make code blue")
	got, err := historyList()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"#1", "#2", "make code red", "make code blue", "colours", "now "} {
		if !strings.Contains(got, want) {
			t.Errorf("the list lacks %q:\n%s\nConsequence: the user cannot tell which version to go back to.\nRemedy: list every version with its number, label, what it changed and which one is on screen.", want, got)
		}
	}
	if strings.Index(got, "#2") > strings.Index(got, "#1") {
		t.Errorf("the list is not newest first:\n%s", got)
	}
}

func TestTheSameStateTwiceIsOneVersion(t *testing.T) {
	histRig(t)
	saveColour(t, "fg=red", "make code red")
	if err := recordVersion("again"); err != nil {
		t.Fatal(err)
	}
	h, _ := loadUIHistory()
	if len(h.Versions) != 1 {
		t.Errorf("%d versions after saving the same state twice.\nConsequence: the list fills with copies and the real changes fall off the end.\nRemedy: do not record a state equal to the last one.", len(h.Versions))
	}
}

func TestTheHistoryKeepsWhatUndoWouldHaveReturnedToSoItDoesNotBeginLate(t *testing.T) {
	histRig(t)
	saveColour(t, "fg=red", "first")
	// A second save makes /ui undo's file hold the first; the history started at "first",
	// so nothing is missing. Starting the history with an interface already changed:
	t.Setenv(configDirEnv, t.TempDir())
	doc := builtinDoc(t)
	if err := saveInterface(doc, cols(t, "fg=red"), nil, behaviour{}); err != nil {
		t.Fatal(err)
	}
	if err := saveInterface(doc, cols(t, "fg=blue"), nil, behaviour{}); err != nil {
		t.Fatal(err)
	}
	if err := recordVersion("blue"); err != nil {
		t.Fatal(err)
	}
	h, _ := loadUIHistory()
	if len(h.Versions) != 2 || h.Versions[0].Label != "before the history began" || !strings.Contains(h.Versions[0].State.Tokens, "red") {
		t.Errorf("the state before the first recorded change was not kept: %+v\nConsequence: whoever had changed things before this feature existed cannot go back to the look they had.\nRemedy: on the first record, keep the state /ui undo returns to.", h.Versions)
	}
}

func TestDiffShowsWhatChangedSinceAVersion(t *testing.T) {
	histRig(t)
	saveColour(t, "fg=red", "red")
	saveColour(t, "fg=blue", "blue")
	got, err := historyDiff("1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "colours") || !strings.Contains(got, "red") || !strings.Contains(got, "blue") {
		t.Errorf("the diff does not show the colour change:\n%s\nConsequence: the user restores blind.\nRemedy: show each document that differs, as /ui diffs are shown.", got)
	}
	same, _ := historyDiff("2")
	if !strings.Contains(same, "the same as #2") {
		t.Errorf("the diff against the current version says %q", same)
	}
}

func TestANamedVersionIsFoundByNameAndTheNameMoves(t *testing.T) {
	histRig(t)
	saveColour(t, "fg=red", "red")
	saveColour(t, "fg=blue", "blue")
	if _, err := historyName("1", "Before the demo"); err != nil {
		t.Fatal(err)
	}
	h, _ := loadUIHistory()
	if v, err := h.resolve("before the demo"); err != nil || v.ID != 1 {
		t.Errorf("the name was not found, case aside: %v %v", v, err)
	}
	note, err := historyName("2", "before the demo")
	if err != nil || !strings.Contains(note, "it was on #1") {
		t.Errorf("giving a name again did not say it moved: %q %v\nConsequence: two versions answer to one name and a restore picks the wrong one.\nRemedy: a name belongs to one version; say where it was.", note, err)
	}
	h, _ = loadUIHistory()
	if v, _ := h.resolve("Before the demo"); v.ID != 2 {
		t.Errorf("the name did not move to #2: %+v", v)
	}
}

func TestANumberAloneCannotBeAName(t *testing.T) {
	histRig(t)
	saveColour(t, "fg=red", "red")
	for _, bad := range []string{"3", "../x", "", strings.Repeat("a", maxVersionName+1)} {
		if _, err := historyName("", bad); err == nil {
			t.Errorf("the name %q was accepted.\nConsequence: /ui history restore 3 would mean two things.\nRemedy: names start with a letter.", bad)
		}
	}
}

func TestRestoreBringsAVersionBackAndUndoReturns(t *testing.T) {
	histRig(t)
	saveColour(t, "fg=red", "red")
	saveColour(t, "fg=blue", "blue")
	out := runHistoryLine("restore 1", true)
	if out.err != "" || !out.restored || !strings.Contains(out.state.Tokens, "red") {
		t.Fatalf("restore failed: %+v", out)
	}
	cur, _ := currentInterface()
	if !strings.Contains(cur.Tokens, "red") {
		t.Errorf("the saved interface is not the restored one: %s", cur.Tokens)
	}
	// The restore is a version of its own, and undo goes back to what the user had.
	h, _ := loadUIHistory()
	if last := h.Versions[len(h.Versions)-1]; !strings.Contains(last.Label, "restored #1") {
		t.Errorf("the restore was not recorded as a version: %+v", last)
	}
	if _, back, _, _, err := undoInterface(); err != nil || !strings.Contains(string(encodeUserTokens(back)), "blue") {
		t.Errorf("/ui undo after a restore did not bring back blue: %s %v\nConsequence: a restore destroys the work it replaced.\nRemedy: keep the saved state as the one undo returns to before restoring.", encodeUserTokens(back), err)
	}
}

func TestRestoreByNameAndRefusalsSayWhy(t *testing.T) {
	histRig(t)
	saveColour(t, "fg=red", "red")
	saveColour(t, "fg=blue", "blue")
	if _, err := historyName("1", "calm"); err != nil {
		t.Fatal(err)
	}
	if out := runHistoryLine("restore calm", true); out.err != "" || !strings.Contains(out.state.Tokens, "red") {
		t.Errorf("restore by name failed: %+v", out)
	}
	for args, want := range map[string]string{
		"restore 99":      "there is no version 99",
		"restore nothing": `no version named "nothing"`,
		"restore":         "say which version",
		"diff":            "say which version",
		"explode":         "not something history does",
		"name":            "name takes",
		"name 1":          "name the version with a word",
	} {
		if out := runHistoryLine(args, true); out.err == "" || !strings.Contains(out.err, want) {
			t.Errorf("%q: got %+v, want a refusal naming %q.\nConsequence: a typo restores nothing and says nothing.\nRemedy: say what was wrong and what to type.", args, out, want)
		}
	}
	if out := runHistoryLine("restore 1", false); out.err == "" || !strings.Contains(out.err, "-scene") {
		t.Errorf("a scene started with -scene was restored over: %+v", out)
	}
}

func TestAnUnreadableHistoryIsSaidAndLeftAlone(t *testing.T) {
	histRig(t)
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	const junk = `{"versions": [oops`
	if err := os.WriteFile(uiHistoryPath(), []byte(junk), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recordVersion("x"); err == nil || !strings.Contains(err.Error(), "left as it is") {
		t.Errorf("a broken history was not reported: %v", err)
	}
	if b, _ := os.ReadFile(uiHistoryPath()); string(b) != junk {
		t.Errorf("the broken history was overwritten with %q.\nConsequence: one bad byte erases every version the user had.\nRemedy: refuse to write over a file that cannot be read.", b)
	}
}

func TestTheOldestUnnamedVersionsGoFirstAndNamedOnesStay(t *testing.T) {
	histRig(t)
	saveColour(t, "fg=red", "v0")
	if _, err := historyName("1", "keeper"); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < maxVersions+10; i++ {
		saveColour(t, fmt.Sprintf("fg=#%06x", i), fmt.Sprintf("v%d", i))
	}
	h, _ := loadUIHistory()
	if len(h.Versions) != maxVersions {
		t.Errorf("%d versions kept, want %d", len(h.Versions), maxVersions)
	}
	if _, err := h.resolve("keeper"); err != nil {
		t.Errorf("the named version was dropped to make room: %v\nConsequence: the version the user marked as safe is the first to go.\nRemedy: drop unnamed versions first.", err)
	}
	if last := h.Versions[len(h.Versions)-1]; last.Label != fmt.Sprintf("v%d", maxVersions+9) {
		t.Errorf("the newest version is not the last: %+v", last.Label)
	}
}

func TestPackagesAreNotInTheHistory(t *testing.T) {
	histRig(t)
	doc := builtinDoc(t)
	// The change before the first recorded one touched a package, so the file /ui undo
	// reads holds a package delta; the history keeps that state as "before it began".
	if err := saveInterfaceWith(doc, cols(t, "fg=red"), nil, behaviour{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := saveInterfaceWith(doc, cols(t, "fg=blue"), nil, behaviour{}, packageDelta{"some-package": nil}); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.ReadFile(previousInterfacePath())
	if !strings.Contains(string(prev), "some-package") {
		t.Fatalf("the undo file holds no package delta, so this test would prove nothing: %s", prev)
	}
	if err := recordVersion("with a package change"); err != nil {
		t.Fatal(err)
	}
	h, _ := loadUIHistory()
	if len(h.Versions) != 2 {
		t.Fatalf("want the earlier state and the new one, got %d versions", len(h.Versions))
	}
	for _, v := range h.Versions {
		if len(v.State.Packages) != 0 {
			t.Errorf("version #%d holds packages: %+v\nConsequence: a restore could bring back a package the user removed, with permissions they revoked.\nRemedy: keep the four documents only.", v.ID, v.State.Packages)
		}
	}
}

func TestTheGuideTellsTheModelWhatTheUserCanDoAndThatItCannot(t *testing.T) {
	histRig(t)
	saveColour(t, "fg=red", "make code red")
	g := historyGuide()
	for _, want := range []string{"/ui history", "You cannot restore", "make code red", "#1"} {
		if !strings.Contains(g, want) {
			t.Errorf("the guide lacks %q:\n%s\nConsequence: the model offers a rollback it cannot make, or does not know the user has one.\nRemedy: say it, with the last versions.", want, g)
		}
	}
}

func TestHistoryIsATypedCommandAndNeverARequestToTheModel(t *testing.T) {
	for _, line := range []string{"/ui history", "/ui history restore 2", "/ui history name before the demo"} {
		if !uiTypedLine(line) {
			t.Errorf("%q is not a typed command.\nConsequence: it is sent to the model as a request to change the interface.\nRemedy: add history to uiTypedLine.", line)
		}
		if _, ok := uiPromptAfterSlash(line); ok {
			t.Errorf("%q is read as a request to the model", line)
		}
	}
}

// ---- through the real loop ------------------------------------------------------------

func TestTypingUIHistoryThroughTheLoopListsAndNeverReachesTheModel(t *testing.T) {
	histRig(t)
	script := append(enterLine("/ui color markdown.code fg=magenta"), enterLine("/ui history")...)
	out, sent := runLoopDefault(t, 100, 30, append(script, exitKeys()...))
	if len(sent) != 0 {
		t.Errorf("/ui history reached the model: %q", sent)
	}
	plain := lastFrame(out)
	if !strings.Contains(plain, "/ui history (newest first") || !strings.Contains(plain, "#1") {
		t.Errorf("the list is not on screen:\n%s\nConsequence: the history exists in files nobody can see.\nRemedy: show the list as a note in the conversation.", plain)
	}
}

func TestRestoringThroughTheLoopChangesTheScreenAndTheSavedInterface(t *testing.T) {
	histRig(t)
	saveColour(t, "fg=red", "red")
	if err := saveInterface(builtinDoc(t), cols(t, "fg=blue"), userTexts{"team.title": "Crews"}, behaviour{}); err != nil {
		t.Fatal(err)
	}
	if err := recordVersion("blue and Crews"); err != nil {
		t.Fatal(err)
	}
	script := append(enterLine("/ui history restore 1"), exitKeys()...)
	out, sent := runLoopDefault(t, 100, 30, script)
	if len(sent) != 0 {
		t.Errorf("the restore reached the model: %q", sent)
	}
	if !strings.Contains(lastFrame(out), "restored #1") {
		t.Errorf("the restore is not confirmed on screen:\n%s", lastFrame(out))
	}
	cur, _ := currentInterface()
	if !strings.Contains(cur.Tokens, "red") || strings.Contains(cur.Texts, "Crews") {
		t.Errorf("the saved interface is not version 1: tokens %q texts %q\nConsequence: the screen says restored and the next start shows something else.\nRemedy: write the restored state to the four documents.", cur.Tokens, cur.Texts)
	}
}

func TestUndoAndResetThroughTheLoopAreVersionsToo(t *testing.T) {
	histRig(t)
	// Reset changes something (the colour goes), and undo brings it back: both are steps.
	// (An undo before the reset would leave the factory look, and a reset on it changes
	// nothing, which is rightly not a version.)
	script := append(enterLine("/ui color markdown.code fg=magenta"), enterLine("/ui reset")...)
	script = append(script, enterLine("/ui undo")...)
	runLoopDefault(t, 100, 30, append(script, exitKeys()...))
	h, _ := loadUIHistory()
	var labels []string
	for _, v := range h.Versions {
		labels = append(labels, v.Label)
	}
	got := strings.Join(labels, " | ")
	if !strings.Contains(got, "/ui undo") || !strings.Contains(got, "/ui reset") {
		t.Errorf("versions: %s\nConsequence: going back is a step the history cannot go back over, so a mistaken undo is lost.\nRemedy: record the state after /ui undo and /ui reset like after any change.", got)
	}
}

func TestAfterARestoreTheNextChangeBuildsOnTheRestoredInterface(t *testing.T) {
	histRig(t)
	saveColour(t, "fg=red", "red")
	if err := saveInterface(builtinDoc(t), cols(t, "fg=blue"), userTexts{"team.title": "Crews"}, behaviour{}); err != nil {
		t.Fatal(err)
	}
	if err := recordVersion("blue and Crews"); err != nil {
		t.Fatal(err)
	}
	// The loop starts from what is saved (blue, Crews). After the restore it must hold
	// version 1 in memory: the next change is saved from memory, not from the files.
	script := append(enterLine("/ui history restore 1"), enterLine("/ui color markdown.link fg=green")...)
	runLoopDefault(t, 100, 30, append(script, exitKeys()...))
	cur, _ := currentInterface()
	if !strings.Contains(cur.Tokens, "red") || !strings.Contains(cur.Tokens, "green") || strings.Contains(cur.Tokens, "blue") || strings.Contains(cur.Texts, "Crews") {
		t.Errorf("the change after a restore was built on the old interface: tokens %q texts %q\nConsequence: the screen says restored and the next change silently brings back what was restored away.\nRemedy: after a restore put the restored colours, words, behaviour and layout in the loop's own state.", cur.Tokens, cur.Texts)
	}
}
