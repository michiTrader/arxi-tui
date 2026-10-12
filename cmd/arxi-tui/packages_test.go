//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	extv2 "github.com/michiTrader/arxi_tui/internal/ext/v2"
)

const pkgHead = `"protocol":"ext/v2","version":"1"`

func pkgDoc(id, extra string) string {
	return `{` + pkgHead + `,"id":"` + id + `","name":"` + id + `"` + extra + `}`
}

const sayCmd = `,"commands":[{"name":"hi","op":{"kind":"builtin","name":"x"}}]`

var openNS = packageNamespace{
	taken:    func(n string) bool { return isFactoryCommand(n) },
	builtins: func(string) bool { return true },
}

func write(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func problemsText(s packageSet) string { return strings.Join(s.problems, "\n") }

func TestLoadPackagesReadsDocumentsAndFolders(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "alpha.json"), pkgDoc("alpha", sayCmd), 0o644)
	write(t, filepath.Join(dir, "beta", "package.json"), pkgDoc("beta", `,"commands":[{"name":"run","op":{"kind":"exec","argv":["./go.sh"]}}]`), 0o644)
	write(t, filepath.Join(dir, "beta", "go.sh"), "#!/bin/sh\necho hi\n", 0o755)
	write(t, filepath.Join(dir, ".hidden.json"), "not even json", 0o644)
	write(t, filepath.Join(dir, "notes.txt"), "ignored", 0o644)

	set := loadPackages(dir, scopeUser, "", nil, openNS)
	if len(set.problems) != 0 {
		t.Fatalf("problems: %s", problemsText(set))
	}
	if len(set.pkgs) != 2 || set.pkgs[0].ID != "alpha" || set.pkgs[1].ID != "beta" {
		t.Fatalf("got %+v", set.pkgs)
	}
	if set.pkgs[0].Dir != "" || set.pkgs[1].Dir == "" {
		t.Fatalf("Dir: %q %q", set.pkgs[0].Dir, set.pkgs[1].Dir)
	}
	for _, p := range set.pkgs {
		if p.Granted || p.runnable() {
			t.Errorf("%s: a package nobody agreed to must not run", p.ID)
		}
	}
	if _, _, ok := set.commandOwner("hi"); ok {
		t.Error("a command of a package without a grant must not be found")
	}
}

func TestLoadPackagesNamesWhatIsWrongAndKeepsTheRest(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "good.json"), pkgDoc("good", sayCmd), 0o644)
	write(t, filepath.Join(dir, "broken.json"), `{"protocol":`, 0o644)
	write(t, filepath.Join(dir, "mismatch.json"), pkgDoc("other", sayCmd), 0o644)
	write(t, filepath.Join(dir, "shadow.json"), pkgDoc("shadow", `,"commands":[{"name":"help","op":{"kind":"builtin","name":"x"}}]`), 0o644)
	write(t, filepath.Join(dir, "future.json"), pkgDoc("future", `,"tools":[]`), 0o644)
	write(t, filepath.Join(dir, "big.json"), pkgDoc("big", `,"description":"`+strings.Repeat("x", extv2.MaxDocBytes)+`"`), 0o644)
	if err := os.Symlink(filepath.Join(dir, "good.json"), filepath.Join(dir, "link.json")); err != nil {
		t.Fatal(err)
	}

	set := loadPackages(dir, scopeUser, "", nil, openNS)
	if len(set.pkgs) != 1 || set.pkgs[0].ID != "good" {
		t.Fatalf("one broken file must not take the others down: %+v", set.pkgs)
	}
	text := problemsText(set)
	for _, want := range []string{"broken.json", "saved as", "mismatch.json", "shadow.json", "future.json", "reserved", "big.json", "link.json", "is a link"} {
		if !strings.Contains(text, want) {
			t.Errorf("problems lack %q:\n%s", want, text)
		}
	}
}

