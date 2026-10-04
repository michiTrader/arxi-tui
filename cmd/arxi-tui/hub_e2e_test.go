package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// buildCore compiles the vendored core (core/ is a nested module).
func buildCore(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds the core; skipped with -short")
	}
	name := "arxi"
	if runtime.GOOS == "windows" {
		name += ".exe" // Windows will not execute a file without its extension
	}
	bin := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/arxi")
	cmd.Dir = "../../core"
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the core: %v\n%s", err, out)
	}
	return bin
}
