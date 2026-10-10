package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func coreName() string {
	if runtime.GOOS == "windows" {
		return "arxi.exe"
	}
	return "arxi"
}

func TestCoreBinaryPrefersTheExplicitSetting(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, coreName()), []byte("x"), 0o755)
	exe := func() (string, error) { return filepath.Join(dir, "arxi-tui"), nil }
	if got := coreBinary("/custom/arxi", exe); got != "/custom/arxi" {
		t.Errorf("got %q, want the ARXI_BIN value", got)
	}
}

func TestCoreBinaryFindsTheCoreBesideTheProgram(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, coreName())
	if err := os.WriteFile(want, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe := func() (string, error) { return filepath.Join(dir, "arxi-tui"), nil }
	if got := coreBinary("", exe); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCoreBinaryIsEmptyWhenThereIsNoCore(t *testing.T) {
	dir := t.TempDir()
	exe := func() (string, error) { return filepath.Join(dir, "arxi-tui"), nil }
	if got := coreBinary("", exe); got != "" {
		t.Errorf("got %q, want none", got)
	}
	// A folder that merely has the core's name is not a core.
	os.Mkdir(filepath.Join(dir, coreName()), 0o755)
	if got := coreBinary("", exe); got != "" {
		t.Errorf("directory taken for the core: %q", got)
	}
	bad := func() (string, error) { return "", errors.New("no exe") }
	if got := coreBinary("", bad); got != "" {
		t.Errorf("unknown executable path gave %q", got)
	}
}

// checkout lays out a repository: root/core with the core's module file and entry.
func checkout(t *testing.T) (root string) {
	t.Helper()
	root = t.TempDir()
	os.MkdirAll(filepath.Join(root, "core", "cmd", "arxi"), 0o755)
	os.WriteFile(filepath.Join(root, "core", "go.mod"), []byte("module github.com/michiTrader/arxi\n\ngo 1.22\n"), 0o644)
	os.WriteFile(filepath.Join(root, "core", "cmd", "arxi", "main.go"), []byte("package main\n"), 0o644)
	return root
}

func fakeBuilder(t *testing.T, fail bool) *int {
	t.Helper()
	n := new(int)
	old := coreBuilder
	t.Cleanup(func() { coreBuilder = old })
	coreBuilder = func(src, out string) error {
		*n++
		if fail {
			return errors.New("boom")
		}
		return os.WriteFile(out, []byte("core"), 0o755)
	}
	return n
}

func TestTheCoreIsBuiltFromCoreWhenNoBinaryExists(t *testing.T) {
	root := checkout(t)
	builds := fakeBuilder(t, false)
	exe := func() (string, error) { return filepath.Join(root, "arxi-tui"), nil }
	var notes []string
	got := prepareCore("", exe, func(l string) { notes = append(notes, l) })
	if want := filepath.Join(root, coreName()); got != want || *builds != 1 {
		t.Fatalf("got %q after %d builds, want %q built once", got, *builds, want)
	}
	if len(notes) != 1 {
		t.Errorf("the user must be told the build is happening: %v", notes)
	}
	// Second start: the binary is current, so nothing is rebuilt and nothing is said.
	notes = nil
	if got2 := prepareCore("", exe, func(l string) { notes = append(notes, l) }); got2 != got || *builds != 1 || len(notes) != 0 {
		t.Errorf("a current core must be reused silently: %q, %d builds, %v", got2, *builds, notes)
	}
}

func TestAStaleCoreIsRebuiltAndAFailedRebuildKeepsTheOldOne(t *testing.T) {
	root := checkout(t)
	bin := filepath.Join(root, coreName())
	os.WriteFile(bin, []byte("old"), 0o755)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(bin, old, old)
	exe := func() (string, error) { return filepath.Join(root, "arxi-tui"), nil }

	fails := fakeBuilder(t, true)
	var notes []string
	if got := prepareCore("", exe, func(l string) { notes = append(notes, l) }); got != bin || *fails != 1 {
		t.Fatalf("a failed rebuild must keep the old core: %q, %d", got, *fails)
	}
	if joined := strings.Join(notes, " "); !strings.Contains(joined, "older one") || !strings.Contains(joined, "go build") {
		t.Errorf("a failed rebuild must name the consequence and the remedy: %v", notes)
	}
	if _, err := os.Stat(bin + ".new"); err == nil {
		t.Error("a failed build left its temporary file behind")
	}

	builds := fakeBuilder(t, false)
	if got := prepareCore("", exe, func(string) {}); got != bin || *builds != 1 {
		t.Fatalf("a stale core must be rebuilt: %q, %d", got, *builds)
	}
	if b, _ := os.ReadFile(bin); string(b) != "core" {
		t.Error("the rebuilt core did not replace the stale one")
	}
}

// The counterfactuals: an explicit ARXI_BIN is never rebuilt over, and a project
// folder that merely contains a core/ is never compiled.
func TestTheBuildNeverTouchesAnExplicitChoiceOrTheUsersProject(t *testing.T) {
	root := checkout(t)
	builds := fakeBuilder(t, false)
	exe := func() (string, error) { return filepath.Join(root, "arxi-tui"), nil }
	if got := prepareCore("/mine/arxi", exe, func(string) {}); got != "/mine/arxi" || *builds != 0 {
		t.Errorf("ARXI_BIN must win untouched: %q, %d", got, *builds)
	}
	project := checkout(t)
	other := t.TempDir()
	t.Chdir(project)
	exe2 := func() (string, error) { return filepath.Join(other, "arxi-tui"), nil }
	if got := prepareCore("", exe2, func(string) {}); got != "" || *builds != 0 {
		t.Errorf("code in the working folder must never be built: %q, %d", got, *builds)
	}
	// A core/ of some other module is not the arxi core.
	os.WriteFile(filepath.Join(root, "core", "go.mod"), []byte("module example.com/other\n"), 0o644)
	os.Remove(filepath.Join(root, coreName()))
	if got := prepareCore("", exe, func(string) {}); got != "" || *builds != 0 {
		t.Errorf("a core/ of another module was built: %q, %d", got, *builds)
	}
}
