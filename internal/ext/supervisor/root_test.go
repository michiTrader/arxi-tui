package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// This file covers Config.Root: a behavioral package ships a manifest naming its
// executable by a path relative to the package (`./tick`), because that relative
// path is half the consent identity and must not vary by install location. Root
// is where that relative path is resolved at spawn. The proof reuses the same
// re-exec-the-test-binary helper the rest of the package uses, but names the
// executable by its BASE name and supplies its directory as Root — so a launch
// that only succeeds when Root+Executable is joined correctly is the evidence.

// TestSupervisorResolvesRelativeExecutableAgainstRoot proves the resolution in
// both directions. The helper binary is named relatively (its base name) and its
// directory is handed in as Root: the handshake completes and the published bind
// frame arrives, which can only happen if the relative name resolved against Root
// to the real binary. The counterfactual points Root at an empty directory with
// the same relative name — the join now names a file that does not exist, the
// spawn fails, and no frame is forwarded — so the test proves Root is the thing
// that located the executable, not that any non-empty Root happens to work. The
// empty-Root passthrough (an absolute os.Args[0], Root unset) is exercised by
// every other test in this package, so it is not repeated here.
func TestSupervisorResolvesRelativeExecutableAgainstRoot(t *testing.T) {
	relExec := filepath.Base(os.Args[0])
	realDir := filepath.Dir(os.Args[0])

	resolved := helperConfig("", "tick", nil)
	resolved.Manifest.Executable = relExec
	resolved.Root = realDir
	resolved.MaxRestarts = 0

	s := Start(context.Background(), resolved)
	defer s.Close()
	select {
	case f, ok := <-s.Frames():
		if !ok {
			t.Fatalf("Frames closed before any frame; supervisor err=%v\n"+
				"consequence: a relative executable that resolves against a correct Root failed to launch, so an installed behavioral package can never be spawned from its digest-keyed tree", s.Err())
		}
		if f.Type != "bind" {
			t.Fatalf("forwarded frame type = %q, want \"bind\"; the helper launched but did not publish, so the resolution proof is inconclusive", f.Type)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("no frame within 6s; the relative executable did not resolve against Root to a launchable binary")
	}

	// Counterfactual: same relative name, Root pointing at a directory that does
	// not contain it. The join must yield a path that cannot be spawned, so the
	// bind never lands — if this still produced a frame, Root would not be where
	// the executable is resolved and the positive case above proved nothing.
	wrong := helperConfig("", "tick", nil)
	wrong.Manifest.Executable = relExec
	wrong.Root = t.TempDir()
	wrong.MaxRestarts = 0

	w := Start(context.Background(), wrong)
	defer w.Close()
	select {
	case f, ok := <-w.Frames():
		if ok {
			t.Fatalf("a relative executable resolved against a Root that does not contain it still forwarded a frame (%q); the join must name the missing file and the spawn must fail", f.Type)
		}
		// Frames closed with no value: supervision stopped because the spawn failed,
		// which is the expected outcome.
	case <-time.After(6 * time.Second):
		t.Fatal("supervisor neither forwarded a frame nor stopped within 6s for an unresolvable relative executable")
	}
}
