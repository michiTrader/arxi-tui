package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeRules(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRulesPreferArxiMdOverAgentsMd(t *testing.T) {
	dir := t.TempDir()
	writeRules(t, dir, "AGENTS.md", "from agents")
	if r := loadProjectRules(dir); r.Name != "AGENTS.md" || r.Text != "from agents" {
		t.Fatalf("got %+v", r)
	}
	writeRules(t, dir, "ARXI.md", "from arxi")
	if r := loadProjectRules(dir); r.Name != "ARXI.md" || r.Text != "from arxi" {
		t.Fatalf("ARXI.md must win: %+v", r)
	}
}

func TestRulesAbsentEmptyAndOffAreNoRules(t *testing.T) {
	dir := t.TempDir()
	if r := loadProjectRules(dir); r.Text != "" || r.Prompt() != "" || r.Notice() != "" {
		t.Fatalf("no file must mean no rules: %+v", r)
	}
	writeRules(t, dir, "AGENTS.md", " \n ")
	if r := loadProjectRules(dir); r.Text != "" {
		t.Fatalf("a blank file is no rules: %+v", r)
	}
	writeRules(t, dir, "AGENTS.md", "x")
	t.Setenv(rulesOffEnv, "off")
	if r := loadProjectRules(dir); r.Text != "" {
		t.Fatalf("switched off, got %+v", r)
	}
	if r := loadProjectRules(""); r.Text != "" {
		t.Fatalf("no folder, got %+v", r)
	}
}

func TestRulesAreCappedAndSaySo(t *testing.T) {
	dir := t.TempDir()
	writeRules(t, dir, "AGENTS.md", strings.Repeat("é", rulesMax))
	r := loadProjectRules(dir)
	if len(r.Text) > rulesMax || !r.Cut || !strings.Contains(r.Notice(), "only the first") {
		t.Fatalf("len=%d cut=%v notice=%q", len(r.Text), r.Cut, r.Notice())
	}
	if !strings.Contains(r.Prompt(), "not by the user") {
		t.Errorf("the model must be told whose rules these are: %q", r.Prompt()[:80])
	}
}

func TestRulesIgnoreALinkToAFileElsewhere(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	outside := t.TempDir()
	writeRules(t, outside, "secret.md", "outside")
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Skip(err)
	}
	if r := loadProjectRules(dir); r.Text != "" {
		t.Fatalf("a link out of the folder must not be read: %+v", r)
	}
}

func TestRulesNoticeIsShownOncePerVersion(t *testing.T) {
	c := &chatSession{}
	if !c.rulesSeen("a") || c.rulesSeen("a") || !c.rulesSeen("b") {
		t.Fatal("a notice must show when new and not repeat until it changes")
	}
}
