package main

import (
	"strconv"
	"strings"
)

// This file is the pure keystone the deferred row-click focus wiring rides on:
// the encoding that lets the single `ui.focus` cursor name one pressable node of
// one instantiated template row (Scene 9's `cmd:/agent {row.id}` per member),
// and the decode that recovers that (node, row) pair when Enter fires.
//
// # Why an encoded key rather than a second cursor
//
// Focus is one string (BINDS.md §4.3: "the id of the currently focused node"),
// held in the loop like ui.hidden and re-attached every repaint. The ring the
// loop Tabs over interleaves ordinary pressable ids (pressableIDs) with the
// targets of a row_template's instantiated rows, and a template's authored id
// repeats across every row — so a row target cannot be named by node id alone
// (engine.RowPresses records the same decision: a target is (RowIndex, NodeID),
// never the id). Rather than widen ui.focus into a pair — which would ripple
// through advanceFocus, findPressable, the focus_glow bind and every getter that
// treats focus as one string — a row target is folded into a single synthetic
// key here and unfolded at dispatch. ui.focus stays one string; only this file
// knows the two shapes it can hold.
//
// # Why the NUL marker
//
// The ring mixes these keys with raw author ids, and findPressable/findNodeByID
// match a focused string against a node's raw n.ID. A synthetic key must
// therefore be one no author id can equal, or a row key could collide with an
// ordinary node and Enter would dispatch the wrong node. enterRowKey solves the
// identical problem for the enter-animation clock with a NUL separator ("no
// author-written id contains a NUL"); this reuses that guarantee, so
// parseRowFocusKey's marker test is also the discriminator the dispatcher needs:
// a key with no marker is an ordinary node id (resolve via findPressable), a key
// with one is a template-row target (look the (row, node) up in RowPresses).
const rowFocusMarker = "\x00row\x00"

// rowFocusKey encodes the focus target "node nodeID of instantiated row
// rowIndex" into the single string ui.focus holds. It is the inverse of
// parseRowFocusKey; the two are a round-trip so the loop can Tab onto a row
// target and later recover exactly which element's on_press to expand and
// dispatch.
func rowFocusKey(nodeID string, rowIndex int) string {
	return nodeID + rowFocusMarker + strconv.Itoa(rowIndex)
}

// parseRowFocusKey recovers the (nodeID, rowIndex) a rowFocusKey encoded, and
// reports ok=false for any string that is not one — an ordinary node id, or a
// malformed key whose index is not a non-negative integer. The ok result is the
// discriminator the dispatcher branches on: false means "focus names a plain
// node, resolve it against the document"; true means "focus names an
// instantiated row, look it up in the row-press enumeration". A plain id can
// never report true because no author id contains the NUL marker (see the file
// comment), so the two shapes ui.focus may hold are always distinguishable.
func parseRowFocusKey(key string) (nodeID string, rowIndex int, ok bool) {
	i := strings.Index(key, rowFocusMarker)
	if i < 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(key[i+len(rowFocusMarker):])
	if err != nil || n < 0 {
		return "", 0, false
	}
	return key[:i], n, true
}
