package main

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// frameWith is the plain text of the last frame that shows what, "" when none does.
func frameWith(out, what string) string {
	frames := strings.Split(out, frameBegin)
	for i := len(frames) - 1; i >= 0; i-- {
		if f := strings.Join(strings.Fields(ansi.Strip(frames[i])), " "); strings.Contains(f, what) {
			return f
		}
	}
	return ""
}
