package main

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

func TestClassifySlash(t *testing.T) {
	user := []string{"deep"}
	cases := []struct {
		line string
		kind slashKind
		name string
		args string
	}{
		{"hello", slashNone, "", ""},
		{"", slashNone, "", ""},
		{"  hello /help", slashNone, "", ""},
		{"/", slashBare, "", ""},
		{"  /  ", slashBare, "", ""},
		{"/help", slashKnown, "help", ""},
		{"  /help  ", slashKnown, "help", ""},
		{"/max chat", slashKnown, "max", "chat"},
		{"/ui color accent bold", slashKnown, "ui", "color accent bold"},
		{"/send /help", slashKnown, "send", "/help"},
		{"/provider add", slashKnown, "provider", "add"},
		// Spellings the host also accepts although the menu lists one name.
		{"/providers", slashKnown, "providers", ""},
		{"/login", slashKnown, "login", ""},
		// A command the user added.
		{"/deep", slashKnown, "deep", ""},
		// Registered in the menu, with no behaviour behind it yet.
		{"/surface", slashPlanned, "surface", ""},
		{"/surface main", slashPlanned, "surface", "main"},
		// Everything else is unknown, including near misses and other casing.
		{"/nonsense", slashUnknown, "nonsense", ""},
		{"/helpx", slashUnknown, "helpx", ""},
		{"/Help", slashUnknown, "Help", ""},
		{"/usr/bin/ls -l", slashNone, "", ""},
		{"/mnt/c/Users/x/file.txt", slashNone, "", ""},
		{"//double", slashNone, "", ""},
		{"/send /mnt/c/x", slashKnown, "send", "/mnt/c/x"},
		{"/nonsense\nsecond line", slashUnknown, "nonsense", "second line"},
		{"/nonsense\targ", slashUnknown, "nonsense", "arg"},
	}
	for _, c := range cases {
		got := classifySlash(c.line, user)
		if got.Kind != c.kind || got.Name != c.name || got.Args != c.args {
			t.Errorf("classifySlash(%q) = %+v, want kind=%v name=%q args=%q", c.line, got, c.kind, c.name, c.args)
		}
	}
}

func TestClassifySlashKnowsEveryRegisteredCommand(t *testing.T) {
	for _, c := range fold.Commands {
		got := classifySlash("/"+c.Name, nil)
		if got.Kind == slashUnknown {
			t.Errorf("/%s is in fold.Commands but classifies as unknown.\nConsequence: the host would refuse a command its own menu lists.\nRemedy: add it to the classifier's known set.", c.Name)
		}
	}
}

func TestClassifySlashPlannedCommandsAreRegistered(t *testing.T) {
	// A planned command is one the menu lists; if it is not registered the
	// "recognised but not available" answer would be a lie.
	registered := map[string]bool{}
	for _, c := range fold.Commands {
		registered[c.Name] = true
	}
	for name := range plannedCommands {
		if !registered[name] {
			t.Errorf("plannedCommands has %q, which is not in fold.Commands", name)
		}
	}
}

func TestSlashWarnings(t *testing.T) {
	unk := classifySlash("/nonsense", nil).warning()
	for _, want := range []string{"Unknown command /nonsense.", "Nothing was sent", "/help", "/send"} {
		if !strings.Contains(unk, want) {
			t.Errorf("unknown warning %q lacks %q", unk, want)
		}
	}
	pl := classifySlash("/surface", nil).warning()
	for _, want := range []string{"/surface", "not available", "Nothing was sent"} {
		if !strings.Contains(pl, want) {
			t.Errorf("planned warning %q lacks %q", pl, want)
		}
	}
	if strings.Contains(pl, "Unknown") {
		t.Errorf("a recognised command must not be called unknown: %q", pl)
	}
	for _, line := range []string{"hello", "/help", "/"} {
		if w := classifySlash(line, nil).warning(); w != "" {
			t.Errorf("classifySlash(%q).warning() = %q, want none", line, w)
		}
	}
}

func TestSlashWarningNeverEchoesControlBytes(t *testing.T) {
	w := classifySlash("/bad\x1b[31mname", nil).warning()
	if strings.ContainsRune(w, 0x1b) {
		t.Errorf("warning carries an escape byte: %q", w)
	}
}
