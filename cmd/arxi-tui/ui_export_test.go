package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/patch"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// framedDoc is the factory scene with the guide's round frame around the prompt: a layout
// that is NOT the built-in one, so the file has a scene part to carry.
func framedDoc(t *testing.T) *scene.Document {
	t.Helper()
	doc := builtinDoc(t)
	src := doc.Source()
	for _, line := range []string{
		`/ui add node above prompt {"id":"input_frame","type":"box","border":"round"}`,
		`/ui move prompt into input_frame`,
	} {
		res, err := patch.Apply(doc.Name(), src, line)
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		src = res.Source
	}
	d, err := scene.ParseNamed(doc.Name(), src)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// exportRig is histRig plus a folder for the files, and a saved interface with a colour,
// a word and a command so every part of the file has something in it.
func exportRig(t *testing.T) string {
	t.Helper()
	histRig(t)
	bh := behaviour{Commands: []userCommand{{Name: "hot", Run: actionList{"cmd:/effort max"}}}}
	if err := saveInterface(framedDoc(t), cols(t, "fg=red"), userTexts{"team.title": "Crews"}, bh); err != nil {
		t.Fatal(err)
	}
	if err := recordVersion("mine"); err != nil {
		t.Fatal(err)
	}
	return t.TempDir()
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// otherMachine empties the config so the saved interface is the factory one, as on a
// machine that never changed anything.
func otherMachine(t *testing.T) {
	t.Helper()
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
}

func TestExportThenImportOnAnotherMachineBringsTheSameInterface(t *testing.T) {
	dir := exportRig(t)
	want, _ := currentInterface()
	file := filepath.Join(dir, "mine.json")
	if out := runExport(file); out.err != "" {
		t.Fatal(out.err)
	}
	otherMachine(t)
	out := runImport(file, true)
	if out.err != "" {
		t.Fatal(out.err)
	}
	got, _ := currentInterface()
	if !sameInterface(got, want) {
		t.Errorf("imported %+v, exported %+v\nConsequence: the interface a friend sends arrives different from the one they made.\nRemedy: keep the four documents' content through the file (spacing may change).", got, want)
	}
	if !out.restored || !sameInterface(out.state, want) {
		t.Errorf("the loop is not told the new state: %+v", out)
	}
}

func TestImportReplacesAndUndoBringsBackWhatWasThere(t *testing.T) {
	dir := exportRig(t)
	file := filepath.Join(dir, "mine.json")
	runExport(file)
	if err := saveInterface(builtinDoc(t), cols(t, "fg=blue"), nil, behaviour{}); err != nil {
		t.Fatal(err)
	}
	before, _ := currentInterface()
	if out := runImport(file, true); out.err != "" {
		t.Fatal(out.err)
	}
	if _, _, _, _, err := undoInterface(); err != nil {
		t.Fatal(err)
	}
	after, _ := currentInterface()
	if !sameState(after, before) {
		t.Errorf("undo gave %+v, want %+v\nConsequence: an import cannot be taken back and the user's own interface is gone.\nRemedy: import through makeSaved.", after, before)
	}
}

func TestAnImportIsAVersionInTheHistory(t *testing.T) {
	dir := exportRig(t)
	file := filepath.Join(dir, "mine.json")
	runExport(file)
	otherMachine(t)
	runImport(file, true)
	h, _ := loadUIHistory()
	if len(h.Versions) == 0 || !strings.Contains(h.Versions[len(h.Versions)-1].Label, "imported mine.json") {
		t.Errorf("history after import: %+v\nConsequence: the import cannot be found or restored from /ui history.\nRemedy: recordVersion after an import.", h.Versions)
	}
}

func TestTheFileHoldsOnlyTheFourDocumentsAndNoPackagesTestsOrHistory(t *testing.T) {
	dir := exportRig(t)
	writeFile(t, filepath.Join(configDir(), "ui_tests.json"), `{"tests":["SECRET-TEST"]}`)
	file := filepath.Join(dir, "mine.json")
	runExport(file)
	b, _ := os.ReadFile(file)
	s := string(b)
	for _, want := range []string{`"format": "arxi-interface"`, `"scene"`, `"tokens"`, `"texts"`, `"behaviour"`, "Crews", "/effort max", "input_frame"} {
		if !strings.Contains(s, want) {
			t.Errorf("the file lacks %q:\n%s", want, s)
		}
	}
	for _, bad := range []string{"SECRET-TEST", `"packages"`, `"versions"`} {
		if strings.Contains(s, bad) {
			t.Errorf("the file holds %q\nConsequence: a file sent to someone carries programs, tests or history they never asked for.\nRemedy: export only the four documents.", bad)
		}
	}
}

func TestExportRefusesWhenThereIsNothingOfYoursAndNeverOverwrites(t *testing.T) {
	histRig(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "x.json")
	if out := runExport(file); !strings.Contains(out.err, "nothing of yours") {
		t.Errorf("exported the factory interface: %+v", out)
	}
	if _, err := os.Stat(file); err == nil {
		t.Error("a file was written for nothing")
	}
	exportRig2 := func() {
		if err := saveInterface(builtinDoc(t), cols(t, "fg=red"), nil, behaviour{}); err != nil {
			t.Fatal(err)
		}
	}
	exportRig2()
	writeFile(t, file, "precious")
	out := runExport(file)
	if !strings.Contains(out.err, "already exists") {
		t.Errorf("overwrite not refused: %+v", out)
	}
	if b, _ := os.ReadFile(file); string(b) != "precious" {
		t.Errorf("the file was overwritten with %q\nConsequence: an export destroys what the user had under that name.\nRemedy: open with O_EXCL.", b)
	}
}

func TestExportWithoutAFileNameUsesADatedNameInTheCurrentFolder(t *testing.T) {
	exportRig(t)
	t.Chdir(t.TempDir())
	out := runExport("")
	if out.err != "" {
		t.Fatal(out.err)
	}
	want := "arxi-interface-" + historyNow().Format("2006-01-02")[:8]
	m, _ := filepath.Glob(want + "*.json")
	if len(m) != 1 {
		t.Errorf("files %v, want one starting %s", m, want)
	}
}

func TestImportRefusesEveryBadFileWithAReasonAndChangesNothing(t *testing.T) {
	dir := exportRig(t)
	good := filepath.Join(dir, "good.json")
	runExport(good)
	goodBytes, _ := os.ReadFile(good)
	otherMachine(t)
	cases := []struct{ name, body, say string }{
		{"notjson.json", "hello", "not an arxi interface file"},
		{"plainscene.json", `{"root":{}}`, "not an arxi interface file"},
		{"format.json", `{"format":"other","version":1}`, "format"},
		{"version.json", `{"format":"arxi-interface","version":2}`, "version 2"},
		{"extra.json", `{"format":"arxi-interface","version":1,"packages":{"x":"y"}}`, "not an arxi interface file"},
		{"trailing.json", string(goodBytes) + `{"format":"arxi-interface","version":1}`, "something follows"},
		{"badscene.json", `{"format":"arxi-interface","version":1,"scene":{"nope":1}}`, "does not load"},
		{"badtokens.json", `{"format":"arxi-interface","version":1,"tokens":{"markdown.code":"fg=notacolour"}}`, "does not load"},
		{"answers.json", `{"format":"arxi-interface","version":1,"behaviour":{"keys":{"f5":"answer:yes"}}}`, "does not load"},
	}
	for _, c := range cases {
		p := filepath.Join(dir, c.name)
		writeFile(t, p, c.body)
		out := runImport(p, true)
		if !strings.Contains(out.err, c.say) {
			t.Errorf("%s: err %q, want it to say %q", c.name, out.err, c.say)
		}
		if !strings.Contains(out.err, "Nothing was changed") {
			t.Errorf("%s: the refusal does not say nothing changed: %q", c.name, out.err)
		}
		if out.restored {
			t.Errorf("%s: the loop is told to switch to a refused interface", c.name)
		}
	}
	cur, _ := currentInterface()
	if !emptyState(cur) {
		t.Errorf("a refused import changed the saved interface: %+v\nConsequence: a broken file from someone else can wreck the interface.\nRemedy: validate with loadInterface before makeSaved.", cur)
	}
	if _, err := os.Stat(previousInterfacePath()); err == nil {
		t.Error("a refused import wrote the undo file")
	}
}

func TestImportRefusesMissingFolderEmptyAndHugeFiles(t *testing.T) {
	exportRig(t)
	dir := t.TempDir()
	huge := filepath.Join(dir, "huge.json")
	writeFile(t, huge, strings.Repeat(" ", maxImportBytes+1))
	for _, c := range []struct{ arg, say string }{
		{"", "say which file"},
		{filepath.Join(dir, "none.json"), "no such file"},
		{dir, "is a folder"},
		{huge, "never more than"},
	} {
		if out := runImport(c.arg, true); !strings.Contains(out.err, c.say) {
			t.Errorf("import %q: %q, want %q", c.arg, out.err, c.say)
		}
	}
}

func TestImportNamesTheCommandsAndKeysTheFileAdds(t *testing.T) {
	dir := exportRig(t)
	bh := behaviour{
		Commands: []userCommand{{Name: "hot", Run: actionList{"cmd:/effort max"}}},
		Keys:     map[string]actionList{"f5": {"cmd:/mode plan"}},
	}
	if err := saveInterface(builtinDoc(t), nil, nil, bh); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "k.json")
	runExport(file)
	otherMachine(t)
	out := runImport(file, true)
	for _, want := range []string{"/hot", "f5", "REPLACED", "/ui undo"} {
		if !strings.Contains(out.note, want) {
			t.Errorf("the note lacks %q: %s\nConsequence: keys and commands from someone else arrive without the user seeing what they are.\nRemedy: list them by name.", want, out.note)
		}
	}
}

func TestImportOfTheSameInterfaceIsRefusedAsNothingToDo(t *testing.T) {
	dir := exportRig(t)
	file := filepath.Join(dir, "same.json")
	runExport(file)
	out := runImport(file, true)
	if !strings.Contains(out.err, "already have") {
		t.Errorf("same interface: %+v", out)
	}
}

func TestImportIsRefusedForAScenePassedWithDashScene(t *testing.T) {
	dir := exportRig(t)
	file := filepath.Join(dir, "m.json")
	runExport(file)
	otherMachine(t)
	out := runImport(file, false)
	if !strings.Contains(out.err, "-scene") {
		t.Errorf("not refused: %+v", out)
	}
	if cur, _ := currentInterface(); !emptyState(cur) {
		t.Error("the saved interface changed")
	}
}

func TestATildePathMeansTheHomeFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got := exportPath("~/a/b.json", time.Now())
	if got != filepath.Join(home, "a", "b.json") {
		t.Errorf("got %s", got)
	}
}

