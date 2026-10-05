package chattools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func project(t *testing.T) *Toolbox {
	t.Helper()
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(dir, "src", "deep"), 0o755))
	must(os.MkdirAll(filepath.Join(dir, ".git"), 0o755))
	must(os.MkdirAll(filepath.Join(dir, "node_modules", "x"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644))
	must(os.WriteFile(filepath.Join(dir, "src", "a.go"), []byte("package src\n// TODO fix\n"), 0o644))
	must(os.WriteFile(filepath.Join(dir, "src", "deep", "b.txt"), []byte("nothing\nTODO later\n"), 0o644))
	must(os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("[remote] url=secret\n"), 0o644))
	must(os.WriteFile(filepath.Join(dir, "node_modules", "x", "i.js"), []byte("TODO dep\n"), 0o644))
	must(os.WriteFile(filepath.Join(dir, "blob.bin"), []byte("ab\x00cd TODO"), 0o644))
	tb, err := New(dir)
	must(err)
	return tb
}

func run(t *testing.T, tb *Toolbox, name string, args any) (Result, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	return tb.Run(name, raw)
}

func TestListShowsFoldersWithASlashAndHidesGit(t *testing.T) {
	tb := project(t)
	res, err := run(t, tb, ToolList, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"main.go\n", "src/\n", "node_modules/\n"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("list is missing %q:\n%s", want, res.Text)
		}
	}
	if strings.Contains(res.Text, ".git") {
		t.Errorf("list shows the repository bookkeeping:\n%s", res.Text)
	}
	if res.Summary != "Listed 4 entries" {
		t.Errorf("summary = %q", res.Summary)
	}
}

func TestReadNumbersTheLinesAndReportsHowMany(t *testing.T) {
	tb := project(t)
	res, err := run(t, tb, ToolRead, map[string]any{"path": "main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "     1\tpackage main\n") || !strings.Contains(res.Text, "     3\tfunc main() {}\n") {
		t.Errorf("lines are not numbered:\n%q", res.Text)
	}
	if res.Summary != "Read 3 lines" || res.Arg != "main.go" {
		t.Errorf("summary=%q arg=%q", res.Summary, res.Arg)
	}
}

func TestReadAPartSaysWhereToContinue(t *testing.T) {
	tb := project(t)
	res, err := run(t, tb, ToolRead, map[string]any{"path": "main.go", "offset": 2, "limit": 1})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Text, "package main") || !strings.Contains(res.Text, "[showing lines 2-2 of 3; read again with offset 3") {
		t.Errorf("a partial read must say what it left out:\n%s", res.Text)
	}
}

func TestReadRefusesWhatIsNotAFileOfTheProject(t *testing.T) {
	tb := project(t)
	outsideFile := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ path, want string }{
		{"../secret.txt", "outside the project"},
		{outsideFile, "outside the project"},
		{".git/config", "bookkeeping"},
		{"nope.go", "does not exist"},
		{"src", "is a folder"},
		{"blob.bin", "binary"},
		{"", "needs a path"},
	} {
		_, err := run(t, tb, ToolRead, map[string]any{"path": c.path})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("read %q: err = %v, want it to mention %q", c.path, err, c.want)
		}
	}
}

func TestASymlinkOutOfTheProjectIsNotFollowed(t *testing.T) {
	tb := project(t)
	outsideDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outsideDir, "s.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(tb.Root(), "link")); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}
	if _, err := run(t, tb, ToolRead, map[string]any{"path": "link/s.txt"}); err == nil || !strings.Contains(err.Error(), "outside the project") {
		t.Errorf("a path through a symlink out of the tree was read: %v", err)
	}
	res, err := run(t, tb, ToolGrep, map[string]any{"pattern": "secret"})
	if err != nil || strings.Contains(res.Text, "s.txt") {
		t.Errorf("grep followed a symlink out of the tree: %v\n%s", err, res.Text)
	}
}

func TestGrepFindsLinesAndSkipsGitDependenciesAndBinaries(t *testing.T) {
	tb := project(t)
	res, err := run(t, tb, ToolGrep, map[string]any{"pattern": "TODO"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"src/a.go:2:// TODO fix", "src/deep/b.txt:2:TODO later"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("grep missed %q:\n%s", want, res.Text)
		}
	}
	for _, bad := range []string{".git", "node_modules", "blob.bin"} {
		if strings.Contains(res.Text, bad) {
			t.Errorf("grep searched %s:\n%s", bad, res.Text)
		}
	}
	if res.Summary != "Found 2 matches in 2 files" {
		t.Errorf("summary = %q", res.Summary)
	}
}

