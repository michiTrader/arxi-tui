package scene

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/theme"
)

// These tests protect invariant 4 (PLAN.md: "every error carries file:line:"),
// restated by AGENTS.md as "a refusal without a location is a bug", by
// LESSONS.md as "file:line: everywhere, or the error is a bug", and specified
// concretely by BINDS.md §4.5: an unsigned bind "fails validation at load time
// with a file:line error pointing at the offending node".
//
// The rule was signed in four places and implemented in none: the measured
// behaviour was `unsigned bind "x" in node type "text"` with no position, and
// `invalid JSON: invalid character ']'` — the latter worse, since encoding/json
// had already computed the offset and the wrapper discarded it.

// TestUnsignedBindErrorPointsAtTheOffendingNode is the §4.5 sentence as a test.
// The line is asserted exactly, not merely for presence of a colon: an address
// that points at the wrong node is more expensive than no address, because it
// sends the reader — or, in Phase 2, the model on its repair attempt — to edit a
// line that was correct.
func TestUnsignedBindErrorPointsAtTheOffendingNode(t *testing.T) {
	// The offending node is on line 4, and the document is deliberately
	// deeper than the root so a walk that reports the document's start
	// instead of the node's cannot pass.
	src := []byte(`{
  "root": { "type": "stack", "children": [
    { "id": "chat", "type": "markdown", "bind": "chat.history" },
    { "id": "oops", "type": "text", "bind": "nope.nothere" }
  ]}
}`)
	doc, err := ParseNamed("scene.json", src)
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	err = doc.Validate()
	if err == nil {
		t.Fatal("Validate accepted an unsigned bind\n" +
			"consequence: BINDS.md §4.5 makes an unsigned bind a load-time refusal; accepting one lets vocabulary enter through the engine instead of through a signature.\n" +
			"remedy: keep the signedBinds check in validateBinds.")
	}

	var sceneErr *Error
	if !errors.As(err, &sceneErr) {
		t.Fatalf("Validate returned %T, want *scene.Error\n"+
			"consequence: a bare error cannot carry an address, so invariant 4 degrades silently the moment someone returns fmt.Errorf here.\n"+
			"remedy: return *scene.Error from every refusal in this package.", err)
	}
	if got, want := sceneErr.Loc.Line, 4; got != want {
		t.Errorf("refusal points at line %d, want %d (the node holding the unsigned bind)\n"+
			"consequence: the reader is sent to the wrong line; in Phase 2 the eval corpus measures the repair loop, so a misaddressed error trains the model to patch a node that was already correct.\n"+
			"remedy: check that nodeOffsets' access paths match the paths validateBinds threads through the walk — a mismatch degrades or misplaces the address.",
			got, want)
	}
	if got := sceneErr.Loc.File; got != "scene.json" {
		t.Errorf("refusal names file %q, want %q\n"+
			"consequence: an address without the file is unusable when a scene is assembled from a preset plus a user document.\n"+
			"remedy: thread the parsed name onto the Document and into Loc.", got, "scene.json")
	}
	if !strings.Contains(err.Error(), "scene.json:4") {
		t.Errorf("formatted refusal = %q, want it to contain %q\n"+
			"consequence: the address exists in the struct but not in what the user reads, which satisfies the type and fails the invariant.\n"+
			"remedy: keep Error.Error() prefixing Loc.String().", err.Error(), "scene.json:4")
	}
}

// TestWhenConditionRefusalIsAddressed covers the second half of §4.5's "every
// bind/when string": a `when` is a bind reference too, and it is the one that
// hides in nodes with no `bind` field at all.
func TestWhenConditionRefusalIsAddressed(t *testing.T) {
	src := []byte(`{
  "root": { "type": "stack", "children": [
    { "id": "gate", "type": "text", "text": "hi", "when": "ui.nonexistent" }
  ]}
}`)
	doc, err := ParseNamed("when.json", src)
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	err = doc.Validate()
	if err == nil {
		t.Fatal("Validate accepted an unsigned when condition\n" +
			"consequence: a `when` naming an unsigned bind is a dead conditional — the node either never shows or always shows, and the scene author cannot tell which.\n" +
			"remedy: validate When against signedBinds alongside Bind.")
	}
	var sceneErr *Error
	if !errors.As(err, &sceneErr) {
		t.Fatalf("Validate returned %T, want *scene.Error", err)
	}
	if got, want := sceneErr.Loc.Line, 3; got != want {
		t.Errorf("when refusal points at line %d, want %d\n"+
			"consequence: the address misses the node carrying the condition.\n"+
			"remedy: pass the same path to the When check as to the Bind check.", got, want)
	}
}

