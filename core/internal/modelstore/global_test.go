package modelstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func noEnv(string) string { return "" }

func noLegacy() (string, error) { return "", errors.New("no config dir") }

func cfg(dir string) func() (string, error) { return func() (string, error) { return dir, nil } }

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProvidersLiveInTheGlobalFolderWhateverTheWorkingDirectory(t *testing.T) {
	config, cwd := t.TempDir(), t.TempDir()
	dir, n, err := Locate(noEnv, cfg(config), noLegacy, cwd)
	if err != nil || n != 0 {
		t.Fatalf("dir=%q migrated=%d err=%v", dir, n, err)
	}
	if want := filepath.Join(config, ".arxi", "providers"); dir != want {
		t.Errorf("dir = %q, want %q", dir, want)
	}
	if _, err := os.Stat(filepath.Join(cwd, "providers")); err == nil {
		t.Error("nothing may be created in the working directory")
	}
	// Another working directory reaches the same folder.
	other, _, _ := Locate(noEnv, cfg(config), noLegacy, t.TempDir())
	if other != dir {
		t.Errorf("a second folder got %q, want %q", other, dir)
	}
}

func TestTheEnvironmentOverridesTheLocation(t *testing.T) {
	env := func(k string) string {
		if k == EnvDir {
			return " /somewhere/else "
		}
		return ""
	}
	dir, n, err := Locate(env, cfg(t.TempDir()), noLegacy, t.TempDir())
	if err != nil || n != 0 || dir != "/somewhere/else" {
		t.Errorf("dir=%q n=%d err=%v", dir, n, err)
	}
}

func TestExistingLocalProvidersAreCopiedOnceAndNeverMoved(t *testing.T) {
	config, cwd := t.TempDir(), t.TempDir()
	local := filepath.Join(cwd, "providers")
	write(t, local, "openai.json", `{"name":"openai"}`)
	write(t, local, "vyceai.json", `{"name":"vyceai"}`)
	write(t, local, "default-model", "openai/gpt-4o\n")
	write(t, local, "notes.txt", "not a provider")

	dir, n, err := Locate(noEnv, cfg(config), noLegacy, cwd)
	if err != nil || n != 3 {
		t.Fatalf("migrated=%d err=%v (want the 2 providers and the default model)", n, err)
	}
	for name, want := range map[string]string{"openai.json": `{"name":"openai"}`, "default-model": "openai/gpt-4o\n"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v", name, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err == nil {
		t.Error("a file that is not a provider was copied")
	}
	if _, err := os.Stat(filepath.Join(local, "openai.json")); err != nil {
		t.Error("the original must be left where it was")
	}
	if fi, _ := os.Stat(filepath.Join(dir, "openai.json")); fi != nil && fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf("a copied provider is readable by others: %v", fi.Mode())
	}

	// The second start does not copy again, so a provider removed globally stays removed.
	if err := os.Remove(filepath.Join(dir, "vyceai.json")); err != nil {
		t.Fatal(err)
	}
	_, n, err = Locate(noEnv, cfg(config), noLegacy, cwd)
	if err != nil || n != 0 {
		t.Errorf("second start: migrated=%d err=%v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "vyceai.json")); err == nil {
		t.Error("a removed provider came back")
	}
}

func TestAnEmptyOrMissingLocalFolderMigratesNothingAndCreatesNothing(t *testing.T) {
	for _, setup := range []func(cwd string){
		func(string) {},
		func(cwd string) { write(t, filepath.Join(cwd, "providers"), "default-model", "x/y") },
	} {
		config, cwd := t.TempDir(), t.TempDir()
		setup(cwd)
		dir, n, err := Locate(noEnv, cfg(config), noLegacy, cwd)
		if err != nil || n != 0 {
			t.Fatalf("migrated=%d err=%v", n, err)
		}
		if _, err := os.Stat(dir); err == nil {
			t.Error("the global folder must not be created until there is something to put in it")
		}
	}
}

func TestWithoutAConfigDirectoryItFallsBackToTheLocalFolder(t *testing.T) {
	dir, n, err := Locate(noEnv, func() (string, error) { return "", errors.New("no home") }, noLegacy, t.TempDir())
	if err != nil || n != 0 || dir != DefaultDir {
		t.Errorf("dir=%q n=%d err=%v", dir, n, err)
	}
}

// The case that made a plain "copy when the global folder is missing" rule wrong: a
// provider is added from a fresh folder first, which creates the global folder, and
// the providers of an older folder must still be brought along afterwards, without
// overwriting anything.
func TestAnOlderFolderIsStillBroughtAlongAfterTheGlobalFolderExists(t *testing.T) {
	config := t.TempDir()
	fresh, older := t.TempDir(), t.TempDir()
	dir, _, _ := Locate(noEnv, cfg(config), noLegacy, fresh)
	write(t, dir, "mine.json", `{"name":"mine"}`)
	write(t, dir, "default-model", "mine/m\n")

	write(t, filepath.Join(older, "providers"), "old.json", `{"name":"old"}`)
	write(t, filepath.Join(older, "providers"), "mine.json", `{"name":"STALE"}`)
	write(t, filepath.Join(older, "providers"), "default-model", "old/o\n")
	_, n, err := Locate(noEnv, cfg(config), noLegacy, older)
	if err != nil || n != 1 {
		t.Fatalf("migrated=%d err=%v (only old.json is new)", n, err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "mine.json")); string(got) != `{"name":"mine"}` {
		t.Errorf("an existing provider was overwritten: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "default-model")); string(got) != "mine/m\n" {
		t.Errorf("the default model was replaced: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "old.json")); err != nil {
		t.Error("old.json was not brought along")
	}
	if err := os.Remove(filepath.Join(dir, "old.json")); err != nil {
		t.Fatal(err)
	}
	if _, n, _ = Locate(noEnv, cfg(config), noLegacy, older); n != 0 {
		t.Errorf("the same folder was copied twice (%d)", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "old.json")); err == nil {
		t.Error("a removed provider came back")
	}
}

// The folder the previous version used (<config dir>/arxi/providers) is brought along
// once and left where it is.
func TestTheOldGlobalFolderIsImportedOnceAndKept(t *testing.T) {
	home, oldCfg := t.TempDir(), t.TempDir()
	old := LegacyDirIn(oldCfg)
	write(t, old, "openai.json", `{"name":"openai"}`)
	write(t, old, "default-model", "openai/gpt-4o\n")

	dir, n, err := Locate(noEnv, cfg(home), cfg(oldCfg), t.TempDir())
	if err != nil || n != 2 {
		t.Fatalf("migrated=%d err=%v", n, err)
	}
	if dir != filepath.Join(home, ".arxi", "providers") {
		t.Errorf("dir = %q", dir)
	}
	if _, err := os.Stat(filepath.Join(old, "openai.json")); err != nil {
		t.Error("the old folder must be kept")
	}
	if err := os.Remove(filepath.Join(dir, "openai.json")); err != nil {
		t.Fatal(err)
	}
	if _, n, _ = Locate(noEnv, cfg(home), cfg(oldCfg), t.TempDir()); n != 0 {
		t.Errorf("imported twice (%d)", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "openai.json")); err == nil {
		t.Error("a removed provider came back from the old folder")
	}
}
