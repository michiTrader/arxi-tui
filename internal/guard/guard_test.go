// Package guard holds only tests: the limits of docs/GUARDRAILS.md, read from that document
// and checked against the repository. It has no code of its own to ship.
package guard

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const root = "../.."

// section returns the table rows between the markers of one limit in docs/GUARDRAILS.md.
func section(t *testing.T, name string) [][]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "docs/GUARDRAILS.md"))
	if err != nil {
		t.Fatalf("docs/GUARDRAILS.md must exist, it is where the limits are written: %v", err)
	}
	s := string(raw)
	begin, end := "<!-- "+name+":begin -->", "<!-- "+name+":end -->"
	i, j := strings.Index(s, begin), strings.Index(s, end)
	if i < 0 || j < i {
		t.Fatalf("docs/GUARDRAILS.md lost its %s markers; put them back around the table", name)
	}
	var rows [][]string
	for _, line := range strings.Split(s[i+len(begin):j], "\n") {
		if !strings.HasPrefix(line, "| `") && !strings.HasPrefix(line, "| android") {
			continue // header, separator, blank
		}
		var cells []string
		for _, c := range strings.Split(strings.Trim(line, "| "), "|") {
			cells = append(cells, strings.Trim(strings.TrimSpace(c), "`"))
		}
		rows = append(rows, cells)
	}
	return rows
}

var requireLine = regexp.MustCompile(`^\s*([a-zA-Z0-9][^\s]*\.[^\s/]+/[^\s]+)\s+v`)

// modulesOf lists the modules a go.mod requires, direct and indirect.
func modulesOf(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimPrefix(strings.TrimSpace(line), "require ")
		if m := requireLine.FindStringSubmatch(line); m != nil {
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

// A new module changes the install, the binary and Termux, so it is decided in the document.
func TestEveryModuleInGoModIsListedInGuardrails(t *testing.T) {
	listed := map[string]bool{}
	for _, r := range section(t, "guard-deps") {
		listed[r[0]] = true
	}
	have := modulesOf(t, "go.mod")
	for _, m := range have {
		if !listed[m] {
			t.Errorf("go.mod requires %s, which docs/GUARDRAILS.md does not allow. Every dependency is one more thing to install, to ship in the binary and to keep working on Termux. If it is worth it, add a row with the reason (section 1); if not, remove the import.", m)
		}
		delete(listed, m)
	}
	for m := range listed {
		t.Errorf("docs/GUARDRAILS.md lists %s but go.mod does not require it any more: the document would allow something that is gone. Remove the row.", m)
	}
}

// The core is its own module with no third-party code; it ships beside the TUI.
func TestTheCoreModuleHasNoThirdPartyDependencies(t *testing.T) {
	if mods := modulesOf(t, "core/go.mod"); len(mods) != 0 {
		t.Errorf("core/go.mod requires %v. The core is installed next to the TUI on every platform, Termux included, and has none; a dependency there is a decision for docs/GUARDRAILS.md (section 1), not a casual import.", mods)
	}
}

// import "C" turns every release into a build that needs a C toolchain and stops being one
// static file. Nothing in the repository may ask for it.
func TestNoGoFileImportsC(t *testing.T) {
	cgo := regexp.MustCompile(`(?m)^\s*import\s+"C"\s*$|^\s*"C"\s*$`)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if cgo.Match(b) {
			t.Errorf("%s imports \"C\". Releases are built with CGO_ENABLED=0 so that one static file installs on Termux with nothing else; cgo breaks that. Use the system call directly, or put the C part in a helper program the user runs on their own (docs/GUARDRAILS.md section 2).", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The release script is what actually makes the Termux file; if its matrix or its static
// flag goes, the guarantee goes with it without any test noticing.
func TestTheReleaseStillBuildsStaticAndForTermux(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(root, "scripts/build-release.sh"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	// The flag must be on the command that builds, not merely somewhere in the file: the
	// words also sit in a comment and an echo, which would keep a check on the text green
	// after the flag was gone from every go build. Continuation lines are joined first.
	builds := 0
	for _, cmd := range strings.Split(strings.ReplaceAll(s, "\\\n", " "), "\n") {
		cmd = strings.TrimSpace(cmd)
		if strings.HasPrefix(cmd, "#") || !strings.Contains(cmd, "go build") || strings.HasPrefix(cmd, "echo") {
			continue
		}
		builds++
		if !strings.Contains(cmd, "CGO_ENABLED=0") {
			t.Errorf("scripts/build-release.sh has a go build without CGO_ENABLED=0: %q. That artifact would link C and need it at run time, so it would not be one static file. Put the flag on that command.", cmd)
		}
	}
	if builds == 0 {
		t.Error("scripts/build-release.sh has no go build command that this test can read; if the script was restructured, teach the test the new shape rather than dropping the check.")
	}
	if !strings.Contains(s, `"android/arm64"`) {
		t.Error("scripts/build-release.sh no longer builds android/arm64: Termux would have no download. Put the target back in TARGETS.")
	}
}

// The Termux build is made here the way the release makes it, and measured against the
// budget in the document.
func TestTheTermuxBinaryBuildsAndFitsItsBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the Termux binary; skipped with -short")
	}
	var budget int64
	for _, r := range section(t, "guard-size") {
		if r[0] == "android/arm64" {
			var err error
			if budget, err = strconv.ParseInt(r[2], 10, 64); err != nil {
				t.Fatalf("docs/GUARDRAILS.md budget %q is not a number of bytes", r[2])
			}
		}
	}
	if budget == 0 {
		t.Fatal("docs/GUARDRAILS.md has no android/arm64 budget row (section 3)")
	}
	out := filepath.Join(t.TempDir(), "arxi-tui")
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", out, "./cmd/arxi-tui")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=android", "GOARCH=arm64")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the Termux build (android/arm64, CGO_ENABLED=0) fails, so Termux users would get no binary:\n%s\nFix the code that does not compile there (a Unix-only call needs a build tag).", b)
	}
	st, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() > budget {
		t.Errorf("the Termux binary is %d bytes and the budget is %d: it grew by %d. Every megabyte is download and storage on a phone. If the growth buys something, raise the budget in docs/GUARDRAILS.md (section 3) and say what; if not, find what pulled it in.",
			st.Size(), budget, st.Size()-budget)
	}
	t.Logf("android/arm64: %d bytes, budget %d", st.Size(), budget)
}

// The factory interface and its goldens are pinned by hash: moving one is a decision.
func TestFactoryInterfaceFilesMatchTheirPins(t *testing.T) {
	rows := section(t, "guard-pins")
	if len(rows) == 0 {
		t.Fatal("docs/GUARDRAILS.md pins no files (section 4)")
	}
	for _, r := range rows {
		b, err := os.ReadFile(filepath.Join(root, r[0]))
		if err != nil {
			t.Errorf("%s is pinned in docs/GUARDRAILS.md but cannot be read: %v. If it was removed on purpose, remove its row.", r[0], err)
			continue
		}
		sum := sha256.Sum256(b)
		if got := hex.EncodeToString(sum[:]); got != r[1] {
			t.Errorf("%s changed (sha256 %s, pinned %s). Default goldens never move: a change to the factory interface is a decision. If it is intended, put the new hash in docs/GUARDRAILS.md (section 4) and say in the commit what the owner will see differently; if not, restore the file (a stray UPDATE_GOLDEN=1 is the usual cause).",
				r[0], got[:12], r[1][:12])
		}
	}
}
