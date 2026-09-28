package main

import (
	"context"

	"github.com/michiTrader/arxi_tui/internal/ext"
	"github.com/michiTrader/arxi_tui/internal/ext/supervisor"
	"github.com/michiTrader/arxi_tui/internal/patch"
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

// consentRequest is the worker→loop message asking the loop to show a consent
// screen and reply with the user's answer. doc is the ext.ConsentScene the worker
// built with the real digest; declared is the capability set the manifest asked
// for; reply carries the answer back to the Prompt the worker is blocked in. It
// crosses goroutines, but doc is built and then never touched again by the worker
// (it parks on the reply receive), so the loop reads it without a race.
type consentRequest struct {
	doc      *scene.Document
	declared []string
	reply    chan supervisor.ConsentAnswer
}

// installOutcome is the worker→loop message carrying an install's final result.
// url is echoed so a message can name what the user typed; sup is the live
// supervisor to hold for `/ui plugin remove` (nil on any failure); installed is
// the laid-out package (non-nil even on a rejection, so a refusal can name what
// was on disk); err distinguishes a rejection (supervisor.ErrConsentRejected)
// from a fetch/install/spawn failure.
type installOutcome struct {
	url       string
	sup       *supervisor.Supervisor
	installed *ext.Installed
	err       error
}

// startInstall launches the install worker for one `/ui plugin install <url>`.
// The whole fetch→install→mount thread runs on the goroutine so the loop is never
// blocked on the network (invariant 6: the panic gesture must stay live through a
// hung download). The consent step bridges back to the loop: the Prompt the thread
// hands supervisor.Mount builds the consent screen with the real digest — the one
// place that digest can reach the screen (installBehavioralPlugin's promptFor doc)
// — sends it to the loop over consentReq, and blocks on a per-request reply channel
// the loop answers through the modal. A remembered plugin never reaches the Prompt,
// so no screen is shown and the thread runs to a silent remount. The final result,
// success or failure, always lands on done exactly once so the loop can clear busy.
func startInstall(
	ctx context.Context,
	url string,
	fetch patch.Fetcher,
	pluginsRoot string,
	gate *ext.Gate,
	store *ext.PluginStore,
	reg *supervisor.Registry,
	consentReq chan<- consentRequest,
	done chan<- installOutcome,
) {
	promptFor := func(digest string) supervisor.Prompt {
		return func(m *ext.Manifest, declared []string) (supervisor.ConsentAnswer, error) {
			doc, err := ext.ConsentScene(m, digest)
			if err != nil {
				// The screen could not be built, so the user cannot be asked. Mount
				// treats a Prompt error as an abort with no spawn — the safe default,
				// "no answer is not a yes" — which is exactly right here.
				return supervisor.ConsentAnswer{}, err
			}
			reply := make(chan supervisor.ConsentAnswer, 1)
			consentReq <- consentRequest{doc: doc, declared: declared, reply: reply}
			return <-reply, nil
		}
	}
	go func() {
		s, installed, err := installBehavioralPlugin(ctx, url, fetch, pluginsRoot, gate, store, reg, promptFor)
		done <- installOutcome{url: url, sup: s, installed: installed, err: err}
	}()
}
