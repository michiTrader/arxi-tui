package chattools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func editable(t *testing.T) (*Toolbox, string) {
	t.Helper()
	tb := project(t).WithEdits()
	return tb, tb.Root()
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestWriteCreatesAFileAndItsFolders(t *testing.T) {
	tb, root := editable(t)
	res, err := run(t, tb, ToolWrite, map[string]any{"path": "pkg/new/x.go", "content": "package x\n\nvar A = 1\n"})
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(root, "pkg", "new", "x.go")); got != "package x\n\nvar A = 1\n" {
		t.Errorf("content = %q", got)
	}
	if res.Summary != "Wrote 3 lines" || res.Arg != "pkg/new/x.go" {
		t.Errorf("result = %+v", res)
	}
	if !strings.Contains(res.Diff, "+ package x") || strings.Contains(res.Diff, " - ") {
		t.Errorf("a new file's diff is all additions:\n%s", res.Diff)
	}
}

func TestWriteReplacesAFileAndKeepsItsMode(t *testing.T) {
	tb, root := editable(t)
	p := filepath.Join(root, "run.sh")
	if err := os.WriteFile(p, []byte("a\nb\nc\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := run(t, tb, ToolWrite, map[string]any{"path": "run.sh", "content": "a\nB\nc\nd\n"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary != "Wrote 4 lines (Added 2 lines, removed 1 line)" {
		t.Errorf("summary = %q", res.Summary)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o755 && filepath.Separator == '/' {
		t.Errorf("mode = %v, want 0755 kept", st.Mode().Perm())
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".arxi-write-") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
}

func TestEditReplacesTheOneOccurrence(t *testing.T) {
	tb, root := editable(t)
	res, err := run(t, tb, ToolEdit, map[string]any{"path": "main.go", "old_string": "func main() {}", "new_string": "func main() {\n\tprintln(\"hi\")\n}"})
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(root, "main.go")); got != "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n" {
		t.Errorf("content = %q", got)
	}
	if res.Summary != "Added 3 lines, removed 1 line" {
		t.Errorf("summary = %q", res.Summary)
	}
	for _, want := range []string{"    3 - func main() {}", "    3 + func main() {", "    4 + \tprintln(\"hi\")", "    1   package main"} {
		if !strings.Contains(res.Diff, want) {
			t.Errorf("diff lacks %q:\n%s", want, res.Diff)
		}
	}
}

func TestEditRefusesWhatWouldBeAGuess(t *testing.T) {
	tb, root := editable(t)
	p := filepath.Join(root, "dup.txt")
	os.WriteFile(p, []byte("x\ny\nx\n"), 0o644)
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"not found", map[string]any{"path": "dup.txt", "old_string": "zzz", "new_string": "q"}, "was not found"},
		{"ambiguous", map[string]any{"path": "dup.txt", "old_string": "x", "new_string": "q"}, "appears 2 times"},
		{"same", map[string]any{"path": "dup.txt", "old_string": "x", "new_string": "x"}, "nothing to change"},
		{"empty old", map[string]any{"path": "dup.txt", "old_string": "", "new_string": "q"}, "needs old_string"},
		{"missing new", map[string]any{"path": "dup.txt", "old_string": "x"}, "needs new_string"},
		{"no file", map[string]any{"path": "ghost.txt", "old_string": "a", "new_string": "b"}, "does not exist"},
		{"folder", map[string]any{"path": "src", "old_string": "a", "new_string": "b"}, "is a folder"},
		{"binary", map[string]any{"path": "blob.bin", "old_string": "a", "new_string": "b"}, "binary"},
	}
	for _, c := range cases {
		if _, err := run(t, tb, ToolEdit, c.args); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want it to mention %q", c.name, err, c.want)
		}
	}
	if got := readFile(t, p); got != "x\ny\nx\n" {
		t.Errorf("a refused edit changed the file: %q", got)
	}
}

func TestEditReplaceAllChangesEveryOccurrence(t *testing.T) {
	tb, root := editable(t)
	p := filepath.Join(root, "dup.txt")
	os.WriteFile(p, []byte("x\ny\nx\n"), 0o644)
	if _, err := run(t, tb, ToolEdit, map[string]any{"path": "dup.txt", "old_string": "x", "new_string": "q", "replace_all": true}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, p); got != "q\ny\nq\n" {
		t.Errorf("content = %q", got)
	}
}

func TestEditMeetsWindowsLineEndings(t *testing.T) {
	tb, root := editable(t)
	p := filepath.Join(root, "crlf.txt")
	os.WriteFile(p, []byte("one\r\ntwo\r\nthree\r\n"), 0o644)
	if _, err := run(t, tb, ToolEdit, map[string]any{"path": "crlf.txt", "old_string": "one\ntwo", "new_string": "1\n2"}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, p); got != "1\r\n2\r\nthree\r\n" {
		t.Errorf("content = %q; the file's own line endings must survive", got)
	}
}