func TestGrantIsBoundToTheDocumentAndEveryFileOfTheFolder(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	dir := filepath.Join(cfg, packagesDirName)
	doc := pkgDoc("tool", `,"commands":[{"name":"run","op":{"kind":"exec","argv":["./go.sh"]}}]`)
	write(t, filepath.Join(dir, "tool", "package.json"), doc, 0o644)
	write(t, filepath.Join(dir, "tool", "go.sh"), "#!/bin/sh\necho one\n", 0o755)

	load := func() installedPackage {
		set := loadPackages(dir, scopeUser, "", readGrants(), openNS)
		if len(set.pkgs) != 1 {
			t.Fatalf("problems: %s", problemsText(set))
		}
		return set.pkgs[0]
	}
	p := load()
	if p.Granted {
		t.Fatal("not granted yet")
	}
	if err := grantPackage(p, ""); err != nil {
		t.Fatal(err)
	}
	if p = load(); !p.Granted || !p.runnable() {
		t.Fatal("the grant must hold for the same bytes")
	}

	mutations := map[string]func(){
		"script body":     func() { write(t, filepath.Join(dir, "tool", "go.sh"), "#!/bin/sh\necho two\n", 0o755) },
		"script exec bit": func() { _ = os.Chmod(filepath.Join(dir, "tool", "go.sh"), 0o644) },
		"extra file":      func() { write(t, filepath.Join(dir, "tool", "lib.sh"), "x", 0o644) },
		"subfolder":       func() { _ = os.MkdirAll(filepath.Join(dir, "tool", "sub"), 0o755) },
		"link in folder":  func() { _ = os.Symlink("/etc/passwd", filepath.Join(dir, "tool", "l")) },
		"document argv": func() {
			write(t, filepath.Join(dir, "tool", "package.json"), strings.Replace(doc, "./go.sh", "./go.sh\",\"-x", 1), 0o644)
		},
		"document metadata": func() {
			write(t, filepath.Join(dir, "tool", "package.json"), strings.Replace(doc, `"name":"tool"`, `"name":"Tool 2"`, 1), 0o644)
		},
	}
	for name, mutate := range mutations {
		// Put the folder back to what was granted, then change one thing.
		write(t, filepath.Join(dir, "tool", "package.json"), doc, 0o644)
		write(t, filepath.Join(dir, "tool", "go.sh"), "#!/bin/sh\necho one\n", 0o755)
		// WriteFile keeps the mode of a file that exists, so say it again.
		if err := os.Chmod(filepath.Join(dir, "tool", "go.sh"), 0o755); err != nil {
			t.Fatal(err)
		}
		_ = os.RemoveAll(filepath.Join(dir, "tool", "lib.sh"))
		_ = os.RemoveAll(filepath.Join(dir, "tool", "sub"))
		_ = os.Remove(filepath.Join(dir, "tool", "l"))
		if !load().Granted {
			t.Fatalf("%s: could not restore the granted state", name)
		}
		mutate()
		set := loadPackages(dir, scopeUser, "", readGrants(), openNS)
		if len(set.pkgs) == 1 && set.pkgs[0].Granted {
			t.Errorf("%s: an edit after the grant must ask again", name)
		}
	}
}

func TestSwitchingOffAndOnAgainDoesNotAskAgain(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	dir := filepath.Join(cfg, packagesDirName)
	write(t, filepath.Join(dir, "a.json"), pkgDoc("a", sayCmd), 0o644)
	p := loadPackages(dir, scopeUser, "", nil, openNS).pkgs[0]
	if err := grantPackage(p, ""); err != nil {
		t.Fatal(err)
	}
	off := pkgDoc("a", `,"disabled":true,"commands":[{"name":"hi","disabled":true,"op":{"kind":"builtin","name":"x"}}]`)
	write(t, filepath.Join(dir, "a.json"), off, 0o644)
	q := loadPackages(dir, scopeUser, "", readGrants(), openNS).pkgs[0]
	if !q.Granted || q.runnable() {
		t.Fatalf("switched off: granted=%v runnable=%v; the grant stays but nothing runs", q.Granted, q.runnable())
	}
}

