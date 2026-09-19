package scene

import (
	"encoding/json"
	"errors"
	"os"
)

// Document is the root of a scene tree. The document is the UI, living as JSON
// on disk, reloaded on change, edited by agents or users.
type Document struct {
	Root *Node `json:"root"`

	// src, file and offsets are the document's address book, kept so a refusal
	// can name a position instead of only a reason (PLAN.md invariant 4). They
	// are unexported and set by the parser: a Document built by hand in a test
	// or synthesized by a future patch path still validates, it just reports
	// the file-only address rather than inventing a line number.
	src     []byte
	file    string
	offsets map[string]int

	// declaredKeys is the set of json keys each object in the source
	// actually wrote, keyed by the same access path as offsets. It exists
	// because the parsed tree cannot answer the question: encoding/json
	// drops an unrecognised key without a trace, and that silence is the
	// defect Warnings() reports. Recorded by the parser for the same
	// reason the offsets are — the token stream is the only place the
	// evidence survives.
	declaredKeys map[string][]string
}

// ParseDocument parses a JSON scene document from bytes. Syntax and type errors
// carry file:line:col, taken from the offset encoding/json already computed —
// dropping it was the measured bug this replaces.
func ParseDocument(data []byte) (*Document, error) {
	return ParseNamed(unnamedSource, data)
}

// ParseNamed is ParseDocument for a document whose origin has a name. The name
// is what the error prints, so a caller that read the bytes from disk passes the
// path and the user gets an address they can open in an editor.
func ParseNamed(name string, data []byte) (*Document, error) {
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, jsonError(name, data, err)
	}
	doc.src = data
	doc.file = name
	doc.offsets, doc.declaredKeys = nodeOffsets(data)
	return &doc, nil
}

// ParseFile reads and parses a scene document from disk. It exists so the path
// reaches the error automatically: the alternative is every caller remembering
// to pass the name it just read from, and the measured history of this rule is
// that an address a caller must remember to supply is an address that goes
// missing.
func ParseFile(path string) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseNamed(path, data)
}

// jsonError converts an encoding/json failure into an addressed scene error.
// Both error types the standard library returns here carry a byte offset, and
// both were previously flattened into a message with %w and no position.
func jsonError(name string, data []byte, err error) error {
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		line, col := position(data, int(syntaxErr.Offset))
		return &Error{
			Loc: Loc{File: name, Line: line, Col: col},
			Msg: "invalid JSON: " + syntaxErr.Error(),
			Err: err,
		}
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		line, col := position(data, int(typeErr.Offset))
		return &Error{
			Loc: Loc{File: name, Line: line, Col: col},
			Msg: "invalid JSON: " + typeErr.Error(),
			Err: err,
		}
	}
	// No offset available: still an addressed error, just file-only. Printing
	// the file alone is honest; printing ":1:1" would not be.
	return &Error{
		Loc: Loc{File: name},
		Msg: "invalid JSON: " + err.Error(),
		Err: err,
	}
}

// Name returns the document's origin as it will appear in errors.
func (d *Document) Name() string {
	if d == nil || d.file == "" {
		return unnamedSource
	}
	return d.file
}

// locOf resolves the address of a node by its access path. A path with no
// recorded offset degrades to the file-only address rather than guessing, which
// keeps a hand-built Document (no source bytes) usable without making its
// errors lie about a line.
func (d *Document) locOf(path string) Loc {
	if d == nil {
		return Loc{}
	}
	off, ok := d.offsets[path]
	if !ok {
		return Loc{File: d.file}
	}
	line, col := position(d.src, off)
	return Loc{File: d.file, Line: line, Col: col}
}
