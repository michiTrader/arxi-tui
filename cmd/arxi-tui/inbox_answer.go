package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// inboxDecider is the optional capability a Driver has when it can answer the
// agent's pending inbox items — the driver half of the closed answer: kind
// vocabulary (approve/reject/reply, BINDS.md §4.8). It is asserted for at the
// answer: dispatch site rather than folded into Driver, for the same reason
// actorLabeler is a separate assertion: the mock driver follows no run and has
// no inbox to answer, so it must not be forced to implement a verb it cannot
// honour. The live serveDriver owns the run id (through the run it is following)
// and fills it in, so the host names only the item — the run is never in
// question at the press, only which of its pending items is answered.
type inboxDecider interface {
	ApproveInboxItem(ctx context.Context, itemID string) error
	RejectInboxItem(ctx context.Context, itemID, reason string) error
	ReplyInboxItem(ctx context.Context, itemID, text string) error
}

// answerInbox routes a parsed answer: kind to the decider's matching verb
// (BINDS.md §4.8: approve/reject/reply mirror the core's inbox.approve/reject/
// reply). It is the host-side join between the closed kind vocabulary and the
// driver's inbox methods; keeping it a single switch means the kind→verb mapping
// exists once, so a press and the verb it fires cannot drift.
//
// text is the operator's free text accompanying the decision: the reason for a
// reject (optional, the driver omits it when empty) and the answer for a reply
// (the substance, always sent). approve carries no text and ignores it — an
// approval is the bare act, so a single free-text field feeds whichever of the
// two text-bearing verbs is pressed.
//
// The default arm is not dead: ParseAction already refuses a kind outside the
// closed set, so an unknown kind is unreachable through the validated scene path
// — but Go does not make this switch exhaustive, so a kind added to answerKinds
// and forgotten here would otherwise be a silent no-op. Naming it names the
// consequence (a signed kind the host silently drops) and the remedy (add its
// arm), the AGENTS.md rule that a missing switch variant is caught by a message,
// not by the compiler.
func answerInbox(ctx context.Context, kind, itemID, text string, dec inboxDecider) error {
	switch kind {
	case "approve":
		return dec.ApproveInboxItem(ctx, itemID)
	case "reject":
		return dec.RejectInboxItem(ctx, itemID, text)
	case "reply":
		return dec.ReplyInboxItem(ctx, itemID, text)
	default:
		return fmt.Errorf("answer:%s has no driver verb; the closed answer kinds are %s (BINDS.md §4.8), so a kind reaching here was signed into the vocabulary without an arm in answerInbox — add one or it is silently dropped", kind, strings.Join(scene.AnswerKinds(), ", "))
	}
}

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