func TestProjectGrantIsPerFolder(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	a, b := t.TempDir(), t.TempDir()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "p.json"), pkgDoc("p", sayCmd), 0o644)
	pa := loadPackages(dir, scopeProject, a, nil, openNS).pkgs[0]
	if err := grantPackage(pa, a); err != nil {
		t.Fatal(err)
	}
	if !loadPackages(dir, scopeProject, a, readGrants(), openNS).pkgs[0].Granted {
		t.Error("granted in folder a")
	}
	if loadPackages(dir, scopeProject, b, readGrants(), openNS).pkgs[0].Granted {
		t.Error("the same package id in another folder is another question")
	}
	if loadPackages(dir, scopeUser, "", readGrants(), openNS).pkgs[0].Granted {
		t.Error("a project grant is not a user grant")
	}
}

func TestMergePackagesUserWinsAndNamesTheClash(t *testing.T) {
	u, p := t.TempDir(), t.TempDir()
	write(t, filepath.Join(u, "mine.json"), pkgDoc("mine", sayCmd), 0o644)
	write(t, filepath.Join(p, "mine.json"), pkgDoc("mine", `,"commands":[{"name":"other","op":{"kind":"builtin","name":"x"}}]`), 0o644)
	write(t, filepath.Join(p, "theirs.json"), pkgDoc("theirs", sayCmd), 0o644) // /hi again
	write(t, filepath.Join(p, "fine.json"), pkgDoc("fine", `,"commands":[{"name":"fine","op":{"kind":"builtin","name":"x"}}]`), 0o644)
	set := mergePackages(loadPackages(u, scopeUser, "", nil, openNS), loadPackages(p, scopeProject, "c", nil, openNS))
	var ids []string
	for _, x := range set.pkgs {
		ids = append(ids, x.ID)
	}
	if strings.Join(ids, ",") != "mine,fine" {
		t.Fatalf("ids = %v", ids)
	}
	text := problemsText(set)
	if !strings.Contains(text, `package id "mine" is already used`) || !strings.Contains(text, "/hi is already a command of package") {
		t.Fatalf("problems:\n%s", text)
	}
}

func TestSaveRemoveAndRevoke(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	dir := filepath.Join(cfg, packagesDirName)
	pkg, err := extv2.Parse([]byte(pkgDoc("a", sayCmd)))
	if err != nil {
		t.Fatal(err)
	}
	path, err := savePackage(dir, pkg, nil)
	if err != nil || filepath.Base(path) != "a.json" {
		t.Fatalf("%s %v", path, err)
	}
	set := loadPackages(dir, scopeUser, "", readGrants(), openNS)
	if len(set.pkgs) != 1 {
		t.Fatalf("%s", problemsText(set))
	}
	p := set.pkgs[0]
	if err := grantPackage(p, ""); err != nil {
		t.Fatal(err)
	}
	if err := removePackage(p, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the file must be gone")
	}
	if len(readGrants()) != 0 {
		t.Fatalf("the grant must go with the package: %v", readGrants())
	}
	// The same id installed again is a new question, even with the same bytes.
	if _, err := savePackage(dir, pkg, nil); err != nil {
		t.Fatal(err)
	}
	if loadPackages(dir, scopeUser, "", readGrants(), openNS).pkgs[0].Granted {
		t.Fatal("a re-installed package must be asked about again")
	}
}

func TestPackageFolderBounds(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "fat", "package.json"), pkgDoc("fat", sayCmd), 0o644)
	for i := 0; i <= maxPackageFiles; i++ {
		write(t, filepath.Join(dir, "fat", "f", string(rune('a'+i%26)), strings.Repeat("n", i/26+1)), "x", 0o644)
	}
	set := loadPackages(dir, scopeUser, "", nil, openNS)
	if len(set.pkgs) != 0 || !strings.Contains(problemsText(set), "more than") {
		t.Fatalf("a folder with too many files must be refused: %+v %s", set.pkgs, problemsText(set))
	}
}