func TestExportAndImportAreTypedCommandsNeverARequestToTheModel(t *testing.T) {
	for _, line := range []string{"/ui export", "/ui export mine.json", "/ui import mine.json"} {
		if !uiTypedLine(line) {
			t.Errorf("%q is not a typed command.\nConsequence: it goes to the model as a request to change the interface.\nRemedy: add export and import to uiTypedLine.", line)
		}
		if _, ok := uiPromptAfterSlash(line); ok {
			t.Errorf("%q is read as a request to the model", line)
		}
	}
}

func TestTheFileNameAfterImportIsKeptWholeIncludingSpaces(t *testing.T) {
	_, args, ok := uiStateLine("/ui import  my file.json ")
	if !ok || args != "my file.json" {
		t.Errorf("args %q ok %v", args, ok)
	}
	if v, a, ok := uiStateLine("/ui export"); !ok || v != "export" || a != "" {
		t.Errorf("bare export: %q %q %v", v, a, ok)
	}
	if _, _, ok := uiStateLine("/ui color x"); ok {
		t.Error("a colour line is read as a state line")
	}
}

func TestTheGuideSaysTheModelCannotImport(t *testing.T) {
	histRig(t)
	g := historyGuide()
	if !strings.Contains(g, "/ui export") || !strings.Contains(g, "restore or import") {
		t.Errorf("guide:\n%s\nConsequence: the model offers to load a file it has no way to load, or does not know the user can share the interface.\nRemedy: say both.", g)
	}
}

