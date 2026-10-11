package main

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

func TestParseSend(t *testing.T) {
	cases := []struct {
		line    string
		text    string
		matched bool
	}{
		{"/send hi", "hi", true},
		{"/send   hi there  ", "hi there", true},
		{"/send /help", "/help", true},
		{"/send /send x", "/send x", true},
		{"/send a\nb", "a\nb", true},
		{"/send\tx", "x", true},
		{"/send", "", true},
		{"/send   ", "", true},
		{"  /send hi", "hi", true},
		// Not the command: a longer word, a bare word, another command.
		{"/sendx hi", "", false},
		{"/sending", "", false},
		{"send hi", "", false},
		{"hi /send x", "", false},
		{"/help", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		text, matched := parseSend(c.line)
		if text != c.text || matched != c.matched {
			t.Errorf("parseSend(%q) = (%q, %v), want (%q, %v)", c.line, text, matched, c.text, c.matched)
		}
	}
}

func TestSendIsInTheRegistryAfterUI(t *testing.T) {
	ui, send := -1, -1
	for i, c := range fold.Commands {
		switch c.Name {
		case "ui":
			ui = i
		case "send":
			send = i
		}
	}
	if send < 0 {
		t.Fatal("/send is not in fold.Commands.\nConsequence: the menu never lists it and a user command could take the name.\nRemedy: add it after ui.")
	}
	if send != ui+1 {
		t.Errorf("send is at %d, want right after ui (%d)", send, ui)
	}
}

func TestLoopSendSubmitsExactlyTheTextAfterTheCommand(t *testing.T) {
	cases := []struct{ line, want string }{
		{"/send hi", "hi"},
		{"/send /help", "/help"},
		{"/send   spaced   out  ", "spaced   out"},
	}
	for _, c := range cases {
		t.Run(c.line, func(t *testing.T) {
			script := append(typed(c.line), scheduledEvent{10 * time.Millisecond, enterEvent()})
			script = append(script, exitKeys()...)
			got := runLoopKeys(t, script)
			if want := []string{c.want}; !reflect.DeepEqual(got, want) {
				t.Errorf("submitted %q, want %q", got, want)
			}
		})
	}
}

func TestLoopBareSendWarnsAndSendsNothing(t *testing.T) {
	script := append(typed("/send"), scheduledEvent{10 * time.Millisecond, enterEvent()})
	script = append(script, exitKeys()...)
	out, got := runLoopKeysOutput(t, script)
	if len(got) != 0 {
		t.Errorf("a bare /send submitted %q; nothing may reach the model", got)
	}
	if !strings.Contains(ansi.Strip(out), "! /send needs text") {
		t.Errorf("a bare /send showed no usage warning; output:\n%s", out)
	}
}

func TestLoopSendWordPickedFromTheMenuDoesNotSendTheWord(t *testing.T) {
	// "/sen" leaves send as the only row; Enter on it must not submit the
	// word "send" as a prompt.
	script := append(typed("/sen"), scheduledEvent{10 * time.Millisecond, enterEvent()})
	script = append(script, typed("hello")...)
	script = append(script, scheduledEvent{10 * time.Millisecond, enterEvent()})
	script = append(script, exitKeys()...)
	got := runLoopKeys(t, script)
	if want := []string{"hello"}; !reflect.DeepEqual(got, want) {
		t.Errorf("submitted %q, want %q (the menu pick must fill \"/send \" and the text after it is what is sent)", got, want)
	}
}
