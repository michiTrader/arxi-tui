package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/term"
)

// logIn starts one conversation as if the TUI had run in folder cwd.
func logIn(t *testing.T, dir, cwd, question string, sec int) {
	t.Helper()
	l := newSessionLog(dir)
	l.cwd = cwd
	l.now = func() time.Time { return time.Date(2026, 10, 6, 12, 0, sec, 0, time.UTC) }
	chat(l, question, "answer")
}

func joinRows(rows []string) string { return strings.Join(rows, "|") }

func rowNames(dir, cwd, scope string) []string {
	var out []string
	for _, r := range resumeMenuData(dir, time.Now(), cwd, scope) {
		out = append(out, r.Name)
	}
	return out
}

// By default /resume lists only what was started in the folder the TUI runs in.
func TestResumeListsOnlyTheCurrentFolderByDefault(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	here, there := t.TempDir(), t.TempDir()
	logIn(t, dir, here, "question from here", 1)
	logIn(t, dir, there, "question from there", 2)
	if got := joinRows(rowNames(dir, here, resumeFolder)); got != "question from here" {
		t.Errorf("folder scope listed %q, want only the one started here", got)
	}
	if got := joinRows(rowNames(dir, there, resumeFolder)); got != "question from there" {
		t.Errorf("folder scope in the other folder listed %q", got)
	}
}

func TestResumeAllListsEveryFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	logIn(t, dir, t.TempDir(), "first", 1)
	logIn(t, dir, t.TempDir(), "second", 2)
	if got := joinRows(rowNames(dir, t.TempDir(), resumeAll)); got != "second|first" {
		t.Errorf("all scope listed %q, want both, newest first", got)
	}
}

// A folder with no conversation of its own, while others exist, says why and how to see them.
func TestResumeEmptyFolderSaysWhereTheOthersAre(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	logIn(t, dir, t.TempDir(), "elsewhere", 1)
	rows := resumeMenuData(dir, time.Now(), t.TempDir(), resumeFolder)
	if len(rows) != 1 || rows[0].Ref != "" || !strings.Contains(rows[0].Name, "in this folder") ||
		!strings.Contains(rows[0].Provider, "resume.scope all") {
		t.Errorf("empty-folder row = %+v; want a non-pickable row that names the setting", rows)
	}
	empty := resumeMenuData(filepath.Join(t.TempDir(), "none"), time.Now(), t.TempDir(), resumeFolder)
	if len(empty) != 1 || empty[0].Name != "no saved conversations yet" {
		t.Errorf("no conversation at all = %+v", empty)
	}
}

// A conversation saved before the folder was kept has none to compare: the folder scope
// does not guess, it leaves it out, and "all" still shows it.
func TestResumeAConversationWithNoFolderShowsOnlyWithAll(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	logIn(t, dir, "", "an old one", 1)
	if got := rowNames(dir, t.TempDir(), resumeFolder); len(got) != 1 || got[0] == "an old one" {
		t.Errorf("folder scope listed the old conversation: %q", got)
	}
	if got := joinRows(rowNames(dir, t.TempDir(), resumeAll)); got != "an old one" {
		t.Errorf("all scope lost the old conversation: %q", got)
	}
}

func TestTheHeaderKeepsTheFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	cwd := t.TempDir()
	logIn(t, dir, cwd, "q", 1)
	list := listSessions(dir)
	if len(list) != 1 || list[0].Cwd != cwd {
		t.Fatalf("list = %+v, want the folder %q kept", list, cwd)
	}
	b, _ := os.ReadFile(filepath.Join(dir, list[0].ID+".jsonl"))
	if !strings.Contains(string(b), `"cwd"`) {
		t.Errorf("the header does not name the folder:\n%s", b)
	}
}

func TestSessionInFolderIgnoresATrailingSeparatorAndLinks(t *testing.T) {
	d := t.TempDir()
	if !sessionInFolder(d+string(filepath.Separator), d) {
		t.Error("a trailing separator is the same folder")
	}
	if sessionInFolder("", d) || sessionInFolder(d, "") {
		t.Error("an unknown folder is in no folder")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(d, link); err == nil && !sessionInFolder(link, d) {
		t.Error("a link to the folder is the folder")
	}
	if sessionInFolder(d, t.TempDir()) {
		t.Error("two different folders matched")
	}
}

func TestTheScopeSettingAcceptsOnlyFolderOrAll(t *testing.T) {
	for _, ok := range []string{"folder", "all", " ALL "} {
		if err := validChatLook("resume.scope", ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	err := validChatLook("resume.scope", "everywhere")
	if err == nil || !strings.Contains(err.Error(), "folder or all") {
		t.Errorf("a bad value must say what is allowed: %v", err)
	}
	setTexts(t)
	if resumeScope() != resumeFolder {
		t.Error("the factory scope is the folder")
	}
	setTexts(t, "resume.scope", "all")
	if resumeScope() != resumeAll {
		t.Error("the user's all was not read")
	}
	setTexts(t, "resume.scope", "garbage")
	if resumeScope() != resumeFolder {
		t.Error("an unreadable value must fall back to the factory scope")
	}
}

// Through the real loop: a conversation of another folder is not offered by /resume, and
// one of this folder is; the user's "all" brings the other back.
func TestLoopResumeMenuFollowsTheFolder(t *testing.T) {
	t.Cleanup(func() { setActiveTexts(nil) })
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	t.Setenv(historyEnv, t.TempDir())
	t.Setenv(sessionsEnv, "")
	here, _ := os.Getwd()
	dir := filepath.Join(cfg, "sessions")
	logIn(t, dir, here, "ZZhere question", 1)
	logIn(t, dir, t.TempDir(), "ZZthere question", 2)
	script := append(typed("/resume "), at(150, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyUp}}))
	out, _ := runNamedLoop(t, 80, 24, append(script, exitKeys()...))
	f := frameWith(out, "ZZhere")
	if f == "" {
		t.Fatalf("the conversation of this folder is not in the /resume menu:\n%s", lastFrame(out))
	}
	if strings.Contains(f, "ZZthere") {
		t.Errorf("the conversation of another folder is in the menu by default:\n%s", f)
	}
	// The loop reads the user's texts.json from the config folder, so the setting is written
	// where the user's would be.
	if err := os.WriteFile(filepath.Join(cfg, "texts.json"), []byte(`{"resume.scope":"all"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ = runNamedLoop(t, 80, 24, append(typed("/resume "), exitKeys()...))
	if f := frameWith(out, "ZZthere"); f == "" {
		t.Errorf("with resume.scope all the other folder's conversation is still missing:\n%s", lastFrame(out))
	}
}
