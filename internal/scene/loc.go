package scene

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The address machinery. Four documents sign the same rule from four angles:
// PLAN.md invariant 4 ("every error carries file:line:"), AGENTS.md ("a refusal
// without a location is a bug"), LESSONS.md ("file:line: everywhere, or the
// error is a bug" — the line number is the feature, paid for in arxi-sim's
// internal/config), and BINDS.md §4.5, which specifies the exact refusal this
// file addresses: an unsigned bind "fails validation at load time with a
// file:line error pointing at the offending node".
//
// It was measured missing. The validator refused with `unsigned bind "x" in
// node type "text"` and encoding/json refused with `invalid character ']'
// looking for beginning of value` — the second one is the worse case, because
// the standard library had computed the byte offset and the wrapper threw it
// away.
//
// Why this lands before Phase 2's eval corpus rather than after: PLAN.md gates
// the /ui work on a corpus that "measures the repair loop, not the first shot:
// order -> patch -> validator error -> retry". The validator error is the only
// input the model gets on the retry. A corpus scored against unaddressed
// refusals would be measuring the model's ability to guess which of several
// hundred lines the engine meant, would bake that handicap into every recorded
// score, and — as with the bind inventory drift before it — would have to be
// rewritten once the addresses appeared. Cheaper now, by the same argument and
// for the same reason.

// Loc is a resolved position in a scene document: the file it came from and a
// 1-based line and column. The zero Loc means "no position known", which
// formats as the file alone rather than as a lie like "file:0:0".
type Loc struct {
	File string
	Line int
	Col  int
}

// String renders the address in the `file:line:col` form the project's error
// discipline requires, degrading gracefully when a position is unknown so a
// caller never has to decide whether the numbers can be trusted.
func (l Loc) String() string {
	file := l.File
	if file == "" {
		// A document parsed from bytes with no name still deserves an
		// address; naming the origin honestly beats printing ":12:5:",
		// which reads like a truncated path.
		file = unnamedSource
	}
	if l.Line <= 0 {
		return file
	}
	if l.Col <= 0 {
		return fmt.Sprintf("%s:%d", file, l.Line)
	}
	return fmt.Sprintf("%s:%d:%d", file, l.Line, l.Col)
}

// unnamedSource names a document that was parsed from bytes rather than read
// from a path — the embedded factory scenes and most tests. It is angle-bracketed
// so it can never be mistaken for a file the user could open and edit.
const unnamedSource = "<scene>"

// Error is a scene refusal that carries its address. Every error this package
// returns is one of these, so a caller printing `%v` gets the location without
// having to ask for it — the discipline has to be structural, because an
// optional address is one that goes missing under deadline.
type Error struct {
	Loc Loc
	Msg string
	// Err is the underlying cause when this error wraps one (a json syntax
	// error, for instance), so errors.Is/As still reach it.
	Err error
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Loc.String(), e.Msg)
}

func (e *Error) Unwrap() error { return e.Err }

// position converts a byte offset into a 1-based line and column.
//
// The column counts runes, not bytes. Scene documents are full of multi-byte
// glyphs by design — `Δr×i`, the `┃` prompt prefix, the box-drawing borders —
// so a byte column would point past the character it means in exactly the
// documents this project ships, and the address would be wrong in the sobria
// default itself.
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
	// utf8.RuneCount over the line prefix, not a byte delta: see above.
	col = utf8.RuneCount(src[lineStart:offset]) + 1
	return line, col
}

// nodePathRoot is the address of the document's root node. Node paths in this
// package are the JSON access path — `root.children.2.prefix` — which is also
// the "absolute address" PLAN.md names as the thing that makes "add a row above
// the input" a patch instead of a fork. Phase 2 addresses nodes; the validator
// already has to, so the two use one notion of address rather than two.
const nodePathRoot = "root"

