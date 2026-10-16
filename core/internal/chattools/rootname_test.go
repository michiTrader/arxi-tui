package chattools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// namedProject is a project whose folder is called "pitlane-strategies", with a
// strategies/smc folder inside, the shape of the session this was found in.
func namedProject(t *testing.T) (*Toolbox, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "pitlane-strategies")
	if err := os.MkdirAll(filepath.Join(dir, "strategies", "smc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "strategies", "smc", "_strategy.py"), []byte("x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tb, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	return tb, dir
}

// The model is told "root: pitlane-strategies" and wrote that name as the path. Read
// literally it did not exist and the model concluded the project was empty.
func TestAPathThatStartsWithTheProjectNameIsReadFromTheProject(t *testing.T) {
	tb, _ := namedProject(t)
	for _, p := range []string{"pitlane-strategies", "pitlane-strategies/", "pitlane-strategies/strategies", `pitlane-strategies\strategies`, "PITLANE-STRATEGIES/strategies/smc"} {
		res, err := tb.list(p)
		if err != nil {
			t.Errorf("list(%q): %v", p, err)
			continue
		}
		if res.Text == "" {
			t.Errorf("list(%q) listed nothing", p)
		}
	}
	res, err := tb.list("pitlane-strategies/strategies")
	if err != nil || !strings.Contains(res.Text, "smc/") {
		t.Errorf("list(project/strategies) = %q, %v; want smc/", res.Text, err)
	}
}

// What exists as written wins. A real sub-folder named like the project is not skipped.
func TestARealSubfolderWithTheProjectsNameIsNotSkipped(t *testing.T) {
	tb, dir := namedProject(t)
	inner := filepath.Join(dir, "pitlane-strategies")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, "inner.txt"), []byte("i\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := tb.list("pitlane-strategies")
	if err != nil || !strings.Contains(res.Text, "inner.txt") {
		t.Fatalf("list of the real sub-folder = %q, %v", res.Text, err)
	}
}

// The name is only dropped when what is left is really there: a path that does not
// exist either way still says so, and a path out of the project still refuses.
func TestTheProjectNameIsNotDroppedFromAPathThatIsNotThereEitherWay(t *testing.T) {
	tb, _ := namedProject(t)
	if _, err := tb.list("pitlane-strategies/nothing"); err == nil || !strings.Contains(err.Error(), "pitlane-strategies/nothing does not exist") {
		// The error names the path as the model wrote it, not a shortened one it never asked for.
		t.Errorf("a missing folder must say so, naming the path as asked; got %v", err)
	}
	if _, err := tb.list("pitlane-strategies/../.."); err == nil {
		t.Error("a path leaving the project must still be refused")
	}
	if err := os.MkdirAll(filepath.Join(filepath.Dir(tb.root), "sibling"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := tb.list("../sibling"); err == nil {
		t.Error("a sibling folder must still be refused")
	}
}

func TestReadAndGrepAcceptThePathWithTheProjectNameToo(t *testing.T) {
	tb, _ := namedProject(t)
	res, err := tb.read("pitlane-strategies/strategies/smc/_strategy.py", 0, 0)
	if err != nil || !strings.Contains(res.Text, "x = 1") {
		t.Errorf("read: %q, %v", res.Text, err)
	}
}
