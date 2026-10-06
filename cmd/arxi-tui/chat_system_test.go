package main

import (
	"strings"
	"testing"
	"time"
)

func TestChatSystemCarriesTodaysDate(t *testing.T) {
	old := chatNow
	chatNow = func() time.Time { return time.Date(2026, 10, 6, 23, 59, 0, 0, time.UTC) }
	defer func() { chatNow = old }()
	got := chatSystem()
	if !strings.HasPrefix(got, chatSystemPrompt) || !strings.Contains(got, "Today is 2026-10-06.") {
		t.Errorf("system = %q", got)
	}
}
