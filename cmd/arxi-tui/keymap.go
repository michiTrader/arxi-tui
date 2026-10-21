package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/michiTrader/arxi_tui/internal/expr"
	"github.com/michiTrader/arxi_tui/internal/keymap"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// The keymap as the loop uses it (internal/keymap, docs/DESIGN-OPEN-TUI.md section 5.3).
//
// What a key means is decided by data: the factory keymap says which keys it takes and when,
// and the loop only answers the questions the conditions ask (keymapFacts) and carries out the
// action the binding names. Keys that are still decided in the Go chain of main.go are those
// the keymap does not name yet; moving one is a row in factory.json and a name here, never a
// new branch.

// A consent screen owns every key and is handled before the keymap is asked, so no name for it
// is published: it would always answer no. Add one only where a binding can really see it.
//
// keymapSchema is every name a condition may read, and its kind. A condition that reads
// anything else is refused when the keymap is loaded, with the nearest names.
var keymapSchema = expr.Schema{
	"menu.open":        expr.Bool, // the line starts with "/" and is a menu
	"key.user_bound":   expr.Bool, // the user bound this very key to an action of their own
	"screen.open":      expr.Bool, // the automations, team, flow, providers or installer screen is up
	"approval.pending": expr.Bool, // a change waits for y/n
	"find.open":        expr.Bool, // the find bar is open
	"agent.working":    expr.Bool, // an answer is being written (or a compaction runs)
	"ui.focused":       expr.Bool, // a node of the scene holds the focus, so Enter presses it
}

// keymapState is what the loop knows when a key is pressed. The loop fills one in per key.
type keymapState struct {
	menuOpen        bool
	userBound       bool
	screenOpen      bool
	approvalPending bool
	findOpen        bool
	agentWorking    bool
	uiFocused       bool
}

// lookup answers the questions a condition asks, from one snapshot. Every name of
// keymapSchema must be answered here; a test walks the schema to make sure.
func (s keymapState) lookup(name string) (expr.Value, bool) {
	switch name {
	case "menu.open":
		return expr.B(s.menuOpen), true
	case "key.user_bound":
		return expr.B(s.userBound), true
	case "screen.open":
		return expr.B(s.screenOpen), true
	case "approval.pending":
		return expr.B(s.approvalPending), true
	case "find.open":
		return expr.B(s.findOpen), true
	case "agent.working":
		return expr.B(s.agentWorking), true
	case "ui.focused":
		return expr.B(s.uiFocused), true
	}
	return expr.Value{}, false
}

// keymapActionCheck is the host's rule for what a key may carry: what a shortcut of the user's
// may carry, because a key is pressed by the user and by nobody else.
func keymapActionCheck(do string) error {
	return validateActions("the keymap", actionList{do}, true, "")
}

var (
	factoryKeymapOnce sync.Once
	factoryKeymapMap  *keymap.Map
	factoryKeymapErr  error
)

// factoryKeymap is the keymap the program starts with. An error here is a programming
// mistake (the embedded file is wrong) and a test fails on it; at run time the keyboard
// simply has no data keys and the loop says why.
func factoryKeymap() (*keymap.Map, error) {
	factoryKeymapOnce.Do(func() {
		factoryKeymapMap, factoryKeymapErr = keymap.Parse(keymap.FactoryJSON, keymapSchema, keymapActionCheck)
	})
	return factoryKeymapMap, factoryKeymapErr
}

// keymapTakes says whether the factory keymap names the key, whatever its condition.
func keymapTakes(k term.Key) bool {
	m, err := factoryKeymap()
	return err == nil && m.Takes(k)
}

// resolveKey asks the keymap what a key does right now. A condition that cannot be decided
// comes back as notice, and the key is then not the keymap's.
func resolveKey(m *keymap.Map, k term.Key, st keymapState) (do string, ok bool, notice string) {
	b, ok, err := m.Resolve(k, st.lookup)
	if err != nil {
		return "", false, fmt.Sprintf("%v", err)
	}
	return b.Do, ok, ""
}

// The queue of messages waiting for an answer to end.
const (
	queueMax       = 8
	queueRetries   = 20
	queueRetryWait = 150 * time.Millisecond
)

// busyReporter is the optional capability of a Driver that knows its session is still
// finishing a turn, a moment after the turn_done event was sent.
type busyReporter interface{ Busy() bool }
