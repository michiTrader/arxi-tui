package main

import (
	"time"
)

// typeLine types a string one key at a time, the way a person does.
func typeLine(s string) []scheduledEvent {
	var out []scheduledEvent
	for _, r := range s {
		out = append(out, scheduledEvent{5 * time.Millisecond, keyEvent(r)})
	}
	return out
}

// quit is the two-press panic gesture that ends a loop run.
func quit() []scheduledEvent {
	return []scheduledEvent{
		{150 * time.Millisecond, ctrlCharEvent('c')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
	}
}
