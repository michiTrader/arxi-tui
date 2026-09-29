package main

import (
	"context"
	"errors"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/ext"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// These tests pin the bundleModal's capture-and-answer contract and the
// startBundleInstall worker's resolve→consent→plan thread. The keypress→answer
// mapping is bundleAnswerForKey's (tested there); what is pinned here is the modal's
// job on top of it — while a screen is up it owns EVERY non-panic key so nothing
// meant for the prompt leaks into chat — and the worker's bridge: it resolves, shows
// the one screen, and turns the single answer into a compose plan or a rejection.

func TestBundleModalNotCapturingWhenIdle(t *testing.T) {
	var bm bundleModal
	if bm.capturing() {
		t.Fatal("a fresh bundle modal reports it is capturing keys; with no consent screen up, keys must reach the normal input path")
	}
	if bm.handleKey(runeKey('y')) {
		t.Fatal("an idle bundle modal consumed a key; with no screen up it must decline every key so the loop routes it normally")
	}
}

func TestBundleModalGrantAnswersTheWorkerAndCloses(t *testing.T) {
	var bm bundleModal
	reply := make(chan ext.BundleAnswer, 1)
	bm.beginConsent(&scene.Document{}, reply)
	if !bm.capturing() {
		t.Fatal("after beginConsent the modal must report capturing so the loop shows the screen and routes keys to it")
	}
	if !bm.handleKey(runeKey('r')) {
		t.Fatal("a key pressed against the screen must be consumed, not passed to chat")
	}
	select {
	case ans := <-reply:
		if ans.Rejected {
			t.Fatal("'r' is a grant-and-remember, not a rejection")
		}
		if !ans.Remember {
			t.Fatal("'r' must ask the gate to remember the per-plugin grants; without Remember the bundle re-prompts every install")
		}
	default:
		t.Fatal("a decided key produced no answer on the reply channel; the worker is blocked on that receive and would hang forever")
	}
	if bm.capturing() {
		t.Fatal("the screen must close once answered; a second answer to a blocked-then-returned worker would send on a channel nobody reads")
	}
}

func TestBundleModalUndecidedKeyLeavesScreenStanding(t *testing.T) {
	var bm bundleModal
	reply := make(chan ext.BundleAnswer, 1)
	bm.beginConsent(&scene.Document{}, reply)
	if !bm.handleKey(runeKey('x')) {
		t.Fatal("while a screen is up EVERY key must be consumed; an unconsumed 'x' would fall through to the chat buffer or the slash menu")
	}
	select {
	case ans := <-reply:
		t.Fatalf("an undecided key produced an answer %+v; a stray key must not grant or reject the bundle (no answer is not a yes, Q15)", ans)
	default:
	}
	if !bm.capturing() {
		t.Fatal("the screen must stay up after an undecided key; closing it would abandon the install with no decision")
	}
}

// answerBundleConsent plays the loop side of the consent bridge for a worker test:
// it receives the worker's consent request, shows it on a fresh modal, and answers
// with the given key, then returns the worker's final outcome. It mirrors exactly
// what the real select does (beginConsent on the request, handleKey to answer), so
// the worker is exercised through the same handshake the loop uses.
func answerBundleConsent(t *testing.T, consentReq chan bundleConsentRequest, done chan bundleOutcome, key term.Key) bundleOutcome {
	t.Helper()
	var bm bundleModal
	for {
		select {
		case req := <-consentReq:
			bm.beginConsent(req.doc, req.reply)
			if !bm.handleKey(key) {
				t.Fatal("the consent key was not consumed by the modal; a real keypress against the screen must always be")
			}
		case out := <-done:
			return out
		}
	}
}

func TestStartBundleInstallGrantsAndPlans(t *testing.T) {
	root := t.TempDir()
	gate := ext.NewGate(ext.NewMemoryConsentStore())
	fetch := routingFetcher{byURL: map[string][]byte{
		bundleFetchURL: []byte(bundleWithTick),
		tickPluginURL:  buildInstallBundle(t, behavioralBundleJSON, false),
	}}
	consentReq := make(chan bundleConsentRequest)
	done := make(chan bundleOutcome, 1)

	startBundleInstall(context.Background(), bundleFetchURL, fetch, fetch, root, gate, consentReq, done)
	out := answerBundleConsent(t, consentReq, done, runeKey('y'))

	if out.err != nil {
		t.Fatalf("a granted bundle returned an error: %v; the worker must produce a plan the loop can execute", out.err)
	}
	if out.plan == nil {
		t.Fatal("a granted bundle produced no compose plan; the loop would have nothing to mount and the install would silently do nothing")
	}
	if len(out.plan.configs) != 1 {
		t.Fatalf("the plan carries %d supervisor configs, want 1 (the one plugin the bundle references); a mismatch means the fan-out and the lay-out disagree", len(out.plan.configs))
	}
	if len(out.plan.scene) == 0 {
		t.Fatal("the plan dropped the bundle's embedded scene; the loop would compose no interface for a bundle that shipped one")
	}
	if out.name != "Trading Desk" {
		t.Fatalf("the outcome names the bundle %q, want \"Trading Desk\"; the loop's success message reads this", out.name)
	}
}

func TestStartBundleInstallRejectionPlansNothing(t *testing.T) {
	root := t.TempDir()
	gate := ext.NewGate(ext.NewMemoryConsentStore())
	fetch := routingFetcher{byURL: map[string][]byte{
		bundleFetchURL: []byte(bundleWithTick),
		tickPluginURL:  buildInstallBundle(t, behavioralBundleJSON, false),
	}}
	consentReq := make(chan bundleConsentRequest)
	done := make(chan bundleOutcome, 1)

	startBundleInstall(context.Background(), bundleFetchURL, fetch, fetch, root, gate, consentReq, done)
	out := answerBundleConsent(t, consentReq, done, runeKey('n'))

	if !errors.Is(out.err, ext.ErrBundleRejected) {
		t.Fatalf("a rejected bundle returned err=%v, want ErrBundleRejected; the loop reports 'you rejected this bundle' off that sentinel", out.err)
	}
	if out.plan != nil {
		t.Fatal("a rejected bundle produced a non-nil plan; the loop could compose a bundle the user said no to")
	}
}

func TestStartBundleInstallReportsAFetchFailure(t *testing.T) {
	root := t.TempDir()
	gate := ext.NewGate(ext.NewMemoryConsentStore())
	// The bundle document itself is missing, so resolveBundle fails before any
	// screen: the worker must report the failure on done without ever asking for
	// consent (there is nothing resolved to consent to).
	fetch := routingFetcher{byURL: map[string][]byte{}}
	consentReq := make(chan bundleConsentRequest)
	done := make(chan bundleOutcome, 1)

	startBundleInstall(context.Background(), bundleFetchURL, fetch, fetch, root, gate, consentReq, done)
	select {
	case out := <-done:
		if out.err == nil {
			t.Fatal("a bundle whose document could not be fetched returned no error; the failure must be reported, not swallowed into a silent no-op")
		}
		if out.plan != nil {
			t.Fatal("a failed fetch produced a plan; nothing was resolved, so there is nothing to compose")
		}
	case <-consentReq:
		t.Fatal("the worker asked for consent despite a failed fetch; there is no resolved bundle to show, so no screen must appear")
	}
}
