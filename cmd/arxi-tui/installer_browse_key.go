package main

import "github.com/michiTrader/arxi_tui/internal/term"

// browseKeyResult reports what one key did to an open community installer browse.
//
// close is set by Esc: the browse should close and the normal scene return to the
// display, the analogue of Esc closing the slash menu.
//
// installURL is set by Enter on a highlighted entry: the selected entry's
// manifest_url, which the loop hands startInstall — the SAME install path a typed
// `/ui plugin install <url>` takes, so a card the user pressed and a line they
// typed cannot diverge on how a plugin is fetched, laid out and consent-gated.
//
// Both zero means the key edited the browse in place (typed a query rune, moved
// the cursor, deleted a character) and the loop only has to repaint.
type browseKeyResult struct {
	close      bool
	installURL string
}

// routeBrowseKey applies one key to the open browse and reports the loop-visible
// outcome. It is the browse analogue of installModal.handleKey and slashMenuKey:
// while the installer is open it OWNS the keyboard the way the consent modal does,
// so a key that is not a recognised navigation or edit is swallowed rather than
// falling through to the chat input — a query character meant for the search box
// must never reach the transcript behind it, and an unhandled key that fell
// through would let a stray rune land in the input the installer covers.
//
// It is a pure function of the browse and the key, which is why it is testable
// without the tty and why the term coupling lives here in the impure-half file
// rather than in installer_browse.go: that file's comment deliberately keeps the
// pure state machine term-free, and this is the term.Key routing it named as the
// deferred wiring. The state edits it delegates to (typeRune/backspace/moveUp/
// moveDown) are the pinned pure transitions; this only decides which one a key
// means.
//
// Enter on an empty match list is a no-op (installURL stays empty): there is no
// entry under the cursor, so a stray Enter on a browse filtered to nothing does
// nothing rather than dispatching a fetch of the empty string. A Ctrl/Alt chord
// is swallowed too — the browse owns the keyboard and has nothing to route a
// chord to — but Ctrl-C never reaches here at all: the loop checks the panic
// gesture before any per-mode key handling (invariant 6), so the escape hatch
// cannot be captured by the installer.
func routeBrowseKey(b *installerBrowse, k term.Key) browseKeyResult {
	switch k.Type {
	case term.KeyEscape:
		return browseKeyResult{close: true}
	case term.KeyEnter:
		ms := b.matches()
		if b.selected >= 0 && b.selected < len(ms) {
			return browseKeyResult{installURL: ms[b.selected].ManifestURL}
		}
		return browseKeyResult{}
	case term.KeyUp:
		b.moveUp()
	case term.KeyDown:
		b.moveDown()
	case term.KeyBackspace:
		b.backspace()
	case term.KeyRunes:
		if k.Mod&(term.ModCtrl|term.ModAlt) == 0 {
			for _, r := range k.Runes {
				b.typeRune(r)
			}
		}
	}
	return browseKeyResult{}
}
