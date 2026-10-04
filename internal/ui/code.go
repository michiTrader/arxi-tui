package ui

import "strings"

// Syntax colour, at the size a transcript needs and no larger.
//
// This is not a parser and does not want to become one. A transcript shows a fence
// and a diff hunk: twenty lines of code that the reader already knows the shape of,
// where colour is there to separate a comment from a keyword from a literal at a
// glance. Getting that right needs a word list and three delimiters per language,
// which is a table; getting it *provably* right needs a grammar per language, which
// is a dependency, and this project has a policy about those.
//
// Highlight colours one line and keeps no state between calls. That is the same
// decision the markdown renderer makes for the same reason: the text arrives a delta
// at a time and has to look right while it is still arriving, and a highlighter with
// state can get out of step with a stream that is still open. The cost is visible
// and small — a block comment or a raw string that spans rows is coloured on its
// first row only.

// codeStyle keys. They are their own family rather than living under md.* because a
// diff hunk is not markdown, and both draw code.
const (
	codeComment = "markdown.code.comment"
	codeKeyword = "markdown.code.keyword"
	codeType    = "markdown.code.type"
	codeFunc    = "markdown.code.func"
	codeString  = "markdown.code.string"
	codeNumber  = "markdown.code.number"
)

// langSpec is everything this highlighter knows about a language.
type langSpec struct {
	lineComment string
	blockOpen   string
	blockClose  string
	quotes      string // every character that opens a string
	rawQuotes   string // quotes inside which a backslash is not an escape
	keywords    map[string]bool
	types       map[string]bool
	builtins    map[string]bool
}

