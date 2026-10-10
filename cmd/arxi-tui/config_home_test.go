package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

func put(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func resetImportOnce() { importOnce = sync.Map{} }

func TestConfigDirIsDotArxiInTheHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv(configDirEnv, "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg")) // an empty legacy folder
	resetImportOnce()
	if got, want := configDir(), filepath.Join(home, ".arxi"); got != want {
		t.Errorf("configDir = %q, want %q", got, want)
	}
}

func TestTheEnvironmentOverridesConfigDirAndImportsNothing(t *testing.T) {
	home, over := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	put(t, filepath.Join(home, "xdg", "arxi"), "theme.json", "{}")
	t.Setenv(configDirEnv, over)
	resetImportOnce()
	if configDir() != over {
		t.Fatalf("configDir = %q", configDir())
	}
	if _, err := os.Stat(filepath.Join(over, "theme.json")); err == nil {
		t.Error("an explicit folder must not be filled from the old one")
	}
}

func TestOldSettingsAreCopiedOnceAndTheOldFolderIsKept(t *testing.T) {
	root := t.TempDir()
	old, dir := filepath.Join(root, "old"), filepath.Join(root, "new")
	put(t, old, "theme.json", `{"a":1}`)
	put(t, old, "sessions/20260101-000000-abcd.jsonl", "line")
	put(t, dir, "texts.json", `{"mine":true}`)
	put(t, old, "texts.json", `{"mine":false}`)
	resetImportOnce()

	importLegacyConfig(dir, old)

	if b, _ := os.ReadFile(filepath.Join(dir, "theme.json")); string(b) != `{"a":1}` {
		t.Errorf("theme.json = %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "sessions", "20260101-000000-abcd.jsonl")); err != nil {
		t.Error("sub-folders (sessions) must come along")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "texts.json")); string(b) != `{"mine":true}` {
		t.Errorf("an existing file was overwritten: %q", b)
	}
	if _, err := os.Stat(filepath.Join(old, "theme.json")); err != nil {
		t.Error("the old folder must be left in place")
	}

	// A setting removed afterwards must not be resurrected.
	os.Remove(filepath.Join(dir, "theme.json"))
	resetImportOnce()
	importLegacyConfig(dir, old)
	if _, err := os.Stat(filepath.Join(dir, "theme.json")); err == nil {
		t.Error("a deleted setting came back from the old folder")
	}
}

func TestNoOldFolderMeansNothingIsCreated(t *testing.T) {
	root := t.TempDir()
	resetImportOnce()
	importLegacyConfig(filepath.Join(root, "new"), filepath.Join(root, "missing"))
	if _, err := os.Stat(filepath.Join(root, "new")); err == nil {
		t.Error("the new folder was created with nothing to import")
	}
}

func TestImportedFilesArePrivate(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("permissions are not POSIX here")
	}
	root := t.TempDir()
	old, dir := filepath.Join(root, "old"), filepath.Join(root, "new")
	put(t, old, "search.json", `{"key":"secret"}`)
	os.Chmod(filepath.Join(old, "search.json"), 0o644)
	resetImportOnce()
	importLegacyConfig(dir, old)
	fi, err := os.Stat(filepath.Join(dir, "search.json"))
	if err != nil || fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("search.json = %v, %v: a copied key file must be 0600", fi, err)
	}
}

func TestHistoryLivesInTheConfigFolder(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(configDirEnv, dir)
	t.Setenv(historyEnv, "")
	if got := historyPath(); got != filepath.Join(dir, "history") {
		t.Errorf("historyPath = %q", got)
	}
	other := t.TempDir()
	t.Setenv(historyEnv, other)
	if got := historyPath(); got != filepath.Join(other, "history") {
		t.Errorf("ARXI_HISTORY_DIR must still win: %q", got)
	}
}

// ---- the project layer ---------------------------------------------------------------

// projectSetup gives a fresh config folder and a project folder with a .arxi inside.
func projectSetup(t *testing.T) (cwd string) {
	t.Helper()
	t.Setenv(configDirEnv, t.TempDir())
	cwd = t.TempDir()
	put(t, filepath.Join(cwd, ".arxi"), "texts.json", `{"command.help":"Project help"}`)
	put(t, filepath.Join(cwd, ".arxi"), "theme.json", `{"menu.name":{"fg":"#ff0000"}}`)
	return cwd
}

func TestProjectSettingsAreNotLoadedUntilTrusted(t *testing.T) {
	cwd := projectSetup(t)
	if st, _ := projectStatus(cwd); st != projectAsk {
		t.Fatalf("status = %v, want ask", st)
	}
	l, err := loadProjectLayer(cwd, behaviour{})
	if err != nil || !l.empty() {
		t.Errorf("an untrusted folder loaded %+v, %v", l, err)
	}
}