func TestGrepInAFolderAndInOneFile(t *testing.T) {
	tb := project(t)
	res, _ := run(t, tb, ToolGrep, map[string]any{"pattern": "TODO", "path": "src/deep"})
	if strings.Contains(res.Text, "a.go") || !strings.Contains(res.Text, "b.txt") {
		t.Errorf("grep ignored the folder:\n%s", res.Text)
	}
	res, _ = run(t, tb, ToolGrep, map[string]any{"pattern": "main", "path": "main.go"})
	if !strings.Contains(res.Text, "main.go:1:package main") {
		t.Errorf("grep of one file:\n%s", res.Text)
	}
}

func TestGrepSaysNoMatchesAndRefusesBadPatterns(t *testing.T) {
	tb := project(t)
	res, err := run(t, tb, ToolGrep, map[string]any{"pattern": "zzzzqq"})
	if err != nil || res.Summary != "No matches" || !strings.Contains(res.Text, "no matches") {
		t.Errorf("res=%+v err=%v", res, err)
	}
	for _, p := range []string{"", "([", "(?=x)"} {
		if _, err := run(t, tb, ToolGrep, map[string]any{"pattern": p}); err == nil {
			t.Errorf("pattern %q was accepted", p)
		}
	}
	if _, err := run(t, tb, ToolGrep, map[string]any{"pattern": "x", "path": "missing"}); err == nil {
		t.Error("a search in a folder that does not exist reported success")
	}
}

func TestGrepStopsAtTheCapAndSaysSo(t *testing.T) {
	tb := project(t)
	var b strings.Builder
	for i := 0; i < maxMatches+50; i++ {
		b.WriteString("hit\n")
	}
	if err := os.WriteFile(filepath.Join(tb.Root(), "many.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := run(t, tb, ToolGrep, map[string]any{"pattern": "hit"})
	if !strings.Contains(res.Text, "stopped early") || strings.Count(res.Text, "many.txt:") != maxMatches {
		t.Errorf("the cap was not applied or not announced (%d lines)", strings.Count(res.Text, "\n"))
	}
}

func TestFilesThatHoldSecretsAreNeverShown(t *testing.T) {
	tb := project(t)
	for name, body := range map[string]string{".env": "KEY=hunter2", ".env.local": "KEY=hunter2", "id_rsa": "hunter2", "server.pem": "hunter2", ".env.example": "KEY=changeme"} {
		if err := os.WriteFile(filepath.Join(tb.Root(), name), []byte(body+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{".env", ".env.local", "id_rsa", "server.pem"} {
		if _, err := run(t, tb, ToolRead, map[string]any{"path": name}); err == nil || !strings.Contains(err.Error(), "secrets") {
			t.Errorf("read %s: err = %v", name, err)
		}
	}
	if res, err := run(t, tb, ToolRead, map[string]any{"path": ".env.example"}); err != nil || !strings.Contains(res.Text, "changeme") {
		t.Errorf("a template must stay readable: %v %q", err, res.Text)
	}
	if res, _ := run(t, tb, ToolGrep, map[string]any{"pattern": "hunter2"}); res.Summary != "No matches" {
		t.Errorf("grep revealed a secret:\n%s", res.Text)
	}
	res, _ := run(t, tb, ToolList, map[string]any{})
	for _, name := range []string{".env\n", ".env.local", "id_rsa", "server.pem"} {
		if strings.Contains(res.Text, name) {
			t.Errorf("list shows %q:\n%s", name, res.Text)
		}
	}
	if !strings.Contains(res.Text, ".env.example") {
		t.Errorf("list hides the template:\n%s", res.Text)
	}
}

func TestAnUnknownToolAndBadArgumentsAreRefused(t *testing.T) {
	tb := project(t)
	if _, err := tb.Run("bash", json.RawMessage(`{"command":"ls"}`)); err == nil || !strings.Contains(err.Error(), "no tool called") {
		t.Errorf("bash must not exist here: %v", err)
	}
	if _, err := tb.Run("read", json.RawMessage(`[1]`)); err == nil {
		t.Error("arguments that are not an object were accepted")
	}
}

func TestNewNeedsARealFolder(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Error("empty dir accepted")
	}
	if _, err := New(filepath.Join(t.TempDir(), "gone")); err == nil {
		t.Error("missing dir accepted")
	}
	f := filepath.Join(t.TempDir(), "f")
	os.WriteFile(f, nil, 0o600)
	if _, err := New(f); err == nil {
		t.Error("a file was accepted as a folder")
	}
}
