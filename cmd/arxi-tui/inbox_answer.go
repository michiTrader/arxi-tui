package main

import "github.com/michiTrader/arxi_tui/internal/fold"

// inboxItemID resolves the inbox item id an `answer:` press targets from the
// item the run is currently blocked on, which the fold projects as
// agent.blocked.blocked_ref (BINDS.md §4.2). It returns the id and true when a
// blocked item names one, and ("", false) when nothing is answerable.
//
// # Why the item id lives here and not in the action string
//
// The scene's approve button names only the *kind* — the closed
// approve/reject/reply vocabulary ParseAction refuses outside of (scene/action.go)
// — never which item. Which item it answers is the one the run is blocked on:
// the core turns a policy "ask" (a tool.call_denied) into an inbox item and emits
// agent.blocked with a blocked_ref carrying that item's inbox_id — the same id
// `arxi inbox approve <inbox_id>` addresses (spec/events.md, BINDS.md §4.2). So
// the host sources the id from the fold at press time, the way the remedy string
// is derived from blocked_ref rather than authored into the scene.
//
// # Why absence is (…, false) and never "" sent onward
//
// An answer whose item id was silently blanked would ask the core to approve
// nothing, or — worse — read as an addressed request the core honours against
// the wrong item. That is the wrong-frame failure this project holds to be worse
// than a loud refusal (the same rule ExpandRowInterpolation applies to a missing
// {row.<field>}: name it, never expand to ""). So a nil blocked_ref, a missing
// inbox_id, a non-string inbox_id, and an empty-string inbox_id are all "nothing
// to answer" — the caller reports it, and no empty item id ever reaches the
// driver's inbox verbs.
func inboxItemID(state fold.State) (string, bool) {
	if state.BlockedRef == nil {
		return "", false
	}
	id, ok := state.BlockedRef["inbox_id"].(string)
	if !ok || id == "" {
		return "", false
	}
	return id, true
}