// childPath, prefixPath, suffixPath and templatePath build the address of a
// child from its parent's. They exist as functions rather than inline string
// concatenation because validateBinds and nodeOffsets must agree on the spelling
// exactly: a mismatch would silently drop the address (the map lookup misses and
// the error degrades to file-only) rather than fail loudly, so the spelling
// lives in one place.
func childPath(parent string, i int) string {
	return parent + ".children." + strconv.Itoa(i)
}

// The segment names are the JSON keys, not the Go field names, because the
// address is printed for someone editing the document.
func prefixPath(parent string) string   { return parent + ".prefix" }
func suffixPath(parent string) string   { return parent + ".suffix" }
func templatePath(parent string) string { return parent + ".row_template" }

// nodeOffsets walks the raw JSON and records the byte offset of every object's
// opening brace, keyed by its access path.
//
// It is a second pass over the source rather than a field on Node, because a
// custom UnmarshalJSON cannot know the offset of the value it is decoding —
// encoding/json hands it a detached []byte. The token stream is the only place
// the offsets exist, and json.Decoder.InputOffset() after a '{' delimiter points
// exactly one byte past that brace, which makes the node's own position exact
// rather than approximated from a nearby comma.
//
// Why offsets for objects and not for the `bind` string itself: §4.5 asks for an
// error "pointing at the offending node", and the node is the object. A bind is
// also frequently written on the same line as its node, so the two agree in
// practice and the object is the one the spec names.
func nodeOffsets(data []byte) map[string]int {
	offsets := make(map[string]int)
	dec := json.NewDecoder(bytes.NewReader(data))

	// Each frame is one open container. An object frame names the value that
	// follows with its most recently read key; an array frame names it with the
	// index of the element about to be read. The concatenation of the open
	// frames' segments is therefore the path of the value the decoder is at,
	// which is what makes this walk emit the same strings validateBinds builds
	// from the tree.
	type frame struct {
		isArray bool
		idx     int    // next array index, only meaningful when isArray
		key     string // last key read, only meaningful for an object
		wantKey bool   // inside an object, keys and values alternate
	}
	var stack []frame

	// pathTo is the address of the value the decoder is about to read. The root
	// value, with nothing open, is "root" — the document's own top-level key is
	// consumed as an object key like any other, so the path of the scene tree
	// comes out as "root" and its children as "root.children.0".
	pathTo := func() string {
		var b strings.Builder
		for _, f := range stack {
			var seg string
			if f.isArray {
				seg = strconv.Itoa(f.idx)
			} else {
				seg = f.key
			}
			if seg == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte('.')
			}
			b.WriteString(seg)
		}
		return b.String()
	}

	// consumed records that a value has just been read, so the enclosing frame
	// moves on: an array steps to the next index, an object expects a key again.
	consumed := func() {
		if len(stack) == 0 {
			return
		}
		top := &stack[len(stack)-1]
		if top.isArray {
			top.idx++
			return
		}
		top.wantKey = true
	}

	for {
		tok, err := dec.Token()
		if err != nil {
			// A malformed document is not this function's refusal to make:
			// ParseDocument already rejects it carrying the syntax error's own
			// offset. Offsets for the prefix that did parse stay useful.
			return offsets
		}

		if delim, ok := tok.(json.Delim); ok {
			switch delim {
			case '{':
				// InputOffset() after a delimiter token points one byte past
				// it, so the brace itself — the node's position — is -1.
				if p := pathTo(); p != "" {
					offsets[p] = int(dec.InputOffset()) - 1
				}
				stack = append(stack, frame{wantKey: true})
			case '[':
				stack = append(stack, frame{isArray: true})
			case '}', ']':
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
				// The container that just closed was itself a value.
				consumed()
			}
			continue
		}

		// A scalar: either the key naming the next value, or a value.
		if len(stack) > 0 && !stack[len(stack)-1].isArray && stack[len(stack)-1].wantKey {
			if key, ok := tok.(string); ok {
				stack[len(stack)-1].key = key
			}
			stack[len(stack)-1].wantKey = false
			continue
		}
		consumed()
	}
}
