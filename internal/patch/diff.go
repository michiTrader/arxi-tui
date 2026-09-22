package patch

import (
	"encoding/json"
	"fmt"
	"strings"
)

// This file computes the change one patch made, as a line-level diff over the
// canonical serialisation of the scene. It is the data the change-diff view
// (PLAN.md, Phase 2) renders before a patch is trusted, and it is deliberately
// separate from how that view draws: the diff is a fact about two byte streams,
// and whether it is shown as a scene or by the engine (the still-open ADR) does
// not change what the fact is.
//
// # Why the diff is computed against a re-serialised old source
//
// Apply always re-serialises the whole tree through json.MarshalIndent, so the
// new source is canonically formatted no matter how the old one was written. A
// diff of the new bytes against the *original* bytes would therefore report
// every reindented line as a change — the user's two-space file diffs clean,
// but a tab-indented or minified one diffs as "everything changed", which is
// precisely the noise the summary() comment in patch.go warns a diff must not
// be. So both sides are put through the same MarshalIndent first: the only
// lines that then differ are the ones the command actually altered.
//
// # Why line-level and not node-level
//
// A node-addressed diff (hunks keyed by node id) was the sketch in PLAN.md, and
// it is the right target eventually. It is not the first cut, because the honest
// unit of a source-to-source transformation is the source: the patch package's
// whole thesis is that the document is text with an address book, and the change
// the user approves is the change to that text. A line diff over the canonical
// form is exact, needs no second parse, and cannot disagree with the bytes that
// get persisted. Node-addressing is a presentation the view can layer on top by
// mapping a hunk's line back through the address book; it is not a different
// truth about what changed.

// Op is what happened to one line between the old and new canonical source.
type Op int

const (
	// OpEqual: the line is unchanged and present on both sides.
	OpEqual Op = iota
	// OpDelete: the line was in the old source and is gone.
	OpDelete
	// OpInsert: the line is new.
	OpInsert
)

// DiffLine is one line of the change, carrying both line numbers so a
// side-by-side view can place it without recomputing them.
//
// OldLine is 0 for an inserted line and NewLine is 0 for a deleted line; an
// equal line carries both. The numbers are 1-based to match the file:line
// addresses everything else in this project prints, so a hunk the view shows
// lines up with a refusal the validator would address.
type DiffLine struct {
	Op      Op
	Text    string
	OldLine int
	NewLine int
}

// Diff is the full aligned line sequence for one patch.
//
// The whole sequence is kept, equal lines included, rather than only the
// changed lines: a side-by-side view needs the unchanged context to place the
// change, and a consumer that wants only the changes (a one-line summary, a
// hunk grouping) can filter, while a consumer handed only the changes could
// never reconstruct the context.
type Diff struct {
	Lines []DiffLine
}

// Changed reports whether the diff contains any insertion or deletion. A patch
// that re-serialises to identical canonical bytes changed nothing the user can
// see, and the view should say so rather than drawing an empty frame.
func (d Diff) Changed() bool {
	for _, l := range d.Lines {
		if l.Op != OpEqual {
			return true
		}
	}
	return false
}

// canonical re-serialises source bytes the same way Apply does, so a diff of
// two canonical forms shows only semantic changes and not formatting.
//
// It unmarshals into `any` (not scene.Node) for the reason apply does: a
// scene.Node round-trip drops every key the type does not declare, and the diff
// must show the document the user actually has, including fields a future phase
// or a plugin fragment carries.
func canonical(src []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(src, &v); err != nil {
		return nil, fmt.Errorf("scene is not valid JSON: %w", err)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("could not canonicalise the scene: %w", err)
	}
	return out, nil
}

// DiffSource computes the line-level diff between two scene sources.
//
// Both sides are canonicalised first (see the file comment), so the caller may
// pass the raw old source and the raw new source and trust that reformatting is
// not reported as a change. newSrc is normally already canonical because it
// came from Apply, but it is canonicalised anyway so the function has one
// contract regardless of where its inputs came from.
func DiffSource(oldSrc, newSrc []byte) (Diff, error) {
	oldCanon, err := canonical(oldSrc)
	if err != nil {
		return Diff{}, fmt.Errorf("old source: %w", err)
	}
	newCanon, err := canonical(newSrc)
	if err != nil {
		return Diff{}, fmt.Errorf("new source: %w", err)
	}
	return diffLines(splitLines(oldCanon), splitLines(newCanon)), nil
}

// splitLines breaks canonical source into lines. MarshalIndent emits no
// trailing newline, so there is no empty final element to special-case.
func splitLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	return strings.Split(string(b), "\n")
}

// diffLines aligns two line sequences by their longest common subsequence and
// emits the classic delete-then-insert order within each changed region.
//
// The LCS is the standard choice rather than a smarter heuristic because it is
// exact and small: a scene document is tens of lines, so the O(n*m) table costs
// nothing, and an exact diff means the view never shows a change that is really
// two unrelated edits fused, or splits one edit across the file. Delete-before-
// insert matches what every diff tool prints, so a reader who has seen a diff
// before reads this one without relearning it.
func diffLines(a, b []string) Diff {
	// lcs[i][j] is the length of the longest common subsequence of a[i:] and
	// b[j:]. Built from the end so the forward walk below reads naturally.
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	var lines []DiffLine
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			lines = append(lines, DiffLine{Op: OpEqual, Text: a[i], OldLine: i + 1, NewLine: j + 1})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			// Dropping a[i] keeps at least as much in common as dropping b[j],
			// so a[i] is a deletion. Ties break towards delete so the pairing
			// with the insert branch below is the conventional order.
			lines = append(lines, DiffLine{Op: OpDelete, Text: a[i], OldLine: i + 1})
			i++
		default:
			lines = append(lines, DiffLine{Op: OpInsert, Text: b[j], NewLine: j + 1})
			j++
		}
	}
	for ; i < len(a); i++ {
		lines = append(lines, DiffLine{Op: OpDelete, Text: a[i], OldLine: i + 1})
	}
	for ; j < len(b); j++ {
		lines = append(lines, DiffLine{Op: OpInsert, Text: b[j], NewLine: j + 1})
	}
	return Diff{Lines: lines}
}