// ---- through the real loop ------------------------------------------------------------

func TestTypingExportThenImportThroughTheLoopChangesTheScreenAndNeverReachesTheModel(t *testing.T) {
	dir := exportRig(t)
	file := filepath.Join(dir, "loop.json")
	script := enterLine("/ui export " + file)
	script = append(script, enterLine("/ui reset")...)
	script = append(script, enterLine("/ui import "+file)...)
	out, sent := runLoopDefault(t, 110, 30, append(script, exitKeys()...))
	if len(sent) != 0 {
		t.Errorf("reached the model: %q", sent)
	}
	if !strings.Contains(lastFrame(out), "imported loop.json") {
		t.Errorf("the import is not confirmed on screen:\n%s", lastFrame(out))
	}
	cur, _ := currentInterface()
	if !strings.Contains(cur.Tokens, "red") || !strings.Contains(cur.Texts, "Crews") || !strings.Contains(cur.Scene, "input_frame") {
		t.Errorf("saved after the loop: %+v\nConsequence: the screen says imported and the next start shows the factory interface.\nRemedy: write the state, then load it in the loop.", cur)
	}
}

func TestARefusedImportThroughTheLoopSaysWhyOnScreen(t *testing.T) {
	dir := exportRig(t)
	bad := filepath.Join(dir, "bad.json")
	writeFile(t, bad, "hello")
	out, sent := runLoopDefault(t, 110, 30, append(enterLine("/ui import "+bad), exitKeys()...))
	if len(sent) != 0 {
		t.Errorf("reached the model: %q", sent)
	}
	if !strings.Contains(lastFrame(out), "Nothing was changed") {
		t.Errorf("no reason on screen:\n%s", lastFrame(out))
	}
}

func TestANullPartInTheFileMeansTheFactoryOneNotTheWordNull(t *testing.T) {
	dir := exportRig(t)
	file := filepath.Join(dir, "nulls.json")
	writeFile(t, file, `{"format":"arxi-interface","version":1,"scene":null,"tokens":{"markdown.code":{"fg":"blue"}},"texts":null,"behaviour":null}`)
	out := runImport(file, true)
	if out.err != "" {
		t.Fatal(out.err)
	}
	cur, _ := currentInterface()
	if cur.Scene != "" || cur.Texts != "" || cur.Behaviour != "" {
		t.Errorf("saved %+v\nConsequence: the four letters null are saved as a document and the screen comes back empty at the next start.\nRemedy: read null as an absent part.", cur)
	}
	if !strings.Contains(cur.Tokens, "blue") {
		t.Errorf("the colours did not arrive: %+v", cur)
	}
}
