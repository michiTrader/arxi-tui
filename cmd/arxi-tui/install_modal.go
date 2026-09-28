package main

import (
	"github.com/michiTrader/arxi_tui/internal/ext/supervisor"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// installModal is the loop-side state of an in-progress `/ui plugin install
// <url>`. The install itself runs on a worker goroutine — a network fetch must
// never block the loop, or the panic gesture (invariant 6) would be dead for the
// length of a hung download — and this struct is the only state the loop keeps
// about it: whether one is running, and whether a consent screen is currently up
// capturing keys.
//
// It is deliberately not the orchestration: the worker owns fetch→install→mount
// (installBehavioralPlugin), and supervisor.Mount owns the decide→grant→spawn
// sequence. This holds just the two loop-visible bits so the select stays thin and
// the security-load-bearing key mapping is the one already pinned as a pure
// function (consentAnswerForKey). The channel send in handleKey is the one impure
// step, and it is here rather than in the loop so the "a decided key answers and
// closes, any other key leaves the prompt standing" transition can be tested
// without the select.
type installModal struct {
	// busy is true from the moment a `/ui plugin install` kicks off a worker until
	// that worker's outcome arrives. A second install while busy is refused: two
	// concurrent consent screens would race the single input focus, and the
	// remembered-grant contract expects one identity decided at a time. busy can be
	// true with consent nil — while the bundle is still fetching, or on a silent
	// remount of an already-trusted plugin (Mount never prompts then).
	busy bool
	// consent is non-nil exactly while the consent screen is up: the worker has
	// reached Mount's prompt for an unseen plugin and is blocked awaiting the
	// answer this modal will send.
	consent *pendingConsent
}

// pendingConsent is the live consent screen and the reply path back to the worker
// blocked in supervisor.Mount's Prompt. doc is the ext.ConsentScene the worker
// built with the real digest (it holds the identity the grant binds to); declared
// is the capability set the answer grants verbatim (what Prompt received, which
// Mount.Grant re-checks against the declared and closed sets); reply carries the
// user's answer to the blocked Prompt and is buffered so the loop's send never
// blocks even if the worker has not yet parked on the receive.
type pendingConsent struct {
	doc      *scene.Document
	declared []string
	reply    chan supervisor.ConsentAnswer
}

// capturing reports whether the consent screen is up and therefore owns the
// keyboard. The loop renders doc and routes every non-panic key through handleKey
// while this is true, and renders the normal scene otherwise.
func (im *installModal) capturing() bool { return im.consent != nil }

// beginConsent puts the consent screen up. It is called from the loop when the
// worker's Prompt sends its request, not from the worker: the doc is built on the
// worker (where the manifest and digest live) but shown here, so the loop owns
// what is on screen and the worker owns only the bytes it computed.
func (im *installModal) beginConsent(doc *scene.Document, declared []string, reply chan supervisor.ConsentAnswer) {
	im.consent = &pendingConsent{doc: doc, declared: declared, reply: reply}
}

// handleKey routes a key pressed while the consent screen is up. It ALWAYS reports
// consumed=true when a screen is up: the user is answering a yes/no, not typing
// chat, so a key that is not one of the three choices is swallowed and leaves the
// prompt standing ("no answer is not a yes", Q15) rather than falling through to
// the input line, where a 'y' meant for the prompt would otherwise land in the
// chat buffer. On one of the three choices (y/r/n or Esc) it sends the answer on
// the pending reply channel — the worker's Prompt is blocked on that receive — and
// clears the screen; busy stays true until Mount finishes granting and spawning
// and the outcome arrives.
//
// Ctrl-C is not handled here: the loop checks the panic gesture before any
// per-mode key handling (invariant 6), so the escape hatch never reaches a consent
// prompt and a modal can never capture it.
func (im *installModal) handleKey(k term.Key) (consumed bool) {
	if im.consent == nil {
		return false
	}
	answer, decided := consentAnswerForKey(k, im.consent.declared)
	if !decided {
		return true
	}
	im.consent.reply <- answer
	im.consent = nil
	return true
}
