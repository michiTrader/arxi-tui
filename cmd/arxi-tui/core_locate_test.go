package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
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
