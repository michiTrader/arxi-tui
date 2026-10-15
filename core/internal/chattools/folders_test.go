package chattools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sibling makes a second project beside the toolbox's, with a marker and a secret.
func sibling(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, d := range []string{"src/pkg", ".git"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{
		"pyproject.toml": "[project]\n", "src/pkg/mod.py": "def glyph():\n    pass\n",
		".env": "KEY=1\n", ".git/config": "x\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	real, _ := filepath.EvalSymlinks(dir)
	return real
}

func TestAnOpenedFolderIsReadableByFullPathAndNothingElseOpens(t *testing.T) {
	tb := project(t)
	other := sibling(t)
	file := filepath.Join(other, "src", "pkg", "mod.py")
	if _, err := run(t, tb, ToolRead, map[string]any{"path": file}); err == nil || !strings.Contains(err.Error(), "outside the project") {
		t.Fatalf("a folder that was never opened was readable: %v", err)
	}
	open, err := tb.WithDir(other)
	if err != nil {
		t.Fatal(err)
	}
	res, err := run(t, open, ToolRead, map[string]any{"path": file})
	if err != nil || !strings.Contains(res.Text, "def glyph") {
		t.Fatalf("the opened folder cannot be read: %v %q", err, res.Text)
	}
	// Opening a folder did not change the toolbox it came from.
	if _, err := run(t, tb, ToolRead, map[string]any{"path": file}); err == nil {
		t.Error("WithDir changed the toolbox it was called on")
	}
	// A folder that is a sibling of the opened one is still closed.
	third := sibling(t)
	if _, err := run(t, open, ToolRead, map[string]any{"path": filepath.Join(third, "src", "pkg", "mod.py")}); err == nil {
		t.Error("opening one folder opened another")
	}
}

func TestAnOpenedFolderKeepsTheSecretAndGitGuards(t *testing.T) {
	other := sibling(t)
	open, err := project(t).WithDir(other)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(other, ".env"), filepath.Join(other, ".git", "config")} {
		if _, err := run(t, open, ToolRead, map[string]any{"path": p}); err == nil {
			t.Errorf("%s was readable inside an opened folder", p)
		}
	}
	res, err := run(t, open, ToolGrep, map[string]any{"pattern": "KEY|glyph", "path": other})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Text, "KEY") || !strings.Contains(res.Text, "glyph") {
		t.Errorf("grep in an opened folder: %q", res.Text)
	}
}

func TestAnOpenedFolderCannotBeLeftThroughASymlink(t *testing.T) {
	other := sibling(t)
	secret := t.TempDir()
	if err := os.WriteFile(filepath.Join(secret, "s.txt"), []byte("no"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(other, "link")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	open, _ := project(t).WithDir(other)
	if _, err := run(t, open, ToolRead, map[string]any{"path": filepath.Join(other, "link", "s.txt")}); err == nil {
		t.Error("a symlink carried a read out of the opened folder")
	}
}

func TestWritesInAnOpenedFolderNeedTheFolderOpenToo(t *testing.T) {
	other := sibling(t)
	target := filepath.Join(other, "new.py")
	tb := project(t).WithEdits()
	args := map[string]any{"path": target, "content": "x = 1\n"}
	if _, err := run(t, tb, ToolWrite, args); err == nil {
		t.Fatal("wrote to a folder that was never opened")
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatal("the file exists after a refused write")
	}
	open, _ := tb.WithDir(other)
	if _, err := run(t, open, ToolWrite, args); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "x = 1\n" {
		t.Errorf("file = %q", b)
	}
	// Opening a folder does not turn writing on.
	ro, _ := project(t).WithDir(other)
	if _, err := run(t, ro, ToolWrite, args); err == nil {
		t.Error("a read-only toolbox wrote after a folder was opened")
	}
}

func TestSuggestFolderIsTheNearestProjectAndNeverADriveOrHome(t *testing.T) {
	tb := project(t)
	other := sibling(t)
	deep := filepath.Join(other, "src", "pkg", "mod.py")
	got, err := tb.SuggestFolder(deep)
	if err != nil || got != other {
		t.Fatalf("suggest(%s) = %q, %v; want %s", deep, got, err, other)
	}
	// A file that does not exist yet is judged by the folder it would go in.
	if got, err := tb.SuggestFolder(filepath.Join(other, "src", "new", "x.py")); err != nil || got != other {
		t.Errorf("suggest for a new file = %q, %v", got, err)
	}
	// Two projects, one inside the other: the nearest one is the folder to open.
	inner := filepath.Join(other, "vendor", "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := tb.SuggestFolder(filepath.Join(inner, "a.go")); err != nil || got != inner {
		t.Errorf("nested project: suggest = %q, %v; want the inner %s", got, err, inner)
	}
	// With no marker anywhere the folder holding the file is the answer, not its parent.
	plain := t.TempDir()
	if err := os.WriteFile(filepath.Join(plain, "x.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(plain)
	marked := false
	for cur := real; filepath.Dir(cur) != cur; cur = filepath.Dir(cur) {
		for _, m := range markers {
			if _, err := os.Stat(filepath.Join(cur, m)); err == nil {
				marked = true
			}
		}
	}
	if marked {
		t.Log("the temp folder sits under a project marker; skipping the no-marker case")
	} else if got, err := tb.SuggestFolder(filepath.Join(plain, "x.txt")); err != nil || got != real {
		t.Errorf("suggest in a plain folder = %q, %v; want %s", got, err, real)
	}
	// A root and the home folder are refused.
	// The root of the volume the project is on. "\\" alone is not an absolute path on
	// Windows (no drive), so it would be read as a place under the project, not the root.
	fsRoot := filepath.VolumeName(tb.Root()) + string(filepath.Separator)
	if _, err := tb.SuggestFolder(fsRoot); err == nil {
		t.Errorf("the filesystem root %s was suggested", fsRoot)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if _, err := tb.WithDir(home); err == nil {
			t.Error("the home folder was opened as a whole")
		}
	}
}

func TestOutsideLooksAtTheWordsOfThePath(t *testing.T) {
	tb := project(t)
	if tb.Outside("") || tb.Outside(".") || tb.Outside("src/a.go") || tb.Outside(filepath.Join(tb.Root(), "src")) {
		t.Error("a path inside the project was called outside")
	}
	if !tb.Outside("../x") || !tb.Outside(filepath.Join(filepath.Dir(tb.Root()), "x")) {
		t.Error("a path outside the project was called inside")
	}
	open, _ := tb.WithDir(sibling(t))
	if open.Outside(filepath.Join(open.Roots()[1], "src")) {
		t.Error("a path in an opened folder was called outside")
	}
}

func TestOpeningAFolderAlreadyReachableChangesNothing(t *testing.T) {
	tb := project(t)
	same, err := tb.WithDir(filepath.Join(tb.Root(), "src"))
	if err != nil || len(same.Roots()) != 1 {
		t.Fatalf("roots = %v, %v", same.Roots(), err)
	}
	if _, err := tb.WithDir(filepath.Join(tb.Root(), "nope")); err == nil {
		t.Error("a folder that does not exist was opened")
	}
	if _, err := tb.WithDir(filepath.Join(tb.Root(), "main.go")); err == nil {
		t.Error("a file was opened as a folder")
	}
}