func TestChangesStayInsideTheProject(t *testing.T) {
	tb, root := editable(t)
	outsideDir := t.TempDir()
	if err := os.Symlink(outsideDir, filepath.Join(root, "link")); err != nil {
		t.Skip("symlinks not available:", err)
	}
	for _, p := range []string{"../escape.txt", filepath.Join(outsideDir, "abs.txt"), "link/new.txt", "link"} {
		_, err := run(t, tb, ToolWrite, map[string]any{"path": p, "content": "x"})
		if err == nil {
			t.Errorf("write to %q must be refused", p)
		}
	}
	if _, err := os.Stat(filepath.Join(outsideDir, "new.txt")); err == nil {
		t.Error("a file was created outside the project through a symlinked folder")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escape.txt")); err == nil {
		t.Error("a file was created above the project")
	}
}

func TestGitAndSecretsAreNeverChanged(t *testing.T) {
	tb, root := editable(t)
	for _, p := range []string{".git/config", ".git/hooks/pre-commit", ".env", "sub/.env.local", "keys/id_rsa", "cert.pem"} {
		_, err := run(t, tb, ToolWrite, map[string]any{"path": p, "content": "x"})
		if err == nil {
			t.Errorf("write to %q must be refused", p)
		}
	}
	if got := readFile(t, filepath.Join(root, ".git", "config")); !strings.Contains(got, "secret") {
		t.Errorf(".git/config was touched: %q", got)
	}
	if _, err := run(t, tb, ToolWrite, map[string]any{"path": ".env.example", "content": "A=\n"}); err != nil {
		t.Errorf("a template is meant to be written: %v", err)
	}
}

func TestWithoutEditsTheToolsRefuseWhateverIsAsked(t *testing.T) {
	tb := project(t)
	for _, name := range []string{ToolWrite, ToolEdit} {
		_, err := run(t, tb, name, map[string]any{"path": "main.go", "content": "x", "old_string": "package", "new_string": "q"})
		if err == nil || !strings.Contains(err.Error(), "only look") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if got := readFile(t, filepath.Join(tb.Root(), "main.go")); !strings.HasPrefix(got, "package main") {
		t.Errorf("a read-only toolbox changed a file: %q", got)
	}
}

func TestPreviewShowsTheChangeAndTouchesNothing(t *testing.T) {
	tb, root := editable(t)
	raw := []byte(`{"path":"main.go","old_string":"package main","new_string":"package app"}`)
	res, err := tb.Preview(ToolEdit, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Diff, "- package main") || !strings.Contains(res.Diff, "+ package app") {
		t.Errorf("diff = %q", res.Diff)
	}
	if got := readFile(t, filepath.Join(root, "main.go")); !strings.HasPrefix(got, "package main") {
		t.Errorf("preview changed the file: %q", got)
	}
	if _, err := tb.Preview(ToolEdit, []byte(`{"path":"main.go","old_string":"nope","new_string":"x"}`)); err == nil {
		t.Error("a preview must refuse what the run would refuse")
	}
}

func TestSameContentIsReportedAsNoChange(t *testing.T) {
	tb, _ := editable(t)
	res, err := run(t, tb, ToolWrite, map[string]any{"path": "main.go", "content": "package main\n\nfunc main() {}\n"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary != "No changes" || res.Diff != "" {
		t.Errorf("result = %+v", res)
	}
}

func TestDiffSeparatesDistantChangesAndKeepsContext(t *testing.T) {
	var a, b []string
	for i := 1; i <= 40; i++ {
		a = append(a, "line"+string(rune('A'+i%26)))
	}
	b = append(b, a...)
	b[2], b[35] = "CHANGED-EARLY", "CHANGED-LATE"
	d, added, removed := lineDiff(strings.Join(a, "\n")+"\n", strings.Join(b, "\n")+"\n")
	if added != 2 || removed != 2 {
		t.Fatalf("added=%d removed=%d", added, removed)
	}
	if !strings.Contains(d, "\n...\n") {
		t.Errorf("distant changes should be separated:\n%s", d)
	}
	if n := strings.Count(d, "\n"); n > 20 {
		t.Errorf("the diff should show only the changes and their context, got %d lines:\n%s", n, d)
	}
}

func TestHugeDiffIsCutAndSaysSo(t *testing.T) {
	var a, b strings.Builder
	for i := 0; i < 500; i++ {
		a.WriteString("old\n")
		b.WriteString("new\n")
	}
	d, _, _ := lineDiff(a.String(), b.String())
	if !strings.Contains(d, "more lines of diff") {
		t.Errorf("a long diff must say it was cut:\n%s", d[len(d)-200:])
	}
	if n := strings.Count(d, "\n"); n > maxDiffLines+2 {
		t.Errorf("diff has %d lines, cap is %d", n, maxDiffLines)
	}
}

func TestEditDefinitionsNameTheTools(t *testing.T) {
	var names []string
	for _, d := range EditDefinitions() {
		names = append(names, d.Name)
		if len(d.Schema) == 0 || d.Description == "" {
			t.Errorf("%s has no schema or description", d.Name)
		}
	}
	if strings.Join(names, ",") != "edit,write" || !Mutating("edit") || !Mutating("write") || Mutating("read") {
		t.Errorf("names = %v", names)
	}
}