// TestBindRefusalInsideNestedStructuresIsAddressed walks the four recursion
// arms validateBinds has — children, prefix, suffix, row_template — because the
// address is built by string concatenation on the way down and a segment name
// that disagrees with nodeOffsets' spelling fails *quietly*: the map lookup
// misses and the error silently degrades to file-only. Nothing else in the suite
// would notice. The row_template arm matters most: SCENES.md Q10 makes templates
// the place relative binds live, so it is exactly where a bind hides.
func TestBindRefusalInsideNestedStructuresIsAddressed(t *testing.T) {
	cases := []struct {
		name string
		src  string
		line int
	}{
		{
			name: "prefix",
			src: `{
  "root": { "type": "stack", "children": [
    { "id": "m", "type": "marquee", "bind": "thinking.text",
      "prefix": { "type": "text", "bind": "bogus.prefix" } }
  ]}
}`,
			line: 4,
		},
		{
			name: "suffix",
			src: `{
  "root": { "type": "stack", "children": [
    { "id": "m", "type": "marquee", "bind": "thinking.text",
      "suffix": { "type": "text", "bind": "bogus.suffix" } }
  ]}
}`,
			line: 4,
		},
		{
			name: "row_template",
			src: `{
  "root": { "type": "stack", "children": [
    { "id": "l", "type": "list", "bind": "team.members",
      "row_template": { "type": "text", "bind": "bogus.row" } }
  ]}
}`,
			line: 4,
		},
		{
			name: "deeply nested children",
			src: `{
  "root": { "type": "stack", "children": [
    { "type": "row", "children": [
      { "type": "stack", "children": [
        { "id": "deep", "type": "text", "bind": "bogus.deep" }
      ]}
    ]}
  ]}
}`,
			line: 5,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc, err := ParseNamed("nested.json", []byte(c.src))
			if err != nil {
				t.Fatalf("ParseNamed: %v", err)
			}
			err = doc.Validate()
			if err == nil {
				t.Fatalf("Validate accepted an unsigned bind in %s\n"+
					"consequence: a bind hidden in a %s escapes §4.5 entirely, so the audit's guarantee has a blind spot in the one place SCENES.md says binds live.\n"+
					"remedy: keep the %s arm in validateBinds' recursion.", c.name, c.name, c.name)
			}
			var sceneErr *Error
			if !errors.As(err, &sceneErr) {
				t.Fatalf("Validate returned %T, want *scene.Error", err)
			}
			if got := sceneErr.Loc.Line; got != c.line {
				t.Errorf("%s refusal points at line %d, want %d\n"+
					"consequence: this is the silent failure mode — nodeOffsets and validateBinds disagree on the path spelling, the lookup misses, and the address degrades or lands on the wrong node with no test noticing.\n"+
					"remedy: the JSON key and the path segment must match; use the childPath/prefixPath/suffixPath/templatePath helpers on both sides.",
					c.name, got, c.line)
			}
		})
	}
}

