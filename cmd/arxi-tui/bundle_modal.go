package main

import (
	"context"

	"github.com/michiTrader/arxi_tui/internal/ext"
	"github.com/michiTrader/arxi_tui/internal/patch"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// bundleModal is the loop-side state of an in-progress `/ui plugin bundle <url>`,
// the bundle sibling of installModal. The resolve-then-plan work runs on a worker
// goroutine — a network fetch of the bundle document plus N plugin archives must
// never block the loop, or the panic gesture (invariant 6) would be dead for the
// length of a hung download — and this struct is the only state the loop keeps about
// it: whether one is running, and whether the one bundle consent screen is currently
// up capturing keys.
//
// It is deliberately not the orchestration: the worker owns
// resolveBundle→BundleConsentScene→planBundleCompose (startBundleInstall), and the
// loop owns executing the returned bundleComposePlan (theme.Merge, patch.Mount the
// scene, supervisor.Start each config). This holds just the two loop-visible bits so
// the select stays thin and the security-load-bearing key mapping is the one already
// pinned as a pure function (bundleAnswerForKey).
//
// # Why a bundle ALWAYS shows the screen, unlike a single plugin
//
// installModal can be busy with consent nil — a remembered single plugin silently
// remounts, no screen. A bundle is different: DESIGN-BLOCK-J.md makes the one screen
// a named confirm of the whole interface being installed, listing already-remembered
// plugins as trusted-no-new-power, so the user sees the full cost even when every
// plugin is remembered and even when the bundle ships only a scene and theme. So the
// worker always reaches the consent bridge, and busy is true-with-consent-nil only
// during the fetch/lay-out that precedes the screen.
type bundleModal struct {
	// busy is true from the moment a `/ui plugin bundle` kicks off a worker until
	// that worker's outcome arrives. A second bundle install while busy is refused,
	// and the loop also refuses a single-plugin install while a bundle is busy (and
	// vice versa): two concurrent consent screens would race the single input focus,
	// and the fan-out grants against the one shared gate one identity at a time.
	busy bool
	// consent is non-nil exactly while the bundle consent screen is up: the worker
	// has built BundleConsentScene and is blocked awaiting the single y/r/n answer
	// this modal will send.
	consent *pendingBundleConsent
}

// pendingBundleConsent is the live bundle consent screen and the reply path back to
// the worker blocked in startBundleInstall's bridge. doc is the
// ext.BundleConsentScene the worker built (it holds the bundle identity and one
// identity+capability block per plugin needing a fresh grant); reply carries the
// user's single answer to the blocked worker and is buffered so the loop's send
// never blocks even if the worker has not yet parked on the receive.
//
// Unlike pendingConsent there is no `declared` set: a bundle answer carries no
// per-capability selection (a bundle is all-or-nothing), so bundleAnswerForKey needs
// only the key and the declared sets live in the fan-out's per-plugin decisions.
type pendingBundleConsent struct {
	doc   *scene.Document
	reply chan ext.BundleAnswer
}

// capturing reports whether the bundle consent screen is up and therefore owns the
// keyboard. The loop renders doc and routes every non-panic key through handleKey
// while this is true, and renders the normal scene otherwise.
func (bm *bundleModal) capturing() bool { return bm.consent != nil }

// beginConsent puts the bundle consent screen up. It is called from the loop when
// the worker's bridge sends its request, not from the worker: the doc is built on
// the worker (where the resolved bundle lives) but shown here, so the loop owns what
// is on screen and the worker owns only the bytes it computed.
func (bm *bundleModal) beginConsent(doc *scene.Document, reply chan ext.BundleAnswer) {
	bm.consent = &pendingBundleConsent{doc: doc, reply: reply}
}

// handleKey routes a key pressed while the bundle consent screen is up. It ALWAYS
// reports consumed=true when a screen is up: the user is answering a yes/no, not
// typing chat, so a key that is not one of the three choices is swallowed and leaves
// the prompt standing ("no answer is not a yes", Q15) rather than falling through to
// the input line, where a 'y' meant for the prompt would land in the chat buffer. On
// one of the three choices (y/r/n or Esc) it sends the answer on the pending reply
// channel — the worker is blocked on that receive — and clears the screen; busy
// stays true until planBundleCompose finishes and the outcome arrives.
//
// Ctrl-C is not handled here: the loop checks the panic gesture before any per-mode
// key handling (invariant 6), so the escape hatch never reaches a consent prompt and
// a modal can never capture it.
func (bm *bundleModal) handleKey(k term.Key) (consumed bool) {
	if bm.consent == nil {
		return false
	}
	answer, decided := bundleAnswerForKey(k)
	if !decided {
		return true
	}
	bm.consent.reply <- answer
	bm.consent = nil
	return true
}

// bundleConsentRequest is the worker→loop message asking the loop to show the one
// bundle consent screen and reply with the user's single answer. doc is the
// ext.BundleConsentScene the worker built from the resolution; reply carries the
// answer back to the worker blocked on the receive. It crosses goroutines, but doc
// is built and then never touched again by the worker (it parks on the reply
// receive), so the loop reads it without a race.
type bundleConsentRequest struct {
	doc   *scene.Document
	reply chan ext.BundleAnswer
}

// bundleOutcome is the worker→loop message carrying a bundle install's final result.
// url is echoed so a message can name what the user typed; name is the bundle's
// human label (empty until the bundle parsed) for the mounted/rejected message;
// plan is the granted compose plan the loop executes (nil on any failure or a
// rejection); err distinguishes a rejection (ext.ErrBundleRejected) from a
// fetch/resolve/grant failure.
type bundleOutcome struct {
	url  string
	name string
	plan *bundleComposePlan
	err  error
}

// startBundleInstall launches the bundle install worker for one `/ui plugin bundle
// <url>`. The whole resolve→consent→plan thread runs on the goroutine so the loop is
// never blocked on the network (invariant 6: the panic gesture must stay live
// through a hung download of the bundle document or any of its N plugin archives).
//
// The consent step bridges back to the loop: the worker builds the one
// BundleConsentScene from the resolution — the screen shows the bundle identity and
// every plugin's terms — sends it to the loop over consentReq, and blocks on a
// per-request reply channel the loop answers through the modal. Unlike a single
// plugin, a bundle ALWAYS reaches this bridge (the screen is a named confirm even
// when every plugin is remembered), so there is no silent path.
//
// planBundleCompose then fans the single answer out to N per-plugin grants and pairs
// each back to its laid-out package, or returns ext.ErrBundleRejected on an `n`. The
// worker grants (the gate is the loop's, shared with the single-plugin path, which
// is why the loop refuses overlapping installs) and plans, but spawns and mounts
// nothing: the plan is one value the loop executes in full or not at all, carrying
// GrantBundle's all-or-nothing shape into the loop. The result, success, rejection
// or failure, always lands on done exactly once so the loop can clear busy.
func startBundleInstall(
	ctx context.Context,
	url string,
	bundleFetch patch.Fetcher,
	archiveFetch patch.Fetcher,
	pluginsRoot string,
	gate *ext.Gate,
	consentReq chan<- bundleConsentRequest,
	done chan<- bundleOutcome,
) {
	go func() {
		res, err := resolveBundle(url, bundleFetch, archiveFetch, pluginsRoot, gate)
		if err != nil {
			done <- bundleOutcome{url: url, err: err}
			return
		}

		// The one screen is a named confirm of the whole install: the bundle's
		// name/description lead it, and ConsentsFor projects the per-plugin decisions
		// into the identity+capability blocks (remembered plugins listed as
		// trusted-no-new-power). It is built here, where the resolution lives, and
		// shown by the loop.
		doc, err := ext.BundleConsentScene(res.bundle.Name, res.bundle.Description, ext.ConsentsFor(res.decisions))
		if err != nil {
			// The screen could not be built, so the user cannot be asked. Treat it as
			// an abort with no grant — the safe default, "no answer is not a yes".
			done <- bundleOutcome{url: url, name: res.bundle.Name, err: err}
			return
		}
		reply := make(chan ext.BundleAnswer, 1)
		consentReq <- bundleConsentRequest{doc: doc, reply: reply}
		answer := <-reply

		plan, err := planBundleCompose(res, gate, answer)
		done <- bundleOutcome{url: url, name: res.bundle.Name, plan: plan, err: err}
	}()
}
