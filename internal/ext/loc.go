package ext

import (
	"bytes"
	"encoding/json"
	"unicode/utf8"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// The manifest is a document, and a refusal without a location is a bug
// (AGENTS.md, PLAN.md invariant 4). DESIGN-BLOCK-H.md H1 makes the promise
// concrete: "The validator maps offset → file:line for JSON already; reusing it
// is why a manifest refusal can carry an address for free." This file is the
// small amount of that machinery a manifest needs on its own — a rune-aware
// line/column and the byte offsets of the top-level keys a field-level refusal
// points at. Fragment refusals do not go through here at all: they are rebased
// onto the manifest bytes (see manifest.go) and reuse the scene validator's own
// addressing unchanged, which is the larger half of the "for free" claim.

// unnamedManifest names a manifest parsed from bytes with no path, mirroring
// scene's unnamedSource. Angle-bracketed so it can never be mistaken for a file
// the user could open.
const unnamedManifest = "<manifest>"

// Error is a manifest refusal that carries its address, reusing scene.Loc so the
// file:line formatting is the one every other refusal in the project prints —
// there is no second spelling of an address to drift from. Every error this
// package returns for a bad manifest is one of these.
type Error struct {
	Loc scene.Loc
	Msg string
	// Err is the underlying cause when this error wraps one (a JSON syntax
	// error), so errors.Is/As still reach it.
	Err error
}

func (e *Error) Error() string {
	return e.Loc.String() + ": " + e.Msg
}

func (e *Error) Unwrap() error { return e.Err }

// position converts a byte offset into a 1-based line and column.
//
// It is a copy of scene.position (unexported there) rather than a shared call,
// and the copy is deliberate: this is a pure function over bytes with no
// vocabulary to keep in step — unlike the bind/token inventories this project
// guards against drift, there is nothing here two copies could disagree *about*.
// The column counts runes, not bytes, for scene.position's reason: a manifest is
// full of multi-byte glyphs (a plugin name, a token's label), so a byte column
// would point past the character it means.
func position(src []byte, offset int) (line, col int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(src) {
		offset = len(src)
	}
	line = 1
	lineStart := 0
	for i := 0; i < offset; i++ {
		if src[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}
	col = utf8.RuneCount(src[lineStart:offset]) + 1
	return line, col
}

// topLevelKeyOffsets records the byte offset of each top-level key token in a
// JSON object, so a refusal about a manifest field ("executable", "protocol")
// can point at the key the author wrote rather than at the file's first byte.
//
// It walks only the outermost object and skips over every nested value with
// json.Decoder token depth-counting, so a key spelled the same inside a mount
// fragment does not shadow the top-level one. A malformed document is not this
// function's refusal to make — ParseNamed rejects it carrying the syntax error's
// own offset — so a token error here just stops the walk and returns what parsed.
func topLevelKeyOffsets(data []byte) map[string]int {
	offsets := make(map[string]int)
	dec := json.NewDecoder(bytes.NewReader(data))

	// The first token must be the opening brace of the outermost object; a
	// manifest that is not an object is refused elsewhere with the type error's
	// own address, so here we simply have no keys to record.
	tok, err := dec.Token()
	if err != nil {
		return offsets
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return offsets
	}

	depth := 1 // inside the outermost object
	wantKey := true
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return offsets
		}
		if delim, ok := tok.(json.Delim); ok {
			switch delim {
			case '{', '[':
				depth++
				wantKey = false
			case '}', ']':
				depth--
				// A value just closed at depth 1's level, so the outer object
				// expects a key again.
				if depth == 1 {
					wantKey = true
				}
			}
			continue
		}
		// A scalar at exactly the outermost object's level is either a key or a
		// value; deeper scalars belong to nested containers and are ignored.
		if depth == 1 {
			if wantKey {
				if key, ok := tok.(string); ok {
					// InputOffset() after the token points one byte past the
					// key's closing quote. Manifest keys are simple identifiers
					// with no escapes, so the opening quote is exactly
					// len(key)+2 bytes back (the two quotes) — an exact position
					// rather than the end-of-previous-token the pre-read offset
					// would give.
					end := int(dec.InputOffset())
					offsets[key] = end - len(key) - 2
				}
				wantKey = false
			} else {
				wantKey = true
			}
		}
	}
	return offsets
}