// TestColumnsCountRunesNotBytes protects a decision that is invisible until it
// is wrong in the shipped product. The scenes this project pins are full of
// multi-byte glyphs by design — `Δr×i` in the header, the `┃` prompt prefix, the
// box-drawing borders — so a byte column points past the character it names in
// the sobria default itself.
func TestColumnsCountRunesNotBytes(t *testing.T) {
	// "Δr×i" is 4 runes and 7 bytes. The unsigned bind's node opens after it
	// on the same line, so a byte column and a rune column disagree here by
	// exactly the multi-byte overhead.
	src := []byte(`{
  "root": { "type": "row", "children": [
    { "type": "text", "text": "Δr×i ┃ ── " }, { "type": "text", "bind": "bogus.x" }
  ]}
}`)
	doc, err := ParseNamed("wide.json", src)
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	err = doc.Validate()
	if err == nil {
		t.Fatal("Validate accepted an unsigned bind")
	}
	var sceneErr *Error
	if !errors.As(err, &sceneErr) {
		t.Fatalf("Validate returned %T, want *scene.Error", err)
	}

	line := strings.Split(string(src), "\n")[sceneErr.Loc.Line-1]
	runes := []rune(line)
	if sceneErr.Loc.Col-1 > len(runes) {
		t.Fatalf("column %d is past the end of a %d-rune line\n"+
			"consequence: the address cannot be resolved by any editor.\n"+
			"remedy: count runes over the line prefix.", sceneErr.Loc.Col, len(runes))
	}
	// The column must land on the node's opening brace. A byte-based count
	// would overshoot it by the multi-byte overhead earlier in the line.
	if got := runes[sceneErr.Loc.Col-1]; got != '{' {
		t.Errorf("column %d of %q is %q, want '{' (the offending node's opening brace)\n"+
			"consequence: columns are being counted in bytes, so every address on a line containing Δ, ┃ or a box-drawing rune — i.e. most lines of the shipped sobria scene — points into the middle of a character.\n"+
			"remedy: use utf8.RuneCount over the line prefix in position(), not a byte delta.",
			sceneErr.Loc.Col, line, got)
	}
}

// TestSyntaxErrorCarriesTheOffsetJSONAlreadyComputed pins the cheapest half of
// the bug: encoding/json reports a byte offset on both SyntaxError and
// UnmarshalTypeError, and the old wrapper dropped it with %w.
func TestSyntaxErrorCarriesTheOffsetJSONAlreadyComputed(t *testing.T) {
	cases := []struct {
		name string
		src  string
		line int
	}{
		{
			// A truncated value: the bad token is on line 4.
			name: "malformed value",
			src:  "{\n  \"root\": {\n    \"type\": \"stack\",\n    \"children\": [ { \"type\": ] }\n  }\n}",
			line: 4,
		},
		{
			// grow is an int; a string there is a type error, on line 3.
			name: "wrong type",
			src:  "{\n  \"root\": { \"type\": \"stack\", \"children\": [\n    { \"type\": \"markdown\", \"grow\": \"yes\" }\n  ]}\n}",
			line: 3,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseNamed("broken.json", []byte(c.src))
			if err == nil {
				t.Fatal("ParseNamed accepted a malformed document")
			}
			var sceneErr *Error
			if !errors.As(err, &sceneErr) {
				t.Fatalf("ParseNamed returned %T, want *scene.Error\n"+
					"consequence: the parse path is the one a hot-reloaded or agent-written document hits first, and it is the path where the offset is free — json already computed it.\n"+
					"remedy: convert json.SyntaxError/UnmarshalTypeError through jsonError.", err)
			}
			if got := sceneErr.Loc.Line; got != c.line {
				t.Errorf("%s refusal points at line %d, want %d\n"+
					"consequence: the user is told the document is broken but not where, which is the exact complaint invariant 4 exists to prevent.\n"+
					"remedy: map the error's Offset through position().", c.name, got, c.line)
			}
			// The cause must survive: callers distinguish a corrupt document
			// from an unsatisfiable one, and errors.As is how.
			var syntaxErr *json.SyntaxError
			var typeErr *json.UnmarshalTypeError
			if !errors.As(err, &syntaxErr) && !errors.As(err, &typeErr) {
				t.Errorf("%s: wrapped cause is unreachable via errors.As\n"+
					"consequence: wrapping that loses the cause forces callers back to string matching on error text.\n"+
					"remedy: keep Error.Unwrap returning the underlying json error.", c.name)
			}
		})
	}
}

// TestUnaddressedDocumentDegradesHonestly protects the decision not to guess. A
// Document built in Go — by a test, or by the Phase 2 patch path before it has
// serialized — has no source bytes, so there is no line to report. Reporting
// "1:1" would be a plausible-looking lie, and a lie in an address is worse than
// its absence: it survives review because it looks like data.
func TestUnaddressedDocumentDegradesHonestly(t *testing.T) {
	doc := &Document{Root: &Node{
		Type:     "stack",
		Children: []*Node{{ID: "x", Type: "text", Bind: "bogus.handbuilt"}},
	}}
	err := doc.Validate()
	if err == nil {
		t.Fatal("Validate accepted an unsigned bind on a hand-built document\n" +
			"consequence: the validator would only work on parsed documents, and the Phase 2 patch path builds trees in memory.\n" +
			"remedy: validation must not depend on the address book existing.")
	}
	var sceneErr *Error
	if !errors.As(err, &sceneErr) {
		t.Fatalf("Validate returned %T, want *scene.Error", err)
	}
	if sceneErr.Loc.Line != 0 {
		t.Errorf("hand-built document reported line %d, want 0 (unknown)\n"+
			"consequence: an invented line number is a lie that looks like data and passes review.\n"+
			"remedy: locOf returns a file-only Loc when no offset is recorded.", sceneErr.Loc.Line)
	}
	if !strings.Contains(err.Error(), "unsigned bind") {
		t.Errorf("message lost its reason: %q\n"+
			"consequence: degrading the address must not degrade the diagnosis.\n"+
			"remedy: Loc.String() renders the file alone when the line is unknown.", err.Error())
	}
}