func TestTrustLoadsAndForgetStopsIt(t *testing.T) {
	cwd := projectSetup(t)
	if _, err := trustProject(cwd); err != nil {
		t.Fatal(err)
	}
	l, err := loadProjectLayer(cwd, behaviour{})
	if err != nil || l.words["command.help"] != "Project help" || len(l.cols) == 0 {
		t.Fatalf("layer = %+v, %v", l, err)
	}
	if was, err := forgetProject(cwd); err != nil || !was {
		t.Fatalf("forget = %v, %v", was, err)
	}
	if l, _ := loadProjectLayer(cwd, behaviour{}); !l.empty() {
		t.Error("a forgotten folder still loads")
	}
	if was, _ := forgetProject(cwd); was {
		t.Error("forgetting twice must say there was nothing")
	}
}

func TestChangingAProjectFileAsksAgain(t *testing.T) {
	cwd := projectSetup(t)
	trustProject(cwd)
	put(t, filepath.Join(cwd, ".arxi"), "behaviour.json", `{"keys":{"f5":"cmd:/mode plan"}}`)
	st, _ := projectStatus(cwd)
	if st != projectChanged {
		t.Fatalf("status = %v, want changed", st)
	}
	if l, _ := loadProjectLayer(cwd, behaviour{}); !l.empty() {
		t.Error("files that changed after consent were loaded without asking")
	}
	if !strings.Contains(projectSummary(cwd, projectLayer{}), "changed since you trusted") {
		t.Error("the status must say why nothing loads")
	}
}

func TestConsentIsPerFolder(t *testing.T) {
	cwd := projectSetup(t)
	trustProject(cwd)
	other := t.TempDir()
	put(t, filepath.Join(other, ".arxi"), "texts.json", `{"command.help":"Project help"}`)
	put(t, filepath.Join(other, ".arxi"), "theme.json", `{"menu.name":{"fg":"#ff0000"}}`)
	if st, _ := projectStatus(other); st != projectAsk {
		t.Errorf("a copy of the same files in another folder is %v, want ask", st)
	}
}

func TestOnlyTheThreeFilesAreEverRead(t *testing.T) {
	cwd := projectSetup(t)
	put(t, filepath.Join(cwd, ".arxi"), "search.json", `{"key":"sk-leak"}`)
	put(t, filepath.Join(cwd, ".arxi"), "secrets/openai.key", "sk-leak")
	put(t, filepath.Join(cwd, ".arxi"), "providers/x.json", `{}`)
	h1 := projectHash(filepath.Join(cwd, ".arxi"))
	os.Remove(filepath.Join(cwd, ".arxi", "search.json"))
	if h2 := projectHash(filepath.Join(cwd, ".arxi")); h1 != h2 {
		t.Error("a file that is not one of the three changed the fingerprint, so it is being read")
	}
}

func TestProjectDirThatIsTheUserFolderIsIgnored(t *testing.T) {
	home := t.TempDir()
	t.Setenv(configDirEnv, filepath.Join(home, ".arxi"))
	put(t, filepath.Join(home, ".arxi"), "theme.json", `{}`)
	if st, _ := projectStatus(home); st != projectNone {
		t.Errorf("running from the home folder made ~/.arxi a project: %v", st)
	}
}

func TestProjectBehaviourLaysOverTheUsersAndHooksAdd(t *testing.T) {
	user := behaviour{Keys: map[string]actionList{"f5": {"cmd:/mode plan"}}}
	proj := behaviour{
		Keys:  map[string]actionList{"f6": {"cmd:/effort low"}},
		Hooks: []userHook{{On: "mode", Do: actionList{"cmd:/effort low"}}},
	}
	m := mergeBehaviour(user, proj)
	if len(m.Keys) != 2 || len(m.Hooks) != 1 {
		t.Errorf("merged = %+v", m)
	}
	if len(user.Keys) != 1 || len(user.Hooks) != 0 {
		t.Error("merging changed the user's own layer, which is what gets saved")
	}
}

func TestAProjectLayerThatBreaksTheMergeIsRefusedWhole(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	cwd := t.TempDir()
	put(t, filepath.Join(cwd, ".arxi"), "behaviour.json", `{"keys":{"f5":"cmd:/mode plan"}}`)
	trustProject(cwd)
	user := behaviour{Keys: map[string]actionList{"F5": {"cmd:/effort low"}}}
	if _, err := loadProjectLayer(cwd, user); err == nil {
		t.Error("two spellings of the same key must be refused, not half applied")
	}
}

func TestProjectCommandIsInTheSlashMenu(t *testing.T) {
	found := false
	for _, m := range fold.FilterSlashMatches("project") {
		found = found || m.Name == "project"
	}
	if !found {
		t.Error("/project is not listed in the slash menu")
	}
}

func TestProjectLineVerbs(t *testing.T) {
	for in, want := range map[string]string{
		"/project": "status", "/project status": "status", "/project trust": "trust",
		"/project forget": "forget", "/project nope": "usage",
	} {
		if got, ok := projectLine(in, 0, fold.SlashAll); !ok || got != want {
			t.Errorf("%q -> %q, %v; want %q", in, got, ok, want)
		}
	}
	if _, ok := projectLine("/projects", 0, fold.SlashAll); ok {
		t.Error("/projects is not /project")
	}
	if _, ok := projectLine("hello", 0, fold.SlashAll); ok {
		t.Error("plain text is not a project command")
	}
}
