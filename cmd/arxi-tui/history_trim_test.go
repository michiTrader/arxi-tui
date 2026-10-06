package main

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/driver"
)

func turns(spec ...string) []driver.ChatTurn {
	var out []driver.ChatTurn
	for i, s := range spec {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		out = append(out, driver.ChatTurn{Role: role, Text: s})
	}
	return out
}

func texts(h []driver.ChatTurn) string {
	var b []string
	for _, t := range h {
		b = append(b, t.Text)
	}
	return strings.Join(b, ",")
}

func TestTrimHistoryKeepsEverythingWhenSmall(t *testing.T) {
	h := turns("a", "b", "c", "d")
	if got := texts(trimHistory(h, 40, 1000)); got != "a,b,c,d" {
		t.Errorf("got %q", got)
	}
	if got := trimHistory(nil, 40, 1000); len(got) != 0 {
		t.Errorf("nil history gave %v", got)
	}
}

func TestTrimHistoryDropsOldestByCount(t *testing.T) {
	h := turns("a", "b", "c", "d", "e", "f")
	// The window of 3 would start on "d" (assistant), so it moves on to "e".
	if got := texts(trimHistory(h, 3, 1000)); got != "e,f" {
		t.Errorf("got %q, want e,f", got)
	}
	if got := texts(trimHistory(h, 4, 1000)); got != "c,d,e,f" {
		t.Errorf("got %q, want c,d,e,f", got)
	}
}

func TestTrimHistoryDropsOldestByBytes(t *testing.T) {
	big := strings.Repeat("x", 60)
	h := turns("q1", big, "q2", big, "q3", "short")
	// 69 bytes from "q2" on fit in 100; one more long answer would not.
	if got := texts(trimHistory(h, 40, 100)); got != "q2,"+big+",q3,short" {
		t.Errorf("budget 100: got %q", got)
	}
	// With 60 the long answer no longer fits, and neither does what is before it.
	if got := texts(trimHistory(h, 40, 60)); got != "q3,short" {
		t.Errorf("budget 60: got %q, want q3,short", got)
	}
}

func TestTrimHistoryKeepsTheNewestTurnEvenWhenHuge(t *testing.T) {
	h := turns("q1", "a1", strings.Repeat("y", 500))
	got := trimHistory(h, 40, 100)
	if len(got) != 1 || got[0].Role != "user" || len(got[0].Text) != 500 {
		t.Errorf("got %d turns, want only the newest", len(got))
	}
}

func TestTrimHistoryDoesNotAliasTheOriginal(t *testing.T) {
	h := turns("a", "b")
	got := trimHistory(h, 40, 1000)
	got[0].Text = "changed"
	if h[0].Text != "a" {
		t.Error("trimHistory shares memory with the stored history")
	}
}