// TestGoldenScenesValidateWithoutRefusal is the regression floor for this
// change: the three pinned scenes must still load. It is the check that catches
// an over-eager refusal — the failure mode where new validation makes the
// default interface unreachable, which is worse than the bug being fixed.
func TestGoldenScenesValidateWithoutRefusal(t *testing.T) {
	for _, name := range goldenScenes {
		path := filepath.Join("..", "..", "testdata", name)
		doc, err := ParseFile(path)
		if err != nil {
			t.Fatalf("ParseFile %s: %v\n"+
				"consequence: a pinned golden scene no longer parses, so the shipped default interface does not boot.\n"+
				"remedy: correct the parser, not the scene.", name, err)
		}
		if err := doc.Validate(); err != nil {
			t.Errorf("%s: %v\n"+
				"consequence: the default interface is unreachable.\n"+
				"remedy: correct the validator, or sign the bind in docs/BINDS.md §4.", name, err)
		}
		if got := doc.Name(); got != path {
			t.Errorf("%s: Name() = %q, want %q\n"+
				"consequence: errors from a file read off disk would not name the file the user can open.\n"+
				"remedy: ParseFile must pass the path to ParseNamed.", name, got, path)
		}
	}
}

// TestTokenRefusalIsAddressed carries the same rule to the token validator.
// LESSONS.md states it for tokens specifically: "a referenced-but-undefined
// token is an error with file:line, and an unused token is a warning".
func TestTokenRefusalIsAddressed(t *testing.T) {
	src := []byte(`{
  "root": { "type": "stack", "children": [
    { "type": "text", "text": "ok" },
    { "type": "text", "text": "bad", "style": { "token": "nope.missing" } }
  ]}
}`)
	doc, err := ParseNamed("tokens.json", src)
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	errs := ValidateTokens(doc, theme.SOBRIA())
	if len(errs) != 1 {
		t.Fatalf("ValidateTokens returned %d errors, want 1", len(errs))
	}
	if got, want := errs[0].Loc.Line, 4; got != want {
		t.Errorf("token refusal points at line %d, want %d\n"+
			"consequence: the token vocabulary is open by design, so a typo'd token name is the most common scene error there is — and it is the one the user most needs a line number for.\n"+
			"remedy: thread the node path into collectTokenErrors and resolve it through locOf.", got, want)
	}
	if !strings.Contains(errs[0].Error(), "tokens.json:4") {
		t.Errorf("formatted token error = %q, want it to contain %q\n"+
			"consequence: the address is in the struct but not in what the user reads.\n"+
			"remedy: prefix TokenError.Error() with Loc.String().", errs[0].Error(), "tokens.json:4")
	}
}

