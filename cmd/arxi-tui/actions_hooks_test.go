package main

import (
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// Whatever a hook sets, through any of the three settings, must not fire another hook:
// a hook starting a hook is how a pair of definitions would loop forever. Each setting
// has its own call in the loop, so each one is tried on its own.
func TestLoopHookSetOfEachSettingStartsNoHook(t *testing.T) {
	for _, set := range []string{"set:effort=low", "set:mode=plan", "set:style=band"} {
		t.Run(set, func(t *testing.T) {
			// F5 changes the effort to high. The hook on that does `set`, and every
			// setting has a hook that would print LOOP-... if it were fired.
			seedBehaviour(t, `{"keys":{"f5":"set:effort=high"},"hooks":[`+
				`{"on":"effort","is":"high","do":"`+set+`"},`+
				`{"on":"effort","is":"low","do":"say:LOOP-effort"},`+
				`{"on":"mode","do":"say:LOOP-mode"},`+
				`{"on":"style","do":"say:LOOP-style"}]}`)
			drv := &modeEffortDriver{effortDriver: effortDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}}
			script := append([]scheduledEvent{fkey(term.KeyF5)}, quitScript...)
			tty := runLoop(t, drv, drv.evCh, script)
			if got := frameWith(tty.output(), "LOOP-"); got != "" {
				t.Errorf("%s fired another hook: %s", set, got)
			}
		})
	}
}
