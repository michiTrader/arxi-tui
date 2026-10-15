package workspacefs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/michiTrader/arxi/internal/workspace"
)

// TestWorktreeProvisionsThroughAnAliasedManagedRoot pins the resolved-path
// contract of worktree provisioning: git registers and reports a worktree by
// its resolved path, while the managed root can be reached through an alias --
// an 8.3 short name on Windows (the CI runner's temp directory lives under
// C:\Users\RUNNER~1\...) or a junction or symlink on any platform. Provision
// must compare and report locations, not spellings: a raw-vs-resolved
// comparison fails every provision through an aliased root with "Git worktree
// root does not match managed workspace path", which is exactly what the
// Windows CI runner delivered once core tests ran there.
func TestWorktreeProvisionsThroughAnAliasedManagedRoot(t *testing.T) {
	probe := testRepo(t)
	alias := aliasPath(t, t.TempDir())
	manager := &Manager{Root: alias}
	req := request(probe, workspace.ModeWorktree, "writer")

	session, err := manager.Provision(context.Background(), req)
	if err != nil {
		t.Fatalf("Provision through an aliased managed root: %v: worktree verification must "+
			"compare the resolved location on both sides; a raw-vs-resolved path comparison "+
			"rejects every legitimate worktree provisioned through a short name or symlink", err)
	}
	root, ok := session.WorkspaceRoot()
	if !ok {
		t.Fatal("a provisioned worktree session reports no workspace root")
	}
	resolvedAlias, err := canonical(alias)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot := filepath.Join(resolvedAlias, "workspaces", safeComponent(req.JobID), safeComponent(req.Member), string(req.Mode))
	if root != wantRoot {
		t.Fatalf("session root = %q, want the resolved workspace path %q: a session rooted at "+
			"the alias spelling would disagree with every path git prints from inside the workspace", root, wantRoot)
	}
	// The second provision takes the exists-and-verify path, which runs the
	// same comparison against the already-registered worktree.
	again, err := manager.Provision(context.Background(), req)
	if err != nil {
		t.Fatalf("re-Provision through the alias: %v: the exists path must verify the same way the first provision did", err)
	}
	if again.Identity() != session.Identity() {
		t.Fatalf("re-Provision returned identity %q, want the first session's %q", again.Identity(), session.Identity())
	}
	if err := manager.Release(context.Background(), req, session); err != nil {
		t.Fatalf("Release through the alias: %v: cleanup must work through the same root spelling the provision reported", err)
	}
}

// aliasPath returns a path that names the same directory as real through an
// alias git resolves to the long form: a junction on Windows (creating one
// needs no privilege, unlike a symlink) and a symlink elsewhere.
func aliasPath(t *testing.T, real string) string {
	t.Helper()
	alias := filepath.Join(filepath.Dir(real), filepath.Base(real)+"-alias")
	var err error
	if runtime.GOOS == "windows" {
		_, err = exec.Command("cmd", "/c", "mklink", "/J", alias, real).CombinedOutput()
	} else {
		err = os.Symlink(real, alias)
	}
	if err != nil {
		t.Skipf("cannot create a path alias here: %v", err)
	}
	t.Cleanup(func() { os.Remove(alias) })
	return alias
}