// words turns a space-separated literal into a set, so the table below reads as a
// list of words rather than as a map literal.
func words(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

var langs = map[string]*langSpec{
	"go": {
		lineComment: "//", blockOpen: "/*", blockClose: "*/",
		quotes: "\"'`", rawQuotes: "`",
		keywords: words(`break case chan const continue default defer else fallthrough for func go goto
			if import interface map package range return select struct switch type var`),
		types: words(`any bool byte complex64 complex128 error float32 float64 int int8 int16 int32
			int64 rune string uint uint8 uint16 uint32 uint64 uintptr nil true false iota`),
		builtins: words(`append cap clear close copy delete len make max min new panic print println recover`),
	},
	"c": {
		lineComment: "//", blockOpen: "/*", blockClose: "*/",
		quotes: "\"'",
		keywords: words(`async await break case catch class const continue default delete do else
			export extern finally for function if import in instanceof let new of return static
			struct switch this throw try typedef typeof var void while yield`),
		types:    words(`bool char double float int long short signed unsigned any boolean number string null true false undefined`),
		builtins: words(`console printf malloc free sizeof require parseInt parseFloat`),
	},
	"py": {
		lineComment: "#",
		quotes:      "\"'",
		keywords: words(`and as assert async await break class continue def del elif else except
			finally for from global if import in is lambda nonlocal not or pass raise return try
			while with yield`),
		types:    words(`bool bytes dict float int list None set str tuple True False self`),
		builtins: words(`enumerate isinstance len open print range sorted sum type zip`),
	},
	"sh": {
		lineComment: "#",
		quotes:      "\"'",
		keywords:    words(`case do done elif else esac fi for function if in local return then until while`),
		types:       words(`export readonly set unset`),
		builtins:    words(`cat cd cp echo exit grep mkdir printf read rm sed test`),
	},
	"json": {
		quotes: "\"",
		types:  words(`true false null`),
	},
}

// langAlias maps what a fence or a log actually says onto the table above.
var langAlias = map[string]string{
	"golang": "go",
	"c++":    "c", "cc": "c", "cpp": "c", "cs": "c", "h": "c", "hpp": "c", "java": "c",
	"js": "c", "json5": "json", "jsonc": "json", "jsx": "c", "kt": "c", "rs": "c",
	"swift": "c", "ts": "c", "tsx": "c",
	"bash": "sh", "shell": "sh", "zsh": "sh",
	"python": "py", "py3": "py",
}

// langFor resolves a fence info string or a diff's lang field. It takes the first
// word, because an info string is allowed to carry more than a language — "go
// {highlight=3}" is still Go — and returns nil for anything unknown, which is how a
// language this table has never heard of comes out as plain text instead of as a
// guess.
func langFor(lang string) *langSpec {
	name := strings.ToLower(strings.TrimSpace(lang))
	if i := strings.IndexAny(name, " \t,;{"); i >= 0 {
		name = name[:i]
	}
	name = strings.TrimPrefix(name, ".")
	if alias, ok := langAlias[name]; ok {
		name = alias
	}
	return langs[name]
}

// Highlight splits one line of code into styled spans. Unclassified text is given
// the base style, so the caller decides what plain code looks like — a fence says
// markdown.codeblock, a diff row says nothing and lets the row's wash answer.
//
// The spans always concatenate back to text exactly. Everything downstream wraps,
// truncates and measures them, so a highlighter that dropped or reordered a byte
// would show up as a corrupted line rather than as a wrong colour.
func Highlight(text, lang, base string) []Span {
	spec := langFor(lang)
	if spec == nil || text == "" {
		return []Span{{Text: text, Style: base}}
	}
	var out []Span
	push := func(s, style string) {
		if s != "" {
			out = append(out, Span{Text: s, Style: style})
		}
	}
	start := 0 // where the current unclassified run began
	for i := 0; i < len(text); {
		if spec.lineComment != "" && strings.HasPrefix(text[i:], spec.lineComment) {
			push(text[start:i], base)
			push(text[i:], codeComment)
			return out
		}
		if spec.blockOpen != "" && strings.HasPrefix(text[i:], spec.blockOpen) {
			end := len(text)
			if j := strings.Index(text[i+len(spec.blockOpen):], spec.blockClose); j >= 0 {
				end = i + len(spec.blockOpen) + j + len(spec.blockClose)
			}
			push(text[start:i], base)
			push(text[i:end], codeComment)
			i, start = end, end
			continue
		}
		c := text[i]
		if strings.IndexByte(spec.quotes, c) >= 0 {
			end := scanString(text, i, strings.IndexByte(spec.rawQuotes, c) < 0)
			push(text[start:i], base)
			push(text[i:end], codeString)
			i, start = end, end
			continue
		}
		if isIdentStart(c) {
			j := i + 1
			for j < len(text) && isIdentPart(text[j]) {
				j++
			}
			if style := spec.classify(text[i:j]); style != "" {
				push(text[start:i], base)
				push(text[i:j], style)
				start = j
			}
			i = j
			continue
		}
		if isDigit(c) && (i == 0 || !isIdentPart(text[i-1])) {
			j := i
			for j < len(text) && (isIdentPart(text[j]) || text[j] == '.') {
				j++
			}
			push(text[start:i], base)
			push(text[i:j], codeNumber)
			i, start = j, j
			continue
		}
		i++
	}
	push(text[start:], base)
	return out
}

// classify looks one word up in the three tables. Order is the order a reader cares
// about: a keyword is structure, a type is a shape, a builtin is a call.
func (s *langSpec) classify(word string) string {
	switch {
	case s.keywords[word]:
		return codeKeyword
	case s.types[word]:
		return codeType
	case s.builtins[word]:
		return codeFunc
	}
	return ""
}

// scanString finds the end of a string literal, or the end of the line when it is
// not closed — which is the common case while a fence is still streaming in.
func scanString(text string, i int, escapes bool) int {
	q := text[i]
	for j := i + 1; j < len(text); j++ {
		if escapes && text[j] == '\\' {
			j++
			continue
		}
		if text[j] == q {
			return j + 1
		}
	}
	return len(text)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}
func isIdentPart(c byte) bool { return isIdentStart(c) || isDigit(c) }
