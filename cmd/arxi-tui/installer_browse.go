package main

import (
	"github.com/michiTrader/arxi_tui/internal/ext"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// installerBrowse is the loop-side state of an open community installer: the
// fetched registry index, the search query the user has typed, and the selection
// cursor. It is the browse analogue of installModal — host state the loop keeps
// while a mode owns the screen — and, like consentAnswerForKey, its transitions
// are pure so they are proven without the tty.
//
// It exists so the keystroke loop's LOGIC lands and is guarded before its tty
// wiring, the same order FilterEntries (the filter core) landed ahead of the loop
// that calls it. Three things the deferred "live half" named all live here as
// pure methods: the search recompute (FilterEntries on every keystroke), the
// cursor clamp on a filter and wrap on ↑/↓ (BINDS.md §4.3: "clamps it to the
// match list on every filter keystroke", "wraps at both ends", "resets it to 0
// when the installer reopens"), and the RegistryEntry→CommunityMatch conversion
// fold.go names as the host's job. What is deliberately NOT here is the impure
// half that comes next: a command to open the installer, the scene swap that puts
// LiveInstallerScene on the display, term.Key routing, and Enter dispatching the
// selected entry's manifest_url through the consent gate. Keeping this core free
// of term and of the display keeps it a pure state machine the loop drives, not
// the loop itself.
//
// The conversion boundary is the reason this is host state and not an ext or fold
// type. fold.CommunityMatch mirrors ext.RegistryEntry field-for-field precisely
// so the fold never imports the registry's fetch/parse surface (ADR-0002: the
// fold is pure host-owned state), and ext never imports the fold. The one place
// that copies one into the other is therefore the host — here — and a dropped
// field would be a silent blank in the browse, so the conversion is covered by a
// fidelity test rather than trusted.
type installerBrowse struct {
	reg      *ext.Registry
	query    string
	selected int
}

// newInstallerBrowse opens a browse over an index. The query starts empty (the
// browse is open but unfiltered, so every entry is listed) and the cursor at 0
// (the first match is highlighted) — the reset BINDS.md §4.3 signs for reopening
// the installer. A nil index is legal and yields an empty browse: an installer
// opened before its index has been fetched shows no rows, the honest empty state,
// not a crash.
func newInstallerBrowse(reg *ext.Registry) *installerBrowse {
	return &installerBrowse{reg: reg}
}

// matches is the fold's community.matches: the index entries filtered by the
// typed query, converted to CommunityMatch. FilterEntries is the pure filter core
// (empty query → all, case-insensitive substring over name/description); the
// conversion copies the six entry fields into the mirrored struct, the ext→fold
// boundary that keeps the fold from importing ext. A nil registry filters to
// nothing through FilterEntries' own nil guard, so this is always safe to call.
func (b *installerBrowse) matches() []fold.CommunityMatch {
	entries := b.reg.FilterEntries(b.query)
	out := make([]fold.CommunityMatch, len(entries))
	for i, e := range entries {
		out[i] = fold.CommunityMatch{
			ID:          e.ID,
			Name:        e.Name,
			Version:     e.Version,
			ManifestURL: e.ManifestURL,
			Description: e.Description,
			Preview:     e.Preview,
		}
	}
	return out
}

// typeRune appends a rune to the query and re-clamps the cursor: a keystroke that
// shrinks the match list must not leave the highlight past the last row, or the
// browse would show no bright row while Enter would still act on the clamped one
// — the exact drift the slash block guards inline.
func (b *installerBrowse) typeRune(r rune) {
	b.query += string(r)
	b.clampSelection()
}

// backspace removes the last rune of the query and re-clamps. It edits over
// []rune, not bytes, so deleting one character of a multi-byte glyph does not
// leave a broken half in the query the search then filters on.
func (b *installerBrowse) backspace() {
	if b.query == "" {
		return
	}
	rs := []rune(b.query)
	b.query = string(rs[:len(rs)-1])
	b.clampSelection()
}

// moveUp and moveDown step the cursor by one, wrapping at both ends: the signed
// behaviour for community.selected (§4.3), and the reason the browse can be
// driven with a single key that always lands on a row rather than dead-ending at
// the top or bottom. On an empty match list the cursor rests at 0.
func (b *installerBrowse) moveUp()   { b.move(-1) }
func (b *installerBrowse) moveDown() { b.move(+1) }

func (b *installerBrowse) move(delta int) {
	n := len(b.matches())
	if n == 0 {
		b.selected = 0
		return
	}
	// The +n before the final %n makes the result non-negative for a downward
	// wrap from row 0: Go's % keeps the sign of the dividend, so (0-1)%n is -1,
	// and ((-1)+n)%n is the last row. An upward wrap needs the same guard once
	// selected can exceed n after a filter, which clampSelection prevents but the
	// formula tolerates regardless.
	b.selected = ((b.selected+delta)%n + n) % n
}

// clampSelection pulls the cursor back into the current match list. It is called
// after every query edit, not after a move (a move wraps and is already in
// range), so the one place the cursor can fall out of range — the list shrinking
// under it — is the one place it is corrected. On an empty list the cursor is 0,
// which is out of range for zero rows but is the value community.selected carries
// for an empty browse; the renderer draws no row bright, so 0 here is the empty
// state, not a highlight on a row that does not exist.
func (b *installerBrowse) clampSelection() {
	n := len(b.matches())
	if n == 0 {
		b.selected = 0
		return
	}
	if b.selected >= n {
		b.selected = n - 1
	}
	if b.selected < 0 {
		b.selected = 0
	}
}

// publish writes the browse's view state onto the fold the renderer reads. It is
// the single place the community.* triple is set together, so a repaint cannot
// publish a query without its matches, or a selection past the list: the three
// are one consistent snapshot of the same browse, the invariant the slash block
// keeps by setting slash.typed/matches/selected in one place. The host calls this
// in repaint while the installer is on screen, exactly where it sets the slash.*
// triple while the menu is open.
func (b *installerBrowse) publish(state *fold.State) {
	state.CommunityQuery = b.query
	state.CommunityMatches = b.matches()
	state.CommunitySelected = b.selected
}