// TestEveryRefusalInThisPackageIsAddressed is the structural guard, and the
// reason it is worth more than the cases above: those check the refusals that
// exist today, this one constrains the refusals added later. A future validation
// rule returning fmt.Errorf compiles, passes every test above, and silently
// reopens the bug this commit closed. AGENTS.md notes the mechanism — Go has no
// exhaustive match, so the suite is the only net.
func TestEveryRefusalInThisPackageIsAddressed(t *testing.T) {
	// Each case is a document that must be refused, spanning both refusal
	// paths (parse and validate). The assertion is uniform: the error is a
	// *scene.Error and prints a file.
	refused := []struct {
		name string
		src  string
	}{
		{"unsigned bind", `{"root":{"type":"text","bind":"bogus.b"}}`},
		{"unsigned when", `{"root":{"type":"text","text":"x","when":"bogus.w"}}`},
		{"malformed json", `{"root":{"type":`},
		{"wrong field type", `{"root":{"type":"text","grow":"one"}}`},
		{"trailing garbage", `{"root":{"type":"text"}} }`},
	}

	// Both parse entry points are exercised for every case. ParseDocument is
	// not a thin alias worth trusting: it is the exported name with almost
	// every caller in the repository, and it is where the dropped-offset bug
	// actually lived. Checking only ParseNamed left a hole this test was
	// written to close and did not — proven by re-introducing the original bug
	// in ParseDocument alone and watching an earlier version of this suite
	// stay green.
	parsers := []struct {
		name  string
		parse func([]byte) (*Document, error)
		// source is the name each parser is expected to report.
		source string
	}{
		{"ParseNamed", func(b []byte) (*Document, error) { return ParseNamed("guard.json", b) }, "guard.json"},
		{"ParseDocument", ParseDocument, unnamedSource},
	}

	for _, p := range parsers {
		for _, c := range refused {
			t.Run(p.name+"/"+c.name, func(t *testing.T) {
				var err error
				doc, perr := p.parse([]byte(c.src))
				if perr != nil {
					err = perr
				} else {
					err = doc.Validate()
				}
				if err == nil {
					t.Fatalf("no refusal for %s\n"+
						"consequence: this input must be rejected; accepting it means a broken scene reaches the renderer.\n"+
						"remedy: restore the check that refuses it.", c.name)
				}
				var sceneErr *Error
				if !errors.As(err, &sceneErr) {
					t.Fatalf("%s refused %s with %T, want *scene.Error\n"+
						"consequence: a refusal that is not a *scene.Error cannot carry an address, and invariant 4 (\"every error carries file:line:\") is violated by construction.\n"+
						"remedy: return *scene.Error — never a bare fmt.Errorf — from every refusal in this package, and route both parse entry points through jsonError.", p.name, c.name, err)
				}
				if sceneErr.Loc.File == "" {
					t.Errorf("%s refused %s without naming a source\n"+
						"consequence: an address with no file cannot be acted on.\n"+
						"remedy: populate Loc.File from the document's name.", p.name, c.name)
				}
				if !strings.Contains(err.Error(), p.source) {
					t.Errorf("%s/%s: formatted error %q omits the source name %q\n"+
						"consequence: the address never reaches the reader.\n"+
						"remedy: keep Error() prefixing Loc.String().", p.name, c.name, err.Error(), p.source)
				}
			})
		}
	}
}

// TestPositionAgreesWithTheDocumentItAddresses is the arithmetic check, run over
// the real scenes rather than a synthetic string. For every node offset recorded
// in each pinned scene, the reported line must be the line the byte actually
// falls on, computed independently by splitting the file. A single off-by-one in
// position() would shift every address in the project by one line — the classic
// way this machinery is wrong while looking right.
func TestPositionAgreesWithTheDocumentItAddresses(t *testing.T) {
	for _, name := range goldenScenes {
		path := filepath.Join("..", "..", "testdata", name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		offsets := nodeOffsets(data)
		if len(offsets) == 0 {
			t.Fatalf("%s: nodeOffsets recorded nothing\n"+
				"consequence: every refusal in this scene would silently degrade to a file-only address.\n"+
				"remedy: the token walk must record an offset per object; check its path bookkeeping.", name)
		}
		lines := strings.Split(string(data), "\n")
		for path, off := range offsets {
			line, col := position(data, off)
			if line < 1 || line > len(lines) {
				t.Fatalf("%s %s: line %d outside 1..%d", name, path, line, len(lines))
			}
			// The recorded offset is a node's opening brace; verify the
			// byte there really is one, which catches an offset that is
			// off by one in either direction.
			if data[off] != '{' {
				t.Errorf("%s %s: offset %d is byte %q, want '{'\n"+
					"consequence: the address points next to the node instead of at it.\n"+
					"remedy: InputOffset() after a delimiter is one past it; subtract exactly 1.",
					name, path, off, data[off])
			}
			runes := []rune(lines[line-1])
			if col-1 > len(runes) {
				t.Errorf("%s %s: column %d past end of %d-rune line %d\n"+
					"consequence: an unresolvable address.\n"+
					"remedy: reset the column at every newline.", name, path, col, len(runes), line)
			}
		}
	}
}
