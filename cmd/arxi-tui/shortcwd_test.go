package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShortCwd(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory")
	}
	if got := shortCwd(home); got != "~" {
		t.Errorf("home = %q", got)
	}
	if got := shortCwd(filepath.Join(home, "a", "b")); got != "~"+string(os.PathSeparator)+"a"+string(os.PathSeparator)+"b" {
		t.Errorf("under home = %q", got)
	}
	long := string(os.PathSeparator) + strings.Repeat("x", 80) + string(os.PathSeparator) + "project"
	got := shortCwd(long)
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "project") || len([]rune(got)) > 40 {
		t.Errorf("long path = %q", got)
	}
	if shortCwd("") != "" {
		t.Error("empty stays empty")
	}
}
